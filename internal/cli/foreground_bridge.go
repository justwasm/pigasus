// foreground_bridge.go: cmd/wagyu → internal/cli callback for the
// hidden `remote-control-foreground` subcommand.
//
// The hidden subcommand lives in `parser_specs.go` because parser
// registration has to happen before NewParser returns.  The actual
// handler — the function that brings up the remotecontrol.Dispatcher
// + remotetransport.TCPServer — lives in cmd/wagyu/ where it has
// access to buildHostFSProvider, buildHostListBackend, etc.  We can't
// have internal/cli/ import cmd/wagyu/ (cyclic), so this file
// provides the package-level hook the same way SetChatBridge does:
//
//   - parser_specs.go's RunJ is `runRCForegroundPlaceholder`,
//     which is defined here.
//   - `runRCForegroundPlaceholder` calls into the package-level
//     `foregroundHandler` var.
//   - cmd/wagyu/main.go sets that var via
//     `cli.RegisterForegroundHandler(runRemoteControlForeground)`
//     at startup, alongside the cli.SetChatBridge call.
//
// When the bridge is unset (the cli-internal test rig never
// invokes main, so it stays unit-test-friendly) we emit a one-line
// notice to stderr and exit 0 — matches the "do nothing" behaviour
// the parent subcommand's "RunJ var was nil at parse time" path
// already has.
package cli

import (
	"fmt"
	"io"
)

// foregroundHandler is the cmd/wagyu-supplied entry point for the
// hidden `remote-control-foreground` subcommand.  Set via
// RegisterForegroundHandler at process start; nil in tests that
// only exercise the parser surface.
//
// Signature mirrors JetskiCLI.RunJ: the handler receives the
// parser's stream pair (so the daemon can log to the same place
// the parser would have) and the argv tail after the subcommand
// name is stripped.
var foregroundHandler func(j *JetskiCLI, args []string) error

// RegisterForegroundHandler installs the cmd/wagyu-supplied
// foreground daemon handler.  Called exactly once from
// cmd/wagyu/main.go at process init (next to
// cli.SetChatBridge).  Tests that exercise the parser surface
// without the main package leave it nil; runRCForegroundPlaceholder
// handles that case gracefully.
func RegisterForegroundHandler(fn func(j *JetskiCLI, args []string) error) {
	foregroundHandler = fn
}

// runRCForegroundPlaceholder is the RunJ bound to the
// `remote-control-foreground` spec in parser_specs.go.  It
// dispatches into the registered foreground handler, falling
// back to a one-line stderr notice when the bridge is unset
// (the cli-internal test rig).
func runRCForegroundPlaceholder(j *JetskiCLI, args []string) error {
	if foregroundHandler == nil {
		// Match the RunJ-nil convention used elsewhere in
		// the parser: the parent subcommand still resolves
		// and exits 0.  We emit a banner so an operator
		// running the bare CLI sees why nothing happened.
		fmt.Fprintln(j.ErrOut, "remote-control-foreground: no handler registered (binary was built without the foreground bridge)")
		return nil
	}
	return foregroundHandler(j, args)
}

// io is referenced so gofmt preserves the import block while
// the only reference is the j.ErrOut path above.  (Removing
// the io import here would break the file once a future refactor
// moves stream writers behind an io.Writer parameter.)
var _ io.Writer = io.Discard
