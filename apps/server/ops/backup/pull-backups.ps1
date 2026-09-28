# ===========================================================================
#
# pull-backups.ps1 - keep a second, local copy of the server backups
#
# Copies the encrypted restic repository from Google Drive to this PC. It
# uses rclone copy, which only adds: files deleted on Drive (by pruning, a
# mistake, or an attacker) stay here. The copy is encrypted; restoring it
# needs the restic password (README.md, "Restore").
#
#   powershell -ExecutionPolicy Bypass -File pull-backups.ps1 [-Target D:\Backups\opensro-restic]
#
# ===========================================================================
param(
	[string]$Remote = "gdrive:opensro-backups",
	[string]$Target = (Join-Path $env:USERPROFILE "Backups\opensro-restic")
)
$ErrorActionPreference = "Stop"

if (-not (Get-Command rclone -ErrorAction SilentlyContinue)) {
	throw "rclone is not installed (winget install Rclone.Rclone)"
}
New-Item -ItemType Directory -Force -Path $Target | Out-Null
& rclone copy $Remote $Target --checksum --transfers 4 --log-level NOTICE
if ($LASTEXITCODE -ne 0) { throw "rclone copy failed with exit code $LASTEXITCODE" }
$stamp = Join-Path $Target "last-pull.txt"
[System.IO.File]::WriteAllText($stamp, (Get-Date).ToUniversalTime().ToString("o"))
Write-Output "Backups copied to $Target"
