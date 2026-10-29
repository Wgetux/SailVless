//go:build !linux

package core

import "syscall"

func procAttr() *syscall.SysProcAttr { return nil }
