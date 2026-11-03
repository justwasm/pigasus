package halloween

import (
	"github.com/justwasm/pigasus/internal/app/subcommands"
	"github.com/justwasm/pigasus/internal/cli"
)

func init() {
	subcommands.RegisterSubcommands(cli.HalloweenSubcommand())
}
