// Package xray generates Xray-core JSON configs from profiles.
package xray

import (
	"encoding/json"
	"fmt"

	"sailvless/internal/link"
	"sailvless/internal/routing"
)

// Inbounds describes the local proxy listeners.
type Inbounds struct {
	Listen    string
	SocksPort int
	HTTPPort  int // 0 = disabled
	User      string
	Pass      string // auth enabled when User != ""
}

type Options struct {
	Inbounds Inbounds
	LogLevel string // debug, info, warning, error, none
	// APIListen enables traffic stats on this loopback address
	// (Xray StatsService / sing-box clash API protected by APISecret).
	APIListen string
	APISecret string
	// Mark (SO_MARK) is set on every outbound socket in TUN mode so the
	// core's own traffic bypasses the tunnel. 0 = off.
	Mark int
	// Routing (nil = everything through the proxy, as before).
	Routing *routing.Config
	// RouteImported applies Routing to imported full configs too
	// (by default the panel's own routing is kept).
	RouteImported bool
}

// retagProxy makes an imported config fit our routing: the first proxy
// outbound is tagged "proxy" and "direct"/"block" outbounds exist.
func retagProxy(cfg map[string]any) {
	outs, _ := cfg["outbounds"].([]any)
	have := map[string]bool{}
	for _, o := range outs {
		if ob, ok := o.(map[string]any); ok {
			tag, _ := ob["tag"].(string)
			have[tag] = true
		}
	}
	if !have["proxy"] {
	tagFirst:
		for _, o := range outs {
			ob, ok := o.(map[string]any)
			if !ok {
				continue
			}
			switch ob["protocol"] {
			case "freedom", "blackhole", "dns", "loopback":
				continue
			}
			ob["tag"] = "proxy"
			break tagFirst
		}
	}
	if !have["direct"] {
		outs = append(outs, obj{"tag": "direct", "protocol": "freedom"})
	}
	if !have["block"] {
		outs = append(outs, obj{"tag": "block", "protocol": "blackhole"})
	}
	cfg["outbounds"] = outs
}

// addMark sets sockopt.mark on all outbounds (including foreign configs).
func addMark(cfg map[string]any, mark int) {
	if mark == 0 {
		return
	}
	outs, _ := cfg["outbounds"].([]any)
	for _, o := range outs {
		ob, ok := o.(map[string]any)
		if !ok {
			continue
		}
		ss, _ := ob["streamSettings"].(map[string]any)
		if ss == nil {
			ss = obj{}
			ob["streamSettings"] = ss
		}
		so, _ := ss["sockopt"].(map[string]any)
		if so == nil {
			so = obj{}
			ss["sockopt"] = so
		}
		so["mark"] = mark
	}
}

// addStats enables per-outbound traffic counters and the stats API.
func addStats(cfg map[string]any, listen string) {
	if listen == "" {
		return
	}
	cfg["stats"] = obj{}
	cfg["api"] = obj{"tag": "api", "listen": listen, "services": []string{"StatsService"}}
	policy, _ := cfg["policy"].(map[string]any)
	if policy == nil {
		policy = obj{}
	}
	sys, _ := policy["system"].(map[string]any)
	if sys == nil {
		sys = obj{}
	}
	sys["statsOutboundUplink"], sys["statsOutboundDownlink"] = true, true
	policy["system"] = sys
	cfg["policy"] = policy
}

type obj = map[string]any

// Build returns the full Xray config for profile p.
func Build(p *link.Profile, o Options) ([]byte, error) {
	out, err := outbound(p)
	if err != nil {
		return nil, err
	}
	logLevel := o.LogLevel
	if logLevel == "" {
		logLevel = "warning"
	}
	cfg := obj{
		"log":       obj{"loglevel": logLevel, "access": "none"},
		"inbounds":  inbounds(o.Inbounds),
		"outbounds": []any{out, obj{"tag": "direct", "protocol": "freedom"}, obj{"tag": "block", "protocol": "blackhole"}},
	}
	addStats(cfg, o.APIListen)
	addMark(cfg, o.Mark)
	if o.Routing != nil {
		addRouting(cfg, *o.Routing)
	}
	return json.MarshalIndent(cfg, "", "  ")
}

func inbounds(in Inbounds) []any {
	listen := in.Listen
	if listen == "" {
		listen = "127.0.0.1"
	}
	socks := obj{"auth": "noauth", "udp": true}
	if in.User != "" {
		socks["auth"] = "password"
		socks["accounts"] = []any{obj{"user": in.User, "pass": in.Pass}}
	}
	res := []any{obj{
		"tag": "socks-in", "protocol": "socks", "listen": listen, "port": in.SocksPort,
		"settings": socks,
		"sniffing": obj{"enabled": true, "destOverride": []string{"http", "tls", "quic"}, "routeOnly": true},
	}}
	if in.HTTPPort > 0 {
		httpSettings := obj{}
		if in.User != "" {
			httpSettings["accounts"] = []any{obj{"user": in.User, "pass": in.Pass}}
		}
		res = append(res, obj{
			"tag": "http-in", "protocol": "http", "listen": listen, "port": in.HTTPPort,
			"settings": httpSettings,
			"sniffing": obj{"enabled": true, "destOverride": []string{"http", "tls"}, "routeOnly": true},
		})
	}
	return res
}

func outbound(p *link.Profile) (obj, error) {
	stream, err := streamSettings(p)
	if err != nil {
		return nil, err
	}
	s := obj{"address": p.Address, "port": p.Port}
	switch p.Protocol {
	case "vless":
		s["id"], s["encryption"] = p.UUID, p.Encryption
		if p.Flow != "" {
			s["flow"] = p.Flow
		}
	case "vmess":
		s["id"], s["security"] = p.UUID, p.Encryption
	case "trojan":
		s["password"] = p.Password
	case "shadowsocks":
		s["method"], s["password"] = p.Encryption, p.Password
	default:
		return nil, fmt.Errorf("xray: unsupported protocol %q", p.Protocol)
	}
	return obj{"tag": "proxy", "protocol": p.Protocol, "settings": s, "streamSettings": stream}, nil
}

// BuildRaw takes a user-supplied full Xray config and replaces its inbounds
// with ours, so local listener/auth settings always apply.
func BuildRaw(raw string, o Options) ([]byte, error) {
	var cfg map[string]any
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return nil, fmt.Errorf("xray json: %w", err)
	}
	cfg["inbounds"] = inbounds(o.Inbounds)
	logCfg, _ := cfg["log"].(map[string]any)
	if logCfg == nil {
		logCfg = obj{}
	}
	if o.LogLevel != "" {
		logCfg["loglevel"] = o.LogLevel
	}
	// never write files to paths from foreign configs; errors go to stdout
	logCfg["access"] = "none"
	delete(logCfg, "error")
	cfg["log"] = logCfg
	addStats(cfg, o.APIListen)
	addMark(cfg, o.Mark)
	if o.Routing != nil && o.RouteImported {
		retagProxy(cfg)
		addRouting(cfg, *o.Routing)
	}
	return json.MarshalIndent(cfg, "", "  ")
}

func streamSettings(p *link.Profile) (obj, error) {
	t := p.Transport
	ss := obj{"network": t.Type}
	switch t.Type {
	case "tcp":
		ss["network"] = "raw"
		if t.HeaderType == "http" {
			req := obj{}
			if t.Path != "" {
				req["path"] = []string{t.Path}
			}
			if t.Host != "" {
				req["headers"] = obj{"Host": []string{t.Host}}
			}
			ss["rawSettings"] = obj{"header": obj{"type": "http", "request": req}}
		}
	case "ws":
		ss["wsSettings"] = obj{"path": t.Path, "host": t.Host}
	case "httpupgrade":
		ss["httpupgradeSettings"] = obj{"path": t.Path, "host": t.Host}
	case "grpc":
		g := obj{"serviceName": t.ServiceName, "multiMode": t.Mode == "multi"}
		if t.Authority != "" {
			g["authority"] = t.Authority
		}
		ss["grpcSettings"] = g
	case "xhttp":
		x := obj{"path": t.Path}
		if t.Host != "" {
			x["host"] = t.Host
		}
		if t.Mode != "" {
			x["mode"] = t.Mode
		}
		if t.Extra != "" {
			if !json.Valid([]byte(t.Extra)) {
				return nil, fmt.Errorf("xhttp extra is not valid JSON")
			}
			x["extra"] = json.RawMessage(t.Extra)
		}
		ss["xhttpSettings"] = x
	default:
		return nil, fmt.Errorf("xray: unsupported transport %q", t.Type)
	}

	sec := p.Security
	switch sec.Type {
	case "none":
		ss["security"] = "none"
	case "tls":
		tls := obj{"serverName": sec.SNI, "allowInsecure": sec.AllowInsecure}
		if sec.Fingerprint != "" {
			tls["fingerprint"] = sec.Fingerprint
		}
		if len(sec.ALPN) > 0 {
			tls["alpn"] = sec.ALPN
		}
		ss["security"] = "tls"
		ss["tlsSettings"] = tls
	case "reality":
		fp := sec.Fingerprint
		if fp == "" {
			fp = "chrome"
		}
		r := obj{"serverName": sec.SNI, "fingerprint": fp, "publicKey": sec.PublicKey, "shortId": sec.ShortID}
		if sec.SpiderX != "" {
			r["spiderX"] = sec.SpiderX
		}
		if sec.Mldsa65Verify != "" {
			r["mldsa65Verify"] = sec.Mldsa65Verify
		}
		ss["security"] = "reality"
		ss["realitySettings"] = r
	default:
		return nil, fmt.Errorf("xray: unsupported security %q", sec.Type)
	}
	return ss, nil
}
