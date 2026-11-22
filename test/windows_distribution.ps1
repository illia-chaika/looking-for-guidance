# Exercises first launch on a clean Windows user profile, as used by the release verification workflow.
param([Parameter(Mandatory)][string]$ReleaseTag)
$ErrorActionPreference = 'Stop'

$installed = Join-Path $env:LOCALAPPDATA 'Programs/looking-for-guidance/Looking for Guidance.exe'
$licenses = Join-Path (Split-Path $installed) 'Licenses.txt'
$shortcut = Join-Path ([Environment]::GetFolderPath('Programs')) 'Looking for Guidance.lnk'
if ((Test-Path (Split-Path $installed)) -or (Test-Path $shortcut)) {
    throw 'The Windows release check needs a fresh installation for this user.'
}
$download = Join-Path ([IO.Path]::GetTempPath()) ("lfg-download-" + [guid]::NewGuid())
$null = New-Item -ItemType Directory $download
$renamed = Join-Path $download 'Looking for Guidance (1).exe'
Copy-Item (Join-Path $PSScriptRoot '../dist/looking-for-guidance-Windows.exe') $renamed
$expected = (Get-FileHash $renamed).Hash
$log = Join-Path $env:APPDATA 'Looking for Guidance/helper.log'
$version = $ReleaseTag.TrimStart('v')

function Wait-For([scriptblock]$Condition, [string]$Description) {
    $deadline = [datetime]::UtcNow.AddSeconds(45)
    while ([datetime]::UtcNow -lt $deadline) {
        if (& $Condition) { return }
        Start-Sleep -Milliseconds 250
    }
    if (Test-Path $log) { Get-Content $log -Tail 30 }
    throw "Timed out waiting for $Description"
}

function Stop-InstalledApp {
    Get-Process -Name 'Looking for Guidance' -ErrorAction SilentlyContinue |
        Where-Object { $_.Path -eq $installed } |
        ForEach-Object { Stop-Process -Id $_.Id -Force; $_.WaitForExit(10000) | Out-Null }
}

try {
    $first = Start-Process -FilePath $renamed -PassThru
    if (-not $first.WaitForExit(45000)) { throw 'The downloaded EXE did not hand over to the installed app.' }
    Wait-For { (Test-Path $licenses) -and (Test-Path $shortcut) -and (Test-Path $log) -and
        (Select-String -LiteralPath $log -SimpleMatch "Looking for Guidance $version started" -Quiet) } 'the installed release'
    if ((Get-FileHash $installed).Hash -ne $expected) { throw 'The installed EXE differs from the download.' }
    $shell = New-Object -ComObject WScript.Shell
    if ($shell.CreateShortcut($shortcut).TargetPath -ne $installed) { throw 'Start menu shortcut points to another file.' }
    $text = Get-Content -LiteralPath $licenses -Raw
    foreach ($name in 'GNU GENERAL PUBLIC LICENSE', 'CC-BY-4.0-Twemoji.txt', 'OFL-PTSans.txt', 'third-party-notices.txt') {
        if (-not $text.Contains($name)) { throw "Installed licences are missing $name" }
    }
    $licenseHash = (Get-FileHash $licenses).Hash
    Stop-InstalledApp
    Set-Content -LiteralPath $licenses -Value 'Notices from a previous version'
    Start-Process -FilePath $installed
    Wait-For { (Get-FileHash $licenses).Hash -eq $licenseHash } 'notices refreshed on relaunch'
} finally {
    Stop-InstalledApp
}
