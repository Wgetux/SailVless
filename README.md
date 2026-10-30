# SailVless

**English** | [Русский](README.ru.md) | [简体中文](README.zh-CN.md)

A native proxy and VPN client for Sailfish OS: subscriptions, a server list with ping, one "Connect" button,
a local SOCKS5/HTTP proxy with a password and a system-wide VPN mode (TUN) for all traffic of the device.
VLESS (REALITY, Vision, XHTTP), VMess, Trojan, Shadowsocks, Hysteria2, TUIC and AmneziaWG.

Free software under the GNU GPL v3 or later. Tested on a OnePlus 6T with Sailfish OS 5.1.0.11 (aarch64, kernel 4.9, Qt 5.6).

## Features

- **Protocols:** VLESS (REALITY, Vision, XHTTP), VMess, Trojan, Shadowsocks (including 2022) on the Xray core;
  Hysteria2 (port hopping, salamander) and TUIC on sing-box. The core is chosen automatically.
- **Transports:** TCP (including HTTP obfuscation), WebSocket, gRPC, HTTPUpgrade, XHTTP; TLS / REALITY, uTLS
  fingerprints, SNI, ALPN.
- **AmneziaWG** (and plain WireGuard): `.conf` files or AmneziaVPN `vpn://` links; obfuscation of AWG 1.0/1.5/2.0
  (Jc, Jmin, Jmax, S1–S4, H1–H4, I1–I5) and AWG 3.1 (HeaderProtectionKey, ContentPaddingAddition, RandomTrailers,
  DisableCookies). Runs in user space (the official amneziawg-go library and the gVisor network stack) without its
  own TUN device and without root: `sailvless-awg` exposes the tunnel as SOCKS5 and Xray chains to it, so routing,
  the local proxy, the TUN mode and the liveness check work for AmneziaWG too. Split configs (AllowedIPs) are honored.
- **Import:** `vless:// vmess:// trojan:// ss:// hysteria2:// hy2:// tuic:// vpn://` links from the clipboard or typed in,
  full Xray or sing-box JSON configs, AmneziaWG/WireGuard configs (pasted or picked as files).
- **Subscriptions:** base64, plain link lists, Xray JSON (array of configs), sing-box JSON; the headers
  `subscription-userinfo` (traffic and expiry), `profile-title` (also `base64:`), `profile-update-interval`,
  `support-url`, `announce`; automatic updates; updates through the tunnel when the panel is blocked.
- **HWID for Remnawave panels:** `x-hwid` (stable: SHA-256 of machine-id and a salt, machine-id itself is never sent),
  `x-device-os`, `x-ver-os`, `x-device-model`, a custom User-Agent; the HWID can be viewed, reset or set by hand;
  a clear message when the device limit is reached.
- **Routing:** everything through the proxy / bypass the LAN and a country (`cn`: Chinese services and IP ranges,
  `ru`: Russian services, .ru/.su/.рф and IP ranges; other codes: the TLD and IP ranges) / only your own rules;
  ad blocking; rules by domain, suffix, keyword, IP/subnet, geosite, geoip with "proxy / direct / block".
- **Rules per server**, checked before the common ones (e.g. some sites direct only with a full-tunnel server);
  rules can be exported to and imported from files, selected and deleted in bulk.
- **DNS:** separate servers through the proxy (DoH, TCP, UDP) and for direct sites; in TUN mode DNS never leaks
  around the tunnel.
- **Local proxy:** SOCKS5 and HTTP on 127.0.0.1 only, login and password (random, on by default), fixed or random
  ports; opening it to the LAN needs an explicit switch with a warning.
- **TUN mode:** all traffic of the device through the tunnel without configuring apps; survives Wi-Fi ↔ mobile data
  switches; on failures the network falls back to direct instead of breaking. Shows up as the system VPN
  "SailVless": status bar icon, Settings → VPN and the top menu switch.
- **Interface:** Silica, cover with state and a button, ping and "fastest server", speed and traffic, logs with copy,
  notifications about a lost connection and the device limit. English, Russian and Simplified Chinese.

## Installation

The package is side-loaded (not from Harbour) and installed as root:

```sh
devel-su pkcon install-local sailvless-<version>.aarch64.rpm
```

The daemon starts in the user session right away. The app is "SailVless" in the app grid.
Do not run another VPN client with its own tunnel (e.g. harbour-xhvpn) at the same time.

## How it works

```
QML/Silica UI ──session D-Bus──► sailvlessd (systemd --user, runs as the user)
                                   ├ profiles, subscriptions, HWID, routing, geo databases
                                   ├ proxy mode: runs xray / sing-box itself (+ sailvless-awg for AmneziaWG)
                                   └──system D-Bus──► sailvless-tun-helper (root, started on demand)
                                                       ├ the core runs as the user, with CAP_NET_ADMIN only
                                                       └ sing-box TUN (its own config, as root)
connman-vpnd ◄── sailvless.so plugin: mirrors the tunnel as the system VPN (no routes or DNS changes)
```

Data lives in `~/.local/share/sailvless/` (directory 0700). Subscription URLs, UUIDs and passwords are kept only in
`secrets.json` (0600); subscription URLs are masked in logs and in the UI. Geo databases: `~/.local/share/sailvless/geo`.

### TUN mode

Kernel 4.9 has no UID routing rules, so the proxy core's own traffic is told apart by a mark:

- the core's sockets get `SO_MARK`; a `fwmark → main` rule sits before the tunnel rules and `fwmark → unreachable`
  keeps marked traffic from falling into the tunnel;
- replies get the mark back through CONNMARK, `src_valid_mark=1` keeps a strict `rp_filter` on `wlan0` happy;
- the LAN is excluded from the tunnel (the phone stays reachable), but DNS queries to the router are redirected into
  the tunnel (otherwise the provider would see them); DNS to private IPv6 addresses is refused (no IPv6 NAT in the kernel);
- IPv6 is always captured: refused by default (apps get IPv4 addresses only) or proxied if enabled;
- server addresses are resolved through the system DNS before the tunnel goes up, which also works with operator
  whitelists;
- on a network change only the core restarts, the tunnel stays, so there is no window for leaks.

The root helper accepts calls only from `defaultuser` (D-Bus policy), builds the TUN config itself and is confined
by systemd (`CapabilityBoundingSet`, `NoNewPrivileges`, `ProtectSystem=strict`, `ProtectHome=read-only`). Everything
it changes is undone on disconnect, when the daemon leaves the bus, when the helper crashes (`ExecStopPost`) and on
the next start. Installing the package restarts `connman-vpn.service`, so a VPN of another client would go down.

## Limitations

- **The Sailfish system proxy** (Settings → Network) has no login and password: with authentication use the TUN mode
  or set the proxy in the app.
- **No per-app routing:** kernel 4.9 has neither UID rules nor nftables.
- **DNS-over-TLS** is not supported by Xray, use DoH.
- **VLESS without TLS/REALITY** to public addresses is refused by Xray v26 (its policy, cannot be overridden).
- **`pinSHA256`** in Hysteria2 links is not supported by sing-box; it is ignored with a warning in the log.
- **Hysteria2/TUIC routing** uses SagerNet rule sets that sing-box downloads itself; their integrity relies on HTTPS
  only. Hysteria2 and TUIC were not tested against real servers.
- **AmneziaWG:** only AmneziaWG is taken from `vpn://` links (OpenVPN/Cloak/XRay containers of Amnezia are not
  supported). No ping for AWG servers (UDP); the liveness check through the tunnel tells whether they work.
  With split configs a server may forward only its own list of networks, so "through the proxy" rules cannot add
  other sites there.
- **Waydroid:** the tunnel rules cover forwarded container traffic too, but forwarding is not enabled and was not tested.

## Building

Needs Windows, Go 1.27+, Sailfish SDK 3.13+ (Docker build engine, target `SailfishOS-5.1.0.11-aarch64`).

```powershell
.\build.ps1 -Cores          # once: Xray and sing-box from official sources at the tags in cores\versions.lock
.\build.ps1 -Test -Rpm      # tests, Go binaries (linux/arm64), RPM through sfdk -> out\rpm
.\deploy.ps1 -Rpm -RootDevice root@<phone>   # install over SSH with a root key
```

The cores are built only from the official repositories at pinned commits, with `go mod verify`.
Development notes: [CLAUDE.md](CLAUDE.md). Translations: `ui/translations/*.json`; the build fails on any
untranslated string, so a new text needs an entry in `ru.json` and `zh_CN.json` (plurals also in `en.json`).
New translations are welcome.

## Debugging

```sh
sailvlessd ctl status        # state, traffic
sailvlessd ctl logs 200      # logs of the daemon, the core and the TUN helper
sailvlessd ctl list          # servers
sailvlessd ctl ping          # TCP ping of all servers
sailvlessd ctl settings      # settings (JSON); change: ctl settings '{"logLevel":"debug"}'
sailvlessd ctl rules <id>    # a server's own rules; rules-export / rules-import for files
sailvlessd ctl hwid          # HWID and device headers
curl -x "$(sailvlessd ctl proxy-url)" https://ifconfig.me
```

## License

SailVless is free software: you can redistribute it and/or modify it under the terms of the GNU General Public
License as published by the Free Software Foundation, either version 3 of the License, or (at your option) any later
version. See [LICENSE](LICENSE).

The package also contains Xray-core (MPL-2.0), sing-box (GPL-3.0-or-later) and, inside `sailvless-awg`, amneziawg-go
(MIT) with gVisor (Apache-2.0); see [THIRD-PARTY.md](THIRD-PARTY.md). SailVless is not affiliated with the Xray,
sing-box or Amnezia projects.
