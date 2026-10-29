// Package link parses share links (vless://, ...) into Profiles.
package link

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// Profile is one proxy server. Secret fields (UUID, Password) are stored
// separately from the rest by the store package.
type Profile struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Protocol string `json:"protocol"` // vless, vmess, trojan, shadowsocks, hysteria2, tuic, amneziawg, xray-json, singbox-json
	Address  string `json:"address"`
	Port     int    `json:"port"`
	SubID    string `json:"subId,omitempty"`

	// secrets: kept out of profiles.json by the store
	UUID     string `json:"uuid,omitempty"`
	Password string `json:"password,omitempty"`
	Raw      string `json:"raw,omitempty"` // full core config for *-json profiles

	Flow       string `json:"flow,omitempty"`
	Encryption string `json:"encryption,omitempty"` // vless encryption / vmess or ss cipher

	// hysteria2 / tuic
	Ports        string `json:"ports,omitempty"` // port hopping, e.g. "20000-30000,443"
	Obfs         string `json:"obfs,omitempty"`  // salamander
	ObfsPassword string `json:"obfsPassword,omitempty"`
	UpMbps       int    `json:"upMbps,omitempty"`
	DownMbps     int    `json:"downMbps,omitempty"`
	Congestion   string `json:"congestion,omitempty"`   // tuic
	UDPRelayMode string `json:"udpRelayMode,omitempty"` // tuic

	Transport Transport `json:"transport"`
	Security  Security  `json:"security"`
}

// Core returns which core runs this profile.
func (p *Profile) Core() string {
	switch p.Protocol {
	case "hysteria2", "tuic", "singbox-json":
		return "sing-box"
	}
	return "xray"
}

type Transport struct {
	Type        string `json:"type"` // tcp, ws, grpc, xhttp, httpupgrade
	Host        string `json:"host,omitempty"`
	Path        string `json:"path,omitempty"`
	Mode        string `json:"mode,omitempty"`        // xhttp mode, grpc "multi"
	HeaderType  string `json:"headerType,omitempty"`  // tcp: none/http
	ServiceName string `json:"serviceName,omitempty"` // grpc
	Authority   string `json:"authority,omitempty"`   // grpc
	Extra       string `json:"extra,omitempty"`       // xhttp extra (raw JSON)
}

type Security struct {
	Type          string   `json:"type"` // none, tls, reality
	SNI           string   `json:"sni,omitempty"`
	Fingerprint   string   `json:"fp,omitempty"`
	ALPN          []string `json:"alpn,omitempty"`
	AllowInsecure bool     `json:"allowInsecure,omitempty"`
	PublicKey     string   `json:"pbk,omitempty"`
	ShortID       string   `json:"sid,omitempty"`
	SpiderX       string   `json:"spx,omitempty"`
	Mldsa65Verify string   `json:"pqv,omitempty"`
	PinSHA256     string   `json:"pinSHA256,omitempty"` // hysteria2 cert pin
}

// StableID derives a profile ID from its identity so re-importing the same
// link updates the existing profile instead of duplicating it.
func StableID(parts ...string) string {
	h := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(h[:6])
}

// Parse parses a single share link.
func Parse(s string) (*Profile, error) {
	s = strings.TrimSpace(s)
	scheme, _, ok := strings.Cut(s, "://")
	if !ok {
		return nil, fmt.Errorf("not a share link")
	}
	switch strings.ToLower(scheme) {
	case "vless":
		return parseVLESS(s)
	case "vmess":
		return parseVMess(s)
	case "trojan":
		return parseTrojan(s)
	case "ss":
		return parseSS(s)
	case "hysteria2", "hy2":
		return parseHysteria2(s)
	case "tuic":
		return parseTUIC(s)
	case "vpn":
		return parseAmneziaVPN(s)
	default:
		return nil, fmt.Errorf("unsupported scheme %q", scheme)
	}
}

// ParseMany parses newline-separated links, collecting per-line errors.
func ParseMany(text string) ([]*Profile, []error) {
	var out []*Profile
	var errs []error
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		p, err := Parse(line)
		if err != nil {
			errs = append(errs, fmt.Errorf("%.16s…: %w", line, err))
			continue
		}
		out = append(out, p)
	}
	return out, errs
}
