package subcommands_test

import (
	"testing"

	"github.com/justwasm/pigasus/internal/core/registry"
	_ "github.com/justwasm/pigasus/internal/modular/subcommands/accounts"
	_ "github.com/justwasm/pigasus/internal/modular/subcommands/agent"
	_ "github.com/justwasm/pigasus/internal/modular/subcommands/changelog"
	_ "github.com/justwasm/pigasus/internal/modular/subcommands/cmdinstall"
	_ "github.com/justwasm/pigasus/internal/modular/subcommands/halloween"
	_ "github.com/justwasm/pigasus/internal/modular/subcommands/mic"
	_ "github.com/justwasm/pigasus/internal/modular/subcommands/models"
	_ "github.com/justwasm/pigasus/internal/modular/subcommands/provider"
	_ "github.com/justwasm/pigasus/internal/modular/subcommands/remotecontrol"
	_ "github.com/justwasm/pigasus/internal/modular/subcommands/update"
)

func TestModularSubcommandsRegistered(t *testing.T) {
	cmds := registry.GetSubcommands()
	if len(cmds) == 0 {
		t.Fatal("expected subcommands to be registered via modular facade")
	}
	names := make(map[string]bool)
	for _, c := range cmds {
		names[c.Name] = true
	}
	expected := []string{"accounts", "models", "provider"}
	for _, exp := range expected {
		if !names[exp] {
			t.Errorf("expected subcommand %q to be registered via modular facade", exp)
		}
	}
}
