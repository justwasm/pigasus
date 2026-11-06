package agent

import (
	"github.com/justwasm/pigasus/internal/app/subcommands"
	"github.com/justwasm/pigasus/internal/core/cliflags"
)

// Specs returns the agent subcommands specs.
func Specs() []cliflags.SubcommandSpec {
	return cliflags.AgentSubcommands()
}

func init() {
	subcommands.RegisterMultiProvider(Specs)
}
