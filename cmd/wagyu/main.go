// Command wagyu is the openagy recovered 1:1 parse-parity shell of Google
// Antigravity CLI v1.3.4 (the v4 binary).
//
// Cycle 121 rename: the directory was `cmd/antigravity`; it is `cmd/wagyu`
// now, so the command no longer collides with the reference binary's own name.
// The Go package is `main` and nothing imports it, so only the directory and
// the references to it changed.
//
// Cycle 38 rewrite: switched the CLI engine from cobra/pflag to a
// 1:1 reimplementation of the v1.3.4 binary's internal `jetski/cli`
// (in internal/cli/jetski.go). The v1.3.4 binary does not import
// spf13 at all (zero matches in `strings antigravity`); it uses
// Go stdlib `flag` plus its own CLI engine. We mirror that stack.
//
// The CLI surface (31 top-level flags + 10 visible subcommands + their
// sub-subcommands) is wired in internal/cli, with this file providing the
// actual Run handlers for each leaf.
//
// What this binary does today (cycle 38):
//   - --help, --version: print v1.3.4-binary-style output and exit
//   - --tools, --services, --oauth, --tui: print static catalog snapshots
//     recovered from Ghidra / REA / c2go2 analysis (no live gRPC)
//   - --print, --prompt, -p: echo the prompt through the agent loop into
//     backend.NewMock() and stream a synthetic reply
//   - --tool NAME --tool-args JSON: dispatch to one of 32 recovered tools
//     (most are stubs; run_command is real via os/exec)
//   - Subcommands: plugin (9 subs), mcp (5 subs), remote-control (4 subs: start, serve, status, stop),
//     agent/agents (1 sub), install / mic-serve / update / models /
//     changelog (leaf stubs matching the v4 one-liners)
//
// What this binary does NOT do yet (deferred to later cycles):
//   - Real gRPC streaming against cloudaicompanion (sandbox unreachable)
//   - Real OAuth device flow (the 30s timeout in v4 is mocked here)
//   - --print actually talking to a real model — we still wire into the
//     mock backend; the agent context (kelvinzer0 / agw.k0s.io) work
//     lives in cmd/openagy-models (cycle 33 agent-1)
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/justwasm/pigasus/internal/remotecontrol"

	"github.com/justwasm/pigasus/internal/agent"
	"github.com/justwasm/pigasus/internal/agyprompt"
	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/branding"
	"github.com/justwasm/pigasus/internal/cli"
	"github.com/justwasm/pigasus/internal/daemon"
	"github.com/justwasm/pigasus/internal/onboarding"
	"github.com/justwasm/pigasus/internal/permissions"
	"github.com/justwasm/pigasus/internal/sandbox"
	"github.com/justwasm/pigasus/internal/tools"
	"golang.org/x/term"
)

// printModeSilenceLogger is the destination for the recovered
// debug-log lines ([lifecycle], [cascade], [cascade chain],
// [cascader chain], [backend], [cascader] error) inside --print
// mode. The v1.3.5 binary does not print any of these — they were
// a recovered-shell artefact, useful while the cycle 40+ cascade /
// cascader wiring was being verified, but not part of the binary's
// user-visible behaviour. By writing to io.Discard we make the
// --print path byte-level identical to v1.3.5 (stderr empty,
// stdout = model response only) while keeping the log lines in
// the code as documentation of the recovered FSMs.
//
// The same logger is reused across all recovered debug paths
// inside runPrintMode so the silence is uniform.
var printModeSilenceLogger = log.New(io.Discard, "", 0)

// glamourTTYCheck is the cycle-231 seam that decides whether the print-
// mode text-format chunk handler routes through glamour-on-Done (TTY)
// or the historical live-stream path (piped). Production wires it to
// golang.org/x/term.IsTerminal against os.Stdout; tests override it to
// pin the routing without spinning up a real PTY.
var glamourTTYCheck = func() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

// glamourRenderWidth is the cycle-268 seam that resolves the print-
// mode glamour render width when the write target is an io.Writer.
// The TUI has its own m.width (set from tea.WindowSizeMsg), so
// renderMarkdown there threads the explicit width; print mode has
// no equivalent and falls back to a runtime calculation against
// the writer's underlying file descriptor.
//
// Production wires it to golang.org/x/term.GetSize against the
// writer's fd; if that fails (the writer is a pipe, a buffer, or
// any non-TTY) it returns 0 so the caller falls back to the
// mdrender package's $COLUMNS / x/term chain. Tests override it to
// pin the width without spinning up a real PTY.
var glamourRenderWidth = func(out io.Writer) int {
	type fdWriter interface {
		Fd() uintptr
	}
	if w, ok := out.(fdWriter); ok {
		fd := int(w.Fd())
		if term.IsTerminal(fd) {
			if cols, _, err := term.GetSize(fd); err == nil && cols > 0 {
				return cols
			}
		}
	}
	return 0
}

// ---- catalog data (recovered) ---------------------------------------------
//
// These slices are static catalogs recovered from the v4 binary's symbol
// table (155,184 function names from gopclntab) and cross-checked against
// the public Antigravity documentation. They drive --tools, --services and
// the stubbed tool dispatcher. They are intentionally hard-coded here
// rather than read at runtime because the recovery was a one-shot
// pipeline; rebuilding the catalog is a separate task.

var recoveredTools = []string{
	"run_command", "create_file", "edit_file", "read_file", "delete_file",
	"list_dir", "search_files", "grep_search", "codesearch", "web_search",
	"web_fetch", "git_status", "git_diff", "git_commit", "git_push",
	"git_pull", "git_checkout", "git_branch", "git_merge", "git_log",
	"todo_write", "todo_read", "spawn_agent", "kill_agent", "list_agents",
	"mcp_call", "mcp_list_tools", "ask_user", "show_diff", "apply_patch",
	"run_tests", "lint_code",
}

var recoveredServices = []string{
	"CortexService/GenerateChat", "CortexService/StreamGenerateChat",
	"CortexService/FetchConfig", "CortexService/FetchLicenses",
	"CortexService/RefreshToken", "LanguageServerService/SelectLicense",
	"LanguageServerService/SelfAssignLicense", "ToolsService/Invoke",
	"ToolsService/ListTools", "WorkspaceService/Init",
	"WorkspaceService/Status", "ProjectService/Get",
	"ProjectService/List", "OAuthService/StartDeviceFlow",
	"OAuthService/PollDeviceCode", "OAuthService/RefreshToken",
	"RemoteControlService/Start", "RemoteControlService/Status",
	"RemoteControlService/Stop", "PluginService/Install",
	"PluginService/Uninstall", "MCPService/AddServer",
}

// Version string printed by --version and `antigravity version`. Cycle 57:
// the v1.3.5 binary prints exactly "1.3.5\n" with no prefix. We match
// 1:1 byte-for-byte. (Earlier cycles prefixed with "antigravity-recovered"
// but that diverges from the live binary's output.)
const cliVersion = cli.ReferenceVersion

// ---- cycle-125 init wiring ----------------------------------------------
//
// trust_prompt.go declares `isWorkspaceTrusted` and `addTrustedWorkspace`
// as package-level function variables (so the file does not import
// internal/auth directly and create an import cycle with cmd/wagyu).
// initWiring() below resolves them at process start. Wiring is done
// here, not in trust_prompt.go's init(), because main.go owns the
// auth package import.

func init() {
	isWorkspaceTrusted = func(dir string) (bool, error) {
		return auth.IsWorkspaceTrusted(dir)
	}
	addTrustedWorkspace = func(dir string) error {
		return auth.AddTrustedWorkspace(dir)
	}

	// v1.3.5 changelog gap: LC_TERMINAL=iTerm2 should upgrade the
	// colour profile to TrueColor when COLORTERM is not forwarded
	// (SSH case). The upstream charmbracelet/colorprofile only
	// consults COLORTERM; we layer our LC_TERMINAL rule on top and
	// push the resolved profile into lipgloss's package-level
	// Writer so every lipgloss.NewStyle() / Sprint() / Sprintf()
	// call from here on uses the right profile. NO_COLOR is
	// respected by the helper itself. See
	// colorprofile_lc_terminal.go for the precedence.
	lipgloss.Writer.Profile = resolveColorProfile(os.Environ())
}

// ---- main ----------------------------------------------------------------

func main() {
	// Cycle daemon-cleanup-on-exit (AGY 1.2.9 changelog): install the
	// global daemon.Tracker's SIGINT / SIGTERM / SIGHUP handlers at
	// the top of main so any headless (-p / --prompt) run terminates
	// background daemons before exiting. The TUI's bubbletea loop
	// installs its own SIGINT handler that translates the signal to
	// a tea.QuitMsg; the two handlers coexist — signal.Notify allows
	// multiple subscribers, and the bubbletea handler runs first
	// because it was installed first.
	//
	// A deferred Tracker.Shutdown is also armed so a normal exit
	// (main returns) drains the daemons the binary spawned. The
	// grace period is short enough that headless exit is prompt
	// (5 s, the recovered DefaultGracePeriod), long enough that a
	// well-behaved daemon that forwards SIGTERM to its own children
	// has time to shut down cleanly.
	daemon.InstallSignalHandlers()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		daemon.Global().Shutdown(ctx)
	}()

	// Cycle 48: write v1.3.5 binary 1:1 config defaults (settings.json,
	// mcp.json, installation_id, jetbox_state.json, jetski_state.pbtxt,
	// last_check.timestamp, cli.log) on first launch. Idempotent:
	// existing files are left alone (matches the v1.3.5 binary's
	// "read existing, don't overwrite" behaviour).
	//
	// Cycle 50 sub-agent A: the v1.3.5 binary is silent on this
	// path (the [legacy-compat] / [warn] legacy compat log lines were
	// a recovered-shell artefact, not part of the v1.3.5 binary's
	// user-visible behaviour). We keep the side-effect (writing
	// the config files is required for first-launch parity), but
	// drop the chatty stderr/stdout output. The work still happens;
	// the user just no longer sees it. Any error from
	// WriteCompatDefaults is captured but not surfaced here;
	// the operator can see the failure in cli.log (which
	// auth.WriteCompatDefaults writes to its own log
	// destination, not via the warning log line).
	if written, err := auth.WriteCompatDefaults(); err != nil {
		// Cycle 50: drop the [warn] line — the v1.3.5
		// binary is silent on stderr for first-launch
		// config writing. The error is recorded in cli.log
		// for operator triage; the user-visible behaviour
		// matches the v1.3.5 binary (silent).
		_ = err
		_ = written
	} else if len(written) > 0 {
		// Cycle 50: silent — matches v1.3.5 binary
		// (which doesn't print anything on first launch).
		_ = written
		_ = auth.AntigravityConfigHome()
	}

	// AGY 1.2.7 changelog line 215: on startup, kill any MCP server
	// or sidecar processes whose install directory has been deleted
	// since the last launch, and prune superseded plugin versions and
	// staging directories from the marketplace cache. Best-effort —
	// any error is dropped because the reference binary is silent on
	// this path and the user-visible behaviour must match (the
	// recoverable state is the cli.log, not stderr).
	if err := cli.RunPluginCleanupOnStartup(auth.AntigravityConfigHome()); err != nil {
		_ = err
	}

	p := cli.NewParser(
		cli.StandardFlags(),
		cli.StandardSubcommands(),
		// The v1.3.5 binary uses argv0's basename in "Usage of X:" and
		// in every subcommand usage preamble (verified by renaming the
		// binary). Mirror that so wrapper scripts and the parity harness
		// see identical text regardless of the installed name.
		filepath.Base(os.Args[0]),
		"Recovered " + branding.Current().CLIName + " (1:1 parse parity with v4 binary)",
		"pigasus is a 1:1 parse-parity shell of Google Antigravity CLI v1.3.3.\n"+
			"Top-level flags, subcommands and sub-subcommands match the v4 binary\n"+
			"so wrapper scripts and operator tooling are portable across both.\n"+
			"Runtime behavior is recovered from Ghidra 12.1.4 + REA 6.4.0 seeds.",
		// Cycle 33 agent-1: extra (openagy-only) flags. These are
		// appended on top of the v4 binary 31 so the 1:1 parse-parity
		// invariant in TestParseAllFlags is preserved.
		cli.ExtraFlags()...,
	)

	// Custom version template so `antigravity --version` matches v4 binary
	// byte-for-byte.
	p.Root().Version = cliVersion + "\n"

	// Top-level Run: this fires when no subcommand is matched. Most v4
	// invocations are `antigravity [flags] [prompt]` with no subcommand,
	// so this is the hot path. In jetski mode we wire it via the
	// RootRun field on the JetskiCLI.
	p.Root().RootRun = func(args []string) error {
		return runRoot(args, p)
	}

	// Cycle 41: wire the runChat dispatcher to the real runLiveTUI
	// so `antigravity chat` drops into the 5-panel bubbletea TUI.
	// The bridge pattern lets internal/cli/dispatch.go's runChat
	// stay import-cycle-free (it can't import cmd/wagyu).
	cli.SetChatBridge(runLiveTUI)

	// Cycle rc-persistent-daemon: same bridge pattern for the
	// hidden `remote-control-foreground` subcommand.  The hidden
	// spec in internal/cli/parser_specs.go binds to
	// runRCForegroundPlaceholder, which forwards to whatever was
	// registered here.  Same reason as the chat bridge — we don't
	// want internal/cli/ importing cmd/wagyu/.
	cli.RegisterForegroundHandler(runRemoteControlForeground)

	// Cycle rc-serve-subcommand: same bridge pattern for the
	// user-facing `remote-control serve` subcommand. The bridge
	// (internal/cli/serve_bridge.go) hands the parser-supplied
	// argv flags to runRemoteControlServe, which builds the
	// dispatcher + TCPServer in the foreground.
	cli.RegisterServeHandler(runRemoteControlServe)

	// Wire hidden `logo` subcommand to output the startup banner logo.
	cli.RegisterLogoHandler(runLogoSubcommand)

	cli.RegisterAgentsLister(func() []string {
		agents := agent.Discover()
		names := make([]string, 0, len(agents))
		for _, a := range agents {
			names = append(names, a.Name)
		}
		return names
	})

	if err := p.Execute(); err != nil {
		// jetski already printed the error/usage to the right stream;
		// exit with the code Parse resolved (1 handler failure, 2 usage
		// error) so the process exit status matches the v1.3.5 binary.
		os.Exit(p.ExitCode())
	}
}

// runRoot is the no-subcommand path. It handles all 31 top-level flags
// (--version, --tools, --services, --oauth, --tui, --print, --tool, …)
// and any free-form positional prompt. Order matters here because some
// flags imply "print and exit" (--version, --tools, --services, --oauth,
// --tui) and must short-circuit before the agent loop kicks in.
func runRoot(args []string, p *cli.Parser) error {
	out := p.Root().Out
	fs := p.Root().FlagSet()
	if fs == nil {
		// jetski.Parse wasn't called yet (test rig); bail.
		return nil
	}

	// --version: must match v4 binary byte-for-byte.
	if v := flagBoolValue(fs, "version"); v {
		fmt.Fprint(out, p.Root().Version)
		return nil
	}

	// --tools: list all 32 recovered tools and exit.
	if v := flagBoolValue(fs, "tools"); v {
		printTools(out)
		return nil
	}

	// --services: list all 22+ recovered gRPC services and exit.
	if v := flagBoolValue(fs, "services"); v {
		printServices(out)
		return nil
	}

	// --oauth: show OAuth config (sandbox: print the JSON skeleton, no live
	// device flow). The v4 binary runs a real device flow; we stub it.
	if v := flagBoolValue(fs, "oauth"); v {
		printOAuthConfig(out)
		return nil
	}

	// --tui: render the static 5-panel snapshot and exit.
	if v := flagBoolValue(fs, "tui"); v {
		return renderTUI(out)
	}

	// --tool NAME --tool-args JSON: execute a single tool and exit. This
	// shadows the agent loop and is the only way to call a tool without
	// spinning up the full cascade.
	if toolName := flagStringValue(fs, "tool"); toolName != "" {
		toolArgs := flagStringValue(fs, "tool-args")
		return dispatchTool(out, toolName, toolArgs)
	}

	// Cycle 236: --diff-with-agy prints the AGY 1.4.2 feature-parity
	// table sourced from
	// internal/cli/testdata/feature_diff_table.json. Pass
	// --output-format json alongside it for raw JSON. Short-circuits
	// before the agent loop, like --tools / --services / --oauth /
	// --tui.
	if flagBoolValue(fs, "diff-with-agy") {
		runFeatDiffCLI(out, printOutputFormat(fs))
		return nil
	}

	// AGY 1.3.3 changelog line 53: --remote-control + ADC (Application
	// Default Credentials) auth fails silently today, so users think
	// the daemon is running when nothing is. Remote Control does not
	// support ADC yet; fail fast with a clear, grep-able error.
	// The check sits after the --help/--version/--tool short-circuits
	// (those still need to work regardless of ADC) and before every
	// path that would otherwise fall through to the agent loop /
	// remote-control daemon. Returns *cli.ExitCodeError so the parser
	// prints the message to stderr and exits with code 1.
	if err := checkRemoteControlADC(fs); err != nil {
		return err
	}

	// --agent NAME: the reference validates the name before creating the
	// session. AGY 1.3.4 changelog: "Fixed `--agent` silently running the
	// default agent when the name matches no agent: headless `-p` runs now
	// exit with code 1 and list the available agents, and interactive
	// sessions show an `Agent Not Found` warning before falling back to the
	// default agent."
	//
	// Cycle agent-not-found-exit-1: catalogue and validator now live in
	// internal/agent (ValidateAgent). The empty string is accepted so
	// `--agent=` (no value) and the omitted flag behave identically.
	agentName := flagStringValue(fs, "agent")
	agentErr := agent.ValidateAgent(agentName)

	// --print / --prompt / -p: echo the prompt through the agent loop. We
	// wire the same backend the shims use (mock in sandbox) so the response
	// is at least deterministic.
	printMode := resolvePrintMode(fs)
	// AGY 1.3.4 changelog line 32: "Fixed combining --print with
	// --prompt-interactive exiting with status 0 after reporting the
	// conflict; it now exits with status 2 like other print-mode usage
	// errors." The parser already rejects this combination with the
	// canonical "error: --print and --prompt-interactive are mutually
	// exclusive" line and exit 2 (see internal/cli/jetski.go), but
	// runRoot can also be entered from a non-parser path (tests, future
	// dispatchers), and on that path the two flags used to be silently
	// accepted with printMode winning and the interactive intent lost.
	// This block re-checks the combination up front, before any model
	// call, with the same message and exit code the parser produces, so
	// the recovered behaviour holds regardless of how runRoot is reached.
	promptInteractive := flagWasSet(fs, "prompt-interactive") || flagWasSet(fs, "i")
	if printMode && promptInteractive {
		fmt.Fprintln(os.Stderr, "error: --print and --prompt-interactive are mutually exclusive")
		return cli.ExitCodeError(2, "error: --print and --prompt-interactive are mutually exclusive")
	}
	// Cycle 118: --json-schema is validated before anything else — before the
	// agent check and before --model/--effort validation, and on the
	// interactive path as well. Recovered ordering:
	// `-p=x --output-format json --json-schema=hello --effort=xhigh` reports
	// the schema error, not the effort error, and `--json-schema=hello` with
	// no --output-format reports the format error even without `-p`.
	if err := validateJSONSchemaFlag(fs); err != nil {
		return cli.ExitCodeError(1, err.Error())
	}
	if agentErr != nil && printMode {
		// Headless `-p` / `--print` / `--prompt`: exit 1 with the
		// available list. The error's Error() method already produces the
		// canonical one-line form, so we just lift it through ExitCodeError.
		return cli.ExitCodeError(1, agentErr.Error())
	}
	if printMode {
		// Cycle 101: the headless path validates --model/--effort before the
		// run starts (the interactive path does not — `agy --model Gemini`
		// reaches the TUI). Verified against 1.3.6.
		if _, err := resolveModelSelection(flagStringValue(fs, "model"), flagStringValue(fs, "effort")); err != nil {
			return cli.ExitCodeError(1, err.Error())
		}
	}
	if agentErr != nil && !isTerminalInteractive(os.Stdin) {
		// Interactive without a TTY: warn on stderr and fall back. A real
		// terminal renders the reference's ⚠ Agent Not Found card inside the
		// TUI instead (runLiveTUI), so it must not also leak into scrollback.
		// Layout verified against agy 1.4.3 (pty capture): header "⚠ Agent
		// Not Found", body "⎿  unknown agent ... Using the default agent
		// instead."
		var nf *agent.AgentNotFoundError
		if errors.As(agentErr, &nf) {
			fmt.Fprintf(os.Stderr, "Agent Not Found: %s. Using the default agent instead.\n",
				strings.TrimPrefix(nf.Error(), "error: "))
		}
	}
	if printMode {
		// Cycle 101: the prompt rides on the flag (-p "prompt"), never on a
		// positional argument. cli.Parse has already rejected positional
		// args, an empty prompt and a flag-shaped prompt value; this guard
		// covers direct runRoot callers (tests).
		prompt := printPrompt(fs)
		if prompt == "" {
			fmt.Fprintln(os.Stderr, "antigravity: --print requires a positional prompt argument")
			return fmt.Errorf("missing prompt")
		}
		// Cycle resume-continuation: --continue alongside --print has
		// to surface the same empty-store error the interactive path
		// would have shown, otherwise `agy -p X --continue` runs an
		// ambiguous fresh-session prompt against a workspace with no
		// resume target.
		if resolveBoolAlias(fs, "continue", "c") {
			cwd, _ := os.Getwd()
			if id, _, err := resolveContinue(cwd); err != nil {
				return cli.ExitCodeError(1, fmt.Sprintf("error: --continue: %v", err))
			} else if id == "" {
				continueNoConversationMessage(out)
				return cli.ExitCodeError(1, "no previous conversation to continue")
			}
		}
		warnAgentMode(fs, false)
		return runPrintMode(out, prompt, fs)
	}

	// --prompt-interactive / -i: same as --print but stays in the session
	// afterwards. The v1.3.5 binary delegates this to the live TUI (same
	// single-line-prompt paradigm as --tui). Drop into runLiveTUI so the
	// operator can keep interacting after the first turn; the TUI handles
	// the initial prompt rendering itself.
	promptInter := flagWasSet(fs, "prompt-interactive") || flagWasSet(fs, "i")
	if promptInter {
		warnAgentMode(fs, false)
		return runLiveTUI(out, fs)
	}

	// --continue / -c: continue the most recent conversation. The v1.4.0
	// changelog (recovered) states: "now falls back to the most recent
	// non-empty conversation in the current workspace or its
	// parent/child directories." Cycle resume-continuation ships that
	// behaviour through resolveConversationSelection (in conversation.go),
	// which now uses FindMostRecent for the parent/child fallback. We
	// mirror what the original runLiveTUI delegate did — the TUI loads
	// prior turns and the operator can pick up where they left off.
	if resolveBoolAlias(fs, "continue", "c") {
		warnAgentMode(fs, false)
		// On --print or any positional argument, the live TUI would
		// not run, so resolve --continue first and surface "No
		// previous conversation to continue." with exit code 1 when
		// nothing matches — this matches agy 1.4.0's documented
		// behaviour and lets wrapper scripts distinguish "no
		// session" from "fresh session" cleanly.
		if printMode {
			cwd, _ := os.Getwd()
			if id, _, err := resolveContinue(cwd); err != nil {
				return cli.ExitCodeError(1, fmt.Sprintf("error: --continue: %v", err))
			} else if id == "" {
				continueNoConversationMessage(out)
				return cli.ExitCodeError(1, "no previous conversation to continue")
			}
		}
		return runLiveTUI(out, fs)
	}

	// Default: no flags + no positional args → launch the live
	// 5-panel Bubbletea TUI. This is the cycle 40 change: before
	// this, `./antigravity` (no args) just printed the help banner
	// and exited; now it drops into a real interactive session
	// that listens on stdin (Ctrl+C / Esc / q to quit, Enter to
	// send, /model NAME to switch model, /clear to reset).
	//
	// Cycle 43: the v1.3.5 binary shows a 3-step onboarding wizard
	// (color scheme → terms → TUI) on first launch. We honor that
	// here by invoking the wizard before runLiveTUI. The wizard
	// can be bypassed with --no-onboarding for parse-parity shells
	// and CI smoke tests that just want the static TUI snapshot.
	//
	// Cycle 44 (issue #2): runLiveTUI now also honours --backend
	// (agw|shim) via pickBackend. The default is "agw"; if no
	// OAuth token is present, we auto-fall back to "shim" with a
	// one-line warning so the binary stays runnable in clean
	// sandboxes / first-launch environments.
	//
	// If there ARE positional args, treat them as a one-shot
	// prompt and run through the agent loop (matches the v4
	// binary's `antigravity "say hi"` behaviour). The wizard is
	// only shown on the no-prompt path.
	if len(args) == 0 {
		warnAgentMode(fs, false)
		if !flagBoolValue(fs, "no-onboarding") {
			// Cycle 51: gate the wizard on jetbox_state.json.
			// The v1.3.5 binary only shows the wizard on first
			// launch (when jetbox_state.json has no
			// post_onboarding_step field). On subsequent
			// launches, the wizard is skipped entirely and the
			// TUI drops in. We mirror that behaviour.
			//
			// The v1.3.5 binary also writes the chosen color
			// scheme to settings.json + sets
			// post_onboarding_step on completion. Our previous
			// behaviour always re-rendered the wizard, which the
			// user flagged as "不应该每次都显示所谓的启动配置选项,
			// 而且它从来没有生效过" (it shouldn't show every
			// time and the choices never take effect).
			if !auth.OnboardingCompleted() {
				wz := onboarding.NewWizard()
				wz.Out = out
				if err := wz.Run(); err == nil && wz.Step == onboarding.StepDone {
					// Persist colorScheme to settings.json so
					// the next launch's RenderV4Style honours it
					// (cycle 51 close-gap with the user complaint
					// that choices "从来没有生效过").
					if schemeErr := auth.SaveSettingsColorScheme(wz.Selected.String()); schemeErr != nil {
						_ = schemeErr
					}
					// Mark onboarding complete so the next
					// launch skips the wizard.
					s, _ := auth.LoadJetboxState()
					s.PostOnboardingStep = "agent_configuration"
					if s.AgentEnvironment == "" {
						s.AgentEnvironment = "local"
					}
					if s.ThemeMode == "" {
						s.ThemeMode = "dark"
					}
					if s.ConversationWidth == "" {
						s.ConversationWidth = "wide"
					}
					if s.TerminalPlacement == "" {
						s.TerminalPlacement = "below_input"
					}
					if s.UserNuxEvent == "" {
						s.UserNuxEvent = "first_run"
					}
					_ = auth.SaveJetboxState(s)
				}
			}
		}
		return runLiveTUI(out, fs)
	}
	prompt := strings.Join(args, " ")
	warnAgentMode(fs, false)
	return runPrintMode(out, prompt, fs)
}

// ---- helpers --------------------------------------------------------------

// resolvePrintMode returns true if --print, --prompt or -p was given. Cycle
// 101: these are string flags in the reference (the prompt is their value), so
// presence is tested with Visit rather than by reading a bool.
func resolvePrintMode(fs *flag.FlagSet) bool {
	return printPromptSet(fs)
}

// customAgentFromFlags resolves --agent to a discovered markdown agent. It
// returns nil for the default agent, the empty value, or a name ValidateAgent
// already rejected.
func customAgentFromFlags(fs *flag.FlagSet) *agent.Agent {
	name := flagStringValue(fs, "agent")
	if name == "" || name == "default" {
		return nil
	}
	if a, ok := agent.Lookup(name); ok {
		return &a
	}
	return nil
}

// customSubagents lists the markdown agents the reference adds to the default
// agent's <subagents> section. The builtin self/research/image-generator
// entries come from agyprompt; these are appended in name order.
func customSubagents() []agyprompt.Subagent {
	agents := agent.Discover()
	out := make([]agyprompt.Subagent, 0, len(agents))
	for _, a := range agents {
		out = append(out, agyprompt.Subagent{Name: a.Name, Description: a.Description})
	}
	return out
}

// agentBody is the custom agent's Markdown body, or "" for the default agent.
func agentBody(a *agent.Agent) string {
	if a == nil {
		return ""
	}
	return a.Body
}

// knownAgent reports whether name refers to an agent this build can run.
// Thin wrapper around agent.ValidateAgent that returns only the bool.
// The catalogue itself lives in internal/agent/list.go; this is kept
// for any caller that just wants a yes/no without the typed error.
// Verified against 1.3.6: `--agent default` and `--agent ""` proceed,
// `--agent nosuchagent` fails with rc 1 (in headless `-p` mode).
func knownAgent(name string) bool {
	return agent.ValidateAgent(name) == nil
}

// printPromptSet reports whether any of the print-mode prompt flags was given.
func printPromptSet(fs *flag.FlagSet) bool {
	for _, n := range []string{"print", "prompt", "p"} {
		if flagWasSet(fs, n) {
			return true
		}
	}
	return false
}

// printPrompt returns the prompt carried by -p/--print/--prompt, preferring the
// long form when several aliases are present.
func printPrompt(fs *flag.FlagSet) string {
	for _, n := range []string{"print", "prompt", "p"} {
		if flagWasSet(fs, n) {
			return flagStringValue(fs, n)
		}
	}
	return ""
}

// flagWasSet reports whether the named flag appeared on the command line.
// stdlib `flag` only records this through Visit, so a flag explicitly set to
// its zero value (e.g. `-p=""`) is distinguishable from an absent one.
func flagWasSet(fs *flag.FlagSet, name string) bool {
	if fs == nil {
		return false
	}
	set := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			set = true
		}
	})
	return set
}

// warnAgentMode emits the reference's stderr warnings for the global
// --mode flag on an agent-session entry point (see
// internal/cli/mode_warning.go for the recovered strings and the probed
// emission surface).
//
// sessionCreation is true on the paths that build the session before any
// TTY check — i.e. --print, where the reference also runs the
// auto-unavailable entitlement check. The interactive paths die at the
// TTY check before reaching it, so they pass false.
func warnAgentMode(fs *flag.FlagSet, sessionCreation bool) {
	mode := flagStringValue(fs, "mode")
	cli.WarnMode(os.Stderr, mode)
	if sessionCreation && mode == cli.ModeAuto {
		fmt.Fprintln(os.Stderr, cli.AutoUnavailableWarning(mode))
	}
}

// resolveBoolAlias returns true if either the canonical or its short alias
// is set. Used for -c/--continue and -i/--prompt-interactive where v4
// lists both as separate flag entries.
func resolveBoolAlias(fs *flag.FlagSet, canonical, alias string) bool {
	if v := flagBoolValue(fs, canonical); v {
		return true
	}
	if v := flagBoolValue(fs, alias); v {
		return true
	}
	return false
}

// flagBoolValue returns the bool value of a flag, or false if unset.
// stdlib `flag` doesn't expose GetBool, so we duck-type the value.
func flagBoolValue(fs *flag.FlagSet, name string) bool {
	if fs == nil {
		return false
	}
	f := fs.Lookup(name)
	if f == nil {
		return false
	}
	return f.Value.String() == "true"
}

// flagStringValue returns the string value of a flag, or "" if unset.
func flagStringValue(fs *flag.FlagSet, name string) string {
	if fs == nil {
		return ""
	}
	f := fs.Lookup(name)
	if f == nil {
		return ""
	}
	return f.Value.String()
}

// flagStringValues returns the per-occurrence values of a
// stringSlice flag. The cli parser implements stringSlice
// via a comma-joined String() (see internal/cli/jetski.go's
// stringSliceValue); a single comma in any path is
// vanishingly rare on real filesystems, so splitting on
// commas gives us the same slice the parser accumulated
// when --add-dir was repeated on the command line. Empty
// entries are dropped; entries are returned in the order
// the parser saw them on the wire.
//
// Used by rc-file-ops to feed --add-dir into the
// WorkdirScope the dispatcher carries.
func flagStringValues(fs *flag.FlagSet, name string) []string {
	raw := flagStringValue(fs, name)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// resolveBoolAlias is defined above (cycle 38: bool alias via flagBoolValue).

// runPrintMode streams a one-shot reply for `prompt` through the agent
// loop into a Mock backend. The streaming output goes line-by-line to w;
// the terminating chunk carries mock usage metadata.
//
// Cycle 33 agent-1: if --model-config is set (or the env var
// OPENAGY_MODELS_CONFIG is non-empty) and the registry contains the
// named model, we swap the Mock for a real provider via
// backend.NewRealClient(). This is the headline change: `antigravity
// --print --model=agw "hi"` now talks to agw.k0s.io (or whatever the
// registry points at) instead of returning a synthetic "mock reply".
//
// Cycle 40 Cascade integration: we also wire an OnStateTransition
// observer so the user sees the recovered state machine walked end-to-end
// (`init -> read -> plan -> execute -> stream -> save -> done`). The
// transitions are printed to stderr so they don't pollute the streamed
// response on stdout; the final happy-path chain is also rendered as a
// single line at the end of the turn for easy grep.
//
// Cycle 41: the actual streaming now goes through the recovered
// `cascader.Run` lifecycle (cascade.RealLifecycle) wrapping a
// backend.Backend (the Init/Run/Emit/Save/Close lifecycle). The
// agent.Loop is still wired in so the cycle 40 Cascade observer keeps
// firing — but the underlying client.Stream call has been replaced
// by backend.Lifecycle.Run, which calls through to the Client
// internally. The visible behavior to the user is identical except
// for an extra set of [lifecycle] transitions on stderr that show the
// recovered Backend FSM walking init -> ready -> execute -> stream ->
// save -> done.
func runPrintMode(w io.Writer, prompt string, fs *flag.FlagSet) error {
	modelName := flagStringValue(fs, "model")
	effort := flagStringValue(fs, "effort")
	if modelName == "" {
		modelName = auth.ResolveSettingsModel("Gemini 3.8 Flash (High)")
	}
	_ = effort // effort is plumbed through the backend in a later cycle

	// Cycle 230 (1.2.16 changelog): --remote-control daemon. When set, we
	// bring up a Unix-socket JSON-RPC server that exposes status /
	// list_agents / get_output to external dashboards. The daemon's
	// SessionState is updated by the chunk handler below so the
	// dashboard sees the live model + turn output for the duration of
	// this turn. The daemon is torn down on every return path so a
	// print turn never outlives its listener.
	rcSess := remotecontrol.NewSessionState(cliVersion)
	rcSess.Model = modelName
	rcDaemon := maybeStartRemoteControlDaemon(context.Background(), fs, rcSess)
	bindRemoteControlSession(rcDaemon, rcSess)
	defer shutdownRemoteControlDaemon(rcDaemon, os.Stderr)

	// Cycle 114: `--output-format` selects the stdout shape. `text` is the
	// default and is what every earlier cycle implemented; `json` replaces the
	// plain text with a single-line envelope (see print_format.go).
	format := printOutputFormat(fs)

	// Cycle 117: --print-timeout. The reference's default headless timeout is
	// unlimited; a positive value bounds the run, and expiry returns whatever
	// the turn produced with a warning on stderr and exit code 0.
	timeout := printTimeout(fs)

	// Cycle headless-partial-response-flag (AGY 1.2.10 close-gap): when set,
	// the chunk handler routes each sanitised text chunk to stdout as soon as
	// the model emits it (json: raw text, stream-json: a `partial_response`
	// event). The buffered-into-envelope behaviour stays in place so the
	// final jsonEnvelope / `result` event still carries the full response.
	// Text format was already streaming, so the flag is a no-op there. The
	// default is false to preserve the byte-level contract against the
	// v1.3.6 capture the clone was pinned to.
	partialResponse := flagBoolValue(fs, "print-partial-response")

	// Cycle 120: --conversation / -c resolve before anything else runs, so a
	// missing conversation warns even on the local-slash-command path (the
	// reference resolves the session up front).
	sel := resolveConversationSelection(fs, os.Stderr)

	// Cycle 99: the reference expands slash commands client-side in print
	// mode — a handful run locally, the interactive-only ones are rejected
	// with a recovered message, and everything else is sent to the model as
	// literal text. Before this every "/..." prompt was forwarded to the
	// model, so `agy -p /help` produced a model reply.
	// Cycle 203: a resolved "/<skill>" returns Handled=false with a non-empty
	// Prompt — the skill's instruction text — which replaces the literal token
	// before the model turn below. (An unknown non-skill prompt leaves Prompt
	// empty and is forwarded as before.)
	res := dispatchPrintSlash(prompt, fs)
	if res.Prompt != "" {
		prompt = res.Prompt
	}
	if res.Handled {
		if format != printFormatText {
			env := jsonEnvelope{Status: printStatusSuccess, Response: res.Output}
			switch {
			case res.Err != nil:
				// A local failure still writes the envelope to stdout (the
				// reference does, on the rc 1 and rc 2 paths alike) with an
				// empty response and the message in `error`. stream-json
				// emits only the `result` event here — there is no command
				// payload to report.
				env.Status = printStatusError
				env.Response = ""
				env.Error = res.ErrorText
			case res.Name != "":
				env.Command = &jsonCommandPayload{Name: res.Name, Data: res.Data}
				if format == printFormatStreamJSON {
					if err := writeJSONLine(w, jsonStreamEvent{
						Event:   streamEventCommandResult,
						Command: env.Command,
					}); err != nil {
						return err
					}
				}
			}
			if format == printFormatStreamJSON {
				if err := writeJSONLine(w, jsonStreamEvent{Event: streamEventResult, Result: &env}); err != nil {
					return err
				}
				return res.Err
			}
			if err := writeJSONLine(w, env); err != nil {
				return err
			}
			return res.Err
		}
		if res.Output != "" {
			fmt.Fprint(w, res.Output)
		}
		return res.Err
	}

	// Cycle 114: a model turn under --output-format json has its answer
	// buffered so it can be embedded in the envelope instead of streamed to
	// stdout. The reference's `response` member holds the answer text itself,
	// without the trailing newline the text format appends.
	started := time.Now()
	conversationID := newEnvelopeConversationID()
	// Cycle 120: a resumed run keeps the conversation's own id, so the
	// envelope reports the id the caller passed (or the one `-c` picked).
	if sel.ID != "" {
		conversationID = sel.ID
	}
	// Cycle 230 (1.2.16): publish the conversation id to the
	// --remote-control daemon so get_output resolves to it by
	// default when the caller does not pass conversation_id.
	rcSess.SetConversationID(conversationID)
	var responseBuf strings.Builder
	out := w
	if format != printFormatText {
		out = &responseBuf
	}
	// Cycle 231: print-mode glamour-on-Done. When stdout is a TTY and
	// the user picked text format, every sanitised chunk is held in a
	// per-turn buffer and the whole reply is glamour-rendered once at
	// chunk.Done. ANSI escapes are noise on a piped stdout, and JSON /
	// stream-json would be made invalid by an ANSI passthrough, so the
	// historical live-stream behaviour (the cycle-50 chunkHandler shape)
	// is preserved for every other format / destination combination.
	textRouter := newPrintTextRouter(format == printFormatText && glamourTTYCheck())
	var turnUsage jsonUsage
	// turnStarted flips on the first chunk of the turn; turnBegan records
	// whether that happened before the --print-timeout deadline. The reference
	// counts a turn only once it has really begun: the captured 1ms run reports
	// `num_turns: 0` and emits no step_update, while a 429 failure reports
	// `num_turns: 1` with the `user_input` step.
	var turnStarted, turnBegan bool
	// deadline is the --print-timeout instant, or the zero time when the run is
	// unbounded (the reference's default).
	var deadline time.Time
	if timeout > 0 {
		deadline = started.Add(timeout)
	}

	be := pickBackendWithLog(fs, modelName, printModeSilenceLogger.Writer(), backend.AgwTimeoutUnlimited)
	// Cycle 230 (1.2.16): publish the resolved backend to the
	// --remote-control daemon. Done after pickBackendWithLog so
	// the dashboard sees the canonical "agw" / "shim" value,
	// not the raw --backend flag value.
	if resolvedBackend, _ := resolveBackend(fs, io.Discard); resolvedBackend != "" && rcDaemon != nil {
		rcSess.SetBackend(resolvedBackend)
	}
	// Cycle 97: build the advertised tool catalog for the current
	// workspace and a runner that executes the model's tool calls.
	workspace, _ := os.Getwd()
	catalog := tools.Catalog(workspace)
	// Cycle 293: init.tools is the reference's 60-name *available* surface,
	// not the twelve declarations sent to the model. Capture the names before
	// adding the 1.4.4 executors (read_file / write_file / edit_file), which
	// exist only so the runner can execute what the model calls.
	initTools := toolDeclsFromRegistry(catalog)
	tools.AddReferenceTools(catalog, workspace)
	// Cycle 115: `--output-format stream-json` reports the session's starting
	// state before the first model call. The tool list is the same one the
	// reference reports there (60 names), and `permission_mode` is the
	// toolPermission setting (captured: "always-proceed").
	if format == printFormatStreamJSON {
		names := make([]string, 0, len(initTools))
		for _, d := range initTools {
			names = append(names, d.Name)
		}
		if err := writeJSONLine(w, jsonStreamEvent{
			Event:          streamEventInit,
			ConversationID: conversationID,
			Init: &jsonInitPayload{
				CWD:            workspace,
				Tools:          names,
				PermissionMode: printPermissionMode(),
			},
		}); err != nil {
			return err
		}
	}
	// Cycle 98: tool calls are gated by the recovered permission policy. In
	// headless mode a call that is not allowed is soft-denied.
	// Cycle 228 (AGY 1.2.10 close-gap): the terminal sandbox runs as the
	// outer gate so an out-of-bounds path lands in the soft-deny path
	// even when the policy would otherwise allow it. The sandbox is the
	// stricter of the two gates; see permissionRunner.Run.
	printPolicy := printModePolicy(fs)
	printSandbox := printModeSandbox(fs, workspace, conversationID)
	gate := &permissionRunner{
		inner:   &registryRunner{reg: catalog},
		policy:  &printPolicy,
		sandbox: printSandbox,
	}
	// Cycle 50 sub-agent A: byte-level 1:1 with the v1.3.5
	// binary's --print mode. The v1.3.5 binary produces ONLY the
	// model response on stdout; stderr is empty. To match that
	// we redirect every recovered debug log line
	// ([lifecycle]/[cascade]/[cascade chain]/[cascader
	// chain]/[backend]/[cascader] error) to the
	// printModeSilenceLogger, and we drop the [usage] line
	// entirely from the chunk handler (the v1.3.5 binary does
	// not print usage info to stdout for --print either).
	//
	// Sanitizer is the chunk-aware variant of SanitizeText. The
	// recovered 1.3.5 binary's printmode emitter happily splits
	// a single `<think>...</think>` block across many 12-byte
	// SSE chunks, so a one-shot regex on each chunk would miss
	// the boundary. Sanitizer buffers partial tags at chunk
	// boundaries and only emits text outside a think block.
	sanitizer := agent.NewSanitizer()
	// Cycle 209: the chunk handler is a named value so both runtimes feed the
	// same output path — the PiG-runtime turn in print_pig.go synthesises
	// backend.StreamChunks and this keeps every byte of the stdout contract in
	// one place.
	chunkHandler := func(chunk backend.StreamChunk) error {
		// Cycle 117: the `user_input` step marks the turn actually
		// starting, which is why it is emitted on the first chunk rather
		// than at init time. Captured: with `--print-timeout=1ms` the
		// reference emits `init` and `result` but NO step_update at all,
		// and reports `num_turns: 0`, because the deadline fired before the
		// turn began. The same counter feeds the envelope's num_turns.
		if !turnStarted {
			turnStarted = true
			turnBegan = deadline.IsZero() || time.Now().Before(deadline)
			if turnBegan && format == printFormatStreamJSON {
				if err := emitStep(w, conversationID, 0, "user_input"); err != nil {
					return err
				}
			}
		}
		if chunk.Error != nil {
			// Cycle 112: the failure is reported once, on stderr, by
			// the headless error reporter (see api_error.go), which
			// emits the recovered `error: <msg>` + `AGY_ERROR: {...}`
			// pair and exit code 3. Printing it here as well would put
			// an `[error] ...` line on stdout, which the reference
			// never does.
			return nil
		}
		// Cycle 114: the terminating chunk's metadata is the envelope's
		// `usage` object. cache_read_tokens has no counterpart on the agw
		// transport, so it stays 0.
		if chunk.Meta != nil {
			turnUsage = jsonUsage{
				InputTokens:    chunk.Meta.PromptTokens,
				OutputTokens:   chunk.Meta.CompletionTokens,
				ThinkingTokens: chunk.Meta.ReasoningTokens,
				TotalTokens:    chunk.Meta.TotalTokens,
			}
		}
		if chunk.Text != "" {
			sanitized := sanitizer.Push(chunk.Text)
			// Cycle headless-partial-response-flag: when set, also
			// stream the sanitised chunk immediately so long-running
			// turns give earlier feedback. The buffered path (write
			// to `out`, which is `w` for text or `&responseBuf` for
			// json/stream-json) keeps running so the final envelope
			// still carries the full response. See
			// print_partial_response.go for the per-format routing.
			partial := partialResponseState{
				W:           w,
				ResponseBuf: &responseBuf,
				Format:      format,
				Enabled:     partialResponse,
			}
			if err := partial.WriteChunk(sanitized); err != nil {
				return err
			}
			// Cycle 231: route the sanitised chunk through the
			// text-format router — glamour-buffer (useGlamour=true)
			// accumulates silently, or live-stream writes to out
			// (useGlamour=false). The printTextRouter's Push method
			// owns the per-format divergence; the chunk handler stays
			// agnostic.
			if err := textRouter.Push(sanitized, out); err != nil {
				return err
			}
			// Cycle 230 (1.2.16 changelog): --remote-control daemon's
			// session mirror the same sanitized text the user sees.
			// The sanitizer is shared (chunkPush below) so a dashboard's
			// get_output tool returns exactly what the user sees,
			// minus <think>…</think> blocks.
			//
			// Cycle 231: when glamour is buffering the reply (text
			// format + TTY), hold the mirror until Done so the
			// dashboard's get_output returns the rendered text the
			// user sees, not the raw sanitised source.
			if rcDaemon != nil && !textRouter.UseGlamour() {
				rcSess.AppendTurnOutput(sanitized)
			}
		}
		if chunk.Done {
			// Cycle 50 sub-agent A: flush any
			// text the sanitizer held back at
			// a chunk boundary, then drop the
			// [usage] line entirely. The
			// v1.3.5 binary does not print
			// prompt/completion/total
			// counters to stdout for
			// `--print`; the recovered
			// shell's [usage] line was a
			// cycle 33+ addition for
			// observability that is no
			// longer part of the binary's
			// user-visible behaviour.
			// We keep the trailing
			// newline so the prompt
			// terminator still prints.
			flushed := sanitizer.Finish()
			// Cycle headless-partial-response-flag: the flush
			// carries whatever the sanitizer held back at the
			// final chunk boundary, so it must also go through
			// the partial side-channel when the flag is set —
			// otherwise a single trailing think-block close would
			// stay buffered until end-of-turn and the user would
			// only see it inside the final envelope.
			partial := partialResponseState{
				W:           w,
				ResponseBuf: &responseBuf,
				Format:      format,
				Enabled:     partialResponse,
			}
			if err := partial.WriteChunk(flushed); err != nil {
				return err
			}
			// Cycle 231: the text-format router either writes the
			// glamour-rendered buffer (useGlamour=true) or the held-
			// back flushed text (useGlamour=false) to out. Returns
			// the text actually written, so the --remote-control
			// daemon's mirror matches what the user saw.
			rendered, err := textRouter.Flush(flushed, out)
			if err != nil {
				return err
			}
			if format == printFormatText {
				fmt.Fprintln(out)
			}
			// Cycle 231: under glamour, mirror the rendered text to
			// the --remote-control daemon once at Done so the
			// dashboard's get_output returns what the user actually
			// saw (the styled output), not the raw sanitised source.
			if rcDaemon != nil && textRouter.UseGlamour() {
				rcSess.AppendTurnOutput(rendered)
			}
		}
		return nil
	}

	// Cycle 209/210: PiG's coding runtime is the only engine a print turn runs
	// on. It owns tools, MCP and skills, and every backend drives it — an agw
	// client through the native provider, everything else (the offline mock
	// the shims use) through piagentbridge.BackendProvider. A resumed
	// conversation replays its earlier turns into the session log. See
	// print_pig.go and docs/cycles/cycle-210/; the clone's loop now serves only
	// the interactive TUI.
	ctx, cancel := printContext(timeout)
	defer cancel()
	denials, runErr := runPigPrintTurn(ctx, pigPrintConfig{
		Backend:   be,
		Model:     modelName,
		Prompt:    prompt,
		History:   pigHistory(sel),
		Catalog:   catalog,
		Policy:    &printPolicy,
		Handler:   chunkHandler,
		Workspace: workspace,
		AgentDir:  auth.AntigravityHome(),
		// The system prompt's <user_information> names this id and the
		// artifact directory under it; it is the same id the stream-json
		// envelope reports.
		ConversationID: conversationID,
		// Cycle effort-picker: forward --effort to PiG so the provider
		// sees the reasoning level the user requested. Validation against
		// (--model, --effort) ran in main.go's mainDispatcher before this
		// point; an empty Effort keeps the model's own default.
		Effort: flagStringValue(fs, "effort"),
		// A named custom agent replaces the default prompt and tool set.
		AgentBody: agentBody(customAgentFromFlags(fs)),
	})
	gate.Denials = append(gate.Denials, denials...)

	// Cycle 117: a turn cut off by --print-timeout is not a failure. Captured
	// from `agy -p='say hi' --print-timeout=1ms`: rc 0, the partial output on
	// stdout, and
	//
	//	[agy] print timeout after 1ms with turn in progress; returning partial output
	//
	// on stderr. An interrupt (Ctrl+C) still exits non-zero, so only a deadline
	// is treated this way.
	timedOut := timeout > 0 && runErr != nil &&
		(errors.Is(runErr, context.DeadlineExceeded) || ctx.Err() == context.DeadlineExceeded)
	if timedOut {
		fmt.Fprintln(os.Stderr, printTimeoutWarning(timeout))
		runErr = nil
	}
	// Cycle 98: report the calls headless mode had to auto-deny. The reference
	// prints this notice at the end of the run; it is the only user-visible
	// trace of the permission gate, so it stays silent when nothing was denied.
	if n := len(gate.Denials); n > 0 {
		fmt.Fprintln(os.Stderr, permissions.HeadlessNotice(n))
	}
	// Cycle 112: a turn that dies on a model API failure is reported with the
	// recovered structured pair — `error: <msg>` then `AGY_ERROR: {...}` on
	// stderr — and exits with code 3 instead of 1. Non-API failures (usage,
	// tool arguments, …) keep the ordinary exit path.
	exit := runErr
	if runErr != nil {
		// Cycle 262: an *EligibilityError surfaces as the AGY_ERROR
		// JSON envelope on stderr in the same shape every other
		// model API failure uses. classifyEligibilityError runs
		// first so it takes precedence over classifyAPIError
		// (a 403 from the backend would otherwise classify as
		// PERMISSION_DENIED — the cycle's "ELIGIBILITY_DENIED"
		// status is what an automation script branches on to
		// surface the verification URL). The TUI path does not
		// touch this branch — it has no stderr and no exit code; it
		// renders the inline notice via cmd/wagyu/elig_inline.go
		// instead.
		if api, ok := classifyEligibilityError(runErr); ok {
			exit = cli.ExitCodeError(headlessAPIExitCode, api.Lines())
		} else if api, ok := classifyAPIError(runErr); ok {
			exit = cli.ExitCodeError(headlessAPIExitCode, api.Lines())
		}
	}
	// Cycle 114: under --output-format json the answer goes out as one envelope
	// instead of the streamed text. The reference emits it on the success and
	// failure paths alike, with `num_turns` 1 once a turn has started even when
	// that turn fails, and it keeps the stderr lines and the exit code of the
	// text format.
	if format != printFormatText {
		env := jsonEnvelope{
			ConversationID:  conversationID,
			Status:          printStatusSuccess,
			Response:        responseBuf.String(),
			DurationSeconds: int(time.Since(started).Seconds()),
			Usage:           turnUsage,
		}
		// num_turns counts a turn only once it has begun before the deadline;
		// a deadline that fires first reports 0, which is what the 1ms capture
		// shows. A resumed conversation's earlier user turns are counted too:
		// the reference reported num_turns 2 for `-c` on a one-turn
		// conversation while a fresh run reports 1.
		if turnBegan {
			env.NumTurns = 1 + sel.PriorUserTurns
		}
		if runErr != nil {
			env.Status = printStatusError
			env.Response = ""
			env.Error = printErrorMessage(runErr)
		}
		if format == printFormatStreamJSON {
			// The step that ends the turn. The reference names it
			// `error_message` when the turn failed and `agent_response`
			// when it produced an answer; the failure sequence is the one
			// that was captured. A timeout emits no closing step at all —
			// the captured 1ms run has no step_update whatsoever.
			if turnBegan && !timedOut {
				stepType := "agent_response"
				if runErr != nil {
					stepType = "error_message"
				}
				if err := emitStep(w, conversationID, 1, stepType); err != nil {
					return err
				}
			}
			// Cycle headless-jsonl-mode-trailing-newline (AGY 1.2.7
			// close-gap): when the user passes `--verbose` alongside
			// `--output-format stream-json`, emit one
			// `assistant_message` event before the closing `result`
			// envelope. The event carries the assistant text body, the
			// same value the envelope's `response` already carries, so
			// tooling that only watches `assistant_message` does not
			// need to parse the buffered envelope. emitAssistantMessage
			// routes through writeJSONLine so the trailing newline is
			// always present — the AGY 1.2.7 changelog bug fix pinned
			// on this property. An empty assistant text (a turn that
			// did not produce any visible text) is a no-op so the
			// stream is not polluted with empty frames.
			if printVerbose(fs) && turnBegan && !timedOut && env.Response != "" {
				if err := emitAssistantMessage(w, env.Response, modelName); err != nil {
					return err
				}
			}
			if err := writeJSONLine(w, jsonStreamEvent{Event: streamEventResult, Result: &env}); err != nil {
				return err
			}
			return exit
		}
		if err := writeJSONLine(w, env); err != nil {
			return err
		}
	}
	return exit
}

// toolDeclsFromRegistry converts a tool registry into the advertised tool
// list, sorted by name. The sort mirrors the reference, which calls
// sort.Strings on the output of printmode.advertisedTools.
func toolDeclsFromRegistry(reg *tools.Registry) []backend.ToolDecl {
	names := reg.List()
	sort.Strings(names)
	decls := make([]backend.ToolDecl, 0, len(names))
	for _, n := range names {
		t, ok := reg.Get(n)
		if !ok {
			continue
		}
		decls = append(decls, backend.ToolDecl{Name: n, Parameters: t.Schema()})
	}
	return decls
}

// advertisedToolDecls is the declaration list the model sees.
//
// Cycle 293: the default agent sends the reference's twelve captured
// declarations (tools.ModelDeclarations), not the 60-name catalogue. The
// catalogue is the *available* surface print mode reports in its init event
// and the executor registry; the two were conflated until 1.4.4 was captured,
// which is why reference-shaped calls failed with `Tool read_file not found`.
//
// A --agent custom agent keeps its captured seven-tool surface, which is the
// registry's own filtered list.
func advertisedToolDecls(agentBody string, reg *tools.Registry) []backend.ToolDecl {
	if agentBody != "" {
		return toolDeclsFromRegistry(reg)
	}
	model := tools.ModelDeclarations()
	decls := make([]backend.ToolDecl, 0, len(model))
	for _, d := range model {
		decls = append(decls, backend.ToolDecl{
			Name:        d.Name,
			Description: d.Description,
			Parameters:  d.Parameters,
		})
	}
	return decls
}

// registryRunner adapts a tools.Registry to agent.ToolRunner.
type registryRunner struct{ reg *tools.Registry }

func (r *registryRunner) Run(ctx context.Context, call backend.ToolCall) (string, error) {
	return r.reg.Dispatch(ctx, call.Name, call.Arguments)
}

// permissionRunner gates every tool call through the recovered permission
// policy. In headless print mode there is nobody to prompt, so a call the
// policy does not allow is soft-denied: the model receives the reference's
// denial text instead of a result, and the call is recorded so the run can
// report it at the end.
//
// With the clone's default (allow) policy the gate is transparent; setting
// OPENAGY_PERMISSION_POLICY=strict restores the reference behaviour, where an
// otherwise-undecided call becomes an ask and therefore a soft-deny.
//
// Cycle 104: policy is a *pointer* so the interactive session can mutate its
// Mode in place when the user cycles modes with Shift+Tab; the gate then sees
// the new mode on the next tool call.
//
// Cycle 228 (AGY 1.2.10 close-gap): the runner also carries a Sandbox. When
// the sandbox is enabled, run_command is re-evaluated against the allow-list
// (cwd + artifact dir + scratch dir) even when the permission policy would
// otherwise allow it. An out-of-bounds path lands the call in the soft-deny
// path; a per-call bypass lifts the check for one invocation only — the next
// call goes back to the sandbox-first contract. See internal/sandbox for the
// carve-out details.
type permissionRunner struct {
	inner   agent.ToolRunner
	policy  *permissions.Policy
	sandbox sandbox.Sandbox
	Denials []string
}

func (r *permissionRunner) Run(ctx context.Context, call backend.ToolCall) (string, error) {
	target := toolTarget(call)
	// Cycle 228: the sandbox check runs first, before the policy.
	// If the command touches an out-of-bounds path we deny with the
	// sandbox-specific reason even when the policy would have
	// allowed — the terminal sandbox is the stricter of the two
	// gates, so its verdict wins. When the sandbox is satisfied
	// (or disabled) the policy has the final say.
	if r.sandbox.Enabled && (call.Name == "run_command" || call.Name == "run_shell_command") {
		if res := r.sandbox.CheckCommand(target); res.Outside {
			r.Denials = append(r.Denials, call.Name)
			return sandboxDeniedToolResult(call.Name, target, r.sandbox.AllowedPaths()), nil
		}
	}
	if v := r.policy.Decide(permissions.Request{Tool: call.Name, Target: target}); v == permissions.Allow {
		res, err := r.inner.Run(ctx, call)
		if err == nil {
			budget := tools.ApplyOutputBudget(res, "", tools.DefaultMaxResultTokens)
			res = budget.Content
		}
		return res, err
	}
	r.Denials = append(r.Denials, call.Name)
	return permissions.DeniedToolResult(call.Name, target), nil
}

// sandboxDeniedToolResult is the model-facing soft-deny text the gate
// returns when the terminal sandbox rejects a run_command call. It
// mirrors the recovered DeniedToolResult shape so a downstream caller
// (the agent loop's tool-result framing) treats it identically to a
// permission-policy denial; only the wording names the sandbox so
// the model can rephrase the command to stay inside the allow-list.
func sandboxDeniedToolResult(tool, target string, allowed []string) string {
	return "Sandbox blocked '" + tool + "' on '" + target + "': the terminal sandbox only permits commands that read or write the workspace, the agent's artifact directory, or the per-conversation scratch directory. Re-run with paths inside those, or run /bypass-sandbox in the TUI to lift the restriction for this call only."
}

// toolTarget extracts the path or command a call applies to, which is what the
// settings allow/deny rules match against.
func toolTarget(call backend.ToolCall) string {
	var p map[string]json.RawMessage
	if err := json.Unmarshal(call.Arguments, &p); err != nil {
		return ""
	}
	// Cycle 275: the reference's wire keys are capitalised (CommandLine,
	// AbsolutePath, TargetFile, …). Without them the sandbox and the policy
	// saw an empty target for every real call and waved it through.
	for _, key := range []string{
		"path", "command", "file", "target",
		"CommandLine", "AbsolutePath", "TargetFile", "DirectoryPath", "SearchPath",
	} {
		if raw, ok := p[key]; ok {
			var s string
			if json.Unmarshal(raw, &s) == nil {
				return s
			}
		}
	}
	return ""
}

// printModePolicy builds the permission policy for a print-mode session from
// the flags, the environment and settings.json.
func printModePolicy(fs *flag.FlagSet) permissions.Policy {
	// Cycle 283: the reference seeds the session's execution mode from
	// `agentMode` in the store's settings.json ("auto" on a store whose
	// user last chose Auto), and only `--mode` overrides it. The clone
	// ignored the key, so it started in the default mode while the
	// reference started in auto — visible as the missing "auto ·" status
	// prefix and as a different permission gate on the very first call.
	mode := flagStringValue(fs, "mode")
	if mode == "" {
		if v := auth.SettingsString("agentMode"); isKnownMode(v) {
			mode = v
		}
	}
	p := permissions.Policy{
		Mode:            mode,
		SkipPermissions: flagBoolValue(fs, "dangerously-skip-permissions"),
		Strict:          permissions.StrictFromEnv(os.Getenv("OPENAGY_PERMISSION_POLICY")),
	}
	if settings, err := permissions.LoadRules(filepath.Join(auth.AntigravityHome(), "settings.json")); err == nil {
		p.Allow = settings.Allow
		p.Deny = settings.Deny
	}
	if p.SkipPermissions {
		printModeSilenceLogger.Println(permissions.SkipPermissionsNotice)
	}
	return p
}

// isKnownMode keeps a malformed settings.json from putting an arbitrary
// string into the status line and the permission gate.
func isKnownMode(v string) bool {
	switch v {
	case permissions.ModeAuto, permissions.ModePlan, permissions.ModeAcceptEdits:
		return true
	}
	return false
}

// printModeSandbox builds the terminal sandbox for a print-mode session.
// The sandbox is enabled when `enableTerminalSandbox` is set in
// settings.json (boolean form, the v4 binary's schema). The same escape
// hatches that disable the policy also disable the sandbox:
//
//   - OPENAGY_DISABLE_SANDBOX=1 — dev override, no user-facing prompt
//   - --dangerously-skip-permissions — full-permission mode
//
// conversationID is the per-conversation id the print-mode turn is
// associated with; an empty id means no scratch dir is added to the
// allow-list.
func printModeSandbox(fs *flag.FlagSet, workspace, conversationID string) sandbox.Sandbox {
	// Skip the sandbox entirely when the user has asked for the
	// unrestricted mode, OR when the dev override env var is set.
	// This mirrors the cycle 228 contract: the sandbox is the
	// stricter of the two gates, so turning it off is intentional
	// and observable in the user's flag set.
	if flagBoolValue(fs, "dangerously-skip-permissions") {
		return sandbox.Sandbox{Enabled: false, Cwd: workspace}
	}
	if os.Getenv("OPENAGY_DISABLE_SANDBOX") == "1" {
		return sandbox.Sandbox{Enabled: false, Cwd: workspace}
	}
	enabled := false
	if v, ok := auth.SettingsBool("enableTerminalSandbox"); ok {
		enabled = v
	}
	return sandbox.Resolve(enabled, workspace, auth.AntigravityHome(), conversationID)
}

// pickBackend implements the cycle 33 + cycle 40 + cycle 44 dispatch.
//
// Cycle 33 path: NewRealClient if the model is in the registry, NewMock
// otherwise. The --model-config flag (or $OPENAGY_MODELS_CONFIG) controls
// the registry path; if neither is set, we go straight to mock so the
// binary stays runnable in a clean sandbox.
//
// Cycle 40 addition: $OPENAGY_BACKEND is a top-level override. When set
// to "agw" we bypass the registry and return a direct agw.k0s.io Client
// (internal/backend/agw_k0s.go). This is the dev-sandbox-friendly way to
// route `--print` to a real LLM without writing a models.json file.
// Other values (or unset) preserve cycle 33 behaviour, so mock is
// always the safe fallback.
//
// Cycle 44 (issue #2): the binary now defaults to "agw" instead of
// mock. The dispatch order is:
//
//  1. --backend flag  -> explicit override ("agw" | "shim")
//  2. $OPENAGY_BACKEND -> top-level env override (preserved for ops)
//  3. default          -> "agw" (cycle 44 change)
//
// If the resolved value is "agw" but no OAuth token is on disk
// (neither $JETSKI_OAUTH_TOKEN nor
// ~/.config/antigravity/oauth-token), we print a warning and
// auto-fall back to "shim" so the binary stays runnable in a
// sandboxed / first-launch environment.
//
// Dispatch order (highest priority first):
//  1. --backend flag        -> explicit (cycle 44)
//  2. $OPENAGY_BACKEND      -> env (cycle 40)
//  3. default "agw"         -> cycle 44 default
//  4. agw + no token        -> shim fallback (cycle 44 warning)
func pickBackend(fs *flag.FlagSet, modelName string) backend.Client {
	// Cycle 279: the interactive backend used to take the agw client's
	// default 60-second *total* per-request timeout. That cap is on the
	// whole HTTP exchange, so a single long model response (deep thinking,
	// a long answer) was cut at 60 s with no way to raise it — the same
	// failure the TUI's turn deadline caused, one layer lower. The
	// headless path (main.go's runPrint) already passes
	// AgwTimeoutUnlimited for exactly this reason.
	//
	// A stalled stream is still bounded: piagentbridge's native provider
	// runs the reference's 5-minute idle watcher
	// (piagentbridge.StallTimeout, AGY 1.3.6 "5-minute stall detection"),
	// which fires only when the gateway stops sending frames, not on
	// elapsed time alone.
	return pickBackendWithLog(fs, modelName, io.Discard, backend.AgwTimeoutUnlimited)
}

// pickBackendWithLog is the underlying dispatch with a
// caller-supplied log writer. Both the live TUI and the --print
// path pass io.Discard: `strings ~/.local/bin/agy | grep "\[backend\]"`
// returns nothing, the reference binary does not log the resolved
// backend on startup. The logWriter parameter is preserved for
// tests that want to assert which backend was picked without
// scraping bubbletea output.
//
// Cycle resume-continuation-r2 (this revision): the prior cycle
// routed the live TUI through os.Stderr and only silenced --print.
// REF does neither: a 1.4.0 launch with a previous conversation
// in the workspace prints the "Resume with -c (or command below):"
// hint banner (see resumeHintBanner()), nothing else. The [backend]
// log line was speculative cycle 40 op-visibility scaffolding
// that should never have shipped. Removed.
//
// agwTimeout is the agw transport's per-request timeout: 0 keeps the client's
// 60s default (the interactive path), backend.AgwTimeoutUnlimited disables it
// (the headless path, whose reference default is unlimited).
func pickBackendWithLog(fs *flag.FlagSet, modelName string, logWriter io.Writer, agwTimeout int) backend.Client {
	resolved, fromFlag := resolveBackend(fs, logWriter)
	// logWriter is preserved for test injection; both production
	// callers (live TUI, --print) now pass io.Discard after the
	// cycle resume-continuation-r2 removal of the speculative
	// [backend] startup log line. See pickBackend above.
	_ = logWriter
	// Whether the family was chosen by the user or fell through to the
	// default. The default has to follow the model: an official Gemini or
	// Claude slug lives on CloudCode, MiniMax on agw.
	explicit := fromFlag || strings.TrimSpace(os.Getenv("OPENAGY_BACKEND")) != ""
	switch resolved {
	case "cloudcode":
		return newOfficialBackend(modelName)
	case "agw":
		if !explicit && isOfficialModel(modelName) {
			return newOfficialBackend(modelName)
		}
		return backend.NewAgwK0sWithConfig(backend.AgwK0sConfig{TimeoutSeconds: agwTimeout})
	case "shim":
		return backend.NewMock()
	}
	// Should be unreachable: resolveBackend returns a canonical
	// "agw", "cloudcode" or "shim". Defensive fallback in case a future
	// change adds a third value without updating this switch.
	return backend.NewMock()
}

// isOfficialModel reports whether name resolves to a model the reference
// serves through CloudCode (Gemini, Claude, GPT-OSS) rather than the agw
// gateway's MiniMax.
func isOfficialModel(name string) bool {
	slug := resolveCatalogueID(name)
	return slug != "" && !isMiniMax(slug)
}

// newOfficialBackend builds the CloudCode transport for one catalogue model.
// The wire id is the catalogue slug; an empty lookup falls back to the name
// itself so a future model still reaches the right endpoint.
func newOfficialBackend(modelName string) backend.Client {
	slug := resolveCatalogueID(modelName)
	if slug == "" {
		slug = strings.TrimSpace(modelName)
	}
	return backend.NewCloudCodeWithConfig(backend.CloudCodeConfig{Model: slug})
}

// resolveBackend decides which backend to use. It honours the
// --backend flag, then the $OPENAGY_BACKEND env, then the
// "agw" default. The OAuth-token check is layered on top: if
// the resolved value is "agw" but no OAuth token is present, we
// silently downgrade to "shim" and print a one-line warning
// (so first-launch users see a clear fallback message rather
// than a confusing 401 from the gateway).
//
// Returns (resolved, fromFlag) where fromFlag is true iff the
// explicit --backend flag was set by the user (used by the
// caller to decide which log prefix to use).
//
// Cycle 50 sub-agent A: the warning is routed to logWriter so
// the --print path (which passes io.Discard) is silent on
// stderr, byte-level matching the v1.3.5 binary.
func resolveBackend(fs *flag.FlagSet, logWriter io.Writer) (string, bool) {
	raw := ""
	fromFlag := false
	source := "default"
	if v := flagStringValue(fs, "backend"); v != "" {
		raw = v
		fromFlag = true
		source = "--backend"
	} else if v := strings.ToLower(strings.TrimSpace(os.Getenv("OPENAGY_BACKEND"))); v != "" {
		raw = v
		source = "OPENAGY_BACKEND"
	}
	resolved := normaliseBackend(raw, logWriter)
	if (resolved == "agw" || resolved == "cloudcode") && !hasOAuthToken() {
		// No token, no real backend. The user is either on a
		// fresh install (login flow not run yet) or running
		// headless / CI. We print a one-liner and downgrade
		// to "shim" so the binary stays runnable.
		fmt.Fprintf(logWriter, "[warn] %s=%s requested but no OAuth token found (need $JETSKI_OAUTH_TOKEN or ~/.config/antigravity/oauth-token); falling back to shim backend\n", source, resolved)
		resolved = "shim"
	}
	return resolved, fromFlag
}

// normaliseBackend maps the various accepted values to the
// canonical "agw" or "shim". Unknown values are warned and
// returned as-is so the caller can surface them.
//
// Cycle 50 sub-agent A: the warning is routed to logWriter so
// --print is silent.
func normaliseBackend(raw string, logWriter io.Writer) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "":
		// Default. Cycle 44 changes the head-line default
		// from mock/shim to agw (issue #2: users no longer
		// need to set $OPENAGY_BACKEND=agw to talk to a
		// real LLM).
		return "agw"
	case "agw", "agw.k0s.io", "agw_k0s", "real", "k0s":
		return "agw"
	case "cloudcode", "cloud-code", "cloud_code", "google", "gemini", "cc":
		return "cloudcode"
	case "shim", "mock", "off", "false", "test":
		return "shim"
	default:
		fmt.Fprintf(logWriter, "[warn] unknown backend %q; expected agw|cloudcode|shim (falling back to default 'agw')\n", raw)
		return "agw"
	}
}

// hasOAuthToken returns true if either $JETSKI_OAUTH_TOKEN is
// set (non-empty) or a token file exists at the conventional
// path. We use os.Stat for the file (cheap; doesn't read
// the contents — auth/LoadAntigravityToken validates the
// JSON).
func hasOAuthToken() bool {
	if v := strings.TrimSpace(os.Getenv("JETSKI_OAUTH_TOKEN")); v != "" {
		return true
	}
	// Cycle 46 fix: cascade 5 path + ANTIGRAVITY_REFRESH_TOKEN env
	// match auth.LoadTokenUnified 1:1 so user doesn't fall back
	// to shim when the token really is on disk.
	if v := strings.TrimSpace(os.Getenv("ANTIGRAVITY_REFRESH_TOKEN")); v != "" {
		return true
	}
	// Token file paths (cascade priority 1-4, same as
	// auth.LoadTokenUnified):
	//   1. $HOME/.config/antigravity/oauth-token (v1.3.5 binary)
	//   2. $HOME/.config/antigravity/antigravity-auth.json (v3)
	//   3. $HOME/.gemini/antigravity-cli/antigravity-oauth-token (v4)
	//   4. $ANTIGRAVITY_HOME/antigravity-oauth-token (recovered 之前)
	candidates := []string{
		filepath.Join(authHome(), "oauth-token"),
		filepath.Join(authHome(), "antigravity-auth.json"),
		auth.TokenFilePath(),
	}
	for _, p := range candidates {
		if p == "" {
			continue
		}
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() && fi.Size() > 0 {
			return true
		}
	}
	// Check OS Keychain (macOS: service "gemini", account "antigravity" - 1:1 agy parity)
	if auth.HasKeychainToken() {
		return true
	}
	return false
}

// authHome returns $XDG_CONFIG_HOME/antigravity or
// $HOME/.config/antigravity. Used to locate the legacy
// oauth-token file the parse-parity shell also reads.
func authHome() string {
	if h := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); h != "" {
		return filepath.Join(h, "antigravity")
	}
	if h := strings.TrimSpace(os.Getenv("HOME")); h != "" {
		return filepath.Join(h, ".config", "antigravity")
	}
	return ""
}

// dispatchTool is the --tool NAME --tool-args JSON path. Only `run_command`
// has a real implementation here; every other tool is a stub that prints
// its name and args. The stub list matches the recoveredTools catalog so
// callers see the same names whether they hit --tools or --tool.
func dispatchTool(w io.Writer, name, argsJSON string) error {
	// Parse the args (we accept JSON object or null).
	var args map[string]any
	if argsJSON == "" {
		argsJSON = "{}"
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return fmt.Errorf("tool: invalid --tool-args JSON: %w", err)
	}

	switch name {
	case "run_command":
		// Real os/exec — the only tool that touches the host in this
		// reconstruction. Other tools are stubs so the sandbox stays
		// hermetic.
		//
		// AGY 1.3.3 changelog line 47: on Windows the shell is resolved
		// via tools.SelectWindowsShell, which probes powershell.exe
		// first and falls back to pwsh.exe (PowerShell 7+) when the
		// legacy 5.1 shell is not on PATH. On macOS/Linux the existing
		// /bin/sh -c path is preserved (a no-op for SelectWindowsShell).
		cmdStr, _ := args["command"].(string)
		if cmdStr == "" {
			return fmt.Errorf("run_command: --tool-args must include {\"command\": \"...\"}")
		}
		// Cycle 276: use the same per-OS default shell the agent path uses
		// (zsh on macOS, bash on Linux, powershell/pwsh on Windows) instead
		// of the old hard-coded `sh -c`.
		argv := tools.DefaultShellArgv(cmdStr)
		if runtime.GOOS == "windows" && tools.SelectWindowsShell() == "" {
			return fmt.Errorf("run_command: no powershell found on PATH (looked for powershell.exe, then pwsh.exe)")
		}
		c := exec.Command(argv[0], argv[1:]...)
		c.Stdout = w
		c.Stderr = w
		// Cycle 277: same pty-on-stdin disposition as the agent path, so
		// `--tool run_command` matches what the reference would do.
		return tools.RunWithPTYStdin(c)
	default:
		// Stub path. Print the tool name + a one-line echo of the args.
		fmt.Fprintf(w, "[tool-stub] %s args=%v\n", name, args)
		return nil
	}
}

// printTools writes the 32-tool catalog to w in the v4 binary format.
func printTools(w io.Writer) {
	fmt.Fprintf(w, "Recovered tools (%d):\n", len(recoveredTools))
	for i, t := range recoveredTools {
		fmt.Fprintf(w, "  %2d. %s\n", i+1, t)
	}
}

// printServices writes the gRPC service catalog to w.
func printServices(w io.Writer) {
	fmt.Fprintf(w, "Recovered gRPC services (%d):\n", len(recoveredServices))
	for i, s := range recoveredServices {
		fmt.Fprintf(w, "  %2d. %s\n", i+1, s)
	}
}

// printOAuthConfig writes the static OAuth config skeleton. The v4 binary
// runs a real device flow against cloudaicompanion; that endpoint is
// unreachable from this sandbox, so we print the config that *would* be
// used and exit 0.
func printOAuthConfig(w io.Writer) {
	cfg := map[string]any{
		"client_id":     "antigravity-cli",
		"client_secret": "<redacted>",
		"device_url":    "https://cloudaicompanion.googleapis.com/v1/oauth/device",
		"token_url":     "https://oauth2.googleapis.com/token",
		"scopes":        []string{"https://www.googleapis.com/auth/cloud-platform"},
		"redirect_uri":  "http://127.0.0.1:0/oauth/callback",
		"auth_method":   "consumer",
		"status":        "stub (sandbox: cloudaicompanion unreachable)",
		"issued_at":     time.Now().Format(time.RFC3339),
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(cfg)
}

// renderTUI prints the static TUI snapshot. Cycle 50: the v1.3.5 binary
// uses a single-line prompt + status row, NOT a 5-panel layout. The
// 5-panel layout was an earlier recovered design (cycles 32-49) that
// diverges from the v1.3.5 binary's Claude Code-style single-line
// prompt. The static snapshot here mirrors what tui_run.go renders
// when bubbletea is offline / not driven (--tui is a "show me the
// shell" flag, not the live TUI).
//
// Layout (v1.3.5 binary 1:1):
//
//	<5-row 24-bit RGB banner>
//	───── (200 col, dim 60;64;67)
//	> (蓝紫 138;180;248)         — prompt prefix
//	─────
//	? for shortcuts .........  Gemini 3.8 Flash · high  — status row
//
// The function is deterministic (no goroutines, no global state) and
// matches tui_run.go View() output byte-for-byte modulo the
// input-buffer slot.
func renderTUI(w io.Writer) error {
	// Same defaults the live TUI uses (cycle 47 tui_run.go).
	email := auth.EffectiveUserEmail()
	if email == "" {
		email = "navigaid@gmail.com"
	}
	model := "Gemini 3.8 Flash (High)"
	tier := "Antigravity Starter Quota"
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "/"
	}

	// 5-row banner (24-bit RGB, byte-for-byte match with v1.3.5 binary
	// state-1-idle.log).
	renderBanner(w, email, model, tier, cwd)
	// Separator (dim 60;64;67, 120 col).
	fmt.Fprintln(w, separator)
	// Prompt prefix: ">" 蓝紫 138;180;248 + 清色 reset.
	fmt.Fprintln(w, "\033[38;2;138;180;248m>\033[39m")
	fmt.Fprintln(w, separator)
	// Status row: "? for shortcuts" (灰 154;160;166) + pad + "<model> · high"
	fmt.Fprintln(w, staticStatusRow(model))
	return nil
}

// runOnboardingWizard runs the cycle 43 onboarding wizard (color
// scheme picker + terms + opt-in checkbox) before the main TUI
// drops in. The v1.3.5 binary shows this on first launch; on
// subsequent launches the binary reads admin_settings.json and
// short-circuits. In this recovered parse-parity shell we always
// show the wizard on the no-flag path so the 1:1 verification can
// observe the rendered screens. The wizard is bypassed when
// --no-onboarding is set (used by the parse-parity CI smoke tests).
//
// On a non-tty stdin (e.g. under `script` capture) we still run the
// wizard so the cycle 43 deliverable can render the screens into
// the captured log; the headless Run() advances the FSM to StepDone
// without prompting for input. This matches the v1.3.5 binary's
// behaviour when stdin is redirected.
func runOnboardingWizard(w io.Writer) error {
	wz := onboarding.NewWizard()
	wz.Out = w
	return wz.Run()
}

// tuiSnapshot was the cycle-32 5-panel layout printed by --tui. The
// v1.3.5 binary's actual --tui output is the single-line prompt + 状态行
// rendered by renderTUI() (above). This constant is kept for any
// external test/import that still references the historical name; the
// returned string is the v1.3.5-binary 1:1 render (24-bit RGB banner
// + separator + prompt + status row).
func tuiSnapshot() string {
	var sb strings.Builder
	renderTUI(&sb)
	return sb.String()
}
