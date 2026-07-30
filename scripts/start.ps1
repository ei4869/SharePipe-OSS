[CmdletBinding()]
param(
    [string]$Config = "config\sharepipe.json",
    [string]$Binary = "outputs\sharepipe-windows-amd64.exe",
    [string]$SecretPath = "workdir\secrets\sharepipe-oss-runtime.dpapi.json"
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"
$projectRoot = Split-Path -Parent $PSScriptRoot
$binaryPath = Join-Path $projectRoot $Binary
$configPath = Join-Path $projectRoot $Config
$secretPath = Join-Path $projectRoot $SecretPath
Import-Module (Join-Path $PSScriptRoot "lib\SharePipe.PublicSecrets.psm1") -Force
foreach ($required in @($binaryPath, $configPath, $secretPath)) {
    if (-not (Test-Path -LiteralPath $required -PathType Leaf)) { throw "Required file was not found: $required" }
}
$secrets = Read-ProtectedSecrets -Path $secretPath
$names = @("SHAREPIPE_ADMIN_PASSWORD_HASH", "SHAREPIPE_SESSION_SECRET", "SHAREPIPE_STORAGE_MASTER_KEY")
$previous = @{}
foreach ($name in $names) { $previous[$name] = [Environment]::GetEnvironmentVariable($name, "Process") }
try {
    $env:SHAREPIPE_ADMIN_PASSWORD_HASH = [string]$secrets.administratorPasswordHash
    $env:SHAREPIPE_SESSION_SECRET = [string]$secrets.sessionSecret
    $env:SHAREPIPE_STORAGE_MASTER_KEY = [string]$secrets.storageMasterKey
    & $binaryPath -config $configPath
    exit $LASTEXITCODE
} finally {
    foreach ($name in $names) { Restore-ProcessEnvironment -Name $name -Value $previous[$name] }
    $secrets = $null
}
