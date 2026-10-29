// Package singbox generates sing-box JSON configs (Hysteria2, TUIC, raw sing-box profiles).
package singbox

import (
	"encoding/json"
	"fmt"
	"strings"

	"sailvless/internal/config/xray"
	"sailvless/internal/link"
)

type obj = map[string]any

// Options mirrors xray.Options so the service can treat both cores alike.
type Options = xray.Options

// Build returns a sing-box config for profile p. Warnings are non-fatal
// notes about link options sing-box cannot honour.
func Build(p *link.Profile, o Options) ([]byte, []string, error) {
	var outbounds []any
	var warnings []string
	switch p.Protocol {
	case "hysteria2", "tuic":
		ob, w, err := outbound(p)
		if err != nil {
			return nil, nil, err
		}
		warnings = w
		outbounds = []any{ob}
	case "singbox-json":
		var raw struct {
			Outbounds []json.RawMessage `json:"outbounds"`
			Route     json.RawMessage   `json:"route"`
			DNS       json.RawMessage   `json:"dns"`
		}
		if err := json.Unmarshal([]byte(p.Raw), &raw); err != nil {
			return nil, nil, fmt.Errorf("sing-box json: %w", err)
		}
		hasProxy := false
		for _, r := range raw.Outbounds {
			var tag struct {
				Tag string `json:"tag"`
			}
			_ = json.Unmarshal(r, &tag)
			hasProxy = hasProxy || tag.Tag == "proxy"
			outbounds = append(outbounds, r)
		}
		if raw.Route != nil || raw.DNS != nil || !hasProxy {
			// a full config: keep its route/dns, replace only inbounds and log
			var full map[string]any
			_ = json.Unmarshal([]byte(p.Raw), &full)
			full["inbounds"] = inbounds(o.Inbounds)
			full["log"] = logCfg(o.LogLevel)
			// foreign experimental settings may write files or open UIs; replace them
			full["experimental"] = experimental(o)
			if o.Mark != 0 {
				route, _ := full["route"].(map[string]any)
				if route == nil {
					route = obj{}
				}
				route["default_mark"] = o.Mark
				full["route"] = route
			}
			b, err := json.MarshalIndent(full, "", "  ")
			return b, nil, err
		}
	default:
		return nil, nil, fmt.Errorf("sing-box: unsupported protocol %q", p.Protocol)
	}
	// block rules use the "reject" action, no outbound needed
	outbounds = append(outbounds, obj{"type": "direct", "tag": "direct"})
	cfg := obj{
		"log":       logCfg(o.LogLevel),
		"dns":       obj{"servers": []any{obj{"type": "local", "tag": "local"}}},
		"inbounds":  inbounds(o.Inbounds),
		"outbounds": outbounds,
		"route": obj{
			"final":                   "proxy",
			"default_domain_resolver": "local",
			"rules":                   []any{obj{"action": "sniff"}},
		},
		"experimental": experimental(o),
	}
	if o.Routing != nil {
		applyRouting(cfg, *o.Routing)
	}
	if o.Mark != 0 {
		cfg["route"].(obj)["default_mark"] = o.Mark
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	return b, warnings, err
}

// experimental enables the clash API (traffic counters) on loopback with a secret.
func experimental(o Options) obj {
	if o.APIListen == "" {
		return obj{}
	}
	return obj{"clash_api": obj{"external_controller": o.APIListen, "secret": o.APISecret}}
}

func logCfg(level string) obj {
	switch level {
	case "", "none":
		return obj{"level": "warn", "disabled": level == "none", "timestamp": true}
	case "warning":
		level = "warn"
	}
	return obj{"level": level, "timestamp": true}
}

func inbounds(in xray.Inbounds) []any {
	listen := in.Listen
	if listen == "" {
		listen = "127.0.0.1"
	}
	mk := func(tag, typ string, port int) obj {
		ib := obj{"type": typ, "tag": tag, "listen": listen, "listen_port": port}
		if in.User != "" {
			ib["users"] = []any{obj{"username": in.User, "password": in.Pass}}
		}
		return ib
	}
	res := []any{mk("socks-in", "socks", in.SocksPort)}
	if in.HTTPPort > 0 {
		res = append(res, mk("http-in", "http", in.HTTPPort))
	}
	return res
}

func outbound(p *link.Profile) (obj, []string, error) {
	var warnings []string
	t := obj{"enabled": true, "server_name": p.Security.SNI, "insecure": p.Security.AllowInsecure}
	if t["server_name"] == "" {
		t["server_name"] = p.Address
	}
	if len(p.Security.ALPN) > 0 {
		t["alpn"] = p.Security.ALPN
	}
	if p.Security.PinSHA256 != "" {
		warnings = append(warnings, "pinSHA256 (certificate hash) is not supported by sing-box and was ignored")
	}
	ob := obj{"tag": "proxy", "server": p.Address, "server_port": p.Port, "tls": t}
	switch p.Protocol {
	case "hysteria2":
		ob["type"] = "hysteria2"
		ob["password"] = p.Password
		if p.Ports != "" {
			ob["server_ports"] = portRanges(p.Ports)
		}
		if p.Obfs == "salamander" {
			ob["obfs"] = obj{"type": "salamander", "password": p.ObfsPassword}
		}
		if p.UpMbps > 0 {
			ob["up_mbps"] = p.UpMbps
		}
		if p.DownMbps > 0 {
			ob["down_mbps"] = p.DownMbps
		}
	case "tuic":
		ob["type"] = "tuic"
		ob["uuid"] = p.UUID
		ob["password"] = p.Password
		if p.Congestion != "" {
			ob["congestion_control"] = p.Congestion
		}
		if p.UDPRelayMode != "" {
			ob["udp_relay_mode"] = p.UDPRelayMode
		}
		if len(p.Security.ALPN) == 0 {
			t["alpn"] = []string{"h3"}
		}
	}
	return ob, warnings, nil
}

// portRanges converts "443,20000-30000" to sing-box's ["443:443", "20000:30000"].
func portRanges(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if a, b, ok := strings.Cut(part, "-"); ok {
			out = append(out, a+":"+b)
		} else {
			out = append(out, part+":"+part)
		}
	}
	return out
}
