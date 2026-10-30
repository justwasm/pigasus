package localharness

import (
	"context"
	"runtime"

	"github.com/justwasm/pigasus/internal/core/registry"
)

func init() {
	registry.RegisterSubsystem(&Subsystem{})
}

// Subsystem implements modular.Subsystem for SDK agent harness connections.
type Subsystem struct{}

func (s *Subsystem) Name() string {
	return "localharness"
}

func (s *Subsystem) Enabled() bool {
	return runtime.GOOS != "js"
}

func (s *Subsystem) Init(ctx context.Context) error {
	return nil
}

func (s *Subsystem) Shutdown(ctx context.Context) error {
	return nil
}
