$ErrorActionPreference = 'Stop'
$script:launcherExecutable = [IO.Path]::GetFullPath($env:LLM_LAUNCHER_DESKTOP_EXE)
Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing
[Windows.Forms.Application]::EnableVisualStyles()
[Windows.Forms.Application]::SetCompatibleTextRenderingDefault($false)

$desktopStrings = ConvertFrom-Json $desktopStringsJson
$diagnosticKeys = [Collections.Generic.Dictionary[string,string]]::new([StringComparer]::Ordinal)
$diagnosticPatterns = @(foreach ($message in $desktopStrings.PSObject.Properties) {
    if ($message.Value.sourcePattern) {
        @{ Key = $message.Name; Pattern = [regex]::new('\A' + $message.Value.sourcePattern + '\z') }
    } elseif ($message.Value.source) {
        $diagnosticKeys.Add($message.Value.source, $message.Name)
    }
})
$languageIDs = @('en', 'de', 'ru', 'zh', 'fr')
$appDir = Join-Path ([Environment]::GetFolderPath('LocalApplicationData')) 'LLM Launcher'
$languagePreference = Join-Path $appDir 'language.txt'
$script:language = 'en'
try {
    if ([IO.File]::Exists($languagePreference)) {
        $savedLanguage = [IO.File]::ReadAllText($languagePreference).Trim()
        if ($savedLanguage -cin $languageIDs) { $script:language = $savedLanguage }
    }
} catch { }

function Get-Text([string]$key) {
    $entry = $desktopStrings.PSObject.Properties[$key]
    if (!$entry) { return $key }
    $text = $entry.Value.($script:language)
    if ($args.Count) {
        $culture = [Globalization.CultureInfo]::GetCultureInfo($script:language)
        return [string]::Format($culture, $text, [object[]]$args)
    }
    $text
}

function Set-TextKey($control, [string]$key) {
    $control.Tag = $key
    $control.Text = Get-Text $key
}

$form = New-Object Windows.Forms.Form
$form.Text = 'LLM Launcher'
try { $form.Icon = [Drawing.Icon]::ExtractAssociatedIcon($env:LLM_LAUNCHER_DESKTOP_EXE) }
catch { $form.Icon = [Drawing.SystemIcons]::Application }
$form.StartPosition = 'CenterScreen'
$form.ClientSize = [Drawing.Size]::new(950, 720)
$form.MinimumSize = [Drawing.Size]::new(900, 665)
$form.Font = [Drawing.Font]::new('Segoe UI', 10)
$form.AutoScaleMode = 'Dpi'
$layout = [Windows.Forms.TableLayoutPanel]::new()
$layout.Dock = 'Fill'
$layout.Padding = [Windows.Forms.Padding]::new(12)
$layout.ColumnCount = 1
$layout.RowCount = 6
foreach ($height in @(48, 46, 320, 25)) {
    [void]$layout.RowStyles.Add([Windows.Forms.RowStyle]::new('Absolute', $height))
}
[void]$layout.RowStyles.Add([Windows.Forms.RowStyle]::new('Percent', 100))
[void]$layout.RowStyles.Add([Windows.Forms.RowStyle]::new('Absolute', 39))
$form.Controls.Add($layout)

function New-Row {
    $row = [Windows.Forms.FlowLayoutPanel]::new()
    $row.Dock = 'Fill'
    $row.WrapContents = $false
    $row
}

function Add-Label($parent, [string]$key, [int]$maxWidth = 0) {
    $label = [Windows.Forms.Label]::new()
    Set-TextKey $label $key
    $label.AutoSize = $true
    if ($maxWidth) { $label.MaximumSize = [Drawing.Size]::new($maxWidth, 0) }
    $label.Margin = [Windows.Forms.Padding]::new(4, 7, 4, 0)
    $parent.Controls.Add($label)
    $label
}

function Add-Button($parent, [string]$key, [scriptblock]$action) {
    $button = [Windows.Forms.Button]::new()
    $button.AutoSize = $true
    $button.Height = 31
    Set-TextKey $button $key
    $button.Add_Click($action)
    $parent.Controls.Add($button)
    $button
}

function Add-Combo($parent, [int]$width, [object[]]$items = @()) {
    $combo = [Windows.Forms.ComboBox]::new()
    $combo.Width = $width
    $combo.DropDownStyle = 'DropDownList'
    $combo.DropDownWidth = [Math]::Max($width, 300)
    if ($items.Length) { $combo.Items.AddRange($items); $combo.SelectedIndex = 0 }
    $parent.Controls.Add($combo)
    $combo
}

function Add-Number($parent, [int]$value, [int]$min, [int]$max) {
    $field = [Windows.Forms.NumericUpDown]::new()
    $field.Width = 80
    $field.Minimum = $min
    $field.Maximum = $max
    $field.Value = $value
    $parent.Controls.Add($field)
    $field
}

$header = [Windows.Forms.TableLayoutPanel]::new()
$header.Dock = 'Fill'
$header.BackColor = [Drawing.Color]::FromArgb(14, 23, 43)
$header.ColumnCount = 2
$header.RowCount = 1
[void]$header.ColumnStyles.Add([Windows.Forms.ColumnStyle]::new('Percent', 100))
[void]$header.ColumnStyles.Add([Windows.Forms.ColumnStyle]::new('AutoSize'))
$title = Add-Label $header 'title'
$title.ForeColor = [Drawing.Color]::White
$title.Font = [Drawing.Font]::new('Segoe UI Semibold', 17)
$languageRow = New-Row
$languageRow.AutoSize = $true
$languageRow.Padding = [Windows.Forms.Padding]::new(0, 3, 6, 0)
$header.Controls.Add($languageRow, 1, 0)
$languageLabel = Add-Label $languageRow 'language'
$languageLabel.ForeColor = [Drawing.Color]::White
$languagePicker = Add-Combo $languageRow 118 @('English', 'Deutsch', 'Русский', '简体中文', 'Français')
$languagePicker.DropDownWidth = $languagePicker.Width
$languagePicker.SelectedIndex = [Array]::IndexOf($languageIDs, $script:language)
$languagePicker.AccessibleName = $languageLabel.Text
$layout.Controls.Add($header, 0, 0)

$storage = New-Row
[void](Add-Label $storage 'projectsFolder')
$root = [Windows.Forms.TextBox]::new()
$root.ReadOnly = $true
$root.Width = 550
$rootPreference = Join-Path $appDir 'projects-path.txt'
$root.Text = Join-Path $appDir 'deployments'
try {
    if ([IO.File]::Exists($rootPreference)) {
        $savedRoot = [IO.File]::ReadAllText($rootPreference)
        if ([IO.Directory]::Exists($savedRoot)) { $root.Text = $savedRoot }
    }
} catch { }
$storage.Controls.Add($root)
[void](Add-Button $storage 'browse' {
    $picker = [Windows.Forms.FolderBrowserDialog]::new()
    $picker.Description = Get-Text 'browsePrompt'
    if ([IO.Directory]::Exists($root.Text)) { $picker.SelectedPath = $root.Text }
    try {
        if ($picker.ShowDialog($form) -eq 'OK') {
            $root.Text = $picker.SelectedPath
            try {
                [void][IO.Directory]::CreateDirectory($appDir)
                [IO.File]::WriteAllText($rootPreference, $root.Text)
            } catch { Write-Journal 'folderSaveError' }
            Reset-Prepared
            $models.Items.Clear()
            $savePlan.Enabled = [bool](Hub-Selected)
            Read-Projects
        }
    } finally { $picker.Dispose() }
})
$layout.Controls.Add($storage, 0, 1)

$tabs = [Windows.Forms.TabControl]::new()
$tabs.Dock = 'Fill'
$setup = [Windows.Forms.TabPage]::new()
$manage = [Windows.Forms.TabPage]::new()
Set-TextKey $setup 'setupTab'
Set-TextKey $manage 'manageTab'
$tabs.TabPages.Add($setup)
$tabs.TabPages.Add($manage)
$layout.Controls.Add($tabs, 0, 2)
$setupLayout = [Windows.Forms.TableLayoutPanel]::new()
$setupLayout.Dock = 'Fill'
$setupLayout.Padding = [Windows.Forms.Padding]::new(6)
$setupLayout.ColumnCount = 1
$setupLayout.RowCount = 7
foreach ($height in @(38, 38, 37, 37, 38, 38, 39)) {
    [void]$setupLayout.RowStyles.Add([Windows.Forms.RowStyle]::new('Absolute', $height))
}
$setup.Controls.Add($setupLayout)
$rows = @(for ($i = 0; $i -lt 7; $i++) {
    $row = New-Row
    $setupLayout.Controls.Add($row, 0, $i)
    $row
})
$catalogLabel = Add-Label $rows[0] 'catalogLoading'
$catalogLabel.AutoSize = $false
$catalogLabel.Width = 530
[void](Add-Button $rows[0] 'updateCatalog' { Update-ModelCatalog })
$rows = @($rows[1..6])
[void](Add-Button $rows[0] 'scan' { Read-Hardware })
$hardware = Add-Label $rows[0] 'hardware'
[void](Add-Label $rows[1] 'task')
$taskLabels = @('taskChat', 'taskCode', 'taskRag', 'taskEmbeddings')
$tasks = Add-Combo $rows[1] 255 @($taskLabels | ForEach-Object { Get-Text $_ })
$taskIDs = @('chat', 'code', 'rag', 'embeddings')
[void](Add-Label $rows[1] 'engine')
$engineLabels = @('engineAuto', 'Ollama', 'engineVllm', 'llama.cpp / GGUF')
$engines = Add-Combo $rows[1] 245 @($engineLabels | ForEach-Object { Get-Text $_ })
$engineIDs = @('', 'ollama', 'vllm', 'llama.cpp')
$imageButton = Add-Button $rows[1] 'imageButton' { Edit-VLLMImage }
$imageButton.Visible = $false
[void](Add-Label $rows[2] 'acceleration')
$devices = Add-Combo $rows[2] 258 @((Get-Text 'auto'), 'CPU')
[void](Add-Label $rows[2] 'context')
$context = Add-Number $rows[2] 4096 512 32768
[void](Add-Label $rows[2] 'port')
$port = Add-Number $rows[2] 0 0 65535
$pickModel = Add-Button $rows[3] 'findModels' {
    Read-Recommendations
}
$models = Add-Combo $rows[3] 390
$importVLLM = Add-Button $rows[3] 'importHf' { Edit-HubSource }
$importVLLM.Visible = $false
[void](Add-Button $rows[3] 'localModel' { Edit-LocalSource })
[void](Add-Label $rows[4] 'projectName')
$name = [Windows.Forms.TextBox]::new()
$name.Width = 275
$name.Text = 'model-' + [DateTime]::Now.ToString('yyyyMMdd-HHmmss')
$rows[4].Controls.Add($name)
$advancedButton = Add-Button $rows[4] 'advanced' { Edit-Advanced }
$savePlan = Add-Button $rows[5] 'preparePlan' { Save-ReviewedPlan }
$startPrepared = Add-Button $rows[5] 'launchPlan' {
    if ($script:preparedPath) { Run-Project 'up' $script:preparedPath }
}
$detailsButton = Add-Button $rows[5] 'viewDetails' {
    if ($script:preparedPath) { Show-Compose $script:preparedPath }
}
$savePlan.Enabled = $false
$startPrepared.Enabled = $false
$detailsButton.Enabled = $false

$manageLayout = [Windows.Forms.TableLayoutPanel]::new()
$manageLayout.Dock = 'Fill'
$manageLayout.Padding = [Windows.Forms.Padding]::new(6)
$manageLayout.ColumnCount = 1
$manageLayout.RowCount = 3
[void]$manageLayout.RowStyles.Add([Windows.Forms.RowStyle]::new('Absolute', 40))
[void]$manageLayout.RowStyles.Add([Windows.Forms.RowStyle]::new('Percent', 100))
[void]$manageLayout.RowStyles.Add([Windows.Forms.RowStyle]::new('Absolute', 40))
$manage.Controls.Add($manageLayout)
$manageTop = New-Row
$manageLayout.Controls.Add($manageTop, 0, 0)
[void](Add-Button $manageTop 'refreshProjects' { Read-Projects })
[void](Add-Label $manageTop 'manageHint')
$projectList = [Windows.Forms.ListBox]::new()
$projectList.Dock = 'Fill'
$projectList.HorizontalScrollbar = $true
$manageLayout.Controls.Add($projectList, 0, 1)
$actions = New-Row
$actions.AutoScroll = $true
$manageLayout.Controls.Add($actions, 0, 2)
[void](Add-Button $actions 'launch' { Invoke-Selected 'up' })
[void](Add-Button $actions 'status' { Invoke-Selected 'status' })
[void](Add-Button $actions 'logs' { Invoke-Selected 'logs' })
[void](Add-Button $actions 'testApi' { Invoke-Selected 'test' })
[void](Add-Button $actions 'stop' { Invoke-Selected 'down' })
[void](Add-Button $actions 'archive' { Invoke-Selected 'delete' })
[void](Add-Button $actions 'restore' { Invoke-Selected 'restore' })
[void](Add-Button $actions 'details' { Invoke-Selected 'inspect' })
[void](Add-Button $actions 'importWeights' { Invoke-Selected 'import-weights' })

$log = [Windows.Forms.TextBox]::new()
$log.Dock = 'Fill'
$log.Multiline = $true
$log.ReadOnly = $true
$log.ScrollBars = 'Both'
$log.WordWrap = $false
$log.Font = [Drawing.Font]::new('Consolas', 10)
[void](Add-Label $layout 'journal')
$layout.Controls.Add($log, 0, 4)
$footer = New-Row
$layout.Controls.Add($footer, 0, 5)
$cancel = Add-Button $footer 'cancelCommand' {
    if ($script:job -and !$script:job.Process.HasExited) {
        try { $script:job.Process.StandardInput.Close() }
        catch { Write-Diagnostic $_.Exception.Message }
    }
    $cancel.Enabled = $false
}
$cancel.Enabled = $false
$technicalButton = Add-Button $footer 'technicalDetails' { Show-TechnicalOutput }
$technicalButton.Enabled = $false
$progress = Add-Label $footer 'idle'
$script:journal = [Collections.Generic.List[hashtable]]::new()
$script:technicalOutput = [Text.StringBuilder]::new()
$script:job = $null
$script:preparedPath = $null
$script:choices = @()
$script:projects = @()
$script:hardwareInfo = $null
$script:catalogInfo = $null
$script:gpus = @()
$script:changingLanguage = $false
$script:advanced = @{}
$script:hfURL = ''
$script:hfEngine = ''
$script:hfToken = ''
$script:vllmImage = ''
$script:localWeights = ''

function Show-Message([string]$text) {
    [void][Windows.Forms.MessageBox]::Show($form, $text, 'LLM Launcher', 'OK', 'Information')
}

function Format-JournalEntry($entry) {
    $values = @(foreach ($value in $entry.Args) {
        if ($value -is [hashtable]) { Format-JournalEntry $value } else { $value }
    })
    Get-Text $entry.Key @values
}

function Update-Journal {
    $log.Text = (@(foreach ($entry in $script:journal) {
        (Format-JournalEntry $entry) + [Environment]::NewLine
    }) -join '')
    $log.SelectionStart = $log.TextLength
    $log.ScrollToCaret()
}

function Clear-Journal {
    $script:journal.Clear()
    [void]$script:technicalOutput.Clear()
    $technicalButton.Enabled = $false
    $log.Clear()
}

function Write-Journal([string]$key) {
    $entry = @{ Key = $key; Args = @($args) }
    $script:journal.Add($entry)
    if ($script:journal.Count -gt 1000) {
        $script:journal.RemoveRange(0, 250)
        Update-Journal
    } else {
        $log.AppendText((Format-JournalEntry $entry) + [Environment]::NewLine)
    }
}

function Find-Diagnostic([string]$line, [switch]$reasonOnly) {
    $key = ''
    if ($diagnosticKeys.TryGetValue($line, [ref]$key)) { return @{ Key = $key; Args = @() } }
    foreach ($diagnostic in $diagnosticPatterns) {
        $match = $diagnostic.Pattern.Match($line)
        if (!$match.Success) { continue }
        $values = @($match.Groups | Select-Object -Skip 1 | ForEach-Object { $_.Value })
        if ($diagnostic.Key -eq 'gpuRejected') {
            if ($reasonOnly) { return }
            $reason = Find-Diagnostic $values[2] -ReasonOnly
            if (!$reason) { return }
            $values[2] = $reason
        }
        return @{ Key = $diagnostic.Key; Args = $values }
    }
}

function Write-TechnicalOutput([string]$line) {
    $line = [regex]::Replace($line, '\x1B\[[0-?]*[ -/]*[@-~]', '')
    if (!$line) { return }
    if (!$technicalButton.Enabled) {
        $technicalButton.Enabled = $true
        Write-Journal 'technicalOutputHint'
    }
    [void]$script:technicalOutput.AppendLine($line)
    if ($script:technicalOutput.Length -gt 100000) {
        [void]$script:technicalOutput.Remove(0, $script:technicalOutput.Length - 75000)
    }
}

function Write-Diagnostic([string]$line) {
    $line = [regex]::Replace($line, '\x1B\[[0-?]*[ -/]*[@-~]', '')
    $line = $line -replace '^Ошибка: ', ''
    if (!$line.Trim()) { return }
    $entry = Find-Diagnostic $line
    if ($entry) {
        $values = $entry.Args
        Write-Journal $entry.Key @values
    } else {
        Write-TechnicalOutput $line
    }
}

function Quote-Arg([string]$value) {
    # Quote according to the Windows argv rules. No shell executes these arguments.
    $escaped = [regex]::Replace($value, '(\\*)"', '$1$1\"')
    '"' + [regex]::Replace($escaped, '(\\+)$', '$1$1') + '"'
}

function Start-Launcher([string[]]$arguments, [scriptblock]$onDone = {}, [string]$data = '', [switch]$capture, [string]$hubToken = '') {
    if ($script:job) { return }
    $process = $null
    try {
        if (![IO.File]::Exists($script:launcherExecutable)) { throw (Get-Text 'workerMissing') }
        $info = [Diagnostics.ProcessStartInfo]::new($script:launcherExecutable)
        $info.WorkingDirectory = [IO.Path]::GetDirectoryName($script:launcherExecutable)
        $info.Arguments = ((@('--desktop-child') + $arguments) | ForEach-Object { Quote-Arg $_ }) -join ' '
        $info.UseShellExecute = $false
        $info.CreateNoWindow = $true
        $info.RedirectStandardInput = $true
        $info.RedirectStandardOutput = $true
        $info.RedirectStandardError = $true
        $info.StandardOutputEncoding = [Text.Encoding]::UTF8
        $info.StandardErrorEncoding = [Text.Encoding]::UTF8
        if ($hubToken) { $info.EnvironmentVariables['LLM_LAUNCHER_HF_TOKEN'] = $hubToken }
        $process = [Diagnostics.Process]::Start($info)
        $script:job = @{
            Process = $process
            StdOut = $process.StandardOutput.ReadLineAsync()
            StdErr = $process.StandardError.ReadLineAsync()
            Output = [Text.StringBuilder]::new()
            OnDone = $onDone
            Capture = $capture.IsPresent
            Data = $data
            Command = $arguments[0]
        }
        $storage.Enabled = $false
        $tabs.Enabled = $false
        $cancel.Enabled = $true
        $progress.Tag = 'running'
        $progress.Text = Get-Text 'running' $script:job.Command
        $timer.Start()
    } catch {
        $failure = $_.Exception
        if ($process) {
            try { $process.StandardInput.Close() } catch { }
            $process.Dispose()
        }
        $script:job = $null
        $timer.Stop()
        $storage.Enabled = $true
        $tabs.Enabled = $true
        $cancel.Enabled = $false
        Set-TextKey $progress 'failed'
        Write-Journal 'commandStartFailed'
        Write-TechnicalOutput $failure.ToString()
    }
}

$timer = [Windows.Forms.Timer]::new()
$timer.Interval = 100
$timer.Add_Tick({
    $job = $script:job
    if (!$job) { $timer.Stop(); return }
    foreach ($key in @('StdOut', 'StdErr')) {
        for ($i = 0; $i -lt 100 -and $job[$key] -and $job[$key].IsCompleted; $i++) {
            $task = $job[$key]
            $job[$key] = $null
            if ($task.IsFaulted) { Write-Diagnostic $task.Exception.Message; break }
            $line = $task.Result
            if ($null -eq $line) { break }
            if ($key -eq 'StdOut') {
                if ($job.Capture) { [void]$job.Output.AppendLine($line) }
                elseif ($job.Command -eq 'logs') { Write-TechnicalOutput $line }
                else { Write-Diagnostic $line }
                $job[$key] = $job.Process.StandardOutput.ReadLineAsync()
            } else {
                Write-Diagnostic $line
                $job[$key] = $job.Process.StandardError.ReadLineAsync()
            }
        }
    }
    if ($job.StdOut -or $job.StdErr -or !$job.Process.HasExited) { return }
    $timer.Stop()
    $exitCode = $job.Process.ExitCode
    $job.Process.Dispose()
    $script:job = $null
    $storage.Enabled = $true
    $tabs.Enabled = $true
    $cancel.Enabled = $false
    if ($exitCode -eq 0) { Set-TextKey $progress 'completed' }
    else {
        Set-TextKey $progress 'failed'
        Write-Journal 'failed'
    }
    try { & $job.OnDone $exitCode $job.Output.ToString() $job.Data }
    catch { Write-Diagnostic $_.Exception.Message }
})

function Set-ComboItems($combo, [object[]]$items, [int]$index) {
    $combo.BeginUpdate()
    try {
        $combo.Items.Clear()
        $combo.Items.AddRange($items)
        $combo.SelectedIndex = $index
    } finally { $combo.EndUpdate() }
}

function Update-HardwareLabels([int]$deviceIndex) {
    $hw = $script:hardwareInfo
    if ($hw) {
        $hardware.Text = Get-Text 'hardwareSummary' $hw.cpu_name ([Math]::Round($hw.ram_total_bytes / 1GB))
    }
    $items = @((Get-Text 'auto'), 'CPU') + @(foreach ($gpu in $script:gpus) {
        Get-Text 'gpuSummary' $gpu.name ([Math]::Round($gpu.vram_free_bytes / 1GB, 1))
    })
    Set-ComboItems $devices $items $deviceIndex
}

function Availability([bool]$value) {
    if ($value) { @{ Key = 'available' } } else { @{ Key = 'unavailable' } }
}

function Read-Hardware {
    Clear-Journal
    Start-Launcher @('scan', '--json', '--dir', $root.Text) {
        param($code, $json)
        if ($code -ne 0) { return }
        $hw = ConvertFrom-Json $json
        $script:hardwareInfo = $hw
        $script:gpus = @($hw.gpus | Where-Object { $_.vendor -eq 'nvidia' })
        Update-HardwareLabels 0
        Write-Journal 'dockerSummary' (Availability $hw.docker.reachable) (Availability $hw.docker.compose) ([Math]::Round($hw.docker.memory_bytes / 1GB, 1))
        foreach ($note in $hw.warnings) { Write-Diagnostic $note }
        if (!$hw.docker.reachable -and !$hw.docker.error) { Write-Journal 'dockerStopped' }
        Read-Catalog
    } -Capture
}

function Update-CatalogLabel {
    if ($script:catalogInfo) {
        $date = $script:catalogInfo.updated -replace 'T.*$', ''
        $catalogLabel.Text = Get-Text 'catalogSummary' (@($script:catalogInfo.models).Count) $date
    }
}

function Read-Catalog([switch]$refreshModels) {
    Start-Launcher @('catalog', '--json') {
        param($code, $json, $refresh)
        if ($code -ne 0) { return }
        $script:catalogInfo = ConvertFrom-Json $json
        Update-CatalogLabel
        if ($refresh -eq 'yes' -and !(Hub-Selected)) { Read-Recommendations }
    } -Capture -Data $(if ($refreshModels) { 'yes' } else { 'no' })
}

function Update-ModelCatalog {
    Start-Launcher @('update') {
        param($code, $output)
        if ($code -eq 0) { Read-Catalog -RefreshModels }
    }
}

function Fit-Args {
    '--dir', $root.Text, '--task', $taskIDs[$tasks.SelectedIndex], '--context', [string]$context.Value
    $engine = Selected-Engine
    if ($engine) { '--runtime'; $engine }
    if ($engine -eq 'vllm' -and $script:vllmImage) { '--image'; $script:vllmImage }
    if ($devices.SelectedIndex -eq 1) { '--cpu' }
    if ($devices.SelectedIndex -ge 2) {
        $gpu = $script:gpus[$devices.SelectedIndex - 2]
        '--gpu'
        if ($gpu.uuid) { $gpu.uuid } else { $gpu.index }
    }
}

function Read-Recommendations {
    Reset-Prepared
    $script:hfEngine = ''
    $script:localWeights = ''
    Clear-Journal
    $models.Items.Clear()
    $savePlan.Enabled = $false
    Start-Launcher (@('recommend', '--json') + @(Fit-Args)) {
        param($code, $json)
        if (!$json) { return }
        $result = ConvertFrom-Json $json
        $script:choices = @($result.recommendations | Where-Object { $_ })
        foreach ($r in $script:choices) {
            [void]$models.Items.Add("$($r.model.name)   |   $($r.model.runtime) / $($r.acceleration)   |   $($r.model.license)")
        }
        if ($script:choices.Count) { $models.SelectedIndex = 0; $savePlan.Enabled = $true }
        else { Write-Journal 'noModels' }
        foreach ($note in $result.notes) { Write-Diagnostic $note }
    } -Capture
}

$models.Add_SelectedIndexChanged({
    Reset-Prepared
    if ($models.SelectedIndex -lt 0) { return }
    $script:hfEngine = ''
    Clear-Journal
    $r = $script:choices[$models.SelectedIndex]
    Write-Journal 'modelSource' $r.model.ref $r.model.url
    foreach ($note in $r.notes) { Write-Diagnostic $note }
})
function Selected-Engine { $engineIDs[$engines.SelectedIndex] }

function Hub-Selected { $script:hfURL -and $script:hfEngine -eq (Selected-Engine) }

function Profile-Key {
    $engine = Selected-Engine
    $model = ''
    if ($models.SelectedIndex -ge 0) {
        $selected = $script:choices[$models.SelectedIndex].model
        $engine = $selected.runtime
        $model = $selected.id
    } elseif (Hub-Selected) {
        $model = $script:hfURL
    }
    if ($engine -and $model) { "$engine/$($taskIDs[$tasks.SelectedIndex])/$model" }
}

function Reset-Prepared {
    $script:preparedPath = $null
    $startPrepared.Enabled = $false
    $detailsButton.Enabled = $false
}

$invalidateChoices = {
    if ($script:changingLanguage) { return }
    Reset-Prepared
    $models.Items.Clear()
    $engine = Selected-Engine
    $importVLLM.Visible = $engine -eq 'vllm' -or $engine -eq 'llama.cpp'
    $imageButton.Visible = $engine -eq 'vllm'
    $savePlan.Enabled = [bool](Hub-Selected)
}
$engines.Add_SelectedIndexChanged($invalidateChoices)
$tasks.Add_SelectedIndexChanged($invalidateChoices)
$devices.Add_SelectedIndexChanged($invalidateChoices)
$context.Add_ValueChanged($invalidateChoices)
$name.Add_TextChanged({ Reset-Prepared })
$port.Add_ValueChanged({ Reset-Prepared })

function Save-ReviewedPlan {
    $hub = Hub-Selected
    if ($hub -and (Selected-Engine) -eq 'vllm' -and $devices.SelectedIndex -eq 1) {
        Show-Message (Get-Text 'vllmNeedsGpu')
        return
    }
    if (!$hub -and $models.SelectedIndex -lt 0) { return }
    $projectName = $name.Text.Trim()
    if ($projectName -notmatch '^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$' -or $projectName -match '^(CON|PRN|AUX|NUL|COM[0-9]|LPT[0-9])$') {
        Show-Message (Get-Text 'invalidFolderName')
        return
    }
    Reset-Prepared
    $dir = New-ProjectPath $root.Text $projectName
    $name.Text = [IO.Path]::GetFileName($dir)
    $fit = @(Fit-Args)
    $fit[1] = $dir
    if ($hub -and (Selected-Engine) -eq 'llama.cpp') {
        $fit = @('--dir', $dir, '--task', $taskIDs[$tasks.SelectedIndex], '--context', [string]$context.Value)
    }
    $secret = ''
    if ($hub) {
        $arguments = @('hf-plan', '--hf-url', $script:hfURL)
        $secret = $script:hfToken
    } else {
        $id = $script:choices[$models.SelectedIndex].model.id
        $arguments = @('plan', '--model', $id)
    }
    $arguments += @('--json', '--port', [string]$port.Value) + $fit
    $key = Profile-Key
    if ($key -and $script:advanced.ContainsKey($key)) {
        $arguments += @('--settings-json', (ConvertTo-Json -InputObject $script:advanced[$key] -Depth 4 -Compress))
    }
    Clear-Journal
    Start-Launcher $arguments {
        param($code, $output, $dir)
        if ($code -ne 0) { return }
        $p = ConvertFrom-Json $output
        $script:preparedPath = $dir
        $startPrepared.Enabled = $true
        $detailsButton.Enabled = $true
        Write-Journal 'planSummary' $p.model.name $p.runtime $p.acceleration $p.context
        Write-Journal 'planFolder' $dir
        Write-Journal 'planApi' "http://127.0.0.1:$($p.port)/v1"
        if ($p.runtime -eq 'vllm') { Write-Journal 'planImage' $p.image }
        if ($p.hf_source) {
            Write-Journal 'hfRevision' $p.hf_source.repo $p.hf_source.revision
            Write-Journal 'planDownload' ([Math]::Round($p.hf_source.bytes / 1GB, 2))
        }
        if ($p.advanced) { Write-Journal 'planSettings' }
        if ($p.hf_source) { Write-Journal 'weightsLocation' (Join-Path $dir 'hf-model') }
        else { Write-Journal 'weightsVolume' }
        Write-Journal 'planReady'
        if ((Hub-Selected) -and $script:localWeights) {
            $startPrepared.Enabled = $false
            Import-LocalWeights $dir $script:localWeights
        }
    } -Data $dir -HubToken $secret -Capture
}

function New-ProjectPath([string]$parent, [string]$projectName) {
    $candidate = Join-Path $parent $projectName
    $baseName = $projectName.Substring(0, [Math]::Min(56, $projectName.Length))
    $index = 2
    while ([IO.Directory]::Exists($candidate) -or [IO.File]::Exists($candidate)) {
        $candidate = Join-Path $parent ($baseName + '-' + $index)
        $index++
    }
    $candidate
}

function Import-LocalWeights([string]$dir, [string]$local = '') {
    if (!$local) {
        Start-Launcher @('inspect', '--json', '--dir', $dir) {
            param($code, $json, $dir)
            if ($code -ne 0) { return }
            $p = ConvertFrom-Json $json
            if (!$p.hf_source) { Show-Message (Get-Text 'localWeightsHelp'); return }
            if ($p.hf_source.files) {
                $picker = [Windows.Forms.FolderBrowserDialog]::new()
                $picker.Description = Get-Text 'snapshotFolder'
            } else {
                $picker = [Windows.Forms.OpenFileDialog]::new()
                $picker.Filter = 'GGUF (*.gguf)|*.gguf'
                $picker.RestoreDirectory = $true
            }
            $selected = ''
            try {
                if ($picker.ShowDialog($form) -eq 'OK') {
                    if ($p.hf_source.files) { $selected = $picker.SelectedPath }
                    else { $selected = $picker.FileName }
                }
            } finally { $picker.Dispose() }
            if ($selected) { Import-LocalWeights $dir $selected }
        } -Capture -Data $dir
        return
    }
    Write-Journal 'verifyingWeights'
    Start-Launcher @('import-weights', '--dir', $dir, '--from', $local) {
        param($code, $output, $dir)
        if ($code -eq 0) {
            Write-Journal 'weightsReady'
            if ($script:preparedPath -eq $dir) { $startPrepared.Enabled = $true }
        } else { Write-Journal 'weightsMismatch' }
    } -Data $dir
}

function Show-Compose([string]$dir) {
    Start-Launcher @('inspect', '--compose', '--dir', $dir) {
        param($code, $output)
        if ($code -ne 0) { return }
        Show-TextDialog (Get-Text 'composeTitle') $output
    } -Capture
}

function Show-TechnicalOutput {
    Show-TextDialog (Get-Text 'technicalDetails') $script:technicalOutput.ToString()
}

function Show-TextDialog([string]$caption, [string]$output) {
    $dialog = New-Dialog $caption 820 560
    $text = [Windows.Forms.TextBox]::new()
    $text.Multiline = $true
    $text.ReadOnly = $true
    $text.WordWrap = $false
    $text.ScrollBars = 'Both'
    $text.Dock = 'Fill'
    $text.Font = [Drawing.Font]::new('Consolas', 10)
    $text.Text = $output
    $dialog.Controls.Add($text)
    try { [void]$dialog.ShowDialog($form) } finally { $dialog.Dispose() }
}

function Update-ProjectList([string]$selectedDir) {
    $projectList.BeginUpdate()
    try {
        $projectList.Items.Clear()
        foreach ($p in $script:projects) {
            $stateKey = "state_$($p.state)"
            $state = $p.state
            if ($desktopStrings.PSObject.Properties[$stateKey]) { $state = Get-Text $stateKey }
            $index = $projectList.Items.Add("[$state]  $($p.model)   |   $($p.dir)")
            if ($p.dir -eq $selectedDir) { $projectList.SelectedIndex = $index }
        }
    } finally { $projectList.EndUpdate() }
}

function Read-Projects {
    Start-Launcher @('list', '--json', '--dir', $root.Text) {
        param($code, $json)
        if ($code -ne 0) { return }
        $old = $null
        if ($projectList.SelectedIndex -ge 0) { $old = $script:projects[$projectList.SelectedIndex].dir }
        $script:projects = @(foreach ($project in (ConvertFrom-Json $json)) {
            if ($null -ne $project) { $project }
        })
        Update-ProjectList $old
        foreach ($p in $script:projects) {
            if ($p.error) {
                Write-Journal 'invalidProject' $p.dir
                Write-Diagnostic $p.error
            }
        }
    } -Capture
}

function Run-Project([string]$command, [string]$dir) {
    Clear-Journal
    if ($command -eq 'import-weights') { Import-LocalWeights $dir; return }
    if ($command -in @('status', 'test')) {
        Start-Launcher @($command, '--json', '--dir', $dir) {
            param($code, $json, $command)
            if (!$json) { return }
            $result = ConvertFrom-Json $json
            if ($command -eq 'test' -and $result.kind) {
                $key = if ($result.ok) { 'testPassed' } else { 'testFailed' }
                Write-Journal $key $result.kind $result.duration_ms
                if ($result.output) { Write-TechnicalOutput $result.output }
            } elseif ($result.project) {
                Write-Journal 'statusSummary' $result.project $result.runtime $result.model
                Write-Journal 'apiReachable' $result.endpoint (Availability $result.reachable)
                $loaded = if ($result.model_loaded) { @{ Key = 'yes' } } else { @{ Key = 'no' } }
                Write-Journal 'modelLoaded' $loaded
                if ($result.containers) { Write-Diagnostic $result.containers }
                if ($result.error) { Write-Diagnostic $result.error }
            }
        } -Data $command -Capture
        return
    }
    $done = {}
    if ($command -in @('up', 'down', 'delete', 'restore')) {
        $done = { param($code, $output); if ($code -eq 0) { Read-Projects } }
    } elseif ($command -eq 'logs') {
        $done = { if ($technicalButton.Enabled) { Show-TechnicalOutput } }
    }
    $secret = ''
    if ($command -eq 'up') { $secret = $script:hfToken }
    Start-Launcher @($command, '--dir', $dir) $done -HubToken $secret
}

function Invoke-Selected([string]$command) {
    if ($projectList.SelectedIndex -lt 0) { Show-Message (Get-Text 'selectProject'); return }
    $p = $script:projects[$projectList.SelectedIndex]
    if ($p.state -eq 'invalid') {
        Write-Journal 'invalidProject' $p.dir
        Write-Diagnostic $p.error
        return
    }
    if ($command -eq 'inspect') { Show-Compose $p.dir; return }
    if ($command -eq 'delete' -and !(Confirm-Archive)) { return }
    Run-Project $command $p.dir
}

function New-Dialog([string]$caption, [int]$width, [int]$height) {
    $dialog = [Windows.Forms.Form]::new()
    $dialog.Text = $caption
    $dialog.ClientSize = [Drawing.Size]::new($width, $height)
    $dialog.StartPosition = 'CenterParent'
    $dialog.ShowInTaskbar = $false
    $dialog.Font = $form.Font
    $dialog
}

function Dialog-Text($parent, [string]$label, [string]$value = '', [int]$lines = 1) {
    [void](Add-Label $parent $label 610)
    $field = [Windows.Forms.TextBox]::new()
    $field.Width = 610
    $field.Text = $value
    if ($lines -gt 1) { $field.Multiline = $true; $field.Height = $lines * 23; $field.ScrollBars = 'Vertical' }
    $parent.Controls.Add($field)
    $field
}

function Dialog-Panel($dialog) {
    $panel = New-Row
    $panel.FlowDirection = 'TopDown'
    $panel.Padding = [Windows.Forms.Padding]::new(12)
    $panel.AutoScroll = $true
    $dialog.Controls.Add($panel)
    $panel
}

function Confirm-Archive {
    $dialog = New-Dialog (Get-Text 'archive') 550 150
    $panel = Dialog-Panel $dialog
    [void](Add-Label $panel 'archivePrompt' 490)
    $buttons = New-Row
    $buttons.AutoSize = $true
    $panel.Controls.Add($buttons)
    $cancelArchive = Add-Button $buttons 'cancel' {}
    $cancelArchive.DialogResult = 'Cancel'
    $dialog.CancelButton = $cancelArchive
    Add-DialogOK $dialog $buttons 'archive'
    $dialog.ActiveControl = $cancelArchive
    try { return $dialog.ShowDialog($form) -eq 'OK' }
    finally { $dialog.Dispose() }
}

function Add-DialogOK($dialog, $panel, [string]$text) {
    $ok = Add-Button $panel $text {}
    $ok.DialogResult = 'OK'
    $dialog.AcceptButton = $ok
}

function Edit-VLLMImage {
    $dialog = New-Dialog (Get-Text 'imageTitle') 680 170
    $panel = Dialog-Panel $dialog
    $image = Dialog-Text $panel 'imageHint' $script:vllmImage
    Add-DialogOK $dialog $panel 'chooseImage'
    try {
        if ($dialog.ShowDialog($form) -eq 'OK') {
            $script:vllmImage = $image.Text.Trim()
            & $invalidateChoices
        }
    } finally { $dialog.Dispose() }
}

function Edit-HubSource {
    $engine = Selected-Engine
    $dialog = New-Dialog (Get-Text 'hubTitle' $engine) 680 230
    $panel = Dialog-Panel $dialog
    $label = 'ggufLink'
    if ($engine -eq 'vllm') { $label = 'safetensorsLink' }
    $initial = ''
    if ($script:hfEngine -eq $engine) { $initial = $script:hfURL }
    $link = Dialog-Text $panel $label $initial
    $token = Dialog-Text $panel 'readToken' $script:hfToken
    $token.UseSystemPasswordChar = $true
    Add-DialogOK $dialog $panel 'useSource'
    try {
        if ($dialog.ShowDialog($form) -eq 'OK') {
            $script:hfURL = $link.Text.Trim()
            $script:hfToken = $token.Text.Trim()
            $script:hfEngine = $engine
            $script:localWeights = ''
            $models.Items.Clear()
            Reset-Prepared
            $savePlan.Enabled = [bool](Hub-Selected)
            if ($script:hfURL) { Write-Journal 'sourceSelected' }
        }
    } finally { $dialog.Dispose() }
}

function Edit-LocalSource {
    $dialog = New-Dialog (Get-Text 'localModel') 680 390
    $panel = Dialog-Panel $dialog
    [void](Add-Label $panel 'localSourceHint' 610)
    $format = Add-Combo $panel 600 @((Get-Text 'snapshotFolder'), 'GGUF / llama.cpp')
    $local = Dialog-Text $panel 'localPath' $script:localWeights
    $local.ReadOnly = $true
    [void](Add-Button $panel 'browse' {
        if ($format.SelectedIndex -eq 0) {
            $picker = [Windows.Forms.FolderBrowserDialog]::new()
            $picker.Description = Get-Text 'snapshotFolder'
        } else {
            $picker = [Windows.Forms.OpenFileDialog]::new()
            $picker.Filter = 'GGUF (*.gguf)|*.gguf'
            $picker.RestoreDirectory = $true
        }
        try {
            if ($picker.ShowDialog($dialog) -eq 'OK') {
                if ($format.SelectedIndex -eq 0) { $local.Text = $picker.SelectedPath }
                else { $local.Text = $picker.FileName }
            }
        } finally { $picker.Dispose() }
    })
    $link = Dialog-Text $panel 'localSourceUrl' $script:hfURL
    $token = Dialog-Text $panel 'readToken' $script:hfToken
    $token.UseSystemPasswordChar = $true
    Add-DialogOK $dialog $panel 'useSource'
    try {
        if ($dialog.ShowDialog($form) -ne 'OK') { return }
        if (!$local.Text -or !$link.Text.Trim()) { Show-Message (Get-Text 'localSourceRequired'); return }
        $engine = if ($format.SelectedIndex -eq 0) { 'vllm' } else { 'llama.cpp' }
        $engines.SelectedIndex = [Array]::IndexOf($engineIDs, $engine)
        $script:hfURL = $link.Text.Trim()
        $script:hfEngine = $engine
        $script:hfToken = $token.Text.Trim()
        $script:localWeights = $local.Text
        $models.Items.Clear()
        Reset-Prepared
        $savePlan.Enabled = $true
        Write-Journal 'localSourceSelected' $local.Text
    } finally { $dialog.Dispose() }
}

function Edit-Advanced {
    $key = Profile-Key
    if (!$key) { Show-Message (Get-Text 'selectModelFirst'); return }
    $parts = $key -split '/', 3
    $engine, $task = $parts[0], $parts[1]
    $dialog = New-Dialog (Get-Text 'advancedTitle' $engine) 690 715
    $panel = Dialog-Panel $dialog
    [void](Add-Label $panel 'advancedHint')
    $stored = $script:advanced[$key]
    if (!$stored) { $stored = @{} }
    $fields = [ordered]@{}
    $floats = @('temperature', 'top_p', 'min_p', 'repeat_penalty', 'presence_penalty', 'frequency_penalty', 'gpu_memory_utilization')
    if ($task -ne 'embeddings') {
        $fields.temperature = Dialog-Text $panel 'temperature' $stored['temperature']
        $fields.max_tokens = Dialog-Text $panel 'maxTokens' $stored['max_tokens']
        $fields.top_p = Dialog-Text $panel 'topP' $stored['top_p']
        $fields.top_k = Dialog-Text $panel 'topK' $stored['top_k']
        $fields.repeat_penalty = Dialog-Text $panel 'repeatPenalty' $stored['repeat_penalty']
        $fields.presence_penalty = Dialog-Text $panel 'presencePenalty' $stored['presence_penalty']
        $fields.frequency_penalty = Dialog-Text $panel 'frequencyPenalty' $stored['frequency_penalty']
        $fields.min_p = Dialog-Text $panel 'minP' $stored['min_p']
        if ($engine -ne 'vllm') {
            $fields.repeat_last_n = Dialog-Text $panel 'repeatWindow' $stored['repeat_last_n']
            $fields.seed = Dialog-Text $panel 'seed' $stored['seed']
        }
        if ($engine -eq 'ollama') {
            $fields.stop = Dialog-Text $panel 'stopStrings' ($stored['stop'] -join [Environment]::NewLine) 2
        }
    }
    if ($engine -eq 'vllm') {
        $fields.gpu_memory_utilization = Dialog-Text $panel 'vramFraction' $stored['gpu_memory_utilization']
        [void](Add-Label $panel 'dtype')
        $dtype = Add-Combo $panel 180 @((Get-Text 'default'), 'auto', 'half', 'bfloat16')
        if ($stored['dtype']) { $dtype.SelectedItem = $stored['dtype'] }
        $eager = [Windows.Forms.CheckBox]::new()
        $eager.AutoSize = $true
        $eager.Text = (Get-Text 'enforceEager')
        $eager.Checked = [bool]$stored['enforce_eager']
        $panel.Controls.Add($eager)
    }
    if ($engine -eq 'ollama') {
        [void](Add-Label $panel 'ollamaEnv' 610)
    } elseif ($engine -eq 'vllm') {
        [void](Add-Label $panel 'vllmEnv')
    }
    $previousEnv = @()
    if ($stored['environment']) {
        $previousEnv = @($stored['environment'].GetEnumerator() | ForEach-Object { "$($_.Key)=$($_.Value)" })
    }
    $envField = Dialog-Text $panel 'envHint' ($previousEnv -join [Environment]::NewLine) 4
    Add-DialogOK $dialog $panel 'saveSettings'
    try {
        if ($dialog.ShowDialog($form) -ne 'OK') { return }
        $result = @{}
        if ($engine -eq 'vllm') {
            if ($dtype.SelectedIndex -gt 0) { $result.dtype = [string]$dtype.SelectedItem }
            if ($eager.Checked) { $result.enforce_eager = $true }
        }
        foreach ($entry in $fields.GetEnumerator()) {
            $value = $entry.Value.Text.Trim()
            if (!$value) { continue }
            if ($entry.Key -eq 'stop') {
                $result[$entry.Key] = @($value -split '\r?\n' | Where-Object { $_ })
                continue
            }
            try {
                if ($entry.Key -in $floats) { $result[$entry.Key] = [double]::Parse($value.Replace(',', '.'), [Globalization.CultureInfo]::InvariantCulture) }
                else { $result[$entry.Key] = [int]::Parse($value, [Globalization.CultureInfo]::InvariantCulture) }
            } catch { throw (Get-Text 'invalidNumber') }
        }
        $environment = @{}
        foreach ($line in ($envField.Text -split '\r?\n')) {
            if (!$line.Trim()) { continue }
            $parts = $line -split '=', 2
            $envName = $parts[0].Trim().ToUpperInvariant()
            if ($parts.Count -ne 2 -or !$envName -or $environment.ContainsKey($envName)) { throw (Get-Text 'invalidEnv') }
            $environment[$envName] = $parts[1]
        }
        if ($environment.Count) { $result.environment = $environment }
        if ($result.Count) { $script:advanced[$key] = $result } else { $script:advanced.Remove($key) }
        Reset-Prepared
    } catch { Show-Message $_.Exception.Message }
    finally { $dialog.Dispose() }
}

function Update-LocalizedText($control) {
    if ($control.Tag) { $control.Text = Get-Text $control.Tag }
    foreach ($child in $control.Controls) { Update-LocalizedText $child }
}

$languagePicker.Add_SelectedIndexChanged({
    if ($languagePicker.SelectedIndex -lt 0) { return }
    $script:language = $languageIDs[$languagePicker.SelectedIndex]
    $script:changingLanguage = $true
    try {
        Update-LocalizedText $layout
        $languagePicker.AccessibleName = $languageLabel.Text
        Set-ComboItems $tasks @($taskLabels | ForEach-Object { Get-Text $_ }) $tasks.SelectedIndex
        Set-ComboItems $engines @($engineLabels | ForEach-Object { Get-Text $_ }) $engines.SelectedIndex
        Update-HardwareLabels $devices.SelectedIndex
        Update-CatalogLabel
        if ($script:job) { $progress.Text = Get-Text 'running' $script:job.Command }
        $selectedDir = ''
        if ($projectList.SelectedIndex -ge 0) { $selectedDir = $script:projects[$projectList.SelectedIndex].dir }
        Update-ProjectList $selectedDir
        Update-Journal
    } finally { $script:changingLanguage = $false }
    try {
        [void][IO.Directory]::CreateDirectory($appDir)
        [IO.File]::WriteAllText($languagePreference, $script:language)
    } catch { Write-Journal 'languageSaveError' }
})

$tabs.Add_SelectedIndexChanged({
    if (!$script:changingLanguage -and $tabs.SelectedTab -eq $manage) { Read-Projects }
})
$form.Add_Shown({ Read-Hardware })
$form.Add_FormClosing({
    param($sender, $event)
    if ($script:job) {
        $event.Cancel = $true
        Show-Message (Get-Text 'waitOrCancel')
    }
})
try { [Windows.Forms.Application]::Run($form) }
finally { $timer.Dispose(); $form.Dispose() }
