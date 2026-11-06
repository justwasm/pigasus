package accounts

import (
	"github.com/justwasm/pigasus/internal/app/subcommands"
	"github.com/justwasm/pigasus/internal/core/cliflags"
)

// Spec returns the accounts subcommand spec.
func Spec() cliflags.SubcommandSpec {
	return cliflags.AccountsSubcommand()
}

func init() {
	subcommands.RegisterProvider(Spec)
}
