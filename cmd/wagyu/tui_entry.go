package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"golang.org/x/term"

	"github.com/justwasm/pigasus/internal/agent"
	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/convstore"
	"github.com/justwasm/pigasus/internal/permissions"
	"github.com/justwasm/pigasus/internal/pluginwatch"
)

func checkWorkspaceTrustGate(w io.Writer, fs *flag.FlagSet) (string, bool, error) {
	canonicalCwd, skipTrust, terr := trustGate(fs)
	if terr != nil {
		fmt.Fprintf(w, "warning: workspace trust check failed: %v\n", terr)
	}
	if !skipTrust {
		res, perr := runTrustPrompt(canonicalCwd, os.Stdin, w)
		if perr != nil {
			return "", false, perr
		}
		if res == trustDeny {
			return "", true, nil
		}
		if err := addTrustedWorkspace(canonicalCwd); err != nil {
			fmt.Fprintf(w, "warning: could not persist trust: %v\n", err)
		}
	}
	return canonicalCwd, false, nil
}

func checkStartupAuthGate() error {
	if !hasOAuthToken() && os.Getenv("GEMINI_API_KEY") == "" && os.Getenv("AGY_ADC_AUTH") == "" {
		loginProg := tea.NewProgram(newLoginModel())
		finalM, err := loginProg.Run()
		if err != nil {
			return err
		}
		if lm, ok := finalM.(*loginModel); !ok || !lm.authenticated {
			return errors.New("authentication required")
		}
	}
	return nil
}

func resolveTUIModelAndBackend(fs *flag.FlagSet) (string, string, bool, backend.Client) {
	modelName := flagStringValue(fs, "model")
	modelName, unavailableFrom := resolveEffectiveModel(modelName)
	if modelName == "" {
		modelName = defaultModel
	}
	altScreen := true
	if flagBoolValue(fs, "no-alt-screen") || !flagBoolValue(fs, "alt-screen") {
		altScreen = false
	}
	be := pickBackend(fs, modelName)
	if ab, ok := be.(agwBackend); ok {
		extModel := strings.TrimSpace(ab.AgwConfig().ExternalModel)
		if isMiniMax(extModel) {
			if !isMiniMax(modelName) {
				modelName = "MiniMax M3"
			}
			unavailableFrom = ""
		}
	}
	return modelName, unavailableFrom, altScreen, be
}

func initTUIRunMode(fs *flag.FlagSet, be backend.Client, modelName, unavailableFrom string, altScreen bool) (*runMode, context.CancelFunc) {
	userEmail := auth.EffectiveUserEmail()
	m := NewRunMode(be, userEmail, modelName, "", "", altScreen)
	m.backendFor = func(label string) backend.Client { return pickBackend(fs, label) }
	if name := flagStringValue(fs, "agent"); name != "" {
		if err := agent.ValidateAgent(name); err != nil {
			var nf *agent.AgentNotFoundError
			if errors.As(err, &nf) {
				m.agentWarning = strings.TrimPrefix(nf.Error(), "error: ") + ". Using the default agent instead."
			}
		}
	}
	m.agentBody = agentBody(customAgentFromFlags(fs))
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
		inner:   &registryRunner{reg: catalog},
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
	resumeHintBanner(canonicalCwd, sel.ID != "",
		resolveBoolAlias(fs, "continue", "c"), os.Stderr)
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
	if err := checkStartupAuthGate(); err != nil {
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

	prog := tea.NewProgram(m, tea.WithColorProfile(resolveColorProfile(os.Environ())))
	m.hyperlinkCaps = hyperlinkCapsFromEnv(
		os.Stderr,
		os.Environ(),
		tmuxHyperlinkProbe(),
		altScreen,
	)
	m.prog = prog
	_, err = prog.Run()
	return err
}

func authResolveSettingsModelOrEmpty() string {
	return strings.TrimSpace(auth.ResolveSettingsModel(""))
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
