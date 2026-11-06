$ErrorActionPreference = 'Stop'
$desktop = Join-Path (Split-Path -Parent $PSScriptRoot) 'cmd/llm-launcher'
$desktopStrings = Get-Content -LiteralPath (Join-Path $desktop 'desktop_strings.json') -Raw -Encoding UTF8 | ConvertFrom-Json
$tokens = $errors = $null
$ast = [Management.Automation.Language.Parser]::ParseFile((Join-Path $desktop 'desktop.ps1'), [ref]$tokens, [ref]$errors)
if ($errors.Count) { throw ($errors -join "`n") }

foreach ($assignment in $ast.EndBlock.Statements) {
    if ($assignment -is [Management.Automation.Language.AssignmentStatementAst] -and
        $assignment.Left.Extent.Text -in @('$diagnosticKeys', '$diagnosticPatterns', '$script:journal', '$script:technicalOutput')) {
        . ([ScriptBlock]::Create($assignment.Extent.Text))
    }
}
foreach ($name in 'Get-Text', 'Availability', 'Format-JournalEntry', 'Update-Journal', 'Clear-Journal', 'Write-Journal', 'Find-Diagnostic', 'Write-TechnicalOutput', 'Write-Diagnostic') {
    $definition = $ast.Find({
        param($node)
        $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -eq $name
    }, $true)
    . ([ScriptBlock]::Create($definition.Extent.Text))
}

$log = [pscustomobject]@{ Text = ''; SelectionStart = 0 }
$log | Add-Member ScriptProperty TextLength { $this.Text.Length }
$log | Add-Member ScriptMethod AppendText { param($text) $this.Text += $text }
$log | Add-Member ScriptMethod ScrollToCaret {}
$log | Add-Member ScriptMethod Clear { $this.Text = '' }
$technicalButton = [pscustomobject]@{ Enabled = $false }

$scanner = Join-Path (Split-Path -Parent $PSScriptRoot) 'internal/hardware/hardware.go'
$diskNote = $desktopStrings.diskScope.source
if (![IO.File]::ReadAllText($scanner).Contains($diskNote)) { throw 'The disk-space warning no longer matches its translation.' }

$script:language = 'ru'
Write-Journal 'dockerSummary' (Availability $true) (Availability $false) (31.3)
Write-Diagnostic $diskNote
$raw = 'External container output'
Write-Diagnostic $raw
if (!$log.Text.Contains($diskNote) -or $log.Text.Contains($raw) -or !$technicalButton.Enabled) {
    throw 'The Russian disk warning or the separate technical output is missing.'
}

foreach ($language in 'fr', 'de', 'zh', 'en') {
    $script:language = $language
    Update-Journal
    $culture = [Globalization.CultureInfo]::GetCultureInfo($language)
    $expected = @(
        [string]::Format($culture, $desktopStrings.dockerSummary.$language, $desktopStrings.available.$language, $desktopStrings.unavailable.$language, 31.3)
        $desktopStrings.diskScope.$language
        $desktopStrings.technicalOutputHint.$language
        ''
    ) -join [Environment]::NewLine
    if ($log.Text -cne $expected) { throw "Expected $language journal:`n$expected`nActual:`n$($log.Text)" }
    if ($script:technicalOutput.ToString().Trim() -cne $raw) { throw 'Original output changed when switching languages.' }
}

Write-Diagnostic 'Ready: http://127.0.0.1:8080/v1 (model qwen)'
if (!$log.Text.EndsWith("API ready: http://127.0.0.1:8080/v1 · model: qwen" + [Environment]::NewLine)) {
    throw 'A streamed entry lost its arguments or line break after switching.'
}
Write-Diagnostic 'GPU RTX 4080 исключена из вариантов Ollama: драйвер NVIDIA 470.00 старее требуемой для compute capability 8.9 ветки 520.'
if (!$log.Text.Contains('GPU RTX 4080 was excluded for Ollama: NVIDIA driver 470.00 is older than branch 520 required for compute capability 8.9.')) {
    throw 'The GPU warning or its nested reason was not localized.'
}
Clear-Journal
if ($log.Text -or $script:journal.Count -or $script:technicalOutput.Length -or $technicalButton.Enabled) {
    throw 'Clearing the journal retained output from the previous command.'
}
'Desktop journal checks passed.'
