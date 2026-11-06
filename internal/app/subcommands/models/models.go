package models

import (
	"github.com/justwasm/pigasus/internal/app/subcommands"
	"github.com/justwasm/pigasus/internal/core/cliflags"
)

// Specs returns the models and model subcommand specs.
func Specs() []cliflags.SubcommandSpec {
	return []cliflags.SubcommandSpec{cliflags.ModelsSubcommand(), cliflags.ModelSubcommand()}
}

func init() {
	subcommands.RegisterMultiProvider(Specs)
}
