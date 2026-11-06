package remotecontrol

import (
	"github.com/justwasm/pigasus/internal/app/subcommands"
	"github.com/justwasm/pigasus/internal/core/cliflags"
)

// Specs returns the remote-control subcommand specs.
func Specs() []cliflags.SubcommandSpec {
	return cliflags.RCSubcommands()
}

func init() {
	subcommands.RegisterMultiProvider(Specs)
}
