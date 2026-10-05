# Deletes YAVCHN database backups older than the privacy policy allows.
#
# The policy at /privacy promises that a backup is kept for at most 30
# days. Each backup is a folder under the backup root. Its date is the one
# in its name, as in prod-2026-10-04-before-accounts, or, for a folder
# whose name has no date, the time the folder was created.
#
#   ./prune-backups.ps1            delete backups older than 30 days
#   ./prune-backups.ps1 -WhatIf    list what would be deleted
#
# Run it daily from Task Scheduler, so a backup goes on time even when
# nothing is deployed.

[CmdletBinding(SupportsShouldProcess)]
param(
    [string]$Root = (Join-Path $env:USERPROFILE 'yavchn-backups'),
    [int]$Days = 30
)

$ErrorActionPreference = 'Stop'
if (-not (Test-Path -LiteralPath $Root -PathType Container)) {
    Write-Host "No backup folder at $Root."
    exit 0
}

$cutoff = (Get-Date).Date.AddDays(-$Days)
foreach ($backup in Get-ChildItem -LiteralPath $Root -Directory) {
    $taken = $backup.CreationTime
    if ($backup.Name -match '(\d{4}-\d{2}-\d{2})') {
        $taken = [datetime]::ParseExact($Matches[1], 'yyyy-MM-dd', [cultureinfo]::InvariantCulture)
    }
    if ($taken -lt $cutoff -and $PSCmdlet.ShouldProcess($backup.FullName, "Delete backup from $($taken.ToString('yyyy-MM-dd'))")) {
        Remove-Item -LiteralPath $backup.FullName -Recurse -Force
        Write-Host "Deleted $($backup.Name), taken $($taken.ToString('yyyy-MM-dd'))."
    }
}
