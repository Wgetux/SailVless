package link

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// DecodeBase64 accepts std/url alphabets, with or without padding and line breaks.
func DecodeBase64(s string) ([]byte, error) {
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == ' ' || r == '\t' {
			return -1
		}
		return r
	}, s)
	s = strings.TrimRight(s, "=")
	if strings.ContainsAny(s, "-_") {
		return base64.RawURLEncoding.DecodeString(s)
	}
	return base64.RawStdEncoding.DecodeString(s)
}

func hostPort(u *url.URL) (string, int, error) {
	if u.Hostname() == "" {
		return "", 0, fmt.Errorf("missing host")
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port <= 0 || port > 65535 {
		return "", 0, fmt.Errorf("bad port %q", u.Port())
	}
	return u.Hostname(), port, nil
}

func finish(p *Profile, idParts ...string) *Profile {
	if p.Name == "" {
		p.Name = fmt.Sprintf("%s:%d", p.Address, p.Port)
	}
	p.ID = StableID(append([]string{p.Protocol, p.Address, strconv.Itoa(p.Port)}, idParts...)...)
	return p
}

// --- vmess://base64(json) (v2rayN format) ---

type vmessJSON struct {
	PS   string          `json:"ps"`
	Add  string          `json:"add"`
	Port json.RawMessage `json:"port"`
	ID   string          `json:"id"`
	Scy  string          `json:"scy"`
	Net  string          `json:"net"`
	Type string          `json:"type"`
	Host string          `json:"host"`
	Path string          `json:"path"`
	TLS  string          `json:"tls"`
	SNI  string          `json:"sni"`
	ALPN string          `json:"alpn"`
	FP   string          `json:"fp"`
	Mode string          `json:"mode"`
}

func parseVMess(s string) (*Profile, error) {
	raw, err := DecodeBase64(strings.TrimPrefix(s[len("vmess://"):], "//"))
	if err != nil {
		return nil, fmt.Errorf("vmess: base64: %w", err)
	}
	var v vmessJSON
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("vmess: json: %w", err)
	}
	port, err := strconv.Atoi(strings.Trim(string(v.Port), `"`))
	if err != nil || port <= 0 || port > 65535 || v.Add == "" || v.ID == "" {
		return nil, fmt.Errorf("vmess: missing address/port/id")
	}
	q := url.Values{}
	set := func(k, val string) {
		if val != "" {
			q.Set(k, val)
		}
	}
	set("type", v.Net)
	set("host", v.Host)
	set("path", v.Path)
	set("headerType", v.Type)
	set("security", v.TLS)
	set("sni", v.SNI)
	set("alpn", v.ALPN)
	set("fp", v.FP)
	if v.Net == "grpc" {
		set("serviceName", v.Path)
		set("mode", v.Type)
	} else {
		set("mode", v.Mode)
	}
	p := &Profile{Protocol: "vmess", Address: v.Add, Port: port, UUID: v.ID, Name: v.PS, Encryption: v.Scy}
	if p.Encryption == "" {
		p.Encryption = "auto"
	}
	if p.Transport, err = parseTransport(q); err != nil {
		return nil, err
	}
	if p.Security, err = parseSecurity(q); err != nil {
		return nil, err
	}
	return finish(p, p.UUID, p.Transport.Type, p.Transport.Path), nil
}

// --- trojan://password@host:port?security=tls&type=ws...#name ---

func parseTrojan(s string) (*Profile, error) {
	u, err := url.Parse(s)
	if err != nil {
		return nil, err
	}
	if u.User == nil || u.User.Username() == "" {
		return nil, fmt.Errorf("trojan: missing password")
	}
	p := &Profile{Protocol: "trojan", Password: u.User.Username(), Name: u.Fragment}
	if p.Address, p.Port, err = hostPort(u); err != nil {
		return nil, err
	}
	q := u.Query()
	if q.Get("security") == "" {
		q.Set("security", "tls") // trojan is TLS by definition
	}
	p.Flow = q.Get("flow")
	if p.Transport, err = parseTransport(q); err != nil {
		return nil, err
	}
	if p.Security, err = parseSecurity(q); err != nil {
		return nil, err
	}
	return finish(p, p.Password, p.Transport.Type), nil
}

// --- ss:// SIP002 (userinfo base64 or plain for 2022 ciphers) and legacy base64(method:pass@host:port) ---

func parseSS(s string) (*Profile, error) {
	body := s[len("ss://"):]
	name := ""
	if i := strings.IndexByte(body, '#'); i >= 0 {
		name, _ = url.PathUnescape(body[i+1:])
		body = body[:i]
	}
	if !strings.Contains(body, "@") { // legacy: everything base64
		dec, err := DecodeBase64(strings.SplitN(body, "?", 2)[0])
		if err != nil {
			return nil, fmt.Errorf("ss: base64: %w", err)
		}
		body = string(dec)
		at := strings.LastIndexByte(body, '@')
		if at < 0 {
			return nil, fmt.Errorf("ss: bad legacy link")
		}
		body = base64.RawURLEncoding.EncodeToString([]byte(body[:at])) + body[at:]
	}
	u, err := url.Parse("ss://" + body)
	if err != nil {
		return nil, err
	}
	if u.Query().Get("plugin") != "" {
		return nil, fmt.Errorf("ss: plugins (%s) are not supported", strings.SplitN(u.Query().Get("plugin"), ";", 2)[0])
	}
	if u.User == nil {
		return nil, fmt.Errorf("ss: missing credentials")
	}
	cred := u.User.Username()
	if pw, ok := u.User.Password(); ok { // plain "method:password"
		cred += ":" + pw
	} else if dec, err := DecodeBase64(cred); err == nil {
		cred = string(dec)
	}
	method, pass, ok := strings.Cut(cred, ":")
	if !ok || method == "" {
		return nil, fmt.Errorf("ss: bad method:password")
	}
	p := &Profile{Protocol: "shadowsocks", Encryption: method, Password: pass, Name: name,
		Transport: Transport{Type: "tcp"}, Security: Security{Type: "none"}}
	if p.Address, p.Port, err = hostPort(u); err != nil {
		return nil, err
	}
	return finish(p, method, pass), nil
}

// --- hysteria2://auth@host:port[,ports]/?sni=&insecure=1&obfs=salamander&obfs-password=&pinSHA256=#name ---

var portListRe = regexp.MustCompile(`^[0-9]+([,-][0-9]+)*$`)

func parseHysteria2(s string) (*Profile, error) {
	// the authority may carry a port list ("443,20000-30000"), which url.Parse rejects
	scheme, rest, _ := strings.Cut(s, "://")
	end := strings.IndexAny(rest, "/?#")
	if end < 0 {
		end = len(rest)
	}
	authority := rest[:end]
	ports := ""
	if c := strings.LastIndexByte(authority, ':'); c > strings.LastIndexByte(authority, ']') {
		if pl := authority[c+1:]; portListRe.MatchString(pl) && strings.ContainsAny(pl, ",-") {
			ports = pl
			first := strings.FieldsFunc(pl, func(r rune) bool { return r == ',' || r == '-' })[0]
			authority = authority[:c+1] + first
		}
	}
	u, err := url.Parse(scheme + "://" + authority + rest[end:])
	if err != nil {
		return nil, err
	}
	p := &Profile{Protocol: "hysteria2", Name: u.Fragment, Ports: ports}
	if u.User != nil {
		p.Password = u.User.Username()
		if pw, ok := u.User.Password(); ok {
			p.Password += ":" + pw
		}
	}
	if p.Address, p.Port, err = hostPort(u); err != nil {
		return nil, err
	}
	q := u.Query()
	if mp := q.Get("mport"); mp != "" && p.Ports == "" && portListRe.MatchString(mp) {
		p.Ports = mp
	}
	p.Obfs = q.Get("obfs")
	p.ObfsPassword = q.Get("obfs-password")
	if p.Obfs != "" && p.Obfs != "salamander" {
		return nil, fmt.Errorf("hysteria2: unsupported obfs %q", p.Obfs)
	}
	p.UpMbps, _ = strconv.Atoi(q.Get("upmbps"))
	p.DownMbps, _ = strconv.Atoi(q.Get("downmbps"))
	p.Transport = Transport{Type: "quic"}
	p.Security = Security{Type: "tls", SNI: q.Get("sni"), AllowInsecure: q.Get("insecure") == "1", PinSHA256: q.Get("pinSHA256")}
	if a := q.Get("alpn"); a != "" {
		p.Security.ALPN = strings.Split(a, ",")
	}
	return finish(p, p.Password), nil
}

// --- tuic://uuid:password@host:port?congestion_control=bbr&alpn=h3&sni=&udp_relay_mode=native&allow_insecure=1#name ---

func parseTUIC(s string) (*Profile, error) {
	u, err := url.Parse(s)
	if err != nil {
		return nil, err
	}
	if u.User == nil || u.User.Username() == "" {
		return nil, fmt.Errorf("tuic: missing uuid")
	}
	pw, _ := u.User.Password()
	p := &Profile{Protocol: "tuic", UUID: u.User.Username(), Password: pw, Name: u.Fragment}
	if p.Address, p.Port, err = hostPort(u); err != nil {
		return nil, err
	}
	q := u.Query()
	p.Congestion = q.Get("congestion_control")
	p.UDPRelayMode = q.Get("udp_relay_mode")
	p.Transport = Transport{Type: "quic"}
	insecure := q.Get("allow_insecure") == "1" || q.Get("insecure") == "1"
	p.Security = Security{Type: "tls", SNI: q.Get("sni"), AllowInsecure: insecure}
	if a := q.Get("alpn"); a != "" {
		p.Security.ALPN = strings.Split(a, ",")
	}
	return finish(p, p.UUID), nil
}
