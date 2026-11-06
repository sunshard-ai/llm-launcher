//go:build windows

package hardware

import (
	"context"

	"llmlauncher/internal/domain"
)

func probePlatform(ctx context.Context, h *domain.Hardware) {
	// Fixed script: no user input is interpolated. Measure-Object.Sum is Double,
	// so an explicit Int32 cast preserves the integer JSON contract. @() keeps
	// zero/one item GPU and disk results as arrays inside the output object.
	// Win32_VideoController.AdapterRAM can truncate above 4 GB; nvidia-smi is
	// used separately for trustworthy NVIDIA VRAM. command() has a 12 s limit.
	script := `[Console]::OutputEncoding=[System.Text.UTF8Encoding]::new($false); $ErrorActionPreference='Stop'; $os=Get-CimInstance Win32_OperatingSystem; $cpu=@(Get-CimInstance Win32_Processor); $gpu=@(Get-CimInstance Win32_VideoController); [ordered]@{OS=[string]$os.Caption; CPU=($cpu.Name -join ' / '); LogicalCPUs=[int](($cpu.NumberOfLogicalProcessors | Measure-Object -Sum).Sum); TotalKB=[uint64]$os.TotalVisibleMemorySize; FreeKB=[uint64]$os.FreePhysicalMemory; GPUs=@($gpu | ForEach-Object {[string]$_.Name}); Disks=@(Get-CimInstance Win32_LogicalDisk -Filter 'DriveType=3' | ForEach-Object {[ordered]@{DeviceID=[string]$_.DeviceID; FreeSpace=[uint64]$_.FreeSpace}})} | ConvertTo-Json -Compress -Depth 4`
	out, err := command(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	if err != nil {
		h.Warnings = append(h.Warnings, "Не удалось получить сведения о железе через PowerShell; проверьте доступ к CIM.")
		return
	}
	if err := applyWindowsHardwareJSON(out, h); err != nil {
		h.Warnings = append(h.Warnings, "PowerShell вернул некорректный JSON со сведениями о железе.")
	}
}
