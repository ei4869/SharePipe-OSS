[CmdletBinding()]
param(
    [string]$Version = "dev",
    [string]$Output = "outputs\sharepipe-windows-amd64.exe"
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"
[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding($false)

$projectRoot = Split-Path -Parent $PSScriptRoot
$goExecutable = if ($env:SHAREPIPE_GO) {
    $env:SHAREPIPE_GO
} else {
    $command = Get-Command go -ErrorAction SilentlyContinue
    if ($null -eq $command) { throw "Go was not found. Install Go 1.26 or set SHAREPIPE_GO." }
    $command.Source
}
$webRoot = Join-Path $projectRoot "web"
$embeddedRoot = Join-Path $projectRoot "cmd\sharepipe\webdist\app"
$outputPath = [System.IO.Path]::GetFullPath((Join-Path $projectRoot $Output))
$rootPrefix = [System.IO.Path]::GetFullPath($projectRoot).TrimEnd([char[]]@('\', '/')) + [System.IO.Path]::DirectorySeparatorChar
if (-not $outputPath.StartsWith($rootPrefix, [System.StringComparison]::OrdinalIgnoreCase)) {
    throw "Build output must stay inside the repository: $outputPath"
}

$env:GOMODCACHE = Join-Path $projectRoot "workdir\cache\gomod"
$env:GOCACHE = Join-Path $projectRoot "workdir\cache\gobuild"
$env:GOPATH = Join-Path $projectRoot "workdir\cache\gopath"
$env:npm_config_cache = Join-Path $projectRoot "workdir\cache\npm"
New-Item -ItemType Directory -Path $env:GOMODCACHE, $env:GOCACHE, $env:GOPATH, $env:npm_config_cache -Force | Out-Null

npm --prefix $webRoot ci
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
npm --prefix $webRoot run check
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
npm --prefix $webRoot run build
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

New-Item -ItemType Directory -Path $embeddedRoot, (Split-Path -Parent $outputPath) -Force | Out-Null
Copy-Item -Path (Join-Path $webRoot "dist\*") -Destination $embeddedRoot -Recurse -Force
$commit = (& git -C $projectRoot rev-parse --short=12 HEAD 2>$null)
if ($LASTEXITCODE -ne 0 -or -not $commit) { $commit = "none" }
$buildDate = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")
$ldflags = "-s -w -X main.version=$Version -X main.commit=$commit -X main.date=$buildDate"
& $goExecutable build -trimpath -ldflags $ldflags -o $outputPath ./cmd/sharepipe
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
Write-Host "Built $outputPath"
