package awg

import (
	"strings"
	"testing"
)

const sample = `# Name = Home AWG
[Interface]
PrivateKey = yAnz5TF+lXXJte14tji3zlMNq+hd2rYUIgJBgB3fBmk=
Address = 10.8.1.2/32, fd00::2/128
DNS = 1.1.1.1, 2606:4700:4700::1111, example.lan
MTU = 1280
Jc = 4
Jmin = 40
Jmax = 70
S1 = 52
S2 = 102
H1 = 1234567
H2 = 2345678
H3 = 3456789
H4 = 4567890
I1 = <b 0xc000000001>

[Peer]
PublicKey = xTIBA5rboUvnH4htodjb6e697QjLERt1NAB4mZqp8Dg=
PresharedKey = TS6+0Wl9xVdcPSfA3vy0mkh7Fv1rgOtrKEXa8Nm+K94=
Endpoint = vpn.example.com:51820
AllowedIPs = 0.0.0.0/0, ::/0
PersistentKeepalive = 25
`

func TestParseAndUAPI(t *testing.T) {
	if !LooksLikeConf(sample) {
		t.Fatal("not detected")
	}
	c, err := Parse(sample)
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "Home AWG" || len(c.Addresses) != 2 || len(c.DNS) != 2 || c.MTU != 1280 || c.Obfuscation["jc"] != "4" || c.Obfuscation["i1"] != "<b 0xc000000001>" {
		t.Fatalf("%+v", c)
	}
	if h, p := c.Endpoint(); h != "vpn.example.com" || p != 51820 {
		t.Fatal(h, p)
	}
	u, err := c.UAPI(0x5356, func(string) (string, error) { return "203.0.113.7", nil })
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"private_key=c809f3e531", "fwmark=21334\n", "jc=4\n", "h4=4567890\n", "i1=<b 0xc000000001>\n",
		"endpoint=203.0.113.7:51820\n", "persistent_keepalive_interval=25\n", "allowed_ip=::/0\n", "preshared_key="} {
		if !strings.Contains(u, want) {
			t.Errorf("UAPI lacks %q", want)
		}
	}
	if _, err := Parse(strings.Replace(sample, "PublicKey = xT", "PublicKey = !!", 1)); err == nil {
		t.Error("bad key accepted")
	}
}

// AmneziaWG 3.1 transport options reach the device in UAPI form
func TestAWG31Options(t *testing.T) {
	conf := strings.Replace(sample, "I1 = <b 0xc000000001>", `S3 = 12
S4 = 12
HeaderProtectionKey = TS6+0Wl9xVdcPSfA3vy0mkh7Fv1rgOtrKEXa8Nm+K94=
ContentPaddingAddition = 2
RandomTrailers = on
DisableCookies = off`, 1)
	c, err := Parse(conf)
	if err != nil {
		t.Fatal(err)
	}
	u, err := c.UAPI(0, func(string) (string, error) { return "192.0.2.1", nil })
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"s3=12\n", "s4=12\n",
		"header_protection_key=4d2ebed1697dc5575c3d27c0defcb49a487b16fd6b80eb6b2845daf0d9be2bde\n",
		"content_padding_addition=2\n", "random_trailers=true\n", "disable_cookies=false\n",
	} {
		if !strings.Contains(u, want) {
			t.Errorf("UAPI lacks %q", want)
		}
	}
}
