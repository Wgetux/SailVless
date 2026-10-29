// Package store persists profiles, settings and secrets under
// $XDG_DATA_HOME/sailvless. Credentials live only in secrets.json (0600).
package store

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"sailvless/internal/link"
	"sailvless/internal/routing"
	"sailvless/internal/sub"
)

type Settings struct {
	SelectedID  string `json:"selectedId"`
	Connected   bool   `json:"connected"` // user wants the tunnel up; restored on daemon start
	Listen      string `json:"listen"`
	SocksPort   int    `json:"socksPort"`
	HTTPPort    int    `json:"httpPort"`
	AuthEnabled bool   `json:"authEnabled"`
	LogLevel    string `json:"logLevel"`

	TunMode bool `json:"tunMode"` // route all device traffic through the root TUN helper
	// proxy IPv6 inside the tunnel (else reject it and hand out IPv4 only);
	// the key was renamed from "tunIpv6" when the default became off
	TunIPv6      bool   `json:"tunIpv6Proxy"`
	BootstrapDNS string `json:"bootstrapDns"`
	RemoteDNS    string `json:"remoteDns"`

	HealthCheck      bool `json:"healthCheck"`      // probe through the server every 2 min (screen on)
	HealthAutoSwitch bool `json:"healthAutoSwitch"` // switch to the fastest other server when it stays dead

	Routing       routing.Config `json:"routing"`
	RouteImported bool           `json:"routeImported"` // apply Routing to imported full configs too

	UserAgent   string `json:"userAgent"`
	HWIDEnabled bool   `json:"hwidEnabled"`
	HWIDManual  string `json:"hwidManual"` // overrides the computed HWID when set
}

const DefaultUserAgent = "SailVless/0.2"

// Subscription is stored without its URL (that lives in secrets.json).
type Subscription struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	AutoUpdate    bool     `json:"autoUpdate"`
	ViaProxy      bool     `json:"viaProxy"`      // fetch through the running tunnel first
	IntervalHours int      `json:"intervalHours"` // 0 = panel's profile-update-interval, else 24
	Meta          sub.Meta `json:"meta"`
	LastUpdate    int64    `json:"lastUpdate"`
	LastError     string   `json:"lastError,omitempty"`
	Count         int      `json:"count"`
}

type profileSecret struct {
	UUID     string `json:"uuid,omitempty"`
	Password string `json:"password,omitempty"`
	Raw      string `json:"raw,omitempty"`
}

type secrets struct {
	ProxyUser string                   `json:"proxyUser"`
	ProxyPass string                   `json:"proxyPass"`
	HWIDSalt  string                   `json:"hwidSalt"`
	SubURLs   map[string]string        `json:"subscriptions"`
	Profiles  map[string]profileSecret `json:"profiles"`
}

type Store struct {
	dir      string
	mu       sync.Mutex
	profiles map[string]*link.Profile
	subs     map[string]*Subscription
	settings Settings
	secrets  secrets
}

func DefaultDir() string {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, "sailvless")
}

func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, err
	}
	s := &Store{
		dir:      dir,
		profiles: map[string]*link.Profile{},
		subs:     map[string]*Subscription{},
		settings: Settings{Listen: "127.0.0.1", SocksPort: 10808, HTTPPort: 10809, AuthEnabled: true, LogLevel: "warning",
			UserAgent: DefaultUserAgent, HWIDEnabled: true,
			BootstrapDNS: "1.1.1.1", HealthCheck: true, RemoteDNS: "1.1.1.1", Routing: routing.Default()},
	}
	var list []*link.Profile
	var subs []*Subscription
	for name, v := range map[string]any{"profiles.json": &list, "subscriptions.json": &subs, "settings.json": &s.settings, "secrets.json": &s.secrets} {
		if err := s.readJSON(name, v); err != nil {
			return nil, err
		}
	}
	if s.secrets.Profiles == nil {
		s.secrets.Profiles = map[string]profileSecret{}
	}
	if s.secrets.SubURLs == nil {
		s.secrets.SubURLs = map[string]string{}
	}
	for _, p := range list {
		sec := s.secrets.Profiles[p.ID]
		p.UUID, p.Password, p.Raw = sec.UUID, sec.Password, sec.Raw
		s.profiles[p.ID] = p
	}
	for _, sb := range subs {
		s.subs[sb.ID] = sb
	}
	dirty := false
	if s.secrets.ProxyUser == "" {
		s.secrets.ProxyUser = "sv-" + RandomToken(4)
		s.secrets.ProxyPass = RandomToken(18)
		dirty = true
	}
	if s.secrets.HWIDSalt == "" {
		s.secrets.HWIDSalt = RandomToken(16)
		dirty = true
	}
	if dirty {
		if err := s.saveLocked(); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// RandomToken returns n random bytes as URL-safe base64.
func RandomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func (s *Store) readJSON(name string, v any) error {
	b, err := os.ReadFile(filepath.Join(s.dir, name))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// writeJSON writes atomically with mode 0600.
func (s *Store) writeJSON(name string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.dir, name+".tmp*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(s.dir, name))
}

func (s *Store) saveLocked() error {
	list := make([]*link.Profile, 0, len(s.profiles))
	s.secrets.Profiles = map[string]profileSecret{}
	for _, p := range s.sortedLocked() {
		c := *p
		if c.UUID != "" || c.Password != "" || c.Raw != "" {
			s.secrets.Profiles[c.ID] = profileSecret{UUID: c.UUID, Password: c.Password, Raw: c.Raw}
		}
		c.UUID, c.Password, c.Raw = "", "", ""
		list = append(list, &c)
	}
	if err := s.writeJSON("secrets.json", s.secrets); err != nil {
		return err
	}
	if err := s.writeJSON("profiles.json", list); err != nil {
		return err
	}
	if err := s.writeJSON("subscriptions.json", s.subsLocked()); err != nil {
		return err
	}
	return s.writeJSON("settings.json", s.settings)
}

func (s *Store) subsLocked() []*Subscription {
	list := make([]*Subscription, 0, len(s.subs))
	for _, sb := range s.subs {
		list = append(list, sb)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	return list
}

// Subscriptions returns copies (without URLs).
func (s *Store) Subscriptions() []Subscription {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Subscription
	for _, sb := range s.subsLocked() {
		out = append(out, *sb)
	}
	return out
}

func (s *Store) Subscription(id string) (Subscription, string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sb, ok := s.subs[id]
	if !ok {
		return Subscription{}, "", false
	}
	return *sb, s.secrets.SubURLs[id], true
}

// SaveSubscription adds or updates a subscription; url == "" keeps the stored one.
func (s *Store) SaveSubscription(sb Subscription, url string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := sb
	s.subs[sb.ID] = &c
	if url != "" {
		s.secrets.SubURLs[sb.ID] = url
	}
	return s.saveLocked()
}

// DeleteSubscription removes the subscription and its profiles.
func (s *Store) DeleteSubscription(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.subs, id)
	delete(s.secrets.SubURLs, id)
	for pid, p := range s.profiles {
		if p.SubID == id {
			delete(s.profiles, pid)
		}
	}
	if _, ok := s.profiles[s.settings.SelectedID]; !ok {
		s.settings.SelectedID = ""
	}
	return s.saveLocked()
}

// ReplaceSubProfiles swaps all profiles of a subscription for ps.
// The selection survives when the selected server is still present.
func (s *Store) ReplaceSubProfiles(subID string, ps []*link.Profile) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for pid, p := range s.profiles {
		if p.SubID == subID {
			delete(s.profiles, pid)
		}
	}
	for _, p := range ps {
		c := *p
		c.SubID = subID
		s.profiles[c.ID] = &c
	}
	if _, ok := s.profiles[s.settings.SelectedID]; !ok {
		s.settings.SelectedID = ""
		if len(ps) > 0 {
			s.settings.SelectedID = ps[0].ID
		}
	}
	return s.saveLocked()
}

func (s *Store) HWIDSalt() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.secrets.HWIDSalt
}

// ResetHWIDSalt makes the computed HWID change (the panel will see a new device).
func (s *Store) ResetHWIDSalt() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.secrets.HWIDSalt = RandomToken(16)
	return s.saveLocked()
}

func (s *Store) sortedLocked() []*link.Profile {
	list := make([]*link.Profile, 0, len(s.profiles))
	for _, p := range s.profiles {
		list = append(list, p)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Name != list[j].Name {
			return list[i].Name < list[j].Name
		}
		return list[i].ID < list[j].ID
	})
	return list
}

// Profiles returns copies sorted by name.
func (s *Store) Profiles() []link.Profile {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []link.Profile
	for _, p := range s.sortedLocked() {
		out = append(out, *p)
	}
	return out
}

func (s *Store) Profile(id string) (link.Profile, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.profiles[id]
	if !ok {
		return link.Profile{}, false
	}
	return *p, true
}

// Upsert adds or replaces profiles (matched by ID).
func (s *Store) Upsert(ps []*link.Profile) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range ps {
		c := *p
		s.profiles[p.ID] = &c
	}
	if s.settings.SelectedID == "" && len(ps) > 0 {
		s.settings.SelectedID = ps[0].ID
	}
	return s.saveLocked()
}

// Rename changes a profile's display name.
func (s *Store) Rename(id, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.profiles[id]
	if !ok {
		return errors.New("no such profile")
	}
	p.Name = name
	return s.saveLocked()
}

func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.profiles, id)
	if s.settings.SelectedID == id {
		s.settings.SelectedID = ""
	}
	return s.saveLocked()
}

func (s *Store) Settings() Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.settings
}

func (s *Store) SetSettings(v Settings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.settings = v
	return s.saveLocked()
}

// ProxyCredentials returns the local inbound login/password.
func (s *Store) ProxyCredentials() (string, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.secrets.ProxyUser, s.secrets.ProxyPass
}

// RegenerateCredentials creates a new random local inbound login/password.
func (s *Store) RegenerateCredentials() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.secrets.ProxyUser = "sv-" + RandomToken(4)
	s.secrets.ProxyPass = RandomToken(18)
	return s.saveLocked()
}
