#!/usr/bin/env pwsh
# Build and deploy yavchn to prod.
# Builds the Docker image, stops the existing prod container, and starts a new one.
# Prod container: yavchn on host port 8086, data in volume yavchn-data.

$ErrorActionPreference = 'Stop'
Set-Location $PSScriptRoot

$image     = 'yavchn:latest'
$container = 'yavchn'
$hostPort  = 8086
$volume    = 'yavchn-data'
$dbPath    = '/data/yavchn.db'

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
    -p "${hostPort}:8080" `
    -v "${volume}:/data" `
    -e "YAVCHN_DB_PATH=$dbPath" `
    $image

if ($LASTEXITCODE -ne 0) {
    Write-Host "DOCKER RUN FAILED (exit $LASTEXITCODE)" -ForegroundColor Red
    exit $LASTEXITCODE
}

Write-Host "DEPLOYED: $container on port $hostPort" -ForegroundColor Green
