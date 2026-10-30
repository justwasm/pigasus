//go:build !windows && !js

package daemon

import (
	"os"
	"os/signal"
	"syscall"
)

func notifySignals(ch chan<- os.Signal) {
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
}

func isHup(sig os.Signal) bool {
	return sig == syscall.SIGHUP
}
