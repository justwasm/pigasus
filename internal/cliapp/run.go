// Command pigasus is the openagy recovered 1:1 parse-parity shell of Google
// Antigravity CLI v1.3.4 (the v4 binary).
//
// Cycle 121 rename: the directory was `cmd/antigravity`; it was renamed to
// `cmd/wagyu` in cycle 121, and renamed to `cmd/pigasus` to align with the
// project module name github.com/justwasm/pigasus.
// The Go package is `main` and nothing imports it, so only the directory and
// the references to it changed.
package cliapp

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/justwasm/pigasus/internal/agent"
	"github.com/justwasm/pigasus/internal/agyprompt"
	"github.com/justwasm/pigasus/internal/agyproxy"
	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/branding"
	"github.com/justwasm/pigasus/internal/builtinskills"
	"github.com/justwasm/pigasus/internal/cli"
	"github.com/justwasm/pigasus/internal/cliflags"
	"github.com/justwasm/pigasus/internal/daemon"
	"github.com/justwasm/pigasus/internal/modelcatalog"
	"github.com/justwasm/pigasus/internal/printmode"
	"github.com/justwasm/pigasus/internal/rchost"
	"github.com/justwasm/pigasus/internal/termcolor"
	"github.com/justwasm/pigasus/internal/tuilib"
)

const cliVersion = cli.ReferenceVersion

func init() {
	lipgloss.Writer.Profile = termcolor.Resolve(os.Environ())
}

// Run is the main CLI entrypoint.
func Run(args []string) int {
	// `pigasus agy` runs the vendor Antigravity CLI behind the local
	// CloudCode proxy (internal/agyproxy). It is dispatched before the
	// clone's startup work — compat defaults, plugin cleanup, signal
	// handlers — because the vendor binary owns those, and because the
	// command line must reach it untouched: `pigasus agy --help` prints the
	// vendor's help, not the clone's.
	if len(args) > 1 && args[1] == "agy" {
		return agyproxy.Launch(args[2:])
	}

	daemon.InstallSignalHandlers()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		daemon.Global().Shutdown(ctx)
	}()

	_, _ = auth.WriteCompatDefaults()
	_ = cli.RunPluginCleanupOnStartup(auth.AntigravityConfigHome())
	// The reference unpacks its builtin skill bundle into
	// <AppDataDir>/builtin/skills on startup; the clone ships the same
	// bundle and fills in whatever is missing. Files that are already there
	// are left alone, so the reference's own install always wins.
	_, _ = builtinskills.Install(auth.AntigravityConfigHome())

	p := setupCLIParser()
	if err := p.Execute(); err != nil {
		return p.ExitCode()
	}
	return 0
}

func setupCLIParser() *cli.Parser {
	p := cli.NewParser(
		cli.StandardFlags(),
		append(cli.StandardSubcommands(), proxySubcommand(), cli.WebSubcommand()),
		filepath.Base(os.Args[0]),
		"Recovered "+branding.Current().CLIName+" (1:1 parse parity with v4 binary)",
		"pigasus is a 1:1 parse-parity shell of Google Antigravity CLI v1.3.3.\n"+
			"Top-level flags, subcommands and sub-subcommands match the v4 binary\n"+
			"so wrapper scripts and operator tooling are portable across both.\n"+
			"Runtime behavior is recovered from Ghidra 12.1.4 + REA 6.4.0 seeds.",
		cli.ExtraFlags()...,
	)

	p.Root().Version = cliVersion + "\n"
	p.Root().RootRun = func(args []string) error {
		return runRoot(args, p)
	}

	registerParserBridges()
	return p
}

func registerParserBridges() {
	cli.SetChatBridge(runLiveTUI)
	cli.RegisterForegroundHandler(rchost.RunForeground)
	cli.RegisterServeHandler(rchost.RunServe)
	cli.RegisterWebHandler(rchost.RunWeb)
	cli.RegisterLogoHandler(tuilib.RunLogoSubcommand)
	rchost.BackendPicker = func(modelName string) backend.Client {
		return pickBackendWithLog(nil, modelName, io.Discard, backend.GatewayTimeoutUnlimited)
	}
	printmode.BackendPicker = func(fs *flag.FlagSet, modelName string, w io.Writer) backend.Client {
		return pickBackendWithLog(fs, modelName, w, backend.GatewayTimeoutUnlimited)
	}
	printmode.BackendResolver = func(fs *flag.FlagSet, w io.Writer) (string, bool) {
		return resolveBackend(fs, w)
	}
	printmode.ModelResolver = func(model, effort string) (string, error) {
		return modelcatalog.ResolveModelSelection(model, effort)
	}
	printmode.ConversationResolver = func(fs *flag.FlagSet, warn io.Writer) printmode.ConversationSelection {
		sel := resolveConversationSelection(fs, warn)
		return printmode.ConversationSelection{
			ID:             sel.ID,
			Source:         sel.Source,
			Store:          sel.Store,
			History:        sel.History,
			PriorUserTurns: sel.PriorUserTurns,
		}
	}
	printmode.FeatDiffRunner = runFeatDiffCLI

	cli.RegisterAgentsLister(func() []string {
		agents := agent.Discover()
		names := make([]string, 0, len(agents))
		for _, a := range agents {
			names = append(names, a.Name)
		}
		return names
	})
}

// runRoot handles the top-level commands and flags.
func runRoot(args []string, p *cli.Parser) error {
	out := p.Root().Out
	fs := p.Root().FlagSet()
	if fs == nil {
		return nil
	}
	// The browser's policy comes from the reference's own four hidden
	// flags; the browser client reads them from here on.
	applyBrowserFlags(fs)
	applyProjectFlags(fs)
	applyLogFileFlag(fs)
	if done, err := checkRootShortCircuits(fs, p, out); done || err != nil {
		return err
	}
	printMode := resolvePrintMode(fs)
	if err := validateRootFlags(fs, printMode); err != nil {
		return err
	}
	if printMode {
		return dispatchPrintMode(fs, out)
	}
	if flagWasSet(fs, "prompt-interactive") || flagWasSet(fs, "i") {
		return dispatchInteractiveMode(fs, out)
	}
	if cliflags.ResolveBoolAlias(fs, "continue", "c") {
		return dispatchInteractiveMode(fs, out)
	}
	return dispatchDefaultRoot(args, fs, out)
}

const (
	telemetryProductName      = auth.TelemetryProductName
	telemetryDisabledValue    = auth.TelemetryDisabledValue
	telemetryDisabledResponse = auth.TelemetryDisabledResponse
	telemetryRowName          = "Enable Telemetry"
)

func IsBusinessAccount() bool {
	return auth.IsBusinessAccount()
}

func telemetryBusinessDescription() string {
	return auth.TelemetryBusinessDescription()
}

func isTelemetryRow(row configRow) bool {
	return row.Name == telemetryRowName
}

// ---- merged from main_flags.go ----
// main_flags.go — the command-line flag helpers and the model-label
// vocabulary the CLI and the TUI share.
//
// The flag accessors are thin wrappers over internal/cliflags (which is the
// single implementation of "was this flag set?" / "what is its value?").
// The model labels are wrappers over internal/modelcatalog.

// resolvePrintMode returns true if --print, --prompt or -p was given.
func resolvePrintMode(fs *flag.FlagSet) bool {
	return flagWasSet(fs, "print") || flagWasSet(fs, "prompt") || flagWasSet(fs, "p")
}

// customSubagents lists the markdown agents added to the default agent's <subagents> section.
func customSubagents() []agyprompt.Subagent {
	agents := agent.Discover()
	out := make([]agyprompt.Subagent, 0, len(agents))
	for _, a := range agents {
		out = append(out, agyprompt.Subagent{Name: a.Name, Description: a.Description})
	}
	return out
}

func knownAgent(name string) bool {
	return agent.ValidateAgent(name) == nil
}

// printPromptSet returns true if any of the print-prompt flags was explicitly set.
func printPromptSet(fs *flag.FlagSet) bool {
	for _, n := range []string{"print", "prompt", "p"} {
		if flagWasSet(fs, n) {
			return true
		}
	}
	return false
}

// printPrompt returns the prompt text passed to --print / --prompt / -p.
func printPrompt(fs *flag.FlagSet) string {
	for _, n := range []string{"print", "prompt", "p"} {
		if flagWasSet(fs, n) {
			return cliflags.StringValue(fs, n)
		}
	}
	return ""
}

// flagWasSet reports whether the named flag was explicitly passed on the command line.
func flagWasSet(fs *flag.FlagSet, name string) bool {
	return cliflags.WasSet(fs, name)
}

// warnAgentMode warns when --mode is used with --agent, matching v1.3.5.
func warnAgentMode(fs *flag.FlagSet, sessionCreation bool) {
	if fs == nil {
		return
	}
	if cliflags.StringValue(fs, "agent") != "" && cliflags.StringValue(fs, "mode") != "" {
		fmt.Fprintln(os.Stderr, "warning: --mode is ignored when --agent is set")
	}
}

// --- the model-label vocabulary ---------------------------------------------
//
// Cycle 346: this block used to hold eighteen one-line wrappers —
// flagBoolValue, flagStringValue, flagIntValue, resolveBoolAlias,
// formatModelLabel, resolveEffectiveModel, canonicalModelLabel,
// wireModelLabel, resolveDefaultEffort, availableModelsBlock,
// resolveModelSelection, modelSlug and the tuilib help renderers — each
// forwarding to internal/cliflags, internal/modelcatalog or
// internal/tuilib. The call sites now name the package directly, so
// there is one hop instead of two, and a new helper in one of those
// packages is reachable without touching this one.
//
// The two aliases that stay are the ones the TUI reads as *values*:
// the default model label and the effort vocabulary.

const defaultModel = modelcatalog.DefaultModel

var validEfforts = modelcatalog.ValidEfforts
