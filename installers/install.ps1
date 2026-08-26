$ErrorActionPreference = 'Stop'
$repo = 'tentaqles/tentaqles'
$binDir = if ($env:TQ_BIN_DIR) { $env:TQ_BIN_DIR } else { Join-Path $env:LOCALAPPDATA 'tentaqles\bin' }
$arch = if ([Environment]::Is64BitOperatingSystem -and $env:PROCESSOR_ARCHITECTURE -eq 'ARM64') { 'arm64' } else { 'amd64' }
$rel = Invoke-RestMethod "https://api.github.com/repos/$repo/releases/latest"
$ver = $rel.tag_name.TrimStart('v')
$url = "https://github.com/$repo/releases/download/$($rel.tag_name)/tq_${ver}_windows_${arch}.zip"
New-Item -ItemType Directory -Force $binDir | Out-Null
$zip = Join-Path $env:TEMP "tq.zip"
Invoke-WebRequest $url -OutFile $zip
Expand-Archive $zip -DestinationPath $binDir -Force
Remove-Item $zip
$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if (($userPath -split ';') -notcontains $binDir) {
  [Environment]::SetEnvironmentVariable('Path', "$userPath;$binDir", 'User')
  Write-Host "Added $binDir to your user PATH (open a new terminal)."
}
Write-Host "Installed tq $ver to $binDir\tq.exe"
Write-Host 'Next: tq init <base-folder>'
