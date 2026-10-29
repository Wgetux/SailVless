<#
.SYNOPSIS
  Deploy SailVless to the phone over SSH (key auth).
    .\deploy.ps1           dev mode: copy out\bin\* to ~/.local/lib/sailvless, install user unit, restart daemon
    .\deploy.ps1 -Rpm      copy newest out\rpm\*.rpm and install it with pkcon install-local
#>
param(
    [string]$Device = 'defaultuser@192.168.2.15',
    [switch]$Rpm
)
$ErrorActionPreference = 'Stop'
$root = $PSScriptRoot
$sshOpts = @('-o', 'BatchMode=yes', '-o', 'ConnectTimeout=10')

function Invoke-Remote([string]$cmd) {
    ssh @sshOpts $Device $cmd
    if ($LASTEXITCODE) { throw "remote command failed ($LASTEXITCODE): $cmd" }
}

if ($Rpm) {
    $rpm = Get-ChildItem (Join-Path $root 'out\rpm') -Filter 'sailvless-*.aarch64.rpm' |
        Sort-Object LastWriteTime -Descending | Select-Object -First 1
    if (-not $rpm) { throw 'No RPM in out\rpm. Run .\build.ps1 -Rpm first.' }
    scp @sshOpts $rpm.FullName "${Device}:/tmp/$($rpm.Name)"
    Invoke-Remote "pkcon -y -p install-local /tmp/$($rpm.Name) && rm -f /tmp/$($rpm.Name)"
    Invoke-Remote 'systemctl --user daemon-reload && systemctl --user enable sailvlessd && systemctl --user restart sailvlessd && systemctl --user --no-pager status sailvlessd | head -5'
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
