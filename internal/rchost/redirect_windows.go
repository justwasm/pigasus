//go:build windows

package rchost

import (
	"os"

	"golang.org/x/sys/windows"
)

func redirectToLogFile(logFile string) error {
	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	handle := windows.Handle(f.Fd())
	_ = windows.SetStdHandle(windows.STD_OUTPUT_HANDLE, handle)
	_ = windows.SetStdHandle(windows.STD_ERROR_HANDLE, handle)
	os.Stdout = f
	os.Stderr = f
	return nil
}
