package cli

import (
	"context"
	"encoding/json"

	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/justwasm/pigasus/internal/accounts"
	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/persistentrc"
	"github.com/justwasm/pigasus/internal/rclink"
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

// rcIsDarwin reports whether this build runs on macOS — the only
// platform with launchd, and therefore the only one where
// `remote-control start` can register a persistent daemon.
//
// It is a variable so the non-darwin path can be exercised from a
// darwin test rig: the branch is the whole point of the change, and
// it has to be testable on the machine CI runs.
var rcIsDarwin = runtime.GOOS == "darwin"

func startOfficialDaemon(j *JetskiCLI, ctx context.Context, opts rcStartOpts) error {
	if curTok, curEmail := accounts.Current(); curTok != nil && curEmail != "" {
		_ = auth.SaveTokenUnified(curTok)
	}
	name := persistentrc.ResolveInstanceName(opts.name)
	// The instance id has to be resolved — and persisted — *before* the
	// daemon is bootstrapped. The daemon is a **separate process** (the
	// LaunchAgent runs "<binary> remote-control serve"), and it reads
	// the id from the same file: if the starter had not written it
	// yet, the two processes could each mint one, and the link the
	// starter prints would name an id the daemon never registered.
	instanceID, persisted := persistentrc.EnsureInstanceID()
	if !persisted {
		fmt.Fprintf(j.ErrOut,
			"remote-control start: the instance id could not be persisted (%s); the daemon may register a different one.\n",
			persistentrc.InstanceIDPath())
	}
	if err := persistentrc.StartOfficialDaemon(ctx, name, opts.session, opts.desktop); err != nil {
		return ExitCodeError(1, fmt.Sprintf("Error: %v", err))
	}
	fmt.Fprintln(j.Out, "Remote-control daemon started.")
	printRCLink(j, name, instanceID)
	return nil
}

// printRCLink prints the link that reaches this machine's remote view.
//
// The link goes through Google's account chooser: the cloud's instance
// rows are per Google account, and the bare deep link resolves against
// the browser's *default* account, so with several accounts signed in
// the dashboard answers "No instance found. Try switching to a
// different account." The chooser names the account the daemon
// registered with and then continues to the deep link.
//
// instanceID is the id the daemon registered with. It is passed in
// rather than derived here: the cloud's registry keys its rows by that
// id, so a link naming any other one resolves to "No instance found".
//
// This is a clone-side shape — the reference prints the bare companion
// origin and asks the user to look for the instance by name. See
// internal/rclink.
func printRCLink(j *JetskiCLI, name, instanceID string) {
	if name == "" {
		name = persistentrc.ResolveInstanceName("")
	}
	fmt.Fprintf(j.Out, "Open %s on another device to take over.\n", rclink.BrowserURL(instanceID, ""))
	if name != "" {
		fmt.Fprintf(j.Out, "Instance name: %s\n", name)
	}
}

// startRCForeground is the non-darwin path of `remote-control start`.
//
// There is no launchd on Linux, and the clone ships no systemd unit, so
// the *persistent* daemon cannot be registered — persistentrc.Start
// refuses with "launchd-backed mode requires macOS".
//
// The daemon itself is not macOS-only: the foreground path brings up the
// very same stack (the dispatcher, the TCP+WebSocket server and the
// cloud tunnel) and blocks until the user interrupts it. So on this
// platform `remote-control start` runs the daemon in the foreground
// rather than pretending to be a service manager — no daemon, the
// subcommand runs in the foreground.
//
// It is exactly what `remote-control run` does, under the name the user
// typed: `run` is the platform-independent spelling, `start` is the
// platform-dependent one.
func startRCForeground(j *JetskiCLI, opts rcStartOpts) error {
	if serveHandler == nil {
		return ExitCodeError(1, "Error: remote-control start: no handler registered (binary was built without the serve bridge)")
	}
	fmt.Fprintf(j.ErrOut,
		"remote-control start: %s/%s has no launchd — running the daemon in the foreground, as `remote-control run` does (Ctrl-C to stop)\n",
		runtime.GOOS, runtime.GOARCH)
	return runRCForegroundDaemon(j, rcForegroundArgs{
		Addr:    opts.addr,
		Name:    opts.name,
		Desktop: opts.desktop,
		Label:   "remote-control start",
	})
}

func startRCDaemon(j *JetskiCLI, opts rcStartOpts) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// macOS registers a launchd agent; everywhere else the daemon runs
	// in the foreground — see startRCForeground.
	if !rcIsDarwin {
		return startRCForeground(j, opts)
	}

	if opts.addr == "" {
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

	// The log file belongs to the *persistent* daemon (launchd
	// redirects the child's stdout and stderr into it). The foreground
	// path has no such file, so the path is only resolved when the
	// persistent one is taken.
	if rcIsDarwin && opts.logFile == "" {
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
	if rcIsDarwin {
		_ = persistentrc.StopOfficialDaemon(ctx)
	}
	if err := persistentrc.Stop(ctx, name); err != nil && !rcIsDarwin {
		return ExitCodeError(1, fmt.Sprintf("Error: %v", err))
	}
	if !rcIsDarwin {
		// There is no service manager here: the daemon runs in the
		// foreground, in the terminal that started it. The sentence on
		// stdout stays the reference's own, so a script that greps for
		// it keeps working; the note on stderr says where the daemon
		// actually is.
		fmt.Fprintf(j.ErrOut,
			"remote-control stop: %s/%s has no launchd — the daemon runs in the foreground (`remote-control run` starts it); stop it with Ctrl-C in its own terminal.\n",
			runtime.GOOS, runtime.GOARCH)
	}
	fmt.Fprintln(j.Out, "Remote-control daemon stopped.")
	return nil
}
