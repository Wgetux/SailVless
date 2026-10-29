package core

import "syscall"

// procAttr makes the core die with the daemon even on SIGKILL, so no orphan
// keeps the ports. Pdeathsig fires when the spawning OS thread exits, hence
// runOnce is called from a goroutine that locks its thread (see supervise).
func procAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL, Setpgid: true}
}
