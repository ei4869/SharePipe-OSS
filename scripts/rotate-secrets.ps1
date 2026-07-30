[CmdletBinding()]
param(
    [ValidateSet("admin", "session")][string]$Mode = "session",
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
$current = Read-ProtectedSecrets -Path $secretPath
$sessionSecret = New-RandomSecret -Bytes 48
$adminPassword = [string]$current.administratorPassword
$adminHash = [string]$current.administratorPasswordHash
if ($Mode -eq "admin") {
    if (-not (Test-Path -LiteralPath $binary -PathType Leaf)) { throw "SharePipe binary was not found: $binary" }
    $adminPassword = New-RandomSecret -Bytes 36
    $previous = [Environment]::GetEnvironmentVariable("SHAREPIPE_PASSWORD_TO_HASH", "Process")
    try {
        $env:SHAREPIPE_PASSWORD_TO_HASH = $adminPassword
        $adminHash = (& $binary hash-password).Trim()
        if ($LASTEXITCODE -ne 0 -or -not $adminHash.StartsWith('$argon2id$')) { throw "SharePipe did not produce a valid administrator password hash." }
    } finally {
        Restore-ProcessEnvironment -Name "SHAREPIPE_PASSWORD_TO_HASH" -Value $previous
    }
}
Write-ProtectedSecrets -Path $secretPath -Value ([ordered]@{
    version = 1
    rotatedAt = (Get-Date).ToUniversalTime().ToString("o")
    administratorPassword = $adminPassword
    administratorPasswordHash = $adminHash
    sessionSecret = $sessionSecret
    storageMasterKey = [string]$current.storageMasterKey
})
if (-not $Quiet) {
    Write-Warning "Restart SharePipe before accepting traffic; existing signed sessions will be invalid."
    if ($Mode -eq "admin") { Write-Warning "New administrator bootstrap password (displayed once): $adminPassword" }
}
$current = $null
$sessionSecret = $null
$adminPassword = $null
$adminHash = $null
