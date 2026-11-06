package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	accounts "github.com/justwasm/pigasus/internal/features/accounts/core"
	"github.com/justwasm/pigasus/internal/features/auth"
	rclink "github.com/justwasm/pigasus/internal/infra/remote/link"
	persistentrc "github.com/justwasm/pigasus/internal/infra/remote/persistent"
)

// ---- subcommand_rc.go ----

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
// internal/infra/remote/link.
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

// ---- subcommand_rc_foreground.go ----

// errNoServeHandler reports that the binary was built without the
// serve bridge — the parser surface without the handler that brings
// the daemon up.
//
// It is a sentinel so each caller can answer in its own way: the
// reference's `remote-control serve` prints a notice and exits 0 (its
// own behaviour, pinned by a test), while `remote-control start` and
// `remote-control run` — the clone's own surfaces — exit 1.
var errNoServeHandler = errors.New("no serve handler registered")

// rcForegroundArgs is the resolved set of options the foreground
// daemon runs with.
type rcForegroundArgs struct {
	// Addr is the bind address (host:port). Empty means the
	// handler's default.
	Addr string
	// Name is the machine's instance label.
	Name string
	// NoLogin skips the inline OAuth device-code login when no
	// bearer token is on disk.
	NoLogin bool
	// Desktop serves the desktop UI (the voice input, the jetbox
	// terminal, the aux pane).
	Desktop bool
	// Label names the subcommand the user typed. The daemon's own
	// progress lines are prefixed with it, so the user sees the
	// command they ran — "remote-control serve", "remote-control
	// run", "remote-control start".
	Label string
}

// runRCForegroundDaemon brings the daemon up in the foreground and
// blocks until it comes down.
//
// It is the **one** implementation behind the three foreground
// surfaces:
//
//   - `remote-control serve` — the reference's own name for it, and
//     the command the launchd plist runs;
//   - `remote-control run` — the clone's explicit "run it here" name,
//     the same on every platform;
//   - `remote-control start` — on a platform without launchd, where
//     there is no service manager to register with.
//
// The stack is the same in all three: the dispatcher, the TCP and
// WebSocket server, and the cloud tunnel, in this terminal, until the
// user interrupts it.
//
// It returns errNoServeHandler when the binary was built without the
// serve bridge; every other error is the daemon's own.
func runRCForegroundDaemon(j *JetskiCLI, args rcForegroundArgs) error {
	if serveHandler == nil {
		return errNoServeHandler
	}
	label := args.Label
	if label == "" {
		label = "remote-control serve"
	}
	if err := serveHandler(j, ServeArgs{
		Addr:    args.Addr,
		Name:    args.Name,
		NoLogin: args.NoLogin,
		Desktop: args.Desktop,
		Label:   label,
	}); err != nil {
		return err
	}
	// The sentinel `remote-control stop` prints too, so a script can
	// share one grep between the two.
	fmt.Fprintln(j.Out, "Remote-control daemon stopped.")
	return nil
}

// resolveRCForegroundName resolves the instance name the foreground
// daemon registers under.
//
// The rules are the reference's: an explicit --name is sanitised by
// persistentrc.ResolveInstanceName, and an unset one falls back to the
// resolved instance name, then to the hostname.
func resolveRCForegroundName(name string) (string, error) {
	if name == "" {
		name = persistentrc.ResolveInstanceName("")
		if name == "" {
			hn, herr := os.Hostname()
			if herr != nil || hn == "" {
				return "", ExitCodeError(1, fmt.Sprintf("Error: --name is required (could not determine hostname: %v)", herr))
			}
			name = hn
		}
		return name, nil
	}
	return persistentrc.ResolveInstanceName(name), nil
}

// ---- subcommand_rc_rename.go ----

// usageRCRename documents the `remote-control rename` subcommand — the
// clone's own way to change the name the dashboard shows for this
// machine.
//
// It is **hidden**: the reference's `remote-control --help` lists
// start, status and stop only, and that block is the reference's, byte
// for byte. `rename` is accepted, not advertised — the same way `run`
// and `serve` are.
//
// The reference has no such subcommand: it renames the instance from
// the Web UI's settings panel, and the language server watches the
// configuration file for the change. The clone offers the same thing
// from the shell, because the shell is where the user is when the
// dashboard is on another machine.
func usageRCRename(n string) string {
	return "Usage: " + n + " remote-control rename <name>\n" +
		"\n" +
		"Set this machine's instance name shown in Remote Control\n" +
		"\n" +
		"The name is written to the configuration file, so a running daemon\n" +
		"picks it up and renames the instance on the dashboard without\n" +
		"dropping the connection.\n" +
		"\n" +
		"Flags:\n" +
		"  -h      Show help\n" +
		"  --help  Show help\n"
}

// runRCRename is the implementation of `pigasus remote-control rename`.
//
// The configuration file is the source of truth — the reference's own
// model: the CLI writes the name into the config proto
// (jetski/cli/remotecontrol.(*Daemon).SetInstanceName at 0x102d35320 →
// config.ApplyProtoUpdate) and the language server reads it back.
//
// A running daemon watches that file and renames the instance over the
// API, so the dashboard follows without the tunnel dropping. A daemon
// that is not running picks the name up when it next starts.
func runRCRename(j *JetskiCLI, args []string) error {
	usage := usageRCRename(j.Name)

	var name string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-h" || a == "--help":
			fmt.Fprint(j.ErrOut, usage)
			return nil
		case strings.HasPrefix(a, "-"):
			return usageFail(j, usage, "Error: flags provided but not defined: "+flagName(a))
		case name == "":
			name = a
		default:
			return usageFail(j, usage, "Error: rename takes exactly one name")
		}
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return usageFail(j, usage, "Error: rename needs a name")
	}
	if err := persistentrc.ValidateName(name); err != nil {
		// The shared validator's own message names the `--name`
		// flag; this subcommand takes the name positionally, so it
		// states the same rule in its own words.
		return ExitCodeError(2, fmt.Sprintf("Error: invalid instance name %q: %s", name, persistentrc.InvalidNameReason))
	}
	if err := persistentrc.SetInstanceName(name); err != nil {
		return ExitCodeError(1, fmt.Sprintf("Error: %v", err))
	}
	fmt.Fprintf(j.Out, "Instance name: %s\n", persistentrc.ResolveInstanceName(""))
	return nil
}

// ---- subcommand_rc_run.go ----

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

// ---- subcommand_rc_serve.go ----

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
	name, err := resolveRCForegroundName(opts.name)
	if err != nil {
		return err
	}
	err = runRCForegroundDaemon(j, rcForegroundArgs{
		Addr:    opts.addr,
		Name:    name,
		NoLogin: opts.noLogin,
		Desktop: opts.desktop,
		Label:   "remote-control serve",
	})
	if err == errNoServeHandler {
		// The reference's own behaviour: a notice, and a zero exit
		// code. Pinned by a test.
		fmt.Fprintln(j.ErrOut, "remote-control serve: no handler registered (binary was built without the serve bridge)")
		return nil
	}
	return err
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
