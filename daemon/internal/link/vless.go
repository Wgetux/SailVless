package link

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// parseVLESS handles the de-facto standard format (XTLS/Xray-core discussion #716):
// vless://uuid@host:port?type=..&security=..&...#name
func parseVLESS(s string) (*Profile, error) {
	u, err := url.Parse(s)
	if err != nil {
		return nil, err
	}
	if u.User == nil || u.User.Username() == "" {
		return nil, fmt.Errorf("missing uuid")
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port <= 0 || port > 65535 {
		return nil, fmt.Errorf("bad port %q", u.Port())
	}
	if u.Hostname() == "" {
		return nil, fmt.Errorf("missing host")
	}
	q := u.Query()
	p := &Profile{
		Protocol:   "vless",
		Address:    u.Hostname(),
		Port:       port,
		UUID:       u.User.Username(),
		Name:       u.Fragment,
		Flow:       q.Get("flow"),
		Encryption: q.Get("encryption"),
	}
	if p.Encryption == "" {
		p.Encryption = "none"
	}
	p.Transport, err = parseTransport(q)
	if err != nil {
		return nil, err
	}
	p.Security, err = parseSecurity(q)
	if err != nil {
		return nil, err
	}
	if p.Name == "" {
		p.Name = fmt.Sprintf("%s:%d", p.Address, p.Port)
	}
	p.ID = stableID(p.Protocol, p.Address, strconv.Itoa(p.Port), p.UUID, p.Transport.Type, p.Transport.Path)
	return p, nil
}

func parseTransport(q url.Values) (Transport, error) {
	t := Transport{Type: strings.ToLower(q.Get("type"))}
	switch t.Type {
	case "", "tcp", "raw":
		t.Type = "tcp"
		t.HeaderType = q.Get("headerType")
		t.Host = q.Get("host")
		t.Path = q.Get("path")
	case "ws", "httpupgrade":
		t.Host = q.Get("host")
		t.Path = q.Get("path")
	case "grpc":
		t.ServiceName = q.Get("serviceName")
		t.Authority = q.Get("authority")
		t.Mode = q.Get("mode")
	case "xhttp", "splithttp":
		t.Type = "xhttp"
		t.Host = q.Get("host")
		t.Path = q.Get("path")
		t.Mode = q.Get("mode")
		t.Extra = q.Get("extra")
	default:
		return t, fmt.Errorf("unsupported transport %q", t.Type)
	}
	return t, nil
}

func parseSecurity(q url.Values) (Security, error) {
	s := Security{
		Type:        strings.ToLower(q.Get("security")),
		SNI:         q.Get("sni"),
		Fingerprint: q.Get("fp"),
	}
	if a := q.Get("alpn"); a != "" {
		s.ALPN = strings.Split(a, ",")
	}
	switch s.Type {
	case "", "none":
		s.Type = "none"
	case "tls":
		s.AllowInsecure = q.Get("allowInsecure") == "1" || q.Get("allowInsecure") == "true" || q.Get("insecure") == "1"
	case "reality":
		s.PublicKey = q.Get("pbk")
		s.ShortID = q.Get("sid")
		s.SpiderX = q.Get("spx")
		s.Mldsa65Verify = q.Get("pqv")
		if s.PublicKey == "" {
			return s, fmt.Errorf("reality: missing pbk")
		}
	default:
		return s, fmt.Errorf("unsupported security %q", s.Type)
	}
	return s, nil
}
