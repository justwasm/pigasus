package provider

import (
	"github.com/justwasm/pigasus/internal/app/subcommands"
	"github.com/justwasm/pigasus/internal/core/cliflags"
)

// Spec returns the provider subcommand spec.
func Spec() cliflags.SubcommandSpec {
	return cliflags.ProviderSubcommand()
}

func init() {
	subcommands.RegisterProvider(Spec)
}
