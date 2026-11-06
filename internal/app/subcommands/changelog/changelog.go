package changelog

import (
	"github.com/justwasm/pigasus/internal/app/subcommands"
	"github.com/justwasm/pigasus/internal/core/cliflags"
)

// Spec returns the changelog subcommand spec.
func Spec() cliflags.SubcommandSpec {
	return cliflags.ChangelogSubcommand()
}

func init() {
	subcommands.RegisterProvider(Spec)
}
