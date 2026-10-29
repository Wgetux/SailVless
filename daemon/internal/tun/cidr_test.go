package tun

import (
	"net/netip"
	"strings"
	"testing"
)

func TestSubtract(t *testing.T) {
	base := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("192.168.0.0/16")}
	holes := []netip.Prefix{netip.MustParsePrefix("10.30.0.0/15"), netip.MustParsePrefix("10.29.8.0/24")}
	out := subtract(base, holes)
	for _, ip := range []string{"10.30.1.1", "10.31.255.1", "10.29.8.5"} {
		a := netip.MustParseAddr(ip)
		for _, p := range out {
			if p.Contains(a) {
				t.Errorf("%s still excluded via %s", ip, p)
			}
		}
	}
	for _, ip := range []string{"10.0.0.1", "10.29.9.1", "10.32.0.1", "10.255.0.1", "192.168.31.1"} {
		a := netip.MustParseAddr(ip)
		found := false
		for _, p := range out {
			found = found || p.Contains(a)
		}
		if !found {
			t.Errorf("%s lost from the exclusion", ip)
		}
	}
	if len(subtract(base, nil)) != 2 {
		t.Error("no holes must keep the base as is")
	}
}

func TestSingBoxConfigInclude(t *testing.T) {
	b, err := SingBoxConfig(Params{Core: "xray", SocksPort: 10808, Include: []string{"10.30.0.0/15"}})
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if strings.Contains(s, `"10.0.0.0/8"`) || !strings.Contains(s, `"192.168.0.0/16"`) {
		t.Fatal("10.0.0.0/8 must be split around the included network")
	}
	if strings.Index(s, `"ip_cidr"`) > strings.Index(s, `"ip_is_private"`) {
		t.Fatal("included networks must be routed before the private->direct rule")
	}
}
