package cli

import (
	"fmt"
)

// usageRCRun documents the `remote-control run` subcommand — the
// clone's explicit "run the daemon here, in this terminal" command.
//
// It is the same operation on **every** platform: the dispatcher, the
// TCP and WebSocket server, and the cloud tunnel, in the foreground,
// blocking until the user interrupts it. `remote-control start` is the
// platform-dependent one — it registers a launchd agent on macOS, and
// falls back to this on every other platform.
//
// The subcommand is **hidden**: the reference's `remote-control --help`
// lists start, status and stop only, and that block is the
// reference's, byte for byte. `run` is accepted, not advertised — the
// same way `serve` is.
//
// It is the clone's own name: the reference calls the same operation
// `serve`. Both exist, one implementation behind them.
func usageRCRun(n string) string {
	return "Usage: " + n + " remote-control run [flags]\n" +
		"\n" +
		"Run the remote-control daemon in the foreground (does not register with launchd)\n" +
		"\n" +
		"Flags:\n" +
		"  -h         Show help\n" +
		"  --help     Show help\n" +
		"  --addr     Bind address for the foreground daemon (host:port, default 0.0.0.0:8787)\n" +
		"  --name     Set this machine's instance name shown in Remote Control (default: hostname)\n" +
		"  --no-login Skip the inline OAuth device-code login when no bearer token is on disk\n" +
		"  --desktop  Serve the desktop UI: the voice input, the jetbox\n" +
		"             terminal, the redesigned aux pane. The page and the\n" +
		"             bundle are the same either way — the switch is the\n" +
		"             identity the page's Mendel read carries\n"
}

// runRCRun is the implementation of `pigasus remote-control run`.
//
// The behaviour is `remote-control serve`'s, on every platform: the
// daemon comes up in this terminal and the command blocks until it
// comes down. The difference is the name — "run" says *here*, and it
// does not depend on the platform having a service manager.
//
// The flags are serve's: --addr, --name, --no-login and --desktop.
func runRCRun(j *JetskiCLI, args []string) error {
	usage := usageRCRun(j.Name)
	opts, err := parseRCServeFlags(j, args, usage)
	if err != nil {
		return err
	}
	if opts.showHelp {
		fmt.Fprint(j.ErrOut, usage)
		return nil
	}
	name, err := resolveRCForegroundName(opts.name)
	if err != nil {
		return err
	}
	err = runRCForegroundDaemon(j, rcForegroundArgs{
		Addr:    opts.addr,
		Name:    name,
		NoLogin: opts.noLogin,
		Desktop: opts.desktop,
		Label:   "remote-control run",
	})
	if err == errNoServeHandler {
		return ExitCodeError(1, "Error: remote-control run: no handler registered (binary was built without the serve bridge)")
	}
	return err
}
