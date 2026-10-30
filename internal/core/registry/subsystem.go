package registry

import (
	"context"
)

// Subsystem represents an optional capability (e.g. TUI, RemoteControl, Daemon).
type Subsystem interface {
	Name() string
	Enabled() bool
	Init(ctx context.Context) error
	Shutdown(ctx context.Context) error
}
