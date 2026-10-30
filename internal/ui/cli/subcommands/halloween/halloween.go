package halloween

import (
	"github.com/justwasm/pigasus/internal/cli"
	"github.com/justwasm/pigasus/internal/core/registry"
)

func init() {
	registry.RegisterSubcommands(cli.HalloweenSubcommand())
}
