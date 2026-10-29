# SailVless

Proxy client (VLESS/VMess/Trojan/SS/Hysteria2/TUIC/AmneziaWG, XHTTP) for Sailfish OS. Notes for development
(also read by AI coding assistants). Personal, unpublished notes may live in `CLAUDE.local.md` (gitignored).

## Target device
- Developed on a OnePlus 6T, Sailfish OS 5.1.0.11, aarch64, kernel 4.9 (no nftables, no `ip rule uidrange`), Qt 5.6.3 + Silica.
- SSH: `ssh defaultuser@192.168.2.15` over USB (Sailfish developer mode default). Scripts take `-Device`/`-RootDevice`.
  Network-switch tests must run detached on the phone (setsid nohup).
- Use root only for package install / system-level tasks.
- Screenshot (lipstick needs group `privileged`):
  `ssh root@<phone> 'runuser -u defaultuser -g privileged -- env DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/100000/dbus/user_bus_socket dbus-send --session --print-reply --dest=org.nemomobile.lipstick /org/nemomobile/lipstick/screenshot org.nemomobile.lipstick.saveScreenshot string:/home/defaultuser/Pictures/x.png'`
- Apps started over SSH don't get a foreground window — open the UI from the launcher.
- SDK workspace is the user profile: `build.ps1 -Rpm` mirrors sources to `%USERPROFILE%\.sailvless-build` and runs sfdk there.
- Install RPM: `.\deploy.ps1 -Rpm -RootDevice root@<phone>` (or set `$env:SAILVLESS_ROOT_SSH`).
- Never run another TUN VPN (e.g. harbour-xhvpn) at the same time.
- Userland is busybox: `pgrep -x`/`pkill -x` match the full argv0 path — use `pidof`.
- `defaultuser` can't read the journal; use `sailvlessd ctl logs` (in-memory buffer of the daemon).
- Test links/configs live in `local/` (gitignored). Import them via stdin so they never hit a command line:
  `Get-Content local\test-link.txt -Raw | ssh defaultuser@<phone> 'sailvlessd ctl import -'`.
- Quick check on device: `curl -x "$(sailvlessd ctl proxy-url)" https://ifconfig.me`.
- Subscription URLs are secrets (often paid and HWID-bound): never print/log/commit them.
- Validate generated configs with real cores: `SAILVLESS_DUMP=C:/SailVless/out/configs go test ./internal/config/`,
  then scp to the phone and run `xray run -test -c` / `sing-box check -c`.
- AmneziaWG: `cmd/sailvless-awg` (amneziawg-go v3 + netstack) exposes SOCKS5; Xray chains to it (profile
  protocol `amneziawg`, conf text in Raw/secrets).
- Xray v26 refuses VLESS without TLS/REALITY to public addresses (hard policy, no override).
- Geo data (Loyalsoldier, sha256-verified) lives in `~/.local/share/sailvless/geo` (`XRAY_LOCATION_ASSET`).

## Layout
- `daemon/` — Go module: `cmd/sailvlessd` (user daemon, D-Bus session `org.sailvless.Daemon`), `cmd/sailvless-tun-helper`
  (root, system D-Bus), `cmd/sailvless-awg` (AmneziaWG sidecar).
- `cores/` — `versions.lock` pins Xray / sing-box tags; `build-cores.ps1` builds them from official sources.
- `ui/` — Silica QML + thin C++ D-Bus bridge (qmake). `rpm/sailvless.spec` packages everything.
- `ui/translations/` — `en.json` (plurals), `ru.json`, `zh_CN.json`; `gen-ts.ps1` (run by build.ps1) fails on any
  untranslated string, so every new `qsTr()` needs an entry in each dictionary.
- `connman/` — connman-vpnd plugin: the TUN mode shows up as a system VPN.
- `systemd/`, `dbus/` — unit and policy files.

## Commands (PowerShell, repo root)
- `.\build.ps1` — cross-compile Go binaries (linux/arm64, CGO off) to `out\bin`.
- `.\build.ps1 -Cores` — also build xray + sing-box from pinned sources (first time records commit hashes in `versions.lock`; commit that change).
- `.\build.ps1 -Test` — run `go test ./...` first.
- `.\build.ps1 -Rpm [-Target <sfdk target>]` — build RPM via sfdk to `out\rpm`.
- `.\deploy.ps1` — dev deploy of `out\bin` to `~/.local/lib/sailvless` + user unit, restart `sailvlessd`.
- `.\deploy.ps1 -Rpm` — install newest RPM via `pkcon install-local`.

## Rules
- Cores: only official repos at pinned tags, `go mod verify`; never download random binaries.
- Secrets (UUIDs, passwords, test links) never go into the repo; on device they live in `~/.local/share/sailvless/secrets.json` (0600).
- Local inbounds bind 127.0.0.1 by default.
- No fake features: if something can't work on Sailfish, say so in the UI.
- UI texts stay generic (no specific services or providers); English source, ru and zh_CN translations.
