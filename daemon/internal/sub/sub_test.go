package sub

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const links = "vless://u@a.example.com:443?security=tls#A\ntrojan://p@b.example.com:443#B\nbogus://x\n"

func TestParseFormats(t *testing.T) {
	ps, f, errs := Parse([]byte(links))
	if f != "links" || len(ps) != 2 || len(errs) != 1 {
		t.Fatalf("plain: %s %d %v", f, len(ps), errs)
	}
	if ps, f, _ = Parse([]byte("\xef\xbb\xbf" + links)); f != "links" || len(ps) != 2 {
		t.Fatalf("bom: %s %d", f, len(ps))
	}
	ps, f, _ = Parse([]byte(base64.StdEncoding.EncodeToString([]byte(links))))
	if f != "base64" || len(ps) != 2 {
		t.Fatalf("base64: %s %d", f, len(ps))
	}
	xr := `[{"remarks":"X1","outbounds":[{"protocol":"vless","tag":"proxy"}]},{"remarks":"X2","outbounds":[{"protocol":"trojan"}]}]`
	ps, f, _ = Parse([]byte(xr))
	if f != "xray-json" || len(ps) != 2 || ps[0].Name != "X1" || ps[0].Raw == "" {
		t.Fatalf("xray: %s %+v", f, ps)
	}
	sb := `{"outbounds":[{"type":"selector","tag":"sel"},{"type":"hysteria2","tag":"H","server":"h.example.com","server_port":443},{"type":"direct","tag":"d"},{"type":"vless","tag":"V","server":"v.example.com","server_port":8443}]}`
	ps, f, _ = Parse([]byte(sb))
	if f != "singbox-json" || len(ps) != 2 || ps[0].Name != "H" || ps[1].Port != 8443 || !strings.Contains(ps[0].Raw, `"tag":"proxy"`) {
		t.Fatalf("singbox: %s %+v", f, ps)
	}
}

func TestFetchHeadersAndHWID(t *testing.T) {
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.Header().Set("subscription-userinfo", "upload=100; download=2.5e3; total=10737418240; expire=1893456000")
		w.Header().Set("profile-title", "base64:"+base64.StdEncoding.EncodeToString([]byte("Мой VPN")))
		w.Header().Set("profile-update-interval", "12")
		w.Header().Set("support-url", "https://t.me/support")
		w.Header().Set("x-hwid-active", "true")
		w.Write([]byte(base64.StdEncoding.EncodeToString([]byte(links))))
	}))
	defer srv.Close()
	ps, m, _, err := Fetch(context.Background(), Request{URL: srv.URL, UserAgent: "UA/1", HWID: "abcdef0123456789",
		Device: DeviceInfo{OS: "SailfishOS", Version: "5.1.0.11", Model: "OnePlus 6T"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 2 || m.Title != "Мой VPN" || m.Download != 2500 || m.Total != 10737418240 || m.Expire != 1893456000 ||
		m.UpdateInterval != 12 || !m.HWIDActive || m.SupportURL != "https://t.me/support" {
		t.Fatalf("meta %+v", m)
	}
	for k, v := range map[string]string{"User-Agent": "UA/1", "X-Hwid": "abcdef0123456789", "X-Device-Os": "SailfishOS", "X-Ver-Os": "5.1.0.11", "X-Device-Model": "OnePlus 6T"} {
		if got.Get(k) != v {
			t.Errorf("header %s = %q, want %q", k, got.Get(k), v)
		}
	}
}

func TestFetchHWIDLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-hwid-max-devices-reached", "true")
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	if _, _, _, err := Fetch(context.Background(), Request{URL: srv.URL, UserAgent: "x", HWID: "abcdef0123456789"}); err != ErrHWIDLimit {
		t.Fatalf("got %v", err)
	}
}

func TestMaskAndHWID(t *testing.T) {
	if m := MaskURL("https://secret-panel.example.com/sub/TOKEN123"); strings.Contains(m, "TOKEN") || strings.Contains(m, "secret-panel") {
		t.Fatalf("leak: %s", m)
	}
	h := ComputeHWID("salt")
	if !HWIDPattern.MatchString(h) || h != ComputeHWID("salt") || h == ComputeHWID("other") {
		t.Fatalf("hwid %q", h)
	}
	if dedupWords("OnePlus OnePlus 6T") != "OnePlus 6T" {
		t.Fatal("dedup")
	}
}
