package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"

	"sailvless/internal/core"
	"sailvless/internal/geo"
	"sailvless/internal/link"
	"sailvless/internal/store"
	"sailvless/internal/sub"
)

// HWIDInfo is what the settings page shows.
type HWIDInfo struct {
	Enabled  bool           `json:"enabled"`
	HWID     string         `json:"hwid"`     // the value actually sent
	Computed string         `json:"computed"` // from machine-id + salt
	Manual   string         `json:"manual"`
	Device   sub.DeviceInfo `json:"device"`
}

func (s *Service) HWID() HWIDInfo {
	set := s.Store.Settings()
	info := HWIDInfo{Enabled: set.HWIDEnabled, Computed: sub.ComputeHWID(s.Store.HWIDSalt()), Manual: set.HWIDManual, Device: sub.DetectDevice()}
	info.HWID = info.Computed
	if info.Manual != "" {
		info.HWID = info.Manual
	}
	return info
}

func (s *Service) ResetHWID() error { return s.Store.ResetHWIDSalt() }

// AddSubscription stores a subscription URL and fetches it.
func (s *Service) AddSubscription(rawURL, name string) (string, error) {
	rawURL = strings.TrimSpace(rawURL)
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return "", errors.New("subscription URL must be http(s)://")
	}
	id := link.StableID("sub", rawURL)
	if existing, _, ok := s.Store.Subscription(id); ok {
		if name != "" {
			existing.Name = name
			if err := s.Store.SaveSubscription(existing, ""); err != nil {
				return "", err
			}
		}
	} else if err := s.Store.SaveSubscription(store.Subscription{ID: id, Name: name, AutoUpdate: true}, rawURL); err != nil {
		return "", err
	}
	s.Logs.Add("sailvlessd: ", fmt.Sprintf("subscription %s added (%s)", id, sub.MaskURL(rawURL)))
	return id, s.UpdateSubscription(id)
}

// UpdateSubscription re-fetches a subscription: directly first, then through
// the running proxy if the direct request fails (blocked panel domain).
func (s *Service) UpdateSubscription(id string) error {
	sb, rawURL, ok := s.Store.Subscription(id)
	if !ok {
		return errors.New("no such subscription")
	}
	set := s.Store.Settings()
	req := sub.Request{URL: rawURL, UserAgent: set.UserAgent}
	if set.HWIDEnabled {
		h := s.HWID()
		req.HWID, req.Device = h.HWID, h.Device
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// try the preferred path first, the other one if it fails at the network level
	proxy := s.proxyClient()
	paths := []string{"direct", "proxy"}
	if sb.ViaProxy {
		paths = []string{"proxy", "direct"}
	}
	var ps []*link.Profile
	var meta sub.Meta
	var perrs []error
	var err error
	via := ""
	for _, path := range paths {
		if path == "proxy" && proxy == nil {
			continue
		}
		if via != "" {
			s.Logs.Add("sailvlessd: ", fmt.Sprintf("subscription %s: %s fetch failed (%v), retrying via %s", id, via, err, path))
		}
		req.Client = nil
		if path == "proxy" {
			req.Client = proxy
		}
		via = path
		ps, meta, perrs, err = sub.Fetch(ctx, req)
		// only network errors are worth a retry: the panel answered otherwise
		if err == nil || errors.Is(err, sub.ErrHWIDLimit) || meta != (sub.Meta{}) {
			break
		}
	}
	sb.LastUpdate = time.Now().Unix()
	if meta != (sub.Meta{}) {
		sb.Meta = meta
	}
	if err != nil {
		sb.LastError = err.Error()
		_ = s.Store.SaveSubscription(sb, "")
		s.Logs.Add("sailvlessd: ", fmt.Sprintf("subscription %s: update failed: %v", id, err))
		if errors.Is(err, sub.ErrHWIDLimit) {
			s.notify("device limit", "Subscription: device limit reached", "Remove an old device in your panel or bot, then update the subscription.")
		}
		return err
	}
	for _, e := range perrs {
		s.Logs.Add("sailvlessd: ", fmt.Sprintf("subscription %s: skipped entry: %v", id, e))
	}
	if sb.Name == "" {
		sb.Name = meta.Title
	}
	if sb.Name == "" {
		sb.Name = sub.MaskURL(rawURL)
	}
	sb.LastError, sb.Count = "", len(ps)

	s.mu.Lock()
	activeID := s.activeID
	s.mu.Unlock()
	before, _ := s.Store.Profile(activeID)
	if err := s.Store.ReplaceSubProfiles(id, ps); err != nil {
		return err
	}
	if err := s.Store.SaveSubscription(sb, ""); err != nil {
		return err
	}
	s.Logs.Add("sailvlessd: ", fmt.Sprintf("subscription %s updated via %s: %d servers, format %s, hwid-active=%v", id, via, len(ps), meta.Format, meta.HWIDActive))

	// reconnect if the server we are using changed
	if activeID != "" && before.SubID == id {
		after, ok := s.Store.Profile(activeID)
		switch {
		case !ok:
			s.Logs.Add("sailvlessd: ", "active server disappeared from the subscription; staying on the old config until reconnect")
		case !reflect.DeepEqual(before, after):
			s.Logs.Add("sailvlessd: ", "active server changed in the subscription; reconnecting")
			return s.Connect(activeID)
		}
	}
	return nil
}

// proxyClient returns an HTTP client through the running local SOCKS inbound, or nil.
func (s *Service) proxyClient() *http.Client {
	st := s.Status()
	if st.State != core.Connected {
		return nil
	}
	u := &url.URL{Scheme: "socks5h", Host: loopbackFor(st.Listen) + ":" + strconv.Itoa(st.SocksPort)}
	if st.Auth {
		user, pass := s.Store.ProxyCredentials()
		u.User = url.UserPassword(user, pass)
	}
	return &http.Client{Timeout: 45 * time.Second, Transport: &http.Transport{Proxy: http.ProxyURL(u)}}
}

// SubscriptionView is a subscription for the UI (URL masked).
type SubscriptionView struct {
	store.Subscription
	MaskedURL    string `json:"maskedUrl"`
	EffectiveHrs int    `json:"effectiveIntervalHours"`
}

func (s *Service) Subscriptions() []SubscriptionView {
	var out []SubscriptionView
	for _, sb := range s.Store.Subscriptions() {
		_, u, _ := s.Store.Subscription(sb.ID)
		out = append(out, SubscriptionView{Subscription: sb, MaskedURL: sub.MaskURL(u), EffectiveHrs: interval(sb)})
	}
	return out
}

func interval(sb store.Subscription) int {
	switch {
	case sb.IntervalHours > 0:
		return sb.IntervalHours
	case sb.Meta.UpdateInterval > 0:
		return sb.Meta.UpdateInterval
	}
	return 24
}

// GeoMaxAge is how old geo databases may get before the weekly refresh.
const GeoMaxAge = 7 * 24 * time.Hour

// AutoUpdateLoop refreshes due subscriptions (failed ones are retried after
// 30 minutes) and geo databases once a week, if they are in use at all.
func (s *Service) AutoUpdateLoop(ctx context.Context) {
	lastTry := map[string]time.Time{}
	var lastGeoTry time.Time
	t := time.NewTicker(5 * time.Minute)
	defer t.Stop()
	for {
		if upd := geo.Updated(s.GeoDir); !upd.IsZero() && time.Since(upd) > GeoMaxAge && time.Since(lastGeoTry) > 6*time.Hour {
			lastGeoTry = time.Now()
			// the running core keeps the old data until the next connect
			_ = s.UpdateGeo()
		}
		for _, sb := range s.Store.Subscriptions() {
			if !sb.AutoUpdate {
				continue
			}
			due := time.Unix(sb.LastUpdate, 0).Add(time.Duration(interval(sb)) * time.Hour)
			if sb.LastError != "" {
				due = lastTry[sb.ID].Add(30 * time.Minute)
			}
			if time.Now().After(due) && time.Since(lastTry[sb.ID]) > 30*time.Minute {
				lastTry[sb.ID] = time.Now()
				_ = s.UpdateSubscription(sb.ID)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
