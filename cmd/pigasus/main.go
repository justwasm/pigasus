// Command pigasus is the openagy recovered 1:1 parse-parity shell of Google
// Antigravity CLI v1.3.4 (the v4 binary).
package main

import (
	"os"

	"github.com/justwasm/pigasus/browsercdp"
	_ "github.com/justwasm/pigasus/dns"
	_ "github.com/justwasm/pigasus/internal/app/subcommands/accounts"
	_ "github.com/justwasm/pigasus/internal/app/subcommands/agent"
	_ "github.com/justwasm/pigasus/internal/app/subcommands/changelog"
	_ "github.com/justwasm/pigasus/internal/app/subcommands/cmdinstall"
	_ "github.com/justwasm/pigasus/internal/app/subcommands/halloween"
	_ "github.com/justwasm/pigasus/internal/app/subcommands/mic"
	_ "github.com/justwasm/pigasus/internal/app/subcommands/models"
	_ "github.com/justwasm/pigasus/internal/app/subcommands/provider"
	_ "github.com/justwasm/pigasus/internal/app/subcommands/remotecontrol"
	_ "github.com/justwasm/pigasus/internal/app/subcommands/update"
	"github.com/justwasm/pigasus/internal/clirunner"
	_ "github.com/justwasm/pigasus/internal/tui"
)

func main() {
	code := clirunner.Run(os.Args)
	// The clone starts the browser itself — the reference's browser
	// surface starts its own — so the browser has to stop with the
	// clone: a headless Chrome left behind holds its profile and its
	// port.
	browsercdp.Shutdown()
	os.Exit(code)
}
