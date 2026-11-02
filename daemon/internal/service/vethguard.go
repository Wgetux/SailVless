package service

import (
	"context"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"

	"sailvless/internal/tun"
)

// connman takes a container's veth for an Ethernet cable and prefers it over
// Wi-Fi and cellular: with no other network up the default route goes there
// and mobile data may not come back. The guard keeps connman off such
// interfaces through its own API, so no system file changes and real Ethernet
// is left alone.

const vethPrefix = "veth"

type connmanService struct {
	Type, State, Iface string
	AutoConnect        bool
}

// vethAction says what the guard must do with one connman service.
func vethAction(svc connmanService, ignore bool) (setAuto, auto, detach bool) {
	if svc.Type != "ethernet" || !strings.HasPrefix(svc.Iface, vethPrefix) {
		return false, false, false
	}
	if !ignore {
		return !svc.AutoConnect, true, false
	}
	return svc.AutoConnect, false, svc.State == "ready" || svc.State == "online"
}

func parseConnmanService(p map[string]dbus.Variant) connmanService {
	var svc connmanService
	svc.Type, _ = p["Type"].Value().(string)
	svc.State, _ = p["State"].Value().(string)
	svc.AutoConnect, _ = p["AutoConnect"].Value().(bool)
	if eth, ok := p["Ethernet"].Value().(map[string]dbus.Variant); ok {
		svc.Iface, _ = eth["Interface"].Value().(string)
	}
	return svc
}

// VethGuardLoop applies the setting now, on every change of connman's service
// list and when the setting changes.
func (s *Service) VethGuardLoop(ctx context.Context) {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		s.Logs.Add("sailvlessd: ", "virtual Ethernet guard unavailable: "+err.Error())
		return
	}
	defer conn.Close()
	if err := conn.AddMatchSignal(dbus.WithMatchInterface("net.connman.Manager"), dbus.WithMatchMember("ServicesChanged")); err != nil {
		s.Logs.Add("sailvlessd: ", "virtual Ethernet guard unavailable: "+err.Error())
		return
	}
	changed := make(chan *dbus.Signal, 16)
	conn.Signal(changed)
	for {
		s.guardVeth(ctx, conn)
		select {
		case <-ctx.Done():
			return
		case <-changed:
		case <-s.vethKick:
		}
		// connman sends these in bursts
		time.Sleep(300 * time.Millisecond)
		for len(changed) > 0 {
			<-changed
		}
	}
}

func (s *Service) guardVeth(ctx context.Context, conn *dbus.Conn) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var list []struct {
		Path  dbus.ObjectPath
		Props map[string]dbus.Variant
	}
	if err := conn.Object("net.connman", "/").CallWithContext(ctx, "net.connman.Manager.GetServices", 0).Store(&list); err != nil {
		return // connman is restarting: its next signal brings us back
	}
	ignore := s.Store.Settings().IgnoreVeth
	for _, e := range list {
		svc := parseConnmanService(e.Props)
		setAuto, auto, detach := vethAction(svc, ignore)
		if setAuto {
			err := conn.Object("net.connman", e.Path).CallWithContext(ctx, "net.connman.Service.SetProperty", 0, "AutoConnect", dbus.MakeVariant(auto)).Err
			if err != nil {
				s.Logs.Add("sailvlessd: ", "virtual Ethernet "+svc.Iface+": "+err.Error())
				continue
			}
			if auto {
				s.Logs.Add("sailvlessd: ", "virtual Ethernet "+svc.Iface+" is back under connman")
			} else {
				s.Logs.Add("sailvlessd: ", "ignoring virtual Ethernet "+svc.Iface)
			}
		}
		if detach {
			if err := conn.Object(tun.BusName, tun.Path).CallWithContext(ctx, tun.Interface+".DetachService", 0, string(e.Path)).Err; err != nil {
				s.Logs.Add("sailvlessd: ", "virtual Ethernet "+svc.Iface+": "+err.Error())
			}
		}
	}
}
