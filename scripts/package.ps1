param([switch]$SkipBuild)
$ErrorActionPreference = 'Stop'
$packageRoot = Split-Path -Parent $PSScriptRoot
Set-Location -LiteralPath $packageRoot
if (-not $SkipBuild) {
    & "$PSScriptRoot\build.ps1"
    if ($LASTEXITCODE -ne 0) { throw '构建失败' }
}
$version = (Get-Content -LiteralPath 'wails.json' -Raw | ConvertFrom-Json).info.productVersion
if ($version -notmatch '^\d+\.\d+\.\d+$') { throw '无效的发布版本' }
$distribution = Join-Path $packageRoot 'dist'
New-Item -ItemType Directory -Path $distribution -Force | Out-Null
$staging = Join-Path $distribution ('stage-' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $staging | Out-Null
Copy-Item -LiteralPath 'build\bin\GoScrcpy.exe' -Destination (Join-Path $staging 'GoScrcpy.exe')
Copy-Item -LiteralPath 'internal\afc\LICENSE' -Destination (Join-Path $staging 'go-ios-LICENSE.txt')
Copy-Item -LiteralPath 'assets\scrcpy-LICENSE' -Destination (Join-Path $staging 'scrcpy-LICENSE.txt')
Copy-Item -LiteralPath 'README.md' -Destination (Join-Path $staging 'README.md')
@"
GoScrcpy $version / Windows x64
https://github.com/wwangmc/GoScrcpy

Run GoScrcpy.exe on Windows 10/11 x64 with WebView2 Runtime installed.
Android: install ADB separately, add it to PATH or set ADB_PATH.
iOS: install Apple USB drivers and PhoneVideoTweak on the jailbroken phone.
https://github.com/wwangmc/PhoneVideoTweak/releases/latest

ADB, Apple USB drivers and WebView2 Runtime are not bundled.
"@ | Set-Content -LiteralPath (Join-Path $staging 'START-HERE.txt') -Encoding utf8
$revision = git rev-parse HEAD
if ($LASTEXITCODE -ne 0) { throw '无法获取源码提交' }
"Version: $version`nCommit: $revision`nPlatform: windows/amd64" | Set-Content -LiteralPath (Join-Path $staging 'BUILD-INFO.txt') -Encoding utf8
$archive = Join-Path $distribution "GoScrcpy-$version-windows-amd64.zip"
Compress-Archive -Path (Join-Path $staging '*') -DestinationPath $archive -Force
Copy-Item -LiteralPath (Join-Path $staging 'GoScrcpy.exe') -Destination (Join-Path $distribution 'GoScrcpy.exe') -Force
$checksums = foreach ($file in @($archive, (Join-Path $distribution 'GoScrcpy.exe'))) {
    $hash = Get-FileHash -LiteralPath $file -Algorithm SHA256
    '{0}  {1}' -f $hash.Hash.ToLowerInvariant(), (Split-Path -Leaf $file)
}
$checksums | Set-Content -LiteralPath (Join-Path $distribution 'SHA256SUMS.txt') -Encoding ascii
Write-Host "Release 文件：$distribution"
