#!/usr/bin/env pwsh
# Build and deploy the production or beta instance.
param(
    [ValidateSet('Production', 'Beta')]
    [string]$Target = 'Production',
    [string]$AuthEnvFile = ''
)

$ErrorActionPreference = 'Stop'
Set-Location $PSScriptRoot

$branch = git branch --show-current
if ($LASTEXITCODE -ne 0) { throw 'Cannot determine the current branch.' }
if ($Target -eq 'Production' -and $branch -ne 'main') {
    throw 'Production deployments require main. Use -Target Beta for feature branches.'
}

if ($Target -eq 'Beta') {
    $image     = 'yavchn:beta'
    $container = 'yavchn-beta'
    $hostPort  = 8087
    $volume    = 'yavchn-beta-data'
    $portBinding = "127.0.0.1:${hostPort}:8080"
    $publicOrigin = 'https://beta.yavchn.com'
} else {
    $image     = 'yavchn:latest'
    $container = 'yavchn'
    $hostPort  = 8086
    $volume    = 'yavchn-data'
    $portBinding = "${hostPort}:8080"
    $publicOrigin = 'https://yavchn.com'
}
$dbPath    = '/data/yavchn.db'
$authArgs = @()
if ($AuthEnvFile -ne '') {
    $authPath = (Resolve-Path -LiteralPath $AuthEnvFile).Path
    $authArgs = @('--env-file', $authPath)
}

Write-Host "Building Docker image $image ..."
docker build -t $image .
if ($LASTEXITCODE -ne 0) {
    Write-Host "DOCKER BUILD FAILED (exit $LASTEXITCODE)" -ForegroundColor Red
    exit $LASTEXITCODE
}
Write-Host "IMAGE BUILT" -ForegroundColor Green

Write-Host "Stopping/removing existing container $container (if running) ..."
docker rm -f $container 2>$null

Write-Host "Starting $container on port $hostPort ..."
docker run -d `
    --name $container `
    --restart unless-stopped `
    -p $portBinding `
    -v "${volume}:/data" `
    -e "YAVCHN_DB_PATH=$dbPath" `
    @authArgs `
    -e "YAVCHN_PUBLIC_ORIGIN=$publicOrigin" `
    $image

if ($LASTEXITCODE -ne 0) {
    Write-Host "DOCKER RUN FAILED (exit $LASTEXITCODE)" -ForegroundColor Red
    exit $LASTEXITCODE
}

Write-Host "DEPLOYED: $container on port $hostPort" -ForegroundColor Green
