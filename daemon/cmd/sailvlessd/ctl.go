package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"strconv"

	"github.com/godbus/dbus/v5"

	"sailvless/internal/dbusapi"
	"sailvless/internal/service"
)

const ctlUsage = `usage: sailvlessd ctl <command>
  import [link|-]        import share link(s); "-" or no arg reads stdin
  list                   list profiles
  connect [id]           connect (default: selected profile)
  disconnect
  status
  delete <id>
  settings [json-patch]  show or change settings, e.g. '{"httpPort":0}'
  regen-creds            new random login/password for local proxy
  random-ports           pick random free local ports
  proxy-url [socks|http] print local proxy URL with credentials
  logs [n]`

func ctl(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, ctlUsage)
		return 2
	}
	conn, err := dbusapi.SessionBus()
	if err != nil {
		fmt.Fprintln(os.Stderr, "session bus:", err)
		return 1
	}
	defer conn.Close()
	obj := conn.Object(dbusapi.BusName, dbusapi.Path)
	call := func(method string, out []any, in ...any) error {
		c := obj.Call(dbusapi.Interface+"."+method, 0, in...)
		if c.Err != nil {
			return c.Err
		}
		if len(out) > 0 {
			return c.Store(out...)
		}
		return nil
	}
	arg := func(i int) string {
		if len(args) > i {
			return args[i]
		}
		return ""
	}

	var s string
	var err2 error
	switch args[0] {
	case "import":
		text := arg(1)
		if text == "" || text == "-" {
			b, err := io.ReadAll(os.Stdin)
			if err != nil {
				return fail(err)
			}
			text = string(b)
		}
		err2 = call("ImportLinks", []any{&s}, text)
		printJSON(s)
	case "list":
		if err2 = call("ListProfiles", []any{&s}); err2 == nil {
			var ps []service.ProfileView
			_ = json.Unmarshal([]byte(s), &ps)
			for _, p := range ps {
				mark := " "
				if p.Selected {
					mark = "*"
				}
				fmt.Printf("%s %s  %-8s %-6s %-8s %s\n", mark, p.ID, p.Protocol, p.Transport.Type, p.Security.Type, p.Name)
			}
		}
	case "connect":
		err2 = call("Connect", nil, arg(1))
	case "disconnect":
		err2 = call("Disconnect", nil)
	case "status":
		if err2 = call("Status", []any{&s}); err2 == nil {
			printJSON(s)
			var st service.Status
			_ = json.Unmarshal([]byte(s), &st)
			if st.Since != 0 {
				fmt.Println("uptime:", service.Uptime(st.Since))
			}
		}
	case "delete":
		err2 = call("DeleteProfile", nil, arg(1))
	case "settings":
		if arg(1) != "" {
			err2 = call("SetSettings", nil, arg(1))
		}
		if err2 == nil {
			err2 = call("GetSettings", []any{&s})
			printJSON(s)
		}
	case "regen-creds":
		err2 = call("RegenerateCredentials", nil)
	case "random-ports":
		err2 = call("RandomizePorts", nil)
	case "proxy-url":
		err2 = proxyURL(call, arg(1))
	case "logs":
		n, _ := strconv.Atoi(arg(1))
		if n == 0 {
			n = 100
		}
		if err2 = call("Logs", []any{&s}, int32(n)); err2 == nil {
			fmt.Println(s)
		}
	default:
		fmt.Fprintln(os.Stderr, ctlUsage)
		return 2
	}
	if err2 != nil {
		return fail(err2)
	}
	return 0
}

func proxyURL(call func(string, []any, ...any) error, kind string) error {
	var s, user, pass string
	if err := call("Status", []any{&s}); err != nil {
		return err
	}
	var st service.Status
	if err := json.Unmarshal([]byte(s), &st); err != nil {
		return err
	}
	scheme, port := "socks5h", st.SocksPort
	if kind == "http" {
		scheme, port = "http", st.HTTPPort
	}
	u := url.URL{Scheme: scheme, Host: net.JoinHostPort(st.Listen, strconv.Itoa(port))}
	if st.Auth {
		if err := call("GetProxyCredentials", []any{&user, &pass}); err != nil {
			return err
		}
		u.User = url.UserPassword(user, pass)
	}
	fmt.Println(u.String())
	return nil
}

func printJSON(s string) {
	var b bytes.Buffer
	if json.Indent(&b, []byte(s), "", "  ") != nil {
		fmt.Println(s)
		return
	}
	fmt.Println(b.String())
}

func fail(err error) int {
	if e, ok := err.(dbus.Error); ok && len(e.Body) > 0 {
		fmt.Fprintln(os.Stderr, "error:", e.Body[0])
	} else {
		fmt.Fprintln(os.Stderr, "error:", err)
	}
	return 1
}
