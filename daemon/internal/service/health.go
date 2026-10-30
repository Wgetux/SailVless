package service

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"

	"sailvless/internal/core"
)

const (
	probeURL      = "http://cp.cloudflare.com/generate_204"
	probeInterval = 2 * time.Minute
	probeRetry    = 15 * time.Second
)

// probeState is the last liveness check, shown in Status.
type probeState struct {
	mu      sync.Mutex
	ms      int // last successful probe, ms
	failing bool
	at      time.Time
}

// probe fetches a 204 page through the local proxy (also in TUN mode).
func (s *Service) probe() (time.Duration, error) {
	c := s.proxyClient()
	if c == nil {
		return 0, fmt.Errorf("not connected")
	}
	c.Timeout = 10 * time.Second
	start := time.Now()
	resp, err := c.Get(probeURL)
	if err != nil {
		return 0, err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return time.Since(start), nil
}

func (s *Service) recordProbe(d time.Duration, err error) {
	s.probes.mu.Lock()
	defer s.probes.mu.Unlock()
	s.probes.at = time.Now()
	s.probes.failing = err != nil
	if err == nil {
		s.probes.ms = int(d.Milliseconds())
	}
}

// displayOn follows MCE: probing with the screen off would only drain the battery.
func displayWatcher(ctx context.Context) func() bool {
	var mu sync.Mutex
	on := true
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return func() bool { return true }
	}
	var st string
	if conn.Object("com.nokia.mce", "/com/nokia/mce/request").Call("com.nokia.mce.request.get_display_status", 0).Store(&st) == nil {
		on = st != "off"
	}
	_ = conn.AddMatchSignal(dbus.WithMatchInterface("com.nokia.mce.signal"), dbus.WithMatchMember("display_status_ind"))
	ch := make(chan *dbus.Signal, 8)
	conn.Signal(ch)
	go func() {
		for {
			select {
			case <-ctx.Done():
				conn.Close()
				return
			case sig := <-ch:
				if sig != nil && len(sig.Body) == 1 {
					v, _ := sig.Body[0].(string)
					mu.Lock()
					on = v != "off"
					mu.Unlock()
				}
			}
		}
	}()
	return func() bool {
		mu.Lock()
		defer mu.Unlock()
		return on
	}
}

// LivenessLoop checks that traffic really flows through the server: a running
// core with a dead or blocked server would otherwise look "connected".
// Failure: retry, then restart the core, then notify (and optionally switch
// to the fastest other server).
func (s *Service) LivenessLoop(ctx context.Context) {
	screenOn := displayWatcher(ctx)
	wait := func(d time.Duration) bool {
		select {
		case <-ctx.Done():
			return false
		case <-time.After(d):
			return true
		}
	}
	for wait(10 * time.Second) {
		set := s.Store.Settings()
		st := s.Status()
		if !set.HealthCheck || !set.Connected || st.State != core.Connected || !screenOn() {
			continue
		}
		// first check soon after (re)connecting, then every probeInterval
		s.probes.mu.Lock()
		last := s.probes.at
		s.probes.mu.Unlock()
		if last.Unix() >= st.Since && time.Since(last) < probeInterval {
			continue
		}
		// the user may disconnect or switch servers while a probe runs: a
		// failure of that old session must not bring it back
		same := func() bool {
			cur := s.Status()
			return s.Store.Settings().Connected && cur.ProfileID == st.ProfileID && cur.State != core.Stopped
		}
		d, err := s.probe()
		if err == nil {
			s.recordProbe(d, nil)
			continue
		}
		if !wait(probeRetry) {
			return
		}
		if d, err = s.probe(); err == nil {
			s.recordProbe(d, nil)
			continue
		}
		if !same() {
			continue
		}
		s.recordProbe(0, err)
		s.Logs.Add("sailvlessd: ", "liveness check failed twice ("+err.Error()+"), restarting the core")
		if st.Tun {
			err = s.restartTunCore()
		}
		if !st.Tun || err != nil {
			_ = s.Connect(st.ProfileID)
		}
		if !wait(20 * time.Second) {
			return
		}
		if d, err = s.probe(); err == nil {
			s.recordProbe(d, nil)
			s.Logs.Add("sailvlessd: ", "server answers again after the restart")
			continue
		}
		if !same() {
			continue
		}
		s.recordProbe(0, err)
		if set.HealthAutoSwitch {
			if id, e := s.connectBestExcept(st.ProfileID); e == nil {
				p, _ := s.Store.Profile(id)
				s.notify("switched", "SailVless: server not responding", tr("Switched to ")+p.Name)
				continue
			}
		}
		s.notify("dead", "SailVless: server not responding", st.ProfileName)
	}
}

// connectBestExcept connects to the fastest reachable server other than skip.
func (s *Service) connectBestExcept(skip string) (string, error) {
	best, bestMs := "", 1<<30
	for id, ms := range s.PingAll() {
		if id != skip && ms >= 0 && ms < bestMs {
			best, bestMs = id, ms
		}
	}
	if best == "" {
		return "", errNoReachable
	}
	s.Logs.Add("sailvlessd: ", fmt.Sprintf("switching to the fastest other server %s (%d ms)", best, bestMs))
	return best, s.Connect(best)
}
