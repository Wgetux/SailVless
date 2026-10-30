package service

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"sailvless/internal/routing"
)

// Rule sets as files, to keep them across reinstalls or share them.
// id "" means the common rules (Routing page), else a server's own rules.

type rulesFile struct {
	SailVlessRules int            `json:"sailvlessRules"` // format version
	Name           string         `json:"name,omitempty"`
	Rules          []routing.Rule `json:"rules"`
}

var unsafeName = regexp.MustCompile(`[^\p{L}\p{N}._-]+`)

func (s *Service) rulesOf(id string) ([]routing.Rule, string, error) {
	set := s.Store.Settings()
	if id == "" {
		return set.Routing.Rules, "common", nil
	}
	p, ok := s.Store.Profile(id)
	if !ok {
		return nil, "", fmt.Errorf("no such profile")
	}
	return set.ProfileRules[id], p.Name, nil
}

// ExportRules writes the rules to ~/Documents/SailVless and returns the path.
func (s *Service) ExportRules(id string) (string, error) {
	rules, name, err := s.rulesOf(id)
	if err != nil {
		return "", err
	}
	if len(rules) == 0 {
		return "", fmt.Errorf("no rules to export")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, "Documents", "SailVless")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	base := strings.Trim(unsafeName.ReplaceAllString(name, "_"), "_.")
	if len([]rune(base)) > 40 {
		base = string([]rune(base)[:40])
	}
	path := filepath.Join(dir, fmt.Sprintf("rules-%s-%s.json", base, time.Now().Format("20060102-150405")))
	b, err := json.MarshalIndent(rulesFile{SailVlessRules: 1, Name: name, Rules: rules}, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, append(b, '\n'), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// ImportRules adds the rules from a file (ours or a bare JSON list) after the
// existing ones; a rule with the same match keeps its current action.
func (s *Service) ImportRules(id, path string) (added, skipped int, err error) {
	st, err := os.Stat(path)
	if err != nil {
		return 0, 0, err
	}
	if !st.Mode().IsRegular() || st.Size() > 1<<20 {
		return 0, 0, fmt.Errorf("%s: not a regular file up to 1 MB", filepath.Base(path))
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, err
	}
	var f rulesFile
	if err := json.Unmarshal(b, &f); err != nil || f.Rules == nil {
		if err2 := json.Unmarshal(b, &f.Rules); err2 != nil {
			return 0, 0, fmt.Errorf("%s: not a SailVless rules file", filepath.Base(path))
		}
	}
	rules, _, err := s.rulesOf(id)
	if err != nil {
		return 0, 0, err
	}
	merged := append([]routing.Rule(nil), rules...)
	have := map[string]bool{}
	key := func(r routing.Rule) string { return r.Kind + "\x00" + strings.ToLower(strings.TrimSpace(r.Value)) }
	for _, r := range rules {
		have[key(r)] = true
	}
	for i, r := range f.Rules {
		r.Value = strings.TrimSpace(r.Value)
		if err := r.Validate(); err != nil {
			return 0, 0, fmt.Errorf("rule %d: %w", i+1, err)
		}
		if have[key(r)] {
			skipped++
			continue
		}
		have[key(r)] = true
		merged = append(merged, r)
		added++
	}
	if added == 0 {
		return 0, skipped, nil
	}
	if id != "" {
		return added, skipped, s.SetProfileRules(id, merged)
	}
	set := s.Store.Settings()
	set.Routing.Rules = merged
	return added, skipped, s.SetSettings(set)
}
