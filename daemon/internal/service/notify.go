package service

import (
	"context"
	"os"
	"strings"
	"sync"
	"time"

	"sailvless/internal/core"
	"sailvless/internal/geo"
)

// Notifier shows a system notification (set by main; nil = disabled).
type Notifier func(summary, body string)

var notifyState struct {
	sync.Mutex
	last map[string]time.Time
}

// Notification texts; the daemon has no Qt translations, so the few strings
// it shows itself are translated here.
var ruTexts = map[string]string{
	"Subscription: device limit reached":                                       "Подписка: превышен лимит устройств",
	"Remove an old device in your panel or bot, then update the subscription.": "Удалите старое устройство в панели или боте и обновите подписку.",
	"SailVless: no connection":                                                 "SailVless: нет соединения",
	"SailVless: connected again":                                               "SailVless: соединение восстановлено",
}

var russian = func() bool {
	lang := os.Getenv("LANG")
	for _, f := range []string{"/var/lib/environment/nemo/locale.conf", "/etc/locale.conf"} {
		if lang != "" {
			break
		}
		if b, err := os.ReadFile(f); err == nil {
			for _, line := range strings.Split(string(b), "\n") {
				if v, ok := strings.CutPrefix(strings.TrimSpace(line), "LANG="); ok {
					lang = strings.Trim(v, `"`)
				}
			}
		}
	}
	return strings.HasPrefix(lang, "ru")
}()

// flagsToCodes turns flag emoji (regional indicator pairs, missing from
// Sailfish fonts) into country codes: "🇫🇮 Finland" -> "FI Finland".
func flagsToCodes(s string) string {
	var b strings.Builder
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		if r >= 0x1F1E6 && r <= 0x1F1FF && i+1 < len(rs) && rs[i+1] >= 0x1F1E6 && rs[i+1] <= 0x1F1FF {
			b.WriteRune('A' + r - 0x1F1E6)
			b.WriteRune('A' + rs[i+1] - 0x1F1E6)
			i++
			if i+1 < len(rs) && rs[i+1] != ' ' {
				b.WriteRune(' ')
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func tr(s string) string {
	if russian {
		if t, ok := ruTexts[s]; ok {
			return t
		}
	}
	return s
}

// notify sends a notification at most once per 10 minutes per key.
func (s *Service) notify(key, summary, body string) {
	if s.Notify == nil {
		return
	}
	summary, body = tr(summary), flagsToCodes(tr(body))
	notifyState.Lock()
	if notifyState.last == nil {
		notifyState.last = map[string]time.Time{}
	}
	if time.Since(notifyState.last[key]) < 10*time.Minute {
		notifyState.Unlock()
		return
	}
	notifyState.last[key] = time.Now()
	notifyState.Unlock()
	s.Notify(summary, body)
}

// HealthLoop tells the user when the connection they asked for stays broken
// (the core keeps retrying on its own) and when it is back.
func (s *Service) HealthLoop(ctx context.Context) {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	var failingSince time.Time
	notified := false
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		st := s.Status()
		want := s.Store.Settings().Connected
		switch {
		case want && st.State == core.Failed:
			if failingSince.IsZero() {
				failingSince = time.Now()
			}
			if !notified && time.Since(failingSince) > 20*time.Second {
				notified = true
				s.notify("down", "SailVless: no connection", st.Error)
			}
		case st.State == core.Connected:
			if notified {
				s.notify("up", "SailVless: connected again", st.ProfileName)
			}
			failingSince, notified = time.Time{}, false
		default:
			failingSince = time.Time{}
		}
	}
}

// GeoInfo is shown in the settings.
type GeoInfo struct {
	Updated int64  `json:"updated"` // unix seconds, 0 = not downloaded
	Source  string `json:"source"`
}

func (s *Service) GeoInfo() GeoInfo {
	info := GeoInfo{Source: geo.Loyalsoldier.Name}
	if t := geo.Updated(s.GeoDir); !t.IsZero() {
		info.Updated = t.Unix()
	}
	return info
}
