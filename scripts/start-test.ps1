[CmdletBinding()]
param(
    [ValidateRange(1024, 65535)][int]$Port = 8080,
    [switch]$Rebuild,
    [switch]$NoBrowser
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"
$projectRoot = Split-Path -Parent $PSScriptRoot
$binary = Join-Path $projectRoot "outputs\sharepipe-windows-amd64.exe"
if ($Rebuild -or -not (Test-Path -LiteralPath $binary -PathType Leaf)) {
    & (Join-Path $PSScriptRoot "build.ps1") -Version "dev"
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
}
$runID = (Get-Date).ToUniversalTime().ToString("yyyyMMdd-HHmmssZ") + "-$PID"
$runRoot = Join-Path $projectRoot "workdir\tmp\quick-start\$runID"
$logRoot = Join-Path $projectRoot "workdir\logs\quick-start\$runID"
New-Item -ItemType Directory -Path $runRoot, $logRoot -Force | Out-Null
$baseURL = "http://127.0.0.1:$Port"
$config = Get-Content -LiteralPath (Join-Path $projectRoot "config\sharepipe.example.json") -Raw -Encoding UTF8 | ConvertFrom-Json
$config.server.listen = "127.0.0.1:$Port"
$config.server.publicBaseURL = $baseURL
$config.server.trustedProxyCIDRs = @()
$config.database.path = Join-Path $runRoot "sharepipe.db"
$config.storage.defaultBackend = "local"
$config.storage.local.root = Join-Path $runRoot "storage"
$config.storage.s3.enabled = $false
$config.backup.directory = Join-Path $runRoot "backups"
$config.logging.directory = $logRoot
$configPath = Join-Path $runRoot "config.json"
[System.IO.File]::WriteAllText($configPath, ($config | ConvertTo-Json -Depth 20), (New-Object System.Text.UTF8Encoding($false)))

$process = Start-Process -FilePath $binary -ArgumentList "-config `"$configPath`"" -WorkingDirectory $projectRoot -RedirectStandardOutput (Join-Path $logRoot "stdout.log") -RedirectStandardError (Join-Path $logRoot "stderr.log") -PassThru
try {
    $deadline = [DateTime]::UtcNow.AddSeconds(30)
    do {
        $process.Refresh()
        if ($process.HasExited) { throw "SharePipe exited during startup." }
        try { $health = Invoke-RestMethod -Uri "$baseURL/api/v1/health" -TimeoutSec 2 } catch { $health = $null }
        if ($null -ne $health -and $health.status -eq "ok") { break }
        Start-Sleep -Milliseconds 250
    } while ([DateTime]::UtcNow -lt $deadline)
    if ($null -eq $health -or $health.status -ne "ok") { throw "SharePipe did not become healthy." }
    if (-not $NoBrowser) { Start-Process $baseURL }
    Write-Host "SharePipe test instance: $baseURL"
    Write-Host "Data: $runRoot"
    Write-Host "Press Enter to stop."
    [void](Read-Host)
} finally {
    $process.Refresh()
    if (-not $process.HasExited) { Stop-Process -Id $process.Id; Wait-Process -Id $process.Id -Timeout 10 -ErrorAction SilentlyContinue }
}
