package routing

import "testing"

func TestEffectiveOrder(t *testing.T) {
	c := Default()
	c.Mode, c.BlockAds = Bypass, true
	c.Rules = []Rule{{Kind: "suffix", Value: " Example.ORG ", Action: "proxy"}}
	rules, final := c.Effective()
	if final != "proxy" {
		t.Fatal(final)
	}
	want := []string{"suffix:example.org", "geosite:category-ads-all", "geoip:private", "geosite:category-ru", "geosite:tld-ru", "geoip:ru"}
	if len(rules) != len(want) {
		t.Fatalf("got %v", rules)
	}
	for i, w := range want {
		if got := rules[i].Kind + ":" + rules[i].Value; got != w {
			t.Errorf("rule %d = %s, want %s", i, got, w)
		}
	}
	c.Mode, c.CustomFinal = Custom, "direct"
	if _, f := c.Effective(); f != "direct" {
		t.Fatal("custom final")
	}
	c.Mode, c.Country = Bypass, "de"
	rules, _ = c.Effective()
	if last := rules[len(rules)-1]; last.Value != "de" || rules[len(rules)-2].Kind != "suffix" {
		t.Fatalf("country bypass: %v", rules)
	}
}

func TestValidate(t *testing.T) {
	if err := Default().Validate(); err != nil {
		t.Fatal(err)
	}
	bad := []func(*Config){
		func(c *Config) { c.Mode = "magic" },
		func(c *Config) { c.Mode, c.Country = Bypass, "russia" },
		func(c *Config) { c.Rules = []Rule{{Kind: "ip", Value: "1.2.3.4/99", Action: "direct"}} },
		func(c *Config) { c.Rules = []Rule{{Kind: "domain", Value: "a b", Action: "direct"}} },
		func(c *Config) { c.Rules = []Rule{{Kind: "domain", Value: "a.com", Action: "drop"}} },
		func(c *Config) { c.DNSRemote = "tls://1.1.1.1" },
		func(c *Config) { c.DNSRemote = "https+local://1.1.1.1/dns-query" },
		func(c *Config) { c.DNSDirect = "dns.example" },
	}
	for i, f := range bad {
		c := Default()
		f(&c)
		if c.Validate() == nil {
			t.Errorf("case %d accepted", i)
		}
	}
	ok := Default()
	ok.DNSRemote, ok.DNSDirect = "tcp://8.8.8.8", "https+local://77.88.8.8/dns-query"
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
}
