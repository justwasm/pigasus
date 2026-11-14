// Package clirunner is the root CLI runner entry host.
// It sits above the architecture tiers alongside internal/tui and internal/printmode.
package clirunner

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/justwasm/pigasus/internal/app/prompt/agyprompt"
	"github.com/justwasm/pigasus/internal/app/session/convstore"
	"github.com/justwasm/pigasus/internal/app/session/supervisor"
	"github.com/justwasm/pigasus/internal/app/subcommands"
	"github.com/justwasm/pigasus/internal/app/tools/shell"
	"github.com/justwasm/pigasus/internal/cli"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/core/branding"
	"github.com/justwasm/pigasus/internal/core/cliflags"
	browserflags "github.com/justwasm/pigasus/internal/core/cliflags/browser"
	logfileflags "github.com/justwasm/pigasus/internal/core/cliflags/logfile"
	projectflags "github.com/justwasm/pigasus/internal/core/cliflags/projects"
	confhome "github.com/justwasm/pigasus/internal/core/confhome"
	"github.com/justwasm/pigasus/internal/core/registry"
	"github.com/justwasm/pigasus/internal/core/settings"
	"github.com/justwasm/pigasus/internal/features/agents"
	"github.com/justwasm/pigasus/internal/features/auth"
	modelcatalog "github.com/justwasm/pigasus/internal/features/models/catalog"
	builtinskills "github.com/justwasm/pigasus/internal/features/tools/skills"
	"github.com/justwasm/pigasus/internal/infra/agyproxy"
	"github.com/justwasm/pigasus/internal/infra/backend/gateway"
	backendselect "github.com/justwasm/pigasus/internal/infra/backend/select"
	"github.com/justwasm/pigasus/internal/infra/daemon"
	"github.com/justwasm/pigasus/internal/printmode"
	"github.com/justwasm/pigasus/internal/rchost"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
	featdiff "github.com/justwasm/pigasus/internal/ui/views/diff/feat"
	"github.com/justwasm/pigasus/internal/ui/views/modelpicker"
	"github.com/justwasm/pigasus/internal/ui/views/onboarding"
)

const cliVersion = cli.ReferenceVersion

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

	// Multi-call binary compatibility: if invoked via symlink or binary named
	// "agy-acp-server" or legacy "pigcap", automatically dispatch to the "acp" subcommand.
	if (appName == "agy-acp-server" || appName == "pigcap") && (len(cmdArgs) == 0 || cmdArgs[0] != "acp") {
		cmdArgs = append([]string{"acp"}, cmdArgs...)
	}

	p := SetupCLIParserWithProg(appName)
	if err := p.ExecuteArgs(cmdArgs); err != nil {
		return p.ExitCode()
	}
	return 0
}

// SetupCLIParser builds the full CLI parser with all subcommands registered,
// including the clone's extensions (server, tui, web, proxy).
func SetupCLIParser(prog string) *cli.Parser {
	return SetupCLIParserWithProg(prog)
}

func buildSubcommands() []cliflags.SubcommandSpec {
	// Base standard subcommands from cli (vendor parity + built-in extensions).
	subs := cli.StandardSubcommands()
	names := make(map[string]bool, len(subs)+8)
	for _, s := range subs {
		names[s.Name] = true
	}

	// Dynamic subcommands from internal/app/subcommands registry.
	for _, s := range subcommands.GetSubcommands() {
		if !names[s.Name] {
			subs = append(subs, s)
			names[s.Name] = true
		}
	}

	// Root extensions.
	for _, ext := range []cliflags.SubcommandSpec{proxySubcommand(), cli.WebSubcommand(), cli.ServerSubcommand(), cli.TUISubcommand()} {
		if !names[ext.Name] {
			subs = append(subs, ext)
			names[ext.Name] = true
		}
	}
	return subs
}

// SetupCLIParserWithProg constructs the CLI parser for the given program name.
func SetupCLIParserWithProg(prog string) *cli.Parser {
	if prog == "" {
		prog = filepath.Base(os.Args[0])
	}
	p := cli.NewParser(
		cli.StandardFlags(),
		buildSubcommands(),
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
	chatBridge := func(w io.Writer, fs *flag.FlagSet) error {
		if runner := registry.GetTUIRunner(); runner != nil {
			return runner(w, fs)
		}
		return nil
	}
	cli.SetChatBridge(chatBridge)
	cli.RegisterForegroundHandler(rchost.RunForeground)
	cli.RegisterServeHandler(rchost.RunServe)
	cli.RegisterWebHandler(rchost.RunWeb)
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
		cwd, _ := os.Getwd()
		sup := supervisor.New(cwd)
		if sel.Store != nil {
			sup.SetSeededConversation(sel.Store.Steps, len(sel.History))
		}
		return printmode.ConversationSelection{
			ID:             sel.ID,
			Source:         sel.Source,
			Store:          sel.Store,
			History:        sel.History,
			PriorUserTurns: sel.PriorUserTurns,
			Supervisor:     sup,
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

// RunRoot handles the top-level commands and flags.
func RunRoot(args []string, p *cli.Parser) error {
	return runRoot(args, p)
}

// ValidateRootFlags validates mutual exclusivity, JSON schema, and agent flags.
func ValidateRootFlags(fs *flag.FlagSet, printMode bool) error {
	return validateRootFlags(fs, printMode)
}

// runRoot handles the top-level commands and flags.
func runRoot(args []string, p *cli.Parser) error {
	out := p.Root().Out
	fs := p.Root().FlagSet()
	if fs == nil {
		return nil
	}
	applySessionFlags(fs)
	if done, err := checkRootShortCircuits(fs, p, out); done || err != nil {
		return err
	}
	if v := cliflags.StringValue(fs, "settings"); v != "" {
		overlay, err := settings.ParseAndValidateFlag(v)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %s\n", err)
			return cli.ExitCodeError(1, err.Error())
		}
		settings.SetSessionOverlay(overlay)
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

func resolvePrintMode(fs *flag.FlagSet) bool {
	return cliflags.WasSet(fs, "print") || cliflags.WasSet(fs, "prompt") || cliflags.WasSet(fs, "p") || cliflags.StringValue(fs, "input-format") == "stream-json"
}

func printPrompt(fs *flag.FlagSet) string {
	for _, n := range []string{"print", "prompt", "p"} {
		if cliflags.WasSet(fs, n) {
			return cliflags.StringValue(fs, n)
		}
	}
	return ""
}

func warnAgentMode(fs *flag.FlagSet, sessionCreation bool) {
	if fs == nil {
		return
	}
	if cliflags.StringValue(fs, "agent") != "" && cliflags.StringValue(fs, "mode") != "" {
		fmt.Fprintln(os.Stderr, "warning: --mode is ignored when --agent is set")
	}
}

func checkRootShortCircuits(fs *flag.FlagSet, p *cli.Parser, out io.Writer) (bool, error) {
	if cliflags.BoolValue(fs, "version") {
		fmt.Fprint(out, p.Root().Version)
		return true, nil
	}
	if cliflags.BoolValue(fs, "tools") {
		printTools(out)
		return true, nil
	}
	if cliflags.BoolValue(fs, "services") {
		printServices(out)
		return true, nil
	}
	if cliflags.BoolValue(fs, "oauth") {
		printOAuthConfig(out)
		return true, nil
	}
	if cliflags.BoolValue(fs, "tui") {
		if renderer := registry.GetTUISnapshotRenderer(); renderer != nil {
			return true, renderer(out)
		}
		return true, nil
	}
	if toolName := cliflags.StringValue(fs, "tool"); toolName != "" {
		return true, dispatchTool(out, toolName, cliflags.StringValue(fs, "tool-args"))
	}
	if cliflags.BoolValue(fs, "diff-with-agy") {
		featdiff.RunCLI(out, printmode.PrintOutputFormat(fs))
		return true, nil
	}
	return false, nil
}

func validateRootFlags(fs *flag.FlagSet, printMode bool) error {
	if err := rchost.CheckRemoteControlADC(fs); err != nil {
		return err
	}
	promptInter := cliflags.WasSet(fs, "prompt-interactive") || cliflags.WasSet(fs, "i")
	if printMode && promptInter {
		fmt.Fprintln(os.Stderr, "error: --print and --prompt-interactive are mutually exclusive")
		return cli.ExitCodeError(2, "error: --print and --prompt-interactive are mutually exclusive")
	}
	if err := printmode.ValidateJSONSchemaFlag(fs); err != nil {
		return cli.ExitCodeError(1, err.Error())
	}
	agentErr := agents.ValidateAgent(cliflags.StringValue(fs, "agent"))
	if agentErr != nil && printMode {
		return cli.ExitCodeError(1, agentErr.Error())
	}
	if printMode {
		if _, err := modelcatalog.ResolveModelSelection(cliflags.StringValue(fs, "model"), cliflags.StringValue(fs, "effort")); err != nil {
			outFmt := cliflags.StringValue(fs, "output-format")
			if outFmt == "json" || outFmt == "stream-json" {
				errMsg := strings.TrimPrefix(err.Error(), "error: ")
				envJSON := fmt.Sprintf(`{"conversation_id":"","status":"ERROR","response":"","error":%q,"duration_seconds":0,"num_turns":0,"usage":{"input_tokens":0,"output_tokens":0,"thinking_tokens":0,"cache_read_tokens":0,"total_tokens":0}}`+"\n", errMsg)
				fmt.Fprint(os.Stdout, envJSON)
			}
			return cli.ExitCodeError(1, err.Error())
		}
	}
	return nil
}

func dispatchPrintMode(fs *flag.FlagSet, out io.Writer) error {
	if cliflags.StringValue(fs, "input-format") == "stream-json" {
		warnAgentMode(fs, false)
		return printmode.RunStreamJSON(out, os.Stdin, fs)
	}
	prompt := printPrompt(fs)
	if prompt == "" {
		fmt.Fprintln(os.Stderr, "antigravity: --print requires a positional prompt argument")
		return fmt.Errorf("missing prompt")
	}
	if cliflags.ResolveBoolAlias(fs, "continue", "c") {
		cwd, _ := os.Getwd()
		id, _, err := convstore.ResolveContinue(cwd)
		if err != nil {
			return cli.ExitCodeError(1, fmt.Sprintf("error: --continue: %v", err))
		}
		if id == "" {
			convstore.ContinueNoConversationMessage(out)
			return cli.ExitCodeError(1, "no previous conversation to continue")
		}
	}
	warnAgentMode(fs, false)
	return printmode.Run(out, prompt, fs)
}

func dispatchInteractiveMode(fs *flag.FlagSet, out io.Writer) error {
	warnAgentMode(fs, false)
	if runner := registry.GetTUIRunner(); runner != nil {
		return runner(out, fs)
	}
	return fmt.Errorf("interactive TUI runner not registered")
}

func dispatchDefaultRoot(args []string, fs *flag.FlagSet, out io.Writer) error {
	if len(args) == 0 {
		warnAgentMode(fs, false)
		maybeRunOnboarding(fs, out)
		if runner := registry.GetTUIRunner(); runner != nil {
			return runner(out, fs)
		}
		return fmt.Errorf("interactive TUI runner not registered")
	}
	prompt := strings.Join(args, " ")
	warnAgentMode(fs, false)
	return printmode.Run(out, prompt, fs)
}

func maybeRunOnboarding(fs *flag.FlagSet, out io.Writer) {
	if cliflags.BoolValue(fs, "no-onboarding") || auth.OnboardingCompleted() {
		return
	}
	wz := onboarding.NewWizard()
	wz.MigrationItems = onboarding.DetectGeminiAssets("")
	wz.Out = out
	if err := wz.Run(); err == nil && wz.Step == onboarding.StepDone {
		_ = settings.SaveColorScheme(wz.Selected.String())
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

func dispatchTool(w io.Writer, name, argsJSON string) error {
	var args map[string]any
	if argsJSON == "" {
		argsJSON = "{}"
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return fmt.Errorf("tool: invalid --tool-args JSON: %w", err)
	}
	if name != "run_command" {
		fmt.Fprintf(w, "[tool-stub] %s args=%v\n", name, args)
		return nil
	}
	cmdStr, _ := args["command"].(string)
	if cmdStr == "" {
		return fmt.Errorf("run_command: --tool-args must include {\"command\": \"...\"}")
	}
	argv := shell.DefaultShellArgv(cmdStr)
	if runtime.GOOS == "windows" && shell.SelectWindowsShell() == "" {
		return fmt.Errorf("run_command: no powershell found on PATH (looked for powershell.exe, then pwsh.exe)")
	}
	c := exec.Command(argv[0], argv[1:]...)
	c.Stdout = w
	c.Stderr = w
	return shell.RunWithPTYStdin(c)
}

func printTools(w io.Writer) {
	fmt.Fprintf(w, "Recovered tools (%d):\n", len(cli.RecoveredTools))
	for i, t := range cli.RecoveredTools {
		fmt.Fprintf(w, "  %2d. %s\n", i+1, t)
	}
}

func printServices(w io.Writer) {
	fmt.Fprintf(w, "Recovered gRPC services (%d):\n", len(cli.RecoveredServices))
	for i, s := range cli.RecoveredServices {
		fmt.Fprintf(w, "  %2d. %s\n", i+1, s)
	}
}

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

func proxySubcommand() cliflags.SubcommandSpec {
	names := []string{"doctor", "log", "capture", "balance", "aggregate", "serve", "version", "help"}
	subs := make([]cliflags.SubSubSpec, 0, len(names))
	for _, name := range names {
		subs = append(subs, cliflags.SubSubSpec{
			Name:  name,
			Short: proxySubShort(name),
			RunJ:  proxyRun(name),
		})
	}
	return cliflags.SubcommandSpec{
		Name:            "proxy",
		Short:           "Manage the local CloudCode proxy",
		Subs:            subs,
		UnknownSubError: "Error: unknown command: %s",
		RunJ:            proxyParent,
	}
}

func proxySubShort(name string) string {
	switch name {
	case "doctor":
		return "Report the proxy, the registry and the upstream"
	case "log":
		return "Tail the proxy log"
	case "capture":
		return "Toggle the request/response capture"
	case "balance":
		return "Toggle multi-account load balancing"
	case "aggregate":
		return "Toggle unified virtual account aggregation"
	case "serve":
		return "Run the gateway server in foreground"
	case "version":
		return "Print the proxy and agy versions"
	case "help":
		return "Show help for proxy commands"
	default:
		return ""
	}
}

func proxyRun(name string) func(j *cli.JetskiCLI, args []string) error {
	return func(j *cli.JetskiCLI, args []string) error {
		opts := agyproxy.AdminOptions{Prog: j.Name, Out: j.Out, Err: j.ErrOut}
		if code := agyproxy.Admin(opts, append([]string{name}, args...)); code != 0 {
			return cli.ExitCodeError(code, "")
		}
		return nil
	}
}

func proxyParent(j *cli.JetskiCLI, args []string) error {
	if len(args) > 0 {
		return cli.ExitCodeError(1, fmt.Sprintf("Error: unknown command: %s", args[0]))
	}
	fmt.Fprint(j.ErrOut, proxyUsage(j.Name))
	return cli.ExitCodeError(1, "")
}

func proxyUsage(prog string) string {
	return "Usage: " + prog + " proxy <command> [args]\n" +
		"\n" +
		"Manage the local CloudCode proxy that gives the official agy\n" +
		"access to the configured providers.\n" +
		"\n" +
		"Commands:\n" +
		"  doctor     agy path/version, registry, port, capture, upstream\n" +
		"  log        tail the proxy log\n" +
		"  capture    toggle the request/response capture\n" +
		"  balance    toggle multi-account load balancing\n" +
		"  version    proxy and agy versions\n"
}

func applySessionFlags(fs *flag.FlagSet) {
	browserflags.Apply(fs)
	projectflags.Apply(fs, projectflags.Options{NewID: agyprompt.NewConversationID})
	logfileflags.Apply(fs)
}

func resolveConversationSelection(fs *flag.FlagSet, warn io.Writer) printmode.ConversationSelection {
	convID := cliflags.StringValue(fs, "conversation")
	cont := cliflags.ResolveBoolAlias(fs, "continue", "c")
	cwd, _ := os.Getwd()
	sel := convstore.ResolveSelection(convID, cont, cwd, warn)
	return printmode.ConversationSelection{
		ID:             sel.ID,
		Source:         sel.Source,
		Store:          sel.Store,
		History:        sel.History,
		PriorUserTurns: sel.PriorUserTurns,
	}
}
