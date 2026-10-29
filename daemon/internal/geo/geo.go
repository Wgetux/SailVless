// Package geo downloads geoip.dat / geosite.dat for Xray, verifying the
// sha256 published next to each file in the upstream release.
package geo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Source is an upstream release that publishes <name> and <name>.sha256sum.
type Source struct {
	Name    string
	BaseURL string
}

var Loyalsoldier = Source{Name: "Loyalsoldier/v2ray-rules-dat", BaseURL: "https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/"}

var Files = []string{"geoip.dat", "geosite.dat"}

// Missing reports which geo files are absent in dir.
func Missing(dir string) []string {
	var out []string
	for _, f := range Files {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			out = append(out, f)
		}
	}
	return out
}

// NeedsGeo tells whether a config references geoip:/geosite: rules.
func NeedsGeo(config []byte) bool {
	s := string(config)
	return strings.Contains(s, `"geoip:`) || strings.Contains(s, `"geosite:`) || strings.Contains(s, `"ext:`)
}

// Update downloads all files from src into dir. client may route through the proxy.
func Update(ctx context.Context, client *http.Client, src Source, dir string) error {
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Minute}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	for _, f := range Files {
		if err := fetchVerified(ctx, client, src.BaseURL+f, filepath.Join(dir, f)); err != nil {
			return fmt.Errorf("%s: %w", f, err)
		}
	}
	return nil
}

func get(ctx context.Context, c *http.Client, url string, w io.Writer, limit int64) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	n, err := io.Copy(w, io.LimitReader(resp.Body, limit+1))
	if err == nil && n > limit {
		err = errors.New("file too large")
	}
	return err
}

func fetchVerified(ctx context.Context, c *http.Client, url, dst string) error {
	var sum strings.Builder
	if err := get(ctx, c, url+".sha256sum", &sum, 4096); err != nil {
		return fmt.Errorf("checksum: %w", err)
	}
	fields := strings.Fields(sum.String())
	if len(fields) == 0 || len(fields[0]) != 64 {
		return errors.New("bad checksum file")
	}
	want := strings.ToLower(fields[0])

	tmp, err := os.CreateTemp(filepath.Dir(dst), filepath.Base(dst)+".tmp*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	h := sha256.New()
	err = get(ctx, c, url, io.MultiWriter(tmp, h), 200<<20)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return fmt.Errorf("sha256 mismatch: got %s, want %s", got, want)
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), dst)
}
