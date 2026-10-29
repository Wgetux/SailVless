package tun

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParamsValidate(t *testing.T) {
	ok := Params{Core: "xray", CoreConfig: "{}", SocksPort: 10808, AssetDir: "/home/u/geo", BootstrapDNS: "1.1.1.1", ServerDomains: []string{"a.example.com"}}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []Params{
		{Core: "bash", CoreConfig: "{}", SocksPort: 10808},
		{Core: "xray", CoreConfig: "nope", SocksPort: 10808},
		{Core: "xray", CoreConfig: "{}", SocksPort: 80},
		{Core: "xray", CoreConfig: "{}", SocksPort: 10808, AssetDir: "rel/dir"},
		{Core: "xray", CoreConfig: "{}", SocksPort: 10808, RemoteDNS: "dns.google"},
		{Core: "xray", CoreConfig: "{}", SocksPort: 10808, ServerDomains: []string{"a\"b"}},
	} {
		if bad.Validate() == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
}

func TestSingBoxConfig(t *testing.T) {
	b, err := SingBoxConfig(Params{Core: "xray", SocksPort: 10808, User: "u", Pass: "p", ServerDomains: []string{"srv.example.com"}})
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{`"auto_route": true`, `"srv.example.com"`, `"routing_mark": 21334`, `"username": "u"`, `"ip_version": 6`, IfName} {
		if !strings.Contains(s, want) {
			t.Errorf("config lacks %s", want)
		}
	}
	var v map[string]any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	if dir := os.Getenv("SAILVLESS_DUMP"); dir != "" {
		_ = os.MkdirAll(dir, 0o755)
		_ = os.WriteFile(filepath.Join(dir, "sing-box_tun.json"), b, 0o644)
	}
}
