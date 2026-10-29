// Package socks5 is a small SOCKS5 server (RFC 1928/1929): username/password
// auth, CONNECT and UDP ASSOCIATE. Outgoing connections go through a Backend,
// e.g. the AmneziaWG userspace network stack.
package socks5

import (
	"context"
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"time"
)

type Backend interface {
	DialContext(ctx context.Context, network, address string) (net.Conn, error)
	// ListenPacket returns an unconnected UDP socket on the backend side.
	ListenPacket() (net.PacketConn, error)
	LookupHost(ctx context.Context, host string) ([]string, error)
}

type Server struct {
	User, Pass string // required when User != ""
	Backend    Backend
	Logf       func(format string, args ...any)
}

const (
	cmdConnect   = 1
	cmdAssociate = 3
	atypIPv4     = 1
	atypDomain   = 3
	atypIPv6     = 4
)

func (s *Server) logf(format string, args ...any) {
	if s.Logf != nil {
		s.Logf(format, args...)
	}
}

// Serve accepts connections until ln is closed.
func (s *Server) Serve(ln net.Listener) error {
	for {
		c, err := ln.Accept()
		if err != nil {
			return err
		}
		go func() {
			defer c.Close()
			if err := s.handle(c); err != nil && !errors.Is(err, io.EOF) {
				s.logf("socks: %v", err)
			}
		}()
	}
}

func (s *Server) handle(c net.Conn) error {
	_ = c.SetDeadline(time.Now().Add(30 * time.Second))
	hdr := make([]byte, 2)
	if _, err := io.ReadFull(c, hdr); err != nil {
		return err
	}
	if hdr[0] != 5 {
		return fmt.Errorf("not SOCKS5")
	}
	methods := make([]byte, hdr[1])
	if _, err := io.ReadFull(c, methods); err != nil {
		return err
	}
	want := byte(0) // no auth
	if s.User != "" {
		want = 2
	}
	if !contains(methods, want) {
		c.Write([]byte{5, 0xff})
		return fmt.Errorf("client does not offer the required auth method")
	}
	c.Write([]byte{5, want})
	if want == 2 {
		if err := s.auth(c); err != nil {
			return err
		}
	}

	req := make([]byte, 3)
	if _, err := io.ReadFull(c, req); err != nil {
		return err
	}
	host, port, err := readAddr(c)
	if err != nil {
		return err
	}
	_ = c.SetDeadline(time.Time{})
	switch req[1] {
	case cmdConnect:
		return s.connect(c, host, port)
	case cmdAssociate:
		return s.associate(c)
	}
	reply(c, 7, nil) // command not supported
	return fmt.Errorf("unsupported command %d", req[1])
}

func contains(b []byte, v byte) bool {
	for _, x := range b {
		if x == v {
			return true
		}
	}
	return false
}

func (s *Server) auth(c net.Conn) error {
	h := make([]byte, 2)
	if _, err := io.ReadFull(c, h); err != nil {
		return err
	}
	user := make([]byte, h[1])
	if _, err := io.ReadFull(c, user); err != nil {
		return err
	}
	pl := make([]byte, 1)
	if _, err := io.ReadFull(c, pl); err != nil {
		return err
	}
	pass := make([]byte, pl[0])
	if _, err := io.ReadFull(c, pass); err != nil {
		return err
	}
	ok := subtle.ConstantTimeCompare(user, []byte(s.User)) == 1 && subtle.ConstantTimeCompare(pass, []byte(s.Pass)) == 1
	if !ok {
		c.Write([]byte{1, 1})
		return fmt.Errorf("bad credentials")
	}
	_, err := c.Write([]byte{1, 0})
	return err
}

// readAddr reads ATYP + address + port.
func readAddr(r io.Reader) (string, int, error) {
	t := make([]byte, 1)
	if _, err := io.ReadFull(r, t); err != nil {
		return "", 0, err
	}
	var host string
	switch t[0] {
	case atypIPv4, atypIPv6:
		n := 4
		if t[0] == atypIPv6 {
			n = 16
		}
		b := make([]byte, n)
		if _, err := io.ReadFull(r, b); err != nil {
			return "", 0, err
		}
		host = net.IP(b).String()
	case atypDomain:
		l := make([]byte, 1)
		if _, err := io.ReadFull(r, l); err != nil {
			return "", 0, err
		}
		b := make([]byte, l[0])
		if _, err := io.ReadFull(r, b); err != nil {
			return "", 0, err
		}
		host = string(b)
	default:
		return "", 0, fmt.Errorf("bad address type %d", t[0])
	}
	p := make([]byte, 2)
	if _, err := io.ReadFull(r, p); err != nil {
		return "", 0, err
	}
	return host, int(binary.BigEndian.Uint16(p)), nil
}

func encodeAddr(addr net.Addr) []byte {
	var ip net.IP
	port := 0
	switch a := addr.(type) {
	case *net.TCPAddr:
		ip, port = a.IP, a.Port
	case *net.UDPAddr:
		ip, port = a.IP, a.Port
	}
	var b []byte
	if ip4 := ip.To4(); ip4 != nil {
		b = append([]byte{atypIPv4}, ip4...)
	} else if ip != nil {
		b = append([]byte{atypIPv6}, ip.To16()...)
	} else {
		b = []byte{atypIPv4, 0, 0, 0, 0}
	}
	return binary.BigEndian.AppendUint16(b, uint16(port))
}

func reply(c net.Conn, code byte, bound net.Addr) {
	c.Write(append([]byte{5, code, 0}, encodeAddr(bound)...))
}

func (s *Server) connect(c net.Conn, host string, port int) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	up, err := s.Backend.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		reply(c, 5, nil) // connection refused
		return fmt.Errorf("connect %s:%d: %w", host, port, err)
	}
	defer up.Close()
	reply(c, 0, up.LocalAddr())
	done := make(chan struct{}, 2)
	go func() { io.Copy(up, c); closeWrite(up); done <- struct{}{} }()
	go func() { io.Copy(c, up); closeWrite(c); done <- struct{}{} }()
	<-done
	<-done
	return nil
}

func closeWrite(c net.Conn) {
	if cw, ok := c.(interface{ CloseWrite() error }); ok {
		cw.CloseWrite()
	} else {
		c.Close()
	}
}

// associate relays UDP datagrams while the control connection stays open.
func (s *Server) associate(c net.Conn) error {
	laddr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)}
	if ta, ok := c.LocalAddr().(*net.TCPAddr); ok {
		laddr.IP = ta.IP
	}
	local, err := net.ListenUDP("udp", laddr)
	if err != nil {
		reply(c, 1, nil)
		return err
	}
	defer local.Close()
	remote, err := s.Backend.ListenPacket()
	if err != nil {
		reply(c, 1, nil)
		return err
	}
	defer remote.Close()
	reply(c, 0, local.LocalAddr())

	var mu sync.Mutex
	var client *net.UDPAddr
	// back: backend -> client, with the SOCKS UDP header
	go func() {
		buf := make([]byte, 65535)
		for {
			n, from, err := remote.ReadFrom(buf)
			if err != nil {
				return
			}
			mu.Lock()
			cl := client
			mu.Unlock()
			if cl == nil {
				continue
			}
			pkt := append(append([]byte{0, 0, 0}, encodeAddr(from)...), buf[:n]...)
			local.WriteToUDP(pkt, cl)
		}
	}()
	// forth: client -> backend
	go func() {
		buf := make([]byte, 65535)
		for {
			n, from, err := local.ReadFromUDP(buf)
			if err != nil {
				return
			}
			if n < 4 || buf[2] != 0 { // fragments are not supported
				continue
			}
			mu.Lock()
			if client == nil {
				client = from
			}
			ok := client.String() == from.String()
			mu.Unlock()
			if !ok {
				continue
			}
			host, port, hlen, err := parseUDPAddr(buf[3:n])
			if err != nil {
				continue
			}
			dst, err := s.resolveUDP(host, port)
			if err != nil {
				s.logf("socks udp: %v", err)
				continue
			}
			remote.WriteTo(buf[3+hlen:n], dst)
		}
	}()
	// the association ends with the TCP control connection
	io.Copy(io.Discard, c)
	return nil
}

func parseUDPAddr(b []byte) (string, int, int, error) {
	if len(b) < 1 {
		return "", 0, 0, errors.New("short")
	}
	switch b[0] {
	case atypIPv4:
		if len(b) < 7 {
			return "", 0, 0, errors.New("short")
		}
		return net.IP(b[1:5]).String(), int(binary.BigEndian.Uint16(b[5:7])), 7, nil
	case atypIPv6:
		if len(b) < 19 {
			return "", 0, 0, errors.New("short")
		}
		return net.IP(b[1:17]).String(), int(binary.BigEndian.Uint16(b[17:19])), 19, nil
	case atypDomain:
		if len(b) < 2 || len(b) < 2+int(b[1])+2 {
			return "", 0, 0, errors.New("short")
		}
		l := int(b[1])
		return string(b[2 : 2+l]), int(binary.BigEndian.Uint16(b[2+l : 4+l])), 4 + l, nil
	}
	return "", 0, 0, errors.New("bad atyp")
}

func (s *Server) resolveUDP(host string, port int) (*net.UDPAddr, error) {
	if ip := net.ParseIP(host); ip != nil {
		return &net.UDPAddr{IP: ip, Port: port}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	addrs, err := s.Backend.LookupHost(ctx, host)
	if err != nil || len(addrs) == 0 {
		return nil, fmt.Errorf("resolve %s: %v", host, err)
	}
	return &net.UDPAddr{IP: net.ParseIP(addrs[0]), Port: port}, nil
}
