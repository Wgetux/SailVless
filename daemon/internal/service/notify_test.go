package service

import "testing"

func TestFlagsToCodes(t *testing.T) {
	for in, want := range map[string]string{
		"🇫🇮 Finland":    "FI Finland",
		"🇵🇱WHITE":       "PL WHITE",
		"no flags here": "no flags here",
		"🇩🇪":            "DE",
	} {
		if got := flagsToCodes(in); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
}
