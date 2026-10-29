package service

import (
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"sailvless/internal/awg"
	"sailvless/internal/core"
	"sailvless/internal/link"
	"sailvless/internal/routing"
	"sailvless/internal/store"
	"sailvless/internal/tun"
)

// awgSettings mirrors cmd/sailvless-awg's settings file.
type awgSettings struct {
	Conf     string `json:"conf"`
	Listen   string `json:"listen"`
	User     string `json:"user"`
	Pass     string `json:"pass"`
	Mark     int    `json:"mark"`
	LogLevel string `json:"logLevel"`
}

// awgSidecar prepares the sidecar settings for an AmneziaWG profile and the
// profile Xray uses instead: a SOCKS5 proxy on loopback with random credentials.
func (s *Service) awgSidecar(p *link.Profile, set store.Settings) (link.Profile, []byte, error) {
	port, err := freePort()
	if err != nil {
		return link.Profile{}, nil, err
	}
	st := awgSettings{
		Conf:     p.Raw,
		Listen:   net.JoinHostPort("127.0.0.1", strconv.Itoa(port)),
		User:     "awg-" + store.RandomToken(4),
		Pass:     store.RandomToken(18),
		LogLevel: set.LogLevel,
	}
	if set.TunMode {
		st.Mark = tun.Mark // its UDP must bypass the tunnel
	}
	b, err := json.Marshal(st)
	if err != nil {
		return link.Profile{}, nil, err
	}
	chained := link.Profile{
		ID: p.ID, Name: p.Name, Protocol: "socks", Address: "127.0.0.1", Port: port,
		UUID: st.User, Password: st.Pass,
		Transport: link.Transport{Type: "tcp"}, Security: link.Security{Type: "none"},
	}
	return chained, b, nil
}

// awgRouting adapts routing to an AmneziaWG config:
//   - split configs (AllowedIPs without 0.0.0.0/0, e.g. AntiZapret): those
//     networks go through the tunnel, everything else direct, like wg-quick;
//     the user's own rules still come first;
//   - the config's DNS server is used through the tunnel (AntiZapret's DNS
//     decides what is blocked).
//
// It also returns the private networks among a split config's AllowedIPs
// (e.g. AntiZapret's 10.30.0.0/15 fake addresses): the TUN must route them
// into the tunnel although LAN ranges are excluded otherwise.
func awgRouting(p *link.Profile, rc routing.Config) (routing.Config, string, []string) {
	cfg, err := awg.Parse(p.Raw)
	if err != nil {
		return rc, "", nil
	}
	var include []string
	dns := ""
	if len(cfg.DNS) > 0 {
		dns = cfg.DNS[0].String()
		rc.DNSRemote = "tcp://" + dns
	}
	if split, nets := cfg.Split(); split {
		rules := append([]routing.Rule(nil), rc.Rules...)
		for _, n := range nets {
			rules = append(rules, routing.Rule{Kind: "ip", Value: n, Action: "proxy"})
			if pfx, err := netip.ParsePrefix(n); err == nil && pfx.Addr().IsPrivate() {
				include = append(include, pfx.Masked().String())
			}
		}
		rc.Rules, rc.Mode, rc.CustomFinal = rules, routing.Custom, "direct"
	}
	return rc, dns, include
}

// ImportFile imports configs from a file chosen in the UI (AmneziaWG/WireGuard
// .conf, links, JSON). An AWG profile without a "# Name" is named after the file.
func (s *Service) ImportFile(path string) ([]string, []string, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, nil, err
	}
	if !st.Mode().IsRegular() || st.Size() > 1<<20 {
		return nil, nil, fmt.Errorf("%s: not a regular file up to 1 MB", filepath.Base(path))
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	text := string(b)
	if awg.LooksLikeConf(text) {
		p, err := link.ParseAWG(text)
		if err != nil {
			return nil, []string{filepath.Base(path) + ": " + err.Error()}, nil
		}
		if c, _ := awg.Parse(text); c != nil && c.Name == "" {
			p.Name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		}
		if err := s.Store.Upsert([]*link.Profile{p}); err != nil {
			return nil, nil, err
		}
		s.Logs.Add("sailvlessd: ", fmt.Sprintf("imported %q from a file", p.Name))
		return []string{p.ID}, nil, nil
	}
	return s.Import(text)
}

func sidecarSpec(coreDir, settingsPath string, settings []byte) (core.Spec, error) {
	var st awgSettings
	if err := json.Unmarshal(settings, &st); err != nil {
		return core.Spec{}, err
	}
	return core.Spec{
		Name:      "awg",
		Bin:       filepath.Join(coreDir, "sailvless-awg"),
		RunArgs:   []string{"-c", settingsPath},
		ReadyAddr: st.Listen,
	}, nil
}

// startSidecar runs sailvless-awg in proxy mode (nil settings: stop it).
func (s *Service) startSidecar(settings []byte) error {
	if settings == nil {
		s.Sidecar.Stop()
		return nil
	}
	path := filepath.Join(s.RuntimeDir, "awg.json")
	if err := os.WriteFile(path, settings, 0o600); err != nil {
		return err
	}
	spec, err := sidecarSpec(s.CoreDir, path, settings)
	if err != nil {
		return err
	}
	spec.Env = os.Environ()
	if err := s.Sidecar.Start(spec); err != nil {
		return fmt.Errorf("amneziawg: %w", err)
	}
	return nil
}
