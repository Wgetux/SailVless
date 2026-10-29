package service

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// statsSource queries traffic counters of the running core. Counters are
// polled lazily (only when someone asks for the status), so an idle daemon
// costs nothing.
type statsSource struct {
	core   string // xray / sing-box
	addr   string // 127.0.0.1:port of the core API
	secret string // clash API secret (sing-box)
	bin    string

	mu         sync.Mutex
	lastPoll   time.Time
	up, down   int64
	upRate     int64 // bytes/s
	downRate   int64
	pollFailed bool
}

// Traffic is reported in Status.
type Traffic struct {
	Up       int64 `json:"up"`
	Down     int64 `json:"down"`
	UpRate   int64 `json:"upRate"`
	DownRate int64 `json:"downRate"`
}

func (s *statsSource) get() Traffic {
	s.mu.Lock()
	defer s.mu.Unlock()
	if time.Since(s.lastPoll) >= 900*time.Millisecond {
		up, down, err := s.query()
		now := time.Now()
		if err == nil {
			if !s.lastPoll.IsZero() && !s.pollFailed {
				dt := now.Sub(s.lastPoll).Seconds()
				s.upRate = int64(float64(max(up-s.up, 0)) / dt)
				s.downRate = int64(float64(max(down-s.down, 0)) / dt)
			}
			s.up, s.down = up, down
		}
		s.pollFailed = err != nil
		s.lastPoll = now
	}
	return Traffic{Up: s.up, Down: s.down, UpRate: s.upRate, DownRate: s.downRate}
}

func (s *statsSource) query() (int64, int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if s.core == "sing-box" {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+s.addr+"/connections", nil)
		req.Header.Set("Authorization", "Bearer "+s.secret)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return 0, 0, err
		}
		defer resp.Body.Close()
		var v struct {
			Up   int64 `json:"uploadTotal"`
			Down int64 `json:"downloadTotal"`
		}
		err = json.NewDecoder(resp.Body).Decode(&v)
		return v.Up, v.Down, err
	}
	out, err := exec.CommandContext(ctx, s.bin, "api", "statsquery", "--server="+s.addr, "-pattern", "outbound>>>").Output()
	if err != nil {
		return 0, 0, err
	}
	var v struct {
		Stat []struct {
			Name  string `json:"name"`
			Value int64  `json:"value"`
		} `json:"stat"`
	}
	if err := json.Unmarshal(out, &v); err != nil {
		return 0, 0, err
	}
	var up, down int64
	for _, st := range v.Stat {
		// outbound>>>TAG>>>traffic>>>uplink; count only proxy traffic
		parts := strings.Split(st.Name, ">>>")
		if len(parts) != 4 {
			continue
		}
		switch parts[1] {
		case "direct", "block", "api", "dns", "dns-out", "freedom", "blackhole":
			continue
		}
		if parts[3] == "uplink" {
			up += st.Value
		} else {
			down += st.Value
		}
	}
	return up, down, nil
}

func newStatsSource(coreName, coreDir string, port int, secret string) *statsSource {
	return &statsSource{core: coreName, addr: net.JoinHostPort("127.0.0.1", itoa(port)), secret: secret, bin: filepath.Join(coreDir, coreName)}
}
