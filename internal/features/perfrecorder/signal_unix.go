//go:build unix

package perfrecorder

import (
	"os"
	"syscall"
)

func defaultDumpSignals() []os.Signal {
	return []os.Signal{syscall.SIGUSR1, syscall.SIGUSR2}
}
