package socks5

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"
)

type plain struct{}

func (plain) DialContext(ctx context.Context, n, a string) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, n, a)
}
func (plain) ListenPacket() (net.PacketConn, error) { return net.ListenPacket("udp", "127.0.0.1:0") }
func (plain) LookupHost(ctx context.Context, h string) ([]string, error) {
	return net.DefaultResolver.LookupHost(ctx, h)
}

func start(t *testing.T) string {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	s := &Server{User: "u", Pass: "p", Backend: plain{}}
	go s.Serve(ln)
	return ln.Addr().String()
}

// handshake authenticates and sends a request; returns the reply's bound address.
func handshake(t *testing.T, c net.Conn, user, pass string, cmd byte, host string, port int) (byte, *net.UDPAddr) {
	c.Write([]byte{5, 1, 2})
	r := make([]byte, 2)
	io.ReadFull(c, r)
	if r[1] != 2 {
		t.Fatalf("method %v", r)
	}
	c.Write(append(append(append([]byte{1, byte(len(user))}, user...), byte(len(pass))), pass...))
	io.ReadFull(c, r)
	if r[1] != 0 {
		return 0xff, nil
	}
	req := []byte{5, cmd, 0, atypIPv4}
	req = append(req, net.ParseIP(host).To4()...)
	req = binary.BigEndian.AppendUint16(req, uint16(port))
	c.Write(req)
	rep := make([]byte, 10)
	io.ReadFull(c, rep)
	return rep[1], &net.UDPAddr{IP: net.IP(rep[4:8]), Port: int(binary.BigEndian.Uint16(rep[8:10]))}
}

func TestConnectAndAuth(t *testing.T) {
	echo, _ := net.Listen("tcp", "127.0.0.1:0")
	defer echo.Close()
	go func() {
		c, err := echo.Accept()
		if err == nil {
			io.Copy(c, c)
			c.Close()
		}
	}()
	addr := start(t)

	c, _ := net.Dial("tcp", addr)
	defer c.Close()
	ep := echo.Addr().(*net.TCPAddr)
	if code, _ := handshake(t, c, "u", "p", cmdConnect, "127.0.0.1", ep.Port); code != 0 {
		t.Fatalf("connect reply %d", code)
	}
	c.Write([]byte("ping"))
	buf := make([]byte, 4)
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := io.ReadFull(c, buf); err != nil || string(buf) != "ping" {
		t.Fatalf("echo: %q %v", buf, err)
	}

	bad, _ := net.Dial("tcp", addr)
	defer bad.Close()
	if code, _ := handshake(t, bad, "u", "wrong", cmdConnect, "127.0.0.1", ep.Port); code != 0xff {
		t.Fatal("wrong password accepted")
	}
}

func TestUDPAssociate(t *testing.T) {
	echo, _ := net.ListenPacket("udp", "127.0.0.1:0")
	defer echo.Close()
	go func() {
		buf := make([]byte, 1500)
		for {
			n, from, err := echo.ReadFrom(buf)
			if err != nil {
				return
			}
			echo.WriteTo(buf[:n], from)
		}
	}()
	addr := start(t)
	c, _ := net.Dial("tcp", addr)
	defer c.Close()
	code, relay := handshake(t, c, "u", "p", cmdAssociate, "0.0.0.0", 0)
	if code != 0 {
		t.Fatalf("associate reply %d", code)
	}
	u, _ := net.DialUDP("udp", nil, relay)
	defer u.Close()
	ep := echo.LocalAddr().(*net.UDPAddr)
	pkt := []byte{0, 0, 0, atypIPv4, 127, 0, 0, 1}
	pkt = binary.BigEndian.AppendUint16(pkt, uint16(ep.Port))
	pkt = append(pkt, "hello"...)
	u.Write(pkt)
	buf := make([]byte, 1500)
	u.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := u.Read(buf)
	if err != nil || !bytes.HasSuffix(buf[:n], []byte("hello")) || buf[3] != atypIPv4 {
		t.Fatalf("udp: %q %v", buf[:n], err)
	}
}
