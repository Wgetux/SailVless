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
)

type Settings struct {
	SelectedID  string `json:"selectedId"`
	Listen      string `json:"listen"`
	SocksPort   int    `json:"socksPort"`
	HTTPPort    int    `json:"httpPort"`
	AuthEnabled bool   `json:"authEnabled"`
	LogLevel    string `json:"logLevel"`
}

type profileSecret struct {
	UUID     string `json:"uuid,omitempty"`
	Password string `json:"password,omitempty"`
}

type secrets struct {
	ProxyUser string                   `json:"proxyUser"`
	ProxyPass string                   `json:"proxyPass"`
	Profiles  map[string]profileSecret `json:"profiles"`
}

type Store struct {
	dir      string
	mu       sync.Mutex
	profiles map[string]*link.Profile
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
		settings: Settings{Listen: "127.0.0.1", SocksPort: 10808, HTTPPort: 10809, AuthEnabled: true, LogLevel: "warning"},
		secrets:  secrets{Profiles: map[string]profileSecret{}},
	}
	var list []*link.Profile
	for name, v := range map[string]any{"profiles.json": &list, "settings.json": &s.settings, "secrets.json": &s.secrets} {
		if err := s.readJSON(name, v); err != nil {
			return nil, err
		}
	}
	if s.secrets.Profiles == nil {
		s.secrets.Profiles = map[string]profileSecret{}
	}
	for _, p := range list {
		sec := s.secrets.Profiles[p.ID]
		p.UUID, p.Password = sec.UUID, sec.Password
		s.profiles[p.ID] = p
	}
	if s.secrets.ProxyUser == "" {
		s.secrets.ProxyUser = "sv-" + RandomToken(4)
		s.secrets.ProxyPass = RandomToken(18)
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
		if c.UUID != "" || c.Password != "" {
			s.secrets.Profiles[c.ID] = profileSecret{UUID: c.UUID, Password: c.Password}
		}
		c.UUID, c.Password = "", ""
		list = append(list, &c)
	}
	if err := s.writeJSON("secrets.json", s.secrets); err != nil {
		return err
	}
	if err := s.writeJSON("profiles.json", list); err != nil {
		return err
	}
	return s.writeJSON("settings.json", s.settings)
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
