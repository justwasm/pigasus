package subcommands_test

import (
	"testing"

	"github.com/justwasm/pigasus/internal/core/registry"
	_ "github.com/justwasm/pigasus/internal/ui/cli/subcommands/accounts"
	_ "github.com/justwasm/pigasus/internal/ui/cli/subcommands/agent"
	_ "github.com/justwasm/pigasus/internal/ui/cli/subcommands/changelog"
	_ "github.com/justwasm/pigasus/internal/ui/cli/subcommands/cmdinstall"
	_ "github.com/justwasm/pigasus/internal/ui/cli/subcommands/halloween"
	_ "github.com/justwasm/pigasus/internal/ui/cli/subcommands/mic"
	_ "github.com/justwasm/pigasus/internal/ui/cli/subcommands/models"
	_ "github.com/justwasm/pigasus/internal/ui/cli/subcommands/provider"
	_ "github.com/justwasm/pigasus/internal/ui/cli/subcommands/remotecontrol"
	_ "github.com/justwasm/pigasus/internal/ui/cli/subcommands/update"
)

func TestSubcommandsRegistered(t *testing.T) {
	cmds := registry.GetSubcommands()
	if len(cmds) == 0 {
		t.Fatal("expected subcommands to be registered")
	}
	names := make(map[string]bool)
	for _, c := range cmds {
		names[c.Name] = true
	}
	expected := []string{"accounts", "models", "provider"}
	for _, exp := range expected {
		if !names[exp] {
			t.Errorf("expected subcommand %q to be registered", exp)
		}
	}
}
