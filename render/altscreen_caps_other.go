//go:build !windows

package render

func probeWindowsConsoleVT() bool {
	return false
}
