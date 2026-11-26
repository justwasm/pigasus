//go:build windows

package render

import "golang.org/x/sys/windows"

func probeWindowsConsoleVT() bool {
	var mode uint32
	if err := windows.GetConsoleMode(windows.Stdout, &mode); err != nil {
		return false
	}
	if mode&windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING != 0 {
		return true
	}
	if err := windows.SetConsoleMode(windows.Stdout, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING); err == nil {
		_ = windows.SetConsoleMode(windows.Stdout, mode)
		return true
	}
	return false
}
