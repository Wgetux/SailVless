package tun

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// RuntimeDir is the helper's systemd RuntimeDirectory.
const RuntimeDir = "/run/sailvless-tun"

const (
	chainOut = "SAILVLESS_OUT"
	chainPre = "SAILVLESS_PRE"
	vmarkSys = "/proc/sys/net/ipv4/conf/all/src_valid_mark"
)

func run(name string, args ...string) error {
	path, err := exec.LookPath(name)
	if err != nil {
		for _, dir := range []string{"/usr/sbin", "/sbin", "/usr/bin", "/bin"} {
			if _, e := os.Stat(filepath.Join(dir, name)); e == nil {
				path, err = filepath.Join(dir, name), nil
				break
			}
		}
	}
	if err != nil {
		return err
	}
	out, err := exec.Command(path, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %v: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// SetupNetwork lets marked traffic (the proxy core) bypass the tunnel:
//   - "fwmark Mark lookup main" ahead of sing-box's rules;
//   - CONNMARK restores the mark on replies and src_valid_mark makes the
//     reverse-path filter honour it (wlan0 has rp_filter=1, replies would be
//     dropped otherwise).
func SetupNetwork() error {
	Cleanup()
	if b, err := os.ReadFile(vmarkSys); err == nil {
		_ = os.WriteFile(filepath.Join(RuntimeDir, "src_valid_mark"), b, 0o600)
	}
	if err := os.WriteFile(vmarkSys, []byte("1"), 0o644); err != nil {
		return err
	}
	mark := "0x" + strconv.FormatInt(Mark, 16)
	pref := strconv.Itoa(MarkRulePref)
	for _, fam := range []string{"-4", "-6"} {
		if err := run("ip", fam, "rule", "add", "fwmark", mark, "lookup", "main", "pref", pref); err != nil {
			return err
		}
	}
	for _, ipt := range []string{"iptables", "ip6tables"} {
		cmds := [][]string{
			{"-t", "mangle", "-N", chainOut},
			{"-t", "mangle", "-N", chainPre},
			{"-t", "mangle", "-A", chainOut, "-m", "mark", "--mark", mark, "-j", "CONNMARK", "--save-mark"},
			{"-t", "mangle", "-A", chainPre, "-m", "connmark", "--mark", mark, "-j", "CONNMARK", "--restore-mark"},
			{"-t", "mangle", "-I", "OUTPUT", "-j", chainOut},
			{"-t", "mangle", "-I", "PREROUTING", "-j", chainPre},
		}
		for _, c := range cmds {
			if err := run(ipt, append([]string{"-w"}, c...)...); err != nil {
				if ipt == "ip6tables" {
					break // IPv6 netfilter is optional
				}
				return err
			}
		}
	}
	return nil
}

// Cleanup removes everything SetupNetwork and sing-box's auto_route may have
// left behind (also after a crash). Safe to call any time.
func Cleanup() {
	for _, fam := range []string{"-4", "-6"} {
		for pref := MarkRulePref; pref <= RuleLast; pref++ {
			for i := 0; i < 16 && run("ip", fam, "rule", "del", "pref", strconv.Itoa(pref)) == nil; i++ {
			}
		}
		_ = run("ip", fam, "route", "flush", "table", strconv.Itoa(TableIndex))
	}
	_ = run("ip", "link", "del", IfName)
	for _, ipt := range []string{"iptables", "ip6tables"} {
		_ = run(ipt, "-w", "-t", "mangle", "-D", "OUTPUT", "-j", chainOut)
		_ = run(ipt, "-w", "-t", "mangle", "-D", "PREROUTING", "-j", chainPre)
		for _, ch := range []string{chainOut, chainPre} {
			_ = run(ipt, "-w", "-t", "mangle", "-F", ch)
			_ = run(ipt, "-w", "-t", "mangle", "-X", ch)
		}
	}
	saved := filepath.Join(RuntimeDir, "src_valid_mark")
	if b, err := os.ReadFile(saved); err == nil {
		_ = os.WriteFile(vmarkSys, b, 0o644)
		_ = os.Remove(saved)
	}
}
