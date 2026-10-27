package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/justwasm/pigasus/internal/persistentrc"
)

// usageRCServe documents the `remote-control serve` subcommand. The
// flag list mirrors usageRCStart — the cycle rc-serve-subcommand
// shorthand is "serve is the foreground twin of start", and the
// foreground path needs the same host / name / log-file overrides
// the persistent path does so a dashboard operator doesn't have to
// learn a new vocabulary.
func usageRCServe(n string) string {
	return "Usage: " + n + " remote-control serve [flags]\n" +
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

type rcServeOpts struct {
	addr     string
	name     string
	noLogin  bool
	desktop  bool
	showHelp bool
}

func parseRCServeFlags(j *JetskiCLI, args []string, usage string) (rcServeOpts, error) {
	var opts rcServeOpts
	for i := 0; i < len(args); i++ {
		a := args[i]
		val, hasVal := "", false
		if eq := strings.IndexByte(a, '='); eq >= 0 {
			val, a, hasVal = a[eq+1:], a[:eq], true
		}
		consume := func(target *string, flag string) error {
			if hasVal {
				*target = val
			} else if i+1 < len(args) {
				i++
				*target = args[i]
			} else {
				return usageFail(j, usage, "Error: flag needs an argument: "+flag)
			}
			return nil
		}
		switch a {
		case "-h", "--help":
			opts.showHelp = true
		case "--addr":
			if err := consume(&opts.addr, "--addr"); err != nil {
				return opts, err
			}
		case "--name":
			if err := consume(&opts.name, "--name"); err != nil {
				return opts, err
			}
		case "--no-login":
			opts.noLogin = true
		case "--desktop":
			opts.desktop = true
		default:
			return opts, usageFail(j, usage, "Error: flags provided but not defined: "+flagName(a))
		}
	}
	return opts, nil
}

// runRCServe is the cycle rc-serve-subcommand implementation of
// `wagyu remote-control serve` — the foreground twin of start.
//
// Behaviour:
//
//   - validates --addr / --name (the same rules persistentrc.Start
//     applies to the start path: --addr refuses loopback, --name
//     matches the conservative [A-Za-z0-9._-]+ character set);
//   - rejects the call when $AGY_ADC_AUTH is set (the same gate
//     --remote-control / /remote-control apply today — Remote
//     Control doesn't support ADC yet);
//   - delegates to the cmd/wagyu-supplied serveHandler (registered
//     via cli.RegisterServeHandler at process init), passing the
//     resolved flags; the handler body lives in cmd/wagyu/ so it
//     can reach buildHost* helpers and avoid the internal/cli ←
//     cmd/wagyu import cycle;
//   - on a successful serve comes back as `Remote-control daemon
//     stopped.` (matches the wording runRCStop emits, so scripts
//     can share a sentinel grep between the two).
//
// The handler accepts --no-login (a cycle rc-serve-subcommand
// addition) so an automated test rig can drive `serve` without
// triggering the OAuth device-code flow on a fresh machine; the
// production default is "login if no token" so the brief's "when
// OAuth is required and the user isn't logged in, serve triggers
// the OAuth browser flow synchronously" semantic holds.
//
// The handler also accepts --desktop (the twin of `pigasus web
// --desktop`): the daemon's Mendel read then carries the hub
// identity, so the page the daemon serves is the reference's
// desktop UI — the voice input, the jetbox terminal, the aux pane
// redesign. The page and the bundle are the same either way; only
// the identity differs. See internal/auth's package comment for
// the measurement.
func runRCServe(j *JetskiCLI, args []string) error {
	usage := usageRCServe(j.Name)
	opts, err := parseRCServeFlags(j, args, usage)
	if err != nil {
		return err
	}
	if opts.showHelp {
		fmt.Fprint(j.ErrOut, usage)
		return nil
	}
	if opts.name == "" {
		opts.name = persistentrc.ResolveInstanceName("")
		if opts.name == "" {
			hn, herr := os.Hostname()
			if herr != nil || hn == "" {
				return ExitCodeError(1, fmt.Sprintf("Error: --name is required (could not determine hostname: %v)", herr))
			}
			opts.name = hn
		}
	} else {
		opts.name = persistentrc.ResolveInstanceName(opts.name)
	}
	if serveHandler == nil {
		fmt.Fprintln(j.ErrOut, "remote-control serve: no handler registered (binary was built without the serve bridge)")
		return nil
	}
	if err := serveHandler(j, ServeArgs{
		Addr:    opts.addr,
		Name:    opts.name,
		NoLogin: opts.noLogin,
		Desktop: opts.desktop,
	}); err != nil {
		return err
	}
	fmt.Fprintln(j.Out, "Remote-control daemon stopped.")
	return nil
}

// serveADCErrorMessage is the single source of truth for the
// error text the user sees when `remote-control serve` collides
// with ADC. Mirrors remoteControlADCErrorMessage (the
// --remote-control flag-path wording) but with the verb
// substituted for "serve" so the surfaced message is consistent
// with the surface the user actually invoked.
//
//   - "remote-control"          grep-able from CI logs
//   - "Application Default Credentials"   the human-readable name
//   - "AGY_ADC_AUTH"            the env var the user can /logout, then unset
//   - "/logout"                 recovery command
//   - "serve"                   verb the user invoked
func serveADCErrorMessage() string {
	return "error: remote-control serve cannot be used with Application Default Credentials " +
		"(AGY_ADC_AUTH) sign-in; Remote Control does not support ADC yet — " +
		"sign out with /logout and unset AGY_ADC_AUTH (or use a different sign-in method) and retry."
}
