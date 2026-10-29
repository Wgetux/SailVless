package service

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"

	"sailvless/internal/core"
	"sailvless/internal/link"
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
