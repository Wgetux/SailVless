package xray

import (
	"net"
	"net/url"

	"sailvless/internal/routing"
)

var outboundTag = map[string]string{"proxy": "proxy", "direct": "direct", "block": "block"}

func domainMatcher(r routing.Rule) string {
	switch r.Kind {
	case "domain":
		return "full:" + r.Value
	case "suffix":
		return "domain:" + r.Value
	case "keyword":
		return r.Value // a plain string is a substring match in Xray
	case "geosite":
		return "geosite:" + r.Value
	}
	return ""
}

func ipMatcher(r routing.Rule) string {
	if r.Kind == "geoip" {
		return "geoip:" + r.Value
	}
	return r.Value
}

// addRouting renders the rule list as Xray "routing" + "dns" sections.
func addRouting(cfg map[string]any, rc routing.Config, ipv4Only bool) {
	rules, final := rc.Effective()
	var out []any
	hasIP := false

	// a plain-IP direct DNS server must not be reached through the proxy
	if ip := net.ParseIP(rc.DNSDirect); ip != nil {
		out = append(out, obj{"type": "field", "ip": []string{rc.DNSDirect}, "port": "53", "outboundTag": "direct"})
	}
	for _, r := range rules {
		rule := obj{"type": "field", "outboundTag": outboundTag[r.Action]}
		if m := domainMatcher(r); m != "" {
			rule["domain"] = []string{m}
		} else {
			rule["ip"] = []string{ipMatcher(r)}
			hasIP = true
		}
		out = append(out, rule)
	}
	if final == "direct" {
		// Xray sends unmatched traffic to the first outbound. A catch-all
		// "network: tcp,udp" rule would match domain requests before
		// IPIfNonMatch resolves them, so IP rules (e.g. AntiZapret's
		// 198.18.0.0/15 answers) would never apply.
		makeDefault(cfg, "direct")
	}
	strategy := "AsIs"
	if hasIP {
		// resolve domains that matched no domain rule, so geoip rules apply to them
		strategy = "IPIfNonMatch"
	}
	cfg["routing"] = obj{"domainStrategy": strategy, "rules": out}
	cfg["dns"] = dnsSection(rc, rules, ipv4Only)
}

// makeDefault moves the outbound with tag to the front (Xray's default route).
func makeDefault(cfg map[string]any, tag string) {
	outs, _ := cfg["outbounds"].([]any)
	for i, o := range outs {
		if ob, ok := o.(map[string]any); ok && ob["tag"] == tag {
			reordered := append([]any{o}, append(append([]any{}, outs[:i]...), outs[i+1:]...)...)
			cfg["outbounds"] = reordered
			return
		}
	}
}

func dnsSection(rc routing.Config, rules []routing.Rule, ipv4Only bool) obj {
	var servers []any
	var direct []string
	for _, r := range routing.DirectDomains(rules) {
		if m := domainMatcher(r); m != "" {
			direct = append(direct, m)
		}
	}
	if len(direct) > 0 {
		servers = append(servers, obj{"address": rc.DNSDirect, "domains": direct, "skipFallback": true})
	}
	remote := rc.DNSRemote
	if u, err := url.Parse(remote); err == nil && u.Scheme == "" && net.ParseIP(remote) != nil {
		remote = "tcp://" + remote // plain IP: TCP works over any proxy, UDP may not
	}
	servers = append(servers, remote)
	strategy := "UseIP"
	if ipv4Only {
		strategy = "UseIPv4"
	}
	return obj{"servers": servers, "queryStrategy": strategy}
}
