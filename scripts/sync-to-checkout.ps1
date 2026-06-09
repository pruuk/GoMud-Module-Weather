# Mirror the weather module source into a GoMud checkout's modules/weather/.
# The repo's go.mod is deliberately EXCLUDED: in-checkout modules have no go.mod
# (they are part of the engine module). Run from the repo root.
#
#   pwsh scripts/sync-to-checkout.ps1 -Checkout C:\Users\Calabe Davis\workspace\GoMud
param([Parameter(Mandatory = $true)][string]$Checkout)

$dest = Join-Path $Checkout "modules\weather"
New-Item -ItemType Directory -Force -Path $dest | Out-Null

# /MIR mirrors (so deletions propagate); exclude repo-only dirs/files. go.mod and
# go.sum MUST NOT travel. robocopy returns 0-7 on success (>=8 is an error).
robocopy "." $dest /MIR `
  /XD .git docs scripts .worktrees `
  /XF go.mod go.sum "*.png" "Screenshot*" `
  /NFL /NDL /NJH /NJS | Out-Null
if ($LASTEXITCODE -ge 8) { throw "robocopy failed with code $LASTEXITCODE" }

Write-Host "Synced module source to $dest (go.mod excluded)."
