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

	tea "charm.land/bubbletea/v2"
	"golang.org/x/term"

	"github.com/justwasm/pigasus/internal/agent"
	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/branding"
	"github.com/justwasm/pigasus/internal/convstore"
	"github.com/justwasm/pigasus/internal/loginui"
	"github.com/justwasm/pigasus/internal/permissions"
	"github.com/justwasm/pigasus/internal/pluginwatch"
	"github.com/justwasm/pigasus/internal/printmode"
	"github.com/justwasm/pigasus/internal/render"
	"github.com/justwasm/pigasus/internal/termcolor"
	"github.com/justwasm/pigasus/internal/toolrunner"
	"github.com/justwasm/pigasus/internal/tuilib"
	"github.com/justwasm/pigasus/internal/workspacetrust"
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
	if !hasOAuthToken() && os.Getenv("GEMINI_API_KEY") == "" && os.Getenv("AGY_ADC_AUTH") == "" {
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
	if flagBoolValue(fs, "no-alt-screen") {
		return false
	}
	if flagWasSet(fs, "alt-screen") {
		return flagBoolValue(fs, "alt-screen")
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
	stored, err := auth.SettingsField("altScreenMode")
	if err != nil {
		return true
	}
	return altScreenModeValue(stored)
}

func resolveTUIModelAndBackend(fs *flag.FlagSet) (string, string, bool, backend.Client) {
	modelName := flagStringValue(fs, "model")
	modelName, unavailableFrom := resolveEffectiveModel(modelName)
	if modelName == "" {
		modelName = defaultModel
	}
	altScreen := resolveAltScreen(fs)
	be := pickBackend(fs, modelName)
	if gb, ok := be.(gatewayBackend); ok {
		extModel := strings.TrimSpace(gb.GatewayConfig().ExternalModel)
		// The transport is the authority on which model will run, so the
		// label follows it. A configured model is asked for under the
		// upstream id the provider spells it with, and the label is
		// whatever the configuration calls it — two configured models
		// must not collapse into one name, or the picker would name a
		// model the endpoint is not serving.
		if label, ok := wireModelLabel(extModel); ok {
			modelName = label
			unavailableFrom = ""
		}
	}
	return modelName, unavailableFrom, altScreen, be
}

func initTUIRunMode(fs *flag.FlagSet, be backend.Client, modelName, unavailableFrom string, altScreen bool) (*runMode, context.CancelFunc) {
	userEmail := auth.EffectiveUserEmail()
	m := NewRunMode(be, userEmail, modelName, "", "", altScreen)
	// Cycle multi-account-switch: the account the session starts on
	// becomes a known account, so /accounts lists it and the user can
	// add a second one and switch back to this one later.
	registerCurrentAccount(userEmail)
	m.backendFor = func(label string) backend.Client { return pickBackend(fs, label) }
	if name := flagStringValue(fs, "agent"); name != "" {
		if err := agent.ValidateAgent(name); err != nil {
			var nf *agent.AgentNotFoundError
			if errors.As(err, &nf) {
				m.agentWarning = strings.TrimPrefix(nf.Error(), "error: ") + ". Using the default agent instead."
			}
		}
	}
	m.agentBody = printmode.AgentBody(printmode.CustomAgentFromFlags(fs))
	if m.agentBody != "" {
		m.agentName = strings.TrimSpace(flagStringValue(fs, "agent"))
	}
	m.reloadVimSettings()
	m.loadAnnouncements()
	m.loadHistory()
	m.flags = fs
	if unavailableFrom != "" {
		m.fallbackFrom = unavailableFrom
		m.fallbackTo = modelName
	}
	watchCtx, watchCancel := context.WithCancel(context.Background())
	if w, werr := pluginwatch.New(auth.AntigravityConfigHome()); werr == nil && w != nil {
		m.pluginWatcher = w
		pluginWatcherPtr.Store(w)
		go w.Run(watchCtx)
	}
	return m, watchCancel
}

func setupTUIToolsAndPolicy(m *runMode, fs *flag.FlagSet) {
	workspace, _ := os.Getwd()
	catalog := agentToolRegistry(workspace, m.agentBody)
	m.registerSubagentQuestionTool(catalog)
	sessionPolicy := printModePolicy(fs)
	sessionSandbox := printModeSandbox(fs, workspace, m.conversationID)
	m.toolSandbox = sessionSandbox
	m.EnableTools(advertisedToolDecls(m.agentBody, catalog), &permissionRunner{
		inner:   &toolrunner.RegistryRunner{Reg: catalog},
		policy:  &sessionPolicy,
		sandbox: sessionSandbox,
	})
	m.mode = sessionPolicy.Mode
	m.policy = &sessionPolicy
	m.input.Placeholder = permissions.ModeBanner(m.mode)
	m.statusLine = "on"
	if v, ok := auth.SettingsBool("statusLine"); ok && !v {
		m.statusLine = "off"
	}
	m.title = "off"
	if v, ok := auth.SettingsBool("title"); ok && v {
		m.title = "on"
	}
	if cmd, ok := loadStatusLineConfig(); ok {
		m.statusLineExternal = true
		m.statusLineCommand = cmd
	}
}

func setupTUIConversation(m *runMode, fs *flag.FlagSet, canonicalCwd string) {
	sel := resolveConversationSelection(fs, os.Stderr)
	if sel.ID != "" {
		m.conversationID = sel.ID
	}
	m.seedConversation(sel.Store)
}

func runLiveTUI(w io.Writer, fs *flag.FlagSet) error {
	if !isTerminalInteractive(os.Stdin) {
		return renderTUI(w)
	}
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
	setupTUIConversation(m, fs, canonicalCwd)

	prog := tea.NewProgram(m, tea.WithColorProfile(termcolor.Resolve(os.Environ())))
	debugOut := io.Discard
	if isDebugMode(fs) {
		debugOut = os.Stderr
	}
	m.hyperlinkCaps = render.ProbeHyperlinkCaps(
		debugOut,
		os.Environ(),
		render.TmuxHyperlinkProbe(debugOut),
		altScreen,
	)
	m.prog = prog
	_, err = prog.Run()
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
		email = "navigaid@gmail.com"
	}
	banner := tuilib.RenderExitBanner(tuilib.ExitBannerOptions{
		BinaryName:     resolveBinaryName(),
		ConversationID: convID,
		LogoLines:      tuilib.BannerLogoLines(),
		AppName:        branding.Current().CLIName,
		Version:        cliVersion,
		Email:          email,
		Cwd:            shortenCwd(cwd),
	})
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
		if flagBoolValue(fs, "debug") || flagBoolValue(fs, "verbose") {
			return true
		}
		if flagIntValue(fs, "v") > 0 {
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

func (m *runMode) reloadResumeList() {
	list, err := convstore.ListConversations()
	if err != nil {
		m.resumeList = nil
		return
	}
	if m.conversationID != "" {
		filtered := list[:0]
		for _, c := range list {
			if c.ID != m.conversationID {
				filtered = append(filtered, c)
			}
		}
		list = filtered
	}
	m.resumeList = list
}
