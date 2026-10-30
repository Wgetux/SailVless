// sailvlessd is the SailVless user daemon (systemd --user). `sailvlessd ctl ...`
// is a command-line client for the same D-Bus API.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"sailvless/internal/dbusapi"
	"sailvless/internal/service"
	"sailvless/internal/store"
)

var version = "0.11.0"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "ctl" {
		os.Exit(ctl(os.Args[2:]))
	}
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Println("sailvlessd", version)
		return
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "sailvlessd:", err)
		os.Exit(1)
	}
}

func run() error {
	st, err := store.Open(store.DefaultDir())
	if err != nil {
		return err
	}
	rt := os.Getenv("XDG_RUNTIME_DIR")
	if rt == "" {
		rt = os.TempDir()
	}
	rt = filepath.Join(rt, "sailvless")
	if err := os.MkdirAll(rt, 0o700); err != nil {
		return err
	}
	svc := service.New(st, coreDir(), rt)
	svc.GeoDir = filepath.Join(store.DefaultDir(), "geo")

	conn, err := dbusapi.SessionBus()
	if err != nil {
		return fmt.Errorf("session bus: %w", err)
	}
	defer conn.Close()
	if err := dbusapi.Serve(conn, svc); err != nil {
		return err
	}
	svc.Logs.Add("sailvlessd: ", fmt.Sprintf("%s started, cores in %s", version, svc.CoreDir))
	svc.Notify = dbusapi.Notifier(conn) // before any goroutine may notify

	if set := st.Settings(); set.Connected && set.SelectedID != "" {
		svc.Logs.Add("sailvlessd: ", "restoring the connection")
		go func() {
			if err := svc.Connect(""); err != nil {
				svc.Logs.Add("sailvlessd: ", "restore failed: "+err.Error())
			}
		}()
	}

	ctx, cancel := context.WithCancel(context.Background())
	go svc.WatchSystemVPN()
	go svc.AutoUpdateLoop(ctx)
	go svc.WatchNetwork(ctx)
	go svc.HealthLoop(ctx)
	go svc.LivenessLoop(ctx)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	<-sig
	cancel()
	svc.Shutdown()
	svc.Logs.Add("sailvlessd: ", "stopped")
	return nil
}

// coreDir finds the directory with core binaries: $SAILVLESS_CORE_DIR, the
// daemon's own directory (dev deploy), or the packaged location.
func coreDir() string {
	if d := os.Getenv("SAILVLESS_CORE_DIR"); d != "" {
		return d
	}
	if exe, err := os.Executable(); err == nil {
		d := filepath.Dir(exe)
		if _, err := os.Stat(filepath.Join(d, "xray")); err == nil {
			return d
		}
	}
	return "/usr/libexec/sailvless"
}
