package subcommands_test

import (
	"testing"

	"github.com/justwasm/pigasus/internal/app/subcommands"
	_ "github.com/justwasm/pigasus/internal/app/subcommands/accounts"
	_ "github.com/justwasm/pigasus/internal/app/subcommands/agent"
	_ "github.com/justwasm/pigasus/internal/app/subcommands/changelog"
	_ "github.com/justwasm/pigasus/internal/app/subcommands/cmdinstall"
	_ "github.com/justwasm/pigasus/internal/app/subcommands/halloween"
	_ "github.com/justwasm/pigasus/internal/app/subcommands/mic"
	_ "github.com/justwasm/pigasus/internal/app/subcommands/models"
	_ "github.com/justwasm/pigasus/internal/app/subcommands/provider"
	_ "github.com/justwasm/pigasus/internal/app/subcommands/remotecontrol"
	_ "github.com/justwasm/pigasus/internal/app/subcommands/update"
)

func TestSubcommandsRegistered(t *testing.T) {
	cmds := subcommands.GetSubcommands()
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
