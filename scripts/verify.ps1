[CmdletBinding()]
param(
    [switch]$Quick,
    [switch]$SkipBrowser,
    [switch]$SkipMinio,
    [switch]$DependencyAudit
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"
[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding($false)
$projectRoot = Split-Path -Parent $PSScriptRoot
$goExecutable = if ($env:SHAREPIPE_GO) { $env:SHAREPIPE_GO } else { (Get-Command go -ErrorAction Stop).Source }
$webRoot = Join-Path $projectRoot "web"
$runID = (Get-Date).ToUniversalTime().ToString("yyyyMMdd-HHmmssZ")
$env:GOMODCACHE = Join-Path $projectRoot "workdir\cache\gomod"
$env:GOCACHE = Join-Path $projectRoot "workdir\cache\gobuild"
$env:GOPATH = Join-Path $projectRoot "workdir\cache\gopath"
$env:npm_config_cache = Join-Path $projectRoot "workdir\cache\npm"
New-Item -ItemType Directory -Path $env:GOMODCACHE, $env:GOCACHE, $env:GOPATH, $env:npm_config_cache, (Join-Path $projectRoot "workdir\intermediate\verify") -Force | Out-Null

npm --prefix $webRoot ci
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
& $goExecutable test ./...
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
& $goExecutable vet ./...
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
npm --prefix $webRoot run check
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
& (Join-Path $PSScriptRoot "build.ps1") -Version "verify" -Output "outputs\sharepipe-verify.exe"
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

if (-not $Quick) {
    & (Join-Path $PSScriptRoot "test-secrets.ps1") -Binary "outputs\sharepipe-verify.exe"
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    & (Join-Path $PSScriptRoot "test-local.ps1") -Binary "outputs\sharepipe-verify.exe" -RunBrowser:(-not $SkipBrowser)
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    if (-not $SkipMinio) {
        & (Join-Path $PSScriptRoot "test-minio.ps1")
        if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    }
}

if ($DependencyAudit) {
    & $goExecutable mod verify
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    & $goExecutable tool govulncheck ./...
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    npm --prefix $webRoot audit --omit=dev --audit-level=high
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
}

$evidence = [ordered]@{
    status = "passed"
    commit = (& git -C $projectRoot rev-parse HEAD).Trim()
    completedAt = (Get-Date).ToUniversalTime().ToString("o")
    quick = [bool]$Quick
    browser = if ($Quick -or $SkipBrowser) { "skipped" } else { "passed" }
    minio = if ($Quick -or $SkipMinio) { "skipped" } else { "passed" }
    dependencyAudit = if ($DependencyAudit) { "passed" } else { "skipped" }
} | ConvertTo-Json
$evidencePath = Join-Path $projectRoot "workdir\intermediate\verify\last-success.json"
[System.IO.File]::WriteAllText($evidencePath, $evidence, (New-Object System.Text.UTF8Encoding($false)))
Write-Host "SharePipe OSS verification passed."
