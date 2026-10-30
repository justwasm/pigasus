//go:build !windows && !js

package terminal

func isSupported() bool {
	return true
}
