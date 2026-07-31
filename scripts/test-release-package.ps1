[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$PackageRoot,
    [ValidateRange(1024, 65535)]
    [int]$Port = 18084
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"
[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding($false)
$projectRoot = Split-Path -Parent $PSScriptRoot
$packageSource = (Resolve-Path -LiteralPath $PackageRoot).Path
$runID = (Get-Date).ToUniversalTime().ToString("yyyyMMdd-HHmmssZ") + "-$PID"
$runRoot = Join-Path $projectRoot "workdir\tmp\release-package\$runID"
$packageCopy = Join-Path $runRoot "package"
$logRoot = Join-Path $projectRoot "workdir\logs\release-package\$runID"
New-Item -ItemType Directory -Path $runRoot, $logRoot -Force | Out-Null
Copy-Item -LiteralPath $packageSource -Destination $packageCopy -Recurse

$required = @(
    "config\sharepipe.example.json",
    "outputs\sharepipe-windows-amd64.exe",
    "scripts\initialize-secrets.ps1",
    "scripts\rotate-secrets.ps1",
    "scripts\start.ps1",
    "scripts\lib\SharePipe.PublicSecrets.psm1",
    "Start-SharePipe.cmd",
    "README.md",
    "LICENSE",
    "NOTICE",
    "THIRD_PARTY_NOTICES.md"
)
foreach ($relative in $required) {
    $path = Join-Path $packageCopy $relative
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
        throw "Release package is missing required file: $relative"
    }
}

$launcherCommand = Get-Content -LiteralPath (Join-Path $packageCopy "Start-SharePipe.cmd") -Raw -Encoding UTF8
if ($launcherCommand -notmatch 'scripts\\start\.ps1' -or $launcherCommand -match 'start-test') {
    throw "Start-SharePipe.cmd does not invoke the packaged production launcher."
}

$configPath = Join-Path $packageCopy "config\sharepipe.json"
$config = Get-Content -LiteralPath (Join-Path $packageCopy "config\sharepipe.example.json") -Raw -Encoding UTF8 | ConvertFrom-Json
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

& (Join-Path $packageCopy "scripts\initialize-secrets.ps1") -Force -Quiet
if ($LASTEXITCODE -ne 0) { throw "Packaged DPAPI secret initialization failed." }

$powershellExecutable = if ($PSVersionTable.PSEdition -eq "Core") {
    Join-Path $PSHOME "pwsh.exe"
} else {
    Join-Path $PSHOME "powershell.exe"
}
$arguments = @(
    "-NoProfile", "-ExecutionPolicy", "Bypass",
    "-File", (Join-Path $packageCopy "scripts\start.ps1")
)
$stdoutPath = Join-Path $logRoot "stdout.log"
$stderrPath = Join-Path $logRoot "stderr.log"
$launcher = Start-Process -FilePath $powershellExecutable -ArgumentList $arguments -WorkingDirectory $packageCopy -WindowStyle Hidden -RedirectStandardOutput $stdoutPath -RedirectStandardError $stderrPath -PassThru
$sharePipeProcess = $null
try {
    $deadline = [DateTime]::UtcNow.AddSeconds(30)
    do {
        $launcher.Refresh()
        if ($launcher.HasExited) {
            $stderr = if (Test-Path -LiteralPath $stderrPath) { Get-Content -LiteralPath $stderrPath -Raw -Encoding UTF8 } else { "" }
            throw "Packaged launcher exited during startup with code $($launcher.ExitCode).`n$stderr"
        }
        try { $health = Invoke-RestMethod -Uri "$baseURL/api/v1/health" -TimeoutSec 2 } catch { $health = $null }
        if ($null -ne $health -and $health.status -eq "ok") { break }
        Start-Sleep -Milliseconds 250
    } while ([DateTime]::UtcNow -lt $deadline)
    if ($null -eq $health -or $health.status -ne "ok") { throw "Packaged SharePipe runtime did not become healthy." }
    $binaryPath = [System.IO.Path]::GetFullPath((Join-Path $packageCopy "outputs\sharepipe-windows-amd64.exe"))
    $sharePipeProcess = Get-CimInstance Win32_Process | Where-Object {
        [string]$_.ExecutablePath -eq $binaryPath
    } | Select-Object -First 1
    if ($null -eq $sharePipeProcess) { throw "Could not identify the packaged SharePipe process." }
    Write-Host "Release package layout, DPAPI initialization, launcher, and health check passed."
} finally {
    if ($null -ne $sharePipeProcess) { Stop-Process -Id $sharePipeProcess.ProcessId -Force -ErrorAction SilentlyContinue }
    $launcher.Refresh()
    if (-not $launcher.HasExited) { Stop-Process -Id $launcher.Id -Force -ErrorAction SilentlyContinue }
}
