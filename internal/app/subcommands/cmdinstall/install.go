package install

import (
	"github.com/justwasm/pigasus/internal/app/subcommands"
	"github.com/justwasm/pigasus/internal/core/cliflags"
)

// Spec returns the install subcommand spec.
func Spec() cliflags.SubcommandSpec {
	return cliflags.InstallSubcommand()
}

func init() {
	subcommands.RegisterProvider(Spec)
}
