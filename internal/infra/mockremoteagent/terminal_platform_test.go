package mockremoteagent

import "runtime"

// openPTYUnsupported reports whether this build has no PTY support.
func openPTYUnsupported() bool {
	return runtime.GOOS == "windows" || runtime.GOOS == "js"
}
