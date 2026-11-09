package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/justwasm/pigasus/internal/core/cliflags"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
)

// ---- parser.go ----

// Package cli builds the 1:1 jetski-cli-based parser used by cmd/wagyu.
//
// Cycle 38 rewrite: the v1.3.4 binary does not use cobra/pflag (zero
// `spf13` strings in the binary). Instead it uses two layers:
//   - Go stdlib `flag` for actual parsing
//   - internal `jetski/cli/...` (CLI engine + logfile + termio + steps)
//
// We reimplemented the jetski/cli surface in jetski.go and rewrote
// this file to use it. The public API (SubcommandSpec, SubSubSpec,
// FlagSpec, StandardFlags, StandardSubcommands, NewParser) is
// unchanged so callers in cmd/wagyu and tests do not need to
// change.
//
// The goal is still parse-parity with the reference binary's --help output:
//   - 31 top-level flags (long form, short form, and aliases)
//   - the reference's 12 visible subcommands (the clone adds six of its
//     own: accounts, halloween, model, provider, proxy, web)
//   - sub-subcommands under plugin (9), mcp (5), remote-control (3), agent (1)

// SubcommandSpec, SubSubSpec, and FlagSpec have been lowered to
// internal/core/cliflags. Type aliases here keep existing package cli callers
// working seamlessly while core/app packages consume the types from Tier 1.
type (
	SubcommandSpec = cliflags.SubcommandSpec
	SubSubSpec     = cliflags.SubSubSpec
	FlagSpec       = cliflags.FlagSpec
)

// Parser is the high-level handle the rest of the openagy CLI uses. It
// holds the root JetskiCLI plus the static specs that built it, so callers
// can introspect the surface without re-parsing --help.
type Parser struct {
	root   *JetskiCLI
	specs  []SubcommandSpec
	flags  []FlagSpec
	out    io.Writer
	errOut io.Writer
	// lastCode is the exit code the most recent Execute/ExecuteArgs
	// resolved to. Callers that need the precise code (2 for usage
	// errors, 1 for handler failures) read it via ExitCode().
	lastCode int
}

// NewParser builds the root JetskiCLI from the given flag and subcommand
// specs. extraFlags are appended to the standard flag set; they're for
// openagy-specific (post-v4) flags the parse-parity tests must not reject.
func NewParser(flags []FlagSpec, subs []SubcommandSpec, use, short, long string, extraFlags ...FlagSpec) *Parser {
	jc := NewJetski(use, "")
	allFlags := append(append([]FlagSpec{}, flags...), extraFlags...)
	allFlags = append(allFlags, FlagSpec{Name: "version", Type: "bool", Usage: ""})
	registerParserFlags(jc, allFlags)

	for _, s := range subs {
		js := buildJetskiSubcommand(jc, s, use)
		jc.AddSubcommand(js)
	}
	return &Parser{root: jc, specs: subs, flags: allFlags}
}

func registerParserFlags(jc *JetskiCLI, allFlags []FlagSpec) {
	for _, f := range allFlags {
		jc.AddFlag(&JetskiFlag{
			Name:     f.Name,
			Short:    f.Short,
			Type:     f.Type,
			Def:      f.Def,
			Usage:    f.Usage,
			Required: false,
			Hidden:   f.Hidden,
		})
	}
}

func buildJetskiSubcommand(jc *JetskiCLI, s SubcommandSpec, use string) *JetskiSubcommand {
	help := s.Help
	if s.HelpFn != nil {
		help = s.HelpFn(use)
	}
	specRun := s.Run
	runJ, _ := s.RunJ.(func(j *JetskiCLI, args []string) error)
	js := &JetskiSubcommand{
		Name:            s.Name,
		Aliases:         s.Aliases,
		Description:     s.Short,
		Hidden:          s.Hidden,
		Help:            help,
		HelpExit:        s.HelpExit,
		UnknownSubError: s.UnknownSubError,
		RunJ:            runJ,
		Run: func(args []string) error {
			if specRun == nil {
				return nil
			}
			out := jc.Out
			if out == nil {
				out = os.Stdout
			}
			return specRun(out, args)
		},
	}
	for _, ss := range s.Subs {
		js.Subs = append(js.Subs, buildJetskiSubSubcommand(jc, ss, use))
	}
	return js
}

func buildJetskiSubSubcommand(jc *JetskiCLI, ss SubSubSpec, use string) *JetskiSubSubcommand {
	specSS := ss
	ssHelp := ss.Help
	if ss.HelpFn != nil {
		ssHelp = ss.HelpFn(use)
	}
	runJ, _ := ss.RunJ.(func(j *JetskiCLI, args []string) error)
	return &JetskiSubSubcommand{
		Name:        ss.Name,
		Description: ss.Short,
		Help:        ssHelp,
		HelpExit:    ss.HelpExit,
		RunJ:        runJ,
		Run: func(args []string) error {
			if specSS.Run == nil {
				return nil
			}
			out := jc.Out
			if out == nil {
				out = os.Stdout
			}
			return specSS.Run(out, args)
		},
	}
}

// SetOut redirects parser output (used by tests to capture --help).
func (p *Parser) SetOut(w io.Writer) {
	p.out = w
	p.root.Out = w
	if p.root.flagSet != nil {
		p.root.flagSet.SetOutput(w)
	}
}

// SetErr redirects parser error output. Sets both the parser's errOut
// and the jetski engine's ErrOut (used for usage/help + error lines).
func (p *Parser) SetErr(w io.Writer) { p.errOut = w; p.root.ErrOut = w }

// Root returns the underlying JetskiCLI for callers that need to install
// custom Run hooks.
func (p *Parser) Root() *JetskiCLI { return p.root }

// SubcommandSpecs returns the registered subcommand specs (read-only).
func (p *Parser) SubcommandSpecs() []SubcommandSpec { return p.specs }

// FlagSpecs returns the registered flag specs (read-only).
func (p *Parser) FlagSpecs() []FlagSpec { return p.flags }

// Execute runs the parser against os.Args[1:] (the default for a
// top-level CLI invocation). Returns the jetski-style exit code
// (0 ok, 2 usage error). Cycle 40: previously this delegated to
// ExecuteArgs(nil) which always passed an empty slice, so flag.Parse
// saw no flags and the root Run got `args=[]` — making `antigravity
// --print "say hi"` silently fall through to the help banner.
func (p *Parser) Execute() error { return p.ExecuteArgs(os.Args[1:]) }

// ExecuteArgs is like Execute but with explicit args (used by tests).
// Passing nil means "use os.Args[1:]" so callers don't have to
// duplicate the slicing in every test.
func (p *Parser) ExecuteArgs(args []string) error {
	if args == nil {
		args = os.Args[1:]
	}
	code := p.root.Parse(args)
	p.lastCode = code
	if code == 0 {
		return nil
	}
	return fmt.Errorf("jetski: exit %d", code)
}

// ExitCode returns the exit code the most recent Execute/ExecuteArgs
// resolved to (0 ok, 1 handler failure, 2 usage error).
func (p *Parser) ExitCode() int { return p.lastCode }

// LookupFlagValue walks a parsed JetskiCLI's FlagSet and returns the string
// representation of the canonical flag. Empty if not set.
//
// In jetski mode the long and short forms are registered as separate
// flags; we check both.
func (p *Parser) LookupFlagValue(canonical string) string {
	fs := p.root.flagSet
	if fs == nil {
		return ""
	}
	if f := fs.Lookup(canonical); f != nil {
		return f.Value.String()
	}
	// Try short form
	for _, jf := range p.root.Flags {
		if jf.Name == canonical && jf.Short != "" {
			if f := fs.Lookup(jf.Short); f != nil {
				return f.Value.String()
			}
		}
	}
	return ""
}

// ParseForTest is a parse-only entry point used by the test rig. It builds
// a fresh Parser with the standard v4 specs and runs jetski against args.
// Returns the resulting Parser so tests can inspect Flag values.
func ParseForTest(args []string) (*Parser, error) {
	p := NewParser(StandardFlags(), StandardSubcommands(),
		"antigravity",
		"Recovered Pigasus CLI (1:1 parse parity with v1.3.4 binary)",
		"pigasus is a 1:1 parse-parity shell of Google Antigravity CLI v1.3.4.\n"+
			"Top-level flags, subcommands and sub-subcommands match the v1.3.4\n"+
			"binary so wrapper scripts and operator tooling are portable across\n"+
			"both. The CLI engine is jetski/cli 1:1 (cycle 38), the parser is\n"+
			"Go stdlib `flag` (no cobra/pflag), the TUI is bubbletea v2.")
	p.SetOut(io.Discard)
	p.SetErr(io.Discard)
	err := p.ExecuteArgs(args)
	return p, err
}

// ---- parser_flags.go ----

// parser_flags.go — flag specifications for StandardFlags and ExtraFlags.

// StandardFlags returns the top-level flags emitted by the reference
// binary's `--help`. Order, type, short alias and aliases match 1:1.
//
// The visible column is the reference's `--help`: 21 long flags
// plus the 3 short aliases (-c, -i, -p) — 24 entries. Three more are
// Hidden, so they never show up in `--help`:
//
//   - the recovered hidden `-v N` int flag (cycle 101);
//   - the hidden `--verbose` bool flag added by the
//     headless-jsonl-mode-trailing-newline cycle (AGY 1.2.7 — emits the
//     `assistant_message` event in `--output-format stream-json`);
//   - the hidden `--print-partial-response` bool flag.
//
// `--print-partial-response` is hidden because the reference has no
// such flag: the 1.4.7 binary carries no "print-partial-response"
// string (【一手】, `strings` over the installed binary), its `--help`
// does not list it, and its changelog never mentions it (the cycle
// report that added it quoted a 1.2.10 changelog line that is in
// neither the live changelog nor the vendored
// `internal/cli/testdata/changelog_reference.txt`). It stays
// parseable as a clone extra, like `--verbose`; the visible `--help`
// stays byte-identical with the reference's — re-verified at 1.4.7,
// where the whole help surface is byte-identical to 1.4.6.
func StandardFlags() []FlagSpec {
	return append(standardFlagsCore(), standardFlagsTail()...)
}

func standardFlagsCore() []FlagSpec {
	return []FlagSpec{
		{Name: "add-dir", Type: "stringSlice", Def: "[]", Usage: "Add a directory to the workspace (repeatable)"},
		{Name: "agent", Type: "string", Usage: "Agent for the current CLI session"},
		{Name: "c", Type: "bool", Usage: "Short alias for --continue"},
		// Hidden: the reference's local-Chrome CDP control 【一手 +
		// 反编译 — out/local-macos/FINDINGS.md §2, items 4-7】. The
		// names, the types, the defaults and the usage strings are the
		// reference's own — they are in its 1.3.4 strings dump
		// ("Port for Chrome DevTools Protocol", "Chrome user data
		// directory.", "Whether Chrome runs in headless mode.",
		// "Whether to use local chrome" 【strings】). The clone's
		// browser client reads them (browsercdp.Config).
		{Name: "cdp_port", Type: "int", Hidden: true, Def: "9222", Usage: "Port for Chrome DevTools Protocol"},
		{Name: "continue", Type: "bool", Usage: "Continue the most recent conversation"},
		{Name: "conversation", Type: "string", Usage: "Resume a previous conversation by ID"},
		{Name: "dangerously-skip-permissions", Type: "bool", Usage: "Auto-approve all tool permission requests without prompting"},
		{Name: "disable-slash-commands", Type: "bool", Usage: "Disable slash command and skill expansion in print mode"},
		{Name: "effort", Type: "string", Usage: "Reasoning effort for the current CLI session (low|medium|high|xhigh|max)"},
		{Name: "i", Type: "string", Usage: "Short alias for --prompt-interactive"},
		{Name: "input-format", Type: "string", Def: "text", Usage: "Input format for print mode (text, stream-json). stream-json reads one NDJSON message per line from stdin and runs a turn for each; it requires --output-format stream-json"},
		{Name: "json-schema", Type: "string", Usage: "Optional JSON schema string or path to a schema file to enforce structured output (for stream-json, only applicable to the final result)"},
		// Hidden: the reference's local-Chrome CDP control, the rest
		// of the four 【一手 + 反编译 — out/local-macos/FINDINGS.md §2,
		// items 5 and 6】.
		{Name: "local_chrome_headless", Type: "bool", Hidden: true, Def: "true", Usage: "Whether Chrome runs in headless mode."},
		{Name: "local_chrome_user_data_dir", Type: "string", Hidden: true, Def: "", Usage: "Chrome user data directory."},
		{Name: "log-file", Type: "string", Usage: "Override CLI log file path"},
		{Name: "mode", Type: "string", Usage: "Set the agent execution mode for this session (" + ModeList() + ")"},
		{Name: "model", Type: "string", Usage: "Model for the current CLI session"},
	}
}

func standardFlagsTail() []FlagSpec {
	return []FlagSpec{
		{Name: "new-project", Type: "bool", Usage: "Create a new project for this session"},
		{Name: "output-format", Type: "string", Def: "text", Usage: "Output format for print mode (text, json, stream-json)"},
		{Name: "p", Type: "string", Usage: "Short alias for --print"},
		{Name: "print", Type: "string", Usage: "Run a single prompt non-interactively and print the response"},
		// Hidden: the reference 1.4.6 has no such flag (no string in
		// the binary, absent from its --help and from its changelog).
		// Parseable as a clone extra so the partial-response
		// side-channel stays reachable without diverging from the
		// reference's visible --help.
		{Name: "print-partial-response", Type: "bool", Hidden: true, Def: "false", Usage: "Stream each chunk of the model's output to stdout in headless mode as it arrives instead of buffering until the turn completes"},
		{Name: "print-timeout", Type: "duration", Def: "0s", Usage: "Optional time limit for print mode; 0 waits until the turn completes"},
		{Name: "project", Type: "string", Usage: "Project ID or project name for the current CLI session"},
		{Name: "prompt", Type: "string", Usage: "Alias for --print"},
		{Name: "prompt-interactive", Type: "string", Usage: "Run an initial prompt interactively and continue the session"},
		{Name: "remote-agent", Type: "bool", Usage: "Run this session's conversations using remote agents; --project then names a remote project by ID or name, otherwise they open in this machine's default remote project"},
		{Name: "remote-control", Type: "bool", Usage: "Create a remote connection for the CLI session on start up"},
		{Name: "sandbox", Type: "bool", Usage: "Run in a sandbox with terminal restrictions enabled"},
		{Name: "settings", Type: "string", Usage: "Settings for this session only, never saved: a JSON object of settings.json keys, inline or as a file path"},
		// Hidden: the reference's local-Chrome CDP control, the last of
		// the four 【一手 + 反编译 — out/local-macos/FINDINGS.md §2,
		// item 7】.
		{Name: "use_local_chrome", Type: "bool", Hidden: true, Def: "false", Usage: "Whether to use local chrome"},
		{Name: "v", Type: "int", Hidden: true, Usage: ""},
		{Name: "verbose", Type: "bool", Hidden: true, Def: "false", Usage: "Print extra stream-json events (assistant_message) in --output-format stream-json"},
	}
}

// ExtraFlags returns the cycle-33+ openagy-specific flags that are NOT
// part of the v4 binary 1:1 surface.
func ExtraFlags() []FlagSpec {
	return append(extraFlagsCore(), extraFlagsRC()...)
}

func extraFlagsCore() []FlagSpec {
	return []FlagSpec{
		{Name: "backend", Type: "string", Hidden: true, Usage: "Backend type: gateway (real) or shim (mock). Default: gateway (auto-falls back to shim if no OAuth token)"},
		{Name: "alt-screen", Type: "bool", Def: "true", Hidden: true, Usage: "Use the terminal's alternate screen buffer (default true; an explicit --alt-screen/--no-alt-screen overrides the /config Rendering Mode setting)"},
		{Name: "no-alt-screen", Type: "bool", Def: "false", Hidden: true, Usage: "Disable the terminal's alternate screen buffer (overrides --alt-screen and the /config Rendering Mode setting)"},
		{Name: "trust-cwd", Type: "bool", Def: "false", Hidden: true, Usage: "Skip the on-startup workspace-trust dialog (or set $OPENAGY_TRUST=1)"},
		{Name: "max-thinking-budget", Type: "int", Def: "0", Hidden: true, Usage: "Cap thinking tokens per turn (integer); values <= 0 disable thinking entirely"},
		{Name: "diff-with-agy", Type: "bool", Def: "false", Hidden: true, Usage: "Print AGY 1.4.2 feature parity (aligned / gap / diverged)"},
		{Name: "debug", Type: "bool", Def: "false", Hidden: true, Usage: "Enable verbose debug logging"},
	}
}

func extraFlagsRC() []FlagSpec {
	return []FlagSpec{
		{Name: "desktop", Type: "bool", Def: "false", Hidden: true, Usage: "Serve the reference desktop UI (the voice input, the jetbox terminal, the aux pane redesign)"},
		{Name: "remote-control-tcp", Type: "string", Def: "", Hidden: true, Usage: "Bind addr (host:port) for the WebSocket JSON-RPC transport; empty disables. Replaces --remote-control when set"},
		{Name: "remote-control-socket", Type: "string", Def: "", Hidden: true, Usage: "Override the unix socket path for --remote-control. $OPENAGY_RC_SOCKET still wins"},
		{Name: "experimental-rc-v2", Type: "bool", Hidden: true, Usage: "Opt-in to the deferred v2 (WebRTC mesh / Hub) features. No-op today; reserved for forward compat."},
	}
}

// ---- parser_specs.go ----

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
// contract). We add hidden subcommands (version, login, logout,
// auth, chat, acp, http, hooks, migrate, stats, logo) that wire to the
// runXxx handlers in dispatch.go, plus four visible ones of the
// clone's own (accounts, halloween, model, provider — the last three
// beside the reference's models). The hidden flag keeps --help at
// 39 lines; the new subcommands are invokable as
// `antigravity <name>` and route to real internal/ handlers.

// StandardSubcommands returns the 12 subcommands the v1.3.5 binary lists
// in `agy --help`:
//
//	agent, agents, changelog, help, install, mcp, mic-serve, models,
//	plugin, plugins, remote-control, update
//
// `agent` and `agents` share one implementation (both print the same
// "Usage: <prog> agent [flags]" block), and `plugins` is an alias of
// `plugin`.
func StandardSubcommands() []SubcommandSpec {
	pluginSubs := standardPluginSubs()
	mcpSubs := standardMCPSubs()
	rcSubs := standardRCSubs()

	var subs []SubcommandSpec
	subs = append(subs, pluginAndMCPSubcommands(pluginSubs, mcpSubs)...)
	subs = append(subs, rcAndMediaSubcommands(rcSubs)...)
	subs = append(subs, generalSubcommands()...)
	subs = append(subs, hiddenUtilitySubcommands()...)
	subs = append(subs, providerSubcommand(), modelSubcommand(), accountsSubcommand())
	subs = append(subs, halloweenSubcommand())
	return subs
}

// halloweenSubcommand is the clone's own command for printing the
// seasonal banner art. The reference has no such command — the art only
// ever appears in its startup banner — so it is a pigasus addition, next
// to the other openagy commands.
func halloweenSubcommand() SubcommandSpec {
	return SubcommandSpec{
		Name:   "halloween",
		Short:  "Print the seasonal banner art",
		HelpFn: usageHalloween,
		RunJ:   runHalloween,
	}
}

func standardPluginSubs() []SubSubSpec {
	return []SubSubSpec{
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
}

func standardMCPSubs() []SubSubSpec {
	return []SubSubSpec{
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
}

func standardRCSubs() []SubSubSpec {
	return []SubSubSpec{
		{Name: "start", Short: "Register and start the remote-control daemon",
			RunJ: runRCStart, HelpFn: usageRCStart},
		{Name: "serve", Short: "Run the remote-control daemon in the foreground (OAuth login inline)",
			RunJ: runRCServe, HelpFn: usageRCServe},
		// `run` is the clone's own, platform-independent spelling of
		// `serve`: the same daemon, in the foreground, on every
		// platform. It is hidden — the reference's `remote-control
		// --help` lists start, status and stop only, and that block
		// is the reference's, byte for byte.
		{Name: "run", Short: "Run the remote-control daemon in the foreground (same on every platform)",
			RunJ: runRCRun, HelpFn: usageRCRun},
		// `rename` is the clone's own: it writes the instance name
		// into the configuration file, and a running daemon picks it
		// up and renames the instance on the dashboard without
		// dropping the tunnel. It is hidden for the same reason `run`
		// is — the reference's `remote-control --help` lists start,
		// status and stop only, and that block is byte for byte the
		// reference's.
		{Name: "rename", Short: "Set this machine's instance name shown in Remote Control",
			RunJ: runRCRename, HelpFn: usageRCRename},
		{Name: "status", Short: "Show the daemon's status",
			RunJ: runRCStatus, HelpFn: usageRCStatus},
		{Name: "stop", Short: "Stop the daemon and unregister it",
			RunJ: runRCStop, HelpFn: usageRCStop},
	}
}

func pluginAndMCPSubcommands(pluginSubs, mcpSubs []SubSubSpec) []SubcommandSpec {
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
			RunJ: func(j *JetskiCLI, args []string) error {
				return dispatchRunXxx("install", j.Out, j.ErrOut, context.Background(), nil, args)
			},
		},
	}
}

func rcAndMediaSubcommands(rcSubs []SubSubSpec) []SubcommandSpec {
	return []SubcommandSpec{
		{
			Name:   "acp",
			Short:  "Start the Agent Client Protocol (ACP) stdio server",
			HelpFn: usageACP,
			RunJ:   runACPCommand,
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
		{
			Name:   "remote-control-foreground",
			Short:  "Foreground subcommand the persistent daemon invokes (hidden)",
			Hidden: true,
			RunJ:   runRCForegroundPlaceholder,
		},
	}
}

func generalSubcommands() []SubcommandSpec {
	return []SubcommandSpec{
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
			Name:     "update",
			Short:    "Update CLI",
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
	}
}

func hiddenUtilitySubcommands() []SubcommandSpec {
	wrapRunXxx := func(name string) func(w io.Writer, args []string) error {
		return func(w io.Writer, args []string) error {
			return dispatchRunXxx(name, w, os.Stderr, context.Background(), nil, args)
		}
	}

	return []SubcommandSpec{
		{Name: "login", Short: "Start the OAuth device-code login flow", Run: wrapRunXxx("login"), Hidden: true},
		{Name: "logout", Short: "Clear the stored OAuth token", Run: wrapRunXxx("logout"), Hidden: true},
		{Name: "auth", Short: "Set up the mTLS bypass proxy", Run: wrapRunXxx("auth"), Hidden: true},
		{Name: "chat", Short: "Launch the recovered TUI chat", Run: wrapRunXxx("chat"), Hidden: true},
		{Name: "http", Short: "Start the recovered HTTP server", Run: wrapRunXxx("http"), Hidden: true},
		{Name: "hooks", Short: "List configured lifecycle hooks", Run: wrapRunXxx("hooks"), Hidden: true},
		{Name: "migrate", Short: "Migrate legacy config to the v4 layout", Run: wrapRunXxx("migrate"), Hidden: true},
		{Name: "stats", Short: "Show on-disk session statistics", Run: wrapRunXxx("stats"), Hidden: true},
		{Name: "logo", Short: "Print the startup banner logo (hidden)", HelpFn: tuilib.UsageLogo, Run: runLogoHandler, Hidden: true},
	}
}

func providerSubcommand() SubcommandSpec {
	return SubcommandSpec{
		Name:    "provider",
		Short:   "Manage the configured model providers",
		Aliases: []string{"providers"},
		HelpFn:  usageProvider,
		Subs: []SubSubSpec{
			{Name: "list", Short: "List the configured providers", RunJ: runProviderList, HelpFn: func(n string) string {
				return "Usage: " + n + " provider list\n"
			}},
			{Name: "presets", Short: "List available preset providers from Catwalk", RunJ: runProviderPresets, HelpFn: usageProviderPresets},
			{Name: "show", Short: "Show one provider in detail", RunJ: runProviderShow, HelpFn: usageProviderShow},
			{Name: "add", Short: "Create or update a provider", RunJ: runProviderAdd, HelpFn: usageProviderAdd},
			{Name: "remove", Short: "Remove a provider and its models", RunJ: runProviderRemove, HelpFn: usageProviderRemove},
			{Name: "test", Short: "Probe the provider's endpoint and credential", RunJ: runProviderTest, HelpFn: usageProviderTest},
		},
		RunJ: runProviderParent,
	}
}

func modelSubcommand() SubcommandSpec {
	return SubcommandSpec{
		Name:   "model",
		Short:  "Manage the configured models",
		HelpFn: usageModel,
		Subs: []SubSubSpec{
			{Name: "list", Short: "List the configured models", RunJ: runModelList, HelpFn: func(n string) string {
				return "Usage: " + n + " model list [--provider <provider>]\n"
			}},
			{Name: "show", Short: "Show one model in detail", RunJ: runModelShow, HelpFn: usageModelShow},
			{Name: "add", Short: "Create or update a model", RunJ: runModelAdd, HelpFn: usageModelAdd},
			{Name: "remove", Short: "Remove a model", RunJ: runModelRemove, HelpFn: usageModelRemove},
		},
		RunJ: runModelParent,
	}
}

func accountsSubcommand() SubcommandSpec {
	return SubcommandSpec{
		Name:   "accounts",
		Short:  "Manage the signed-in Google accounts",
		HelpFn: usageAccounts,
		Subs: []SubSubSpec{
			{Name: "tui", Short: "Interactive accounts and routing management TUI", RunJ: runAccountsTUI, HelpFn: usageAccountsTUI},
			{Name: "list", Short: "List the known accounts", RunJ: runAccountsList, HelpFn: func(n string) string {
				return "Usage: " + n + " accounts list\n"
			}},
			{Name: "show", Short: "Show one account in detail", RunJ: runAccountsShow, HelpFn: usageAccountsShow},
			{Name: "add", Short: "Sign in to another Google account", RunJ: runAccountsAdd, HelpFn: usageAccountsAdd},
			{Name: "switch", Short: "Make one account the active one", RunJ: runAccountsSwitch, HelpFn: usageAccountsSwitch},
			{Name: "enable", Short: "Clear quarantine / disabled status of an account", RunJ: runAccountsEnable, HelpFn: usageAccountsEnable},
			{Name: "remove", Short: "Forget one account", RunJ: runAccountsRemove, HelpFn: usageAccountsRemove},
		},
		RunJ: runAccountsParent,
	}
}

func init() {
	cliflags.RegisterProvider("accounts", accountsSubcommand)
	cliflags.RegisterProvider("provider", providerSubcommand)
	cliflags.RegisterProvider("model", modelSubcommand)
	cliflags.RegisterProvider("halloween", halloweenSubcommand)
	cliflags.RegisterMultiProvider("agent", AgentSubcommands)
	cliflags.RegisterProvider("changelog", ChangelogSubcommand)
	cliflags.RegisterProvider("update", UpdateSubcommand)
	cliflags.RegisterProvider("models", ModelsSubcommand)
	cliflags.RegisterProvider("install", InstallSubcommand)
	cliflags.RegisterProvider("mic-serve", MicServeSubcommand)
	cliflags.RegisterMultiProvider("rc", RCSubcommands)
}

// Exported subcommand constructors for modular composition:

func AccountsSubcommand() SubcommandSpec  { return accountsSubcommand() }
func ProviderSubcommand() SubcommandSpec  { return providerSubcommand() }
func ModelSubcommand() SubcommandSpec     { return modelSubcommand() }
func HalloweenSubcommand() SubcommandSpec { return halloweenSubcommand() }

func AgentSubcommands() []SubcommandSpec {
	subs := generalSubcommands()
	return []SubcommandSpec{subs[0], subs[1]}
}

func ChangelogSubcommand() SubcommandSpec { return generalSubcommands()[2] }
func UpdateSubcommand() SubcommandSpec {
	return SubcommandSpec{
		Name:     "update",
		Short:    "Update CLI",
		Help:     "Usage of update:\n",
		HelpExit: 2,
		Run:      RunUpdate,
	}
}
func ModelsSubcommand() SubcommandSpec { return generalSubcommands()[4] }
func HelpSubcommand() SubcommandSpec   { return generalSubcommands()[5] }

func InstallSubcommand() SubcommandSpec {
	return pluginAndMCPSubcommands(standardPluginSubs(), standardMCPSubs())[3]
}

func MicServeSubcommand() SubcommandSpec {
	return rcAndMediaSubcommands(standardRCSubs())[0]
}

func RCSubcommands() []SubcommandSpec {
	return rcAndMediaSubcommands(standardRCSubs())[1:]
}

func WASMSubcommands() []SubcommandSpec {
	return []SubcommandSpec{
		accountsSubcommand(),
		generalSubcommands()[0], // agent
		generalSubcommands()[1], // agents
		generalSubcommands()[2], // changelog
		halloweenSubcommand(),
		generalSubcommands()[5], // help
		modelSubcommand(),
		generalSubcommands()[4], // models
		providerSubcommand(),
	}
}

// ---- flagset.go ----

// Package cli — flagset.go: a standalone flag set built from the
// standard specs.
//
// The root command owns a parser; a subcommand that runs a session of its
// own — the TUI attached to a server — needs the same flags without the
// root command's parser around them.

// NewFlagSet builds a stdlib flag set from the given specs.
//
// The set parses like the root command's: the same names, the same
// types, the same defaults. Its output is discarded, so a caller that
// wants the parse error on the terminal sets the output itself.
func NewFlagSet(name string, specs []FlagSpec) *flag.FlagSet {
	j := NewJetski(name, "")
	for _, s := range specs {
		j.AddFlag(&JetskiFlag{
			Name:     s.Name,
			Short:    s.Short,
			Type:     s.Type,
			Def:      s.Def,
			Usage:    s.Usage,
			Required: false,
			Hidden:   s.Hidden,
		})
	}
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	j.registerFlags(fs)
	return fs
}

// ---- thinking_budget.go ----

// AGY 1.2.13 changelog gate for --max-thinking-budget.
//
// "Fixed the --max-thinking-budget flag accepting a 0 value, which
// caused infinite thinking loops that locked up the CLI until Ctrl-C;
// values <= 0 now disable thinking entirely, matching the documented
// semantics."
//
// This file is the single source of truth for the gate: any caller
// holding a parsed --max-thinking-budget value funnels it through
// ThinkingEnabled() / ThinkingBudget() before forwarding to the model
// adapter. Centralising the rule means a future consumer (the print
// path, the TUI, a test rig) cannot drift back to the pre-fix
// behaviour where a raw 0 was passed through.
//
// The flag is registered in parser_specs.go under ExtraFlags() as a
// hidden int with a default of 0. Zero is the post-fix "disabled"
// value, so the unset default itself lands in the disabled branch —
// users who want thinking on must pass --max-thinking-budget=N with
// N > 0.

// ThinkingEnabled reports whether thinking should be on for the
// given --max-thinking-budget value. The rule is the AGY 1.2.13
// fix: any non-positive budget (the unset default 0, an explicit 0,
// or any negative number) disables thinking. Only positive integer
// budgets keep thinking on.
//
// This is a pure function so callers do not have to remember the
// rule and so the test suite can pin it without spinning up a
// Parser.
func ThinkingEnabled(budget int) bool {
	return budget > 0
}

// ThinkingBudget returns the budget to forward to the model adapter,
// or 0 when thinking is disabled. The post-fix semantic is that 0
// is the wire-level "off" sentinel, so callers that always forward
// the returned int to a `thinking_budget` field get "thinking off"
// for free when the user passed a non-positive budget.
//
// Returns (enabled, budget). When enabled is false, budget is 0 and
// the caller should NOT set the field at all (or should set it to 0,
// depending on the wire contract). The two-value form lets callers
// that distinguish "no field" from "field = 0" do the right thing
// without re-applying the gate.
func ThinkingBudget(raw int) (enabled bool, budget int) {
	if raw > 0 {
		return true, raw
	}
	return false, 0
}

// ThinkingBudgetFromFlagSet looks up --max-thinking-budget on a
// parsed stdlib *flag.FlagSet and returns the (enabled, budget)
// pair the wire-level adapter should honour. Returns (false, 0)
// when the flag is not registered or its value is malformed.
//
// The flag value comes back as a string because the intAtoiValue
// wrapper (see internal/cli/jetski.go) keeps the raw text alongside
// the parsed int — we re-parse so a malformed value surfaces as
// disabled rather than silently coercing to 0.
func ThinkingBudgetFromFlagSet(fs *flag.FlagSet) (enabled bool, budget int) {
	if fs == nil {
		return false, 0
	}
	f := fs.Lookup("max-thinking-budget")
	if f == nil {
		return false, 0
	}
	s := f.Value.String()
	if s == "" {
		return false, 0
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return false, 0
	}
	return ThinkingBudget(n)
}
