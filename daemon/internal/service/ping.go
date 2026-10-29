package service

import (
	"encoding/json"
	"net"
	"strconv"
	"sync"
	"time"

	"sailvless/internal/link"
)

const (
	PingFailed = -1 // timeout / refused
	PingNA     = -2 // UDP-based protocol: TCP ping says nothing
)

// endpoint returns host:port to probe and whether a TCP probe is meaningful.
func endpoint(p *link.Profile) (string, bool) {
	addr, port := p.Address, p.Port
	switch p.Protocol {
	case "hysteria2", "tuic":
		return "", false
	case "xray-json":
		addr, port = xrayJSONEndpoint(p.Raw)
	case "singbox-json":
		if p.Transport.Type == "hysteria2" || p.Transport.Type == "tuic" || p.Transport.Type == "hysteria" || p.Transport.Type == "wireguard" {
			return "", false
		}
	}
	if addr == "" || port == 0 {
		return "", false
	}
	return net.JoinHostPort(addr, strconv.Itoa(port)), true
}

// xrayJSONEndpoint finds the server of the first proxy outbound in an Xray config.
func xrayJSONEndpoint(raw string) (string, int) {
	var cfg struct {
		Outbounds []struct {
			Protocol string `json:"protocol"`
			Settings struct {
				Address string `json:"address"`
				Port    int    `json:"port"`
				Vnext   []struct {
					Address string `json:"address"`
					Port    int    `json:"port"`
				} `json:"vnext"`
				Servers []struct {
					Address string `json:"address"`
					Port    int    `json:"port"`
				} `json:"servers"`
			} `json:"settings"`
		} `json:"outbounds"`
	}
	if json.Unmarshal([]byte(raw), &cfg) != nil {
		return "", 0
	}
	for _, o := range cfg.Outbounds {
		s := o.Settings
		switch {
		case s.Address != "":
			return s.Address, s.Port
		case len(s.Vnext) > 0:
			return s.Vnext[0].Address, s.Vnext[0].Port
		case len(s.Servers) > 0:
			return s.Servers[0].Address, s.Servers[0].Port
		}
	}
	return "", 0
}

// PingAll measures TCP connect time to every server (ms; see PingFailed/PingNA).
// Probes go directly, not through the tunnel.
func (s *Service) PingAll() map[string]int {
	profiles := s.Store.Profiles()
	res := make(map[string]int, len(profiles))
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 16)
	for i := range profiles {
		p := &profiles[i]
		addr, ok := endpoint(p)
		if !ok {
			res[p.ID] = PingNA
			continue
		}
		wg.Add(1)
		go func(id, addr string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			ms := PingFailed
			start := time.Now()
			if c, err := net.DialTimeout("tcp", addr, 3*time.Second); err == nil {
				ms = int(time.Since(start).Milliseconds())
				c.Close()
			}
			mu.Lock()
			res[id] = ms
			mu.Unlock()
		}(p.ID, addr)
	}
	wg.Wait()
	s.mu.Lock()
	s.pings = res
	s.mu.Unlock()
	return res
}

// ConnectBest pings all servers and connects to the fastest reachable one.
func (s *Service) ConnectBest() (string, error) {
	best, bestMs := "", 1<<30
	for id, ms := range s.PingAll() {
		if ms >= 0 && ms < bestMs {
			best, bestMs = id, ms
		}
	}
	if best == "" {
		return "", errNoReachable
	}
	return best, s.Connect(best)
}

func itoa(n int) string { return strconv.Itoa(n) }
