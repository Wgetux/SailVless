// Package xray generates Xray-core JSON configs from profiles.
package xray

import (
	"encoding/json"
	"fmt"

	"sailvless/internal/link"
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
	switch p.Protocol {
	case "vless":
		s := obj{"address": p.Address, "port": p.Port, "id": p.UUID, "encryption": p.Encryption}
		if p.Flow != "" {
			s["flow"] = p.Flow
		}
		return obj{"tag": "proxy", "protocol": "vless", "settings": s, "streamSettings": stream}, nil
	}
	return nil, fmt.Errorf("xray: unsupported protocol %q", p.Protocol)
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
