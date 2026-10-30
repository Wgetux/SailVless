//go:build linux

// sailvless-tun-helper is the root part of the TUN mode (system D-Bus
// org.sailvless.Tun, started on demand by systemd). It:
//   - prepares policy routing so the proxy core's marked traffic bypasses the tunnel;
//   - runs the proxy core as the calling user with only CAP_NET_ADMIN (for SO_MARK);
//   - runs sing-box with a TUN inbound that it configures itself;
//   - tears everything down when asked, when the caller disappears from the bus,
//     or on exit; `--cleanup` does the same after a crash (ExecStopPost).
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"

	"sailvless/internal/core"
	"sailvless/internal/tun"
)

const (
	coreDir     = "/usr/libexec/sailvless"
	capNetAdmin = 12 // CAP_NET_ADMIN
	idleExit    = 5 * time.Minute
)

type helper struct {
	conn *dbus.Conn
	logs *core.LogBuf

	mu       sync.Mutex
	core     *core.Runner
	coreSpec core.Spec
	tunR     *core.Runner
	side     *core.Runner // AmneziaWG sidecar, if any
	owner    string       // unique bus name of the caller that started the tunnel
	lastUsed time.Time

	lastState          string    // last emitted state, to notice the tunnel coming up
	restarting         bool      // Start is replacing the tunnel: "stopped" in between is not news
	exiting            bool      // the helper itself is stopping (e.g. package update)
	sysConnecting      bool      // connmanConnect is running (it retries)
	ownDisconnectUntil time.Time // a disconnect reported until then is the helper's own reset
}

type status struct {
	State string `json:"state"` // stopped, connecting, connected, error
	Error string `json:"error,omitempty"`
	Since int64  `json:"since,omitempty"`
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--cleanup" {
		tun.Cleanup()
		return
	}
	if err := serve(); err != nil {
		fmt.Fprintln(os.Stderr, "sailvless-tun-helper:", err)
		os.Exit(1)
	}
}

func serve() error {
	if err := os.MkdirAll(tun.RuntimeDir, 0o711); err != nil {
		return err
	}
	tun.Cleanup() // leftovers of a crashed run
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return err
	}
	h := &helper{conn: conn, logs: core.NewLogBuf(1000), lastUsed: time.Now()}
	if err := conn.Export(h, tun.Path, tun.Interface); err != nil {
		return err
	}
	node := `<node><interface name="` + tun.Interface + `">
  <method name="Start"><arg direction="in" type="s" name="paramsJson"/></method>
  <method name="Stop"/>
  <method name="RestartCore"/>
  <method name="Status"><arg direction="out" type="s" name="statusJson"/></method>
  <method name="Logs"><arg direction="in" type="i" name="lines"/><arg direction="out" type="s" name="text"/></method>
  <method name="RequestConnect"/>
  <method name="RequestDisconnect"/>
  <signal name="StateChanged"><arg type="s" name="statusJson"/></signal>
  <signal name="Requested"><arg type="s" name="action"/></signal>
</interface>` + introspect.IntrospectDataString + `</node>`
	if err := conn.Export(introspect.Introspectable(node), tun.Path, "org.freedesktop.DBus.Introspectable"); err != nil {
		return err
	}
	reply, err := conn.RequestName(tun.BusName, dbus.NameFlagDoNotQueue)
	if err != nil {
		return err
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		return errors.New("bus name already taken")
	}

	// watchdog: stop the tunnel when the daemon that started it goes away
	_ = conn.AddMatchSignal(dbus.WithMatchInterface("org.freedesktop.DBus"), dbus.WithMatchMember("NameOwnerChanged"))
	signals := make(chan *dbus.Signal, 16)
	conn.Signal(signals)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	idle := time.NewTicker(time.Minute)
	defer idle.Stop()
	h.logs.Add("helper: ", "started")
	for {
		select {
		case s := <-signals:
			if s.Name == "org.freedesktop.DBus.NameOwnerChanged" && len(s.Body) == 3 {
				name, _ := s.Body[0].(string)
				newOwner, _ := s.Body[2].(string)
				h.mu.Lock()
				gone := h.owner != "" && name == h.owner && newOwner == ""
				h.mu.Unlock()
				if gone {
					h.logs.Add("helper: ", "caller left the bus, stopping the tunnel")
					h.stop()
				}
				// connman-vpnd restarted (package update, crash): show the
				// running tunnel as a system VPN again
				if name == vpnBus && newOwner != "" && h.status().State == "connected" {
					go func() {
						// a fresh connman-vpnd needs a few seconds to pair with connmand
						time.Sleep(10 * time.Second)
						if h.status().State == "connected" {
							h.logs.Add("helper: ", "connman-vpnd restarted, showing the tunnel as a system VPN again")
							h.connmanConnect()
						}
					}()
				}
			}
		case <-idle.C:
			h.mu.Lock()
			quit := h.core == nil && time.Since(h.lastUsed) > idleExit
			h.mu.Unlock()
			if quit {
				return nil // D-Bus activation starts us again on demand
			}
		case <-sig:
			h.mu.Lock()
			h.exiting = true
			h.mu.Unlock()
			h.stop()
			return nil
		}
	}
}

func (h *helper) emit() {
	st := h.status()
	h.mu.Lock()
	quiet := h.restarting && st.State == "stopped"
	h.mu.Unlock()
	if quiet {
		return
	}
	b, _ := json.Marshal(st)
	_ = h.conn.Emit(tun.Path, tun.Interface+".StateChanged", string(b))
	h.mu.Lock()
	becameUp := st.State == "connected" && h.lastState != "connected"
	h.lastState = st.State
	h.mu.Unlock()
	if becameUp {
		go h.connmanConnect() // show the tunnel as a system VPN
	}
}

// status combines both processes: connected only when both are up.
func (h *helper) status() status {
	h.mu.Lock()
	c, t, sd := h.core, h.tunR, h.side
	h.mu.Unlock()
	if c == nil || t == nil {
		return status{State: "stopped"}
	}
	if sd != nil {
		if ss, serr, _ := sd.Status(); ss == core.Failed {
			return status{State: "error", Error: "amneziawg: " + serr}
		}
	}
	cs, cerr, since := c.Status()
	ts, terr, _ := t.Status()
	switch {
	case cs == core.Failed:
		return status{State: "error", Error: cerr}
	case ts == core.Failed:
		return status{State: "error", Error: "tun: " + terr}
	case cs == core.Connected && ts == core.Connected:
		return status{State: "connected", Since: since.Unix()}
	}
	return status{State: "connecting"}
}

func dbusErr(err error) *dbus.Error {
	if err == nil {
		return nil
	}
	return dbus.NewError(tun.Interface+".Error", []any{err.Error()})
}

// callerCreds returns uid/gid of the D-Bus caller.
func (h *helper) callerCreds(sender dbus.Sender) (uint32, uint32, string, error) {
	var uid uint32
	if err := h.conn.BusObject().Call("org.freedesktop.DBus.GetConnectionUnixUser", 0, string(sender)).Store(&uid); err != nil {
		return 0, 0, "", err
	}
	if uid == 0 {
		return 0, 0, "", errors.New("refusing to run the core as root")
	}
	home, gid, err := lookupUser(uid)
	return uid, gid, home, err
}

// lookupUser reads /etc/passwd (no cgo).
func lookupUser(uid uint32) (string, uint32, error) {
	b, err := os.ReadFile("/etc/passwd")
	if err != nil {
		return "", 0, err
	}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Split(line, ":")
		if len(f) >= 7 && f[2] == fmt.Sprint(uid) {
			var gid uint32
			fmt.Sscan(f[3], &gid)
			return f[5], gid, nil
		}
	}
	return "", 0, fmt.Errorf("uid %d not found", uid)
}

func (h *helper) Start(paramsJSON string, sender dbus.Sender) *dbus.Error {
	var p tun.Params
	if err := json.Unmarshal([]byte(paramsJSON), &p); err != nil {
		return dbusErr(err)
	}
	if err := p.Validate(); err != nil {
		return dbusErr(err)
	}
	uid, gid, home, err := h.callerCreds(sender)
	if err != nil {
		return dbusErr(err)
	}
	if p.AssetDir != "" && !strings.HasPrefix(filepath.Clean(p.AssetDir)+"/", filepath.Clean(home)+"/") {
		return dbusErr(errors.New("asset dir must be inside the caller's home"))
	}
	// connman takes "stopped" as the VPN going down; a restart is not that
	h.mu.Lock()
	h.restarting = true
	h.lastState = "" // re-check the system VPN once the new tunnel is up
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		h.restarting = false
		h.mu.Unlock()
		h.emit()
	}()
	h.stop()
	h.mu.Lock()
	h.lastUsed = time.Now()
	h.mu.Unlock()

	// the directory stays root's (the unit has no CAP_DAC_OVERRIDE, so root
	// could not manage a user-owned 0700 dir); only the config goes to the caller
	userDir := filepath.Join(tun.RuntimeDir, "core")
	_ = os.RemoveAll(userDir)
	if err := os.Mkdir(userDir, 0o711); err != nil {
		return dbusErr(err)
	}
	corePath := filepath.Join(userDir, p.Core+".json")
	if err := os.WriteFile(corePath, []byte(p.CoreConfig), 0o600); err != nil {
		return dbusErr(err)
	}
	if err := os.Chown(corePath, int(uid), int(gid)); err != nil {
		return dbusErr(err)
	}
	tunCfg, err := tun.SingBoxConfig(p)
	if err != nil {
		return dbusErr(err)
	}
	tunPath := filepath.Join(tun.RuntimeDir, "tun.json")
	if err := os.WriteFile(tunPath, tunCfg, 0o600); err != nil {
		return dbusErr(err)
	}

	if err := tun.SetupNetwork(); err != nil {
		tun.Cleanup()
		return dbusErr(fmt.Errorf("network setup: %w", err))
	}

	// the core runs as the caller; CAP_NET_ADMIN only lets it set SO_MARK
	userAttr := &syscall.SysProcAttr{
		Credential:  &syscall.Credential{Uid: uid, Gid: gid, Groups: []uint32{gid}},
		AmbientCaps: []uintptr{capNetAdmin},
		Pdeathsig:   syscall.SIGKILL,
		Setpgid:     true,
	}
	env := []string{"HOME=" + home, "PATH=/usr/bin:/bin", "XRAY_LOCATION_ASSET=" + p.AssetDir}
	coreSpec := core.Spec{
		Name: p.Core, Bin: filepath.Join(coreDir, p.Core), Env: env, Attr: userAttr,
		ReadyAddr: fmt.Sprintf("127.0.0.1:%d", p.SocksPort),
	}
	if p.Core == "sing-box" {
		coreSpec.TestArgs = []string{"check", "-c", corePath}
		coreSpec.RunArgs = []string{"run", "-c", corePath, "-D", userDir}
	} else {
		coreSpec.TestArgs = []string{"run", "-test", "-c", corePath}
		coreSpec.RunArgs = []string{"run", "-c", corePath}
	}
	tunSpec := core.Spec{
		Name: "tun", Bin: filepath.Join(coreDir, "sing-box"), Env: []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin"},
		TestArgs: []string{"check", "-c", tunPath},
		RunArgs:  []string{"run", "-c", tunPath, "-D", tun.RuntimeDir},
		Attr:     &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL, Setpgid: true},
	}

	coreR, tunR := core.NewRunner(h.logs), core.NewRunner(h.logs)
	coreR.OnChange = func(core.State, string) { h.emit() }
	tunR.OnChange = func(core.State, string) { h.emit() }
	var sideR *core.Runner
	var sideSpec core.Spec
	if p.Sidecar != "" {
		// AmneziaWG sidecar: same user and capability as the core (its UDP needs SO_MARK)
		var st struct {
			Listen string `json:"listen"`
		}
		_ = json.Unmarshal([]byte(p.Sidecar), &st)
		sidePath := filepath.Join(userDir, "awg.json")
		if err := os.WriteFile(sidePath, []byte(p.Sidecar), 0o600); err != nil {
			return dbusErr(err)
		}
		if err := os.Chown(sidePath, int(uid), int(gid)); err != nil {
			return dbusErr(err)
		}
		sideSpec = core.Spec{Name: "awg", Bin: filepath.Join(coreDir, "sailvless-awg"), Env: env, Attr: userAttr,
			RunArgs: []string{"-c", sidePath}, ReadyAddr: st.Listen}
		sideR = core.NewRunner(h.logs)
		sideR.OnChange = func(core.State, string) { h.emit() }
	}
	h.mu.Lock()
	h.core, h.coreSpec, h.tunR, h.side, h.owner = coreR, coreSpec, tunR, sideR, string(sender)
	h.mu.Unlock()

	h.logs.Add("helper: ", fmt.Sprintf("starting %s as uid %d and the TUN front-end", p.Core, uid))
	if sideR != nil {
		if err := sideR.Start(sideSpec); err != nil {
			h.stop()
			return dbusErr(err)
		}
	}
	if err := coreR.Start(coreSpec); err != nil {
		h.stop()
		return dbusErr(err)
	}
	if err := tunR.Start(tunSpec); err != nil {
		h.stop()
		return dbusErr(err)
	}
	return nil
}

func (h *helper) stop() {
	h.mu.Lock()
	c, t, sd := h.core, h.tunR, h.side
	h.core, h.tunR, h.side, h.owner = nil, nil, nil, ""
	h.lastUsed = time.Now()
	h.mu.Unlock()
	if t == nil && c == nil {
		return
	}
	if t != nil {
		t.Stop()
	}
	if c != nil {
		c.Stop()
	}
	if sd != nil {
		sd.Stop()
	}
	tun.Cleanup()
	_ = os.RemoveAll(filepath.Join(tun.RuntimeDir, "core"))
	_ = os.Remove(filepath.Join(tun.RuntimeDir, "tun.json"))
	h.logs.Add("helper: ", "tunnel stopped, routes restored")
	h.emit()
}

// RestartCore restarts only the proxy core after a network change: its
// connections are bound to the old uplink, while the tunnel and the routing
// stay in place, so no traffic leaks around the tunnel meanwhile.
func (h *helper) RestartCore() *dbus.Error {
	h.mu.Lock()
	c, spec := h.core, h.coreSpec
	h.mu.Unlock()
	if c == nil {
		return dbusErr(errors.New("tunnel is not running"))
	}
	h.logs.Add("helper: ", "restarting the core on request (network change or failed liveness check)")
	go h.connmanConnect() // the system VPN may have been refused while there was no network
	return dbusErr(c.Start(spec))
}

func (h *helper) Stop() *dbus.Error {
	h.mu.Lock()
	idle := h.core == nil && h.tunR == nil
	h.mu.Unlock()
	h.stop()
	if idle {
		h.emit() // the connman plugin may be waiting for a start that failed
	}
	return nil
}

func (h *helper) Status() (string, *dbus.Error) {
	b, _ := json.Marshal(h.status())
	return string(b), nil
}

func (h *helper) Logs(n int32) (string, *dbus.Error) {
	return strings.Join(h.logs.Tail(int(n)), "\n"), nil
}
