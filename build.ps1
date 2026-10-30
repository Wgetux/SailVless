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
    if (-not (Get-Command sfdk -ErrorAction SilentlyContinue) -and (Test-Path 'C:\SailfishOS\bin\sfdk.exe')) {
        $env:Path += ';C:\SailfishOS\bin'
    }
    if (-not (Get-Command sfdk -ErrorAction SilentlyContinue)) { throw 'sfdk not found. Install Sailfish SDK 3.13.5 (Docker build engine).' }
    foreach ($b in 'sailvlessd', 'sailvless-tun-helper', 'sailvless-awg', 'xray', 'sing-box') {
        if (-not (Test-Path (Join-Path $outBin $b))) { throw "out\bin\$b missing (run .\build.ps1 -Cores once)" }
    }
    & (Join-Path $root 'ui\translations\gen-ts.ps1')

    # The SDK build engine only sees its workspace (the user profile), so the
    # sources are mirrored there and built in place.
    $shadow = Join-Path $env:USERPROFILE '.sailvless-build'
    foreach ($d in 'rpm', 'ui', 'systemd', 'dbus', 'connman') {
        robocopy (Join-Path $root $d) (Join-Path $shadow $d) /MIR /NFL /NDL /NJH /NJS /NP /XD .qmake.stash | Out-Null
        if ($LASTEXITCODE -ge 8) { throw "robocopy $d failed" }
    }
    robocopy $outBin (Join-Path $shadow 'bin') /MIR /NFL /NDL /NJH /NJS /NP | Out-Null
    if ($LASTEXITCODE -ge 8) { throw 'robocopy bin failed' }
    Remove-Item -Recurse -Force (Join-Path $shadow 'RPMS') -ErrorAction SilentlyContinue

    Push-Location $shadow
    try {
        $log = sfdk -c "target=$Target" build 2>&1 | ForEach-Object { "$_" }
        $code = $LASTEXITCODE
        $log | Select-Object -Last 5 | ForEach-Object { Write-Host $_ }
        if ($code) { $log | Select-Object -Last 40 | ForEach-Object { Write-Host $_ }; throw 'sfdk build failed' }
        # lupdate parses every QML file during the build: treat its syntax errors as fatal
        $qmlErrors = $log | Where-Object { $_ -match '\.(qml|js):\d+:\d+: error' }
        if ($qmlErrors) { $qmlErrors | ForEach-Object { Write-Host $_ }; throw 'QML syntax errors' }
    } finally { Pop-Location }

    $outRpm = Join-Path $root 'out\rpm'
    New-Item -ItemType Directory -Force $outRpm | Out-Null
    Get-ChildItem (Join-Path $shadow 'RPMS') -Filter *.rpm | ForEach-Object {
        Copy-Item $_.FullName $outRpm -Force
        Write-Host "rpm: $(Join-Path $outRpm $_.Name)"
    }
}
