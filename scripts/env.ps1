$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
$env:GOPATH = Join-Path $projectRoot '.cache\go'
$env:GOCACHE = Join-Path $projectRoot '.cache\go-build'
$env:GOTELEMETRY = 'off'
$env:GOTOOLCHAIN = 'local'
Set-Location -LiteralPath $projectRoot
