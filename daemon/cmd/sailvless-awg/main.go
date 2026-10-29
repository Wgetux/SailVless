// sailvless-awg runs an AmneziaWG tunnel in a userspace network stack
// (amneziawg-go + gVisor netstack, no TUN device, no root) and exposes it as
// a local SOCKS5 server. Xray chains to it, so routing, statistics, the local
// proxy and the TUN mode work for AmneziaWG profiles like for any other.
//
// Usage: sailvless-awg -c settings.json   (secrets only in that 0600 file)
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/amnezia-vpn/amneziawg-go/v3/conn"
	"github.com/amnezia-vpn/amneziawg-go/v3/device"
	"github.com/amnezia-vpn/amneziawg-go/v3/tun/netstack"

	"sailvless/internal/awg"
	"sailvless/internal/socks5"
)

// Settings is written by sailvlessd.
type Settings struct {
	Conf     string `json:"conf"`   // AmneziaWG config (wg-quick format)
	Listen   string `json:"listen"` // 127.0.0.1:port
	User     string `json:"user"`
	Pass     string `json:"pass"`
	Mark     int    `json:"mark"` // SO_MARK for the UDP socket (TUN mode)
	LogLevel string `json:"logLevel"`
}

func main() {
	path := flag.String("c", "", "settings file")
	flag.Parse()
	if err := run(*path); err != nil {
		fmt.Fprintln(os.Stderr, "sailvless-awg:", err)
		os.Exit(1)
	}
}

type backend struct{ net *netstack.Net }

func (b backend) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	return b.net.DialContext(ctx, network, addr)
}

func (b backend) ListenPacket() (net.PacketConn, error) {
	return b.net.ListenUDPAddrPort(netip.AddrPortFrom(netip.IPv4Unspecified(), 0))
}

func (b backend) LookupHost(ctx context.Context, host string) ([]string, error) {
	return b.net.LookupContextHost(ctx, host)
}

func run(path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var s Settings
	if err := json.Unmarshal(raw, &s); err != nil {
		return err
	}
	cfg, err := awg.Parse(s.Conf)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	var local []netip.Addr
	for _, p := range cfg.Addresses {
		local = append(local, p.Addr())
	}
	dns := cfg.DNS
	if len(dns) == 0 {
		dns = []netip.Addr{netip.MustParseAddr("1.1.1.1")}
	}
	mtu := cfg.MTU
	if mtu == 0 {
		mtu = 1420
	}
	tdev, tnet, err := netstack.CreateNetTUN(local, dns, mtu)
	if err != nil {
		return err
	}
	level := device.LogLevelError
	if s.LogLevel == "debug" || s.LogLevel == "info" {
		level = device.LogLevelVerbose
	}
	dev := device.NewDevice(tdev, conn.NewDefaultBind(), device.NewLogger(level, "awg: "))
	defer dev.Close()

	// the endpoint is resolved by the system (operator DNS / the TUN's hosts)
	uapi, err := cfg.UAPI(s.Mark, func(host string) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return "", err
		}
		for _, ip := range ips {
			if ip.IP.To4() != nil {
				return ip.IP.String(), nil
			}
		}
		return ips[0].IP.String(), nil
	})
	if err != nil {
		return err
	}
	if err := dev.IpcSet(uapi); err != nil {
		return fmt.Errorf("configure: %w", err)
	}
	if err := dev.Up(); err != nil {
		return err
	}

	ln, err := net.Listen("tcp", s.Listen)
	if err != nil {
		return err
	}
	srv := &socks5.Server{User: s.User, Pass: s.Pass, Backend: backend{tnet},
		Logf: func(f string, a ...any) {
			if level == device.LogLevelVerbose {
				fmt.Printf(f+"\n", a...)
			}
		}}
	go srv.Serve(ln)
	fmt.Printf("AmneziaWG up (%d peer(s), mtu %d), SOCKS5 on %s\n", len(cfg.Peers), mtu, s.Listen)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	<-sig
	ln.Close()
	return nil
}
