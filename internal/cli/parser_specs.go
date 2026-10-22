// Static v1.3.5-binary specs consumed by NewParser / ParseForTest.
//
// Cycle 38: switched from "v1.3.4 / 31 flag / 10 subcommand" to
// v1.3.5 actual --help (21 flag / 12 subcommand). The cycle 33
// inventory was a guess extrapolated from the ghidra symbol table;
// the actual --help list is much smaller.
//
// We re-extracted the spec from `/workspace/agy/install/antigravity --help`
// on 2026-10-14 (md5 b7f7ed7c66bc835ee21bd9b7304b8f74, version "1.3.5").
//
// Cycle 41: the 12 v1.3.5-visible subcommands stay 1:1 in --help
// (the v1.3.5 binary's 39-line help output is the parse-parity
// contract). We add 10 hidden subcommands (version, login, logout,
// auth, chat, acp, http, hooks, migrate, stats) that wire to the
// runXxx handlers in dispatch.go. The hidden flag keeps --help at
// 39 lines; the new subcommands are invokable as
// `antigravity <name>` and route to real internal/ handlers.
package cli

import (
	"context"
	"io"
	"os"
)

// StandardFlags returns the 21 top-level flags emitted by the v1.3.5
// binary's `--help`. Order, type, short alias and aliases match 1:1.
// The "Def" defaults also match. The 22nd entry is the recovered
// hidden `-v N` int flag (cycle 101); the 23rd is the hidden
// `--verbose` bool flag added by the
// headless-jsonl-mode-trailing-newline cycle (AGY 1.2.7 — emits the
// `assistant_message` event in `--output-format stream-json`). Both
// are Hidden, so the visible --help column stays at 21.
//
// v1.3.5 binary --help (parity with /workspace/agy/install/antigravity):
//
//	--add-dir                       stringSlice (default [])
//	--agent                         string
//	-c                              bool   (alias for --continue)
//	--continue                      bool
//	--conversation                  string
//	--dangerously-skip-permissions  bool
//	--disable-slash-commands        bool
//	--effort                        string
//	-i                              string (alias for --prompt-interactive)
//	--input-format                  string (default text)
//	--json-schema                   string
//	--log-file                      string
//	--mode                          string
//	--model                         string
//	--new-project                   bool
//	--output-format                 string (default text)
//	-p                              string (alias for --print)
//	--print                         string
//	--print-partial-response        bool
//	--print-timeout                 duration (default 0s)
//	--project                       string
//	--prompt                        string (alias for --print)
//	--prompt-interactive            string
//	--remote-control                bool
//	--sandbox                       bool
//
// Cycle 101 correction: the recovered 1.3.6 binary treats -p/--print/--prompt
// and -i/--prompt-interactive as *string* flags whose value is the prompt, not
// as booleans with a positional prompt. Evidence (1.3.6):
//
//	$ agy -p
//	flag needs an argument: -p                      (stdlib flag, rc 2)
//	$ agy -p hi extra
//	Error: unexpected argument "extra".             (rc 2)
//	$ agy -p --agent nosuchagent hi
//	Error: -p took "--agent" as its prompt, …       (rc 2)
//	$ agy -p /clear --disable-slash-commands
//	(the slash rejection is suppressed — the flag after the prompt is parsed)
//
// Positional prompts are rejected outright: "Prompts are read only from
// -p/--print, -i/--prompt-interactive, or stdin".
//
// `-v` is a hidden int flag (it needs an argument and parses via strconv.Atoi:
// `agy -v foo` → `invalid value "foo" for flag -v: strconv.Atoi: parsing "foo":
// invalid syntax`). It is absent from --help, so it is registered Hidden.
func StandardFlags() []FlagSpec {
	return []FlagSpec{
		{Name: "add-dir", Type: "stringSlice", Def: "[]", Usage: "Add a directory to the workspace (repeatable)"},
		{Name: "agent", Type: "string", Usage: "Agent for the current CLI session"},
		{Name: "c", Type: "bool", Usage: "Short alias for --continue"},
		{Name: "continue", Type: "bool", Usage: "Continue the most recent conversation"},
		{Name: "conversation", Type: "string", Usage: "Resume a previous conversation by ID"},
		{Name: "dangerously-skip-permissions", Type: "bool", Usage: "Auto-approve all tool permission requests without prompting"},
		{Name: "disable-slash-commands", Type: "bool", Usage: "Disable slash command and skill expansion in print mode"},
		{Name: "effort", Type: "string", Usage: "Reasoning effort for the current CLI session (low|medium|high|xhigh|max)"},
		{Name: "i", Type: "string", Usage: "Short alias for --prompt-interactive"},
		{Name: "input-format", Type: "string", Def: "text", Usage: "Input format for print mode (text, stream-json). stream-json reads one NDJSON message per line from stdin and runs a turn for each; it requires --output-format stream-json"},
		{Name: "json-schema", Type: "string", Usage: "Optional JSON schema string or path to a schema file to enforce structured output (for stream-json, only applicable to the final result)"},
		{Name: "log-file", Type: "string", Usage: "Override CLI log file path"},
		{Name: "mode", Type: "string", Usage: "Set the agent execution mode for this session (" + ModeList() + ")"},
		{Name: "model", Type: "string", Usage: "Model for the current CLI session"},
		{Name: "new-project", Type: "bool", Usage: "Create a new project for this session"},
		{Name: "output-format", Type: "string", Def: "text", Usage: "Output format for print mode (text, json, stream-json)"},
		{Name: "p", Type: "string", Usage: "Short alias for --print"},
		{Name: "print", Type: "string", Usage: "Run a single prompt non-interactively and print the response"},
		// Cycle headless-partial-response-flag (AGY 1.2.10 close-gap): the
		// reference added a boolean that makes -p/--print stream each chunk
		// of the model's output to stdout as soon as it arrives, instead of
		// buffering the whole answer until the turn completes. For text
		// format the behaviour is unchanged (text is already streamed);
		// for json/stream-json the buffered response would normally only
		// surface inside the final envelope, and the flag gives earlier
		// feedback on long-running turns. The default is `false` to keep
		// the byte-level match against the reference's 1.2.10 capture
		// (where the flag was new and unset).
		{Name: "print-partial-response", Type: "bool", Usage: "Stream each chunk of the model's output to stdout in headless mode as it arrives instead of buffering until the turn completes"},
		{Name: "print-timeout", Type: "duration", Def: "0s", Usage: "Optional time limit for print mode; 0 waits until the turn completes"},
		{Name: "project", Type: "string", Usage: "Project ID or project name for the current CLI session"},
		{Name: "prompt", Type: "string", Usage: "Alias for --print"},
		{Name: "prompt-interactive", Type: "string", Usage: "Run an initial prompt interactively and continue the session"},
		{Name: "remote-control", Type: "bool", Usage: "Create a remote connection for the CLI session on start up"},
		{Name: "sandbox", Type: "bool", Usage: "Run in a sandbox with terminal restrictions enabled"},
		// Cycle 101: hidden recovered flag. `-v N` is accepted by the 1.3.6
		// binary (strconv.Atoi validation) but is absent from --help. We
		// register it so `agy -v 2` and `agy -v foo` reproduce the
		// reference's accept/reject behaviour; the value is otherwise
		// unused (the reference's own use of it is unrecovered).
		{Name: "v", Type: "int", Hidden: true, Usage: ""},
		// Cycle headless-jsonl-mode-trailing-newline: --verbose expands the
		// `--output-format stream-json` event stream with an extra
		// `assistant_message` event that carries the full assistant
		// text after every model turn. AGY 1.2.7 added this flag; the
		// AGY 1.2.7 changelog line documents the matching bug fix
		// "headless mode not emitting a final newline after the
		// assistant message's trailing JSON event" — every event,
		// including the new `assistant_message`, must end with
		// `\n` so the next shell prompt does not glue to the last
		// byte of stdout. Hidden so the v1.3.5 --help column count
		// stays unchanged.
		{Name: "verbose", Type: "bool", Hidden: true, Def: "false", Usage: "Print extra stream-json events (assistant_message) in --output-format stream-json"},
	}
}

// ExtraFlags returns the cycle-33+ openagy-specific flags that are NOT
// part of the v4 binary 1:1 surface.
//
// Cycle 44 (issue #2): adds --backend so users no longer have to set
// $OPENAGY_BACKEND=agw to talk to a real LLM by default. Accepted
// values are `agw` (real agw.k0s.io backend) or `shim` (mock). When
// the flag is unset AND $OPENAGY_BACKEND is unset, the binary now
// defaults to "agw" (cycle 44 head-line behaviour change) and
// auto-falls back to "shim" if no OAuth token is present.
//
// Cycle 54: --legacy-5panel + --model-config + --no-onboarding removed.
// The legacy 5-panel layout has been deleted; --model-config is
// unused (model registry lives in $OPENAGY_MODELS_CONFIG env only);
// --no-onboarding is implicit since runRoot already gates the wizard
// on jetbox_state.json (cycle 51).
//
// Cycle 125: --trust-cwd added as a test hook for the on-startup
// workspace-trust gate (the dialog the reference shows when cwd is
// not in settings.json's trustedWorkspaces list). The same effect
// is available via $OPENAGY_TRUST=1 — see cmd/wagyu/trust_prompt.go.
func ExtraFlags() []FlagSpec {
	return []FlagSpec{
		{Name: "backend", Type: "string", Hidden: true, Usage: "Backend type: agw (real) or shim (mock). Default: agw (auto-falls back to shim if no OAuth token)"},
		// Cycle 70: --alt-screen keeps the program in the alternate screen buffer (default true; matches
		// v1.3.5 binary which always uses alt-screen). The flag is here so users can disable it for
		// non-TTY / non-alt-screen-aware contexts (e.g. recording sessions) and so the runtime toggle
		// (Ctrl+L in runMode.Update) can mirror the same bool back into the flag set.
		{Name: "alt-screen", Type: "bool", Def: "true", Hidden: true, Usage: "Use the alternate screen buffer (default true; toggle at runtime via Ctrl+L)"},
		{Name: "no-alt-screen", Type: "bool", Def: "false", Hidden: true, Usage: "Disable alt-screen at startup (overrides --alt-screen; runtime toggle still works)"},
		// Cycle 169: mouse tracking is unconditional in alt-screen mode.
		// The reference binary always emits DECSET 1002+1006 so wheel
		// events scroll the transcript AND the in-app text-selection
		// overlay (mouse-down → drag → release) can copy the selected
		// range into the clipboard via OSC52. There is no --mouse /
		// --no-mouse / /mouse command; the missing terminal-native
		// fallback the cycle-166 toggle provided is replaced by the
		// in-app selection itself.
		// Cycle 125: skip the on-startup workspace-trust dialog. The
		// reference shows the dialog the first time it is launched
		// from a directory that isn't already in the trusted list;
		// the flag (or $OPENAGY_TRUST=1) is for tests + scripted use
		// where the gate would otherwise block the TUI from starting.
		{Name: "trust-cwd", Type: "bool", Def: "false", Hidden: true, Usage: "Skip the on-startup workspace-trust dialog (or set $OPENAGY_TRUST=1)"},
		// AGY 1.2.13 changelog: "Fixed the --max-thinking-budget flag
		// accepting a 0 value, which caused infinite thinking loops that
		// locked up the CLI until Ctrl-C; values <= 0 now disable
		// thinking entirely, matching the documented semantics."
		//
		// int type (parsed with strconv.Atoi like the cycle-101 hidden
		// `-v` int), Def "0" so the flag's zero value matches the new
		// disabled-thinking semantic. Hidden because it is not in the
		// v1.3.5 --help visible surface — the reference accepts it but
		// documents it out-of-band. See internal/cli/thinking_budget.go
		// for the single source of truth on the <= 0 gate.
		{Name: "max-thinking-budget", Type: "int", Def: "0", Hidden: true, Usage: "Cap thinking tokens per turn (integer); values <= 0 disable thinking entirely"},
		// Cycle 236: --diff-with-agy prints the feature-parity table
		// sourced from internal/cli/testdata/feature_diff_table.json.
		// Hidden because it is not in the v1.4.2 --help surface; the
		// mirror slash command /feature-diff covers the TUI side.
		// Pass --output-format json alongside it for raw JSON.
		{Name: "diff-with-agy", Type: "bool", Def: "false", Hidden: true, Usage: "Print AGY 1.4.2 feature parity (aligned / gap / diverged)"},
		{Name: "debug", Type: "bool", Def: "false", Hidden: true, Usage: "Enable verbose debug logging"},
		// Cycle rc-ws-transport: --remote-control-tcp boots the
		// JSON-RPC daemon over a TCP+WebSocket transport on the
		// given bind address (default 127.0.0.1:0 — tests pick a
		// free port; production usually wants 0.0.0.0:7351 to
		// match agy's reference). When set, replaces the per-turn
		// Unix-socket listener that --remote-control would have
		// opened on its own. Hidden because it is not in the v1.3.5
		// --help surface; the --remote-control flag the cycle-230
		// daemon wired is unchanged.
		//
		// The string type is what the cycle plan asked for
		// (--remote-control-tcp=<addr>). Empty string is treated
		// as "flag absent" by the host helper, which falls back
		// to the Unix-socket transport — same semantic as
		// --remote-control being unset.
		{Name: "remote-control-tcp", Type: "string", Def: "", Hidden: true, Usage: "Bind addr (host:port) for the WebSocket JSON-RPC transport; empty disables. Replaces --remote-control when set"},
		// Cycle rc-polish (fresh-eyes G13): --remote-control-socket
		// was already read by cmd/wagyu/remote_control.go:99 but
		// never registered as a flag, so the read was effectively
		// dead code (flagStringValue returns "" when the flag is
		// not registered). Registering it here makes that read
		// resolve to a real value when the user passes the flag,
		// and the existing HostConfig.SocketPath seam in
		// internal/remotecontrol/host.go lets the flag actually
		// pick the bind path. Hidden for symmetry with
		// --remote-control-tcp; not in the v1.4.2 --help
		// surface. $OPENAGY_RC_SOCKET still wins when both are
		// set, matching the existing seam convention.
		{Name: "remote-control-socket", Type: "string", Def: "", Hidden: true, Usage: "Override the unix socket path for --remote-control. $OPENAGY_RC_SOCKET still wins"},
		// Cycle rc-webrtc-mesh-defer: --experimental-rc-v2 is the
		// forward-compat flag that lets a dashboard / operator opt
		// into knowing the daemon is on the legacy TCP+WS path
		// rather than the deferred v2 mesh path (WebRTC mesh +
		// Antigravity Hub; both parked — see
		// docs/deferred-rc-transport.md). The flag is *accepted*
		// cleanly today (no-op — we never enable v2 features
		// behind it), but the deferred_features envelope on
		// RemoteControlStatus is the canonical signal a dashboard
		// should poll instead. Hidden for parity with the other
		// experimental / v2 shapes: not in the v1.4.2 --help
		// surface, not advertised anywhere user-facing. Future
		// rc-webrtc-mesh / rc-hub-server cycles either wire
		// semantics behind this flag (when --experimental-rc-v2
		// is true AND the deferred_features entry flips to true)
		// or remove the flag entirely once v2 is the default.
		{Name: "experimental-rc-v2", Type: "bool", Hidden: true, Usage: "Opt-in to the deferred v2 (WebRTC mesh / Hub) features. No-op today; reserved for forward compat."},
	}
}

// StandardSubcommands returns the 12 subcommands the v1.3.5 binary lists
// in `agy --help`:
//
//	agent, agents, changelog, help, install, mcp, mic-serve, models,
//	plugin, plugins, remote-control, update
//
// `agent` and `agents` share one implementation (both print the same
// "Usage: <prog> agent [flags]" block), and `plugins` is an alias of
// `plugin`.
//
// The visible surface is wired to the exact v1.3.5 usage blocks and
// Run handlers in subcommand_parity.go. A separate set of Hidden openagy-only
// subcommands (login, logout, auth, chat, acp, http, hooks, migrate,
// stats) routes to the recovered internal/ packages in dispatch.go;
// they are not part of the v1.3.5 surface and stay out of --help.
//
// `agy version` is deliberately NOT a subcommand: the v1.3.5 binary
// treats it as an unexpected argument, so we let the top-level
// unknown-argument path handle it.
func StandardSubcommands() []SubcommandSpec {
	// wrapRunXxx builds a SubcommandSpec.Run closure that calls
	// dispatch.go's runXxx handler for the hidden openagy subcommands.
	wrapRunXxx := func(name string) func(w io.Writer, args []string) error {
		return func(w io.Writer, args []string) error {
			return dispatchRunXxx(name, w, os.Stderr, context.Background(), nil, args)
		}
	}

	pluginSubs := []SubSubSpec{
		{Name: "list", Short: "List installed plugins", RunJ: runPluginList},
		{Name: "import", Short: "Import plugins from gemini or claude", RunJ: runPluginImport},
		{Name: "install", Short: "Install a plugin", RunJ: runPluginInstall},
		{Name: "uninstall", Short: "Uninstall a plugin", RunJ: runPluginUninstall},
		{Name: "enable", Short: "Enable a plugin", RunJ: runPluginToggle("enable")},
		{Name: "disable", Short: "Disable a plugin", RunJ: runPluginToggle("disable")},
		{Name: "validate", Short: "Validate a plugin", RunJ: runPluginValidate},
		{Name: "link", Short: "Generate link to a marketplace", RunJ: runPluginLink},
		{Name: "help", Short: "Show plugin command help", RunJ: runPluginHelp},
	}

	mcpSubs := []SubSubSpec{
		{Name: "add", Short: "Add or update an MCP server configuration",
			RunJ: runMCPAdd, HelpFn: usageMCPAdd},
		{Name: "remove", Short: "Remove an MCP server configuration",
			RunJ: runMCPRemove, HelpFn: func(n string) string {
				return usageMCPSimple(n, "remove", "Remove an MCP server configuration", "name")
			}},
		{Name: "list", Short: "List all configured MCP servers",
			RunJ: runMCPList, HelpFn: func(n string) string {
				return usageMCPSimple(n, "list", "List all configured MCP servers", "")
			}},
		{Name: "enable", Short: "Enable an MCP server",
			RunJ: runMCPSetEnabled(true), HelpFn: func(n string) string {
				return usageMCPSimple(n, "enable", "Enable an MCP server", "name")
			}},
		{Name: "disable", Short: "Disable an MCP server",
			RunJ: runMCPSetEnabled(false), HelpFn: func(n string) string {
				return usageMCPSimple(n, "disable", "Disable an MCP server", "name")
			}},
	}

	rcSubs := []SubSubSpec{
		{Name: "start", Short: "Register and start the remote-control daemon",
			RunJ: runRCStart, HelpFn: usageRCStart},
		// Cycle rc-serve-subcommand: foreground twin of start.
		// --addr / --name / --no-login are the resolved argv
		// that runRCServe forwards to the cmd/wagyu-supplied
		// serveHandler (registered via cli.RegisterServeHandler).
		// serve rejects loopback binds the same way start does;
		// --no-login skips the inline OAuth device-code flow.
		{Name: "serve", Short: "Run the remote-control daemon in the foreground (OAuth login inline)",
			RunJ: runRCServe, HelpFn: usageRCServe},
		{Name: "status", Short: "Show the daemon's status",
			RunJ: runRCStatus, HelpFn: usageRCStatus},
		{Name: "stop", Short: "Stop the daemon and unregister it",
			RunJ: runRCStop, HelpFn: usageRCStop},
	}

	return []SubcommandSpec{
		{
			Name:            "plugin",
			Short:           "Manage plugins (install, uninstall, list, enable, disable)",
			Subs:            pluginSubs,
			HelpFn:          usagePlugin,
			UnknownSubError: "Error: unknown command: %s",
			RunJ:            runPluginParent,
		},
		{
			Name:            "plugins",
			Aliases:         []string{"plugin"},
			Short:           "Alias for plugin",
			Subs:            pluginSubs,
			HelpFn:          usagePlugin,
			UnknownSubError: "Error: unknown command: %s",
			RunJ:            runPluginParent,
		},
		{
			Name:   "mcp",
			Short:  "Manage MCP servers (add, remove, list, enable, disable)",
			Subs:   mcpSubs,
			HelpFn: usageMCP,
			RunJ:   runMCPParent,
		},
		{
			Name:   "install",
			Short:  "Configure environment paths and shell settings",
			HelpFn: usageInstall,
			// RunJ (not Run) so the handler can write its setup/warning
			// block to the parser's ErrOut and its banner to Out — the
			// reference's stdout/stderr split.
			RunJ: func(j *JetskiCLI, args []string) error {
				return dispatchRunXxx("install", j.Out, j.ErrOut, context.Background(), nil, args)
			},
		},
		{
			Name:   "mic-serve",
			Short:  "Serve this machine's microphone to a CLI on another host",
			HelpFn: usageMicServe,
			RunJ:   runMicServe,
		},
		{
			Name:   "remote-control",
			Short:  "Manage the remote-control background daemon (start, status, stop)",
			Subs:   rcSubs,
			HelpFn: usageRemoteControl,
			RunJ:   runRCParent,
		},
		// Cycle rc-persistent-daemon: hidden subcommand the
		// LaunchAgent plist invokes.  NOT in --help output
		// (Hidden: true).  Reachable only via direct
		// invocation.  Defined here rather than in the
		// hidden list below because the parent subcommand
		// list is logically grouped around "things
		// related to remote-control".  The Run handler is
		// registered by cmd/wagyu's main via
		// cli.RegisterForegroundHandler so the cmd/wagyu/
		// package can stay out of internal/cli/.
		{
			Name:   "remote-control-foreground",
			Short:  "Foreground subcommand the persistent daemon invokes (hidden)",
			Hidden: true,
			RunJ:   runRCForegroundPlaceholder,
		},
		{
			Name:   "agent",
			Short:  "List available agents",
			HelpFn: usageAgent,
			RunJ:   runAgents,
		},
		{
			Name:    "agents",
			Aliases: []string{"agent"},
			Short:   "List available agents",
			HelpFn:  usageAgent,
			RunJ:    runAgents,
		},
		{
			Name:   "changelog",
			Short:  "Show changelog and release notes",
			HelpFn: usageChangelog,
			RunJ:   runChangelog,
		},
		{
			Name:  "update",
			Short: "Update CLI",
			// `update --help` is handled by the stdlib flag package in
			// the reference, which prints just "Usage of update:" and
			// exits 2.
			Help:     "Usage of update:\n",
			HelpExit: 2,
			Run:      RunUpdate,
		},
		{
			Name:   "models",
			Short:  "List available models",
			HelpFn: usageModels,
			RunJ:   runModelsParity,
		},
		{
			Name:  "help",
			Short: "Show help for subcommands",
			RunJ:  runHelpCommand,
		},
		// ---- Hidden openagy-only subcommands (not in --help) ----
		{Name: "login", Short: "Start the OAuth device-code login flow", Run: wrapRunXxx("login"), Hidden: true},
		{Name: "logout", Short: "Clear the stored OAuth token", Run: wrapRunXxx("logout"), Hidden: true},
		{Name: "auth", Short: "Set up the mTLS bypass proxy", Run: wrapRunXxx("auth"), Hidden: true},
		{Name: "chat", Short: "Launch the recovered TUI chat", Run: wrapRunXxx("chat"), Hidden: true},
		{Name: "acp", Short: "Start the Agent Client Protocol listener", Run: wrapRunXxx("acp"), Hidden: true},
		{Name: "http", Short: "Start the recovered HTTP server", Run: wrapRunXxx("http"), Hidden: true},
		{Name: "hooks", Short: "List configured lifecycle hooks", Run: wrapRunXxx("hooks"), Hidden: true},
		{Name: "migrate", Short: "Migrate legacy config to the v4 layout", Run: wrapRunXxx("migrate"), Hidden: true},
		{Name: "stats", Short: "Show on-disk session statistics", Run: wrapRunXxx("stats"), Hidden: true},
		{Name: "logo", Short: "Print the startup banner logo (hidden)", HelpFn: UsageLogo, Run: runLogoHandler, Hidden: true},
	}
}
