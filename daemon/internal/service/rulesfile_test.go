package service

import (
	"os"
	"path/filepath"
	"testing"

	"sailvless/internal/link"
	"sailvless/internal/routing"
	"sailvless/internal/store"
)

func TestRulesFileRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir on Windows
	st, err := store.Open(filepath.Join(home, "data"))
	if err != nil {
		t.Fatal(err)
	}
	s := New(st, "", t.TempDir())
	p, _ := link.Parse("vless://u@h.example.com:443?security=tls#My/Server")
	if err := st.Upsert([]*link.Profile{p}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ExportRules(p.ID); err == nil {
		t.Fatal("exported an empty rule set")
	}
	rules := []routing.Rule{{Kind: "suffix", Value: "bank.example", Action: "direct"}, {Kind: "ip", Value: "10.1.0.0/16", Action: "block"}}
	if err := s.SetProfileRules(p.ID, rules); err != nil {
		t.Fatal(err)
	}
	path, err := s.ExportRules(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(path) != filepath.Join(home, "Documents", "SailVless") {
		t.Fatalf("path %s", path)
	}

	// into the common rules: both added; again: both skipped
	if a, sk, err := s.ImportRules("", path); err != nil || a != 2 || sk != 0 {
		t.Fatalf("import: %d %d %v", a, sk, err)
	}
	if a, sk, err := s.ImportRules("", path); err != nil || a != 0 || sk != 2 {
		t.Fatalf("re-import: %d %d %v", a, sk, err)
	}
	if got := st.Settings().Routing.Rules; len(got) != 2 || got[0] != rules[0] {
		t.Fatalf("common rules %+v", got)
	}

	// a bare JSON list works too; bad rules are refused as a whole
	bare := filepath.Join(home, "bare.json")
	os.WriteFile(bare, []byte(`[{"kind":"domain","value":"x.example","action":"proxy"}]`), 0o600)
	if a, _, err := s.ImportRules(p.ID, bare); err != nil || a != 1 || len(st.Settings().ProfileRules[p.ID]) != 3 {
		t.Fatalf("bare import: %d %v", a, err)
	}
	os.WriteFile(bare, []byte(`[{"kind":"domain","value":"x y","action":"proxy"}]`), 0o600)
	if _, _, err := s.ImportRules(p.ID, bare); err == nil {
		t.Fatal("bad rule imported")
	}
}
