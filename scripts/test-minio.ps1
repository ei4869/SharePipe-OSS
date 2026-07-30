[CmdletBinding()]
param([ValidateRange(1024, 65535)][int]$Port = 19000)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"
$projectRoot = Split-Path -Parent $PSScriptRoot
$goExecutable = if ($env:SHAREPIPE_GO) { $env:SHAREPIPE_GO } else { (Get-Command go -ErrorAction Stop).Source }
$toolRoot = Join-Path $projectRoot "workdir\tools\minio"
$binary = Join-Path $toolRoot "minio.exe"
New-Item -ItemType Directory -Path $toolRoot -Force | Out-Null
if (-not (Test-Path -LiteralPath $binary -PathType Leaf)) {
    $downloadRoot = Join-Path $projectRoot "workdir\tmp\minio-download"
    New-Item -ItemType Directory -Path $downloadRoot -Force | Out-Null
    $download = Join-Path $downloadRoot "minio.exe"
    $checksum = Join-Path $downloadRoot "minio.exe.sha256sum"
    $base = "https://dl.min.io/server/minio/release/windows-amd64"
    Invoke-WebRequest -UseBasicParsing -Uri "$base/minio.exe" -OutFile $download
    Invoke-WebRequest -UseBasicParsing -Uri "$base/minio.exe.sha256sum" -OutFile $checksum
    $expected = ((Get-Content -LiteralPath $checksum -Encoding UTF8 | Select-Object -First 1) -split '\s+')[0].ToLowerInvariant()
    $actual = (Get-FileHash -LiteralPath $download -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($actual -ne $expected) { throw "MinIO download checksum mismatch." }
    Move-Item -LiteralPath $download -Destination $binary
}
$runID = (Get-Date).ToUniversalTime().ToString("yyyyMMdd-HHmmssZ") + "-$PID"
$dataRoot = Join-Path $projectRoot "workdir\minio\$runID"
$logRoot = Join-Path $projectRoot "workdir\logs\minio\$runID"
New-Item -ItemType Directory -Path $dataRoot, $logRoot -Force | Out-Null
$access = "sharepipe-test"
$secret = "sharepipe-test-$([guid]::NewGuid().ToString('N'))"
$oldUser = [Environment]::GetEnvironmentVariable("MINIO_ROOT_USER", "Process")
$oldPassword = [Environment]::GetEnvironmentVariable("MINIO_ROOT_PASSWORD", "Process")
try {
    $env:MINIO_ROOT_USER = $access
    $env:MINIO_ROOT_PASSWORD = $secret
    $process = Start-Process -FilePath $binary -ArgumentList @("server", $dataRoot, "--address", "127.0.0.1:$Port", "--console-address", "127.0.0.1:$($Port + 1)") -WorkingDirectory $projectRoot -RedirectStandardOutput (Join-Path $logRoot "stdout.log") -RedirectStandardError (Join-Path $logRoot "stderr.log") -PassThru
} finally {
    [Environment]::SetEnvironmentVariable("MINIO_ROOT_USER", $oldUser, "Process")
    [Environment]::SetEnvironmentVariable("MINIO_ROOT_PASSWORD", $oldPassword, "Process")
}
try {
    $deadline = [DateTime]::UtcNow.AddSeconds(30)
    do {
        $process.Refresh()
        if ($process.HasExited) { throw "MinIO exited during startup." }
        try { $response = Invoke-WebRequest -UseBasicParsing -Uri "http://127.0.0.1:$Port/minio/health/live" -TimeoutSec 2 } catch { $response = $null }
        if ($null -ne $response -and $response.StatusCode -eq 200) { break }
        Start-Sleep -Milliseconds 250
    } while ([DateTime]::UtcNow -lt $deadline)
    if ($null -eq $response -or $response.StatusCode -ne 200) { throw "MinIO did not become healthy." }
    $oldEndpoint = [Environment]::GetEnvironmentVariable("SHAREPIPE_MINIO_ENDPOINT", "Process")
    $oldAccess = [Environment]::GetEnvironmentVariable("SHAREPIPE_MINIO_ACCESS_KEY", "Process")
    $oldSecret = [Environment]::GetEnvironmentVariable("SHAREPIPE_MINIO_SECRET_KEY", "Process")
    $oldBucket = [Environment]::GetEnvironmentVariable("SHAREPIPE_MINIO_BUCKET", "Process")
    try {
        $env:SHAREPIPE_MINIO_ENDPOINT = "http://127.0.0.1:$Port"
        $env:SHAREPIPE_MINIO_ACCESS_KEY = $access
        $env:SHAREPIPE_MINIO_SECRET_KEY = $secret
        $env:SHAREPIPE_MINIO_BUCKET = "sharepipe-contract"
        & $goExecutable test ./internal/storage -run '^TestS3MinIOContract$' -count=1 -v
        if ($LASTEXITCODE -ne 0) { throw "MinIO storage contract failed." }
    } finally {
        [Environment]::SetEnvironmentVariable("SHAREPIPE_MINIO_ENDPOINT", $oldEndpoint, "Process")
        [Environment]::SetEnvironmentVariable("SHAREPIPE_MINIO_ACCESS_KEY", $oldAccess, "Process")
        [Environment]::SetEnvironmentVariable("SHAREPIPE_MINIO_SECRET_KEY", $oldSecret, "Process")
        [Environment]::SetEnvironmentVariable("SHAREPIPE_MINIO_BUCKET", $oldBucket, "Process")
    }
    Write-Host "MinIO storage contract passed."
} finally {
    $process.Refresh()
    if (-not $process.HasExited) { Stop-Process -Id $process.Id; Wait-Process -Id $process.Id -Timeout 10 -ErrorAction SilentlyContinue }
    $secret = $null
}
