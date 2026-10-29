<#
.SYNOPSIS
  Deploy SailVless to the phone over SSH (key auth).
    .\deploy.ps1           dev mode: copy out\bin\* to ~/.local/lib/sailvless, install user unit, restart daemon
    .\deploy.ps1 -Rpm      copy newest out\rpm\*.rpm and install it with pkcon install-local
#>
param(
    [string]$Device = 'defaultuser@192.168.2.15',
    [switch]$Rpm,
    # e.g. root@192.168.2.15 if key-based root SSH is set up; otherwise you are asked to run devel-su on the phone
    [string]$RootDevice = $env:SAILVLESS_ROOT_SSH
)
$ErrorActionPreference = 'Stop'
$root = $PSScriptRoot
$sshOpts = @('-o', 'BatchMode=yes', '-o', 'ConnectTimeout=10')

function Invoke-Remote([string]$cmd) {
    ssh @sshOpts $Device $cmd
    if ($LASTEXITCODE) { throw "remote command failed ($LASTEXITCODE): $cmd" }
}

if ($Rpm) {
    $pkg = Get-ChildItem (Join-Path $root 'out\rpm') -Filter 'sailvless-*.aarch64.rpm' |
        Sort-Object LastWriteTime -Descending | Select-Object -First 1
    if (-not $pkg) { throw 'No RPM in out\rpm. Run .\build.ps1 -Rpm first.' }
    scp @sshOpts $pkg.FullName "${Device}:/tmp/$($pkg.Name)"
    if ($LASTEXITCODE) { throw 'scp failed' }
    $remoteRpm = "/tmp/$($pkg.Name)"
    if ($RootDevice) {
        Invoke-Remote "chmod 644 $remoteRpm"
        ssh @sshOpts $RootDevice "rpm -U --force --replacepkgs $remoteRpm && rm -f $remoteRpm"
        if ($LASTEXITCODE) { throw 'rpm install as root failed' }
    } else {
        Write-Host "Root access is needed to install. On the phone run:`n  devel-su rpm -U --force $remoteRpm`nthen press Enter here."
        Read-Host | Out-Null
    }
    Invoke-Remote "rpm -q sailvless && test -x /usr/bin/sailvlessd"
    # only now drop the dev install: its unit in ~/.config would shadow the packaged one; data in ~/.local/share stays
    Invoke-Remote 'rm -f .config/systemd/user/sailvlessd.service; rm -rf .local/lib/sailvless; systemctl --user daemon-reload && systemctl --user restart sailvlessd && systemctl --user --no-pager status sailvlessd | head -5'
    return
}

$bin = Join-Path $root 'out\bin'
$files = Get-ChildItem $bin -File -ErrorAction SilentlyContinue
if (-not $files) { throw 'Nothing in out\bin. Run .\build.ps1 first.' }

$dst = '.local/lib/sailvless'
Invoke-Remote "mkdir -p $dst/.staging .config/systemd/user && chmod 700 $dst"
# upload to a staging dir, then rename: overwriting a running binary in place fails with ETXTBSY
scp @sshOpts @($files.FullName) "${Device}:$dst/.staging/"
if ($LASTEXITCODE) { throw 'scp failed' }
Invoke-Remote "chmod 755 $dst/.staging/* && mv -f $dst/.staging/* $dst/"
$unit = Join-Path $root 'systemd\sailvlessd.service'
if (Test-Path $unit) {
    # dev unit points at ~/.local/lib; the RPM installs its own unit pointing at /usr/bin
    (Get-Content $unit -Raw) -replace '/usr/bin/sailvlessd', '%h/.local/lib/sailvless/sailvlessd' -replace '/usr/libexec/sailvless', '%h/.local/lib/sailvless' |
        ssh @sshOpts $Device 'cat > .config/systemd/user/sailvlessd.service'
}
Invoke-Remote "systemctl --user daemon-reload && (systemctl --user restart sailvlessd 2>/dev/null; systemctl --user --no-pager status sailvlessd 2>&1 | head -5; true)"
