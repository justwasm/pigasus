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

	"github.com/justwasm/pigasus/internal/infra/agyproxy"
	"github.com/justwasm/pigasus/internal/cli"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/core/branding"
	"github.com/justwasm/pigasus/internal/core/cliflags"
	confhome "github.com/justwasm/pigasus/internal/core/confhome"
	"github.com/justwasm/pigasus/internal/core/settings"
	"github.com/justwasm/pigasus/internal/features/agents"
	"github.com/justwasm/pigasus/internal/features/auth"
	modelcatalog "github.com/justwasm/pigasus/internal/features/models/catalog"
	builtinskills "github.com/justwasm/pigasus/internal/features/tools/skills"
	"github.com/justwasm/pigasus/internal/infra/backend/gateway"
	backendselect "github.com/justwasm/pigasus/internal/infra/backend/select"
	"github.com/justwasm/pigasus/internal/infra/daemon"
	"github.com/justwasm/pigasus/internal/printmode"
	"github.com/justwasm/pigasus/internal/rchost"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/internal/ui/views/modelpicker"
	termcolor "github.com/justwasm/pigasus/internal/ui/render/color"
	featdiff "github.com/justwasm/pigasus/internal/ui/views/diff/feat"
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
	// The reference normalises settings.json on every start-up: it
	// rewrites the file in its own format and drops the keys that hold
	// their default value. The error is advisory — a settings file that
	// cannot be normalised must not keep the CLI from starting.
	_ = settings.NormalizeOnStartup()
	_ = cli.RunPluginCleanupOnStartup(confhome.Antigravity())
	// The reference unpacks its builtin skill bundle into
	// <AppDataDir>/builtin/skills on startup; the clone ships the same
	// bundle and fills in whatever is missing. Files that are already there
	// are left alone, so the reference's own install always wins.
	_, _ = builtinskills.Install(confhome.Antigravity())

	appName := filepath.Base(os.Args[0])
	var cmdArgs []string
	if len(args) > 0 {
		appName = filepath.Base(args[0])
		cmdArgs = args[1:]
	} else {
		cmdArgs = os.Args[1:]
	}

	p := setupCLIParserWithProg(appName)
	if err := p.ExecuteArgs(cmdArgs); err != nil {
		return p.ExitCode()
	}
	return 0
}

// SetupCLIParser builds the full CLI parser with all subcommands registered,
// including the clone's extensions (server, tui, web, proxy).
func SetupCLIParser(prog string) *cli.Parser {
	return setupCLIParserWithProg(prog)
}

func setupCLIParserWithProg(prog string) *cli.Parser {
	if prog == "" {
		prog = filepath.Base(os.Args[0])
	}
	p := cli.NewParser(
		cli.StandardFlags(),
		append(cli.StandardSubcommands(), proxySubcommand(), cli.WebSubcommand(), cli.ServerSubcommand(), cli.TUISubcommand()),
		prog,
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
	cli.RegisterServerHandler(runServer)
	cli.RegisterTUIHandler(runTUIHandler)
	cli.RegisterACPHandler(runAcpHandler)
	cli.RegisterLogoHandler(tuilib.RunLogoSubcommand)
	rchost.BackendPicker = func(modelName string) backend.Client {
		return backendselect.Pick(nil, modelName, modelpicker.ResolveCatalogueID)
	}
	printmode.BackendPicker = func(fs *flag.FlagSet, modelName string, w io.Writer) backend.Client {
		return backendselect.PickWithLog(fs, modelName, w, gateway.GatewayTimeoutUnlimited, modelpicker.ResolveCatalogueID)
	}
	printmode.BackendResolver = func(fs *flag.FlagSet, w io.Writer) (string, bool) {
		return backendselect.Resolve(fs, w)
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
	printmode.FeatDiffRunner = featdiff.RunCLI

	cli.RegisterAgentsLister(func() []string {
		found := agents.Discover()
		names := make([]string, 0, len(found))
		for _, a := range found {
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
	applySessionFlags(fs)
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
	if cliflags.WasSet(fs, "prompt-interactive") || cliflags.WasSet(fs, "i") {
		return dispatchInteractiveMode(fs, out)
	}
	if cliflags.ResolveBoolAlias(fs, "continue", "c") {
		return dispatchInteractiveMode(fs, out)
	}
	return dispatchDefaultRoot(args, fs, out)
}

// ---- merged from main_flags.go ----
// main_flags.go — the command-line flag helpers and the model-label
// vocabulary the CLI and the TUI share.
//
// The flag accessors are thin wrappers over internal/core/cliflags (which is the
// single implementation of "was this flag set?" / "what is its value?").
// The model labels are wrappers over internal/features/models/catalog.

// resolvePrintMode returns true if --print, --prompt, -p, or --input-format stream-json was given.
func resolvePrintMode(fs *flag.FlagSet) bool {
	return cliflags.WasSet(fs, "print") || cliflags.WasSet(fs, "prompt") || cliflags.WasSet(fs, "p") || cliflags.StringValue(fs, "input-format") == "stream-json"
}

// printPrompt returns the prompt text passed to --print / --prompt / -p.
func printPrompt(fs *flag.FlagSet) string {
	for _, n := range []string{"print", "prompt", "p"} {
		if cliflags.WasSet(fs, n) {
			return cliflags.StringValue(fs, n)
		}
	}
	return ""
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
// forwarding to internal/core/cliflags, internal/features/models/catalog or
// internal/tuilib. The call sites now name the package directly, so
// there is one hop instead of two, and a new helper in one of those
// packages is reachable without touching this one.
//
// The one alias that stays is the one the TUI reads as a *value*:
// the default model label.

const defaultModel = modelcatalog.DefaultModel
