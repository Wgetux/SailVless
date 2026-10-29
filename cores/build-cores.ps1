<#
.SYNOPSIS
  Builds proxy cores (Xray, sing-box) for linux/arm64 from official sources at the tags pinned in versions.lock.
  Go module checksums are verified against sum.golang.org (GOFLAGS=-mod=readonly, GONOSUMDB unset).
#>
param(
    [string[]]$Only = @()   # e.g. -Only xray
)
$ErrorActionPreference = 'Stop'
$root = $PSScriptRoot
$lock = Join-Path $root 'versions.lock'
$srcDir = Join-Path $root 'src'
$binDir = Join-Path $root 'bin'
New-Item -ItemType Directory -Force $srcDir, $binDir | Out-Null

if (-not (Get-Command go -ErrorAction SilentlyContinue)) { throw 'Go not found. Install it: winget install --id GoLang.Go -e' }

$mainPkg = @{ 'xray' = './main'; 'sing-box' = './cmd/sing-box' }
$lines = Get-Content $lock
$updated = $false

for ($i = 0; $i -lt $lines.Count; $i++) {
    $line = $lines[$i]
    if ($line -match '^\s*(#|$)') { continue }
    $name, $repo, $tag, $commit, $tags = $line.Split('|')
    if ($Only.Count -and $Only -notcontains $name) { continue }

    $dir = Join-Path $srcDir $name
    if (-not (Test-Path (Join-Path $dir '.git'))) {
        git clone --quiet --depth 1 --branch $tag $repo $dir
    } else {
        git -C $dir fetch --quiet --depth 1 origin "refs/tags/${tag}:refs/tags/${tag}"
        git -C $dir checkout --quiet --force $tag
    }
    if ($LASTEXITCODE) { throw "git failed for $name" }
    $head = (git -C $dir rev-parse HEAD).Trim()
    if ($commit) {
        if ($head -ne $commit) { throw "$name ${tag}: commit $head does not match pinned $commit" }
    } else {
        Write-Host "$name ${tag}: recording commit $head"
        $lines[$i] = "$name|$repo|$tag|$head|$tags"
        $updated = $true
    }

    $env:GOOS = 'linux'; $env:GOARCH = 'arm64'; $env:CGO_ENABLED = '0'
    $env:GOFLAGS = '-mod=readonly'
    $out = Join-Path $binDir $name
    $goArgs = @('build', '-trimpath', '-ldflags', '-s -w -buildid=', '-o', $out)
    if ($tags) { $goArgs += @('-tags', $tags) }
    $goArgs += $mainPkg[$name]
    Push-Location $dir
    try {
        go mod verify
        if ($LASTEXITCODE) { throw "go mod verify failed for $name" }
        & go @goArgs
        if ($LASTEXITCODE) { throw "go build failed for $name" }
    } finally { Pop-Location; Remove-Item Env:GOOS, Env:GOARCH, Env:CGO_ENABLED, Env:GOFLAGS }
    $hash = (Get-FileHash $out -Algorithm SHA256).Hash.ToLower()
    Write-Host ("{0,-9} {1} -> {2} sha256={3}" -f $name, $tag, $out, $hash)
}

if ($updated) { Set-Content -Path $lock -Value $lines -Encoding utf8NoBOM }
