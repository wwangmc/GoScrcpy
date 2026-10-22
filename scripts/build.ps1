param([switch]$Portable)
. "$PSScriptRoot\env.ps1"
$wailsCLI = Join-Path $env:GOPATH 'bin\wails.exe'
if (-not (Test-Path -LiteralPath $wailsCLI)) {
    go install github.com/wailsapp/wails/v2/cmd/wails@v2.10.2
    if ($LASTEXITCODE -ne 0) { throw 'Wails CLI 安装失败' }
}
& $wailsCLI build -o GoScrcpy.exe
if ($LASTEXITCODE -ne 0) { throw 'Wails 构建失败' }
Copy-Item -LiteralPath 'internal\afc\LICENSE' -Destination 'build\bin\go-ios-LICENSE.txt' -Force
if ($Portable) {
    $adbExe = if ($env:ADB_PATH) { $env:ADB_PATH } else { (Get-Command adb -ErrorAction Stop).Source }
    $adbDirectory = Split-Path -Parent $adbExe
    Copy-Item -LiteralPath $adbExe -Destination 'build\bin\adb.exe' -Force
    foreach ($library in @('AdbWinApi.dll', 'AdbWinUsbApi.dll')) {
        $source = Join-Path $adbDirectory $library
        if (-not (Test-Path -LiteralPath $source)) { throw "缺少 ADB 运行库：$source" }
        Copy-Item -LiteralPath $source -Destination (Join-Path 'build\bin' $library) -Force
    }
    Copy-Item -LiteralPath 'assets\scrcpy-LICENSE' -Destination 'build\bin\scrcpy-LICENSE.txt' -Force
    Write-Host '已复制本机 ADB 及运行库。分发时请另行核对所使用 platform-tools 的许可要求。'
}
Write-Host "构建完成：$projectRoot\build\bin\GoScrcpy.exe"
