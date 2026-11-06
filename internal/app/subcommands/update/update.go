package update

import (
	"github.com/justwasm/pigasus/internal/app/subcommands"
	"github.com/justwasm/pigasus/internal/core/cliflags"
)

// Spec returns the update subcommand spec.
func Spec() cliflags.SubcommandSpec {
	return cliflags.UpdateSubcommand()
}

func init() {
	subcommands.RegisterProvider(Spec)
}
