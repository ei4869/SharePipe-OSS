[CmdletBinding()]
param(
    [string]$Binary = "outputs\sharepipe-verify.exe",
    [ValidateRange(1024, 65535)][int]$Port = 18083
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"
$projectRoot = Split-Path -Parent $PSScriptRoot
$runID = (Get-Date).ToUniversalTime().ToString("yyyyMMdd-HHmmssZ") + "-$PID"
$runRelative = "workdir\tmp\dpapi-acceptance\$runID"
$runRoot = Join-Path $projectRoot $runRelative
$logRoot = Join-Path $projectRoot "workdir\logs\dpapi-acceptance\$runID"
$secretRelative = "$runRelative\runtime.dpapi.json"
$secretPath = Join-Path $projectRoot $secretRelative
$configRelative = "$runRelative\config.json"
$configPath = Join-Path $projectRoot $configRelative
$binaryPath = Join-Path $projectRoot $Binary
New-Item -ItemType Directory -Path $runRoot, $logRoot -Force | Out-Null

& (Join-Path $PSScriptRoot "initialize-secrets.ps1") -Force -Quiet -Binary $Binary -SecretPath $secretRelative
if ($LASTEXITCODE -ne 0) { throw "DPAPI secret initialization failed." }
& (Join-Path $PSScriptRoot "rotate-secrets.ps1") -Mode session -Quiet -Binary $Binary -SecretPath $secretRelative
if ($LASTEXITCODE -ne 0) { throw "DPAPI session rotation failed." }
& (Join-Path $PSScriptRoot "rotate-secrets.ps1") -Mode admin -Quiet -Binary $Binary -SecretPath $secretRelative
if ($LASTEXITCODE -ne 0) { throw "DPAPI administrator rotation failed." }

$config = Get-Content -LiteralPath (Join-Path $projectRoot "config\sharepipe.example.json") -Raw -Encoding UTF8 | ConvertFrom-Json
$baseURL = "http://127.0.0.1:$Port"
$config.server.listen = "127.0.0.1:$Port"
$config.server.publicBaseURL = $baseURL
$config.server.trustedProxyCIDRs = @()
$config.database.path = Join-Path $runRoot "sharepipe.db"
$config.storage.defaultBackend = "local"
$config.storage.local.root = Join-Path $runRoot "storage"
$config.storage.s3.enabled = $false
$config.backup.directory = Join-Path $runRoot "backups"
$config.logging.directory = $logRoot
[System.IO.File]::WriteAllText($configPath, ($config | ConvertTo-Json -Depth 20), (New-Object System.Text.UTF8Encoding($false)))

$arguments = @(
    "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", (Join-Path $PSScriptRoot "start.ps1"),
    "-Config", $configRelative, "-Binary", $Binary, "-SecretPath", $secretRelative
)
$launcher = Start-Process -FilePath "powershell.exe" -ArgumentList $arguments -WorkingDirectory $projectRoot -RedirectStandardOutput (Join-Path $logRoot "stdout.log") -RedirectStandardError (Join-Path $logRoot "stderr.log") -PassThru
$sharePipeProcess = $null
try {
    $deadline = [DateTime]::UtcNow.AddSeconds(30)
    do {
        $launcher.Refresh()
        if ($launcher.HasExited) { throw "DPAPI-protected launcher exited during startup." }
        try { $health = Invoke-RestMethod -Uri "$baseURL/api/v1/health" -TimeoutSec 2 } catch { $health = $null }
        if ($null -ne $health -and $health.status -eq "ok") { break }
        Start-Sleep -Milliseconds 250
    } while ([DateTime]::UtcNow -lt $deadline)
    if ($null -eq $health -or $health.status -ne "ok") { throw "DPAPI-protected runtime did not become healthy." }
    $sharePipeProcess = Get-CimInstance Win32_Process | Where-Object {
        [string]$_.ExecutablePath -eq [System.IO.Path]::GetFullPath($binaryPath)
    } | Select-Object -First 1
    if ($null -eq $sharePipeProcess) { throw "Could not identify the DPAPI-protected SharePipe process." }
    Write-Host "DPAPI initialization, rotation, and protected startup passed."
} finally {
    if ($null -ne $sharePipeProcess) { Stop-Process -Id $sharePipeProcess.ProcessId -Force -ErrorAction SilentlyContinue }
    $launcher.Refresh()
    if (-not $launcher.HasExited) { Stop-Process -Id $launcher.Id -Force -ErrorAction SilentlyContinue }
}
