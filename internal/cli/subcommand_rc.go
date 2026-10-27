package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/justwasm/pigasus/endpoints"
	"github.com/justwasm/pigasus/internal/accounts"
	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/persistentrc"
)

// ---- usage blocks ---------------------------------------------------------

// usageRemoteControl is the reference's `remote-control` usage block.
// `serve` is deliberately absent: the reference has the subcommand (it
// starts the foreground daemon) but does not list it here — the
// listing shows start, status and stop only.
func usageRemoteControl(n string) string {
	return "Usage: " + n + " remote-control <subcommand>\n" +
		"\n" +
		"Available subcommands:\n" +
		"  start    Register and start the remote-control daemon\n" +
		"  status   Show the daemon's status\n" +
		"  stop     Stop the daemon and unregister it\n" +
		"\n" +
		"Run \"" + n + " remote-control <subcommand> --help\" for flags.\n"
}

// The usage blocks below are the reference's, byte for byte. The
// reference's `start` advertises `--name` and `--session` only; the
// clone also accepts `--addr` and `--log-file` (the foreground
// daemon's overrides, and the persistent daemon's log path), but they
// are not advertised — the reference's `remote-control --help` does not
// list them either, and `agy remote-control start --addr 1.2.3.4:9`
// prints this very usage block.
func usageRCStart(n string) string {
	return "Usage: " + n + " remote-control start [flags]\n" +
		"\n" +
		"Register and start the remote-control daemon\n" +
		"\n" +
		"Flags:\n" +
		"  -h         Show help\n" +
		"  --help     Show help\n" +
		"  --name     Set this machine's instance name shown in Remote Control\n" +
		"  --session  Register the daemon login-scoped (stops at logout) instead of starting at boot\n"
}

func usageRCStatus(n string) string {
	return "Usage: " + n + " remote-control status [flags]\n" +
		"\n" +
		"Show the daemon's status\n" +
		"\n" +
		"Flags:\n" +
		"  -h      Show help\n" +
		"  --help  Show help\n"
}

func usageRCStop(n string) string {
	return "Usage: " + n + " remote-control stop [flags]\n" +
		"\n" +
		"Stop the daemon and unregister it\n" +
		"\n" +
		"Flags:\n" +
		"  -h      Show help\n" +
		"  --help  Show help\n"
}

// ---- remote-control -------------------------------------------------------

// runRCParent handles `remote-control` with no subcommand: like `mcp`, the
// reference prints the parent usage block to stderr and exits 0.
func runRCParent(j *JetskiCLI, args []string) error {
	fmt.Fprint(j.ErrOut, usageRemoteControl(j.Name))
	return nil
}

// runRCStart is the cycle rc-persistent-daemon implementation of
// `wagyu remote-control start`.  It registers a launchd
// LaunchAgent (macOS) or session-detached foreground process
// (--session) that runs the long-lived WebSocket listener.
//
// Flags:
//
//	--addr        bind address (default 0.0.0.0:8787)
//	--log-file    absolute log-file path (default
//	              $TMPDIR/openagy-remote-control-<name>.log)
//	--name        machine instance label (default: hostname)
//	--session     login-scoped instead of boot-time
//	--desktop     serve the desktop UI (the voice input, the
//	              jetbox terminal, the aux pane)
//
// On success, prints
//
//	Daemon started; pid=<pid>; addr=<addr>
//
// to stdout so wrapper scripts can parse the PID without parsing
// the launchd label.  Errors return as cli.ExitCodeError(1, …) so
// the process exits with the right code without an extra banner.
//
// `--desktop` is a clone extension: the reference's `start` usage
// block lists `--name` and `--session` only, and so does ours —
// the flag is accepted, not advertised, the same way `--addr` and
// `--log-file` are. It reaches the daemon through the LaunchAgent
// plist and turns the daemon's Mendel read into the hub's identity.
type rcStartOpts struct {
	addr     string
	name     string
	logFile  string
	session  bool
	desktop  bool
	showHelp bool
}

func parseRCStartFlags(j *JetskiCLI, args []string, usage string) (rcStartOpts, error) {
	var opts rcStartOpts
	for i := 0; i < len(args); i++ {
		a, val, hasVal := args[i], "", false
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
		case "--log-file":
			if err := consume(&opts.logFile, "--log-file"); err != nil {
				return opts, err
			}
		case "--name":
			if err := consume(&opts.name, "--name"); err != nil {
				return opts, err
			}
		case "--session":
			opts.session = true
		case "--desktop":
			opts.desktop = true
		default:
			return opts, usageFail(j, usage, "Error: flags provided but not defined: "+flagName(a))
		}
	}
	return opts, nil
}

func startOfficialDaemon(j *JetskiCLI, ctx context.Context, opts rcStartOpts) error {
	if curTok, curEmail := accounts.Current(); curTok != nil && curEmail != "" {
		_ = auth.SaveTokenUnified(curTok)
	}
	name := persistentrc.ResolveInstanceName(opts.name)
	if err := persistentrc.StartOfficialDaemon(ctx, name, opts.session, opts.desktop); err != nil {
		return ExitCodeError(1, fmt.Sprintf("Error: %v", err))
	}
	fmt.Fprintln(j.Out, "Remote-control daemon started.")
	if name != "" {
		fmt.Fprintf(j.Out, "Open %s and look for %q.\n", endpoints.Current().CompanionOrigin, name)
	} else {
		fmt.Fprintf(j.Out, "Run `%s remote-control status` to get this machine's instance name, then open %s to find it.\n", j.Name, endpoints.Current().CompanionOrigin)
	}
	return nil
}

func startRCDaemon(j *JetskiCLI, opts rcStartOpts) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if runtime.GOOS == "darwin" && opts.addr == "" {
		return startOfficialDaemon(j, ctx, opts)
	}

	binary, err := os.Executable()
	if err != nil || binary == "" {
		binary = os.Args[0]
	}
	spec := persistentrc.Spec{
		Name:       opts.name,
		Addr:       opts.addr,
		LogFile:    opts.logFile,
		BinaryPath: binary,
		Session:    opts.session,
		Desktop:    opts.desktop,
	}
	if err := persistentrc.Start(ctx, spec); err != nil {
		return ExitCodeError(1, fmt.Sprintf("Error: %v", err))
	}

	stCtx, stCancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer stCancel()
	report, sErr := persistentrc.Status(stCtx, opts.name)
	if sErr != nil {
		fmt.Fprintf(j.Out, "Daemon started (status probe failed: %v); addr=%s\n", sErr, spec.Addr)
		return nil
	}
	bindingAddr := report.Addr
	if bindingAddr == "" {
		bindingAddr = spec.Addr
	}
	fmt.Fprintf(j.Out, "Daemon started; pid=%d; addr=%s\n", report.PID, bindingAddr)
	return nil
}

func runRCStart(j *JetskiCLI, args []string) error {
	usage := usageRCStart(j.Name)
	opts, err := parseRCStartFlags(j, args, usage)
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

	if opts.logFile == "" {
		opts.logFile = persistentrc.LogFilePath(opts.name)
		if opts.logFile == "" {
			return ExitCodeError(1, "Error: --log-file is required (no TMPDIR available)")
		}
	}

	return startRCDaemon(j, opts)
}

type rcStatusOpts struct {
	name       string
	jsonOutput bool
	showHelp   bool
}

func parseRCStatusFlags(j *JetskiCLI, args []string, usage string) (rcStatusOpts, error) {
	var opts rcStatusOpts
	i := 0
	for i < len(args) {
		a := args[i]
		val, hasVal := "", false
		if eq := strings.IndexByte(a, '='); eq >= 0 {
			val, a, hasVal = a[eq+1:], a[:eq], true
		}
		switch a {
		case "-h", "--help":
			opts.showHelp = true
		case "--json":
			opts.jsonOutput = true
		case "--name":
			if hasVal {
				opts.name = val
			} else if i+1 < len(args) {
				i++
				opts.name = args[i]
			} else {
				return opts, usageFail(j, usage, "Error: flag needs an argument: --name")
			}
		default:
			return opts, usageFail(j, usage, "Error: flags provided but not defined: "+flagName(a))
		}
		i++
	}
	return opts, nil
}

// runRCStatus is the implementation of `wagyu remote-control status`.
// Defaults to plaintext output matching official `agy remote-control status`.
// Emits JSON when `--json` is passed.
func runRCStatus(j *JetskiCLI, args []string) error {
	usage := usageRCStatus(j.Name)
	opts, err := parseRCStatusFlags(j, args, usage)
	if err != nil {
		return err
	}
	if opts.showHelp {
		fmt.Fprint(j.ErrOut, usage)
		return nil
	}
	name := persistentrc.ResolveInstanceName(opts.name)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if !opts.jsonOutput {
		for _, l := range persistentrc.PlaintextDaemonStatus(ctx, name) {
			fmt.Fprintln(j.Out, l)
		}
		return nil
	}
	return printRCStatusJSON(j, ctx, name)
}

func printRCStatusJSON(j *JetskiCLI, ctx context.Context, name string) error {
	report, err := persistentrc.Status(ctx, name)
	if err != nil {
		report = persistentrc.StatusReport{
			Running: false,
			Name:    name,
			Hint:    fmt.Sprintf("status probe failed: %v", err),
		}
	}
	if report.Name == "" {
		report.Name = name
	}
	if report.Account == "" {
		report.Account = persistentrc.ResolveActiveAccount()
	}
	out, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return ExitCodeError(1, fmt.Sprintf("Error: status JSON encode failed: %v", err))
	}
	fmt.Fprintln(j.Out, string(out))
	return nil
}

func parseRCStopFlags(j *JetskiCLI, args []string, usage string) (name string, showHelp bool, err error) {
	for i := 0; i < len(args); i++ {
		a, val, hasVal := args[i], "", false
		if eq := strings.IndexByte(a, '='); eq >= 0 {
			val, a, hasVal = a[eq+1:], a[:eq], true
		}
		switch a {
		case "-h", "--help":
			showHelp = true
		case "--name":
			if hasVal {
				name = val
			} else if i+1 < len(args) {
				i++
				name = args[i]
			} else {
				return "", false, usageFail(j, usage, "Error: flag needs an argument: --name")
			}
		default:
			return "", false, usageFail(j, usage, "Error: flags provided but not defined: "+flagName(a))
		}
	}
	return name, showHelp, nil
}

// runRCStop is the cycle rc-persistent-daemon implementation of
// `wagyu remote-control stop`.  Idempotent — the underlying
// `persistentrc.Stop` treats a missing agent as success.
//
// Flags:
//
//	--name  the instance to stop (default: hostname)
//
// On success prints `Remote-control daemon stopped.` to
// stdout (matches the recovered stub's wording so existing
// scripts keep working).
func runRCStop(j *JetskiCLI, args []string) error {
	usage := usageRCStop(j.Name)
	name, showHelp, err := parseRCStopFlags(j, args, usage)
	if err != nil {
		return err
	}
	if showHelp {
		fmt.Fprint(j.ErrOut, usage)
		return nil
	}
	if name == "" {
		hn, herr := os.Hostname()
		if herr != nil || hn == "" {
			return ExitCodeError(1, fmt.Sprintf("Error: --name is required (could not determine hostname: %v)", herr))
		}
		name = hn
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if runtime.GOOS == "darwin" {
		_ = persistentrc.StopOfficialDaemon(ctx)
	}
	if err := persistentrc.Stop(ctx, name); err != nil && runtime.GOOS != "darwin" {
		return ExitCodeError(1, fmt.Sprintf("Error: %v", err))
	}
	fmt.Fprintln(j.Out, "Remote-control daemon stopped.")
	return nil
}
