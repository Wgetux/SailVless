package service

import (
	"testing"

	"github.com/godbus/dbus/v5"
)

func TestVethAction(t *testing.T) {
	veth := func(state string, auto bool) connmanService {
		return connmanService{Type: "ethernet", Iface: "vethAbC", State: state, AutoConnect: auto}
	}
	for _, c := range []struct {
		name                string
		svc                 connmanService
		ignore              bool
		setAuto, auto, drop bool
	}{
		{"new veth", veth("configuration", true), true, true, false, false},
		{"connected veth", veth("ready", true), true, true, false, true},
		{"quiet veth", veth("idle", false), true, false, false, false},
		{"connected, autoconnect already off", veth("ready", false), true, false, false, true},
		{"setting off", veth("idle", false), false, true, true, false},
		{"setting off, nothing to undo", veth("ready", true), false, false, true, false},
		{"real cable", connmanService{Type: "ethernet", Iface: "eth0", State: "online", AutoConnect: true}, true, false, false, false},
		{"Wi-Fi", connmanService{Type: "wifi", State: "online", AutoConnect: true}, true, false, false, false},
	} {
		setAuto, auto, drop := vethAction(c.svc, c.ignore)
		if setAuto != c.setAuto || auto != c.auto || drop != c.drop {
			t.Errorf("%s: got %v %v %v", c.name, setAuto, auto, drop)
		}
	}
}

func TestParseConnmanService(t *testing.T) {
	got := parseConnmanService(map[string]dbus.Variant{
		"Type": dbus.MakeVariant("ethernet"), "State": dbus.MakeVariant("ready"), "AutoConnect": dbus.MakeVariant(true),
		"Ethernet": dbus.MakeVariant(map[string]dbus.Variant{"Interface": dbus.MakeVariant("vethAbC")}),
	})
	if got != (connmanService{Type: "ethernet", State: "ready", Iface: "vethAbC", AutoConnect: true}) {
		t.Fatalf("%+v", got)
	}
	if got := parseConnmanService(map[string]dbus.Variant{}); got != (connmanService{}) {
		t.Fatalf("empty: %+v", got)
	}
}
