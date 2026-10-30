//go:build linux

package main

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"

	"sailvless/internal/tun"
)

// Integration with connman-vpnd through the "sailvless" VPN plugin, so the
// tunnel shows up as a system VPN (status bar icon, Settings, top menu).

const (
	vpnBus     = "net.connman.vpn"
	vpnManager = "net.connman.vpn.Manager"
	vpnConn    = "net.connman.vpn.Connection"
	vpnType    = "sailvless"

	netSettle = 1500 * time.Millisecond // connmand reports the network after the VPN
	netQuiet  = 5 * time.Second         // a disconnect this soon after a change is not the user's
)

// RequestConnect / RequestDisconnect are called by the connman plugin when the
// user toggles the SailVless VPN in the system UI; the daemon acts on them.
func (h *helper) RequestConnect() *dbus.Error {
	h.logs.Add("helper: ", "system VPN switch: connect requested")
	_ = h.conn.Emit(tun.Path, tun.Interface+".Requested", "connect")
	return nil
}

func (h *helper) RequestDisconnect() *dbus.Error {
	// tearing the tunnel down on exit makes connman-vpnd report a disconnect:
	// that is not the user either, the daemon brings the new helper up
	h.mu.Lock()
	exiting := h.exiting
	h.mu.Unlock()
	if exiting {
		h.logs.Add("helper: ", "exiting, keeping the system VPN switch on")
		return nil
	}
	// connman-vpnd disconnects every VPN when it stops (package update, phone
	// shutdown): that is not the user switching SailVless off
	if st := h.unitState("connman-vpn.service"); st == "deactivating" || st == "inactive" {
		h.logs.Add("helper: ", "connman-vpnd is "+st+", keeping the tunnel")
		return nil
	}
	// connmand also takes VPNs down when the network goes away: the tunnel must
	// survive that. The system VPN comes back with the network (RestartCore).
	go func() {
		time.Sleep(netSettle)
		if h.networkChanging() {
			h.logs.Add("helper: ", "network changed, keeping the tunnel")
			return
		}
		h.logs.Add("helper: ", "system VPN switch: disconnect requested")
		_ = h.conn.Emit(tun.Path, tun.Interface+".Requested", "disconnect")
	}()
	return nil
}

// networkChanging tells a disconnect caused by the network from the user's.
func (h *helper) networkChanging() bool {
	h.mu.Lock()
	recent := time.Since(h.netChanged) < netQuiet
	h.mu.Unlock()
	st := h.connmanState("net.connman", "/", "net.connman.Manager")
	return recent || st != "online" && st != "ready"
}

// unitState returns the ActiveState of a systemd unit ("unknown" on errors).
func (h *helper) unitState(unit string) string {
	var path dbus.ObjectPath
	if err := h.conn.Object("org.freedesktop.systemd1", "/org/freedesktop/systemd1").
		Call("org.freedesktop.systemd1.Manager.GetUnit", 0, unit).Store(&path); err != nil {
		return "unknown"
	}
	v, err := h.conn.Object("org.freedesktop.systemd1", path).GetProperty("org.freedesktop.systemd1.Unit.ActiveState")
	if err != nil {
		return "unknown"
	}
	s, _ := v.Value().(string)
	return s
}

// connmanProvider returns the object path of our VPN connection, creating it
// on first use (no settings: the plugin only mirrors the tunnel's state).
func (h *helper) connmanProvider() (dbus.ObjectPath, error) {
	mgr := h.conn.Object(vpnBus, "/")
	var conns []struct {
		Path  dbus.ObjectPath
		Props map[string]dbus.Variant
	}
	if err := mgr.Call(vpnManager+".GetConnections", 0).Store(&conns); err != nil {
		return "", err
	}
	for _, c := range conns {
		if t, _ := c.Props["Type"].Value().(string); t == vpnType {
			return c.Path, nil
		}
	}
	props := map[string]dbus.Variant{
		"Type":       dbus.MakeVariant(vpnType),
		"Name":       dbus.MakeVariant("SailVless"),
		"Host":       dbus.MakeVariant("127.0.0.1"),
		"VPN.Domain": dbus.MakeVariant("sailvless"),
	}
	var path dbus.ObjectPath
	if err := mgr.Call(vpnManager+".Create", 0, props).Store(&path); err != nil {
		return "", err
	}
	h.logs.Add("helper: ", "created the system VPN connection "+string(path))
	return path, nil
}

// connmanConnect marks our VPN connection as connected once the tunnel is up
// (started from the app). With the tunnel interface already present the
// plugin reports READY at once; it never changes routes or DNS. connmand
// refuses while the phone has no network yet (tunnel restored at boot before
// Wi-Fi is up), so it retries for a few minutes; a network change calls it again.
func (h *helper) connmanConnect() {
	h.mu.Lock()
	if h.sysConnecting {
		h.mu.Unlock()
		return
	}
	h.sysConnecting = true
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		h.sysConnecting = false
		h.mu.Unlock()
	}()

	var lastErr string
	for try := 0; ; try++ {
		h.mu.Lock()
		up := h.core != nil
		h.mu.Unlock()
		if !up {
			return
		}
		err := h.connmanConnectOnce()
		if err == nil {
			if try > 0 {
				h.logs.Add("helper: ", "system VPN shown")
			}
			return
		}
		if err.Error() != lastErr {
			h.logs.Add("helper: ", "system VPN connect: "+err.Error()+", retrying")
			lastErr = err.Error()
		}
		if try == 30 { // a few minutes; the next network change starts over
			h.logs.Add("helper: ", "system VPN connect: giving up until the next network change")
			return
		}
		time.Sleep(3 * time.Second)
	}
}

func (h *helper) connmanConnectOnce() error {
	path, err := h.connmanProvider()
	if err != nil {
		return err
	}
	if h.connmanState(vpnBus, path, vpnConn) == "ready" {
		return nil
	}
	// connman-vpnd only accepts Connect from connmand: go through its service
	// object, vpn_<connection id>
	svc := dbus.ObjectPath("/net/connman/service/vpn_" + path[strings.LastIndex(string(path), "/")+1:])
	// Connect answers only when the connection is done: don't wait for it
	err = h.connmanCall(svc, "Connect")
	var de dbus.Error
	if err != nil && !errors.Is(err, context.DeadlineExceeded) && !(errors.As(err, &de) &&
		(de.Name == "net.connman.Error.InProgress" || de.Name == "net.connman.Error.AlreadyConnected")) {
		return err
	}
	// connman-vpnd may hold the request: it starts VPNs without a daemon only
	// once connmand is online. Only READY means it worked.
	for i := 0; i < 5; i++ {
		if h.connmanState(vpnBus, path, vpnConn) == "ready" {
			return nil
		}
		time.Sleep(time.Second)
	}
	return errors.New("not shown yet (state " + h.connmanState(vpnBus, path, vpnConn) + ")")
}

// connmanCall calls a net.connman.Service method without waiting long:
// connmand answers Connect only when the connection is done.
func (h *helper) connmanCall(svc dbus.ObjectPath, method string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	return h.conn.Object("net.connman", svc).CallWithContext(ctx, "net.connman.Service."+method, 0).Err
}

// connmanState reads "State" through connman's own GetProperties (connman has
// no org.freedesktop.DBus.Properties); "" if unknown.
func (h *helper) connmanState(dest string, path dbus.ObjectPath, iface string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var props map[string]dbus.Variant
	if err := h.conn.Object(dest, path).CallWithContext(ctx, iface+".GetProperties", 0).Store(&props); err != nil {
		return ""
	}
	st, _ := props["State"].Value().(string)
	return st
}
