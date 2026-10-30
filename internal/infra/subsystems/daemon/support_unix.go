//go:build !windows && !js

package daemon

func isSupported() bool {
	return true
}
