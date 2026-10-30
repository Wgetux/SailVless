//go:build linux

package main

import (
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
	h.logs.Add("helper: ", "system VPN switch: disconnect requested")
	_ = h.conn.Emit(tun.Path, tun.Interface+".Requested", "disconnect")
	return nil
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
// plugin reports READY at once; it never changes routes or DNS.
func (h *helper) connmanConnect() {
	path, err := h.connmanProvider()
	if err != nil {
		h.logs.Add("helper: ", "system VPN integration unavailable: "+err.Error())
		return
	}
	var state string
	if v, err := h.conn.Object(vpnBus, path).GetProperty(vpnConn + ".State"); err == nil {
		state, _ = v.Value().(string)
	}
	if state == "ready" || state == "configuration" || state == "association" {
		return // already connected or being connected from the system UI
	}
	// connman-vpnd only accepts Connect from connmand: go through its service
	// object, vpn_<connection id>
	svc := dbus.ObjectPath("/net/connman/service/vpn_" + path[strings.LastIndex(string(path), "/")+1:])
	for try := 0; ; try++ {
		call := h.conn.Object("net.connman", svc).Call("net.connman.Service.Connect", 0)
		var de dbus.Error
		if call.Err == nil || errors.As(call.Err, &de) &&
			(de.Name == "net.connman.Error.InProgress" || de.Name == "net.connman.Error.AlreadyConnected") {
			return
		}
		// a just created connection reaches connmand a moment later
		if try == 4 || !strings.Contains(call.Err.Error(), "doesn't exist") {
			h.logs.Add("helper: ", "system VPN connect: "+call.Err.Error())
			return
		}
		time.Sleep(2 * time.Second)
	}
}
