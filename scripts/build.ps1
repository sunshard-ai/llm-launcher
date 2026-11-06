param(
    [ValidatePattern('^[0-9]+\.[0-9]+\.[0-9]+([-+][A-Za-z0-9.-]+)?$')]
    [string]$Version = '0.3.2',
    [string]$Go = 'go'
)

$ErrorActionPreference = 'Stop'
$ProjectDir = Split-Path -Parent $PSScriptRoot
$OriginalLocation = Get-Location
$PreviousGoOS = $env:GOOS
$PreviousGoArch = $env:GOARCH
$PreviousCGO = $env:CGO_ENABLED

try {
    Set-Location -LiteralPath $ProjectDir
    & $Go version
    if ($LASTEXITCODE -ne 0) { throw 'Go SDK 1.22 or newer is required.' }
    $NativeOS = & $Go env GOHOSTOS
    if ($LASTEXITCODE -ne 0) { throw 'Cannot determine native Go OS.' }
    $NativeArch = & $Go env GOHOSTARCH
    if ($LASTEXITCODE -ne 0) { throw 'Cannot determine native Go architecture.' }
    $env:CGO_ENABLED = '0'
    $env:GOOS = $NativeOS.Trim()
    $env:GOARCH = $NativeArch.Trim()
    & $Go test ./...
    if ($LASTEXITCODE -ne 0) { throw 'Tests failed.' }
    & $Go vet ./...
    if ($LASTEXITCODE -ne 0) { throw 'go vet failed.' }
    New-Item -ItemType Directory -Path dist -Force | Out-Null

    $Targets = @(
        @{ OS = 'linux'; Arch = 'amd64'; Suffix = '' },
        @{ OS = 'linux'; Arch = 'arm64'; Suffix = '' },
        @{ OS = 'windows'; Arch = 'amd64'; Suffix = '.exe' },
        @{ OS = 'darwin'; Arch = 'arm64'; Suffix = '' }
    )
    $Checksums = @()
    foreach ($Target in $Targets) {
        $env:CGO_ENABLED = '0'
        $env:GOOS = $Target.OS
        $env:GOARCH = $Target.Arch
        $Filename = "llm-launcher-$($Target.OS)-$($Target.Arch)$($Target.Suffix)"
        $OutputPath = Join-Path dist $Filename
        Write-Host "Building $OutputPath"
        & $Go build -trimpath -buildvcs=false -ldflags "-s -w -X main.version=$Version" -o $OutputPath ./cmd/llm-launcher
        if ($LASTEXITCODE -ne 0) { throw "Build failed: $Filename" }
        $Digest = (Get-FileHash -LiteralPath $OutputPath -Algorithm SHA256).Hash.ToLowerInvariant()
        $Checksums += "$Digest  $Filename"
    }
    $env:GOOS = 'windows'
    $env:GOARCH = 'amd64'
    $DesktopFilename = 'LLM-Launcher-Windows-x64.exe'
    $DesktopPath = Join-Path dist $DesktopFilename
    Write-Host "Building $DesktopPath"
    & $Go build -trimpath -buildvcs=false -ldflags "-s -w -H=windowsgui -X main.version=$Version -X main.desktopMode=1" -o $DesktopPath ./cmd/llm-launcher
    if ($LASTEXITCODE -ne 0) { throw "Build failed: $DesktopFilename" }
    $DesktopDigest = (Get-FileHash -LiteralPath $DesktopPath -Algorithm SHA256).Hash.ToLowerInvariant()
    $Checksums += "$DesktopDigest  $DesktopFilename"
    $ChecksumPath = Join-Path $ProjectDir 'dist/SHA256SUMS.txt'
    [System.IO.File]::WriteAllLines($ChecksumPath, $Checksums, [System.Text.Encoding]::ASCII)
} finally {
    $env:GOOS = $PreviousGoOS
    $env:GOARCH = $PreviousGoArch
    $env:CGO_ENABLED = $PreviousCGO
    Set-Location -LiteralPath $OriginalLocation.Path
}
