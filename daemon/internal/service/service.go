// Package service implements the daemon logic behind the D-Bus API.
package service

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"

	"sailvless/internal/config/singbox"
	"sailvless/internal/config/xray"
	"sailvless/internal/core"
	"sailvless/internal/geo"
	"sailvless/internal/link"
	"sailvless/internal/store"
	"sailvless/internal/sub"
	"sailvless/internal/tun"
)

type Service struct {
	Store      *store.Store
	Logs       *core.LogBuf
	Runner     *core.Runner
	CoreDir    string // directory with xray / sing-box binaries
	RuntimeDir string // tmpfs dir for generated configs (0700)
	GeoDir     string // writable dir with geoip.dat / geosite.dat

	// OnState is called on every connection state change.
	OnState func(Status)

	mu        sync.Mutex
	activeID  string
	activeCfg store.Settings
	stats     *statsSource
	pings     map[string]int

	tunConn   *dbus.Conn
	tunActive bool
	tunState  tunStatus
	tunLostAt time.Time
}

var errNoReachable = errors.New("no reachable server")

type Status struct {
	State       core.State `json:"state"`
	Error       string     `json:"error,omitempty"`
	ProfileID   string     `json:"profileId,omitempty"`
	ProfileName string     `json:"profileName,omitempty"`
	Core        string     `json:"core,omitempty"`
	Since       int64      `json:"since,omitempty"` // unix seconds
	Listen      string     `json:"listen"`
	SocksPort   int        `json:"socksPort"`
	HTTPPort    int        `json:"httpPort"`
	Auth        bool       `json:"auth"`
	Tun         bool       `json:"tun"`
	Traffic     *Traffic   `json:"traffic,omitempty"`
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

// Import parses links (or a full Xray / sing-box JSON config) and stores them;
// returns imported IDs and per-line errors.
func (s *Service) Import(text string) ([]string, []string, error) {
	var ps []*link.Profile
	var errs []error
	if t := strings.TrimSpace(text); strings.HasPrefix(t, "{") || strings.HasPrefix(t, "[") {
		var err error
		if ps, _, err = sub.ParseJSON([]byte(t), false); err != nil {
			errs = append(errs, err)
		}
	} else {
		ps, errs = link.ParseMany(text)
	}
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
	Selected bool   `json:"selected"`
	Ping     *int   `json:"ping,omitempty"` // ms, PingFailed, PingNA; absent = not measured
	Core     string `json:"core"`
}

func (s *Service) Profiles() []ProfileView {
	sel := s.Store.Settings().SelectedID
	s.mu.Lock()
	pings := s.pings
	s.mu.Unlock()
	var out []ProfileView
	for _, p := range s.Store.Profiles() {
		v := ProfileView{Profile: p, Selected: p.ID == sel, Core: p.Core()}
		if ms, ok := pings[p.ID]; ok {
			v.Ping = &ms
		}
		v.UUID, v.Password, v.Raw, v.ObfsPassword = "", "", "", ""
		out = append(out, v)
	}
	return out
}

// SelectProfile marks a server as selected without connecting.
func (s *Service) SelectProfile(id string) error {
	if _, ok := s.Store.Profile(id); !ok {
		return errors.New("no such profile")
	}
	set := s.Store.Settings()
	set.SelectedID = id
	return s.Store.SetSettings(set)
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
	if set.SelectedID != id || !set.Connected {
		set.SelectedID, set.Connected = id, true
		if err := s.Store.SetSettings(set); err != nil {
			return err
		}
	}
	user, pass := "", ""
	if set.AuthEnabled {
		user, pass = s.Store.ProxyCredentials()
	}
	apiPort, err := freePort()
	if err != nil {
		return err
	}
	opts := xray.Options{
		Inbounds:  xray.Inbounds{Listen: set.Listen, SocksPort: set.SocksPort, HTTPPort: set.HTTPPort, User: user, Pass: pass},
		LogLevel:  set.LogLevel,
		APIListen: net.JoinHostPort("127.0.0.1", strconv.Itoa(apiPort)),
		APISecret: store.RandomToken(16),
	}
	if set.TunMode {
		opts.Mark = tun.Mark
	}
	coreName := p.Core()
	var cfg []byte
	var warnings []string
	switch {
	case coreName == "sing-box":
		cfg, warnings, err = singbox.Build(&p, opts)
	case p.Protocol == "xray-json":
		cfg, err = xray.BuildRaw(p.Raw, opts)
	default:
		cfg, err = xray.Build(&p, opts)
	}
	if err != nil {
		return err
	}
	for _, w := range warnings {
		s.Logs.Add("sailvlessd: ", "warning: "+w)
	}
	if coreName == "xray" && geo.NeedsGeo(cfg) {
		if missing := geo.Missing(s.GeoDir); len(missing) > 0 {
			s.Logs.Add("sailvlessd: ", fmt.Sprintf("config uses geo rules, downloading %v", missing))
			if err := s.UpdateGeo(); err != nil {
				return fmt.Errorf("config needs geo data and download failed: %w", err)
			}
		}
	}
	s.removeConfigs()
	s.mu.Lock()
	s.activeID, s.activeCfg = id, set
	s.stats = newStatsSource(coreName, s.CoreDir, apiPort, opts.APISecret)
	s.mu.Unlock()

	if set.TunMode {
		// the root helper runs the core (with CAP_NET_ADMIN for SO_MARK) and the tunnel
		s.Runner.Stop()
		s.Logs.Add("sailvlessd: ", fmt.Sprintf("connecting to %q via %s in TUN mode", p.Name, coreName))
		return s.startTun(&p, coreName, cfg, set, user, pass)
	}
	s.stopTun()

	path := filepath.Join(s.RuntimeDir, coreName+".json")
	if err := os.WriteFile(path, cfg, 0o600); err != nil {
		return err
	}
	s.Logs.Add("sailvlessd: ", fmt.Sprintf("connecting to %q via %s", p.Name, coreName))
	spec := core.Spec{
		Name:      coreName,
		Bin:       filepath.Join(s.CoreDir, coreName),
		Env:       os.Environ(),
		ReadyAddr: net.JoinHostPort(loopbackFor(set.Listen), strconv.Itoa(set.SocksPort)),
	}
	if coreName == "sing-box" {
		spec.TestArgs = []string{"check", "-c", path}
		spec.RunArgs = []string{"run", "-c", path, "-D", s.RuntimeDir}
	} else {
		spec.TestArgs = []string{"run", "-test", "-c", path}
		spec.RunArgs = []string{"run", "-c", path}
		spec.Env = append(spec.Env, "XRAY_LOCATION_ASSET="+s.GeoDir)
	}
	return s.Runner.Start(spec)
}

// UpdateGeo downloads geo databases: through the tunnel when it is up, else directly.
func (s *Service) UpdateGeo() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	var client *http.Client
	if c := s.proxyClient(); c != nil {
		c.Timeout = 5 * time.Minute
		client = c
	}
	start := time.Now()
	err := geo.Update(ctx, client, geo.Loyalsoldier, s.GeoDir)
	if err != nil && client != nil {
		s.Logs.Add("sailvlessd: ", fmt.Sprintf("geo update via proxy failed (%v), trying direct", err))
		err = geo.Update(ctx, nil, geo.Loyalsoldier, s.GeoDir)
	}
	if err != nil {
		s.Logs.Add("sailvlessd: ", "geo update failed: "+err.Error())
		return err
	}
	s.Logs.Add("sailvlessd: ", fmt.Sprintf("geo data updated from %s in %s (sha256 verified)", geo.Loyalsoldier.Name, time.Since(start).Round(time.Second)))
	return nil
}

func (s *Service) removeConfigs() {
	for _, c := range []string{"xray", "sing-box"} {
		_ = os.Remove(filepath.Join(s.RuntimeDir, c+".json"))
	}
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

// Disconnect is the user's "off": the tunnel stays down across daemon restarts.
func (s *Service) Disconnect() {
	set := s.Store.Settings()
	if set.Connected {
		set.Connected = false
		_ = s.Store.SetSettings(set)
	}
	s.Shutdown()
}

// Shutdown stops the core but keeps the user's intent (daemon exit, upgrade).
func (s *Service) Shutdown() {
	s.Runner.Stop()
	s.stopTun()
	s.mu.Lock()
	s.activeID, s.stats = "", nil
	s.mu.Unlock()
	s.removeConfigs()
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// StatusWithTraffic adds traffic counters (polled from the core on demand).
func (s *Service) StatusWithTraffic() Status {
	out := s.Status()
	s.mu.Lock()
	src := s.stats
	s.mu.Unlock()
	if src != nil && out.State == core.Connected {
		t := src.get()
		out.Traffic = &t
	}
	return out
}

func (s *Service) Status() Status {
	st, msg, since := s.Runner.Status()
	s.mu.Lock()
	id, set := s.activeID, s.activeCfg
	tunOn, ts := s.tunActive, s.tunState
	s.mu.Unlock()
	if tunOn {
		st, msg, since = ts.coreState(), ts.Error, time.Time{}
		if ts.Since > 0 {
			since = time.Unix(ts.Since, 0)
		}
	}
	if id == "" {
		set = s.Store.Settings()
	}
	out := Status{State: st, Error: msg, ProfileID: id, Listen: set.Listen, SocksPort: set.SocksPort, HTTPPort: set.HTTPPort, Auth: set.AuthEnabled, Tun: tunOn}
	if p, ok := s.Store.Profile(id); ok {
		out.ProfileName = p.Name
		out.Core = p.Core()
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
	for _, d := range []string{v.BootstrapDNS, v.RemoteDNS} {
		if net.ParseIP(d) == nil {
			return fmt.Errorf("DNS server must be an IP address: %q", d)
		}
	}
	if strings.TrimSpace(v.UserAgent) == "" {
		return fmt.Errorf("user agent must not be empty")
	}
	if v.HWIDManual != "" && !sub.HWIDPattern.MatchString(v.HWIDManual) {
		return fmt.Errorf("HWID must be 10-64 chars of A-Z a-z 0-9 = - (panel requirement)")
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
