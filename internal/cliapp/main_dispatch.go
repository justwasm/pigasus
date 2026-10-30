package cliapp

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	pi "github.com/MichaelKinsy/PiG/ai"

	"github.com/justwasm/pigasus/internal/accounts"
	"github.com/justwasm/pigasus/internal/agent"
	"github.com/justwasm/pigasus/internal/agyproxy"
	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/backendselect"
	"github.com/justwasm/pigasus/internal/cli"
	"github.com/justwasm/pigasus/internal/cliflags"
	"github.com/justwasm/pigasus/internal/convstore"
	"github.com/justwasm/pigasus/internal/modelcatalog"
	"github.com/justwasm/pigasus/internal/onboarding"
	"github.com/justwasm/pigasus/internal/permissions"
	"github.com/justwasm/pigasus/internal/persistentrc"
	"github.com/justwasm/pigasus/internal/printmode"
	"github.com/justwasm/pigasus/internal/rchost"
	"github.com/justwasm/pigasus/internal/sandbox"
	"github.com/justwasm/pigasus/internal/toolrunner"
	"github.com/justwasm/pigasus/internal/tools"
	"github.com/justwasm/pigasus/internal/urlchunk"
	"github.com/justwasm/pigasus/render"
	"golang.org/x/term"
)

// checkRootShortCircuits handles flags that print catalog/info and exit.
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
		return true, renderTUI(out)
	}
	if toolName := cliflags.StringValue(fs, "tool"); toolName != "" {
		return true, dispatchTool(out, toolName, cliflags.StringValue(fs, "tool-args"))
	}
	if cliflags.BoolValue(fs, "diff-with-agy") {
		runFeatDiffCLI(out, printmode.PrintOutputFormat(fs))
		return true, nil
	}
	return false, nil
}

// validateRootFlags validates mutual exclusivity, JSON schema, and agent flags.
func validateRootFlags(fs *flag.FlagSet, printMode bool) error {
	if err := rchost.CheckRemoteControlADC(fs); err != nil {
		return err
	}
	promptInter := flagWasSet(fs, "prompt-interactive") || flagWasSet(fs, "i")
	if printMode && promptInter {
		fmt.Fprintln(os.Stderr, "error: --print and --prompt-interactive are mutually exclusive")
		return cli.ExitCodeError(2, "error: --print and --prompt-interactive are mutually exclusive")
	}
	if err := printmode.ValidateJSONSchemaFlag(fs); err != nil {
		return cli.ExitCodeError(1, err.Error())
	}
	agentErr := agent.ValidateAgent(cliflags.StringValue(fs, "agent"))
	if agentErr != nil && printMode {
		return cli.ExitCodeError(1, agentErr.Error())
	}
	if printMode {
		if _, err := modelcatalog.ResolveModelSelection(cliflags.StringValue(fs, "model"), cliflags.StringValue(fs, "effort")); err != nil {
			return cli.ExitCodeError(1, err.Error())
		}
	} else if agentErr != nil && !isTerminalInteractive(os.Stdin) {
		var nf *agent.AgentNotFoundError
		if errors.As(agentErr, &nf) {
			fmt.Fprintf(os.Stderr, "Agent Not Found: %s. Using the default agent instead.\n",
				strings.TrimPrefix(nf.Error(), "error: "))
		}
	}
	return nil
}

// dispatchPrintMode runs the headless print mode turn.
func dispatchPrintMode(fs *flag.FlagSet, out io.Writer) error {
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

// dispatchInteractiveMode runs interactive mode for --prompt-interactive or --continue.
func dispatchInteractiveMode(fs *flag.FlagSet, out io.Writer) error {
	warnAgentMode(fs, false)
	return runLiveTUI(out, fs)
}

// dispatchDefaultRoot handles the launch with no specific mode flag.
func dispatchDefaultRoot(args []string, fs *flag.FlagSet, out io.Writer) error {
	if len(args) == 0 {
		warnAgentMode(fs, false)
		maybeRunOnboarding(fs, out)
		return runLiveTUI(out, fs)
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
	wz.Out = out
	if err := wz.Run(); err == nil && wz.Step == onboarding.StepDone {
		_ = auth.SaveSettingsColorScheme(wz.Selected.String())
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

type permissionRunner struct {
	inner   agent.ToolRunner
	policy  *permissions.Policy
	sandbox sandbox.Sandbox
	Denials []string
}

func (r *permissionRunner) Run(ctx context.Context, call backend.ToolCall) (string, error) {
	runner := toolrunner.PermissionRunner{
		Inner:   r.inner,
		Policy:  r.policy,
		Sandbox: r.sandbox,
		Denials: r.Denials,
	}
	res, err := runner.Run(ctx, call)
	r.Denials = runner.Denials
	return res, err
}

func advertisedToolDecls(agentBody string, reg *tools.Registry) []backend.ToolDecl {
	return toolrunner.AdvertisedToolDecls(agentBody, reg)
}

func toolTarget(call backend.ToolCall) string {
	return toolrunner.ToolTarget(call)
}

func printModePolicy(fs *flag.FlagSet) permissions.Policy {
	return printmode.PrintModePolicy(fs)
}

func printModeSandbox(fs *flag.FlagSet, workspace, conversationID string) sandbox.Sandbox {
	return printmode.PrintModeSandbox(fs, workspace, conversationID)
}

func pigPrintModel(be backend.Client, modelName string) *pi.Model {
	return printmode.PigPrintModel(be, modelName)
}

// dispatchTool is the --tool NAME --tool-args JSON path.
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
	argv := tools.DefaultShellArgv(cmdStr)
	if runtime.GOOS == "windows" && tools.SelectWindowsShell() == "" {
		return fmt.Errorf("run_command: no powershell found on PATH (looked for powershell.exe, then pwsh.exe)")
	}
	c := exec.Command(argv[0], argv[1:]...)
	c.Stdout = w
	c.Stderr = w
	return tools.RunWithPTYStdin(c)
}

// printTools writes the recovered tools catalog to w.
func printTools(w io.Writer) {
	fmt.Fprintf(w, "Recovered tools (%d):\n", len(cli.RecoveredTools))
	for i, t := range cli.RecoveredTools {
		fmt.Fprintf(w, "  %2d. %s\n", i+1, t)
	}
}

// printServices writes the gRPC service catalog to w.
func printServices(w io.Writer) {
	fmt.Fprintf(w, "Recovered gRPC services (%d):\n", len(cli.RecoveredServices))
	for i, s := range cli.RecoveredServices {
		fmt.Fprintf(w, "  %2d. %s\n", i+1, s)
	}
}

// printOAuthConfig writes the static OAuth config skeleton.
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

// RenderTUI prints the static TUI snapshot to w.
func RenderTUI(w io.Writer) error {
	return renderTUI(w)
}

// renderTUI prints the static TUI snapshot.
func renderTUI(w io.Writer) error {
	return renderTUIWithFlags(w, nil)
}

func renderTUIWithFlags(w io.Writer, fs *flag.FlagSet) error {
	email := auth.EffectiveUserEmail()
	if email == "" {
		_, email = accounts.Current()
	}
	model := "Gemini 3.8 Flash (High)"
	// The tier is filled in once the server names it; until then the
	// banner shows the email alone.
	tier := ""
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "/"
	}
	renderBanner(w, email, model, tier, cwd)
	fmt.Fprintln(w, separator)
	if fs != nil && cliflags.BoolValue(fs, "remote-control") {
		if IsBusinessAccount() {
			fmt.Fprintln(w, cWarningBlueOpen+"> /remote-control"+cWarningBlueClose)
			fmt.Fprintln(w, cWarningBlueOpen+"  \u23bf  "+cWarningBlueClose+panelGray238+rchost.SlashADCErrorMessage()+panelGray238C)
			fmt.Fprintln(w, "")
		} else {
			convID := cliflags.StringValue(fs, "conversation")
			if convID == "" && cliflags.ResolveBoolAlias(fs, "continue", "c") {
				sel := resolveConversationSelection(fs, io.Discard)
				convID = sel.ID
			}
			uuid := persistentrc.InstallationUUID()
			hostname := persistentrc.ResolveInstanceName("")
			if hostname == "" {
				hn, err := os.Hostname()
				if err == nil && hn != "" {
					hostname = hn
				}
			}
			if hostname == "" {
				hostname = "localhost"
			}
			chunkWidth := rcURLDefaultWidth
			if f, ok := w.(*os.File); ok {
				if cols, _, err := term.GetSize(int(f.Fd())); err == nil && cols > 0 {
					chunkWidth = cols
				}
			} else if cols, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && cols > 0 {
				chunkWidth = cols
			}
			targetChunk := chunkWidth - len(rcURLIndent) - 1
			if targetChunk < 1 {
				targetChunk = 1
			}
			msg, extra := buildRemoteControlCard(convID, uuid, hostname, func(u string) []string {
				return urlchunk.Rows(u, rcURLIndent, targetChunk, render.HyperlinkCaps{})
			})
			if runtime.GOOS != "darwin" {
				extra = append(extra,
					"  Note: this platform has no launchd to auto-start the background daemon.",
					"  Run `pigasus remote-control run` in another terminal to connect.")
			}
			fmt.Fprintln(w, cWarningBlueOpen+"> /remote-control"+cWarningBlueClose)
			fmt.Fprintln(w, cWarningBlueOpen+"  \u23bf  "+cWarningBlueClose+panelGray238+msg+panelGray238C)
			for _, line := range extra {
				fmt.Fprintln(w, line)
			}
			fmt.Fprintln(w, "")
		}
	}
	fmt.Fprintln(w, "\033[38;2;138;180;248m>\033[39m")
	fmt.Fprintln(w, separator)
	fmt.Fprintln(w, staticStatusRow(model))
	return nil
}

// runOnboardingWizard runs the onboarding wizard.
func runOnboardingWizard(w io.Writer) error {
	wz := onboarding.NewWizard()
	wz.Out = w
	return wz.Run()
}

// tuiSnapshot returns the rendered snapshot string.
func tuiSnapshot() string {
	var sb strings.Builder
	_ = renderTUI(&sb)
	return sb.String()
}

// ---- merged from main_backend.go ----
// main_backend.go — the transport dispatch: which backend serves a model.
//
// The clone has three transports:
//
//   - cloudcode — the reference's own. It serves the six models the server
//     advertises and needs an OAuth token.
//   - gateway  — one configured endpoint, reached with the endpoint, the
//     credential and the upstream model id the user's provider
//     configuration names. It serves the models the user added.
//   - shim     — the deterministic stand-in.
//
// The default is "gateway", and the default *model* decides what that
// means: a model in the reference's catalogue goes to CloudCode, a model the
// user configured goes to its own endpoint. An explicit `--backend gateway`
// forces the configured endpoint even for a reference model.
//
// The rule itself now lives in internal/backendselect; the wrappers here
// keep the clone's own vocabulary (and its tests').

// pickBackend implements the backend dispatch.
func pickBackend(fs *flag.FlagSet, modelName string) backend.Client {
	return pickBackendWithLog(fs, modelName, io.Discard, backend.GatewayTimeoutUnlimited)
}

// pickBackendWithLog is the underlying dispatch with a caller-supplied log writer.
func pickBackendWithLog(fs *flag.FlagSet, modelName string, logWriter io.Writer, gatewayTimeout int) backend.Client {
	return backendselect.PickWithLog(fs, modelName, logWriter, gatewayTimeout, resolveCatalogueID)
}

// isReferenceModel reports whether name resolves to a model the reference
// serves itself — one of the models in its own catalogue.
//
// A model the user configured is not one of those: it has its own endpoint,
// and the configured transport is what reaches it.
func isReferenceModel(name string) bool {
	return backendselect.IsReferenceModel(name, resolveCatalogueID)
}

// resolveBackend decides which backend to use.
func resolveBackend(fs *flag.FlagSet, logWriter io.Writer) (string, bool) {
	return backendselect.Resolve(fs, logWriter)
}

// normaliseBackend maps accepted values to "gateway", "cloudcode" or "shim".
func normaliseBackend(raw string, logWriter io.Writer) string {
	return backendselect.Normalise(raw, logWriter)
}

// hasOAuthToken returns true if an OAuth token exists in env, on disk, or in Keychain.
func hasOAuthToken() bool {
	return backendselect.HasOAuthToken()
}

// authHome returns $XDG_CONFIG_HOME/antigravity or $HOME/.config/antigravity.
func authHome() string {
	return backendselect.AuthHome()
}

type gatewayBackend interface {
	GatewayConfig() backend.GatewayConfig
}

// proxySubcommand is the hidden `proxy` group: the local CloudCode proxy's
// admin commands.
//
//	wagyu proxy doctor     agy path/version, registry, port, capture, upstream
//	wagyu proxy log        tail the proxy log
//	wagyu proxy capture    toggle the request/response capture
//	wagyu proxy version    proxy and agy versions
//
// It is hidden: `agy --help` lists the reference's twelve visible
// subcommands and nothing else, and the proxy is an openagy-only feature.
//
// The provider/model registry is *not* here: it belongs to the clone
// (`wagyu provider`, `wagyu model`), and the proxy only reads it.
func proxySubcommand() cli.SubcommandSpec {
	names := []string{"doctor", "log", "capture", "balance", "aggregate", "version", "help"}
	subs := make([]cli.SubSubSpec, 0, len(names))
	for _, name := range names {
		subs = append(subs, cli.SubSubSpec{
			Name:  name,
			Short: proxySubShort(name),
			RunJ:  proxyRun(name),
		})
	}
	return cli.SubcommandSpec{
		Name:  "proxy",
		Short: "Manage the local CloudCode proxy",
		Subs:  subs,
		// The same wording the other parents use.
		UnknownSubError: "Error: unknown command: %s",
		RunJ:            proxyParent,
	}
}

// proxySubShort is the one-line description of a proxy subcommand.
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
	case "version":
		return "Print the proxy and agy versions"
	case "help":
		return "Show help for proxy commands"
	default:
		return ""
	}
}

// proxyRun runs one proxy admin command and turns its exit status into the
// clone's exit-code error, so the parser maps it to the process status.
func proxyRun(name string) func(j *cli.JetskiCLI, args []string) error {
	return func(j *cli.JetskiCLI, args []string) error {
		opts := agyproxy.AdminOptions{Prog: j.Name, Out: j.Out, Err: j.ErrOut}
		if code := agyproxy.Admin(opts, append([]string{name}, args...)); code != 0 {
			return cli.ExitCodeError(code, "")
		}
		return nil
	}
}

// proxyParent handles `wagyu proxy` with no subcommand: it prints the
// proxy's usage, like every other parent command in the clone.
func proxyParent(j *cli.JetskiCLI, args []string) error {
	if len(args) > 0 {
		return cli.ExitCodeError(1, fmt.Sprintf("Error: unknown command: %s", args[0]))
	}
	fmt.Fprint(j.ErrOut, proxyUsage(j.Name))
	return cli.ExitCodeError(1, "")
}

// proxyUsage is the hidden group's usage block.
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
