//go:build windows || js

package daemon

import (
	"os"
	"os/signal"
	"syscall"
)

func notifySignals(ch chan<- os.Signal) {
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
}

func isHup(sig os.Signal) bool {
	return false
}
