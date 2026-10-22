param([Parameter(Mandatory = $true)][string]$DeviceSerial)
. "$PSScriptRoot\env.ps1"
$executable = Join-Path $projectRoot 'build\bin\GoScrcpy.exe'
if (-not (Test-Path -LiteralPath $executable)) { throw '请先执行 scripts\build.ps1' }
$runId = [guid]::NewGuid().ToString('N')
$outputLog = Join-Path $projectRoot ".cache\native-smoke-$runId.log"
$errorLog = Join-Path $projectRoot ".cache\native-smoke-$runId-error.log"
New-Item -ItemType Directory -Path (Join-Path $projectRoot '.cache') -Force | Out-Null
$testProcess = Start-Process -FilePath $executable -ArgumentList @('--smoke-test', $DeviceSerial) -WorkingDirectory (Split-Path -Parent $executable) -WindowStyle Hidden -RedirectStandardOutput $outputLog -RedirectStandardError $errorLog -PassThru
if (-not $testProcess.WaitForExit(55000)) {
    Stop-Process -Id $testProcess.Id -ErrorAction SilentlyContinue
    throw "原生自检超时。日志：$errorLog"
}
$report = (Get-Content -LiteralPath $outputLog -Raw -ErrorAction SilentlyContinue) + (Get-Content -LiteralPath $errorLog -Raw -ErrorAction SilentlyContinue)
Write-Host $report
if ($report -notmatch 'NATIVE_SMOKE_PASS:') { throw "原生自检失败。日志：$errorLog" }
