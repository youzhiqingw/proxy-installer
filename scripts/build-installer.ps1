param(
  [string]$SingBoxPath = ""
)

$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent $PSScriptRoot
Set-Location $Root

if (-not $SingBoxPath) {
  $SingBoxPath = @(
    "$env:LOCALAPPDATA\proxy-installer\runtime\sing-box.exe",
    "$env:LOCALAPPDATA\proxy-installer\app\sing-box.exe",
    "$env:LOCALAPPDATA\VPSNodeStarter\sing-box.exe"
  ) | Where-Object { Test-Path -LiteralPath $_ } | Select-Object -First 1
}

wails build -clean
if ($LASTEXITCODE -ne 0) {
  throw "wails build failed with exit code $LASTEXITCODE"
}

if ($SingBoxPath -and (Test-Path -LiteralPath $SingBoxPath)) {
  Copy-Item -LiteralPath $SingBoxPath -Destination "build\bin\sing-box.exe" -Force
} else {
  Write-Warning "sing-box.exe not found; installer will rely on auto-download for node speed testing."
}

New-Item -ItemType Directory -Force "tmp" | Out-Null
$webviewSetup = "tmp\MicrosoftEdgeWebview2Setup.exe"
if (-not (Test-Path -LiteralPath $webviewSetup)) {
  Invoke-WebRequest -Uri "https://go.microsoft.com/fwlink/p/?LinkId=2124703" -OutFile $webviewSetup
}

$makensis = @(
  "$env:ProgramFiles\NSIS\makensis.exe",
  "${env:ProgramFiles(x86)}\NSIS\makensis.exe",
  (Get-Command makensis.exe -ErrorAction SilentlyContinue).Source
) | Where-Object { $_ -and (Test-Path -LiteralPath $_) } | Select-Object -First 1

if (-not $makensis) {
  throw "makensis.exe not found. Install NSIS first, for example: winget install --id NSIS.NSIS -e"
}

& $makensis -DARG_WAILS_AMD64_BINARY="..\..\bin\proxy-installer.exe" "build\windows\installer\project.nsi"
if ($LASTEXITCODE -ne 0) {
  throw "makensis failed with exit code $LASTEXITCODE"
}
Copy-Item "build\bin\proxy-installer-setup-amd64.exe" "build\proxy-installer-setup-amd64.exe" -Force
Get-Item "build\proxy-installer-setup-amd64.exe"
