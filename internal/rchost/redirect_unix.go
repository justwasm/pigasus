//go:build !windows

package rchost

import (
	"os"

	"golang.org/x/sys/unix"
)

func redirectToLogFile(logFile string) error {
	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if err := unix.Dup2(int(f.Fd()), int(os.Stdout.Fd())); err != nil {
		_ = f.Close()
		return err
	}
	if err := unix.Dup2(int(f.Fd()), int(os.Stderr.Fd())); err != nil {
		_ = f.Close()
		return err
	}
	_ = f.Close()
	return nil
}
