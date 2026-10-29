package singbox

import (
	"net"
	"net/url"
	"strings"

	"sailvless/internal/routing"
)

// sing-box 1.12+ has no geosite/geoip databases: categories come as rule-sets
// from the official SagerNet repositories (downloaded by sing-box through the proxy).
const (
	geositeURL = "https://raw.githubusercontent.com/SagerNet/sing-geosite/rule-set/geosite-%s.srs"
	geoipURL   = "https://raw.githubusercontent.com/SagerNet/sing-geoip/rule-set/geoip-%s.srs"
)

func ruleMatch(r routing.Rule, sets map[string]obj) obj {
	switch r.Kind {
	case "domain":
		return obj{"domain": []string{r.Value}}
	case "suffix":
		return obj{"domain_suffix": []string{r.Value}}
	case "keyword":
		return obj{"domain_keyword": []string{r.Value}}
	case "ip":
		return obj{"ip_cidr": []string{r.Value}}
	case "geoip":
		if r.Value == "private" {
			return obj{"ip_is_private": true}
		}
	}
	tag := r.Kind + "-" + r.Value
	tmpl := geositeURL
	if r.Kind == "geoip" {
		tmpl = geoipURL
	}
	sets[tag] = obj{"type": "remote", "tag": tag, "format": "binary",
		"url": strings.Replace(tmpl, "%s", r.Value, 1), "download_detour": "proxy"}
	return obj{"rule_set": []string{tag}}
}

func withAction(m obj, action string) obj {
	if action == "block" {
		m["action"] = "reject"
	} else {
		m["outbound"] = action
	}
	return m
}

// dnsServer converts a DNS setting (IP, https://…, tcp://…, *+local, localhost).
func dnsServer(tag, s string, viaProxy bool) obj {
	srv := obj{"tag": tag}
	if s == "localhost" {
		srv["type"] = "local"
		return srv
	}
	if net.ParseIP(s) != nil {
		srv["type"], srv["server"] = "udp", s
		if viaProxy {
			srv["type"] = "tcp" // TCP works over any proxy
		}
	} else if u, err := url.Parse(s); err == nil {
		scheme := strings.TrimSuffix(u.Scheme, "+local")
		srv["type"], srv["server"] = scheme, u.Hostname()
		if u.Port() != "" {
			srv["server_port"] = u.Port()
		}
		if scheme == "https" && u.Path != "" && u.Path != "/dns-query" {
			srv["path"] = u.Path
		}
	}
	if viaProxy {
		srv["detour"] = "proxy"
	}
	return srv
}

// applyRouting fills route rules, rule-sets and DNS of a generated config.
func applyRouting(cfg obj, rc routing.Config) {
	rules, final := rc.Effective()
	sets := map[string]obj{}
	routeRules := []any{obj{"action": "sniff"}, obj{"protocol": "dns", "action": "hijack-dns"}}
	var directDNS []any
	for _, r := range rules {
		routeRules = append(routeRules, withAction(ruleMatch(r, sets), r.Action))
	}
	for _, r := range routing.DirectDomains(rules) {
		directDNS = append(directDNS, ruleMatch(r, sets))
	}
	route := cfg["route"].(obj)
	route["rules"] = routeRules
	route["final"] = final
	route["default_domain_resolver"] = "direct-dns"
	if len(sets) > 0 {
		var list []any
		for _, s := range sets {
			list = append(list, s)
		}
		route["rule_set"] = list
	}
	dnsRules := []any{}
	for _, m := range directDNS {
		m.(obj)["server"] = "direct-dns"
		dnsRules = append(dnsRules, m)
	}
	cfg["dns"] = obj{
		"servers": []any{dnsServer("remote-dns", rc.DNSRemote, true), dnsServer("direct-dns", rc.DNSDirect, false)},
		"rules":   dnsRules,
		"final":   "remote-dns",
	}
}
