# tqx: the development build of tentaqles, installed beside the released program and never over it.
#
#   dev\tqx.ps1                 build tqx from this checkout and install it
#   dev\tqx.ps1 -Playground     build, then open a nested shell on a throwaway home with two test clients
#   dev\tqx.ps1 -Playground -Reset   the same, with a fresh throwaway home
#   dev\tqx.ps1 -Playground -SetupOnly   build the throwaway home and open nothing
#   dev\tqx.ps1 -NoBuild ...    skip the build and use the tqx that is installed
#
# tqx lands in %LOCALAPPDATA%\tentaqles-dev\bin\tqx.exe. The released tentaqles and tq, your real
# ~/.tentaqles, your git configuration and your shell profile are not touched: the playground is a
# nested PowerShell whose HOME, USERPROFILE and TQ_HOME point into %LOCALAPPDATA%\tentaqles-dev\playground,
# built by `tqx lobe sandbox`, and everything is put back when you type exit.
param([switch]$Playground, [switch]$Reset, [switch]$NoBuild, [switch]$SetupOnly, [string]$Version = '0.15.0-lobe')

$ErrorActionPreference = 'Stop'
$repo = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
$dev  = Join-Path $env:LOCALAPPDATA 'tentaqles-dev'
$bin  = Join-Path $dev 'bin'
$tqx  = Join-Path $bin 'tqx.exe'
$pg   = Join-Path $dev 'playground'

if (-not $NoBuild) {
    New-Item -ItemType Directory -Force $bin | Out-Null
    Push-Location $repo
    try {
        $sha = (git rev-parse HEAD).Trim()
        $stamp = "$Version+" + $sha.Substring(0, 8)
        if (git status --porcelain) { $stamp += '.dirty' }
        $pkg = 'github.com/tentaqles/tentaqles/internal/cmd'
        $old = $env:CGO_ENABLED
        $env:CGO_ENABLED = '0'
        try {
            go build -ldflags "-s -w -X $pkg.Version=$stamp -X $pkg.Commit=$sha" -o $tqx ./cmd/tentaqles
            if ($LASTEXITCODE -ne 0) { throw "go build failed" }
        }
        finally { $env:CGO_ENABLED = $old }
    }
    finally { Pop-Location }
    Write-Host ("tqx installed: " + (& $tqx version))
    Write-Host "  $tqx"
}
if (-not (Test-Path $tqx)) { throw "tqx.exe not found in $bin (run dev\tqx.ps1 without -NoBuild first)" }
if (-not $Playground) { return }

if ($Reset -and (Test-Path $pg)) { Remove-Item -Recurse -Force $pg -Confirm:$false }

$homeVars  = @('HOME', 'USERPROFILE', 'TQ_HOME', 'TQ_NO_BANNER', 'PATH')
$clearVars = @('CLAUDE_CONFIG_DIR', 'CODEX_HOME', 'TQ_WS', 'TQ_WS_ROOT', '__TQ_STATE', 'TQ_ENABLED', 'TQ_LOBE_NO_HOOKS',
               'GIT_CONFIG_GLOBAL', 'GIT_CONFIG_SYSTEM', 'XDG_CONFIG_HOME')
$keep = @{}
foreach ($n in ($homeVars + $clearVars)) { $keep[$n] = [Environment]::GetEnvironmentVariable($n, 'Process') }
try {
    $home2 = Join-Path $pg 'home'
    $env:HOME = $home2; $env:USERPROFILE = $home2; $env:TQ_HOME = Join-Path $home2 '.tentaqles'; $env:TQ_NO_BANNER = '1'
    $env:PATH = "$bin;$env:PATH"
    foreach ($n in $clearVars) { [Environment]::SetEnvironmentVariable($n, $null, 'Process') }

    if (-not (Test-Path (Join-Path $env:TQ_HOME 'config.yaml'))) {
        New-Item -ItemType Directory -Force $pg | Out-Null
        $out = & $tqx lobe sandbox $pg
        if ($LASTEXITCODE -ne 0) { throw "tqx lobe sandbox refused: $out" }
        foreach ($c in @('acme', 'globex')) { git init -q (Join-Path $pg "clients\$c\app") }
        Write-Host "tqx playground built: $out"
    }
    Write-Host ""
    Write-Host "tqx playground: $pg"
    Write-Host "  clients\acme     a test client (Claude Code and Codex)"
    Write-Host "  clients\globex   a second test client (Claude Code)"
    if ($SetupOnly) { return }
    Write-Host "Your real tentaqles home is not used here. Type exit to leave."
    Write-Host "Try:  tqx list      tqx doctor      cd clients\acme\app; tqx env --shell pwsh --json"
    $init = "Set-Location '$pg'; function prompt { '[tqx playground] ' + (Get-Location) + '> ' }"
    & powershell.exe -NoProfile -NoExit -Command $init
}
finally {
    foreach ($n in $keep.Keys) { [Environment]::SetEnvironmentVariable($n, $keep[$n], 'Process') }
    if (-not $SetupOnly) { Write-Host "Left the tqx playground. Your environment is as it was." }
}
