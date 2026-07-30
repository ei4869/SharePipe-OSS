[CmdletBinding()]
param(
    [ValidateRange(1024, 65535)][int]$Port = 18082,
    [string]$Binary = "outputs\sharepipe-verify.exe",
    [switch]$RunBrowser
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"
$projectRoot = Split-Path -Parent $PSScriptRoot
$binaryPath = Join-Path $projectRoot $Binary
if (-not (Test-Path -LiteralPath $binaryPath -PathType Leaf)) { throw "Verification binary was not found: $binaryPath" }
$runID = (Get-Date).ToUniversalTime().ToString("yyyyMMdd-HHmmssZ") + "-$PID"
$runRoot = Join-Path $projectRoot "workdir\tmp\local-acceptance\$runID"
$logRoot = Join-Path $projectRoot "workdir\logs\local-acceptance\$runID"
New-Item -ItemType Directory -Path $runRoot, $logRoot -Force | Out-Null
$baseURL = "http://127.0.0.1:$Port"
$config = Get-Content -LiteralPath (Join-Path $projectRoot "config\sharepipe.example.json") -Raw -Encoding UTF8 | ConvertFrom-Json
$config.server.listen = "127.0.0.1:$Port"
$config.server.publicBaseURL = $baseURL
$config.server.adminAccess = "local"
$config.server.trustedProxyCIDRs = @()
$config.database.path = Join-Path $runRoot "sharepipe.db"
$config.storage.defaultBackend = "local"
$config.storage.local.root = Join-Path $runRoot "storage"
$config.storage.s3.enabled = $false
$config.backup.directory = Join-Path $runRoot "backups"
$config.logging.directory = $logRoot
$configPath = Join-Path $runRoot "config.json"
[System.IO.File]::WriteAllText($configPath, ($config | ConvertTo-Json -Depth 20), (New-Object System.Text.UTF8Encoding($false)))
$process = Start-Process -FilePath $binaryPath -ArgumentList "-config `"$configPath`"" -WorkingDirectory $projectRoot -RedirectStandardOutput (Join-Path $logRoot "stdout.log") -RedirectStandardError (Join-Path $logRoot "stderr.log") -PassThru
try {
    $deadline = [DateTime]::UtcNow.AddSeconds(30)
    do {
        $process.Refresh()
        if ($process.HasExited) { throw "SharePipe exited during local acceptance." }
        try { $health = Invoke-RestMethod -Uri "$baseURL/api/v1/health" -TimeoutSec 2 } catch { $health = $null }
        if ($null -ne $health -and $health.status -eq "ok") { break }
        Start-Sleep -Milliseconds 250
    } while ([DateTime]::UtcNow -lt $deadline)
    if ($null -eq $health -or $health.status -ne "ok") { throw "Local acceptance health check failed." }
    $session = Invoke-RestMethod -Uri "$baseURL/api/v1/admin/session" -TimeoutSec 10
    if (-not [bool]$session.local -or -not [bool]$session.authenticated) { throw "Loopback administrator trust failed." }
    if ($RunBrowser) {
        $previousBase = [Environment]::GetEnvironmentVariable("SHAREPIPE_E2E_BASE_URL", "Process")
        $previousAdmin = [Environment]::GetEnvironmentVariable("SHAREPIPE_E2E_ADMIN_BASE_URL", "Process")
        $previousTasks = [Environment]::GetEnvironmentVariable("SHAREPIPE_E2E_UPLOAD_TASKS", "Process")
        try {
            $env:SHAREPIPE_E2E_BASE_URL = $baseURL
            $env:SHAREPIPE_E2E_ADMIN_BASE_URL = $baseURL
            $env:SHAREPIPE_E2E_UPLOAD_TASKS = "1"
            Push-Location (Join-Path $projectRoot "web")
            try {
                npx --no-install playwright test e2e/admin-local.spec.ts e2e/upload-task-controls.spec.ts
                if ($LASTEXITCODE -ne 0) { throw "Browser acceptance failed." }
            } finally { Pop-Location }
        } finally {
            [Environment]::SetEnvironmentVariable("SHAREPIPE_E2E_BASE_URL", $previousBase, "Process")
            [Environment]::SetEnvironmentVariable("SHAREPIPE_E2E_ADMIN_BASE_URL", $previousAdmin, "Process")
            [Environment]::SetEnvironmentVariable("SHAREPIPE_E2E_UPLOAD_TASKS", $previousTasks, "Process")
        }
    }
    Write-Host "Local storage acceptance passed."
} finally {
    $process.Refresh()
    if (-not $process.HasExited) { Stop-Process -Id $process.Id; Wait-Process -Id $process.Id -Timeout 10 -ErrorAction SilentlyContinue }
}
