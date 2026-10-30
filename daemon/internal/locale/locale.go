// Package locale reads the phone's language (the daemon has no Qt
// translations; it needs the language for its few own texts and defaults).
package locale

import (
	"os"
	"strings"
)

// Lang returns the system locale, e.g. "ru_RU.utf8", "zh_CN.UTF-8" ("" if unknown).
var Lang = func() string {
	if l := os.Getenv("LANG"); l != "" {
		return l
	}
	for _, f := range []string{"/var/lib/environment/nemo/locale.conf", "/etc/locale.conf"} {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(b), "\n") {
			if v, ok := strings.CutPrefix(strings.TrimSpace(line), "LANG="); ok {
				return strings.Trim(v, `"`)
			}
		}
	}
	return ""
}()

// Language returns the language code: "ru", "zh", "en", …
func Language(lang string) string {
	l, _, _ := strings.Cut(lang, "_")
	l, _, _ = strings.Cut(l, ".")
	return strings.ToLower(l)
}

// Country returns the lower-case region code ("cn" for zh_CN.UTF-8), or "".
func Country(lang string) string {
	_, r, ok := strings.Cut(lang, "_")
	if !ok {
		return ""
	}
	r, _, _ = strings.Cut(r, ".")
	r, _, _ = strings.Cut(r, "@")
	if len(r) != 2 {
		return ""
	}
	return strings.ToLower(r)
}
