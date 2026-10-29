// Package service implements the daemon logic behind the D-Bus API.
package service

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"sailvless/internal/config/xray"
	"sailvless/internal/core"
	"sailvless/internal/link"
	"sailvless/internal/store"
)

type Service struct {
	Store      *store.Store
	Logs       *core.LogBuf
	Runner     *core.Runner
	CoreDir    string // directory with xray / sing-box binaries
	RuntimeDir string // tmpfs dir for generated configs (0700)

	// OnState is called on every connection state change.
	OnState func(Status)

	mu        sync.Mutex
	activeID  string
	activeCfg store.Settings
}

type Status struct {
	State       core.State `json:"state"`
	Error       string     `json:"error,omitempty"`
	ProfileID   string     `json:"profileId,omitempty"`
	ProfileName string     `json:"profileName,omitempty"`
	Since       int64      `json:"since,omitempty"` // unix seconds
	Listen      string     `json:"listen"`
	SocksPort   int        `json:"socksPort"`
	HTTPPort    int        `json:"httpPort"`
	Auth        bool       `json:"auth"`
}

func New(st *store.Store, coreDir, runtimeDir string) *Service {
	logs := core.NewLogBuf(1000)
	s := &Service{Store: st, Logs: logs, Runner: core.NewRunner(logs), CoreDir: coreDir, RuntimeDir: runtimeDir}
	s.Runner.OnChange = func(core.State, string) {
		if s.OnState != nil {
			s.OnState(s.Status())
		}
	}
	return s
}

// Import parses links and stores them; returns imported IDs and per-line errors.
func (s *Service) Import(text string) ([]string, []string, error) {
	ps, errs := link.ParseMany(text)
	var msgs []string
	for _, e := range errs {
		msgs = append(msgs, e.Error())
	}
	if len(ps) == 0 {
		if len(msgs) == 0 {
			msgs = append(msgs, "no links found")
		}
		return nil, msgs, nil
	}
	if err := s.Store.Upsert(ps); err != nil {
		return nil, msgs, err
	}
	ids := make([]string, len(ps))
	for i, p := range ps {
		ids[i] = p.ID
	}
	s.Logs.Add("sailvlessd: ", fmt.Sprintf("imported %d profile(s), %d error(s)", len(ps), len(msgs)))
	return ids, msgs, nil
}

// ProfileView is a profile without credentials, for the UI.
type ProfileView struct {
	link.Profile
	Selected bool `json:"selected"`
}

func (s *Service) Profiles() []ProfileView {
	sel := s.Store.Settings().SelectedID
	var out []ProfileView
	for _, p := range s.Store.Profiles() {
		p.UUID, p.Password = "", ""
		out = append(out, ProfileView{Profile: p, Selected: p.ID == sel})
	}
	return out
}

func (s *Service) DeleteProfile(id string) error {
	s.mu.Lock()
	active := s.activeID == id
	s.mu.Unlock()
	if active {
		s.Disconnect()
	}
	return s.Store.Delete(id)
}

// Connect starts the core for profile id ("" = selected profile).
func (s *Service) Connect(id string) error {
	set := s.Store.Settings()
	if id == "" {
		id = set.SelectedID
	}
	p, ok := s.Store.Profile(id)
	if !ok {
		return errors.New("no such profile")
	}
	if set.SelectedID != id {
		set.SelectedID = id
		if err := s.Store.SetSettings(set); err != nil {
			return err
		}
	}
	user, pass := "", ""
	if set.AuthEnabled {
		user, pass = s.Store.ProxyCredentials()
	}
	cfg, err := xray.Build(&p, xray.Options{
		Inbounds: xray.Inbounds{Listen: set.Listen, SocksPort: set.SocksPort, HTTPPort: set.HTTPPort, User: user, Pass: pass},
		LogLevel: set.LogLevel,
	})
	if err != nil {
		return err
	}
	path := filepath.Join(s.RuntimeDir, "xray.json")
	if err := os.WriteFile(path, cfg, 0o600); err != nil {
		return err
	}
	s.mu.Lock()
	s.activeID, s.activeCfg = id, set
	s.mu.Unlock()
	s.Logs.Add("sailvlessd: ", fmt.Sprintf("connecting to %q via xray", p.Name))
	bin := filepath.Join(s.CoreDir, "xray")
	return s.Runner.Start(core.Spec{
		Name:      "xray",
		Bin:       bin,
		TestArgs:  []string{"run", "-test", "-c", path},
		RunArgs:   []string{"run", "-c", path},
		Env:       append(os.Environ(), "XRAY_LOCATION_ASSET="+s.CoreDir),
		ReadyAddr: net.JoinHostPort(loopbackFor(set.Listen), strconv.Itoa(set.SocksPort)),
	})
}

// loopbackFor returns an address to probe a listener bound to listen.
func loopbackFor(listen string) string {
	switch listen {
	case "", "0.0.0.0":
		return "127.0.0.1"
	case "::":
		return "::1"
	}
	return listen
}

func (s *Service) Disconnect() {
	s.Runner.Stop()
	s.mu.Lock()
	s.activeID = ""
	s.mu.Unlock()
	_ = os.Remove(filepath.Join(s.RuntimeDir, "xray.json"))
}

func (s *Service) Status() Status {
	st, msg, since := s.Runner.Status()
	s.mu.Lock()
	id, set := s.activeID, s.activeCfg
	s.mu.Unlock()
	if id == "" {
		set = s.Store.Settings()
	}
	out := Status{State: st, Error: msg, ProfileID: id, Listen: set.Listen, SocksPort: set.SocksPort, HTTPPort: set.HTTPPort, Auth: set.AuthEnabled}
	if p, ok := s.Store.Profile(id); ok {
		out.ProfileName = p.Name
	}
	if !since.IsZero() {
		out.Since = since.Unix()
	}
	return out
}

// SetSettings validates and saves settings, reconnecting if a session is active.
func (s *Service) SetSettings(v store.Settings) error {
	if err := validate(v); err != nil {
		return err
	}
	if err := s.Store.SetSettings(v); err != nil {
		return err
	}
	return s.reconnectIfActive()
}

func (s *Service) RegenerateCredentials() error {
	if err := s.Store.RegenerateCredentials(); err != nil {
		return err
	}
	return s.reconnectIfActive()
}

func (s *Service) reconnectIfActive() error {
	s.mu.Lock()
	id := s.activeID
	s.mu.Unlock()
	if id == "" {
		return nil
	}
	return s.Connect(id)
}

func validate(v store.Settings) error {
	if ip := net.ParseIP(v.Listen); ip == nil {
		return fmt.Errorf("listen: invalid IP %q", v.Listen)
	}
	if v.SocksPort < 1024 || v.SocksPort > 65535 {
		return fmt.Errorf("socks port must be 1024-65535")
	}
	if v.HTTPPort != 0 && (v.HTTPPort < 1024 || v.HTTPPort > 65535 || v.HTTPPort == v.SocksPort) {
		return fmt.Errorf("http port must be 1024-65535 and differ from socks port (0 disables)")
	}
	switch v.LogLevel {
	case "debug", "info", "warning", "error", "none":
	default:
		return fmt.Errorf("bad log level %q", v.LogLevel)
	}
	return nil
}

// RandomPorts picks two currently free loopback ports in 20000-60000.
func RandomPorts() (int, int, error) {
	var ports []int
	for len(ports) < 2 {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return 0, 0, err
		}
		p := l.Addr().(*net.TCPAddr).Port
		l.Close()
		if p >= 20000 && p <= 60000 && (len(ports) == 0 || ports[0] != p) {
			ports = append(ports, p)
		}
	}
	return ports[0], ports[1], nil
}

// Uptime is a helper for logs/tests.
func Uptime(since int64) time.Duration {
	if since == 0 {
		return 0
	}
	return time.Since(time.Unix(since, 0)).Round(time.Second)
}
