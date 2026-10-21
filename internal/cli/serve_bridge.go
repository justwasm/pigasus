// serve_bridge.go: cmd/wagyu → internal/cli callback for the
// `remote-control serve` subcommand.
//
// Cycle rc-serve-subcommand. The same bridge pattern
// `foreground_bridge.go` uses for the hidden
// `remote-control-foreground` subcommand: the parser-level
// RunJ lives in internal/cli/ (so the spec is registered in
// parser_specs.go alongside the rest of the remote-control
// siblings), but the handler body — which has to reach into
// buildHostFSProvider / buildHostListBackend / buildHostMessageBackend
// and the rest of the cmd/wagyu wiring — lives in cmd/wagyu/.
//
// Why a fresh file rather than extending foreground_bridge.go:
// the foreground subcommand is hidden (launchd invokes it
// internally), so it never sees the parser's argv flow; serve
// is a user-facing subcommand that needs flag validation
// (`--addr`, `--name`, `--no-login`) which the parser does
// before the bridge runs. Putting the bridge for the two
// subcommands in the same file would muddle the two surfaces'
// contracts.
//
// Set via RegisterServeHandler at process init, alongside
// the chat + foreground bridges in cmd/wagyu/main.go. Tests
// that exercise the parser surface without the main package
// leave it nil; runRCServe handles that case gracefully.
package cli

import "fmt"

// ServeArgs is the typed argv slice the parser-supplied
// runRCServe hands to the cmd/wagyu-supplied serveHandler.
// We pass a struct (rather than the raw argv tail) so the
// handler body in cmd/wagyu/ doesn't have to re-parse the
// flags — and so adding a flag in runRCServe surfaces here
// automatically without a coordinated edit.
type ServeArgs struct {
	// Addr is the resolved --addr value. Empty means
	// "use the foreground default (0.0.0.0:8787)"; the
	// handler body enforces that empty → default and
	// validates the result with persistentrc.ValidateAddr.
	Addr string

	// Name is the resolved --name value. Always non-empty
	// after runRCServe's hostname fallback.
	Name string

	// NoLogin suppresses the inline OAuth device-code
	// login when no bearer token is on disk. Tests use
	// this; production leaves it false so the brief's
	// "trigger OAuth login inline" semantic holds.
	NoLogin bool
}

// serveHandler is the cmd/wagyu-supplied entry point for
// the user-facing `remote-control serve` subcommand. Set via
// RegisterServeHandler at process start; nil in tests that
// only exercise the parser surface.
//
// The handler receives the parser's JetskiCLI (so it can write
// results to j.Out and usage/errors to j.ErrOut — the split
// the v1.3.5 binary uses) and a typed ServeArgs with the
// already-validated flag values. Returning a non-nil error
// surfaces it through the parser's exit-code path; the
// canonical "Application Default Credentials" gate lives in
// the handler and returns *ExitCodeError with code 3 on the
// ADC path.
var serveHandler func(j *JetskiCLI, args ServeArgs) error

// RegisterServeHandler installs the cmd/wagyu-supplied
// `remote-control serve` handler. Called exactly once from
// cmd/wagyu/main.go at process init, alongside the chat and
// foreground bridges. Tests that exercise the parser surface
// without the main package leave it nil; runRCServe
// handles that case gracefully.
func RegisterServeHandler(fn func(j *JetskiCLI, args ServeArgs) error) {
	serveHandler = fn
}

// EnsureServeHandlerRegistered is a small test seam: it
// reports whether the cmd/wagyu bridge has been wired into
// the parser. The cli-internal test rig uses it to gate
// integration assertions (e.g. "did main.go actually call
// RegisterServeHandler?") without exposing the package-level
// var directly.
func EnsureServeHandlerRegistered() bool {
	return serveHandler != nil
}

// errServeHandlerNotRegistered is the in-package error
// sentinel a future direct-caller path can return when the
// bridge has not been wired. Today runRCServe itself emits
// the "no handler registered" notice to stderr and exits
// nil; this sentinel is parked for a future cycle that
// wants to surface the gap as an actual exit-3 (e.g. so the
// parity verifier can spot a binary built without the
// bridge).
var errServeHandlerNotRegistered = fmt.Errorf("serve handler not registered")
