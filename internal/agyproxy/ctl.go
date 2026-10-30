// The proxy's control surface: the launcher that runs the vendor agy behind
// the local CloudCode proxy, and the admin commands that inspect it.
//
// This file is the "control" half of the package; server.go and its
// neighbours are the proxy itself.
//
// Cycle wrapper-merge moved this code out of cmd/agy and into the clone;
// cycle wrapper-flatten dissolved the wrapper as a separate product — the
// launcher is now the clone's `pigasus agy`, and the admin commands are the
// clone's `pigasus proxy …`. There is no second binary and no second
// namespace left.
package agyproxy

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Version is the proxy's own version, independent of agy's.
const Version = "0.1.0"

// AdminOptions carries the streams and the program name the admin commands
// print with. The clone passes its parser's streams, so `pigasus proxy
// doctor` writes where the rest of the clone writes.
type AdminOptions struct {
	// Prog is the program name used in usage and error text ("pigasus").
	Prog string
	// Out receives the command output.
	Out io.Writer
	// Err receives the error messages.
	Err io.Writer
}

// DefaultOptions returns the options a standalone use of the admin commands
// would use: the process streams and the program's own name.
func DefaultOptions() AdminOptions {
	return AdminOptions{
		Prog: filepath.Base(os.Args[0]),
		Out:  os.Stdout,
		Err:  os.Stderr,
	}
}

// Admin dispatches the admin subcommands. Any error is printed once and
// turned into exit status 1; nothing here touches the network for long.
func Admin(opts AdminOptions, args []string) int {
	if len(args) == 0 {
		fmt.Fprint(opts.Out, usageText(opts.Prog))
		return 0
	}
	switch args[0] {
	case "help", "-h", "--help":
		fmt.Fprint(opts.Out, usageText(opts.Prog))
		return 0
	case "version", "-v", "--version":
		return wrapErr(opts, cmdVersion(opts, args[1:]))
	case "capture":
		return wrapErr(opts, cmdCapture(opts, args[1:]))
	case "balance":
		return wrapErr(opts, cmdBalance(opts, args[1:]))
	case "aggregate":
		return wrapErr(opts, cmdAggregate(opts, args[1:]))
	case "log":
		return wrapErr(opts, cmdLog(opts, args[1:]))
	case "doctor":
		return wrapErr(opts, cmdDoctor(opts, args[1:]))
	default:
		return wrapErr(opts, fmt.Errorf("unknown command %q (try: %s proxy help)", args[0], opts.Prog))
	}
}

// wrapErr prints err once and maps it to an exit status. A flag.ErrHelp is
// the flag package's own "-h" path: the usage text is already printed, so
// it is a success, not a failure.
func wrapErr(opts AdminOptions, err error) int {
	if err == nil {
		return 0
	}
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	fmt.Fprintf(opts.Err, "%s proxy: %v\n", opts.Prog, err)
	return 1
}

// usageText is the help text. It is one string so `proxy help` and a bare
// `proxy` print the same thing.
func usageText(prog string) string {
	return `usage: ` + prog + ` proxy <command> [args]

  capture on|off             toggle request/response capture
  capture dir [path]         show or set the capture directory
  balance on|off             toggle multi-account load balancing
  aggregate on|off           toggle unified virtual account aggregation
  log [-n N]                 tail the proxy log (default 40 lines)
  doctor                     agy path/version, registry, port, capture, upstream
  version                    proxy and agy versions
  help                       this text

The proxy gives the official agy access to the configured providers:
  ` + prog + ` agy -p "hello"         run agy with the proxy attached
  ` + prog + ` agy models             the vendor model listing

The provider/model registry is the clone's: ` + prog + ` provider and
` + prog + ` model edit the same file.

environment:
  AGY_REAL_BIN               path to the real agy binary
  OPENAGY_WRAPPER_ADDR       listen address (default 127.0.0.1:0)
  OPENAGY_CAPTURE_DIR        capture directory
  OPENAGY_UPSTREAM           upstream base URL
  OPENAGY_WRAPPER_CONFIG     proxy config file
  OPENAGY_WRAPPER_LOG        proxy log sink: a file path (default
                             ~/.openagy/wrapper.log), "stderr", or "off".
                             stderr smears agy's TUI — debugging only.
`
}
