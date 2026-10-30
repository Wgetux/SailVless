package locale

import "testing"

func TestParse(t *testing.T) {
	for _, c := range []struct{ in, lang, country string }{
		{"zh_CN.UTF-8", "zh", "cn"}, {"ru_RU.utf8", "ru", "ru"}, {"en_GB", "en", "gb"},
		{"de_DE@euro", "de", "de"}, {"C", "c", ""}, {"", "", ""},
	} {
		if Language(c.in) != c.lang || Country(c.in) != c.country {
			t.Errorf("%q: %q %q", c.in, Language(c.in), Country(c.in))
		}
	}
}
