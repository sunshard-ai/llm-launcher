$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'test-desktop-journal.ps1')
foreach ($name in 'Set-TextKey', 'Quote-Arg', 'Start-Launcher', 'New-ProjectPath', 'Update-CatalogLabel', 'Read-Catalog', 'Update-ModelCatalog', 'Import-LocalWeights', 'Show-ModelChoices') {
    $definition = $ast.Find({
        param($node)
        $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -eq $name
    }, $true)
    . ([ScriptBlock]::Create($definition.Extent.Text))
}
$script:language = 'en'
$models = [pscustomobject]@{ DroppedDown = $false; Focused = $false }
$models | Add-Member ScriptMethod Focus { $this.Focused = $true; return $true }
$script:choices = @(@{}, @{})
Show-ModelChoices
if (!$models.DroppedDown -or !$models.Focused -or !$log.Text.Contains('2 compatible profiles')) {
    throw 'Successful recommendations did not display the model choices.'
}
$storage = [pscustomobject]@{ Enabled = $true }
$tabs = [pscustomobject]@{ Enabled = $true }
$cancel = [pscustomobject]@{ Enabled = $false }
$progress = [pscustomobject]@{ Tag = ''; Text = '' }
$timer = [pscustomobject]@{ Enabled = $false }
$timer | Add-Member ScriptMethod Start { $this.Enabled = $true }
$timer | Add-Member ScriptMethod Stop { $this.Enabled = $false }
$script:job = $null
$testDir = Join-Path ([IO.Path]::GetTempPath()) ('llm desktop tests ' + [Guid]::NewGuid().ToString('N'))
[void][IO.Directory]::CreateDirectory($testDir)
$savedDirectory = [Environment]::CurrentDirectory
$savedExecutable = $env:LLM_LAUNCHER_DESKTOP_EXE
try {
    [void][IO.Directory]::CreateDirectory((Join-Path $testDir 'model'))
    [void][IO.Directory]::CreateDirectory((Join-Path $testDir 'model-2'))
    if ((New-ProjectPath $testDir 'model') -cne (Join-Path $testDir 'model-3')) {
        throw 'Replanning did not select a new folder.'
    }
    $script:launcherExecutable = Join-Path $testDir 'missing.exe'
    Start-Launcher @('help')
    if ($script:job -or !$storage.Enabled -or !$tabs.Enabled -or $cancel.Enabled -or !$log.Text.Contains('Could not start the command.')) {
        throw 'A start failure left the interface locked or hid the recovery step.'
    }
    if ([Environment]::OSVersion.Platform -eq 'Win32NT') {
        $script:launcherExecutable = Join-Path $env:SystemRoot 'System32/where.exe'
    } else { $script:launcherExecutable = '/bin/echo' }
    # The worker path is fixed for the session. A changed environment variable
    # or current directory must not affect later command starts.
    $env:LLM_LAUNCHER_DESKTOP_EXE = Join-Path $testDir 'also missing.exe'
    [Environment]::CurrentDirectory = $testDir
    foreach ($attempt in 1..2) {
        Start-Launcher @('help')
        $job = $script:job
        if (!$job -or !$timer.Enabled -or $storage.Enabled -or $tabs.Enabled) {
            throw 'A command could not be retried after a start failure.'
        }
        if ($job.Process.StartInfo.WorkingDirectory -cne [IO.Path]::GetDirectoryName($script:launcherExecutable)) {
            throw 'The command inherited an unrelated working directory.'
        }
        $job.Process.StandardInput.Close()
        if (!$job.Process.WaitForExit(5000)) { $job.Process.Kill(); throw 'Process fixture timed out.' }
        $job.Process.Dispose()
        $script:job = $null
        $timer.Stop()
    }
} finally {
    [Environment]::CurrentDirectory = $savedDirectory
    $env:LLM_LAUNCHER_DESKTOP_EXE = $savedExecutable
    [IO.Directory]::Delete($testDir, $true)
}

$script:commands = [Collections.Generic.List[string]]::new()
$script:reloads = 0
$script:hubSelected = $false
$script:fakeCatalog = '{"updated":"2026-11-08","models":[{},{},{}]}'
$catalogLabel = [pscustomobject]@{ Text = '' }
$startPrepared = [pscustomobject]@{ Enabled = $false }
function Hub-Selected { $script:hubSelected }
function Read-Recommendations { $script:reloads++ }
function Start-Launcher([string[]]$arguments, [scriptblock]$onDone = {}, [string]$data = '', [switch]$capture, [string]$hubToken = '') {
    $script:commands.Add($arguments[0])
    $script:lastArguments = $arguments
    if ($arguments[0] -eq 'catalog') { & $onDone 0 $script:fakeCatalog $data }
    else { & $onDone 0 '' $data }
}
Update-ModelCatalog
if (($script:commands -join ',') -cne 'update,catalog' -or $script:reloads -ne 1 -or !$catalogLabel.Text.Contains('3 profiles')) {
    throw 'Catalog update did not refresh its version and recommendations.'
}
$script:hubSelected = $true
Update-ModelCatalog
if ($script:reloads -ne 1) { throw 'Catalog update discarded an imported model choice.' }
$script:preparedPath = 'C:\Models with spaces\project'
$sourcePath = 'D:\Downloaded weights\snapshot'
Import-LocalWeights $script:preparedPath $sourcePath
if ($script:lastArguments[4] -cne $sourcePath -or !$startPrepared.Enabled) {
    throw 'Local weights lost their path or did not unlock the prepared plan.'
}
foreach ($language in 'en', 'de', 'ru', 'zh', 'fr') {
    $script:language = $language
    $entry = Find-Diagnostic 'local port 11434 is busy or unavailable; choose another port or use 0 for automatic selection'
    if (!$entry -or $entry.Key -ne 'portUnavailable') { throw 'Occupied port recovery was not recognized.' }
    if (!(Format-JournalEntry $entry).Contains('11434')) { throw 'Localized recovery lost the occupied port.' }
}
'Desktop workflow checks passed (mock controls and native process fixture).'
