package subsystems_test

import (
	"testing"

	"github.com/justwasm/pigasus/internal/core/registry"
	_ "github.com/justwasm/pigasus/internal/infra/subsystems/daemon"
	_ "github.com/justwasm/pigasus/internal/infra/subsystems/localharness"
	_ "github.com/justwasm/pigasus/internal/infra/subsystems/remotecontrol"
	_ "github.com/justwasm/pigasus/internal/infra/subsystems/terminal"
)

func TestSubsystemsRegistered(t *testing.T) {
	subs := registry.GetSubsystems()
	names := make(map[string]bool)
	for _, s := range subs {
		names[s.Name()] = true
	}
	expected := []string{"daemon", "localharness", "remote-control", "terminal"}
	for _, exp := range expected {
		if !names[exp] {
			t.Errorf("expected subsystem %q to be registered", exp)
		}
	}
}
