<#
.SYNOPSIS
  Build SailVless.
    .\build.ps1            Go daemon/helper for linux/arm64 -> out\bin
    .\build.ps1 -Cores     also (re)build xray + sing-box from pinned sources (cores\versions.lock)
    .\build.ps1 -Test      run go tests on the host first
    .\build.ps1 -Rpm       also build the RPM with sfdk (Sailfish SDK) -> out\rpm
#>
param(
    [switch]$Cores,
    [switch]$Test,
    [switch]$Rpm,
    [string]$Target = 'SailfishOS-5.1.0.11-aarch64'   # exact name: sfdk tools list
)
$ErrorActionPreference = 'Stop'
$root = $PSScriptRoot
$outBin = Join-Path $root 'out\bin'
New-Item -ItemType Directory -Force $outBin | Out-Null

if (-not (Get-Command go -ErrorAction SilentlyContinue)) { throw 'Go not found. Install it: winget install --id GoLang.Go -e' }

if ($Cores) { & (Join-Path $root 'cores\build-cores.ps1') }

$daemon = Join-Path $root 'daemon'
if (Test-Path (Join-Path $daemon 'go.mod')) {
    Push-Location $daemon
    try {
        if ($Test) {
            go test ./...
            if ($LASTEXITCODE) { throw 'go test failed' }
        }
        $env:GOOS = 'linux'; $env:GOARCH = 'arm64'; $env:CGO_ENABLED = '0'
        foreach ($cmd in Get-ChildItem -Directory (Join-Path $daemon 'cmd')) {
            go build -trimpath -ldflags '-s -w' -o (Join-Path $outBin $cmd.Name) "./cmd/$($cmd.Name)"
            if ($LASTEXITCODE) { throw "go build failed: $($cmd.Name)" }
            Write-Host "built $($cmd.Name)"
        }
    } finally { Pop-Location; Remove-Item Env:GOOS, Env:GOARCH, Env:CGO_ENABLED -ErrorAction SilentlyContinue }
}

foreach ($c in 'xray', 'sing-box') {
    $p = Join-Path $root "cores\bin\$c"
    if (Test-Path $p) { Copy-Item $p $outBin -Force }
}

if ($Rpm) {
    if (-not (Get-Command sfdk -ErrorAction SilentlyContinue)) { throw 'sfdk not found. Install Sailfish SDK 3.13.5 (Docker build engine).' }
    $outRpm = Join-Path $root 'out\rpm'
    New-Item -ItemType Directory -Force $outRpm | Out-Null
    Push-Location $root
    try {
        sfdk config --push target $Target
        sfdk config --push output-dir $outRpm
        sfdk build
        if ($LASTEXITCODE) { throw 'sfdk build failed' }
    } finally { Pop-Location }
    Get-ChildItem $outRpm -Filter *.rpm | ForEach-Object { Write-Host "rpm: $($_.FullName)" }
}
