$ErrorActionPreference = 'Stop'
$repo = 'tentaqles/tentaqles'
$binDir = if ($env:TQ_BIN_DIR) { $env:TQ_BIN_DIR } else { Join-Path $env:LOCALAPPDATA 'tentaqles\bin' }
$arch = if ([Environment]::Is64BitOperatingSystem -and $env:PROCESSOR_ARCHITECTURE -eq 'ARM64') { 'arm64' } else { 'amd64' }
# /releases/latest ignores pre-releases; fall back to the newest release of any kind.
try { $rel = Invoke-RestMethod "https://api.github.com/repos/$repo/releases/latest" }
catch { $rel = (Invoke-RestMethod "https://api.github.com/repos/$repo/releases?per_page=1")[0] }
if (-not $rel) { throw "no releases found for $repo" }
$ver = $rel.tag_name.TrimStart('v')
$archive = "tq_${ver}_windows_${arch}.zip"
$base = "https://github.com/$repo/releases/download/$($rel.tag_name)"
New-Item -ItemType Directory -Force $binDir | Out-Null
$zip = Join-Path $env:TEMP "tq.zip"
Invoke-WebRequest "$base/$archive" -OutFile $zip
# Refuse an archive whose SHA-256 is not the one the release lists.
$sumsFile = Join-Path $env:TEMP "tq-checksums.txt"
Invoke-WebRequest "$base/checksums.txt" -OutFile $sumsFile
$want = (Get-Content $sumsFile | Where-Object { ($_.Trim() -split '\s+')[1] -eq $archive } | ForEach-Object { ($_.Trim() -split '\s+')[0] } | Select-Object -First 1)
Remove-Item $sumsFile
$got = (Get-FileHash $zip -Algorithm SHA256).Hash.ToLower()
if (-not $want -or $got -ne $want.ToLower()) {
  Remove-Item $zip
  throw "checksum mismatch for $archive (expected $want, got $got); not installing"
}
# With a logged-in gh, also check GitHub's build provenance: the archive was built by this
# repo's release workflow. Without gh the checksum above is the only check.
if (Get-Command gh -ErrorAction SilentlyContinue) {
  # gh writes to stderr, which 'Stop' turns into a terminating error in Windows PowerShell
  $ErrorActionPreference = 'Continue'
  & gh auth status *> $null; $loggedIn = ($LASTEXITCODE -eq 0)
  if ($loggedIn) { & gh attestation verify $zip --repo $repo --signer-workflow "$repo/.github/workflows/release.yml" *> $null; $verified = ($LASTEXITCODE -eq 0) }
  $ErrorActionPreference = 'Stop'
  if ($loggedIn) {
    if (-not $verified) {
      Remove-Item $zip
      throw "build provenance check failed for $archive (gh attestation verify); not installing"
    }
    Write-Host "Verified build provenance for $archive"
  }
}
Expand-Archive $zip -DestinationPath $binDir -Force
Remove-Item $zip
$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if (($userPath -split ';') -notcontains $binDir) {
  [Environment]::SetEnvironmentVariable('Path', "$userPath;$binDir", 'User')
  Write-Host "Added $binDir to your user PATH (open a new terminal)."
}
Write-Host "Installed tq $ver to $binDir\tq.exe"
Write-Host 'Next: tq init <base-folder>'
