//go:build windows || js

package agyproxy

import (
	"os"
	"os/exec"
	"os/signal"
	"syscall"
)

func notifyProxySignals(sigs chan<- os.Signal) {
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
}

func exitStatus(exitErr *exec.ExitError) int {
	return 1
}
