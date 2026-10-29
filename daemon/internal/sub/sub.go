// Package sub fetches and parses proxy subscriptions.
package sub

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"sailvless/internal/link"
)

// Meta is what the panel tells about the subscription via response headers.
type Meta struct {
	Title          string `json:"title,omitempty"`
	Upload         int64  `json:"upload"`
	Download       int64  `json:"download"`
	Total          int64  `json:"total"`  // 0 = unlimited
	Expire         int64  `json:"expire"` // unix seconds, 0 = never
	UpdateInterval int    `json:"updateIntervalHours,omitempty"`
	SupportURL     string `json:"supportUrl,omitempty"`
	WebPageURL     string `json:"webPageUrl,omitempty"`
	Announce       string `json:"announce,omitempty"`
	HWIDActive     bool   `json:"hwidActive,omitempty"`
	HWIDLimit      bool   `json:"hwidLimitReached,omitempty"`
	HWIDNotSent    bool   `json:"hwidNotSupported,omitempty"`
	Format         string `json:"format,omitempty"` // links, base64, xray-json, singbox-json
}

type Request struct {
	URL       string
	UserAgent string
	HWID      string // empty = don't send
	Device    DeviceInfo
	// Client is used for the request (e.g. one that dials through the running proxy).
	Client *http.Client
}

var ErrHWIDLimit = errors.New("device limit reached: remove an old device in the panel/bot (x-hwid-max-devices-reached)")

// Fetch downloads and parses a subscription.
func Fetch(ctx context.Context, r Request) ([]*link.Profile, Meta, []error, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.URL, nil)
	if err != nil {
		return nil, Meta{}, nil, fmt.Errorf("bad url")
	}
	req.Header.Set("User-Agent", r.UserAgent)
	req.Header.Set("Accept", "*/*")
	if r.HWID != "" {
		req.Header.Set("x-hwid", r.HWID)
		req.Header.Set("x-device-os", r.Device.OS)
		req.Header.Set("x-ver-os", r.Device.Version)
		req.Header.Set("x-device-model", r.Device.Model)
	}
	c := r.Client
	if c == nil {
		c = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, Meta{}, nil, redact(err, r.URL)
	}
	defer resp.Body.Close()
	meta := parseHeaders(resp.Header)
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, meta, nil, redact(err, r.URL)
	}
	if meta.HWIDLimit {
		return nil, meta, nil, ErrHWIDLimit
	}
	if resp.StatusCode != http.StatusOK {
		msg := fmt.Sprintf("HTTP %d", resp.StatusCode)
		if resp.StatusCode == http.StatusNotFound && r.HWID == "" {
			msg += " (panel may require HWID: enable it in settings)"
		}
		return nil, meta, nil, errors.New(msg)
	}
	profiles, format, errs := Parse(body)
	meta.Format = format
	if len(profiles) == 0 {
		if len(errs) > 0 {
			return nil, meta, errs, fmt.Errorf("no usable servers (%d unparsable entries)", len(errs))
		}
		return nil, meta, nil, errors.New("no servers in subscription")
	}
	return profiles, meta, errs, nil
}

// redact removes the secret URL from network errors.
func redact(err error, u string) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return fmt.Errorf("%s: %v", ue.Op, ue.Err)
	}
	return errors.New(strings.ReplaceAll(err.Error(), u, MaskURL(u)))
}

// MaskURL hides everything but the scheme: subscription URLs are bearer
// secrets and even the panel domain is sensitive.
func MaskURL(s string) string {
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return "…"
	}
	return u.Scheme + "://…"
}

func parseHeaders(h http.Header) Meta {
	var m Meta
	m.Title = decodeMaybeB64(h.Get("profile-title"))
	m.SupportURL = h.Get("support-url")
	m.WebPageURL = h.Get("profile-web-page-url")
	m.Announce = decodeMaybeB64(h.Get("announce"))
	m.UpdateInterval, _ = strconv.Atoi(strings.TrimSpace(h.Get("profile-update-interval")))
	for _, kv := range strings.Split(h.Get("subscription-userinfo"), ";") {
		k, v, ok := strings.Cut(strings.TrimSpace(kv), "=")
		if !ok {
			continue
		}
		n, _ := strconv.ParseFloat(strings.TrimSpace(v), 64)
		switch strings.ToLower(k) {
		case "upload":
			m.Upload = int64(n)
		case "download":
			m.Download = int64(n)
		case "total":
			m.Total = int64(n)
		case "expire":
			m.Expire = int64(n)
		}
	}
	m.HWIDActive = h.Get("x-hwid-active") == "true"
	m.HWIDLimit = h.Get("x-hwid-max-devices-reached") == "true" || h.Get("x-hwid-limit") == "true"
	m.HWIDNotSent = h.Get("x-hwid-not-supported") == "true"
	return m
}

func decodeMaybeB64(s string) string {
	if rest, ok := strings.CutPrefix(s, "base64:"); ok {
		if b, err := base64.StdEncoding.DecodeString(rest); err == nil {
			return string(b)
		}
		if b, err := link.DecodeBase64(rest); err == nil {
			return string(b)
		}
	}
	return s
}

// Parse detects the body format: JSON (Xray / sing-box), plain links or base64 links.
func Parse(body []byte) ([]*link.Profile, string, []error) {
	text := strings.TrimSpace(strings.TrimPrefix(string(body), "\xef\xbb\xbf")) // strip UTF-8 BOM
	if strings.HasPrefix(text, "{") || strings.HasPrefix(text, "[") {
		ps, format, err := ParseJSON([]byte(text), true)
		if err != nil {
			return nil, "json", []error{err}
		}
		return ps, format, nil
	}
	if strings.Contains(text, "://") {
		ps, errs := link.ParseMany(text)
		return ps, "links", errs
	}
	dec, err := link.DecodeBase64(text)
	if err != nil {
		return nil, "unknown", []error{errors.New("body is neither links, base64 nor JSON")}
	}
	ps, errs := link.ParseMany(string(dec))
	return ps, "base64", errs
}

// ParseJSON accepts a full Xray config, an array of Xray configs (as Remnawave
// serves them) or a sing-box config. Each becomes a raw-JSON profile; with
// splitSingBoxOutbounds each sing-box proxy outbound becomes its own profile,
// otherwise the sing-box config is kept whole.
func ParseJSON(b []byte, splitSingBoxOutbounds bool) ([]*link.Profile, string, error) {
	var arr []json.RawMessage
	if err := json.Unmarshal(b, &arr); err != nil {
		arr = []json.RawMessage{b}
	}
	var out []*link.Profile
	format := ""
	for i, raw := range arr {
		var probe struct {
			Remarks   string `json:"remarks"`
			Outbounds []struct {
				Protocol string `json:"protocol"` // xray
				Type     string `json:"type"`     // sing-box
				Tag      string `json:"tag"`
				Server   string `json:"server"`
				Port     int    `json:"server_port"`
			} `json:"outbounds"`
		}
		if err := json.Unmarshal(raw, &probe); err != nil {
			return nil, "", fmt.Errorf("json config %d: %w", i, err)
		}
		if len(probe.Outbounds) == 0 {
			return nil, "", fmt.Errorf("json config %d has no outbounds", i)
		}
		if probe.Outbounds[0].Protocol != "" {
			format = "xray-json"
			p := &link.Profile{Protocol: "xray-json", Name: probe.Remarks, Raw: string(raw),
				Transport: link.Transport{Type: "json"}, Security: link.Security{Type: "json"}}
			if p.Name == "" {
				p.Name = fmt.Sprintf("Xray config %d", i+1)
			}
			p.ID = link.StableID("xray-json", string(raw))
			out = append(out, p)
			continue
		}
		format = "singbox-json"
		if !splitSingBoxOutbounds {
			p := &link.Profile{Protocol: "singbox-json", Name: fmt.Sprintf("sing-box config %d", i+1), Raw: string(raw),
				Transport: link.Transport{Type: "json"}, Security: link.Security{Type: "json"}}
			p.ID = link.StableID("singbox-json", string(raw))
			out = append(out, p)
			continue
		}
		ps, err := splitSingBox(raw)
		if err != nil {
			return nil, "", err
		}
		out = append(out, ps...)
	}
	return out, format, nil
}

var singBoxProxyTypes = map[string]bool{
	"vless": true, "vmess": true, "trojan": true, "shadowsocks": true, "hysteria2": true,
	"tuic": true, "hysteria": true, "anytls": true, "shadowtls": true, "wireguard": true, "socks": true, "http": true,
}

// splitSingBox turns each proxy outbound of a sing-box config into its own profile.
func splitSingBox(raw []byte) ([]*link.Profile, error) {
	var cfg struct {
		Outbounds []map[string]any `json:"outbounds"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}
	var out []*link.Profile
	for _, ob := range cfg.Outbounds {
		typ, _ := ob["type"].(string)
		if !singBoxProxyTypes[typ] {
			continue
		}
		server, _ := ob["server"].(string)
		port, _ := ob["server_port"].(float64)
		name, _ := ob["tag"].(string)
		ob["tag"] = "proxy"
		single, _ := json.Marshal(map[string]any{"outbounds": []any{ob}})
		p := &link.Profile{Protocol: "singbox-json", Name: name, Address: server, Port: int(port), Raw: string(single),
			Transport: link.Transport{Type: typ}, Security: link.Security{Type: "json"}}
		p.ID = link.StableID("singbox-json", string(single))
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil, errors.New("sing-box config has no proxy outbounds")
	}
	return out, nil
}
