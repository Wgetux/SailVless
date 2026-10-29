// Package core supervises a proxy core process (xray / sing-box).
package core

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
)

type State string

const (
	Stopped    State = "stopped"
	Connecting State = "connecting"
	Connected  State = "connected"
	Failed     State = "error"
)

// Spec describes how to run a core.
type Spec struct {
	Name      string   // "xray"
	Bin       string   // path to binary
	TestArgs  []string // config check, e.g. run -test -c file; nil = skip
	RunArgs   []string
	Env       []string
	ReadyAddr string // TCP address that accepts connections once the core is up
	// Attr overrides process attributes (credentials, ambient capabilities);
	// it must keep Pdeathsig so the child dies with its supervisor.
	Attr *syscall.SysProcAttr
}

type Runner struct {
	Logs     *LogBuf
	OnChange func(State, string)

	mu      sync.Mutex
	state   State
	errMsg  string
	since   time.Time
	cancel  context.CancelFunc
	stopped chan struct{}
}

func NewRunner(logs *LogBuf) *Runner { return &Runner{Logs: logs, state: Stopped} }

func (r *Runner) Status() (State, string, time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.state, r.errMsg, r.since
}

func (r *Runner) set(s State, msg string) {
	r.mu.Lock()
	changed := r.state != s || r.errMsg != msg
	r.state, r.errMsg = s, msg
	if s == Connected {
		r.since = time.Now()
	} else if s != Connecting {
		r.since = time.Time{}
	}
	cb := r.OnChange
	r.mu.Unlock()
	if changed && cb != nil {
		cb(s, msg)
	}
}

// Start validates the config and launches the core, replacing a running one.
// The core is restarted with backoff if it dies until Stop is called.
func (r *Runner) Start(spec Spec) error {
	r.Stop()
	r.set(Connecting, "")
	if spec.TestArgs != nil {
		cmd := exec.Command(spec.Bin, spec.TestArgs...)
		cmd.Env = spec.Env
		cmd.SysProcAttr = spec.Attr // same user as the real run
		out, err := cmd.CombinedOutput()
		if err != nil {
			msg := lastLines(string(out), 5)
			for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
				r.Logs.Add(spec.Name+"(test): ", l)
			}
			r.set(Failed, "config check failed: "+msg)
			return fmt.Errorf("config check failed: %s", msg)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	r.mu.Lock()
	r.cancel, r.stopped = cancel, stopped
	r.mu.Unlock()
	go r.supervise(ctx, spec, stopped)
	return nil
}

func (r *Runner) supervise(ctx context.Context, spec Spec, stopped chan struct{}) {
	defer close(stopped)
	// keep the spawning thread alive for the child's lifetime (Pdeathsig)
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	backoff := time.Second
	for {
		startedAt := time.Now()
		err := r.runOnce(ctx, spec)
		if ctx.Err() != nil {
			r.set(Stopped, "")
			return
		}
		msg := fmt.Sprintf("%s exited: %v", spec.Name, err)
		r.Logs.Add("sailvlessd: ", msg+fmt.Sprintf("; restarting in %s", backoff))
		r.set(Failed, msg)
		if time.Since(startedAt) > time.Minute {
			backoff = time.Second
		}
		select {
		case <-ctx.Done():
			r.set(Stopped, "")
			return
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
		r.set(Connecting, "")
	}
}

func (r *Runner) runOnce(ctx context.Context, spec Spec) error {
	cmd := exec.Command(spec.Bin, spec.RunArgs...)
	cmd.Env = spec.Env
	cmd.SysProcAttr = procAttr()
	if spec.Attr != nil {
		cmd.SysProcAttr = spec.Attr
	}
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		return err
	}
	go r.Logs.consume(spec.Name+": ", stdout)
	go r.Logs.consume(spec.Name+": ", stderr)

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	ready := time.NewTicker(150 * time.Millisecond)
	defer ready.Stop()
	deadline := time.Now().Add(10 * time.Second)
	isReady := spec.ReadyAddr == ""
	if isReady {
		r.set(Connected, "")
	}
	for {
		select {
		case err := <-done:
			return err
		case <-ctx.Done():
			if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
				r.Logs.Add("sailvlessd: ", fmt.Sprintf("%s: SIGTERM failed: %v", spec.Name, err))
			}
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				if err := cmd.Process.Kill(); err != nil {
					r.Logs.Add("sailvlessd: ", fmt.Sprintf("%s: SIGKILL failed: %v", spec.Name, err))
				}
				// never block the caller forever on a process we cannot reap
				select {
				case <-done:
				case <-time.After(5 * time.Second):
					r.Logs.Add("sailvlessd: ", fmt.Sprintf("%s (pid %d) did not exit", spec.Name, cmd.Process.Pid))
				}
			}
			return ctx.Err()
		case <-ready.C:
			if isReady {
				continue
			}
			if c, err := net.DialTimeout("tcp", spec.ReadyAddr, 200*time.Millisecond); err == nil {
				c.Close()
				isReady = true
				r.set(Connected, "")
			} else if time.Now().After(deadline) {
				r.set(Failed, "core did not open "+spec.ReadyAddr)
				deadline = time.Now().Add(time.Hour) // report once, keep waiting
			}
		}
	}
}

// Stop terminates the core and waits for it to exit.
func (r *Runner) Stop() {
	r.mu.Lock()
	cancel, stopped := r.cancel, r.stopped
	r.cancel, r.stopped = nil, nil
	r.mu.Unlock()
	if cancel != nil {
		cancel()
		<-stopped
	}
	r.set(Stopped, "")
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, " | ")
}
