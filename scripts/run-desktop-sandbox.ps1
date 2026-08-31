# Run the desktop setup app against a throwaway environment.
#
# The app's last step APPLIES a plan: it creates workspace folders, writes
# manifests, and installs hooks into shell profiles. That is the point of it,
# and it is also why trying it out on a working machine is a bad idea -- the
# obvious way to test a setup wizard is to let it set something up.
#
# This points TQ_HOME, HOME, USERPROFILE and GIT_CONFIG_GLOBAL at a scratch
# directory first, so every button including Apply is harmless: workspaces are
# created under the scratch tree, hooks are written to scratch profiles, and
# the real configuration is never read or touched. Delete the folder when done.
#
#   powershell -ExecutionPolicy Bypass -File scripts\run-desktop-sandbox.ps1
#
# Pass -Real to run against the actual environment instead. Only do that when
# you mean to change something.
param(
    [switch]$Real,
    # Populate the sandbox work folder with folders that already exist and have
    # never seen tq -- the situation almost every real user arrives in.
    [switch]$Populate,
    [string]$Exe = "$PSScriptRoot\..\desktop\build\bin\tentaqles-setup.exe"
)

$ErrorActionPreference = 'Stop'

if (-not (Test-Path $Exe)) {
    Write-Host "Not built yet. Build it with:" -ForegroundColor Yellow
    Write-Host "    cd desktop; wails build"
    exit 1
}

if ($Real) {
    Write-Host "Running against your REAL environment. Apply will change this machine." -ForegroundColor Yellow
    & $Exe
    exit $LASTEXITCODE
}

$scratch = Join-Path $env:TEMP ("tq-desktop-" + [guid]::NewGuid().ToString('N').Substring(0, 8))
$sHome = Join-Path $scratch 'home'
# DefaultBase() suggests $HOME/work, so create exactly that: the app's own
# default should be usable without typing anything.
$sWork = Join-Path $sHome 'work'
# Windows' folder picker resolves %USERPROFILE%\Desktop and friends, and pops
# "Location is not available" when they are missing. A redirected home needs
# the standard shell folders or the dialog fails before it opens.
New-Item -ItemType Directory -Force -Path $sHome, $sWork,
    (Join-Path $sHome 'Desktop'), (Join-Path $sHome 'Documents'), (Join-Path $sHome 'Downloads') | Out-Null
'' | Out-File (Join-Path $sHome '.gitconfig') -Encoding utf8

$env:TQ_HOME = Join-Path $scratch 'tentaqles'
$env:HOME = $sHome
$env:USERPROFILE = $sHome
$env:GIT_CONFIG_GLOBAL = Join-Path $sHome '.gitconfig'
foreach ($n in @('__TQ_STATE', 'TQ_WS', 'TQ_WS_ROOT', 'CLAUDE_CONFIG_DIR', 'GH_CONFIG_DIR')) {
    Remove-Item "env:$n" -ErrorAction SilentlyContinue
}

if ($Populate) {
    $seed = @(
        @{ Name = 'personal';  Repos = @('dotfiles', 'blog');      Who = 'Renato';       Mail = 'me@personal.test' },
        @{ Name = 'acme';      Repos = @('api', 'web', 'infra');   Who = 'Renato (Acme)'; Mail = 'renato@acme.test' },
        @{ Name = 'globex';    Repos = @('platform');              Who = 'R. Domingues'; Mail = 'rd@globex.test' }
    )
    foreach ($c in $seed) {
        foreach ($r in $c.Repos) {
            $g = Join-Path $sWork (Join-Path $c.Name (Join-Path $r '.git'))
            New-Item -ItemType Directory -Force -Path $g | Out-Null
            @(
                '[core]'
                "`tbare = false"
                '[user]'
                "`tname = $($c.Who)"
                "`temail = $($c.Mail)"
            ) -join "`n" | ForEach-Object { [IO.File]::WriteAllText((Join-Path $g 'config'), $_, (New-Object Text.UTF8Encoding $false)) }
        }
    }
    Write-Host "Seeded:       personal, acme, globex (with repos and git identities)"
}

Write-Host "Sandbox:      $scratch"
Write-Host "Work folder:  $sWork   (the app should offer this already)"
Write-Host ""
Write-Host "Everything the app writes lands in the sandbox. Click through as far"
Write-Host "as you like, Apply included."
Write-Host ""

& $Exe

Write-Host ""
Write-Host "--- what the app actually wrote ---"
if (Test-Path $scratch) {
    Get-ChildItem $scratch -Recurse -File -ErrorAction SilentlyContinue |
        Select-Object -First 40 |
        ForEach-Object { "  " + $_.FullName.Substring($scratch.Length + 1) }
    $n = (Get-ChildItem $scratch -Recurse -File -ErrorAction SilentlyContinue | Measure-Object).Count
    Write-Host "  ($n files in total)"
}
Write-Host ""
Write-Host "Remove the sandbox with:"
Write-Host "    Remove-Item '$scratch' -Recurse -Force"
