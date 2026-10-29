package sub

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"regexp"
	"strings"
)

// HWIDPattern is what Remnawave (>= 3.0) accepts; other values are ignored by the panel.
var HWIDPattern = regexp.MustCompile(`^[a-zA-Z0-9=-]{10,64}$`)

// DeviceInfo is sent as x-device-os / x-ver-os / x-device-model.
type DeviceInfo struct {
	OS      string `json:"os"`
	Version string `json:"version"`
	Model   string `json:"model"`
}

// ComputeHWID derives a stable id: sha256(salt + machine-id), 32 hex chars.
// Changing the salt ("reset") yields a new id; the machine id never leaves the device.
func ComputeHWID(salt string) string {
	mid, _ := os.ReadFile("/etc/machine-id")
	h := sha256.Sum256([]byte("sailvless-hwid\x00" + salt + "\x00" + strings.TrimSpace(string(mid))))
	return hex.EncodeToString(h[:16])
}

// DetectDevice reads /etc/os-release and /etc/hw-release.
func DetectDevice() DeviceInfo {
	osr := readRelease("/etc/os-release")
	hw := readRelease("/etc/hw-release")
	d := DeviceInfo{OS: "SailfishOS", Version: osr["VERSION_ID"], Model: dedupWords(hw["NAME"])}
	if d.Model == "" {
		d.Model = "Sailfish device"
	}
	return d
}

func readRelease(path string) map[string]string {
	m := map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		return m
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), "=")
		if ok && !strings.HasPrefix(k, "#") {
			m[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"`)
		}
	}
	return m
}

// dedupWords turns "OnePlus OnePlus 6T" into "OnePlus 6T".
func dedupWords(s string) string {
	f := strings.Fields(s)
	var out []string
	for i, w := range f {
		if i > 0 && strings.EqualFold(w, f[i-1]) {
			continue
		}
		out = append(out, w)
	}
	return strings.Join(out, " ")
}
