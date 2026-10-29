package link

import "testing"

func TestParseVLESSRealityXHTTP(t *testing.T) {
	p, err := Parse("vless://11111111-2222-3333-4444-555555555555@example.com:8443?type=xhttp&path=%2Fabc&mode=stream-one&security=reality&fp=chrome&sni=www.example.org&pbk=PUBKEY&sid=0a1b&extra=%7B%22x%22%3A1%7D#My%20Server")
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "My Server" || p.Address != "example.com" || p.Port != 8443 {
		t.Fatalf("basic fields: %+v", p)
	}
	if p.Transport.Type != "xhttp" || p.Transport.Path != "/abc" || p.Transport.Mode != "stream-one" || p.Transport.Extra != `{"x":1}` {
		t.Fatalf("transport: %+v", p.Transport)
	}
	if p.Security.Type != "reality" || p.Security.PublicKey != "PUBKEY" || p.Security.ShortID != "0a1b" || p.Security.SNI != "www.example.org" {
		t.Fatalf("security: %+v", p.Security)
	}
	if p.Encryption != "none" || p.ID == "" {
		t.Fatalf("defaults: %+v", p)
	}
}

func TestParseVLESSDefaultsAndErrors(t *testing.T) {
	p, err := Parse("vless://uuid@[2001:db8::1]:443?security=tls&alpn=h2,http/1.1&flow=xtls-rprx-vision")
	if err != nil {
		t.Fatal(err)
	}
	if p.Address != "2001:db8::1" || p.Transport.Type != "tcp" || len(p.Security.ALPN) != 2 || p.Flow != "xtls-rprx-vision" {
		t.Fatalf("%+v", p)
	}
	for _, bad := range []string{
		"vless://@host:443",
		"vless://uuid@host:0",
		"vless://uuid@host:443?security=reality",
		"vless://uuid@host:443?type=quic",
		"foo",
	} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("expected error for %q", bad)
		}
	}
}
