[CmdletBinding()]
param(
    [switch]$Force,
    [switch]$Quiet,
    [string]$Binary = "outputs\sharepipe-windows-amd64.exe",
    [string]$SecretPath = "workdir\secrets\sharepipe-oss-runtime.dpapi.json"
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"
$projectRoot = Split-Path -Parent $PSScriptRoot
$binary = Join-Path $projectRoot $Binary
$secretPath = Join-Path $projectRoot $SecretPath
Import-Module (Join-Path $PSScriptRoot "lib\SharePipe.PublicSecrets.psm1") -Force

if (-not (Test-Path -LiteralPath $binary -PathType Leaf)) { throw "Build SharePipe before initializing secrets: $binary" }
if ((Test-Path -LiteralPath $secretPath -PathType Leaf) -and -not $Force) { throw "Secrets already exist. Use rotate-secrets.ps1, or pass -Force only when intentionally replacing an unused setup." }

$adminPassword = New-RandomSecret -Bytes 36
$sessionSecret = New-RandomSecret -Bytes 48
$storageBytes = New-Object byte[] 32
$random = [System.Security.Cryptography.RandomNumberGenerator]::Create()
try {
    $random.GetBytes($storageBytes)
    $storageKey = [Convert]::ToBase64String($storageBytes)
} finally {
    [System.Array]::Clear($storageBytes, 0, $storageBytes.Length)
    $random.Dispose()
}
$previous = [Environment]::GetEnvironmentVariable("SHAREPIPE_PASSWORD_TO_HASH", "Process")
try {
    $env:SHAREPIPE_PASSWORD_TO_HASH = $adminPassword
    $adminHash = (& $binary hash-password).Trim()
    if ($LASTEXITCODE -ne 0 -or -not $adminHash.StartsWith('$argon2id$')) { throw "SharePipe did not produce a valid administrator password hash." }
} finally {
    Restore-ProcessEnvironment -Name "SHAREPIPE_PASSWORD_TO_HASH" -Value $previous
}

Write-ProtectedSecrets -Path $secretPath -Value ([ordered]@{
    version = 1
    rotatedAt = (Get-Date).ToUniversalTime().ToString("o")
    administratorPassword = $adminPassword
    administratorPasswordHash = $adminHash
    sessionSecret = $sessionSecret
    storageMasterKey = $storageKey
})
$check = Read-ProtectedSecrets -Path $secretPath
if ([string]$check.sessionSecret -cne $sessionSecret -or [string]$check.storageMasterKey -cne $storageKey) { throw "DPAPI secret round-trip validation failed." }
if (-not $Quiet) {
    Write-Warning "Administrator bootstrap password (displayed once): $adminPassword"
    Write-Warning "Store it in a password manager. Do not paste it into chat, logs, screenshots, or shell history."
}
$adminPassword = $null
$adminHash = $null
$sessionSecret = $null
$storageKey = $null
$check = $null
