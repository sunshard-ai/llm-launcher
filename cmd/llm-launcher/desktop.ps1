$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing
[Windows.Forms.Application]::EnableVisualStyles()
[Windows.Forms.Application]::SetCompatibleTextRenderingDefault($false)

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
$layout.RowCount = 5
foreach ($height in @(48, 46, 282)) {
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

function Add-Label($parent, [string]$text, [int]$width = 0) {
    $label = [Windows.Forms.Label]::new()
    $label.Text = $text
    $label.AutoSize = $width -eq 0
    if ($width) { $label.Width = $width }
    $label.Margin = [Windows.Forms.Padding]::new(4, 7, 4, 0)
    $parent.Controls.Add($label)
    $label
}

function Add-Button($parent, [string]$text, [scriptblock]$action) {
    $button = [Windows.Forms.Button]::new()
    $button.AutoSize = $true
    $button.Height = 31
    $button.Text = $text
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

$header = New-Row
$header.BackColor = [Drawing.Color]::FromArgb(14, 23, 43)
$title = Add-Label $header 'LLM Launcher   /   Локальные модели'
$title.ForeColor = [Drawing.Color]::White
$title.Font = [Drawing.Font]::new('Segoe UI Semibold', 17)
$layout.Controls.Add($header, 0, 0)

$storage = New-Row
[void](Add-Label $storage 'Папка проектов')
$root = [Windows.Forms.TextBox]::new()
$root.ReadOnly = $true
$root.Width = 550
$appDir = Join-Path ([Environment]::GetFolderPath('LocalApplicationData')) 'LLM Launcher'
$rootPreference = Join-Path $appDir 'projects-path.txt'
$root.Text = Join-Path $appDir 'deployments'
try {
    if ([IO.File]::Exists($rootPreference)) {
        $savedRoot = [IO.File]::ReadAllText($rootPreference)
        if ([IO.Directory]::Exists($savedRoot)) { $root.Text = $savedRoot }
    }
} catch { }
$storage.Controls.Add($root)
[void](Add-Button $storage 'Выбрать…' {
    $picker = [Windows.Forms.FolderBrowserDialog]::new()
    $picker.Description = 'Выберите папку для планов развёртывания'
    if ([IO.Directory]::Exists($root.Text)) { $picker.SelectedPath = $root.Text }
    try {
        if ($picker.ShowDialog($form) -eq 'OK') {
            $root.Text = $picker.SelectedPath
            try {
                [void][IO.Directory]::CreateDirectory($appDir)
                [IO.File]::WriteAllText($rootPreference, $root.Text)
            } catch { Write-Log 'Выбранную папку не удалось сохранить для следующего запуска.' }
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
$setup = [Windows.Forms.TabPage]::new('Новое развёртывание')
$manage = [Windows.Forms.TabPage]::new('Мои развёртывания')
$tabs.TabPages.Add($setup)
$tabs.TabPages.Add($manage)
$layout.Controls.Add($tabs, 0, 2)
$setupLayout = [Windows.Forms.TableLayoutPanel]::new()
$setupLayout.Dock = 'Fill'
$setupLayout.Padding = [Windows.Forms.Padding]::new(6)
$setupLayout.ColumnCount = 1
$setupLayout.RowCount = 6
foreach ($height in @(38, 37, 37, 38, 38, 39)) {
    [void]$setupLayout.RowStyles.Add([Windows.Forms.RowStyle]::new('Absolute', $height))
}
$setup.Controls.Add($setupLayout)
$rows = @(for ($i = 0; $i -lt 6; $i++) {
    $row = New-Row
    $setupLayout.Controls.Add($row, 0, $i)
    $row
})
[void](Add-Button $rows[0] 'Сканировать компьютер' { Read-Hardware })
[void](Add-Button $rows[0] 'Обновить список моделей' {
    Start-Launcher @('update') {
        param($code, $output)
        if ($code -eq 0) { & $invalidateChoices }
    }
})
$hardware = Add-Label $rows[0] 'CPU, память, GPU, Docker'
[void](Add-Label $rows[1] 'Задача')
$tasks = Add-Combo $rows[1] 255 @('Чат и тексты', 'Работа с кодом', 'Ответ по контексту (RAG)', 'Векторы для поиска')
$taskIDs = @('chat', 'code', 'rag', 'embeddings')
[void](Add-Label $rows[1] 'Движок')
$engines = Add-Combo $rows[1] 245 @('Авто (каталог)', 'Ollama', 'vLLM / NVIDIA (эксп.)', 'llama.cpp / GGUF')
$engineIDs = @('', 'ollama', 'vllm', 'llama.cpp')
$imageButton = Add-Button $rows[1] 'Образ vLLM…' { Edit-VLLMImage }
$imageButton.Visible = $false
[void](Add-Label $rows[2] 'Ускорение')
$devices = Add-Combo $rows[2] 258 @('Авто', 'CPU')
[void](Add-Label $rows[2] 'Контекст')
$context = Add-Number $rows[2] 4096 512 32768
[void](Add-Label $rows[2] 'Порт (0 = по умолчанию)')
$port = Add-Number $rows[2] 0 0 65535
$pickModel = Add-Button $rows[3] 'Подобрать модели' {
    Read-Recommendations
}
$models = Add-Combo $rows[3] 490
$importVLLM = Add-Button $rows[3] 'Свой HF…' { Edit-HubSource }
$importVLLM.Visible = $false
[void](Add-Label $rows[4] 'Имя папки проекта')
$name = [Windows.Forms.TextBox]::new()
$name.Width = 275
$name.Text = 'model-' + [DateTime]::Now.ToString('yyyyMMdd-HHmmss')
$rows[4].Controls.Add($name)
$advancedButton = Add-Button $rows[4] 'Проф. настройки…' { Edit-Advanced }
$savePlan = Add-Button $rows[5] 'Подготовить план' { Save-ReviewedPlan }
$startPrepared = Add-Button $rows[5] 'Запустить подготовленный план' {
    if ($script:preparedPath) { Run-Project 'up' $script:preparedPath }
}
$detailsButton = Add-Button $rows[5] 'Посмотреть детали…' {
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
[void](Add-Button $manageTop 'Обновить список' { Read-Projects })
[void](Add-Label $manageTop 'Сохранённое состояние. Для проверки API нажмите «Статус».')
$projectList = [Windows.Forms.ListBox]::new()
$projectList.Dock = 'Fill'
$projectList.HorizontalScrollbar = $true
$manageLayout.Controls.Add($projectList, 0, 1)
$actions = New-Row
$manageLayout.Controls.Add($actions, 0, 2)
[void](Add-Button $actions 'Запустить' { Invoke-Selected 'up' })
[void](Add-Button $actions 'Статус' { Invoke-Selected 'status' })
[void](Add-Button $actions 'Логи' { Invoke-Selected 'logs' })
[void](Add-Button $actions 'Тест API' { Invoke-Selected 'test' })
[void](Add-Button $actions 'Остановить' { Invoke-Selected 'down' })
[void](Add-Button $actions 'Архивировать' { Invoke-Selected 'delete' })
[void](Add-Button $actions 'Восстановить' { Invoke-Selected 'restore' })
[void](Add-Button $actions 'Детали…' { Invoke-Selected 'inspect' })

$log = [Windows.Forms.TextBox]::new()
$log.Dock = 'Fill'
$log.Multiline = $true
$log.ReadOnly = $true
$log.ScrollBars = 'Both'
$log.WordWrap = $false
$log.Font = [Drawing.Font]::new('Consolas', 10)
$layout.Controls.Add($log, 0, 3)
$footer = New-Row
$layout.Controls.Add($footer, 0, 4)
$cancel = Add-Button $footer 'Отменить команду' {
    if ($script:job -and !$script:job.Process.HasExited) {
        try { $script:job.Process.StandardInput.Close() }
        catch { Write-Log $_.Exception.Message }
    }
    $cancel.Enabled = $false
}
$cancel.Enabled = $false
$progress = Add-Label $footer 'Готово к работе'
$script:job = $null
$script:preparedPath = $null
$script:choices = @()
$script:projects = @()
$script:gpuIDs = @()
$script:advanced = @{}
$script:hfURL = ''
$script:hfEngine = ''
$script:hfToken = ''
$script:vllmImage = ''

function Show-Message([string]$text) {
    [void][Windows.Forms.MessageBox]::Show($form, $text, 'LLM Launcher', 'OK', 'Information')
}

function Write-Log([string]$line) {
    if ($log.TextLength -gt 100000) { $log.Text = $log.Text.Substring($log.TextLength - 75000) }
    $line = [regex]::Replace($line, '\x1B\[[0-?]*[ -/]*[@-~]', '')
    $log.AppendText($line + [Environment]::NewLine)
}

function Quote-Arg([string]$value) {
    # Quote according to the Windows argv rules. No shell executes these arguments.
    $escaped = [regex]::Replace($value, '(\\*)"', '$1$1\"')
    '"' + [regex]::Replace($escaped, '(\\+)$', '$1$1') + '"'
}

function Start-Launcher([string[]]$arguments, [scriptblock]$onDone = {}, [string]$data = '', [switch]$capture, [string]$hubToken = '') {
    if ($script:job) { return }
    try {
        $info = [Diagnostics.ProcessStartInfo]::new($env:LLM_LAUNCHER_DESKTOP_EXE)
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
        }
        $storage.Enabled = $false
        $tabs.Enabled = $false
        $cancel.Enabled = $true
        $progress.Text = 'Выполняется: ' + $arguments[0]
        $timer.Start()
    } catch { Show-Message $_.Exception.Message }
}

$timer = [Windows.Forms.Timer]::new()
$timer.Interval = 100
$timer.Add_Tick({
    $job = $script:job
    foreach ($key in @('StdOut', 'StdErr')) {
        for ($i = 0; $i -lt 100 -and $job[$key] -and $job[$key].IsCompleted; $i++) {
            $task = $job[$key]
            $job[$key] = $null
            if ($task.IsFaulted) { Write-Log $task.Exception.Message; break }
            $line = $task.Result
            if ($null -eq $line) { break }
            if ($key -eq 'StdOut') {
                if ($job.Capture) { [void]$job.Output.AppendLine($line) } else { Write-Log $line }
                $job[$key] = $job.Process.StandardOutput.ReadLineAsync()
            } else {
                Write-Log $line
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
    if ($exitCode -eq 0) { $progress.Text = 'Команда завершена' }
    else { $progress.Text = 'Команда завершилась с ошибкой. См. журнал.' }
    try { & $job.OnDone $exitCode $job.Output.ToString() $job.Data }
    catch { Write-Log $_.Exception.Message }
})

function Read-Hardware {
    $log.Clear()
    Start-Launcher @('scan', '--json', '--dir', $root.Text) {
        param($code, $json)
        if ($code -ne 0) { return }
        $hw = ConvertFrom-Json $json
        $hardware.Text = "$($hw.cpu_name)   /   RAM $([Math]::Round($hw.ram_total_bytes / 1GB)) ГиБ"
        $devices.Items.Clear()
        $devices.Items.AddRange(@('Авто', 'CPU'))
        $script:gpuIDs = @()
        foreach ($gpu in $hw.gpus) {
            if ($gpu.vendor -ne 'nvidia') { continue }
            $id = $gpu.uuid
            if (!$id) { $id = $gpu.index }
            $script:gpuIDs += $id
            [void]$devices.Items.Add("$($gpu.name) | свободно $([Math]::Round($gpu.vram_free_bytes / 1GB, 1)) ГиБ")
        }
        $devices.SelectedIndex = 0
        Write-Log "Docker доступен: $($hw.docker.reachable) | Compose: $($hw.docker.compose) | RAM Docker: $([Math]::Round($hw.docker.memory_bytes / 1GB, 1)) ГиБ"
        if ($hw.docker.error) { Write-Log $hw.docker.error }
        foreach ($note in $hw.warnings) { Write-Log $note }
    } -Capture
}

function Fit-Args {
    '--dir', $root.Text, '--task', $taskIDs[$tasks.SelectedIndex], '--context', [string]$context.Value
    $engine = Selected-Engine
    if ($engine) { '--runtime'; $engine }
    if ($engine -eq 'vllm' -and $script:vllmImage) { '--image'; $script:vllmImage }
    if ($devices.SelectedIndex -eq 1) { '--cpu' }
    if ($devices.SelectedIndex -ge 2) { '--gpu'; $script:gpuIDs[$devices.SelectedIndex - 2] }
}

function Read-Recommendations {
    Reset-Prepared
    $script:hfEngine = ''
    $log.Clear()
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
        foreach ($note in $result.notes) { Write-Log $note }
    } -Capture
}

$models.Add_SelectedIndexChanged({
    Reset-Prepared
    if ($models.SelectedIndex -lt 0) { return }
    $script:hfEngine = ''
    $log.Clear()
    $r = $script:choices[$models.SelectedIndex]
    Write-Log "$($r.model.ref) | $($r.model.url)"
    foreach ($note in $r.notes) { Write-Log $note }
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
        Show-Message 'Для vLLM выберите NVIDIA GPU или «Авто».'
        return
    }
    if (!$hub -and $models.SelectedIndex -lt 0) { return }
    $projectName = $name.Text.Trim()
    if ($projectName -notmatch '^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$' -or $projectName -match '^(CON|PRN|AUX|NUL|COM[0-9]|LPT[0-9])$') {
        Show-Message 'Имя папки должно содержать до 64 латинских букв, цифр, дефисов или подчёркиваний.'
        return
    }
    Reset-Prepared
    $dir = Join-Path $root.Text $projectName
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
    $log.Clear()
    Start-Launcher $arguments {
        param($code, $output, $dir)
        if ($code -ne 0) { return }
        $p = ConvertFrom-Json $output
        $script:preparedPath = $dir
        $startPrepared.Enabled = $true
        $detailsButton.Enabled = $true
        Write-Log "$($p.model.name) | $($p.runtime) / $($p.acceleration) | контекст $($p.context)"
        Write-Log "Папка: $dir"
        Write-Log "API после запуска: http://127.0.0.1:$($p.port)/v1"
        if ($p.runtime -eq 'vllm') { Write-Log "Образ: $($p.image)" }
        if ($p.hf_source) {
            Write-Log "Hugging Face: $($p.hf_source.repo) | $($p.hf_source.revision)"
            Write-Log "Размер весов и конфигурации: $([Math]::Round($p.hf_source.bytes / 1GB, 2)) ГиБ"
        }
        if ($p.advanced) { Write-Log 'Проф. настройки применены к плану.' }
        Write-Log 'План готов. Запуск начнётся после нажатия «Запустить».'
    } -Data $dir -HubToken $secret -Capture
}

function Show-Compose([string]$dir) {
    Start-Launcher @('inspect', '--compose', '--dir', $dir) {
        param($code, $output)
        if ($code -ne 0) { return }
        $dialog = New-Dialog 'Детали развёртывания · Docker Compose' 820 560
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
    } -Capture
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
        $projectList.Items.Clear()
        foreach ($p in $script:projects) {
            $index = $projectList.Items.Add("[$($p.state)]  $($p.model)   |   $($p.dir)")
            if ($p.dir -eq $old) { $projectList.SelectedIndex = $index }
            if ($p.error) { Write-Log "$($p.dir): $($p.error)" }
        }
    } -Capture
}

function Run-Project([string]$command, [string]$dir) {
    $log.Clear()
    $done = {}
    if ($command -in @('up', 'down', 'delete', 'restore')) {
        $done = { param($code, $output); if ($code -eq 0) { Read-Projects } }
    }
    $secret = ''
    if ($command -eq 'up') { $secret = $script:hfToken }
    Start-Launcher @($command, '--dir', $dir) $done -HubToken $secret
}

function Invoke-Selected([string]$command) {
    if ($projectList.SelectedIndex -lt 0) { Show-Message 'Выберите развёртывание из списка.'; return }
    $p = $script:projects[$projectList.SelectedIndex]
    if ($p.state -eq 'invalid') { Show-Message $p.error; return }
    if ($command -eq 'inspect') { Show-Compose $p.dir; return }
    if ($command -eq 'delete') {
        $answer = [Windows.Forms.MessageBox]::Show($form, 'Остановить и архивировать проект? План и скачанные веса сохранятся.', 'LLM Launcher', 'YesNo', 'Question')
        if ($answer -ne 'Yes') { return }
    }
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
    [void](Add-Label $parent $label)
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

function Add-DialogOK($dialog, $panel, [string]$text) {
    $ok = Add-Button $panel $text {}
    $ok.DialogResult = 'OK'
    $dialog.AcceptButton = $ok
}

function Edit-VLLMImage {
    $dialog = New-Dialog 'Образ vLLM' 680 170
    $panel = Dialog-Panel $dialog
    $image = Dialog-Text $panel 'Пусто = стандартный, latest = текущий digest, либо официальный тег или digest' $script:vllmImage
    Add-DialogOK $dialog $panel 'Выбрать образ'
    try {
        if ($dialog.ShowDialog($form) -eq 'OK') {
            $script:vllmImage = $image.Text.Trim()
            & $invalidateChoices
        }
    } finally { $dialog.Dispose() }
}

function Edit-HubSource {
    $engine = Selected-Engine
    $dialog = New-Dialog "Модель с Hugging Face · $engine" 680 230
    $panel = Dialog-Panel $dialog
    $label = 'Ссылка на отдельный файл .gguf (blob/resolve, main или commit)'
    if ($engine -eq 'vllm') { $label = 'Ссылка на репозиторий модели Safetensors (или /tree/commit)' }
    $initial = ''
    if ($script:hfEngine -eq $engine) { $initial = $script:hfURL }
    $link = Dialog-Text $panel $label $initial
    $token = Dialog-Text $panel 'Токен чтения (необязательно, только на время сеанса)' $script:hfToken
    $token.UseSystemPasswordChar = $true
    Add-DialogOK $dialog $panel 'Использовать файл'
    try {
        if ($dialog.ShowDialog($form) -eq 'OK') {
            $script:hfURL = $link.Text.Trim()
            $script:hfToken = $token.Text.Trim()
            $script:hfEngine = $engine
            $models.Items.Clear()
            Reset-Prepared
            $savePlan.Enabled = [bool](Hub-Selected)
            if ($script:hfURL) { Write-Log 'Источник выбран. Кнопка «Подготовить план» проверит источник и определит размер загрузки.' }
        }
    } finally { $dialog.Dispose() }
}

function Edit-Advanced {
    $key = Profile-Key
    if (!$key) { Show-Message 'Сначала выберите модель из списка или укажите источник Hugging Face.'; return }
    $parts = $key -split '/', 3
    $engine, $task = $parts[0], $parts[1]
    $dialog = New-Dialog "Проф. настройки · $engine" 690 715
    $panel = Dialog-Panel $dialog
    [void](Add-Label $panel 'Пустое поле сохраняет значение движка или модели.')
    $stored = $script:advanced[$key]
    if (!$stored) { $stored = @{} }
    $fields = [ordered]@{}
    $floats = @('temperature', 'top_p', 'min_p', 'repeat_penalty', 'presence_penalty', 'frequency_penalty', 'gpu_memory_utilization')
    if ($task -ne 'embeddings') {
        $fields.temperature = Dialog-Text $panel 'Температура (0–2)' $stored['temperature']
        $fields.max_tokens = Dialog-Text $panel 'Максимум токенов ответа' $stored['max_tokens']
        $fields.top_p = Dialog-Text $panel 'Top P (0.001–1)' $stored['top_p']
        $fields.top_k = Dialog-Text $panel 'Top K (1–1000)' $stored['top_k']
        $fields.repeat_penalty = Dialog-Text $panel 'Штраф за повторения (0.1–3)' $stored['repeat_penalty']
        $fields.presence_penalty = Dialog-Text $panel 'Штраф за уже встречавшиеся токены (-2–2)' $stored['presence_penalty']
        $fields.frequency_penalty = Dialog-Text $panel 'Штраф за частоту токенов (-2–2)' $stored['frequency_penalty']
        $fields.min_p = Dialog-Text $panel 'Min P (0–1)' $stored['min_p']
        if ($engine -ne 'vllm') {
            $fields.repeat_last_n = Dialog-Text $panel 'Окно повторов (-1 = контекст)' $stored['repeat_last_n']
            $fields.seed = Dialog-Text $panel 'Seed' $stored['seed']
        }
        if ($engine -eq 'ollama') {
            $fields.stop = Dialog-Text $panel 'Стоп-строки, по одной на строку' ($stored['stop'] -join [Environment]::NewLine) 2
        }
    }
    if ($engine -eq 'vllm') {
        $fields.gpu_memory_utilization = Dialog-Text $panel 'Доля VRAM (0.01–0.95, с проверкой бюджета)' $stored['gpu_memory_utilization']
        [void](Add-Label $panel 'Тип вычислений (dtype)')
        $dtype = Add-Combo $panel 180 @('По умолчанию', 'auto', 'half', 'bfloat16')
        if ($stored['dtype']) { $dtype.SelectedItem = $stored['dtype'] }
        $eager = [Windows.Forms.CheckBox]::new()
        $eager.AutoSize = $true
        $eager.Text = 'Enforce eager (без CUDA Graphs)'
        $eager.Checked = [bool]$stored['enforce_eager']
        $panel.Controls.Add($eager)
    }
    if ($engine -eq 'ollama') {
        [void](Add-Label $panel 'Ollama: FLASH_ATTENTION, KV_CACHE_TYPE, KEEP_ALIVE, LOAD_TIMEOUT, MAX_QUEUE (префикс OLLAMA_).')
    } elseif ($engine -eq 'vllm') {
        [void](Add-Label $panel 'vLLM: VLLM_LOGGING_LEVEL (DEBUG, INFO, WARNING, ERROR).')
    }
    $previousEnv = @()
    if ($stored['environment']) {
        $previousEnv = @($stored['environment'].GetEnumerator() | ForEach-Object { "$($_.Key)=$($_.Value)" })
    }
    $envField = Dialog-Text $panel 'Переменные без секретов: ИМЯ=значение, свои имена с префиксом LLM_USER_' ($previousEnv -join [Environment]::NewLine) 4
    Add-DialogOK $dialog $panel 'Сохранить настройки'
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
            if ($entry.Key -eq 'stop') { $result[$entry.Key] = @($value -split '\r?\n' | Where-Object { $_ }) }
            elseif ($entry.Key -in $floats) { $result[$entry.Key] = [double]::Parse($value.Replace(',', '.'), [Globalization.CultureInfo]::InvariantCulture) }
            else { $result[$entry.Key] = [int]::Parse($value, [Globalization.CultureInfo]::InvariantCulture) }
        }
        $environment = @{}
        foreach ($line in ($envField.Text -split '\r?\n')) {
            if (!$line.Trim()) { continue }
            $parts = $line -split '=', 2
            $envName = $parts[0].Trim().ToUpperInvariant()
            if ($parts.Count -ne 2 -or !$envName -or $environment.ContainsKey($envName)) { throw 'Проверьте строки переменных: одно уникальное ИМЯ=значение на строку.' }
            $environment[$envName] = $parts[1]
        }
        if ($environment.Count) { $result.environment = $environment }
        if ($result.Count) { $script:advanced[$key] = $result } else { $script:advanced.Remove($key) }
        Reset-Prepared
    } catch { Show-Message $_.Exception.Message }
    finally { $dialog.Dispose() }
}

$tabs.Add_SelectedIndexChanged({ if ($tabs.SelectedTab -eq $manage) { Read-Projects } })
$form.Add_Shown({ Read-Hardware })
$form.Add_FormClosing({
    param($sender, $event)
    if ($script:job) {
        $event.Cancel = $true
        Show-Message 'Дождитесь завершения команды или нажмите «Отменить команду».'
    }
})
try { [Windows.Forms.Application]::Run($form) }
finally { $timer.Dispose(); $form.Dispose() }
