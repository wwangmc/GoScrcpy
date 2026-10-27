param([string]$DeviceSerial = '', [string]$IOSDeviceSerial = '')
. "$PSScriptRoot\env.ps1"
$previousDevice = $env:SCRCPY_TEST_DEVICE
$previousIOSDevice = $env:MKS_TEST_DEVICE
try {
    $env:SCRCPY_TEST_DEVICE = $DeviceSerial
    $env:MKS_TEST_DEVICE = $IOSDeviceSerial
    go test -count=1 -v ./...
    if ($LASTEXITCODE -ne 0) { throw 'Go 测试失败' }
    go vet ./...
    if ($LASTEXITCODE -ne 0) { throw 'Go 静态检查失败' }
    node --test --test-isolation=none frontend/protocol.test.mjs frontend/drop.test.mjs frontend/shell.test.mjs frontend/ios-settings.test.mjs frontend/files.test.mjs frontend/tools.test.mjs
    if ($LASTEXITCODE -ne 0) { throw '前端协议测试失败' }
} finally {
    $env:SCRCPY_TEST_DEVICE = $previousDevice
    $env:MKS_TEST_DEVICE = $previousIOSDevice
}
