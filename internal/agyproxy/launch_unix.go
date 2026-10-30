//go:build !windows && !js

package agyproxy

import (
	"os"
	"os/exec"
	"os/signal"
	"syscall"
)

func notifyProxySignals(sigs chan<- os.Signal) {
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
}

func exitStatus(exitErr *exec.ExitError) int {
	if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return 128 + int(ws.Signal())
	}
	return 1
}
