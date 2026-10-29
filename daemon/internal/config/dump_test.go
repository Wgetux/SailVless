// Package config_test dumps generated configs for every protocol/transport so
// they can be validated by the real cores on the device:
//
//	SAILVLESS_DUMP=out/configs go test ./internal/config/
//	# then on the phone: xray run -test -c X / sing-box check -c X
package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"sailvless/internal/config/singbox"
	"sailvless/internal/config/xray"
	"sailvless/internal/link"
)

var samples = map[string]string{
	"vless-reality-xhttp": "vless://11111111-2222-3333-4444-555555555555@example.com:443?type=xhttp&path=%2Fx&mode=stream-one&security=reality&fp=chrome&sni=www.example.org&pbk=Z84J2IelR9ch3k8VtlVhhs5ycBUlXA7wHBWcBrjqnAw&sid=6ba85179",
	"vless-vision-tcp":    "vless://11111111-2222-3333-4444-555555555555@example.com:443?security=reality&flow=xtls-rprx-vision&sni=www.example.org&pbk=Z84J2IelR9ch3k8VtlVhhs5ycBUlXA7wHBWcBrjqnAw&sid=6ba85179",
	"vless-ws-tls":        "vless://11111111-2222-3333-4444-555555555555@example.com:443?type=ws&path=%2Fws&host=cdn.example.com&security=tls&sni=cdn.example.com&alpn=http%2F1.1",
	"vless-grpc-tls":      "vless://11111111-2222-3333-4444-555555555555@example.com:443?type=grpc&serviceName=svc&mode=multi&security=tls&sni=example.com",
	"vless-httpupgrade":   "vless://11111111-2222-3333-4444-555555555555@example.com:443?type=httpupgrade&path=%2Fup&host=example.com&security=tls&sni=example.com",
	"vmess-tcp-http":      "vmess://eyJhZGQiOiJleGFtcGxlLmNvbSIsInBvcnQiOiI4MCIsImlkIjoiMTExMTExMTEtMjIyMi0zMzMzLTQ0NDQtNTU1NTU1NTU1NTU1IiwibmV0IjoidGNwIiwidHlwZSI6Imh0dHAiLCJob3N0IjoiZXhhbXBsZS5jb20iLCJwYXRoIjoiLyIsInRscyI6IiJ9",
	"trojan-tls":          "trojan://password@example.com:443?sni=example.com",
	"ss-aead":             "ss://YWVzLTI1Ni1nY206c2VjcmV0@example.com:8388",
	"ss-2022":             "ss://2022-blake3-aes-128-gcm:MTIzNDU2Nzg5MDEyMzQ1Ng%3D%3D@example.com:8388",
	"hy2-hop-obfs":        "hy2://pw@example.com:443,20000-30000/?sni=example.com&obfs=salamander&obfs-password=op&upmbps=50&downmbps=200",
	"tuic":                "tuic://11111111-2222-3333-4444-555555555555:pw@example.com:443?congestion_control=bbr&udp_relay_mode=native&sni=example.com",
}

func TestDumpConfigs(t *testing.T) {
	// vmess built from JSON, see link tests; add one here too
	samples["vmess-ws-tls"] = "vmess://eyJhZGQiOiJleGFtcGxlLmNvbSIsInBvcnQiOiI0NDMiLCJpZCI6IjExMTExMTExLTIyMjItMzMzMy00NDQ0LTU1NTU1NTU1NTU1NSIsIm5ldCI6IndzIiwicGF0aCI6Ii93cyIsInRscyI6InRscyIsInNuaSI6ImV4YW1wbGUuY29tIn0"
	dir := os.Getenv("SAILVLESS_DUMP")
	opts := xray.Options{Inbounds: xray.Inbounds{SocksPort: 10808, HTTPPort: 10809, User: "u", Pass: "p"}, APIListen: "127.0.0.1:10900", APISecret: "s"}
	for name, s := range samples {
		p, err := link.Parse(s)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var b []byte
		if p.Core() == "sing-box" {
			b, _, err = singbox.Build(p, opts)
		} else {
			b, err = xray.Build(p, opts)
		}
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if dir != "" {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, p.Core()+"_"+name+".json"), b, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
}
