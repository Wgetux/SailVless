package geo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestUpdateVerifiesChecksum(t *testing.T) {
	body := []byte("geodata")
	sum := sha256.Sum256(body)
	good := hex.EncodeToString(sum[:])
	checksum := good
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if filepath.Ext(r.URL.Path) == ".sha256sum" {
			w.Write([]byte(checksum + "  " + filepath.Base(r.URL.Path) + "\n"))
			return
		}
		w.Write(body)
	}))
	defer srv.Close()
	src := Source{Name: "test", BaseURL: srv.URL + "/"}
	dir := t.TempDir()

	if err := Update(context.Background(), srv.Client(), src, dir); err != nil {
		t.Fatal(err)
	}
	if len(Missing(dir)) != 0 {
		t.Fatal("files missing after update")
	}

	checksum = "0000000000000000000000000000000000000000000000000000000000000000"
	body = []byte("tampered")
	if err := Update(context.Background(), srv.Client(), src, dir); err == nil {
		t.Fatal("tampered file accepted")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "geoip.dat")); string(b) != "geodata" {
		t.Fatalf("old file replaced: %q", b)
	}
}
