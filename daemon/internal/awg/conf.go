// Package awg parses AmneziaWG configs (wg-quick format with the AmneziaWG
// obfuscation fields) and renders them for amneziawg-go's UAPI.
package awg

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
)

type Peer struct {
	PublicKey    string // base64
	PresharedKey string // base64, optional
	Endpoint     string // host:port
	AllowedIPs   []string
	Keepalive    int
}

type Config struct {
	PrivateKey string // base64
	Addresses  []netip.Prefix
	DNS        []netip.Addr
	MTU        int
	// Obfuscation: Jc, Jmin, Jmax, S1-S4, H1-H4, I1-I5 (AWG 1.0/1.5/2.0),
	// passed to amneziawg-go as is (lower-case keys).
	Obfuscation map[string]string
	Peers       []Peer
	Name        string // from a leading "# Name = ..." comment, if any
}

var obfuscationKeys = map[string]bool{
	"jc": true, "jmin": true, "jmax": true,
	"s1": true, "s2": true, "s3": true, "s4": true,
	"h1": true, "h2": true, "h3": true, "h4": true,
	"i1": true, "i2": true, "i3": true, "i4": true, "i5": true,
}

// LooksLikeConf tells whether text is a WireGuard/AmneziaWG config.
func LooksLikeConf(text string) bool {
	return strings.Contains(text, "[Interface]") && strings.Contains(text, "[Peer]")
}

// Parse reads a wg-quick style config.
func Parse(text string) (*Config, error) {
	c := &Config{Obfuscation: map[string]string{}}
	section := ""
	var peer *Peer
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "#") {
			if k, v, ok := strings.Cut(strings.TrimLeft(line, "# "), "="); ok && strings.EqualFold(strings.TrimSpace(k), "name") && c.Name == "" {
				c.Name = strings.TrimSpace(v)
			}
			continue
		}
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") {
			section = strings.ToLower(strings.Trim(line, "[] "))
			if section == "peer" {
				c.Peers = append(c.Peers, Peer{})
				peer = &c.Peers[len(c.Peers)-1]
			}
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("bad line %q", line)
		}
		key, val := strings.ToLower(strings.TrimSpace(k)), strings.TrimSpace(v)
		switch section {
		case "interface":
			switch {
			case key == "privatekey":
				c.PrivateKey = val
			case key == "address":
				for _, a := range splitList(val) {
					p, err := netip.ParsePrefix(a)
					if err != nil {
						ip, err2 := netip.ParseAddr(a)
						if err2 != nil {
							return nil, fmt.Errorf("bad Address %q", a)
						}
						p = netip.PrefixFrom(ip, ip.BitLen())
					}
					c.Addresses = append(c.Addresses, p)
				}
			case key == "dns":
				for _, a := range splitList(val) {
					if ip, err := netip.ParseAddr(a); err == nil {
						c.DNS = append(c.DNS, ip)
					} // search domains are ignored
				}
			case key == "mtu":
				c.MTU, _ = strconv.Atoi(val)
			case obfuscationKeys[key]:
				c.Obfuscation[key] = val
			}
			// ListenPort, Table, PostUp... are wg-quick specifics, not needed in userspace
		case "peer":
			switch key {
			case "publickey":
				peer.PublicKey = val
			case "presharedkey":
				peer.PresharedKey = val
			case "endpoint":
				peer.Endpoint = val
			case "allowedips":
				peer.AllowedIPs = append(peer.AllowedIPs, splitList(val)...)
			case "persistentkeepalive":
				peer.Keepalive, _ = strconv.Atoi(val)
			}
		}
	}
	return c, c.validate()
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func keyHex(b64 string) (string, error) {
	b, err := base64.StdEncoding.DecodeString(b64)
	if err != nil || len(b) != 32 {
		return "", fmt.Errorf("bad key")
	}
	return hex.EncodeToString(b), nil
}

func (c *Config) validate() error {
	if _, err := keyHex(c.PrivateKey); err != nil {
		return fmt.Errorf("Interface PrivateKey: %w", err)
	}
	if len(c.Addresses) == 0 {
		return fmt.Errorf("Interface Address is missing")
	}
	if len(c.Peers) == 0 {
		return fmt.Errorf("no [Peer]")
	}
	for i, p := range c.Peers {
		if _, err := keyHex(p.PublicKey); err != nil {
			return fmt.Errorf("peer %d PublicKey: %w", i+1, err)
		}
		if p.PresharedKey != "" {
			if _, err := keyHex(p.PresharedKey); err != nil {
				return fmt.Errorf("peer %d PresharedKey: %w", i+1, err)
			}
		}
		if _, _, err := net.SplitHostPort(p.Endpoint); err != nil {
			return fmt.Errorf("peer %d Endpoint %q: %w", i+1, p.Endpoint, err)
		}
	}
	return nil
}

// Split reports split tunnelling: the peers' AllowedIPs do not cover the
// whole Internet, so only those networks go into the tunnel (as wg-quick
// would route them) and everything else goes direct.
func (c *Config) Split() (bool, []string) {
	var nets []string
	full4 := false
	for _, p := range c.Peers {
		for _, a := range p.AllowedIPs {
			pfx, err := netip.ParsePrefix(a)
			if err != nil {
				continue
			}
			if pfx.Bits() == 0 && pfx.Addr().Is4() {
				full4 = true
			}
			nets = append(nets, pfx.String())
		}
	}
	return !full4, nets
}

// Endpoint returns host and port of the first peer (for display and DNS).
func (c *Config) Endpoint() (string, int) {
	h, p, _ := net.SplitHostPort(c.Peers[0].Endpoint)
	port, _ := strconv.Atoi(p)
	return h, port
}

// UAPI renders the device configuration. Endpoints must already be IPs
// (resolve passes host names through the system resolver).
func (c *Config) UAPI(fwmark int, resolve func(host string) (string, error)) (string, error) {
	var b strings.Builder
	pk, _ := keyHex(c.PrivateKey)
	fmt.Fprintf(&b, "private_key=%s\nreplace_peers=true\n", pk)
	if fwmark != 0 {
		fmt.Fprintf(&b, "fwmark=%d\n", fwmark)
	}
	// obfuscation keys in a stable order
	for _, k := range []string{"jc", "jmin", "jmax", "s1", "s2", "s3", "s4", "h1", "h2", "h3", "h4", "i1", "i2", "i3", "i4", "i5"} {
		if v, ok := c.Obfuscation[k]; ok {
			fmt.Fprintf(&b, "%s=%s\n", k, v)
		}
	}
	for _, p := range c.Peers {
		pub, _ := keyHex(p.PublicKey)
		fmt.Fprintf(&b, "public_key=%s\n", pub)
		if p.PresharedKey != "" {
			psk, _ := keyHex(p.PresharedKey)
			fmt.Fprintf(&b, "preshared_key=%s\n", psk)
		}
		host, port, _ := net.SplitHostPort(p.Endpoint)
		if _, err := netip.ParseAddr(host); err != nil {
			ip, err := resolve(host)
			if err != nil {
				return "", fmt.Errorf("resolve %s: %w", host, err)
			}
			host = ip
		}
		fmt.Fprintf(&b, "endpoint=%s\n", net.JoinHostPort(host, port))
		if p.Keepalive > 0 {
			fmt.Fprintf(&b, "persistent_keepalive_interval=%d\n", p.Keepalive)
		}
		fmt.Fprintf(&b, "replace_allowed_ips=true\n")
		for _, a := range p.AllowedIPs {
			fmt.Fprintf(&b, "allowed_ip=%s\n", a)
		}
	}
	return b.String(), nil
}
