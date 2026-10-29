package xray

import (
	"encoding/json"
	"testing"

	"sailvless/internal/link"
	"sailvless/internal/routing"
)

// "everything else direct" must make "direct" the default outbound instead of
// a catch-all rule, or IPIfNonMatch never resolves domains for the IP rules.
func TestFinalDirectIsDefaultOutbound(t *testing.T) {
	p, _ := link.Parse("trojan://pw@example.com:443")
	rc := routing.Default()
	rc.Mode, rc.CustomFinal = routing.Custom, "direct"
	rc.Rules = []routing.Rule{{Kind: "ip", Value: "198.18.0.0/15", Action: "proxy"}}
	b, err := Build(p, Options{Inbounds: Inbounds{SocksPort: 10808}, Routing: &rc, IPv4Only: true})
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Outbounds []struct {
			Tag      string         `json:"tag"`
			Settings map[string]any `json:"settings"`
		} `json:"outbounds"`
		Routing struct {
			DomainStrategy string           `json:"domainStrategy"`
			Rules          []map[string]any `json:"rules"`
		} `json:"routing"`
		DNS struct {
			QueryStrategy string `json:"queryStrategy"`
		} `json:"dns"`
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Outbounds[0].Tag != "direct" || cfg.Outbounds[0].Settings["domainStrategy"] != "UseIPv4" {
		t.Fatalf("default outbound: %+v", cfg.Outbounds[0])
	}
	for _, r := range cfg.Routing.Rules {
		if _, ok := r["network"]; ok {
			t.Fatalf("catch-all rule present: %v", r)
		}
	}
	if cfg.Routing.DomainStrategy != "IPIfNonMatch" || cfg.DNS.QueryStrategy != "UseIPv4" {
		t.Fatalf("strategy %q / %q", cfg.Routing.DomainStrategy, cfg.DNS.QueryStrategy)
	}
}
