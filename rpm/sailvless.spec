Name:       sailvless
Summary:    VLESS / VMess / Trojan / Shadowsocks / Hysteria2 / TUIC / AmneziaWG proxy client
Version:    1.1.0
Release:    1
# own code: GPL-3.0-or-later; bundled: Xray-core (MPL-2.0), sing-box (GPL-3.0-or-later),
# amneziawg-go (MIT), gVisor and btree (Apache-2.0), godbus (BSD-2-Clause), golang.org/x (BSD-3-Clause)
License:    GPL-3.0-or-later AND MPL-2.0 AND MIT AND Apache-2.0 AND BSD-2-Clause AND BSD-3-Clause
URL:        https://github.com/Wgetux/SailVless

# Go binaries are static, prebuilt and stripped by build.ps1
%define debug_package %{nil}
%global __strip /bin/true
%global __provides_exclude_from ^%{_libexecdir}/sailvless/.*$
%global __requires_exclude_from ^(%{_libexecdir}/sailvless/.*|%{_bindir}/sailvlessd)$
%define userunitdir /usr/lib/systemd/user

Requires:   sailfishsilica-qt5 >= 0.10.9
Requires:   systemd
BuildRequires:  pkgconfig(sailfishapp) >= 1.1.0
BuildRequires:  pkgconfig(Qt5Core)
BuildRequires:  pkgconfig(Qt5Qml)
BuildRequires:  pkgconfig(Qt5Quick)
BuildRequires:  pkgconfig(Qt5DBus)
BuildRequires:  qt5-qttools-linguist
BuildRequires:  connman-devel
BuildRequires:  pkgconfig(dbus-1)
BuildRequires:  pkgconfig(glib-2.0)

%description
Proxy client for Sailfish OS in the spirit of v2rayNG: subscriptions (with
HWID for Remnawave panels), server list with ping, local SOCKS5/HTTP proxy
with optional authentication. Cores: Xray-core and sing-box.

%prep
# sfdk builds in place (build.ps1 mirrors the sources into the SDK workspace)

%build
# connman-vpnd plugin: shows the tunnel as a system VPN (status bar icon)
gcc %{optflags} -shared -fPIC -Wall $(pkg-config --cflags dbus-1 glib-2.0) \
    -o connman/sailvless.so connman/sailvless-vpn.c
cd ui
%qmake5 VERSION=%{version}
%make_build

%install
cd ui
%qmake5_install
cd ..
install -D -m 755 bin/sailvlessd %{buildroot}%{_bindir}/sailvlessd
install -D -m 755 bin/xray %{buildroot}%{_libexecdir}/sailvless/xray
install -D -m 755 bin/sing-box %{buildroot}%{_libexecdir}/sailvless/sing-box
install -D -m 755 bin/sailvless-awg %{buildroot}%{_libexecdir}/sailvless/sailvless-awg
install -D -m 644 systemd/sailvlessd.service %{buildroot}%{userunitdir}/sailvlessd.service
mkdir -p %{buildroot}%{userunitdir}/user-session.target.wants
ln -s ../sailvlessd.service %{buildroot}%{userunitdir}/user-session.target.wants/sailvlessd.service
install -D -m 644 dbus/org.sailvless.Daemon.service %{buildroot}%{_datadir}/dbus-1/services/org.sailvless.Daemon.service
# TUN helper: root, started on demand over the system bus
install -D -m 755 bin/sailvless-tun-helper %{buildroot}%{_libexecdir}/sailvless/sailvless-tun-helper
install -D -m 644 systemd/sailvless-tun.service %{buildroot}%{_unitdir}/sailvless-tun.service
install -D -m 644 dbus/org.sailvless.Tun.conf %{buildroot}%{_sysconfdir}/dbus-1/system.d/org.sailvless.Tun.conf
install -D -m 644 dbus/org.sailvless.Tun.system.service %{buildroot}%{_datadir}/dbus-1/system-services/org.sailvless.Tun.service
install -D -m 755 connman/sailvless.so %{buildroot}%{_libdir}/connman/plugins-vpn/sailvless.so
install -D -m 644 connman/sailfish-vpn/details.qml %{buildroot}%{_datadir}/sailfish-vpn/sailvless/details.qml
mkdir -p %{buildroot}%{_datadir}/licenses/sailvless
install -m 644 licenses/*.txt %{buildroot}%{_datadir}/licenses/sailvless/

%post
systemctl daemon-reload || :
# a running tunnel is torn down (routes restored); the user daemon starts it again
systemctl try-restart sailvless-tun.service || :
systemctl-user daemon-reload || :
systemctl-user try-restart sailvlessd.service || :
# load the VPN plugin (connman-vpnd only runs VPN connections, the network stays up)
systemctl try-restart connman-vpn.service || :

%preun
if [ $1 -eq 0 ]; then
    systemctl-user stop sailvlessd.service || :
    systemctl stop sailvless-tun.service || :
fi

%postun
systemctl try-restart connman-vpn.service || :
systemctl daemon-reload || :
systemctl-user daemon-reload || :

%files
%defattr(-,root,root,-)
%{_bindir}/sailvless
%{_bindir}/sailvlessd
%{_libexecdir}/sailvless
%{_datadir}/sailvless
%{_datadir}/applications/sailvless.desktop
%{_datadir}/icons/hicolor/*/apps/sailvless.png
%{userunitdir}/sailvlessd.service
%{userunitdir}/user-session.target.wants/sailvlessd.service
%{_datadir}/dbus-1/services/org.sailvless.Daemon.service
%{_unitdir}/sailvless-tun.service
%config %{_sysconfdir}/dbus-1/system.d/org.sailvless.Tun.conf
%{_datadir}/dbus-1/system-services/org.sailvless.Tun.service
%{_libdir}/connman/plugins-vpn/sailvless.so
%{_datadir}/sailfish-vpn/sailvless
%{_datadir}/licenses/sailvless
