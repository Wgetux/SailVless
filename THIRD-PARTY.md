# Third-party software

SailVless itself is licensed under the GNU General Public License v3.0 or later (see [LICENSE](LICENSE)).
The package also ships the following components; their license texts are in [licenses/](licenses/) and are
installed to `/usr/share/licenses/sailvless/`.

## Proxy cores (separate programs, built from official sources)

The exact tags and commits are pinned in [cores/versions.lock](cores/versions.lock); `build-cores.ps1` checks them
out from the official repositories and verifies the module sums (`go mod verify`).

| Component | Version | License | Source |
|---|---|---|---|
| Xray-core | v26.10.21 | MPL-2.0 | https://github.com/XTLS/Xray-core |
| sing-box | v1.14.4 | GPL-3.0-or-later (with the author's notice on the name) | https://github.com/SagerNet/sing-box |

## Libraries linked into SailVless binaries

| Module | Version | License |
|---|---|---|
| github.com/amnezia-vpn/amneziawg-go/v3 | v3.1.20260828 | MIT |
| gvisor.dev/gvisor | 2023-12-02 (1f7806d17489) | Apache-2.0 |
| github.com/godbus/dbus/v5 | v5.2.4 | BSD-2-Clause |
| github.com/google/btree | v1.1.3 | Apache-2.0 |
| golang.org/x/crypto, net, sys, time | v0.42.0, v0.44.0, v0.36.0, v0.9.0 | BSD-3-Clause |
| Go standard library | 1.27 | BSD-3-Clause |

## Data downloaded at run time

- Geo databases for Xray: [Loyalsoldier/v2ray-rules-dat](https://github.com/Loyalsoldier/v2ray-rules-dat) (GPL-3.0),
  SHA-256 checked against the release.
- Rule sets for sing-box: [SagerNet/sing-geosite](https://github.com/SagerNet/sing-geosite),
  [SagerNet/sing-geoip](https://github.com/SagerNet/sing-geoip), downloaded by sing-box itself.
