# SailVless

[English](README.md) | [Русский](README.ru.md) | **简体中文**

适用于 Sailfish OS 的原生代理与 VPN 客户端：订阅、带延迟测试的服务器列表、一键“连接”、带密码的本地 SOCKS5/HTTP
代理，以及接管设备全部流量的系统级 VPN 模式（TUN）。支持 VLESS（REALITY、Vision、XHTTP）、VMess、Trojan、
Shadowsocks、Hysteria2、TUIC 和 AmneziaWG。

自由软件，采用 GNU GPL 第 3 版或更新版本授权。已在 OnePlus 6T、Sailfish OS 5.1.0.11（aarch64，内核 4.9，Qt 5.6）上测试。

## 功能

- **协议：** VLESS（REALITY、Vision、XHTTP）、VMess、Trojan、Shadowsocks（含 2022）使用 Xray 核心；
  Hysteria2（端口跳跃、salamander）和 TUIC 使用 sing-box。核心自动选择。
- **传输：** TCP（含 HTTP 伪装）、WebSocket、gRPC、HTTPUpgrade、XHTTP；TLS / REALITY、uTLS 指纹、SNI、ALPN。
- **AmneziaWG**（及普通 WireGuard）：`.conf` 配置或 AmneziaVPN `vpn://` 链接；支持 AWG 1.0/1.5/2.0 混淆参数
  （Jc、Jmin、Jmax、S1–S4、H1–H4、I1–I5）和 AWG 3.1（HeaderProtectionKey、ContentPaddingAddition、RandomTrailers、
  DisableCookies）。在用户空间运行（官方 amneziawg-go 库和 gVisor 网络栈），无需自己的 TUN 设备和 root 权限：
  `sailvless-awg` 以 SOCKS5 形式提供隧道，Xray 串联到它，因此路由、本地代理、TUN 模式和连通性检查对 AmneziaWG 同样有效。
  支持分流配置（AllowedIPs）。
- **导入：** 从剪贴板或手动输入 `vless:// vmess:// trojan:// ss:// hysteria2:// hy2:// tuic:// vpn://` 链接，
  完整的 Xray 或 sing-box JSON 配置，AmneziaWG/WireGuard 配置（粘贴或选择文件）。
- **订阅：** base64、链接列表、Xray JSON（配置数组）、sing-box JSON；支持 `subscription-userinfo`（流量和到期时间）、
  `profile-title`（含 `base64:`）、`profile-update-interval`、`support-url`、`announce` 等响应头；自动更新；
  面板被屏蔽时可通过隧道更新。
- **Remnawave 面板的 HWID：** `x-hwid`（稳定值：machine-id 加盐后的 SHA-256，不发送 machine-id 本身）、`x-device-os`、
  `x-ver-os`、`x-device-model`、自定义 User-Agent；HWID 可查看、重置或手动设置；达到设备上限时给出明确提示。
- **路由：** 全部走代理 / 绕过局域网和指定国家（`cn`：中国服务和 IP 段；`ru`：俄罗斯服务、.ru/.su/.рф 和 IP 段；
  其他代码：该国顶级域名和 IP 段）/ 仅使用自己的规则；广告拦截；按域名、后缀、关键字、IP/子网、geosite、geoip
  设置规则，动作为“代理 / 直连 / 拦截”。手机语言为中文时，默认国家代码为 `cn`。
- **单个服务器的规则**，优先于通用规则匹配（例如在全局隧道服务器上让某些网站直连）；规则可导出到文件、从文件导入，
  也可多选后批量删除。
- **DNS：** 经代理的 DNS（DoH、TCP、UDP）和直连网站的 DNS 分开设置；TUN 模式下 DNS 不会绕过隧道泄漏。
- **本地代理：** SOCKS5 和 HTTP 仅监听 127.0.0.1，带用户名和密码（随机生成，默认开启），端口可固定或随机；
  向局域网开放需要手动打开开关并会有警告。
- **TUN 模式：** 设备的全部流量经隧道，无需逐个设置应用；Wi-Fi ↔ 移动数据切换后保持连接；出错时网络回退为直连，
  而不是断网。在系统中显示为 VPN “SailVless”：状态栏图标、设置 → VPN 以及顶部菜单开关。
- **界面：** Silica 风格，封面显示状态并带按钮，延迟测试和“最快服务器”，速度和流量，可复制的日志，
  连接中断和设备上限通知。支持英语、俄语和简体中文。

## 安装

软件包需手动安装（不在 Harbour 商店），以 root 身份执行：

```sh
devel-su pkcon install-local sailvless-<版本>.aarch64.rpm
```

安装后守护进程会立即在用户会话中启动。应用在应用列表中名为“SailVless”。
请勿同时运行其他带有自己隧道的 VPN 客户端（例如 harbour-xhvpn）。

## 工作原理

```
QML/Silica 界面 ──session D-Bus──► sailvlessd（systemd --user，以用户身份运行）
                                   ├ 配置、订阅、HWID、路由、地理数据库
                                   ├ 代理模式：自行运行 xray / sing-box（AmneziaWG 另加 sailvless-awg）
                                   └──system D-Bus──► sailvless-tun-helper（root，按需启动）
                                                       ├ 核心以用户身份运行，仅有 CAP_NET_ADMIN
                                                       └ sing-box TUN（自有配置，以 root 运行）
connman-vpnd ◄── sailvless.so 插件：将隧道显示为系统 VPN（不修改路由和 DNS）
```

数据位于 `~/.local/share/sailvless/`（目录权限 0700）。订阅地址、UUID 和密码只保存在 `secrets.json`（0600）中；
日志和界面中的订阅地址会被隐藏。地理数据库：`~/.local/share/sailvless/geo`。

### TUN 模式

内核 4.9 不支持按 UID 的路由规则，因此代理核心自身的流量通过标记区分：

- 核心的套接字带 `SO_MARK`；`fwmark → main` 规则位于隧道规则之前，`fwmark → unreachable` 防止带标记的流量落入隧道；
- 回包通过 CONNMARK 恢复标记，`src_valid_mark=1` 让 `wlan0` 上严格的 `rp_filter` 接受它们；
- 局域网不走隧道（手机仍可从局域网访问），但发往路由器的 DNS 查询会被重定向进隧道（否则运营商能看到）；
  发往私有 IPv6 地址的 DNS 被拒绝（内核没有 IPv6 NAT）；
- IPv6 始终被接管：默认拒绝（应用只获得 IPv4 地址），也可选择走代理；
- 隧道建立前通过系统 DNS 解析服务器地址，在运营商白名单环境下同样可用；
- 网络切换时只重启核心，隧道保持不变，因此没有泄漏窗口。

root 辅助服务只接受 `defaultuser` 的调用（D-Bus 策略），自行生成 TUN 配置，并受 systemd 限制（`CapabilityBoundingSet`、
`NoNewPrivileges`、`ProtectSystem=strict`、`ProtectHome=read-only`）。它所做的修改会在断开连接、守护进程离开总线、
辅助服务崩溃（`ExecStopPost`）以及下次启动时撤销。安装软件包会重启 `connman-vpn.service`，其他客户端的 VPN 会因此断开。

## 限制

- **Sailfish 系统代理**（设置 → 网络）不支持用户名和密码：启用认证时请使用 TUN 模式，或在应用中设置代理。
- **不支持按应用分流：** 内核 4.9 既没有 UID 规则也没有 nftables。
- **DNS-over-TLS** 不受 Xray 支持，请使用 DoH。
- **到公网地址的无 TLS/REALITY 的 VLESS** 会被 Xray v26 拒绝（其策略，无法绕过）。
- Hysteria2 链接中的 **`pinSHA256`** 不受 sing-box 支持，会被忽略并在日志中警告。
- **Hysteria2/TUIC 的路由** 使用 sing-box 自行下载的 SagerNet 规则集，其完整性仅由 HTTPS 保证。Hysteria2 和 TUIC
  尚未在真实服务器上测试。
- **AmneziaWG：** `vpn://` 链接中只读取 AmneziaWG（不支持 Amnezia 的 OpenVPN/Cloak/XRay 容器）。AWG 服务器不显示
  延迟（UDP），可通过隧道连通性检查判断是否可用。分流配置中服务器可能只转发其自身列表中的网络，此时“代理”规则无法
  添加其他网站。
- **Waydroid：** 隧道规则也覆盖容器的转发流量，但不会开启转发，也未经测试。

## 构建

需要 Windows、Go 1.27+、Sailfish SDK 3.13+（Docker 构建引擎，目标 `SailfishOS-5.1.0.11-aarch64`）。

```powershell
.\build.ps1 -Cores          # 仅一次：按 cores\versions.lock 中的标签从官方源码构建 Xray 和 sing-box
.\build.ps1 -Test -Rpm      # 测试、Go 二进制（linux/arm64）、通过 sfdk 构建 RPM -> out\rpm
.\deploy.ps1 -Rpm -RootDevice root@<手机>   # 通过 root SSH 密钥安装
```

核心只从官方仓库按固定提交构建，并执行 `go mod verify`。开发说明见 [CLAUDE.md](CLAUDE.md)。
翻译位于 `ui/translations/*.json`；有未翻译的字符串时构建会失败，因此新文本需要在 `ru.json` 和 `zh_CN.json`
（复数形式还需 `en.json`）中添加条目。欢迎贡献新的翻译。

## 调试

```sh
sailvlessd ctl status        # 状态、流量
sailvlessd ctl logs 200      # 守护进程、核心和 TUN 辅助服务的日志
sailvlessd ctl list          # 服务器
sailvlessd ctl ping          # 所有服务器的 TCP 延迟
sailvlessd ctl settings      # 设置（JSON）；修改：ctl settings '{"logLevel":"debug"}'
sailvlessd ctl rules <id>    # 服务器自己的规则；rules-export / rules-import 用于文件
sailvlessd ctl hwid          # HWID 和设备请求头
curl -x "$(sailvlessd ctl proxy-url)" https://ifconfig.me
```

## 许可证

SailVless 是自由软件：你可以依据自由软件基金会发布的 GNU 通用公共许可证第 3 版或（由你选择）任何更新版本的条款
重新分发和/或修改它。见 [LICENSE](LICENSE)。

软件包还包含 Xray-core（MPL-2.0）、sing-box（GPL-3.0-or-later），以及 `sailvless-awg` 中的 amneziawg-go（MIT）和
gVisor（Apache-2.0），见 [THIRD-PARTY.md](THIRD-PARTY.md)。SailVless 与 Xray、sing-box 和 Amnezia 项目无关联。
