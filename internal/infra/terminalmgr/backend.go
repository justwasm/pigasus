package terminalmgr

import (
	"context"
	"os"
	"strings"
)

const (
	// EnvSocket is the Unix domain socket path exported by `pigasus terminal`
	// to all child PTY sessions so pigasus can inspect and control host tabs.
	EnvSocket = "PIGASUS_TERMINAL_SOCKET"

	// EnvSessionID is the terminal session ID exported to each child PTY tab
	// so a process (such as pigasus TUI) can identify and capture its own tab.
	EnvSessionID = "PIGASUS_TERMINAL_ID"
)

// Backend abstracts local in-process terminal management and remote IPC
// connection to a host `pigasus terminal` multiplexer.
type Backend interface {
	List() []SessionInfo
	New(ctx context.Context, opts NewOptions) (CaptureResult, error)
	Capture(ctx context.Context, opts CaptureOptions) (CaptureResult, error)
	Send(ctx context.Context, opts SendOptions) (CaptureResult, error)
	Resize(idOrName string, rows, cols int) (SessionInfo, error)
	Kill(idOrName string) (SessionInfo, error)
	Switch(idOrName string) (SessionInfo, error)
	Rename(idOrName, title string) (SessionInfo, error)
}

var _ Backend = (*Manager)(nil)

// SelfSessionIDFromEnv returns the current tab's terminal ID when running
// inside `pigasus terminal` (or rcterminal).
func SelfSessionIDFromEnv() string {
	if id := strings.TrimSpace(os.Getenv(EnvSessionID)); id != "" {
		return id
	}
	return strings.TrimSpace(os.Getenv("PIGASUS_TERMINAL_SESSION_ID"))
}
