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
	"strconv"
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
	"github.com/justwasm/pigasus/internal/core/clilog"
	"github.com/justwasm/pigasus/internal/core/confhome"
	"github.com/justwasm/pigasus/internal/core/registry"
	"github.com/justwasm/pigasus/internal/core/settings"
	"github.com/justwasm/pigasus/internal/features/agents"
	"github.com/justwasm/pigasus/internal/features/auth"
	modelcatalog "github.com/justwasm/pigasus/internal/features/models/catalog"
	builtinskills "github.com/justwasm/pigasus/internal/features/tools/skills"
	"github.com/justwasm/pigasus/internal/infra/agyproxy"
	"github.com/justwasm/pigasus/internal/infra/backend/backendselect"
	"github.com/justwasm/pigasus/internal/infra/backend/gateway"
	"github.com/justwasm/pigasus/internal/infra/daemon"
	"github.com/justwasm/pigasus/internal/printmode"
	"github.com/justwasm/pigasus/internal/rchost"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
	featdiff "github.com/justwasm/pigasus/internal/ui/views/diff/feat"
	"github.com/justwasm/pigasus/internal/ui/views/modelpicker"
	"github.com/justwasm/pigasus/internal/ui/views/onboarding"
	"github.com/justwasm/pigasus/palette"
	"golang.org/x/term"
)

const cliVersion = cli.ReferenceVersion

// Run is the main CLI entrypoint.
func Run(args []string) int {
	// `pigasus agy` runs the vendor Antigravity CLI behind the local
	// CloudCode proxy (internal/agyproxy). It supports `pigasus agy`,
	// `pigasus agy@latest`, `pigasus agy@1.5.11`, `pigasus agy@v1.5.11`.
	// It is dispatched before the clone's startup work — compat defaults,
	// plugin cleanup, signal handlers — because the vendor binary owns those,
	// and because the command line must reach it untouched: `pigasus agy --help`
	// prints the vendor's help, not the clone's.
	actualArgs := args
	if len(actualArgs) == 0 {
		actualArgs = os.Args
	}
	if len(actualArgs) > 1 {
		sub := actualArgs[1]
		if sub == "agy" || strings.HasPrefix(sub, "agy@") {
			ver := ""
			if strings.HasPrefix(sub, "agy@") {
				ver = strings.TrimPrefix(sub, "agy@")
			}
			return agyproxy.LaunchWithVersion(ver, actualArgs[2:])
		}
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
	if sc := settings.String("colorScheme"); sc != "" {
		palette.SetScheme(sc)
	}
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
	if appName == "livechat" && (len(cmdArgs) == 0 || cmdArgs[0] != "livechat") {
		cmdArgs = append([]string{"livechat"}, cmdArgs...)
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
		replaced := false
		for i, existing := range subs {
			if existing.Name == s.Name {
				subs[i] = s
				replaced = true
				break
			}
		}
		if !replaced && !names[s.Name] {
			subs = append(subs, s)
			names[s.Name] = true
		}
	}

	// Root extensions.
	for _, ext := range []cliflags.SubcommandSpec{proxySubcommand(), logsSubcommand(), cli.WebSubcommand(), cli.ServerSubcommand(), cli.TUISubcommand(), cli.LiveChatSubcommand()} {
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
	if explicitLog := strings.TrimSpace(cliflags.StringValue(fs, "log-file")); explicitLog != "" {
		clilog.InitWithPath(explicitLog)
	} else {
		clilog.Init()
	}
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

	stdinPrompt, hasStdinPrompt, err := detectStdinPrompt(p.Root().InReader(), p.Root().Name)
	if err != nil {
		return err
	}
	if hasStdinPrompt {
		if err := validateRootFlags(fs, true); err != nil {
			return err
		}
		if cliflags.StringValue(fs, "mode") == "auto" {
			fmt.Fprintln(os.Stderr, cli.AutoUnavailableWarning("auto"))
		}
		return dispatchPrintModeWithPrompt(fs, out, stdinPrompt)
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
	if strings.TrimSpace(os.Getenv("GEMINI_API_KEY")) != "" || modelcatalog.GeminiAPIKeyAvailable() {
		if cliflags.BoolValue(fs, "remote-control") {
			fmt.Fprintln(os.Stderr, "error: --remote-control is not available with a Gemini API key")
			return cli.ExitCodeError(2, "error: --remote-control is not available with a Gemini API key")
		}
		if cliflags.BoolValue(fs, "remote-agent") {
			fmt.Fprintln(os.Stderr, "error: --remote-agent is not available with a Gemini API key")
			return cli.ExitCodeError(2, "error: --remote-agent is not available with a Gemini API key")
		}
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

func detectStdinPrompt(r io.Reader, prog string) (string, bool, error) {
	if r == nil {
		r = os.Stdin
	}
	if f, ok := r.(*os.File); ok {
		if term.IsTerminal(int(f.Fd())) {
			return "", false, nil
		}
	}
	raw, err := io.ReadAll(r)
	if err != nil {
		return "", false, err
	}
	if len(raw) == 0 {
		return "", false, nil
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		if prog == "" {
			prog = filepath.Base(os.Args[0])
		}
		return "", false, cli.ExitCodeError(1, fmt.Sprintf("error: Error: empty prompt. Usage: %s --print \"your prompt here\"", prog))
	}
	return strings.TrimRight(string(raw), "\r\n"), true, nil
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
	return dispatchPrintModeWithPrompt(fs, out, prompt)
}

func dispatchPrintModeWithPrompt(fs *flag.FlagSet, out io.Writer, prompt string) error {
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

func logsSubcommand() cliflags.SubcommandSpec {
	return cliflags.SubcommandSpec{
		Name:  "logs",
		Short: "Show or tail session logs under ~/.openagy/log/",
		RunJ: func(j *cli.JetskiCLI, args []string) error {
			return runLogsSubcommand(j.Out, j.ErrOut, args)
		},
	}
}

func runLogsSubcommand(out, errOut io.Writer, args []string) error {
	if out == nil {
		out = os.Stdout
	}
	if errOut == nil {
		errOut = os.Stderr
	}
	n := 50
	showPath := false
	listFiles := false
	for i := 0; i < len(args); i++ {
		a := strings.TrimSpace(args[i])
		switch {
		case a == "--path" || a == "-p":
			showPath = true
		case a == "--list" || a == "-l":
			listFiles = true
		case a == "-n" && i+1 < len(args):
			i++
			if v, err := strconv.Atoi(args[i]); err == nil && v > 0 {
				n = v
			}
		case strings.HasPrefix(a, "-n="):
			if v, err := strconv.Atoi(strings.TrimPrefix(a, "-n=")); err == nil && v > 0 {
				n = v
			}
		case a == "--help" || a == "-h":
			fmt.Fprintln(out, "Usage: pigasus logs [-n N] [--path] [--list]")
			return nil
		default:
			if v, err := strconv.Atoi(a); err == nil && v > 0 {
				n = v
			}
		}
	}
	if showPath {
		p := clilog.ResolveReadablePath()
		if p == "" {
			p = clilog.Dir()
		}
		fmt.Fprintln(out, p)
		return nil
	}
	if listFiles {
		files, err := clilog.ListFiles(30)
		if err != nil {
			fmt.Fprintf(errOut, "Error listing logs: %v\n", err)
			return cli.ExitCodeError(1, err.Error())
		}
		if len(files) == 0 {
			fmt.Fprintf(out, "No session logs found in %s\n", clilog.Dir())
			return nil
		}
		fmt.Fprintf(out, "Session logs in %s:\n", clilog.Dir())
		for _, f := range files {
			fmt.Fprintf(out, "  %s  (%d bytes, %s)\n", f.Name, f.Size, f.ModTime.Format("2006-01-02 15:04:05"))
		}
		return nil
	}
	p := clilog.ResolveReadablePath()
	if p == "" {
		fmt.Fprintf(out, "No session logs found in %s\n", clilog.Dir())
		return nil
	}
	lines, err := clilog.Tail(p, n)
	if err != nil {
		fmt.Fprintf(errOut, "Error reading %s: %v\n", p, err)
		return cli.ExitCodeError(1, err.Error())
	}
	fmt.Fprintf(out, "==> %s <==\n", p)
	for _, line := range lines {
		fmt.Fprintln(out, line)
	}
	return nil
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
