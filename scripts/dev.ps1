. "$PSScriptRoot\env.ps1"
$wailsCLI = Join-Path $env:GOPATH 'bin\wails.exe'
if (-not (Test-Path -LiteralPath $wailsCLI)) {
    go install github.com/wailsapp/wails/v2/cmd/wails@v2.10.2
    if ($LASTEXITCODE -ne 0) { throw 'Wails CLI 安装失败' }
}
& $wailsCLI dev
if ($LASTEXITCODE -ne 0) { throw 'Wails 开发模式启动失败' }
