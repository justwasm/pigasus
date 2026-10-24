// Command pigasus is the openagy recovered 1:1 parse-parity shell of Google
// Antigravity CLI v1.3.4 (the v4 binary).
package main

import (
	"os"

	"github.com/justwasm/pigasus/internal/cliapp"
)

func main() {
	os.Exit(cliapp.Run(os.Args))
}
