// Command pigasus is the openagy recovered 1:1 parse-parity shell of Google
// Antigravity CLI v1.3.4 (the v4 binary).
package main

import (
	"os"

	"github.com/justwasm/pigasus/browsercdp"
	"github.com/justwasm/pigasus/internal/cliapp"
	_ "github.com/justwasm/pigasus/internal/dns"
)

func main() {
	code := cliapp.Run(os.Args)
	// The clone starts the browser itself — the reference's browser
	// surface starts its own — so the browser has to stop with the
	// clone: a headless Chrome left behind holds its profile and its
	// port.
	browsercdp.Shutdown()
	os.Exit(code)
}
