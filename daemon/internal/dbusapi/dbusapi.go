// Package dbusapi exposes the service on the session bus. Structured values are
// passed as JSON strings, which QML parses natively.
package dbusapi

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"

	"sailvless/internal/service"
)

const (
	BusName   = "org.sailvless.Daemon"
	Path      = dbus.ObjectPath("/org/sailvless/Daemon")
	Interface = "org.sailvless.Daemon1"
)

// SessionBus connects to the session bus, falling back to Sailfish's socket
// location when DBUS_SESSION_BUS_ADDRESS is not set (e.g. some SSH sessions).
func SessionBus() (*dbus.Conn, error) {
	if os.Getenv("DBUS_SESSION_BUS_ADDRESS") == "" {
		if rt := os.Getenv("XDG_RUNTIME_DIR"); rt != "" {
			os.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path="+rt+"/dbus/user_bus_socket")
		}
	}
	return dbus.ConnectSessionBus()
}

type api struct {
	svc *service.Service
}

func dbusErr(err error) *dbus.Error {
	if err == nil {
		return nil
	}
	return dbus.NewError(Interface+".Error", []any{err.Error()})
}

func toJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func (a *api) ImportLinks(text string) (string, *dbus.Error) {
	ids, errs, err := a.svc.Import(text)
	if err != nil {
		return "", dbusErr(err)
	}
	return toJSON(map[string]any{"imported": ids, "errors": errs}), nil
}

func (a *api) ListProfiles() (string, *dbus.Error) { return toJSON(a.svc.Profiles()), nil }

func (a *api) DeleteProfile(id string) *dbus.Error { return dbusErr(a.svc.DeleteProfile(id)) }

func (a *api) Connect(id string) *dbus.Error { return dbusErr(a.svc.Connect(id)) }

func (a *api) Disconnect() *dbus.Error { a.svc.Disconnect(); return nil }

func (a *api) Status() (string, *dbus.Error) { return toJSON(a.svc.Status()), nil }

func (a *api) GetSettings() (string, *dbus.Error) { return toJSON(a.svc.Store.Settings()), nil }

// SetSettings merges the given JSON object into the current settings.
func (a *api) SetSettings(patch string) *dbus.Error {
	set := a.svc.Store.Settings()
	if err := json.Unmarshal([]byte(patch), &set); err != nil {
		return dbusErr(fmt.Errorf("settings: %w", err))
	}
	return dbusErr(a.svc.SetSettings(set))
}

func (a *api) GetProxyCredentials() (string, string, *dbus.Error) {
	u, p := a.svc.Store.ProxyCredentials()
	return u, p, nil
}

func (a *api) RegenerateCredentials() *dbus.Error { return dbusErr(a.svc.RegenerateCredentials()) }

func (a *api) RandomizePorts() *dbus.Error {
	socks, http, err := service.RandomPorts()
	if err != nil {
		return dbusErr(err)
	}
	set := a.svc.Store.Settings()
	set.SocksPort = socks
	if set.HTTPPort != 0 {
		set.HTTPPort = http
	}
	return dbusErr(a.svc.SetSettings(set))
}

func (a *api) Logs(n int32) (string, *dbus.Error) {
	return strings.Join(a.svc.Logs.Tail(int(n)), "\n"), nil
}

const introXML = `<interface name="` + Interface + `">
  <method name="ImportLinks"><arg direction="in" type="s" name="text"/><arg direction="out" type="s" name="resultJson"/></method>
  <method name="ListProfiles"><arg direction="out" type="s" name="profilesJson"/></method>
  <method name="DeleteProfile"><arg direction="in" type="s" name="id"/></method>
  <method name="Connect"><arg direction="in" type="s" name="id"/></method>
  <method name="Disconnect"/>
  <method name="Status"><arg direction="out" type="s" name="statusJson"/></method>
  <method name="GetSettings"><arg direction="out" type="s" name="settingsJson"/></method>
  <method name="SetSettings"><arg direction="in" type="s" name="patchJson"/></method>
  <method name="GetProxyCredentials"><arg direction="out" type="s" name="user"/><arg direction="out" type="s" name="password"/></method>
  <method name="RegenerateCredentials"/>
  <method name="RandomizePorts"/>
  <method name="Logs"><arg direction="in" type="i" name="lines"/><arg direction="out" type="s" name="text"/></method>
  <signal name="StateChanged"><arg type="s" name="statusJson"/></signal>
</interface>`

// Serve exports the API and claims the bus name. Status changes are emitted as signals.
func Serve(conn *dbus.Conn, svc *service.Service) error {
	if err := conn.Export(&api{svc: svc}, Path, Interface); err != nil {
		return err
	}
	node := `<node>` + introXML + introspect.IntrospectDataString + `</node>`
	if err := conn.Export(introspect.Introspectable(node), Path, "org.freedesktop.DBus.Introspectable"); err != nil {
		return err
	}
	svc.OnState = func(st service.Status) {
		_ = conn.Emit(Path, Interface+".StateChanged", toJSON(st))
	}
	reply, err := conn.RequestName(BusName, dbus.NameFlagDoNotQueue)
	if err != nil {
		return err
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		return fmt.Errorf("%s is already owned (another sailvlessd running?)", BusName)
	}
	return nil
}
