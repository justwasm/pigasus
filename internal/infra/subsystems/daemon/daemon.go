package daemon

import (
	"context"

	"github.com/justwasm/pigasus/internal/core/registry"
)

func init() {
	registry.RegisterSubsystem(&Subsystem{})
}

// Subsystem implements modular.Subsystem for background OS daemons.
type Subsystem struct{}

func (s *Subsystem) Name() string {
	return "daemon"
}

func (s *Subsystem) Enabled() bool {
	return isSupported()
}

func (s *Subsystem) Init(ctx context.Context) error {
	return nil
}

func (s *Subsystem) Shutdown(ctx context.Context) error {
	return nil
}
