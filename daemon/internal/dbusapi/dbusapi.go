// Package dbusapi exposes the service on the session bus. Structured values are
// passed as JSON strings, which QML parses natively.
package dbusapi

import (
	"encoding/json"
	"fmt"
	"os"

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

func (a *api) Status() (string, *dbus.Error) { return toJSON(a.svc.StatusWithTraffic()), nil }

func (a *api) SelectProfile(id string) *dbus.Error { return dbusErr(a.svc.SelectProfile(id)) }

// PingAll returns {"id": ms} (-1 failed, -2 not applicable for UDP protocols).
func (a *api) PingAll() (string, *dbus.Error) { return toJSON(a.svc.PingAll()), nil }

func (a *api) ConnectBest() (string, *dbus.Error) {
	id, err := a.svc.ConnectBest()
	return id, dbusErr(err)
}

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

func (a *api) AddSubscription(url, name string) (string, *dbus.Error) {
	id, err := a.svc.AddSubscription(url, name)
	if id == "" {
		return "", dbusErr(err)
	}
	// the subscription is saved even if the first fetch failed; report both
	res := map[string]any{"id": id}
	if err != nil {
		res["error"] = err.Error()
	}
	return toJSON(res), nil
}

func (a *api) UpdateSubscription(id string) *dbus.Error { return dbusErr(a.svc.UpdateSubscription(id)) }

func (a *api) ListSubscriptions() (string, *dbus.Error) { return toJSON(a.svc.Subscriptions()), nil }

func (a *api) DeleteSubscription(id string) *dbus.Error {
	return dbusErr(a.svc.Store.DeleteSubscription(id))
}

// SetSubscription merges {"name","autoUpdate","viaProxy","intervalHours"} into a subscription.
func (a *api) SetSubscription(id, patch string) *dbus.Error {
	sb, _, ok := a.svc.Store.Subscription(id)
	if !ok {
		return dbusErr(fmt.Errorf("no such subscription"))
	}
	var p struct {
		Name          *string `json:"name"`
		AutoUpdate    *bool   `json:"autoUpdate"`
		ViaProxy      *bool   `json:"viaProxy"`
		IntervalHours *int    `json:"intervalHours"`
	}
	if err := json.Unmarshal([]byte(patch), &p); err != nil {
		return dbusErr(err)
	}
	if p.ViaProxy != nil {
		sb.ViaProxy = *p.ViaProxy
	}
	if p.Name != nil {
		sb.Name = *p.Name
	}
	if p.AutoUpdate != nil {
		sb.AutoUpdate = *p.AutoUpdate
	}
	if p.IntervalHours != nil {
		if *p.IntervalHours < 0 || *p.IntervalHours > 24*30 {
			return dbusErr(fmt.Errorf("interval must be 0-720 hours"))
		}
		sb.IntervalHours = *p.IntervalHours
	}
	return dbusErr(a.svc.Store.SaveSubscription(sb, ""))
}

func (a *api) UpdateGeo() *dbus.Error { return dbusErr(a.svc.UpdateGeo()) }

func (a *api) GetGeoInfo() (string, *dbus.Error) { return toJSON(a.svc.GeoInfo()), nil }

// Notifier sends notifications through the freedesktop interface that
// lipstick implements; the nemo hints make Sailfish show a banner too.
func Notifier(conn *dbus.Conn) func(summary, body string) {
	return func(summary, body string) {
		hints := map[string]dbus.Variant{
			"x-nemo-preview-summary": dbus.MakeVariant(summary),
			"x-nemo-preview-body":    dbus.MakeVariant(body),
		}
		obj := conn.Object("org.freedesktop.Notifications", "/org/freedesktop/Notifications")
		obj.Go("org.freedesktop.Notifications.Notify", 0, nil,
			"SailVless", uint32(0), "/usr/share/icons/hicolor/86x86/apps/sailvless.png",
			summary, body, []string{}, hints, int32(-1))
	}
}

func (a *api) GetHWID() (string, *dbus.Error) { return toJSON(a.svc.HWID()), nil }

func (a *api) ResetHWID() *dbus.Error { return dbusErr(a.svc.ResetHWID()) }

func (a *api) Logs(n int32) (string, *dbus.Error) {
	return a.svc.LogsText(int(n)), nil
}

const introXML = `<interface name="` + Interface + `">
  <method name="ImportLinks"><arg direction="in" type="s" name="text"/><arg direction="out" type="s" name="resultJson"/></method>
  <method name="ListProfiles"><arg direction="out" type="s" name="profilesJson"/></method>
  <method name="DeleteProfile"><arg direction="in" type="s" name="id"/></method>
  <method name="Connect"><arg direction="in" type="s" name="id"/></method>
  <method name="Disconnect"/>
  <method name="Status"><arg direction="out" type="s" name="statusJson"/></method>
  <method name="SelectProfile"><arg direction="in" type="s" name="id"/></method>
  <method name="PingAll"><arg direction="out" type="s" name="pingsJson"/></method>
  <method name="ConnectBest"><arg direction="out" type="s" name="id"/></method>
  <method name="GetSettings"><arg direction="out" type="s" name="settingsJson"/></method>
  <method name="SetSettings"><arg direction="in" type="s" name="patchJson"/></method>
  <method name="GetProxyCredentials"><arg direction="out" type="s" name="user"/><arg direction="out" type="s" name="password"/></method>
  <method name="RegenerateCredentials"/>
  <method name="RandomizePorts"/>
  <method name="AddSubscription"><arg direction="in" type="s" name="url"/><arg direction="in" type="s" name="name"/><arg direction="out" type="s" name="resultJson"/></method>
  <method name="UpdateSubscription"><arg direction="in" type="s" name="id"/></method>
  <method name="ListSubscriptions"><arg direction="out" type="s" name="subscriptionsJson"/></method>
  <method name="DeleteSubscription"><arg direction="in" type="s" name="id"/></method>
  <method name="SetSubscription"><arg direction="in" type="s" name="id"/><arg direction="in" type="s" name="patchJson"/></method>
  <method name="UpdateGeo"/>
  <method name="GetGeoInfo"><arg direction="out" type="s" name="geoJson"/></method>
  <method name="GetHWID"><arg direction="out" type="s" name="hwidJson"/></method>
  <method name="ResetHWID"/>
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
