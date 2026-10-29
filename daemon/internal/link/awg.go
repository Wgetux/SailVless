package link

import (
	"bytes"
	"compress/zlib"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"sailvless/internal/awg"
)

// ParseAWG imports an AmneziaWG/WireGuard config (wg-quick text). The whole
// config is a secret and kept in Raw; Address/Port show the endpoint.
func ParseAWG(text string) (*Profile, error) {
	c, err := awg.Parse(text)
	if err != nil {
		return nil, fmt.Errorf("amneziawg: %w", err)
	}
	host, port := c.Endpoint()
	p := &Profile{Protocol: "amneziawg", Address: host, Port: port, Raw: strings.TrimSpace(text), Name: c.Name,
		Transport: Transport{Type: "udp"}, Security: Security{Type: "none"}}
	if len(c.Obfuscation) == 0 {
		p.Transport.Type = "wireguard" // plain WireGuard works the same way
	}
	if p.Name == "" {
		p.Name = "AmneziaWG " + host
	}
	p.ID = StableID("amneziawg", host, strconv.Itoa(port), c.PrivateKey)
	return p, nil
}

// parseAmneziaVPN decodes vpn:// links exported by AmneziaVPN: base64url of a
// Qt qCompress()ed JSON (4-byte length + zlib). The AmneziaWG config text sits
// in a "config" field somewhere inside (containers[].awg.last_config).
func parseAmneziaVPN(s string) (*Profile, error) {
	raw, err := DecodeBase64(strings.TrimPrefix(s, "vpn://"))
	if err != nil {
		return nil, fmt.Errorf("vpn://: base64: %w", err)
	}
	if len(raw) < 5 {
		return nil, fmt.Errorf("vpn://: too short")
	}
	zr, err := zlib.NewReader(bytes.NewReader(raw[4:]))
	if err != nil {
		return nil, fmt.Errorf("vpn://: %w", err)
	}
	js, err := io.ReadAll(io.LimitReader(zr, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("vpn://: %w", err)
	}
	var doc any
	if err := json.Unmarshal(js, &doc); err != nil {
		return nil, fmt.Errorf("vpn://: %w", err)
	}
	conf, name := findConf(doc), ""
	if m, ok := doc.(map[string]any); ok {
		name, _ = m["description"].(string)
	}
	if conf == "" {
		return nil, fmt.Errorf("vpn://: no AmneziaWG config inside (other Amnezia protocols are not supported)")
	}
	p, err := ParseAWG(conf)
	if err != nil {
		return nil, err
	}
	if name != "" {
		p.Name = name
	}
	return p, nil
}

// findConf looks for a wg-quick config in "config" fields, also inside
// JSON-encoded strings (last_config is JSON in a string).
func findConf(v any) string {
	switch t := v.(type) {
	case map[string]any:
		for k, x := range t {
			if s, ok := x.(string); ok {
				if k == "config" && awg.LooksLikeConf(s) {
					return s
				}
				if strings.HasPrefix(strings.TrimSpace(s), "{") {
					var inner any
					if json.Unmarshal([]byte(s), &inner) == nil {
						if c := findConf(inner); c != "" {
							return c
						}
					}
				}
			} else if c := findConf(x); c != "" {
				return c
			}
		}
	case []any:
		for _, x := range t {
			if c := findConf(x); c != "" {
				return c
			}
		}
	}
	return ""
}
