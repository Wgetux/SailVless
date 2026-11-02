package service

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"

	"sailvless/internal/core"
)

// WatchNetwork restarts the connection when the uplink changes (Wi-Fi <-> LTE):
// the core's connections to the server are bound to the old address and would
// hang until TCP timeouts. Route changes come from netlink (no privileges needed).
func (s *Service) WatchNetwork(ctx context.Context) {
	fd, err := syscall.Socket(syscall.AF_NETLINK, syscall.SOCK_RAW|syscall.SOCK_CLOEXEC, syscall.NETLINK_ROUTE)
	if err != nil {
		s.Logs.Add("sailvlessd: ", "network watch unavailable: "+err.Error())
		return
	}
	const (
		rtmgrpIPv4Ifaddr = 0x10
		rtmgrpIPv4Route  = 0x40
		rtmgrpIPv6Route  = 0x400
	)
	groups := uint32(rtmgrpIPv4Ifaddr | rtmgrpIPv4Route | rtmgrpIPv6Route)
	if err := syscall.Bind(fd, &syscall.SockaddrNetlink{Family: syscall.AF_NETLINK, Groups: groups}); err != nil {
		syscall.Close(fd)
		s.Logs.Add("sailvlessd: ", "network watch unavailable: "+err.Error())
		return
	}
	events := make(chan struct{}, 1)
	go func() {
		buf := make([]byte, 64*1024)
		for {
			if _, _, err := syscall.Recvfrom(fd, buf, 0); err != nil {
				if err == syscall.EINTR || err == syscall.ENOBUFS {
					continue
				}
				return
			}
			select {
			case events <- struct{}{}:
			default:
			}
		}
	}()
	go func() {
		<-ctx.Done()
		syscall.Close(fd)
	}()

	last := defaultRoute()
	for {
		select {
		case <-ctx.Done():
			return
		case <-events:
		}
		// let the switch settle: connman adds/removes routes in bursts
		quiet := time.NewTimer(3 * time.Second)
	settle:
		for {
			select {
			case <-ctx.Done():
				quiet.Stop()
				return
			case <-events:
				quiet.Reset(3 * time.Second)
			case <-quiet.C:
				break settle
			}
		}
		cur := defaultRoute()
		if cur == last {
			continue
		}
		s.Logs.Add("sailvlessd: ", fmt.Sprintf("uplink changed: %q -> %q", last, cur))
		last = cur
		if cur != "" {
			s.restartAfterNetworkChange()
		}
	}
}

func (s *Service) restartAfterNetworkChange() {
	st := s.Status()
	if st.ProfileID == "" || !s.Store.Settings().Connected {
		return
	}
	if st.State != core.Connected && st.State != core.Failed && st.State != core.Connecting {
		return
	}
	if st.Tun && st.State == core.Connected {
		// keep the tunnel up (no leak window), just renew the core's connections
		s.Logs.Add("sailvlessd: ", "restarting the core on the new network")
		err := s.restartTunCore()
		if err == nil {
			return
		}
		s.Logs.Add("sailvlessd: ", "core restart failed, reconnecting: "+err.Error())
	}
	s.Logs.Add("sailvlessd: ", "reconnecting on the new network")
	if err := s.Connect(st.ProfileID); err != nil {
		s.Logs.Add("sailvlessd: ", "reconnect failed: "+err.Error())
	}
}

// localOnly tells interfaces that never lead to the internet: connman treats a
// container's veth as a cable and gives it a default route when nothing else
// is up, which must not count as a network change.
func localOnly(iface string) bool {
	return strings.HasPrefix(iface, "veth") || strings.HasPrefix(iface, "waydroid")
}

// defaultRoute returns "iface gateway" of the IPv4 default route in the main
// table ("" when offline). The tunnel uses its own table, so it does not show up.
func defaultRoute() string {
	f, err := os.Open("/proc/net/route")
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	best, bestMetric := "", int(^uint(0)>>1)
	for sc.Scan() {
		fl := strings.Fields(sc.Text())
		if len(fl) < 8 || fl[1] != "00000000" || fl[7] != "00000000" || localOnly(fl[0]) {
			continue
		}
		var metric int
		fmt.Sscan(fl[6], &metric)
		if metric < bestMetric {
			best, bestMetric = fl[0]+" "+fl[2], metric
		}
	}
	return best
}
