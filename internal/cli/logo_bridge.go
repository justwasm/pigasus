package cli

import (
	"fmt"
	"io"
)

// logoHandler is the cmd/wagyu-supplied entry point for the
// hidden `logo` subcommand. Set via RegisterLogoHandler at process
// start; nil in tests that only exercise the parser surface.
var logoHandler func(w io.Writer, args []string) error

// RegisterLogoHandler installs the cmd/wagyu-supplied logo printer.
// Called from cmd/wagyu/main.go at process init.
func RegisterLogoHandler(fn func(w io.Writer, args []string) error) {
	logoHandler = fn
}

// runLogoHandler is the Run bound to the hidden `logo` spec in parser_specs.go.
// When unbridged (e.g. standalone cli tests), it prints fallback text.
func runLogoHandler(w io.Writer, args []string) error {
	if logoHandler == nil {
		fmt.Fprintln(w, "logo: not bridged (build with cmd/wagyu to enable)")
		return nil
	}
	return logoHandler(w, args)
}
