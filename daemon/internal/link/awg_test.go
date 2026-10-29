package link

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"testing"
)

const awgConf = `[Interface]
PrivateKey = yAnz5TF+lXXJte14tji3zlMNq+hd2rYUIgJBgB3fBmk=
Address = 10.8.1.2/32
DNS = 1.1.1.1
Jc = 4
Jmin = 40
Jmax = 70
S1 = 52
S2 = 102
H1 = 1
H2 = 2
H3 = 3
H4 = 4

[Peer]
PublicKey = xTIBA5rboUvnH4htodjb6e697QjLERt1NAB4mZqp8Dg=
Endpoint = 198.51.100.9:51820
AllowedIPs = 0.0.0.0/0
`

func TestParseAWGAndVPNLink(t *testing.T) {
	p, err := ParseAWG(awgConf)
	if err != nil {
		t.Fatal(err)
	}
	if p.Protocol != "amneziawg" || p.Address != "198.51.100.9" || p.Port != 51820 || p.Raw == "" || p.Transport.Type != "udp" {
		t.Fatalf("%+v", p)
	}

	// AmneziaVPN export: last_config is a JSON string with the config inside
	last, _ := json.Marshal(map[string]any{"config": awgConf, "mtu": "1280"})
	doc, _ := json.Marshal(map[string]any{
		"description": "My server",
		"containers":  []any{map[string]any{"container": "amnezia-awg", "awg": map[string]any{"last_config": string(last)}}},
	})
	var z bytes.Buffer
	w := zlib.NewWriter(&z)
	w.Write(doc)
	w.Close()
	payload := binary.BigEndian.AppendUint32(nil, uint32(len(doc)))
	payload = append(payload, z.Bytes()...)
	q, err := Parse("vpn://" + base64.RawURLEncoding.EncodeToString(payload))
	if err != nil {
		t.Fatal(err)
	}
	if q.Name != "My server" || q.ID != p.ID {
		t.Fatalf("%+v", q)
	}
}
