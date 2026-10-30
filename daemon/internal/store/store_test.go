package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sailvless/internal/link"
	"sailvless/internal/routing"
)

func TestSecretsSeparated(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := link.Parse("vless://SECRET-UUID@h.example.com:443?security=tls#n")
	if err := s.Upsert([]*link.Profile{p}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSubscription(Subscription{ID: "s1"}, "https://panel.example.com/SECRET-TOKEN"); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"profiles.json", "subscriptions.json", "settings.json"} {
		b, _ := os.ReadFile(filepath.Join(dir, f))
		if strings.Contains(string(b), "SECRET") {
			t.Errorf("%s contains a secret", f)
		}
	}
	b, _ := os.ReadFile(filepath.Join(dir, "secrets.json"))
	if !strings.Contains(string(b), "SECRET-UUID") || !strings.Contains(string(b), "SECRET-TOKEN") {
		t.Error("secrets.json misses secrets")
	}
	s2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := s2.Profile(p.ID)
	if !ok || got.UUID != "SECRET-UUID" {
		t.Fatalf("reload: %+v", got)
	}
	if _, u, _ := s2.Subscription("s1"); u != "https://panel.example.com/SECRET-TOKEN" {
		t.Fatal("sub url not reloaded")
	}
}

func TestProfileRules(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	p, _ := link.Parse("vless://u@h.example.com:443?security=tls#n")
	if err := s.Upsert([]*link.Profile{p}); err != nil {
		t.Fatal(err)
	}
	set := s.Settings()
	set.ProfileRules = map[string][]routing.Rule{p.ID: {{Kind: "suffix", Value: "example.com", Action: "direct"}}}
	if err := s.SetSettings(set); err != nil {
		t.Fatal(err)
	}
	// a settings patch works on a copy until it is saved
	patched := s.Settings()
	if err := json.Unmarshal([]byte(`{"profileRules":{"x":[]}}`), &patched); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Settings().ProfileRules["x"]; ok {
		t.Fatal("patch changed the stored rules")
	}
	if err := s.Delete(p.ID); err != nil {
		t.Fatal(err)
	}
	if len(s.Settings().ProfileRules) != 0 {
		t.Fatal("rules of a deleted profile kept")
	}
}
