package cliapp

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"golang.org/x/term"

	"github.com/justwasm/pigasus/browsercdp"
	"github.com/justwasm/pigasus/internal/app/session/setup"
	"github.com/justwasm/pigasus/internal/cli"
	"github.com/justwasm/pigasus/internal/app/session/convstore"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/core/branding"
	"github.com/justwasm/pigasus/internal/core/cliflags"
	confhome "github.com/justwasm/pigasus/internal/core/confhome"
	"github.com/justwasm/pigasus/internal/core/policy/permissions"
	workspacetrust "github.com/justwasm/pigasus/internal/core/policy/trust"
	settings "github.com/justwasm/pigasus/internal/core/settings"
	accounts "github.com/justwasm/pigasus/internal/features/accounts/core"
	"github.com/justwasm/pigasus/internal/features/agents"
	"github.com/justwasm/pigasus/internal/features/auth"
	backendselect "github.com/justwasm/pigasus/internal/infra/backend/select"
	"github.com/justwasm/pigasus/internal/ui/foundations/history"
	"github.com/justwasm/pigasus/internal/ui/mdrender"
	modelcatalog "github.com/justwasm/pigasus/internal/features/models/catalog"
	pluginwatch "github.com/justwasm/pigasus/internal/features/plugins/watch"
	builtinskills "github.com/justwasm/pigasus/internal/features/tools/skills"
	"github.com/justwasm/pigasus/internal/app/toolrunner"
	"github.com/justwasm/pigasus/internal/infra/daemon"
	"github.com/justwasm/pigasus/internal/ui/views/modelpicker"
	"github.com/justwasm/pigasus/internal/ui/views/loginui"
	"github.com/justwasm/pigasus/internal/infra/memtrim"
	"github.com/justwasm/pigasus/internal/printmode"
	"github.com/justwasm/pigasus/internal/app/tools"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/internal/ui/termguard"
	termcolor "github.com/justwasm/pigasus/internal/ui/render/color"
	"github.com/justwasm/pigasus/palette"
	"github.com/justwasm/pigasus/render"
)

func checkWorkspaceTrustGate(w io.Writer, fs *flag.FlagSet) (string, bool, error) {
	canonicalCwd, skipTrust, terr := workspacetrust.Gate(fs)
	if terr != nil {
		fmt.Fprintf(w, "warning: workspace trust check failed: %v\n", terr)
	}
	if !skipTrust {
		res, perr := workspacetrust.RunTrustPrompt(canonicalCwd, os.Stdin, w)
		if perr != nil {
			return "", false, perr
		}
		if res == workspacetrust.Deny {
			return "", true, nil
		}
		if err := workspacetrust.AddTrustedWorkspace(canonicalCwd); err != nil {
			fmt.Fprintf(w, "warning: could not persist trust: %v\n", err)
		}
	}
	return canonicalCwd, false, nil
}

// checkStartupAuthGate asks for a sign-in when the session has no
// credential yet.
//
// The login screen is a whole-screen view, and the reference runs it
// in the terminal's alternate buffer — so the gate asks for the same
// alternate-screen setting the session itself uses.
func checkStartupAuthGate(fs *flag.FlagSet) error {
	if !backendselect.HasOAuthToken() && os.Getenv("GEMINI_API_KEY") == "" && os.Getenv("AGY_ADC_AUTH") == "" {
		lm := loginui.New()
		lm.AltScreen = resolveAltScreen(fs)
		loginProg := tea.NewProgram(lm)
		finalM, err := loginProg.Run()
		if err != nil {
			return err
		}
		if lm, ok := finalM.(*loginui.Model); !ok || !lm.Authenticated {
			return errors.New("authentication required")
		}
	}
	return nil
}

// resolveAltScreen decides whether the session renders in the terminal's
// alternate screen buffer.
//
// Precedence: the clone's own flags first (they are an explicit request
// on the command line), then the reference's `altScreenMode` setting,
// then the default — the alternate screen.
//
// The setting is read because the reference reads it too. Measured
// against agy 1.4.7 (2026-10-25, raw capture of the terminal bytes
// under a pty): the reference enters the alternate buffer for "always"
// and for an absent key, and stays inline for "never". Every other value
// ("off", "inline", "default", "auto", "none", "false") is rejected by
// the reference's settings validation and falls back to the default.
//
// The session and the login gate have to agree on it: a login screen
// that switches the terminal out of the alternate buffer (or into it)
// while the session is in the other one leaves the terminal in a state
// the user did not ask for.
func resolveAltScreen(fs *flag.FlagSet) bool {
	if cliflags.BoolValue(fs, "no-alt-screen") {
		return false
	}
	if cliflags.WasSet(fs, "alt-screen") {
		return cliflags.BoolValue(fs, "alt-screen")
	}
	return altScreenModeFromSettings()
}

// altScreenModeValue maps a stored `altScreenMode` value to the
// alternate-screen flag. nil (the key is absent) and every value the
// reference does not know mean the default, the alternate screen; only
// "never" means the inline view.
func altScreenModeValue(stored any) bool {
	s, _ := stored.(string)
	return s != "never"
}

// altScreenModeFromSettings reads `altScreenMode` out of settings.json.
// A missing or unreadable file leaves the default in place.
func altScreenModeFromSettings() bool {
	stored, err := settings.Field("altScreenMode")
	if err != nil {
		return true
	}
	return altScreenModeValue(stored)
}

func resolveTUIModelAndBackend(fs *flag.FlagSet) (string, string, bool, backend.Client) {
	modelName := cliflags.StringValue(fs, "model")
	modelName, unavailableFrom := modelcatalog.ResolveEffectiveModel(modelName)
	if modelName == "" {
		modelName = defaultModel
	}
	altScreen := resolveAltScreen(fs)
	be := backendselect.Pick(fs, modelName, modelpicker.ResolveCatalogueID)
	if gb, ok := be.(gatewayBackend); ok {
		extModel := strings.TrimSpace(gb.GatewayConfig().ExternalModel)
		// The transport is the authority on which model will run, so the
		// label follows it. A configured model is asked for under the
		// upstream id the provider spells it with, and the label is
		// whatever the configuration calls it — two configured models
		// must not collapse into one name, or the picker would name a
		// model the endpoint is not serving.
		if label, ok := modelcatalog.WireLabel(extModel); ok {
			modelName = label
			unavailableFrom = ""
		}
	}
	return modelName, unavailableFrom, altScreen, be
}

func initTUIRunMode(fs *flag.FlagSet, be backend.Client, modelName, unavailableFrom string, altScreen bool) (*runMode, context.CancelFunc) {
	userEmail := auth.EffectiveUserEmail()
	m := newRunMode(be, userEmail, modelName, "", "", altScreen)
	// Cycle 309: the background task's completion notification reaches
	// the model through the session's inbox.
	m.installTaskNotifier()
	// Cycle multi-account-switch: the account the session starts on
	// becomes a known account, so /accounts lists it and the user can
	// add a second one and switch back to this one later.
	accounts.RegisterCurrent(userEmail)
	m.backendFor = func(label string) backend.Client { return backendselect.Pick(fs, label, modelpicker.ResolveCatalogueID) }
	if name := cliflags.StringValue(fs, "agent"); name != "" {
		if err := agents.ValidateAgent(name); err != nil {
			var nf *agents.AgentNotFoundError
			if errors.As(err, &nf) {
				m.agentWarning = strings.TrimPrefix(nf.Error(), "error: ") + ". Using the default agent instead."
			}
		}
	}
	m.agentBody = agents.AgentBody(agents.CustomAgentFromFlags(fs))
	if m.agentBody != "" {
		m.agentName = strings.TrimSpace(cliflags.StringValue(fs, "agent"))
	}
	m.reloadVimSettings()
	m.announceQueue.Load()
	// Cycle 286: agy shares prompt history across all sessions
	// globally. When a fresh CLI session starts, pressing Up
	// immediately recalls the most recent prompt from earlier
	// sessions. If the history list is already populated (e.g. by a
	// test), the load preserves the existing entries.
	m.promptHistory.Load(convstore.LoadHistory("", history.Cap))
	m.flags = fs
	if unavailableFrom != "" {
		m.fallbackFrom = unavailableFrom
		m.fallbackTo = modelName
	}
	watchCtx, watchCancel := context.WithCancel(context.Background())
	if w, werr := pluginwatch.New(confhome.Antigravity()); werr == nil && w != nil {
		m.pluginWatcher = w
		pluginWatcherPtr.Store(w)
		go w.Run(watchCtx)
	}
	return m, watchCancel
}

func setupTUIToolsAndPolicy(m *runMode, fs *flag.FlagSet) {
	workspace, _ := os.Getwd()
	if fs != nil {
		var extraWs []string
		for _, dir := range cliflags.GetterStringValues(fs, "add-dir") {
			if abs, err := filepath.Abs(dir); err == nil {
				extraWs = append(extraWs, abs)
			}
		}
		if len(extraWs) > 0 {
			tools.SetExtraWorkspaces(extraWs)
		}
	}
	// The three MCP tools (call_mcp_tool, list_resources, read_resource)
	// need a live MCP connection; the pool is lazy, so this costs nothing
	// when no server is configured.
	setup.InstallMCPProvider(auth.AntigravityHome(), workspace)
	catalog := agentToolRegistry(workspace, m.agentBody)
	m.catalog = catalog
	m.registerSubagentQuestionTool(catalog)
	sessionPolicy := printmode.PrintModePolicy(fs)
	sessionSandbox := printmode.PrintModeSandbox(fs, workspace, m.conversationID)
	m.toolSandbox = sessionSandbox
	m.enableTools(toolrunner.AdvertisedToolDecls(m.agentBody, catalog), &permissionRunner{
		inner:   &toolrunner.RegistryRunner{Reg: catalog},
		policy:  &sessionPolicy,
		sandbox: sessionSandbox,
		hook:    m.hookToolGate,
		post:    m.hookPostToolUse,
	})
	m.mode = sessionPolicy.Mode
	m.policy = &sessionPolicy
	m.input.Placeholder = permissions.ModeBanner(m.mode)
	m.statusLine = "on"
	if v, ok := settings.Bool("statusLine"); ok && !v {
		// The legacy boolean form. The reference reads it as an object
		// without a command, so no script runs and the built-in row
		// stays; the clone additionally hides the row, which is its
		// cycle-105 behaviour.
		m.statusLine = "off"
	}
	m.title = "off"
	if v, ok := settings.Bool("title"); ok && v {
		m.title = "on"
	}
	m.reloadStatusLineConfig()
}

func setupTUIConversation(m *runMode, fs *flag.FlagSet, canonicalCwd string) {
	sel := resolveConversationSelection(fs, os.Stderr)
	if sel.ID != "" {
		m.conversationID = sel.ID
		m.conversationStarted = true
	}
	m.seedConversation(sel.Store)
}

// RunLiveTUI launches the interactive Bubbletea TUI session (or renders static snapshot if non-interactive).
func RunLiveTUI(w io.Writer, fs *flag.FlagSet) error {
	if w == nil {
		w = os.Stdout
	}
	if fs == nil {
		fs = flag.NewFlagSet("pigga", flag.ContinueOnError)
	}
	daemon.InstallSignalHandlers()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		daemon.Global().Shutdown(ctx)
		browsercdp.Shutdown()
	}()

	registerParserBridges()
	_, _ = auth.WriteCompatDefaults()
	// The reference normalises settings.json on every start-up: it
	// rewrites the file in its own format and drops the keys that hold
	// their default value. The error is advisory — a settings file that
	// cannot be normalised must not keep the CLI from starting.
	_ = settings.NormalizeOnStartup()
	_ = cli.RunPluginCleanupOnStartup(confhome.Antigravity())
	_, _ = builtinskills.Install(confhome.Antigravity())
	return runLiveTUI(w, fs)
}

func runLiveTUI(w io.Writer, fs *flag.FlagSet) error {
	// The chat bridge and the remote-control daemon reach the TUI
	// without going through runRoot, so the browser's policy is read
	// from the flag set here too.
	applySessionFlags(fs)
	if !isTerminalInteractive(os.Stdin) {
		return renderTUIWithFlags(w, fs)
	}
	return termguard.Run(func() error {
		canonicalCwd, shouldExit, err := checkWorkspaceTrustGate(w, fs)
		if err != nil || shouldExit {
			return err
		}
		if err := checkStartupAuthGate(fs); err != nil {
			return err
		}
		modelName, unavail, altScreen, be := resolveTUIModelAndBackend(fs)
		m, watchCancel := initTUIRunMode(fs, be, modelName, unavail, altScreen)
		defer watchCancel()
		if m.pluginWatcher != nil {
			defer pluginWatcherPtr.Store(nil)
			defer m.pluginWatcher.Close()
		}
		setupTUIToolsAndPolicy(m, fs)
		if cols, rows, err := term.GetSize(int(os.Stdout.Fd())); err == nil && cols > 0 {
			m.width, m.height = cols, rows
			mdrender.SetCurrentWidth(cols)
		}
		setupTUIConversation(m, fs, canonicalCwd)

		return runTUIProgram(m, fs, altScreen, canonicalCwd)
	})
}

func setupTUIRemoteControl(m *runMode, fs *flag.FlagSet) {
	if m == nil || fs == nil {
		return
	}
	m.remoteControlDesktop = cliflags.BoolValue(fs, "desktop")
	if cliflags.BoolValue(fs, "remote-control") {
		convID := cliflags.StringValue(fs, "conversation")
		if convID == "" && cliflags.ResolveBoolAlias(fs, "continue", "c") {
			sel := resolveConversationSelection(fs, io.Discard)
			convID = sel.ID
		}
		m.handleRemoteControl("", convID)
	}
}

func runTUIProgram(m *runMode, fs *flag.FlagSet, altScreen bool, canonicalCwd string) error {
	prog := tea.NewProgram(m, tea.WithColorProfile(termcolor.Resolve(os.Environ())))
	debugOut := io.Discard
	if isDebugMode(fs) {
		debugOut = os.Stderr
	}
	if cols, rows, err := term.GetSize(int(os.Stdout.Fd())); err == nil && cols > 0 {
		m.width, m.height = cols, rows
	}
	m.hyperlinkCaps = render.ProbeHyperlinkCaps(
		debugOut,
		os.Environ(),
		render.TmuxHyperlinkProbe(debugOut),
		altScreen,
	)
	m.prog = prog
	setupTUIRemoteControl(m, fs)
	// 【一手】 agy 1.4.10: "the CLI now returns about 100 MB of memory
	// left over from startup to the operating system 15 seconds after
	// the interactive UI opens; print and headless modes are
	// unaffected."
	//
	// The trim lives here and not in the print path: the headless runs
	// never build the interactive UI's start-up state, so there is
	// nothing of that size to give back.
	cancelMemTrim := memtrim.After(memtrim.Delay)
	defer cancelMemTrim()
	_, err := prog.Run()
	printTUIExitBanner(m, canonicalCwd)
	return err
}

func printTUIExitBanner(m *runMode, cwd string) {
	convID := ""
	if m != nil && m.conversationID != "" {
		convID = m.conversationID
	} else if cwd != "" {
		convID, _, _ = convstore.ResolveContinue(cwd)
	}
	email := auth.EffectiveUserEmail()
	if email == "" {
		_, email = accounts.Current()
	}
	banner := tuilib.RenderExitBanner(tuilib.ExitBannerOptions{
		BinaryName:     resolveBinaryName(),
		ConversationID: convID,
		LogoLines:      tuilib.BannerLogoLines(),
		AppName:        branding.Current().CLIName,
		Version:        cliVersion,
		Email:          email,
		Cwd:            tuilib.ShortenCwd(cwd),
	})
	// The banner is painted after the session ended, so it wears the
	// scheme the session wore.
	if m != nil {
		restore := palette.PushScheme(m.colorScheme)
		defer restore()
	}
	fmt.Fprint(os.Stderr, banner)
}

func resolveBinaryName() string {
	if env := strings.TrimSpace(os.Getenv("PIGASUS_BIN")); env != "" {
		return env
	}
	raw := strings.TrimSpace(os.Args[0])
	base := filepath.Base(raw)
	if base != "" && base != "." && base != "/" && base != "main" && !strings.HasPrefix(base, "go-build") {
		return base
	}
	if _, err := os.Stat("cmd/pigasus"); err == nil {
		return "pigasus"
	}
	if _, err := os.Stat("cmd/wagyu"); err == nil {
		return "wagyu"
	}
	if branding.Current().Name != "" {
		return strings.ToLower(branding.Current().Name)
	}
	return "pigasus"
}

func isDebugMode(fs *flag.FlagSet) bool {
	if fs != nil {
		if cliflags.BoolValue(fs, "debug") || cliflags.BoolValue(fs, "verbose") {
			return true
		}
		if cliflags.IntValue(fs, "v") > 0 {
			return true
		}
	}
	for _, env := range []string{"PIGASUS_DEBUG", "OPENAGY_DEBUG", "DEBUG"} {
		if v := os.Getenv(env); v != "" && v != "0" && strings.ToLower(v) != "false" {
			return true
		}
	}
	return false
}

func isTerminalInteractive(f *os.File) bool {
	if f == nil {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}
