// Package routing turns the user's routing settings (mode, presets, own
// rules, DNS) into an ordered rule list that the core config builders render.
package routing

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"

	"sailvless/internal/locale"
)

// Rule matches traffic and sends it to an action.
type Rule struct {
	Kind   string `json:"kind"`   // domain (exact), suffix, keyword, ip (IP/CIDR), geosite, geoip
	Value  string `json:"value"`  // e.g. "example.com", "10.0.0.0/8", "category-ru"
	Action string `json:"action"` // proxy, direct, block
}

const (
	Global = "global" // everything through the proxy (LAN stays direct)
	Bypass = "bypass" // LAN + one country direct
	Custom = "custom" // only the user's rules, then CustomFinal
)

type Config struct {
	Mode        string `json:"mode"`
	Country     string `json:"country"`     // bypass mode, ISO code, e.g. "ru"
	BlockAds    bool   `json:"blockAds"`    // geosite:category-ads-all -> block
	Rules       []Rule `json:"rules"`       // user's rules, highest priority
	CustomFinal string `json:"customFinal"` // custom mode: proxy or direct
	DNSRemote   string `json:"dnsRemote"`   // through the proxy: https://…, tcp://IP, IP
	DNSDirect   string `json:"dnsDirect"`   // for direct domains: IP, https+local://…, localhost
}

// Default: the bypass country follows the phone's region (zh_CN -> cn).
func Default() Config {
	country := locale.Country(locale.Lang)
	if !countRe.MatchString(country) {
		country = "ru"
	}
	return Config{Mode: Global, Country: country, CustomFinal: "proxy",
		DNSRemote: "https://1.1.1.1/dns-query", DNSDirect: "localhost"}
}

var (
	codeRe  = regexp.MustCompile(`^[a-z0-9!@:._-]{1,64}$`)
	domRe   = regexp.MustCompile(`^[a-z0-9*._-]{1,253}$`)
	countRe = regexp.MustCompile(`^[a-z]{2}$`)
)

func (r Rule) Validate() error {
	switch r.Action {
	case "proxy", "direct", "block":
	default:
		return fmt.Errorf("bad action %q", r.Action)
	}
	v := strings.ToLower(strings.TrimSpace(r.Value))
	switch r.Kind {
	case "domain", "suffix", "keyword":
		if !domRe.MatchString(v) {
			return fmt.Errorf("bad domain %q", r.Value)
		}
	case "ip":
		if net.ParseIP(v) == nil {
			if _, _, err := net.ParseCIDR(v); err != nil {
				return fmt.Errorf("bad IP/CIDR %q", r.Value)
			}
		}
	case "geosite", "geoip":
		if !codeRe.MatchString(v) {
			return fmt.Errorf("bad %s code %q", r.Kind, r.Value)
		}
	default:
		return fmt.Errorf("bad rule kind %q", r.Kind)
	}
	return nil
}

func (c Config) Validate() error {
	switch c.Mode {
	case Global, Bypass, Custom:
	default:
		return fmt.Errorf("bad routing mode %q", c.Mode)
	}
	if c.Mode == Bypass && !countRe.MatchString(c.Country) {
		return fmt.Errorf("country must be a two-letter code")
	}
	if c.CustomFinal != "proxy" && c.CustomFinal != "direct" {
		return fmt.Errorf("custom final must be proxy or direct")
	}
	for i, r := range c.Rules {
		if err := r.Validate(); err != nil {
			return fmt.Errorf("rule %d: %w", i+1, err)
		}
	}
	if err := checkDNS(c.DNSRemote, false); err != nil {
		return fmt.Errorf("remote DNS: %w", err)
	}
	if err := checkDNS(c.DNSDirect, true); err != nil {
		return fmt.Errorf("direct DNS: %w", err)
	}
	return nil
}

// checkDNS accepts what Xray understands (it has no DNS-over-TLS).
func checkDNS(s string, direct bool) error {
	if net.ParseIP(s) != nil {
		return nil
	}
	if direct && s == "localhost" {
		return nil
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return fmt.Errorf("%q: use an IP, https://…/dns-query or tcp://IP", s)
	}
	switch u.Scheme {
	case "https", "tcp":
	case "https+local", "tcp+local":
		if !direct {
			return fmt.Errorf("%q: a *+local server would bypass the proxy", s)
		}
	default:
		return fmt.Errorf("%q: unsupported scheme (DNS-over-TLS is not supported by Xray, use DoH)", s)
	}
	return nil
}

// Effective returns the ordered rules and the final action.
func (c Config) Effective() ([]Rule, string) {
	var out []Rule
	for _, r := range c.Rules {
		r.Value = strings.ToLower(strings.TrimSpace(r.Value))
		out = append(out, r)
	}
	if c.BlockAds {
		out = append(out, Rule{Kind: "geosite", Value: "category-ads-all", Action: "block"})
	}
	// LAN is always direct (also needed for the TUN mode)
	out = append(out, Rule{Kind: "geoip", Value: "private", Action: "direct"})
	final := "proxy"
	switch c.Mode {
	case Bypass:
		cc := strings.ToLower(c.Country)
		switch cc {
		case "cn":
			out = append(out, Rule{Kind: "geosite", Value: "cn", Action: "direct"})
		case "ru":
			out = append(out,
				Rule{Kind: "geosite", Value: "category-ru", Action: "direct"},
				Rule{Kind: "geosite", Value: "tld-ru", Action: "direct"})
		default:
			out = append(out, Rule{Kind: "suffix", Value: cc, Action: "direct"})
		}
		out = append(out, Rule{Kind: "geoip", Value: cc, Action: "direct"})
	case Custom:
		final = c.CustomFinal
	}
	return out, final
}

// DirectDomains lists domain matchers of direct rules (for the direct DNS server).
func DirectDomains(rules []Rule) []Rule {
	var out []Rule
	for _, r := range rules {
		if r.Action == "direct" && r.Kind != "ip" && r.Kind != "geoip" {
			out = append(out, r)
		}
	}
	return out
}
