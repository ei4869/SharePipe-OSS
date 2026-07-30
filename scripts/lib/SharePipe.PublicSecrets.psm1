Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"
Add-Type -AssemblyName System.Security

function Get-AllowedSIDs {
    return @(
        [System.Security.Principal.WindowsIdentity]::GetCurrent().User.Value,
        "S-1-5-18",
        "S-1-5-32-544"
    )
}

function Set-PrivateAcl {
    param([Parameter(Mandatory = $true)][string]$Path, [Parameter(Mandatory = $true)][bool]$Container)

    if ($Container -and -not (Test-Path -LiteralPath $Path -PathType Container)) {
        New-Item -ItemType Directory -Path $Path -Force | Out-Null
    }
    $fullPath = [System.IO.Path]::GetFullPath($Path)
    $icacls = Join-Path $env:SystemRoot "System32\icacls.exe"
    if (-not (Test-Path -LiteralPath $icacls -PathType Leaf)) { throw "icacls.exe was not found." }
    $permission = if ($Container) { "(OI)(CI)F" } else { "F" }
    $arguments = @($fullPath, "/inheritance:r", "/grant:r")
    foreach ($sidValue in Get-AllowedSIDs) {
        $arguments += "*$($sidValue):$permission"
    }
    & $icacls @arguments | Out-Null
    if ($LASTEXITCODE -ne 0) { throw "Could not restrict access to the secret path: $fullPath" }
}

function Assert-PrivateAcl {
    param([Parameter(Mandatory = $true)][string]$Path)

    $allowed = @(Get-AllowedSIDs)
    $acl = Get-Acl -LiteralPath $Path
    if (-not $acl.AreAccessRulesProtected) { throw "Secret path inherits access rules: $Path" }
    foreach ($rule in $acl.Access) {
        if ($rule.AccessControlType -ne "Allow") { continue }
        $sid = $rule.IdentityReference.Translate([System.Security.Principal.SecurityIdentifier]).Value
        if ($sid -notin $allowed) { throw "Secret path grants access to an unexpected identity: $Path" }
    }
}

function New-RandomSecret {
    param([ValidateRange(32, 256)][int]$Bytes = 48)

    $buffer = New-Object byte[] $Bytes
    $random = [System.Security.Cryptography.RandomNumberGenerator]::Create()
    try {
        $random.GetBytes($buffer)
        return [Convert]::ToBase64String($buffer).TrimEnd('=').Replace('+', '-').Replace('/', '_')
    } finally {
        [System.Array]::Clear($buffer, 0, $buffer.Length)
        $random.Dispose()
    }
}

function Get-Entropy {
    $sha = [System.Security.Cryptography.SHA256]::Create()
    try { return $sha.ComputeHash([System.Text.Encoding]::UTF8.GetBytes("SharePipe-OSS|runtime|dpapi-v1")) }
    finally { $sha.Dispose() }
}

function Write-ProtectedSecrets {
    param([Parameter(Mandatory = $true)][string]$Path, [Parameter(Mandatory = $true)][object]$Value)

    $parent = Split-Path -Parent ([System.IO.Path]::GetFullPath($Path))
    Set-PrivateAcl -Path $parent -Container $true
    $plain = $null
    $cipher = $null
    $entropy = $null
    try {
        $plain = [System.Text.Encoding]::UTF8.GetBytes(($Value | ConvertTo-Json -Depth 10 -Compress))
        $entropy = Get-Entropy
        $cipher = [System.Security.Cryptography.ProtectedData]::Protect($plain, $entropy, [System.Security.Cryptography.DataProtectionScope]::CurrentUser)
        $envelope = [ordered]@{
            format = "sharepipe-oss-dpapi-v1"
            protection = "Windows-DPAPI-CurrentUser"
            updatedAt = (Get-Date).ToUniversalTime().ToString("o")
            ciphertext = [Convert]::ToBase64String($cipher)
        } | ConvertTo-Json -Depth 5
        [System.IO.File]::WriteAllText($Path, $envelope, (New-Object System.Text.UTF8Encoding($false)))
        Set-PrivateAcl -Path $Path -Container $false
        Assert-PrivateAcl -Path $Path
    } finally {
        foreach ($buffer in @($plain, $cipher, $entropy)) {
            if ($null -ne $buffer) { [System.Array]::Clear($buffer, 0, $buffer.Length) }
        }
    }
}

function Read-ProtectedSecrets {
    param([Parameter(Mandatory = $true)][string]$Path)

    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) { throw "Protected secret file was not found: $Path" }
    Assert-PrivateAcl -Path $Path
    $envelope = Get-Content -LiteralPath $Path -Raw -Encoding UTF8 | ConvertFrom-Json
    if ([string]$envelope.format -ne "sharepipe-oss-dpapi-v1") { throw "Unsupported protected secret format." }
    $cipher = $null
    $plain = $null
    $entropy = $null
    try {
        $cipher = [Convert]::FromBase64String([string]$envelope.ciphertext)
        $entropy = Get-Entropy
        $plain = [System.Security.Cryptography.ProtectedData]::Unprotect($cipher, $entropy, [System.Security.Cryptography.DataProtectionScope]::CurrentUser)
        return [System.Text.Encoding]::UTF8.GetString($plain) | ConvertFrom-Json
    } finally {
        foreach ($buffer in @($cipher, $plain, $entropy)) {
            if ($null -ne $buffer) { [System.Array]::Clear($buffer, 0, $buffer.Length) }
        }
    }
}

function Restore-ProcessEnvironment {
    param([Parameter(Mandatory = $true)][string]$Name, [AllowNull()][string]$Value)
    if ($null -eq $Value) { Remove-Item "Env:$Name" -ErrorAction SilentlyContinue }
    else { [Environment]::SetEnvironmentVariable($Name, $Value, "Process") }
}

Export-ModuleMember -Function @(
    "Assert-PrivateAcl",
    "New-RandomSecret",
    "Read-ProtectedSecrets",
    "Restore-ProcessEnvironment",
    "Write-ProtectedSecrets"
)
