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
	rpSys    = "/proc/sys/net/ipv4/conf/all/rp_filter"
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
//     dropped otherwise);
//   - loose rp_filter: unmarked sockets bound to the uplink bypass the tunnel
//     too, and their replies fail the strict check (the reverse route points
//     to the TUN). connmand's online check is one: it would never pass, the
//     system would stay "ready", not "online", and connman-vpnd starts VPNs
//     without a daemon (ours) only when online.
func SetupNetwork() error {
	Cleanup()
	if err := setSysctl(vmarkSys, "src_valid_mark", "1"); err != nil {
		return err
	}
	if err := setSysctl(rpSys, "rp_filter", "2"); err != nil {
		return err
	}
	mark := "0x" + strconv.FormatInt(Mark, 16)
	pref := strconv.Itoa(MarkRulePref)
	// marked traffic must never fall through to the tunnel's table: without a
	// route in main (e.g. no IPv6 on Wi-Fi) it would loop back into the TUN.
	// "unreachable" fails such dials at once so the core falls back to IPv4.
	stop := strconv.Itoa(MarkRulePref + 1)
	for _, fam := range []string{"-4", "-6"} {
		if err := run("ip", fam, "rule", "add", "fwmark", mark, "lookup", "main", "pref", pref); err != nil {
			return err
		}
		if err := run("ip", fam, "rule", "add", "fwmark", mark, "unreachable", "pref", stop); err != nil {
			return err
		}
	}
	flushResolver()
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
	return setupDNSRedirect(mark)
}

const (
	chainDNS  = "SAILVLESS_DNS"
	chainDNS6 = "SAILVLESS_DNS6"
)

var privateV4 = "10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,100.64.0.0/10,169.254.0.0/16"

// setupDNSRedirect closes the DNS leak around the tunnel: LAN ranges are
// excluded from the TUN (so the phone stays reachable from the LAN), but the
// system resolver usually asks the router (192.168.x.1), which would forward
// queries to the provider. Such queries get a mark that re-routes them into
// the TUN, where sing-box answers them (hijack-dns): the tunnel's own table
// has no LAN routes, so they use DNSTable, see RouteDNS. A socket bound to the
// uplink (connmand's online check) has no route there and stays on it, which a
// DNAT would break. The kernel has no IPv6 NAT, so DNS to private IPv6
// servers is rejected and the resolver falls back to IPv4. Marked traffic (the
// core itself) is left alone.
func setupDNSRedirect(mark string) error {
	dnsMark := "0x" + strconv.FormatInt(DNSMark, 16)
	if err := run("ip", "-4", "rule", "add", "fwmark", dnsMark, "lookup", strconv.Itoa(DNSTable),
		"pref", strconv.Itoa(MarkRulePref+2)); err != nil {
		return err
	}
	for _, proto := range []string{"udp", "tcp"} {
		if err := run("iptables", "-w", "-t", "mangle", "-A", chainOut, "-m", "mark", "!", "--mark", mark,
			"-d", privateV4, "-p", proto, "--dport", "53", "-j", "MARK", "--set-mark", dnsMark); err != nil {
			return err
		}
	}
	cmds6 := [][]string{
		{"-N", chainDNS6},
		{"-A", chainDNS6, "-m", "mark", "--mark", mark, "-j", "RETURN"},
		{"-A", chainDNS6, "-d", "fc00::/7,fe80::/10", "-p", "udp", "--dport", "53", "-j", "REJECT"},
		{"-A", chainDNS6, "-d", "fc00::/7,fe80::/10", "-p", "tcp", "--dport", "53", "-j", "REJECT", "--reject-with", "tcp-reset"},
		{"-I", "OUTPUT", "-j", chainDNS6},
	}
	for _, c := range cmds6 {
		if err := run("ip6tables", append([]string{"-w"}, c...)...); err != nil {
			return err
		}
	}
	return nil
}

// RouteDNS points DNSTable at the TUN; call it when the interface is up
// (the route goes away with the interface).
func RouteDNS() error {
	return run("ip", "-4", "route", "replace", "default", "dev", IfName, "table", strconv.Itoa(DNSTable))
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
		_ = run("ip", fam, "route", "flush", "table", strconv.Itoa(DNSTable))
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
	_ = run("iptables", "-w", "-t", "nat", "-D", "OUTPUT", "-j", chainDNS)
	_ = run("iptables", "-w", "-t", "nat", "-F", chainDNS)
	_ = run("iptables", "-w", "-t", "nat", "-X", chainDNS)
	_ = run("ip6tables", "-w", "-D", "OUTPUT", "-j", chainDNS6)
	_ = run("ip6tables", "-w", "-F", chainDNS6)
	_ = run("ip6tables", "-w", "-X", chainDNS6)
	restoreSysctl(vmarkSys, "src_valid_mark")
	restoreSysctl(rpSys, "rp_filter")
	flushResolver()
}

// setSysctl saves the current value in RuntimeDir (Cleanup restores it, also
// after a crash) and writes the new one.
func setSysctl(path, name, value string) error {
	if b, err := os.ReadFile(path); err == nil {
		_ = os.WriteFile(filepath.Join(RuntimeDir, name), b, 0o600)
	}
	return os.WriteFile(path, []byte(value), 0o644)
}

func restoreSysctl(path, name string) {
	saved := filepath.Join(RuntimeDir, name)
	if b, err := os.ReadFile(saved); err == nil {
		_ = os.WriteFile(path, b, 0o644)
		_ = os.Remove(saved)
	}
}

// flushResolver drops systemd-resolved's cache: answers obtained outside the
// tunnel (provider DNS, AAAA records) must not outlive the switch.
func flushResolver() {
	if run("resolvectl", "flush-caches") != nil {
		_ = run("systemd-resolve", "--flush-caches")
	}
}
