package tui

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
	"github.com/justwasm/pigasus/browsercdp"
	"github.com/justwasm/pigasus/internal/app/agentturn"
	"github.com/justwasm/pigasus/internal/app/prompt/implicit"
	"github.com/justwasm/pigasus/internal/app/session/convstore"
	"github.com/justwasm/pigasus/internal/app/session/setup"
	"github.com/justwasm/pigasus/internal/app/toolrunner"
	"github.com/justwasm/pigasus/internal/app/tools"
	"github.com/justwasm/pigasus/internal/cli"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/core/branding"
	"github.com/justwasm/pigasus/internal/core/cliflags"
	confhome "github.com/justwasm/pigasus/internal/core/confhome"
	"github.com/justwasm/pigasus/internal/core/policy/permissions"
	workspacetrust "github.com/justwasm/pigasus/internal/core/policy/trust"
	"github.com/justwasm/pigasus/internal/core/settings"
	accounts "github.com/justwasm/pigasus/internal/features/accounts/core"
	"github.com/justwasm/pigasus/internal/features/agents"
	"github.com/justwasm/pigasus/internal/features/auth"
	modelcatalog "github.com/justwasm/pigasus/internal/features/models/catalog"
	pluginwatch "github.com/justwasm/pigasus/internal/features/plugins/watch"
	builtinskills "github.com/justwasm/pigasus/internal/features/tools/skills"
	backendselect "github.com/justwasm/pigasus/internal/infra/backend/select"
	"github.com/justwasm/pigasus/internal/infra/daemon"
	"github.com/justwasm/pigasus/internal/infra/memtrim"
	"github.com/justwasm/pigasus/internal/infra/remoteagentclient"
	"github.com/justwasm/pigasus/internal/printmode"
	"github.com/justwasm/pigasus/internal/ui/foundations/history"
	tuiviewport "github.com/justwasm/pigasus/internal/ui/foundations/viewport"
	"github.com/justwasm/pigasus/internal/ui/mdrender"
	termcolor "github.com/justwasm/pigasus/internal/ui/render/color"
	"github.com/justwasm/pigasus/internal/ui/termguard"
	"github.com/justwasm/pigasus/internal/ui/toolsteps"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/internal/ui/tuimodel"
	"github.com/justwasm/pigasus/internal/ui/views/loginui"
	"github.com/justwasm/pigasus/internal/ui/views/modelpicker"
	"github.com/justwasm/pigasus/internal/ui/views/seasonal"
	"github.com/justwasm/pigasus/palette"
	"github.com/justwasm/pigasus/render"
	"golang.org/x/term"
)

// ---- tui_run.go ----

// tui_run.go — cycle 71 v1.3.5 binary 1:1 TUI (24-bit RGB logo +
// charm.land/bubbles/v2 textarea input component).
//
// user live run 反馈 (cycle 47): "tui 细节不一致, 自己在 tmux 里面对比两者
// tui 交互, 个别体验不一样, close the gap".
//
// user 反馈 (cycle 71): "这个输入组件的结构还是和 agy 不太一样, 它是放在
// 两条横线中间的, 而且是可以扩充大小的, 而当前实现不支持 ctrl+j 多行输入
// 而且光标的位置还是没有跟随输入的文字"
// → 之前是手写 []rune 数组: 单行, ctrl+j 不响应, 光标 X 用 1+len(buffer)
//
//	估算 (multi-byte rune / multi-line 都错位). 本 commit 改:
//	m.composer.input 是 charm.land/bubbles/v2/textarea.Model, MaxHeight=8 +
//	DynamicHeight=true, 自带 ctrl+j / alt+←→ / ctrl+w / ctrl+a/e 等全套
//	编辑键, m.composer.input.Cursor() 精确给出 v.Cursor.X/Y. prompt 行包在两条
//	──── 之间, 跟 /tmp/agy_real_idle.txt /tmp/agy_multiline.txt 1:1.

// logoLines are the four *text* rows of the v1.3.5 binary 1:1 banner —
// the colour matrix that forms the left side of each row now lives in
// logo_palette.go (the logoArt matrix plus the renderLogoLines
// renderer).
//
// Cycle 126: row 1 ("Antigravity CLI <ver>") uses 24-bit RGB
// (38;2;66;133;244) plus bold, matching the ref binary's literal
// output (`script -q /tmp/agycmp/ref-color.txt agy`). Rows 2-4
// (account, model, cwd) use 24-bit RGB (38;2;95;99;104) instead of
// the 256-colour 241 the clone previously emitted. Cycle 88 (and
// later cycles) was correctly paired with the ref binary's escape
// sequence *of that build* (some ref builds sampled through tmux
// emit 256-colour and some emit 24-bit; both decode to the same
// visual shade in tmux's 256-colour cube, but a real terminal
// distinguishes them). The 1.3.6 binary on this machine emits
// 24-bit RGB to a TTY, so the clone now matches.
//
// Cycle 290: the per-cell ANSI sequences used to live inline here as
// `logoBlock` plus a hardcoded `logoLines[4]`. Both were moved to
// logo_palette.go as a 5-row palette-indexed logoArt matrix plus a
// renderer, so this file now only formats the text half of each
// banner row. The `%s` placeholders receive the rendered art row
// from `renderLogoRow(logoArt[i])` (16 cols wide), and 2 spaces pad
// the gutter so banner text begins at column 18.
var logoLines = []string{
	"%s  " + cBlueBoldOpen + "Pigasus CLI " + cliVersion + "\x1b[0m",
	"%s  " + cDimGrayOpen + "%s" + cDimGrayClose,
	"%s  " + cDimGrayOpen + "%s" + cDimGrayClose,
	"%s  " + cDimGrayOpen + "%s" + cDimGrayClose,
}

// separatorWidth is the v1.3.5 binary 1:1 horizontal separator
// width. Cycle 80: ref capture showed 220 chars (cycle 78 used 200
// which was wrong). The v1.3.5 binary renders two full-width
// horizontal rules around the input box — matching the terminal width
// of 220 cols when captured in a 220-col tmux pane.
const separatorWidth = 220

// helpCommandsPageSize is the historical fixed /help window size. The
// live value is computed per terminal height by helpListWindowFor in
// history.go (18 rows at the 30-row geometry). Kept only so the
// cycle-132 constant has a named home; do not add new uses.
const helpCommandsPageSize = 18

// RootModel is the v1.3.5 binary 1:1 single-prompt TUI.
type RootModel struct {
	tuimodel.Base

	session      sessionState
	turn         TurnService
	display      DisplayModel
	commands     CommandsModel
	viewport     uiState
	prompt       PromptModel
	overlays     OverlayModel
	statusLine   StatusLineModel
	conversation ConversationModel
	diff         DiffModel
	auth         AuthModel
	rewind       RewindModel
	activeItems  ActiveItemsModel
	help         HelpModel
	audio        AudioModel
	askQuestion  AskQuestionModel
	tasks        TasksModel
	agents       AgentsModel
	tools        ToolsModel
	remote       RemoteModel
	compaction   CompactionModel
	swarm        SwarmModel
}

type headerCacheKey struct {
	width        int
	seasonal     *seasonal.Event
	email        string
	model        string
	tier         string
	cwd          string
	agentWarning string
	fallbackFrom string
	fallbackTo   string
}

func (m *RootModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// The palette's colours follow the selected scheme while the
	// update runs: the rows this update paints (the streamed text, the
	// tool rows, the panels) wear the scheme's colours.
	restore := palette.PushScheme(m.viewport.colorScheme)
	defer restore()
	if mod, cmd, handled := m.updateLogin(msg); handled {
		return mod, cmd
	}
	if !tuiviewport.IsScrollMsg(msg) {
		m.viewport.rowsCacheDirty = true
		m.invalidateViewCache()
	}
	if mod, cmd, handled := m.updateMouseAndMiscMsg(msg); handled {
		return mod, cmd
	}
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.updateKeyMsg(msg)
	default:
		if mod, cmd, handled := m.updateStreamingMsg(msg); handled {
			return mod, cmd
		}
		if mod, cmd, handled := m.updateTicksAndCompactMsg(msg); handled {
			return mod, cmd
		}
	}
	return m, nil
}

// currentVerbosity returns the active verbosity level.
func (m *RootModel) currentVerbosity() toolsteps.Verbosity {
	if m == nil || m.display.verbosity == "" {
		v := settings.String("verbosity")
		if v == "" {
			return toolsteps.VerbosityMedium
		}
		return toolsteps.ParseVerbosity(v)
	}
	return toolsteps.ParseVerbosity(m.display.verbosity)
}

// effectiveExpanded returns whether tool-step blocks should be drawn expanded
// under the current verbosity setting and user ctrl+o toggle.
func (m *RootModel) effectiveExpanded(verb toolsteps.Verbosity) bool {
	if m == nil {
		return false
	}
	if m.viewport.expandedUserToggled {
		return m.viewport.expanded
	}
	return verb == toolsteps.VerbosityHigh
}

func (m *RootModel) isExpanded() bool {
	return m.effectiveExpanded(m.currentVerbosity())
}

var _ tuimodel.Model = (*RootModel)(nil)

// ---- tui_entry.go ----

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

// unifiedAccessToken is the bearer the remote-agent client sends. It prefers
// the shared credential and lets the normal refresh path keep it fresh.
func unifiedAccessToken(ctx context.Context) (string, error) {
	tok, err := auth.LoadTokenUnified()
	if err != nil {
		return "", err
	}
	if tok == nil || tok.Token.AccessToken == "" {
		return "", auth.ErrNoToken
	}
	if updated, _, rerr := tok.RefreshIfNeeded(ctx); rerr == nil && updated != nil {
		tok = updated
	}
	return tok.Token.AccessToken, nil
}

func initTUIRootModel(fs *flag.FlagSet, be backend.Client, modelName, unavailableFrom string, altScreen bool) (*RootModel, context.CancelFunc) {
	userEmail := auth.EffectiveUserEmail()
	m := newRootModel(be, userEmail, modelName, "", "", altScreen)
	// Cycle 309: the background task's completion notification reaches
	// the model through the session's inbox.
	m.installTaskNotifier()
	m.ensureSubagentRegistry()
	// Cycle multi-account-switch: the account the session starts on
	// becomes a known account, so /accounts lists it and the user can
	// add a second one and switch back to this one later.
	accounts.RegisterCurrent(userEmail)
	m.turn.backendFor = func(label string) backend.Client {
		return backendselect.Pick(fs, label, modelpicker.ResolveCatalogueID)
	}
	if cliflags.BoolValue(fs, "remote-agent") {
		endpoint := strings.TrimSpace(os.Getenv("AICODE_ENDPOINT_URL"))
		if endpoint == "" {
			endpoint = remoteagentclient.DefaultEndpoint
		}
		client := remoteagentclient.New(remoteagentclient.Config{
			BaseURL:   endpoint,
			ProjectID: cliflags.StringValue(fs, "project"),
			TokenFunc: unifiedAccessToken,
		})
		remoteEngine := agentturn.NewRemoteAgentEngine(agentturn.RemoteAgentConfig{
			Client:    client,
			Model:     modelName,
			Workspace: m.session.cwd,
			Project:   cliflags.StringValue(fs, "project"),
		})
		m.turn.engine = remoteEngine
		m.remote.SetAttached(true)
		m.remote.SetFiles(newRemoteFileCache(client))
		m.session.remoteAgent = true
		m.wireRemoteInteractionHandler()
		// Remote skills feed the existing skills surfaces (the / menu and
		// the /skills panel) through the provider, and the cache keeps the
		// menu from blocking on the proxy.
		builtinskills.SetRemoteSkillsProvider(remoteEngine.CachedSkills)
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, _ = remoteEngine.Skills(ctx)
		}()
	}
	if name := cliflags.StringValue(fs, "agent"); name != "" {
		if err := agents.ValidateAgent(name); err != nil {
			var nf *agents.AgentNotFoundError
			if errors.As(err, &nf) {
				m.session.agentWarning = strings.TrimPrefix(nf.Error(), "error: ") + ". Using the default agent instead."
			}
		}
	}
	m.session.agentBody = agents.AgentBody(agents.CustomAgentFromFlags(fs))
	if m.session.agentBody != "" {
		m.session.agentName = strings.TrimSpace(cliflags.StringValue(fs, "agent"))
	}
	m.reloadVimSettings()
	m.overlays.announceQueue.Load()
	// Cycle 286: agy shares prompt history across all sessions
	// globally. When a fresh CLI session starts, pressing Up
	// immediately recalls the most recent prompt from earlier
	// sessions. If the history list is already populated (e.g. by a
	// test), the load preserves the existing entries.
	m.prompt.promptHistory.Load(convstore.LoadHistory("", history.Cap))
	m.session.flags = fs
	if unavailableFrom != "" {
		m.session.fallbackFrom = unavailableFrom
		m.session.fallbackTo = modelName
	}
	watchCtx, watchCancel := context.WithCancel(context.Background())
	if w, werr := pluginwatch.New(confhome.Antigravity()); werr == nil && w != nil {
		m.viewport.pluginWatcher = w
		pluginWatcherPtr.Store(w)
		go w.Run(watchCtx)
	}
	return m, watchCancel
}

func setupTUIToolsAndPolicy(m *RootModel, fs *flag.FlagSet) {
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
	catalog := agentToolRegistry(workspace, m.session.agentBody)
	m.tools.catalog = catalog
	m.registerSubagentQuestionTool(catalog)
	sessionPolicy := printmode.PrintModePolicy(fs)
	sessionSandbox := printmode.PrintModeSandbox(fs, workspace, m.session.conversationID)
	m.tools.sandbox = sessionSandbox
	m.enableTools(toolrunner.AdvertisedToolDecls(m.session.agentBody, catalog), &permissionRunner{
		inner:   &toolrunner.RegistryRunner{Reg: catalog},
		policy:  &sessionPolicy,
		sandbox: sessionSandbox,
		hook:    m.hookToolGate,
		post:    m.hookPostToolUse,
	})
	m.session.mode = sessionPolicy.Mode
	m.session.policy = &sessionPolicy
	m.prompt.input.Placeholder = permissions.ModeBanner(m.session.mode)
	m.statusLine.visible = "on"
	if v, ok := settings.Bool("statusLine"); ok && !v {
		// The legacy boolean form. The reference reads it as an object
		// without a command, so no script runs and the built-in row
		// stays; the clone additionally hides the row, which is its
		// cycle-105 behaviour.
		m.statusLine.visible = "off"
	}
	m.statusLine.title = "off"
	if v, ok := settings.Bool("title"); ok && v {
		m.statusLine.title = "on"
	}
	m.reloadStatusLineConfig()
}

func setupTUIConversation(m *RootModel, fs *flag.FlagSet, canonicalCwd string) {
	sel := resolveConversationSelection(fs, os.Stderr)
	if sel.ID != "" {
		m.session.conversationID = sel.ID
		m.session.conversationStarted = true
	}
	m.seedConversation(sel.Store)
	m.remoteStartupConversation()
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
		m, watchCancel := initTUIRootModel(fs, be, modelName, unavail, altScreen)
		defer watchCancel()
		if m.viewport.pluginWatcher != nil {
			defer pluginWatcherPtr.Store(nil)
			defer m.viewport.pluginWatcher.Close()
		}
		setupTUIToolsAndPolicy(m, fs)
		if cols, rows, err := term.GetSize(int(os.Stdout.Fd())); err == nil && cols > 0 {
			m.viewport.width, m.viewport.height = cols, rows
			mdrender.SetCurrentWidth(cols)
		}
		setupTUIConversation(m, fs, canonicalCwd)

		return runTUIProgram(m, fs, altScreen, canonicalCwd)
	})
}

func setupTUIRemoteControl(m *RootModel, fs *flag.FlagSet) {
	if m == nil || fs == nil {
		return
	}
	m.session.remoteControlDesktop = cliflags.BoolValue(fs, "desktop")
	if cliflags.BoolValue(fs, "remote-control") {
		convID := cliflags.StringValue(fs, "conversation")
		if convID == "" && cliflags.ResolveBoolAlias(fs, "continue", "c") {
			sel := resolveConversationSelection(fs, io.Discard)
			convID = sel.ID
		}
		m.handleRemoteControl("", convID)
	}
}

func runTUIProgram(m *RootModel, fs *flag.FlagSet, altScreen bool, canonicalCwd string) error {
	prog := tea.NewProgram(m, tea.WithColorProfile(termcolor.Resolve(os.Environ())))
	debugOut := io.Discard
	if isDebugMode(fs) {
		debugOut = os.Stderr
	}
	if cols, rows, err := term.GetSize(int(os.Stdout.Fd())); err == nil && cols > 0 {
		m.viewport.width, m.viewport.height = cols, rows
	}
	m.viewport.hyperlinkCaps = render.ProbeHyperlinkCaps(
		debugOut,
		os.Environ(),
		render.TmuxHyperlinkProbe(debugOut),
		altScreen,
	)
	m.turn.prog = prog
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

func printTUIExitBanner(m *RootModel, cwd string) {
	convID := ""
	if m != nil && m.session.conversationID != "" {
		convID = m.session.conversationID
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
		restore := palette.PushScheme(m.viewport.colorScheme)
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

// ---- tui_interrupted.go ----

// tui_interrupted.go — the composer hint for a turn the user cancelled.

// interruptedHintRow renders the reference's hint for a turn the user
// cancelled with Esc.
//
// 【反编译】 agy 1.4.8 renders the line in the *composer* —
// `cli/model/model.(*PromptModel).View` (0x102bb49f0) asks
// `PromptModel.IsInterrupted` (0x102bb4800) and, when the latest turn was
// interrupted, renders `(*PromptModel).interruptedLine` (0x102bb48b0).
// The transcript keeps no error row for a cancelled turn.
//
// The reference's own literal is
//
//	⎿  Interrupted · What should %s do instead?
//
// with the CLI's own name as the argument. The split below — the hook in
// the blue the other "⎿" rows use, the sentence in dim gray — is the
// clone's own styling; the reference paints the whole line with one
// lipgloss style that the decompilation does not name.
func (m *RootModel) interruptedHintRow() string {
	if !m.conversation.turnInterrupted {
		return ""
	}
	line := agentturn.InterruptedHintLine(branding.Current().CLIName)
	const hook = "  ⎿  "
	return cWarningBlueOpen + hook + cWarningBlueClose +
		cDimGrayOpen + strings.TrimPrefix(line, hook) + cDimGrayClose
}

// ---- tui_implicit.go ----

// tui_implicit.go — the reference's implicit-trajectory store.
//
// 【一手】 agy 1.4.7: a `!` command never reaches the conversation. The
// reference records it in a separate, per-workspace store —
// `<gemini-dir>/implicit/<uuid>.pb`, AES-256-GCM encrypted — as a
// `CORTEX_STEP_TYPE_RUN_COMMAND` step, and the cumulative-prompt handler
// later converts that trajectory into chat messages for the next prompt.
//
// The conversation store holds no USER_IMPLICIT record and the brain's
// transcript.jsonl only carries MODEL / SYSTEM / USER_EXPLICIT sources: the
// command lives *only* here. The clone therefore writes the record as the
// command finishes, and it is the only place the clone writes it.

// recordImplicitStep writes a finished shell command into the reference's
// implicit trajectory store.
//
// The step's fields are the reference's own:
//
//   - the command is written twice, as `command_line` and as
//     `proposed_command_line` — the reference writes the same bytes in both
//     for a command the user typed;
//   - `blocking` and `used_ide_terminal` are set for every `!` command;
//   - a canceled command has no exit code: the reference never set the
//     field, so the record leaves it out.
//
// The write is best-effort: the reference writes the trajectory from its
// server goroutine and a failure there does not reach the user either.
func (m *RootModel) recordImplicitStep(cmd, output string, exitCode int, hasExitCode, canceled bool) {
	if cmd == "" {
		return
	}
	workspace := implicit.WorkspaceURI(m.session.cwd)
	now := time.Now()
	step := implicit.Step{
		Status: implicit.StepStatusDone,
		RunCommand: implicit.RunCommand{
			CommandLine:         cmd,
			ProposedCommandLine: cmd,
			CWD:                 m.session.cwd,
			ExitCode:            exitCode,
			HasExitCode:         hasExitCode,
			CombinedOutput:      output,
			Blocking:            true,
			UsedIDETerminal:     true,
		},
		CreatedAt:   now,
		CompletedAt: now,
	}
	if canceled {
		step.Status = implicit.StepStatusCanceled
	}
	_ = implicit.Open(convstore.StoreDir()).Record(workspace, step)
}

// ---- tui_cumulative.go ----

// tui_cumulative.go — the cumulative prompt's implicit-trajectory messages.
//
// 【反编译】 agy 1.4.7: the reference's cumulative-prompt handler
// (`google3/third_party/jetski/prompt/cumulative_prompt_handler/cumulative_prompt_handler`,
// init 0x10225a6f0) converts the implicit trajectory — the `!` commands the
// user ran outside the conversation — into chat messages and carries them
// ahead of the current turn. The messages stay out of the conversation: the
// reference records a `!` command in the implicit trajectory only (see
// tui_implicit.go), and the conversation store holds no record of it.
//
// The clone therefore keeps the converted messages in their own field and
// prepends them to the turn's message list, rather than appending them to
// transcript — the conversation store would then carry a `!` command the
// reference never wrote there.
//
// The converted messages carry no <USER_REQUEST> envelope: the reference
// wraps only what the user typed, and these messages are the reference's own
// rendering of a step.

// collectImplicitMessages asks the session supervisor to fold in the
// implicit trajectory's newly appended steps. The supervisor owns the
// converted messages and WireMessages prepends them to the turn, so the
// host keeps no copy.
func (m *RootModel) collectImplicitMessages() {
	if sup := m.session.sup(); sup != nil {
		sup.CollectImplicit()
	}
}

// ---- tui_compact_spinner.go ----

// tui_compact_spinner.go — the auto-compaction spinner's tick.
//
// # Why the message and the command live together
//
// A tick is one fact: the message the command produces and the message
// the host's update switch handles have to be the *same type*. They were
// not, for two years.
//
// Commit 64e71d67 moved the tick commands into the shared viewer package
// and aliased the /usage panel's message type (`usageQuotaTickMsg`),
// but left the compaction spinner's message behind as a second,
// distinct type in the host. The command produced the shared package's
// type; the host's switch named the host's own. Nothing matched, the
// spinner's frame never advanced, and the compaction status row sat on
// its first Braille glyph for the whole compaction.
//
// The fix is structural rather than a patch: the message and the
// command that arms it live in one file, so a second type cannot be
// introduced beside them. The test in tui_compact_spinner_test.go pins
// the pair.
//
// # Why the host owns it
//
// The compaction spinner is a host-level turn fact: the host owns the
// compaction events themselves (compactStartMsg, compactEndMsg) and
// draws the status row that carries the spinner. The ui tier has no
// owner for it, and the lower tiers do not import the terminal
// framework at all.

// compactSpinnerTickMsg fires every 100 ms while auto-compaction runs.
type compactSpinnerTickMsg struct{}

// compactSpinnerTick arms the next compactSpinnerTickMsg.
func compactSpinnerTick() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return compactSpinnerTickMsg{} })
}

// ---- tui_help.go ----

// Cycle 381: the /help overlay's rendering, its tab list and its
// list navigation moved to internal/ui/views/helppanel. What stayed
// here is the startup banner.
//
// The *themed* banner moved to internal/ui/views/seasonal, which owns
// the event, the art and the sentences; the plain banner stayed here,
// because it draws the host's own logo.
//
// The voice dictation moved to internal/ui/views/voice, which owns the
// phase, the trigger, the recorder and the banner; the host keeps the
// wiring in voice_host.go.

// renderBanner renders the startup banner.
//
// 【一手】 agy 1.4.7: with a seasonal event switched on the banner is
// the event's own art block and gains the seasonal sentence; otherwise
// it is the plain logo.
func renderBanner(w io.Writer, email, model, tier, cwd string) {
	renderBannerWithTheme(w, seasonalTheme(), email, model, tier, cwd)
}

// renderBannerWithTheme renders the banner, themed when ev is not nil.
func renderBannerWithTheme(w io.Writer, ev *seasonal.Event, email, model, tier, cwd string) {
	if ev != nil {
		seasonal.RenderBanner(w, ev, seasonal.State{
			Email:   email,
			Tier:    tier,
			Model:   model,
			Cwd:     tuilib.ShortenCwd(cwd),
			CLIName: branding.Current().CLIName,
			Version: cliVersion,
		})
		return
	}
	artRows := tuilib.RenderLogoLines()
	fmt.Fprintf(w, "%s  "+cBlueBoldOpen+"%s "+cliVersion+"\x1b[0m\n", artRows[0], branding.Current().CLIName)
	emailLine := email
	if tier != "" {
		emailLine += " (" + tier + ")"
	}
	fmt.Fprintf(w, logoLines[1]+"\n", artRows[1], emailLine)
	fmt.Fprintf(w, logoLines[2]+"\n", artRows[2], model)
	fmt.Fprintf(w, logoLines[3]+"\n", artRows[3], tuilib.ShortenCwd(cwd))
	if !tuilib.EnvHideLogoOn() {
		fmt.Fprintln(w, artRows[4])
	} else {
		fmt.Fprintln(w)
	}
}
