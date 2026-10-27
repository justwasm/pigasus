// carddump writes the /config → Color Scheme preview card for every
// scheme into a directory, one file per scheme, so the comparison
// harness can diff them against the reference's own card.
//
// The reference's card is captured in a true-colour pty (see the cycle
// report); this program writes the clone's side of that comparison.
//
// Usage:
//
//	carddump <out-dir>
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/justwasm/pigasus/internal/onboarding"
	"github.com/justwasm/pigasus/internal/themepreview"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: carddump <out-dir>")
		os.Exit(2)
	}
	out := os.Args[1]
	if err := os.MkdirAll(out, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for _, scheme := range onboarding.AllSchemes() {
		name := strings.ReplaceAll(scheme.String(), " ", "_")
		body := strings.Join(themepreview.Render(scheme), "\n") + "\n"
		path := filepath.Join(out, name+".txt")
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println(path)
	}
}
