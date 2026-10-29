package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"

	"sailvless/internal/core"
	"sailvless/internal/link"
	"sailvless/internal/store"
	"sailvless/internal/tun"
)

// tunStatus mirrors the helper's StateChanged payload.
type tunStatus struct {
	State string `json:"state"`
	Error string `json:"error,omitempty"`
	Since int64  `json:"since,omitempty"`
}

func (t tunStatus) coreState() core.State {
	switch t.State {
	case "connected":
		return core.Connected
	case "connecting":
		return core.Connecting
	case "error":
		return core.Failed
	}
	return core.Stopped
}

// tunBus returns the system bus connection, subscribing to helper signals once.
// The connection stays open: the helper stops the tunnel when it goes away.
func (s *Service) tunBus() (*dbus.Conn, error) {
	s.mu.Lock()
	c := s.tunConn
	s.mu.Unlock()
	if c != nil && c.Connected() {
		return c, nil
	}
	c, err := dbus.ConnectSystemBus()
	if err != nil {
		return nil, fmt.Errorf("system bus: %w", err)
	}
	if err := c.AddMatchSignal(dbus.WithMatchObjectPath(tun.Path), dbus.WithMatchInterface(tun.Interface), dbus.WithMatchMember("StateChanged")); err != nil {
		c.Close()
		return nil, err
	}
	// notice the helper disappearing (crash, kill): its routes are cleaned by
	// ExecStopPost, but the UI must not keep showing "connected"
	if err := c.AddMatchSignal(dbus.WithMatchInterface("org.freedesktop.DBus"), dbus.WithMatchMember("NameOwnerChanged"), dbus.WithMatchArg(0, tun.BusName)); err != nil {
		c.Close()
		return nil, err
	}
	ch := make(chan *dbus.Signal, 16)
	c.Signal(ch)
	go func() {
		for sig := range ch {
			if sig.Name == "org.freedesktop.DBus.NameOwnerChanged" {
				if len(sig.Body) == 3 && sig.Body[2] == "" {
					s.helperLost()
				}
				continue
			}
			if len(sig.Body) != 1 {
				continue
			}
			js, _ := sig.Body[0].(string)
			var st tunStatus
			if json.Unmarshal([]byte(js), &st) != nil {
				continue
			}
			s.mu.Lock()
			active := s.tunActive
			if active {
				s.tunState = st
			}
			s.mu.Unlock()
			if active && s.OnState != nil {
				s.OnState(s.Status())
			}
		}
	}()
	s.mu.Lock()
	s.tunConn = c
	s.mu.Unlock()
	return c, nil
}

func (s *Service) startTun(p *link.Profile, coreName string, cfg []byte, set store.Settings, user, pass string) error {
	c, err := s.tunBus()
	if err != nil {
		return err
	}
	params := tun.Params{
		Core: coreName, CoreConfig: string(cfg), AssetDir: s.GeoDir,
		SocksPort: set.SocksPort, User: user, Pass: pass,
		ServerDomains: serverDomains(p),
		BootstrapDNS:  set.BootstrapDNS, RemoteDNS: set.RemoteDNS,
		IPv6: set.TunIPv6,
	}
	b, _ := json.Marshal(params)
	s.mu.Lock()
	s.tunActive, s.tunState = true, tunStatus{State: "connecting"}
	s.mu.Unlock()
	if s.OnState != nil {
		s.OnState(s.Status())
	}
	call := c.Object(tun.BusName, tun.Path).Call(tun.Interface+".Start", 0, string(b))
	if call.Err != nil {
		msg := call.Err.Error()
		var de dbus.Error
		if errors.As(call.Err, &de) && len(de.Body) > 0 {
			msg = fmt.Sprint(de.Body[0])
		}
		s.mu.Lock()
		s.tunState = tunStatus{State: "error", Error: "TUN: " + msg}
		s.mu.Unlock()
		if s.OnState != nil {
			s.OnState(s.Status())
		}
		return errors.New("TUN: " + msg)
	}
	return nil
}

// helperLost marks the tunnel as failed and retries once; repeated losses
// within a minute are left as an error for the user to see.
func (s *Service) helperLost() {
	s.mu.Lock()
	if !s.tunActive {
		s.mu.Unlock()
		return
	}
	s.tunState = tunStatus{State: "error", Error: "TUN helper stopped unexpectedly"}
	retry := time.Since(s.tunLostAt) > time.Minute
	s.tunLostAt = time.Now()
	id := s.activeID
	s.mu.Unlock()
	s.Logs.Add("sailvlessd: ", "TUN helper left the bus")
	if s.OnState != nil {
		s.OnState(s.Status())
	}
	if !retry {
		return
	}
	go func() {
		time.Sleep(3 * time.Second)
		s.mu.Lock()
		still := s.tunActive && s.activeID == id
		s.mu.Unlock()
		if still && s.Store.Settings().Connected {
			s.Logs.Add("sailvlessd: ", "restarting the tunnel")
			if err := s.Connect(id); err != nil {
				s.Logs.Add("sailvlessd: ", "restart failed: "+err.Error())
			}
		}
	}()
}

func (s *Service) stopTun() {
	s.mu.Lock()
	active, c := s.tunActive, s.tunConn
	s.tunActive, s.tunState = false, tunStatus{State: "stopped"}
	s.mu.Unlock()
	if !active || c == nil {
		return
	}
	if call := c.Object(tun.BusName, tun.Path).Call(tun.Interface+".Stop", 0); call.Err != nil {
		s.Logs.Add("sailvlessd: ", "TUN stop: "+call.Err.Error())
	}
}

func (s *Service) restartTunCore() error {
	s.mu.Lock()
	c := s.tunConn
	s.mu.Unlock()
	if c == nil {
		return errors.New("no helper connection")
	}
	return c.Object(tun.BusName, tun.Path).Call(tun.Interface+".RestartCore", 0).Err
}

// helperLogs returns the helper's log (core + tunnel) while TUN mode is used.
func (s *Service) helperLogs(n int) string {
	s.mu.Lock()
	active, c := s.tunActive, s.tunConn
	s.mu.Unlock()
	if !active || c == nil {
		return ""
	}
	var text string
	if err := c.Object(tun.BusName, tun.Path).Call(tun.Interface+".Logs", 0, int32(n)).Store(&text); err != nil {
		return "TUN helper log unavailable: " + err.Error()
	}
	return text
}

// LogsText is the daemon log plus, in TUN mode, the helper's.
func (s *Service) LogsText(n int) string {
	text := strings.Join(s.Logs.Tail(n), "\n")
	if h := s.helperLogs(n); h != "" {
		text += "\n--- TUN helper ---\n" + h
	}
	return text
}

// serverDomains lists proxy server host names: they must be resolved outside
// the tunnel, or resolving them would need the tunnel itself.
func serverDomains(p *link.Profile) []string {
	set := map[string]bool{}
	add := func(h string) {
		if h != "" && net.ParseIP(h) == nil {
			set[strings.ToLower(h)] = true
		}
	}
	add(p.Address)
	if p.Raw != "" {
		var cfg struct {
			Outbounds []struct {
				Server   string `json:"server"` // sing-box
				Settings struct {
					Address string `json:"address"`
					Vnext   []struct {
						Address string `json:"address"`
					} `json:"vnext"`
					Servers []struct {
						Address string `json:"address"`
					} `json:"servers"`
				} `json:"settings"`
			} `json:"outbounds"`
		}
		if json.Unmarshal([]byte(p.Raw), &cfg) == nil {
			for _, o := range cfg.Outbounds {
				add(o.Server)
				add(o.Settings.Address)
				for _, v := range o.Settings.Vnext {
					add(v.Address)
				}
				for _, v := range o.Settings.Servers {
					add(v.Address)
				}
			}
		}
	}
	out := make([]string, 0, len(set))
	for d := range set {
		out = append(out, d)
	}
	return out
}
