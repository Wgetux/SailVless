package link

import (
	"encoding/base64"
	"testing"
)

func TestParseVMess(t *testing.T) {
	js := `{"v":"2","ps":"vm","add":"vm.example.com","port":"443","id":"uuid-1","aid":"0","scy":"auto","net":"ws","type":"none","host":"h.example.com","path":"/ws","tls":"tls","sni":"s.example.com","fp":"chrome"}`
	p, err := Parse("vmess://" + base64.StdEncoding.EncodeToString([]byte(js)))
	if err != nil {
		t.Fatal(err)
	}
	if p.Protocol != "vmess" || p.Port != 443 || p.UUID != "uuid-1" || p.Transport.Type != "ws" || p.Transport.Path != "/ws" ||
		p.Security.Type != "tls" || p.Security.SNI != "s.example.com" || p.Name != "vm" {
		t.Fatalf("%+v", p)
	}
	// numeric port and grpc
	js = `{"add":"1.2.3.4","port":8443,"id":"u","net":"grpc","path":"svc","type":"multi","tls":""}`
	p, err = Parse("vmess://" + base64.RawURLEncoding.EncodeToString([]byte(js)))
	if err != nil {
		t.Fatal(err)
	}
	if p.Transport.ServiceName != "svc" || p.Transport.Mode != "multi" || p.Security.Type != "none" {
		t.Fatalf("%+v", p)
	}
}

func TestParseTrojan(t *testing.T) {
	p, err := Parse("trojan://pa%40ss@t.example.com:443?type=grpc&serviceName=g&sni=x.example.com#tr")
	if err != nil {
		t.Fatal(err)
	}
	if p.Password != "pa@ss" || p.Security.Type != "tls" || p.Transport.ServiceName != "g" || p.Name != "tr" {
		t.Fatalf("%+v", p)
	}
}

func TestParseSS(t *testing.T) {
	cases := map[string]struct{ method, pass, host string }{
		// SIP002, base64 userinfo
		"ss://" + base64.RawURLEncoding.EncodeToString([]byte("aes-256-gcm:secret")) + "@ss.example.com:8388#n": {"aes-256-gcm", "secret", "ss.example.com"},
		// SIP002, plain userinfo (2022 ciphers)
		"ss://2022-blake3-aes-128-gcm:a2V5%3D@1.2.3.4:8388#n": {"2022-blake3-aes-128-gcm", "a2V5=", "1.2.3.4"},
		// legacy
		"ss://" + base64.StdEncoding.EncodeToString([]byte("chacha20-ietf-poly1305:p@ss@old.example.com:443")) + "#old": {"chacha20-ietf-poly1305", "p@ss", "old.example.com"},
	}
	for in, want := range cases {
		p, err := Parse(in)
		if err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		if p.Encryption != want.method || p.Password != want.pass || p.Address != want.host {
			t.Errorf("%s: got %q %q %q", in, p.Encryption, p.Password, p.Address)
		}
	}
	if _, err := Parse("ss://YWVzLTI1Ni1nY206eA@h.example.com:1?plugin=obfs-local%3Bobfs%3Dhttp"); err == nil {
		t.Error("plugin should be rejected")
	}
}

func TestParseHysteria2(t *testing.T) {
	p, err := Parse("hy2://auth%3Ax@hy.example.com:443,20000-30000/?sni=s.example.com&insecure=1&obfs=salamander&obfs-password=op#hy")
	if err != nil {
		t.Fatal(err)
	}
	if p.Protocol != "hysteria2" || p.Port != 443 || p.Ports != "443,20000-30000" || p.Password != "auth:x" ||
		p.Obfs != "salamander" || p.ObfsPassword != "op" || !p.Security.AllowInsecure || p.Name != "hy" || p.Core() != "sing-box" {
		t.Fatalf("%+v", p)
	}
	p, err = Parse("hysteria2://pw@[2001:db8::2]:8443?mport=1000-2000")
	if err != nil {
		t.Fatal(err)
	}
	if p.Address != "2001:db8::2" || p.Ports != "1000-2000" {
		t.Fatalf("%+v", p)
	}
}

func TestParseTUIC(t *testing.T) {
	p, err := Parse("tuic://uuid-2:pw@tu.example.com:443?congestion_control=bbr&alpn=h3&udp_relay_mode=quic&allow_insecure=1#tu")
	if err != nil {
		t.Fatal(err)
	}
	if p.UUID != "uuid-2" || p.Password != "pw" || p.Congestion != "bbr" || p.UDPRelayMode != "quic" || !p.Security.AllowInsecure {
		t.Fatalf("%+v", p)
	}
}

func TestStableIDsDiffer(t *testing.T) {
	a, _ := Parse("trojan://a@h.example.com:443")
	b, _ := Parse("trojan://b@h.example.com:443")
	c, _ := Parse("trojan://a@h.example.com:443#renamed")
	if a.ID == b.ID || a.ID != c.ID {
		t.Fatalf("ids: %s %s %s", a.ID, b.ID, c.ID)
	}
}
