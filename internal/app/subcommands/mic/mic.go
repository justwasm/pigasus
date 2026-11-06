package mic

import (
	"github.com/justwasm/pigasus/internal/app/subcommands"
	"github.com/justwasm/pigasus/internal/core/cliflags"
)

// Spec returns the mic-serve subcommand spec.
func Spec() cliflags.SubcommandSpec {
	return cliflags.MicServeSubcommand()
}

func init() {
	subcommands.RegisterProvider(Spec)
}
