package tui

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/justwasm/pigasus/internal/app/agentturn"
	"github.com/justwasm/pigasus/internal/app/prompt/agyprompt"
	"github.com/justwasm/pigasus/internal/app/session/convstore"
	"github.com/justwasm/pigasus/internal/app/session/slashactions"
	browsersubagent "github.com/justwasm/pigasus/internal/app/subagent/browser"
	"github.com/justwasm/pigasus/internal/app/subagenttools"
	"github.com/justwasm/pigasus/internal/app/tools/browser"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/core/cliflags"
	confhome "github.com/justwasm/pigasus/internal/core/confhome"
	"github.com/justwasm/pigasus/internal/core/policy/permissions"
	settings "github.com/justwasm/pigasus/internal/core/settings"
	"github.com/justwasm/pigasus/internal/features/auth"
	modelctx "github.com/justwasm/pigasus/internal/features/models/ctx"
	rclink "github.com/justwasm/pigasus/internal/infra/remote/link"
	persistentrc "github.com/justwasm/pigasus/internal/infra/remote/persistent"
	"github.com/justwasm/pigasus/internal/ui/editor/editorkit"
	"github.com/justwasm/pigasus/internal/ui/editor/vimkeys"
	"github.com/justwasm/pigasus/internal/ui/mdrender"
	"github.com/justwasm/pigasus/internal/ui/toolsteps"
	"github.com/justwasm/pigasus/internal/ui/views/agentspanel"
	"github.com/justwasm/pigasus/internal/ui/views/codesearchpanel"
	"github.com/justwasm/pigasus/internal/ui/views/contextpanel"
	"github.com/justwasm/pigasus/internal/ui/views/loginui"
	"github.com/justwasm/pigasus/internal/ui/views/narration"
	"github.com/justwasm/pigasus/internal/ui/views/statusline"
	"github.com/justwasm/pigasus/internal/ui/views/statusrow"
	"github.com/justwasm/pigasus/internal/ui/views/themepreview"
	"github.com/justwasm/pigasus/internal/ui/views/usagepanel"
	"github.com/justwasm/pigasus/palette"

	atfiles "github.com/justwasm/pigasus/internal/app/prompt/mentions/files"
	"github.com/justwasm/pigasus/internal/app/session/rewind"
	subagent "github.com/justwasm/pigasus/internal/app/subagent/core"
	"github.com/justwasm/pigasus/internal/app/subagent/questions"
	taskregistry "github.com/justwasm/pigasus/internal/app/task"
	"github.com/justwasm/pigasus/internal/cli"
	coreprojects "github.com/justwasm/pigasus/internal/core/cliflags/projects"
	lifecycle "github.com/justwasm/pigasus/internal/core/lifecycle"
	"github.com/justwasm/pigasus/internal/features/models"
	"github.com/justwasm/pigasus/internal/features/models/catalog"
	modelcatalog "github.com/justwasm/pigasus/internal/features/models/catalog"
	plugins "github.com/justwasm/pigasus/internal/features/plugins/core"
	"github.com/justwasm/pigasus/internal/features/tools/skills"
	backendselect "github.com/justwasm/pigasus/internal/infra/backend/select"
	"github.com/justwasm/pigasus/internal/tui/panels"
	tuiviewer "github.com/justwasm/pigasus/internal/ui/foundations/viewer"
	tuiviewport "github.com/justwasm/pigasus/internal/ui/foundations/viewport"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	"github.com/justwasm/pigasus/internal/ui/slashcommands"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/internal/ui/views/accountspanel"
	"github.com/justwasm/pigasus/internal/ui/views/activeitemspanel"
	"github.com/justwasm/pigasus/internal/ui/views/adddirpanel"
	"github.com/justwasm/pigasus/internal/ui/views/changelog"
	"github.com/justwasm/pigasus/internal/ui/views/configpanel"
	"github.com/justwasm/pigasus/internal/ui/views/creditspanel"
	diffaction "github.com/justwasm/pigasus/internal/ui/views/diff/action"
	"github.com/justwasm/pigasus/internal/ui/views/diffpanel"
	"github.com/justwasm/pigasus/internal/ui/views/effortpanel"
	"github.com/justwasm/pigasus/internal/ui/views/feedbackpanel"
	"github.com/justwasm/pigasus/internal/ui/views/helppanel"
	"github.com/justwasm/pigasus/internal/ui/views/hookspanel"
	"github.com/justwasm/pigasus/internal/ui/views/hostactions"
	"github.com/justwasm/pigasus/internal/ui/views/mentionpicker"
	"github.com/justwasm/pigasus/internal/ui/views/modelpicker"
	"github.com/justwasm/pigasus/internal/ui/views/permissionspanel"
	"github.com/justwasm/pigasus/internal/ui/views/pluginsui"
	projectspanel "github.com/justwasm/pigasus/internal/ui/views/projects"
	"github.com/justwasm/pigasus/internal/ui/views/providerpanel"
	"github.com/justwasm/pigasus/internal/ui/views/renamepanel"
	"github.com/justwasm/pigasus/internal/ui/views/rewindpanel"
	"github.com/justwasm/pigasus/internal/ui/views/skillsui"
	"github.com/justwasm/pigasus/internal/ui/views/slashpicker"
	"github.com/justwasm/pigasus/internal/ui/views/subagentquestionpanel"
	"github.com/justwasm/pigasus/internal/ui/views/swarmboard"
	"github.com/justwasm/pigasus/internal/ui/views/taskspanel"
	"github.com/justwasm/pigasus/internal/ui/views/viewerpanel"
	"github.com/justwasm/pigasus/internal/ui/views/voice"
)

var (
	panelGray238  = palette.SubtleOpen
	panelGray238C = "\x1b[39m"
)

var clipboardWrite = slashactions.ClipboardWrite

const codesearchPipeMessage = "Cannot view a pipe, socket, or device; only regular files can be viewed."
const codesearchEmptyMessage = "/codesearch needs a query."

func splitFileLines(data []byte) []string {
	text := strings.TrimSuffix(string(data), "\n")
	if text == "" {
		return nil
	}
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSuffix(line, "\r")
	}
	return lines
}

func editorBaseName(editor string) string {
	name := strings.ToLower(filepath.Base(editor))
	name = strings.TrimSuffix(name, ".exe")
	name = strings.TrimSuffix(name, ".cmd")
	return name
}

func editorLineArgs(editor, path string, line int) []string {
	switch editorBaseName(editor) {
	case "vi", "vim", "nvim", "neovim", "view", "gvim", "mvim", "vimdiff":
		return []string{fmt.Sprintf("+%d", line), path}
	case "code", "code-insiders", "codium", "vscodium":
		return []string{"-g", fmt.Sprintf("%s:%d", path, line)}
	case "subl", "sublime_text":
		return []string{fmt.Sprintf("%s:%d", path, line)}
	}
	return []string{path}
}

// PanelHostSeams adapts RootModel to the panel builders in internal/cliapp/panels.

// Tasks
func (m *RootModel) TaskRegistry() *taskregistry.Registry  { return m.tasks.reg }
func (m *RootModel) KillSelectedTask(t *taskregistry.Task) { m.killSelectedTask(t) }
func (m *RootModel) TerminalSize() (int, int)              { return m.viewport.width, m.viewport.height }
func (m *RootModel) ClosePanel() tea.Cmd                   { return m.closePanel() }
func (m *RootModel) newTasksPanel() *taskspanel.Panel      { return panels.NewTasksPanel(m) }
func (m *RootModel) tasksPanel() *taskspanel.Panel {
	if m.overlays.panel.tp == nil {
		m.overlays.panel.tp = m.newTasksPanel()
	}
	return m.overlays.panel.tp
}

func (m *RootModel) killSelectedTask(t *taskregistry.Task) {
	if t == nil {
		return
	}
	mgr := m.tasks.subagentManager
	if mgr == nil {
		mgr = subagent.NewManager()
	}
	taskregistry.KillByID(m.tasks.reg, t.ID, mgr)
}

func (m *RootModel) openTasksDetail(id string, isSubagent bool) tea.Cmd {
	if m.overlays.panel.kind != panelregistry.Tasks || m.overlays.panel.tp == nil {
		m.overlays.panel = slashPanel{kind: panelregistry.Tasks, cmd: ""}
		m.initOpenedSlashPanel(panelregistry.Tasks)
	}
	if m.overlays.panel.tp != nil {
		m.overlays.panel.tp.OpenTaskDetail(id, isSubagent)
		m.overlays.panel.tp.SetFromActiveItems(true)
	}
	m.activeItems.Panel.SetFocus(false)
	return armTasksTick()
}

func (m *RootModel) tasksStatusSuffix() string {
	taskPart := ""
	reg := m.tasks.reg
	if m.tasks.ctrl != nil && m.tasks.ctrl.Registry() != nil {
		reg = m.tasks.ctrl.Registry()
	}
	if reg != nil {
		taskPart = taskregistry.StatusSuffix(reg.CountRunning())
	}
	planPart := m.enginePlan()
	if planPart != "" {
		if taskPart != "" {
			return taskPart + " · " + planPart
		}
		return " · " + planPart
	}
	return taskPart
}

func (m *RootModel) shouldTickTasks() bool {
	if m.overlays.panel.kind == panelregistry.Tasks {
		return true
	}
	reg := m.tasks.reg
	if m.tasks.ctrl != nil && m.tasks.ctrl.Registry() != nil {
		reg = m.tasks.ctrl.Registry()
	}
	if reg != nil && reg.CountRunning() > 0 {
		return true
	}
	return false
}

func (m *RootModel) startTasksSubscription() {
	if m.tasks.ctrl != nil {
		m.tasks.ctrl.SetOnChange(func() {
			if m.turn.prog != nil {
				m.turn.prog.Send(tasksTickMsg{})
			}
		})
		m.tasks.ctrl.StartSubscription()
		return
	}
	if m.tasks.reg == nil || m.tasks.subscribed {
		return
	}
	ch, cancel := m.tasks.reg.Subscribe()
	m.tasks.cancel = cancel
	m.tasks.subscribed = true
	go func() {
		for ev := range ch {
			_ = ev
			atomic.StoreInt32(&m.tasks.dirty, 1)
			if m.turn.prog != nil {
				m.turn.prog.Send(tasksTickMsg{})
			}
		}
	}()
}

func (m *RootModel) consumeTasksDirty() bool {
	if m.tasks.ctrl != nil {
		return m.tasks.ctrl.ConsumeDirty()
	}
	return atomic.SwapInt32(&m.tasks.dirty, 0) == 1
}

type tasksTickMsg struct{}

func armTasksTick() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return tasksTickMsg{} })
}

// Config
func (m *RootModel) RefreshSeasonalTheme() { m.refreshSeasonalTheme() }
func (m *RootModel) SlashRespond(cmd, msg string, extra ...string) {
	m.slashRespond(cmd, msg, extra...)
}
func (m *RootModel) SeasonalNow() time.Time { return seasonalNow() }
func (m *RootModel) ConfigOverrideFor(row configpanel.Row) (string, bool) {
	return m.configOverrideFor(row)
}
func (m *RootModel) ApplyThemeLive(scheme string) tea.Cmd { return m.applyThemeLive(scheme) }
func (m *RootModel) ApplyAltScreenLive(stored any)        { m.applyAltScreenLive(stored) }
func (m *RootModel) ApplyVerbosityLive(verbosity string)  { m.applyVerbosityLive(verbosity) }
func (m *RootModel) newConfigPanel() *configpanel.Panel   { return panels.NewConfigPanel(m) }
func (m *RootModel) configPanel() *configpanel.Panel {
	if m.overlays.panel.cfg == nil {
		m.overlays.panel.cfg = m.newConfigPanel()
	}
	return m.overlays.panel.cfg
}

func (m *RootModel) applyThemeLive(scheme string) tea.Cmd {
	palette.SetScheme(scheme)
	mdrender.SetColorScheme(scheme)
	promptColor, placeholderColor := themepreview.ColorsFor(scheme)
	if m != nil {
		m.viewport.colorScheme = scheme
		st := m.prompt.input.Styles()
		promptStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(promptColor))
		placeholderStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(placeholderColor))
		st.Focused.Prompt = promptStyle
		st.Focused.Placeholder = placeholderStyle
		st.Blurred.Prompt = promptStyle
		st.Blurred.Placeholder = placeholderStyle
		m.prompt.input.SetStyles(st)
		m.rerenderTranscriptForTheme()
	}
	return tea.ClearScreen
}

func (m *RootModel) applyAltScreenLive(stored any) {
	m.viewport.altScreen = altScreenModeValue(stored)
}

func (m *RootModel) applyVerbosityLive(verbosity string) {
	if m == nil {
		return
	}
	m.display.verbosity = verbosity
	m.viewport.expandedUserToggled = false
	m.rerenderBlocks()
	m.rerenderLiveSteps()
}

func (m *RootModel) configOverrideFor(row configpanel.Row) (string, bool) {
	if m == nil || m.session.flags == nil {
		return "", false
	}
	switch row.SettingsKey {
	case "enableTerminalSandbox":
		if cliflags.BoolValue(m.session.flags, "sandbox") {
			return "(overridden by --sandbox)", true
		}
	case "toolPermission":
		if cliflags.BoolValue(m.session.flags, "dangerously-skip-permissions") {
			return "(overridden by --dangerously-skip-permissions)", true
		}
	case "agentMode":
		if cliflags.StringValue(m.session.flags, "mode") != "" {
			return "(overridden by --mode)", true
		}
	case "acp":
		if cliflags.StringValue(m.session.flags, "acp") != "" {
			return "(overridden by --acp)", true
		}
	}
	return "", false
}

// Permissions
func (m *RootModel) newPermissionsPanel() *permissionspanel.Panel {
	return panels.NewPermissionsPanel(m)
}
func (m *RootModel) permissionsPanel() *permissionspanel.Panel {
	if m.overlays.panel.perm == nil {
		m.overlays.panel.perm = m.newPermissionsPanel()
	}
	return m.overlays.panel.perm
}

// Hooks
func (m *RootModel) newHooksPanel() *hookspanel.Panel { return panels.NewHooksPanel(m) }
func (m *RootModel) hooksPanel() *hookspanel.Panel {
	if m.overlays.panel.hk == nil {
		m.overlays.panel.hk = m.newHooksPanel()
	}
	return m.overlays.panel.hk
}

// Plugins
var pluginWatcherPtr = &plugins.WatcherPtr

func (m *RootModel) newPluginPanel() *pluginsui.Panel { return panels.NewPluginPanel(m) }
func (m *RootModel) pluginPanel() *pluginsui.Panel {
	if m.overlays.panel.plg == nil {
		m.overlays.panel.plg = m.newPluginPanel()
	}
	return m.overlays.panel.plg
}

// openBrowser opens url with the default system browser.
func openBrowser(url string) error {
	if flag.Lookup("test.v") != nil {
		return nil
	}
	if v := os.Getenv("OPENAGY_NO_BROWSER"); v != "" {
		switch v {
		case "1", "true", "TRUE", "True", "yes", "YES":
			return nil
		}
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}

// Credits
func (m *RootModel) OpenURL(url string) error             { return openBrowser(url) }
func (m *RootModel) newCreditsPanel() *creditspanel.Panel { return panels.NewCreditsPanel(m) }
func (m *RootModel) creditsPanel() *creditspanel.Panel {
	if m.overlays.panel.cr == nil {
		m.overlays.panel.cr = m.newCreditsPanel()
	}
	return m.overlays.panel.cr
}

// AddDir
func (m *RootModel) ClosePanelSilent() tea.Cmd { return m.closePanelSilent() }
func (m *RootModel) SubmitAddDir(path string) tea.Cmd {
	msg := tuilib.AddDirFeedback(path)
	m.slashRespond("/add-dir", msg)
	return m.closePanelSilent()
}
func (m *RootModel) newAddDirPanel() *adddirpanel.Panel { return panels.NewAddDirPanel(m) }
func (m *RootModel) addDirPanel() *adddirpanel.Panel {
	if m.overlays.panel.ad == nil {
		m.overlays.panel.ad = m.newAddDirPanel()
	}
	return m.overlays.panel.ad
}

// Feedback
func (m *RootModel) SubmitFeedback() {
	m.submitFeedback()
}

func (m *RootModel) submitFeedback() {
	m.overlays.panel = slashPanel{}
	m.slashRespond("/feedback", "Feedback recorded (openagy has no upload endpoint; saved locally only)")
}
func (m *RootModel) newFeedbackPanel() *feedbackpanel.Panel { return panels.NewFeedbackPanel(m) }
func (m *RootModel) feedbackPanel() *feedbackpanel.Panel {
	if m.overlays.panel.fb == nil {
		m.overlays.panel.fb = m.newFeedbackPanel()
	}
	return m.overlays.panel.fb
}

// Skills
func (m *RootModel) SkillsCatalog() []skillsui.SkillInfo { return slashSkillCatalog() }
func (m *RootModel) TerminalOSC8() bool                  { return m.viewport.hyperlinkCaps.OSC8 }
func (m *RootModel) newSkillsPanel() *skillsui.Panel     { return panels.NewSkillsPanel(m) }
func (m *RootModel) skillsPanel() *skillsui.Panel {
	if m.overlays.panel.sk == nil {
		m.overlays.panel.sk = m.newSkillsPanel()
	}
	return m.overlays.panel.sk
}

// Provider
func (m *RootModel) OpenBrowser(url string) error           { return cli.OpenBrowser(url) }
func (m *RootModel) ClipboardRead() string                  { return clipboardReadText() }
func (m *RootModel) ClipboardWrite(text string) error       { return clipboardWrite(text) }
func (m *RootModel) CurrentModelLabel() string              { return m.panelCurrentModelLabel() }
func (m *RootModel) newProviderPanel() *providerpanel.Panel { return panels.NewProviderPanel(m) }
func (m *RootModel) providerPanel() *providerpanel.Panel {
	if m.overlays.panel.prov == nil {
		m.overlays.panel.prov = m.newProviderPanel()
		m.overlays.panel.prov.Reload()
	}
	return m.overlays.panel.prov
}

// Rename
func (m *RootModel) ConversationName() string { return m.session.conversationName }
func (m *RootModel) SubmitRename(name string) tea.Cmd {
	next, msg := tuilib.RenameFeedback(name, m.session.conversationName)
	if msg != "" {
		m.slashRespond("/rename", msg)
	}
	if next != "" {
		m.session.conversationName = next
	}
	return m.closePanelSilent()
}
func (m *RootModel) newRenamePanel() *renamepanel.Panel { return panels.NewRenamePanel(m) }
func (m *RootModel) renamePanel() *renamepanel.Panel {
	if m.overlays.panel.rn == nil {
		m.overlays.panel.rn = m.newRenamePanel()
	}
	return m.overlays.panel.rn
}

func (m *RootModel) SessionTierFor(email string) (string, bool) {
	if m == nil || !m.session.tierFetched {
		return "", false
	}
	if !strings.EqualFold(m.session.tierAccount, email) {
		return "", false
	}
	return m.session.tier, true
}

func (m *RootModel) startAccountLogin() tea.Cmd {
	lm := loginui.New()
	lm.Width = m.viewport.width
	lm.Height = m.viewport.height
	m.auth.login = lm
	return lm.Init()
}

func (m *RootModel) ApplyActiveAccount(email string) {
	m.session.email = email
	m.forgetSessionTier()
	if m.turn.backendFor != nil {
		if b := m.turn.backendFor(m.session.model); b != nil {
			m.turn.backend = b
		}
	}
}

func (m *RootModel) SignOutActiveAccount() {
	m.session.email = ""
}

func (m *RootModel) forgetSessionTier() {
	m.session.tier = ""
	m.session.tierAccount = ""
	m.session.tierFetched = false
}

// plannerResponses returns the assistant ("planner") responses in order, which
// is what /copy indexes. The reference's command description calls them
// "planner response[s]".
func (m *RootModel) plannerResponses() []string {
	return slashactions.PlannerResponses(m.SessionMessages())
}

// deadcode: staged (test seam for slash_actions_test.go)
func resolveCopy(args string, responses []string) (string, string) {
	return slashactions.ResolveCopy(args, responses)
}

// conversationTitle is the text /title names. The reference's value is
// unrecovered; a /rename name wins, then the first user message, then the
// program name — 【推测】.
func (m *RootModel) conversationTitle() string {
	fallback := ""
	if len(os.Args) > 0 {
		fallback = os.Args[0]
	}
	return slashactions.ConversationTitle(m.session.conversationName, m.SessionMessages(), fallback)
}

// mcpServersFeedback renders /mcp. Recovered: `No MCP servers configured.` for
// the empty case (the reference's default). The per-server line is 【推测】 —
// the reference opens an interactive panel (`MCP servers`, `MCP server %d of
// %d  (enter to search)`) whose layout cannot be captured.
func mcpServersFeedback(cfg *auth.MCPConfig) string {
	return slashactions.MCPServersFeedback(cfg)
}

type browserAgentDoneMsg struct {
	answer string
	err    error
}

var browserAgentFactory = func(m *RootModel) (browser.BrowserSubagentProvider, func(), error) {
	return m.newBrowserAgent()
}

var browserProbe = func(ctx context.Context) error {
	_, err := browsersubagent.Probe(ctx)
	return err
}

const browserAgentTimeout = 10 * time.Minute

func (m *RootModel) runBrowserCommand(args string) (bool, tea.Cmd) {
	task := strings.TrimSpace(args)
	if task == "" {
		m.slashRespond("/browser", "Usage: /browser <task>")
		return true, nil
	}
	runner, closeRunner, err := browserAgentFactory(m)
	if err != nil {
		m.slashRespond("/browser", "The browser agent is not available: "+err.Error())
		return true, nil
	}
	m.slashRespond("/browser", "Running the browser agent…")
	return true, m.runBrowserTask(runner, closeRunner, task)
}

func (m *RootModel) runBrowserTask(runner browser.BrowserSubagentProvider, closeRunner func(), task string) tea.Cmd {
	return func() tea.Msg {
		if closeRunner != nil {
			defer closeRunner()
		}
		ctx, cancel := context.WithTimeout(context.Background(), browserAgentTimeout)
		defer cancel()
		answer, err := runner.RunBrowserTask(ctx, task)
		if err != nil && errors.Is(err, context.DeadlineExceeded) {
			err = fmt.Errorf("the browser agent did not finish within %s", browserAgentTimeout)
		}
		return browserAgentDoneMsg{answer: answer, err: err}
	}
}

func (m *RootModel) updateBrowserAgentDoneMsg(msg browserAgentDoneMsg) {
	if msg.err != nil {
		m.conversation.response = append(m.conversation.response,
			panelGray166+msg.err.Error()+panelGray166C,
			"",
		)
		return
	}
	answer := strings.TrimRight(strings.TrimSpace(msg.answer), "\n")
	if answer == "" {
		m.conversation.response = append(m.conversation.response,
			panelGray238+"(the browser agent returned no answer)"+panelGray238C,
			"",
		)
		return
	}
	for _, line := range strings.Split(answer, "\n") {
		m.conversation.response = append(m.conversation.response, line)
	}
	m.conversation.response = append(m.conversation.response, "")
}

func (m *RootModel) newBrowserAgent() (browser.BrowserSubagentProvider, func(), error) {
	if m.tools.catalog == nil {
		return nil, nil, errors.New("the browser tools are not available in this session")
	}
	if m.session.agentBody != "" {
		return nil, nil, errors.New("this session runs an agent body, which has no browser agent")
	}
	if m.turn.backend == nil {
		return nil, nil, errors.New("no model is selected")
	}
	if err := browserProbe(context.Background()); err != nil {
		return nil, nil, err
	}
	workspace := m.session.cwd
	if workspace == "" {
		workspace = defaultWorkspaceForRootModel()
	}
	agentDir := confhome.Antigravity()
	provider := agentturn.NewBackendProviderAdapter(m.turn.backend, m.session.model)
	runner := browsersubagent.New(subagenttools.Deps{
		Provider: provider,
		Backend:  m.turn.backend,
		Model:    m.session.model,
		CWD:      workspace,
		AgentDir: agentDir,
	}, browsersubagent.Tools(m.tools.catalog))
	return runner, func() {}, nil
}

const rcURLIndent = "     "
const rcURLDefaultWidth = 80

func (m *RootModel) rcURLChunkWidth() int {
	width := m.viewport.width
	if width <= 0 {
		width = rcURLDefaultWidth
	}
	w := width - len(rcURLIndent) - 1
	if w < 1 {
		w = 1
	}
	return w
}

func buildRemoteControlCard(conversationID, uuid, hostname string, urlRows func(string) []string) (string, []string) {
	return buildRemoteControlCardForAccount("", conversationID, uuid, hostname, urlRows)
}

func buildRemoteControlCardForAccount(email, conversationID, uuid, hostname string, urlRows func(string) []string) (string, []string) {
	msg := "Remote control on for this session."
	instanceID := persistentrc.InstanceIDPrefix + uuid + persistentrc.InstanceIDSuffix
	lead := "Open on another device to take over:"
	rawQuery := ""
	if conversationID != "" {
		lead = "Open on another device to continue this conversation:"
		rawQuery = "p=" + url.QueryEscape("c/"+conversationID)
	}
	extra := []string{rcURLIndent + lead}
	if hostname != "" {
		extra = append(extra, rcURLIndent+"Hostname: "+hostname)
	}
	if strings.TrimSpace(email) == "" {
		email = rclink.ActiveAccount()
	}
	browserURL := rclink.BrowserURLForAccount(strings.TrimSpace(email), instanceID, rawQuery)
	if urlRows != nil {
		extra = append(extra, urlRows(browserURL)...)
	} else {
		extra = append(extra, rcURLIndent+browserURL)
	}
	return msg, extra
}

func narrationSessionStart(m *RootModel) time.Time {
	if !m.session.narrationStart.IsZero() {
		return m.session.narrationStart
	}
	return time.Time{}
}

func narrationFeedback(m *RootModel, args string) (text, echo string, mutedNow, already bool) {
	text, echo, mutedNow, already = narration.Feedback(args, narrationSessionStart(m), m.session.narrationOff)
	if mutedNow {
		m.session.narrationOff = true
	}
	return text, echo, mutedNow, already
}

func (m *RootModel) handleNarrationSlash(args string) (bool, tea.Cmd) {
	text, _, mutedNow, already := narrationFeedback(m, args)
	if mutedNow || already {
		return true, nil
	}
	if text != "" {
		for _, ln := range strings.Split(text, "\n") {
			m.conversation.response = append(m.conversation.response, panelGray238+ln+panelGray238C)
		}
		m.conversation.response = append(m.conversation.response, "")
	}
	return true, nil
}

func (m *RootModel) sessionTokenTotals() (prompt, completion int) {
	if m == nil {
		return 0, 0
	}
	return int(atomic.LoadInt64(&m.session.store.SessionPromptTokens)),
		int(atomic.LoadInt64(&m.session.store.SessionCompletionTokens))
}

func (m *RootModel) recordTurnUsage(meta *backend.ChunkMeta) {
	if m == nil || meta == nil {
		return
	}
	atomic.AddInt64(&m.session.store.SessionPromptTokens, int64(meta.PromptTokens))
	atomic.AddInt64(&m.session.store.SessionCompletionTokens, int64(meta.CompletionTokens))
	atomic.StoreInt64(&m.session.store.LastTurnPromptTokens, int64(meta.PromptTokens))
	atomic.StoreInt64(&m.session.store.LastTurnCompletionTokens, int64(meta.CompletionTokens))
}

func (m *RootModel) externalStateFor() *statusline.State {
	if m == nil {
		return nil
	}
	return m.statusLine.ExternalState()
}

func (m *RootModel) ensureExternalState() *statusline.State {
	if m == nil {
		return nil
	}
	return m.statusLine.EnsureExternalState()
}

func (m *RootModel) runExternalStatusLineTick() {
	if m == nil {
		return
	}
	statusCfg := m.statusLine.cfg
	titleCfg := m.statusLine.titleCfg
	if !statusCfg.Active() && !titleCfg.Active() {
		return
	}
	payload, err := json.Marshal(m.buildStatusLinePayloadDoc())
	if err != nil {
		if statusCfg.Active() {
			state := m.ensureExternalState()
			state.SetRendered("", fmt.Errorf("status-line payload marshal: %w", err))
			state.RecordFailure()
		}
		return
	}
	go func() {
		statusline.RenderStatusLine(m.ensureExternalState(), statusCfg, payload)
		statusline.RenderWindowTitle(m.titleStoreFor(), titleCfg, payload)
	}()
}

type statusLineExternalTickMsg struct{}

func armExternalStatusLineTick() tea.Cmd {
	return tea.Tick(statusline.ScriptInterval, func(time.Time) tea.Msg { return statusLineExternalTickMsg{} })
}

func (m *RootModel) maybeArmStatusLineTick() tea.Cmd {
	if m == nil || m.session.store.StatusLineTickArmed {
		return nil
	}
	if !m.statusLineScriptActive() && !m.statusLine.titleCfg.Active() {
		return nil
	}
	m.session.store.StatusLineTickArmed = true
	return armExternalStatusLineTick()
}

func (m *RootModel) externalRenderOK() bool {
	if m == nil {
		return false
	}
	return m.statusLine.ExternalRenderOK()
}

func (m *RootModel) externalRenderedForRow() (string, error) {
	if !m.externalRenderOK() {
		return "", nil
	}
	state := m.externalStateFor()
	if state == nil {
		return "", nil
	}
	out, lastErr := state.Current()
	if strings.TrimSpace(out) == "" {
		return "", nil
	}
	return out, lastErr
}

func (m *RootModel) buildStatusLinePayload() statusline.Payload {
	model := m.session.statusModel
	if model == "" {
		model = m.session.model
	}
	contextWindow := 0
	if model != "" {
		contextWindow = modelctx.PublishedContextWindow(model)
	}
	taskCount := 0
	if m.tasks.reg != nil {
		taskCount = m.tasks.reg.CountRunning()
	}
	prompt, completion := m.sessionTokenTotals()
	return statusline.PayloadFrom(statusline.PayloadState{
		Cwd:                      m.session.cwd,
		ConversationID:           m.session.conversationID,
		ConversationTitle:        m.conversationTitle(),
		Model:                    m.session.model,
		Effort:                   m.currentEffortName(),
		AgentName:                m.session.agentName,
		CycleMode:                m.statusModeLabel(),
		PlanTier:                 m.session.tier,
		Email:                    m.session.email,
		TerminalWidth:            m.viewport.width,
		Streaming:                m.conversation.streaming,
		ToolInFlight:             m.toolInFlight(),
		TurnFailed:               m.turnFailed(),
		SandboxEnabled:           m.tools.sandbox.Enabled,
		Quota:                    statusline.QuotaBuckets(m.cachedQuota(m.session.email), time.Now()),
		TaskCount:                taskCount,
		PendingInputCount:        m.pendingInputCount(),
		Subagents:                m.subagentInfos(),
		VimMode:                  m.vimModeName(),
		SessionPromptTokens:      prompt,
		SessionCompletionTokens:  completion,
		LastTurnPromptTokens:     int(atomic.LoadInt64(&m.session.store.LastTurnPromptTokens)),
		LastTurnCompletionTokens: int(atomic.LoadInt64(&m.session.store.LastTurnCompletionTokens)),
		ContextWindowSize:        contextWindow,
		PricingModel:             model,
	})
}

func (m *RootModel) agentState() string {
	state := statusline.AgentState(m.conversation.streaming, m.toolInFlight(), m.turnFailed())
	if state == statusline.StateIdle {
		return ""
	}
	return state
}

func (m *RootModel) toolInFlight() bool {
	return m.conversation.activeTool != ""
}

func (m *RootModel) turnFailed() bool {
	return m.display.lastTurnOutcome == tea.ProgramStateError
}

func (m *RootModel) pendingInputCount() int {
	n := len(m.session.store.StreamQueue)
	if m.session.store.QueuedPrompt != "" {
		n++
	}
	return n
}

func (m *RootModel) subagentInfos() []statusline.SubagentInfo {
	if m.agents.registry == nil {
		return nil
	}
	return statusline.Subagents(m.agents.registry.States())
}

func (m *RootModel) vimModeName() string {
	if !m.vimEnabled() {
		return ""
	}
	return statusline.VimModeName(m.vimMode())
}

func (m *RootModel) contextUsedPercent() float64 {
	if m == nil {
		return 0
	}
	model := m.session.statusModel
	if model == "" {
		model = m.session.model
	}
	if model == "" {
		return 0
	}
	// PromptTokens in the latest turn represents the active context window size.
	// Accumulating session prompt tokens across all turns calculates total billing usage,
	// which would overflow the single-turn context window and result in 3000%+ figures.
	prompt := int(atomic.LoadInt64(&m.session.store.LastTurnPromptTokens))
	if prompt <= 0 {
		prompt, _ = m.sessionTokenTotals()
	}
	return statusline.ContextUsedPercent(prompt, modelctx.ContextWindowFor(model))
}

// deadcode: staged (context usage percentage seam tested in engine_plan_test.go)
func (m *RootModel) contextUsage() float64 {
	if m == nil {
		return 0
	}
	return m.contextUsedPercent()
}

type StatusLinePayloadWithPlan struct {
	statusline.Payload
	Plan string `json:"plan,omitempty"`
}

func (m *RootModel) buildStatusLinePayloadDoc() any {
	base := m.buildStatusLinePayload()
	plan := m.enginePlan()
	if plan == "" {
		return base
	}
	return StatusLinePayloadWithPlan{
		Payload: base,
		Plan:    plan,
	}
}

// deadcode: staged (test seam for engine_plan_test.go)
func (m *RootModel) statusLinePlanProgress() string {
	return m.enginePlan()
}

func (m *RootModel) reloadStatusLineConfig() {
	m.statusLine.Reload()
}

func (m *RootModel) statusLineScriptActive() bool {
	return m.statusLine.ScriptActive()
}

func (m *RootModel) statusAreaState() statusline.AreaState {
	return m.statusLine.AreaState(statusrow.Render(m.statusRowState()))
}

func (m *RootModel) windowTitleText() string {
	if m.statusLine.titleCfg.Active() {
		if title := m.titleStoreFor().Get(); title != "" {
			return title
		}
	}
	if m.statusLine.title == "on" {
		return m.conversationTitle()
	}
	return ""
}

// DiffTurnCount
func (m *RootModel) diffTurnCount() int {
	return m.DiffTurnCount()
}
func (m *RootModel) SessionCwd() string { return m.session.cwd }
func (m *RootModel) DiffTurnCount() int { return m.diffTurnCount() }
func (m *RootModel) diffPanel() *diffpanel.Panel {
	return m.diff.Panel(m)
}

func (m *RootModel) effortTiers() []string {
	return catalog.EffortTiersFor(m.effortModel())
}

func (m *RootModel) effortModel() string {
	if label := strings.TrimSpace(m.session.model); label != "" && catalog.ModelAvailable(label) {
		return label
	}
	if effective, _ := catalog.ResolveEffectiveModel(""); strings.TrimSpace(effective) != "" {
		return effective
	}
	return catalog.DefaultModel
}

func (m *RootModel) EffortTiers() []string {
	return m.effortTiers()
}

func (m *RootModel) CurrentEffortName() string {
	return m.currentEffortName()
}
func (m *RootModel) ApplyEffortSelection(level int) tea.Cmd { return m.applyEffortSelection(level) }
func (m *RootModel) newEffortPanel() *effortpanel.Panel     { return panels.NewEffortPanel(m) }
func (m *RootModel) effortPanel() *effortpanel.Panel {
	if m.overlays.panel.ef == nil {
		m.overlays.panel.ef = m.newEffortPanel()
	}
	return m.overlays.panel.ef
}

// Help
func (m *RootModel) ModelName() string      { return m.session.model }
func (m *RootModel) TierName() string       { return m.session.tier }
func (m *RootModel) UserEmail() string      { return m.session.email }
func (m *RootModel) ConversationID() string { return m.session.conversationID }
func (m *RootModel) AppendResponse(lines []string) {
	m.conversation.response = append(m.conversation.response, lines...)
}
func (m *RootModel) SelectCommand(cmd string) {
	m.prompt.input.SetValue(cmd)
	m.prompt.input.CursorEnd()
	m.prompt.picker.Reset()
	m.prompt.promptHistory.Reset()
	m.ResetInputSelection()
}
func (m *RootModel) newHelpPanel() *helppanel.Panel { return panels.NewHelpPanel(m) }

func (m *RootModel) helpPanel() *helppanel.Panel {
	if m.help.Panel == nil {
		m.help.Panel = m.newHelpPanel()
	}
	return m.help.Panel
}

func (m *RootModel) openHelp(tab int) {
	hp := m.helpPanel()
	hp.Open(tab)
	m.overlays.panel = slashPanel{kind: panelregistry.Help, cmd: "/help"}
}

// ActiveItems
func (m *RootModel) OpenTasksDetail(id string, isSubagent bool) { m.openTasksDetail(id, isSubagent) }
func (m *RootModel) newActiveItemsPanel() *activeitemspanel.Panel {
	return panels.NewActiveItemsPanel(m)
}
func (m *RootModel) SwarmItem() *activeitemspanel.SwarmItem {
	if m == nil || m.swarm.dismissed {
		return nil
	}
	if m.swarm.item != nil {
		return m.swarm.item
	}
	if m.swarm.Board != nil && (m.swarm.Board.PostCount() > 0 || m.swarm.Board.Title() != "") {
		latestText := ""
		if post, ok := m.swarm.Board.LatestPost(); ok {
			latestText = post.Body
		}
		return &activeitemspanel.SwarmItem{
			Title:     m.swarm.Board.Title(),
			Latest:    latestText,
			PostCount: m.swarm.Board.PostCount(),
			IsDone:    m.swarm.Board.IsDone(),
		}
	}
	return nil
}
func (m *RootModel) OpenSwarmBoard() {
	if m == nil {
		return
	}
	if m.swarm.Board == nil {
		m.swarm.Board = m.newSwarmBoard()
	}
	m.swarm.Board.Open()
}
func (m *RootModel) DismissSwarm() {
	if m == nil {
		return
	}
	m.swarm.dismissed = true
}
func (m *RootModel) newSwarmBoard() *swarmboard.Board {
	return swarmboard.NewBoard(func(convID string) {
		m.swarm.fromBoard = true
		m.openTasksDetail(convID, true)
	})
}

// Accounts
func (m *RootModel) StartAccountLogin() tea.Cmd             { return m.startAccountLogin() }
func (m *RootModel) FetchPlanTierCmd() tea.Cmd              { return m.fetchPlanTierCmd() }
func (m *RootModel) FetchQuotaCmd(email string) tea.Cmd     { return fetchAccountQuotaCmd(email) }
func (m *RootModel) newAccountsPanel() *accountspanel.Panel { return panels.NewAccountsPanel(m) }
func (m *RootModel) accountsPanel() *accountspanel.Panel {
	if m.overlays.panel.acct == nil {
		m.overlays.panel.acct = m.newAccountsPanel()
	}
	return m.overlays.panel.acct
}

// Agents
func (m *RootModel) ActiveAgentName() string                { return m.session.agentName }
func (m *RootModel) RunningSubagents() []*subagent.Subagent { return m.runningSubagents() }
func (m *RootModel) HasSessionTurns() bool                  { return m.session.messageCount() > 0 }
func (m *RootModel) ApplyAgentSwitch(name, body string) {
	m.applyAgentSwitch(name, body)
	m.overlays.panel = slashPanel{}
}
func (m *RootModel) KillAgentsSubagent(sa *subagent.Subagent) bool { return m.killAgentsSubagent(sa) }
func (m *RootModel) PendingSubagentAuthorization(id string) (lifecycle.PendingApproval, bool) {
	return m.pendingSubagentAuthorization(id)
}
func (m *RootModel) AnswerSubagentAuthorization(id string, approved bool) bool {
	return m.answerSubagentAuthorization(id, approved)
}
func (m *RootModel) AgentsWorkspace() string { return m.panelAgentsWorkspace() }
func (m *RootModel) newAgentsPanel() *agentspanel.Panel {
	return panels.NewAgentsPanel(m)
}
func (m *RootModel) agentsPanel() *agentspanel.Panel {
	if m.overlays.panel.ag == nil {
		m.overlays.panel.ag = m.newAgentsPanel()
	}
	return m.overlays.panel.ag
}
func (m *RootModel) runningSubagents() []*subagent.Subagent {
	mgr := m.tasks.subagentManager
	if mgr == nil {
		return nil
	}
	return mgr.List()
}
func (m *RootModel) killAgentsSubagent(sa *subagent.Subagent) bool {
	mgr := m.tasks.subagentManager
	if mgr == nil || sa == nil {
		return false
	}
	return mgr.KillRunning(sa.ID)
}
func (m *RootModel) pendingSubagentAuthorization(subagentID string) (lifecycle.PendingApproval, bool) {
	mgr := m.tasks.subagentManager
	if mgr == nil {
		return lifecycle.PendingApproval{}, false
	}
	return mgr.PendingAuthorization(subagentID)
}
func (m *RootModel) answerSubagentAuthorization(approvalID string, approve bool) bool {
	mgr := m.tasks.subagentManager
	if mgr == nil {
		return false
	}
	return mgr.AnswerAuthorization(approvalID, approve)
}
func (m *RootModel) panelAgentsWorkspace() string {
	workspace := m.session.cwd
	if workspace == "" {
		workspace, _ = os.Getwd()
	}
	return workspace
}

// Codesearch
func (m *RootModel) SearchRoot() string    { return m.session.cwd }
func (m *RootModel) SearchStreaming() bool { return m.display.streamingSearch }
func (m *RootModel) CodesearchReadFile(path string) ([]string, string, bool) {
	return m.codesearchReadFile(path)
}
func (m *RootModel) CodesearchOpenEditor(path string, line int) tea.Cmd {
	return m.codesearchOpenEditor(path, line)
}
func (m *RootModel) CodesearchSendComments(text string) tea.Cmd {
	return m.codesearchSendComments(text)
}
func (m *RootModel) newCodesearchPanel() *codesearchpanel.Panel {
	return panels.NewCodesearchPanel(m)
}
func (m *RootModel) codesearchPanel() *codesearchpanel.Panel {
	if m.overlays.panel.csp == nil {
		m.overlays.panel.csp = m.newCodesearchPanel()
	}
	return m.overlays.panel.csp
}
func (m *RootModel) codesearchReadFile(path string) ([]string, string, bool) {
	full := m.codesearchPath(path)
	info, err := os.Stat(full)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Sprintf("File not found: %s", full), false
		}
		return nil, err.Error(), false
	}
	if !info.Mode().IsRegular() {
		return nil, codesearchPipeMessage, false
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return nil, err.Error(), false
	}
	return splitFileLines(data), "", true
}
func (m *RootModel) codesearchPath(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(m.session.cwd, path)
}
func (m *RootModel) codesearchOpenEditor(path string, line int) tea.Cmd {
	name, args, notice, err := editorkit.ResolveEditor(settings.String("editor"))
	if err != nil {
		m.conversation.response = append(m.conversation.response, "Failed to resolve editor: "+err.Error())
		return nil
	}
	if _, err := exec.LookPath(name); err != nil {
		m.conversation.response = append(m.conversation.response, "Failed to resolve editor: "+err.Error())
		return nil
	}
	if notice != "" {
		m.conversation.response = append(m.conversation.response, notice)
	}
	full := m.codesearchPath(path)
	c := exec.Command(name, append(args, editorLineArgs(name, full, line)...)...)
	c.Dir = m.session.cwd
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return editorFinishedMsg{err: err}
	})
}
func (m *RootModel) codesearchSendComments(text string) tea.Cmd {
	closed := m.closePanel()
	if strings.TrimSpace(text) == "" {
		return closed
	}
	return tea.Batch(closed, m.startModelTurn(text))
}

// Model
// statusModeLabel is the agent mode the built-in status row and the status
// line's `cycle_mode` name — the mode's own word, or "" while the session
// has no configured mode.
//
// When running in the default mode (no agentMode set in settings or flags),
// agy 1.5.8 omits the mode label from the status line. When explicitly configured
// (e.g. {"agentMode": "auto"} in settings/--settings) or cycled away from start,
// agy explicitly renders "auto · " (in cyan).
func (m *RootModel) statusModeLabel() string {
	mode := strings.TrimSpace(m.session.mode)
	if mode == "" {
		return ""
	}
	if mode == permissions.ModeAuto && !settings.Has("agentMode") && m.session.startMode == permissions.ModeAuto {
		return ""
	}
	return mode
}

func modeLabelFrom(mode, start string) string {
	mode = strings.TrimSpace(mode)
	if mode == "" || mode == strings.TrimSpace(start) {
		return ""
	}
	return mode
}

// promptPlaceholder is the composer's placeholder for a session: the live
// mode's banner, measured against the mode the session started in.
//
// agy 1.5.8 starts in Auto with an empty prompt and shows nothing, while a session
// that started in Default names the mode it cycles to.
func (m *RootModel) promptPlaceholder(mode string) string {
	return permissions.ModeBanner(modeLabelFrom(mode, m.session.startMode))
}

func (m *RootModel) ApplyModel(label string) {
	m.session.model = label
	m.session.statusModel = tuilib.StripTier(label)
	m.syncBackendForModel()
}
func (m *RootModel) CustomModelRows() []modelpicker.Row { return m.engineModelRows() }
func (m *RootModel) engineModelRows() []modelpicker.Row {
	if len(m.display.engineModelRowsCache) > 0 {
		return m.display.engineModelRowsCache
	}
	eng := m.turnEngine()
	if eng == nil {
		return nil
	}
	lister, ok := eng.(agentturn.ModelLister)
	if !ok {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	models, err := lister.ListModels(ctx)
	if err != nil || len(models) == 0 {
		return nil
	}
	rows := make([]modelpicker.Row, 0, len(models))
	for _, info := range models {
		rows = append(rows, modelpicker.Row{
			Display:     info.DisplayName,
			Slug:        info.ID,
			Note:        info.Description,
			Tiers:       info.Tiers,
			DefaultTier: info.DefaultTier,
		})
	}
	m.display.engineModelRowsCache = rows
	return rows
}

func (m *RootModel) syncBackendForModel() {
	if m.turn.backendFor == nil || strings.TrimSpace(m.session.model) == "" {
		return
	}
	if b := m.turn.backendFor(m.session.model); b != nil {
		m.turn.backend = b
		if backend.IsMock(b) && !backendselect.IsReferenceModel(m.session.model, modelpicker.ResolveCatalogueID) {
			m.session.modelWarning = fmt.Sprintf("Model %q is not configured in %s. Falling back to mock shim.", m.session.model, models.ProvidersPath())
		} else {
			m.session.modelWarning = ""
		}
	}
	m.session.wireSettingsPending = true
}
func (m *RootModel) newModelPanel() *modelpicker.Panel { return panels.NewModelPanel(m) }
func (m *RootModel) modelPanel() *modelpicker.Panel {
	if m.overlays.panel.mdl == nil {
		m.overlays.panel.mdl = m.newModelPanel()
	}
	return m.overlays.panel.mdl
}

// Viewer
func (m *RootModel) ViewerSpec() (tuiviewer.Spec, bool) {
	return m.getPanelRegistry().ViewerSpec(m.overlays.panel.kind)
}
func (m *RootModel) ViewerPageRows(spec tuiviewer.Spec) int {
	if m.overlays.panel.kind == panelregistry.Changelog {
		return changelog.PageRows(spec, m.viewport.height)
	}
	return spec.Visible
}
func (m *RootModel) ViewerWidth() int                   { return m.viewerVPWidth() }
func (m *RootModel) newViewerPanel() *viewerpanel.Panel { return panels.NewViewerPanel(m) }
func (m *RootModel) viewerPanel() *viewerpanel.Panel {
	if m.overlays.panel.vw == nil {
		p := m.newViewerPanel()
		p.Open()
		m.overlays.panel.vw = p
	}
	return m.overlays.panel.vw
}

func (m *RootModel) initViewerViewport() {
	if m.overlays.panel.vw == nil {
		m.overlays.panel.vw = m.newViewerPanel()
	}
	m.overlays.panel.vw.Open()
}

func (m *RootModel) viewerVPWidth() int {
	if m.viewport.width > 0 {
		return m.viewport.width
	}
	return 80
}

func (m *RootModel) handleViewerPanelKey(key tea.Key) (bool, tea.Cmd) {
	handled, cmd, offset := m.viewerPanel().HandleKey(key, m.overlays.panel.window)
	m.overlays.panel.window = offset
	return handled, cmd
}

// listResumeConversations returns the store's conversations for the /resume picker.
// The active conversation is included and tagged [CURRENT] (matching agy 1.5.1).
func (m *RootModel) listResumeConversations() ([]convstore.ConversationMeta, error) {
	return convstore.ListConversations()
}

func (m *RootModel) renameResumeConversation(meta convstore.ConversationMeta) {
	m.session.conversationName = meta.Title
	m.openRenamePanel()
}

func (m *RootModel) openRenamePanel() {
	m.overlays.panel.kind = panelregistry.Rename
	m.overlays.panel.cmd = "/rename"
	m.overlays.panel.rn = m.newRenamePanel()
	m.armRenameFocus()
}

func (m *RootModel) ContextState() contextpanel.State {
	return m.contextState()
}

func (m *RootModel) contextState() contextpanel.State {
	st := contextpanel.State{
		Model: m.contextModel(),
		Width: m.viewport.width,
	}
	switch {
	case m.conversation.streaming:
		st.Mode = contextpanel.ModeStreaming
		st.User, st.Agent, st.Tool = m.streamingTokenCounts()
	case m.session.messageCount() == 0:
		st.Mode = contextpanel.ModeEmpty
	default:
		st.Mode = contextpanel.ModeLive
		st.User, st.Agent, st.Tool = m.sessionTokenCounts()
		base, rules, skills, subagents := m.promptCategoryTokens()
		st.SystemPrompt = base - rules
		if st.SystemPrompt < 0 {
			st.SystemPrompt = 0
		}
		st.Rules = rules
		st.Skills = skills
		st.Subagents = subagents
		st.SystemTools = contextpanel.ToolTokenEstimate(m.wireToolDecls())
		st.SystemMessages = contextpanel.SystemMessageTokens(m.SessionMessages())
	}
	st.CheckpointBuffer = contextpanel.CheckpointBufferTokens(m.SessionMessages())
	st.User -= st.CheckpointBuffer
	if st.User < 0 {
		st.User = 0
	}
	return st
}

func (m *RootModel) promptCategoryTokens() (base, rules, skills, subagents int) {
	base, skills, subagents = m.promptSectionTokens()
	opts := m.promptOptions()
	rules = agyprompt.SectionTokenCount("user_rules", agyprompt.SectionBody(opts, "user_rules"))
	return base, rules, skills, subagents
}

func (m *RootModel) contextModel() string {
	if m.session.model != "" {
		return m.session.model
	}
	return defaultModel
}

func (m *RootModel) streamingTokenCounts() (user, agent, tool int) {
	user, _, tool = m.sessionTokenCounts()
	user = contextpanel.AdjustStreamingUserTokens(m.SessionMessages(), user)
	completed := atomic.LoadInt64(&m.session.store.SessionCompletionTokens)
	lastTurn := atomic.LoadInt64(&m.session.store.LastTurnCompletionTokens)
	agent = contextpanel.StreamingAgentTokens(completed, lastTurn, len(m.conversation.liveReplyBuf))
	return user, agent, tool
}

func (m *RootModel) wireToolDecls() []backend.ToolDecl {
	return append([]backend.ToolDecl(nil), m.tools.decls...)
}

func (m *RootModel) sessionTokenCounts() (user, agent, tool int) {
	user = contextpanel.UserMessageTokens(m.SessionMessages())
	completed := atomic.LoadInt64(&m.session.store.SessionCompletionTokens)
	lastTurn := atomic.LoadInt64(&m.session.store.LastTurnCompletionTokens)
	agent = contextpanel.AgentTokens(completed, lastTurn)
	var steps []toolsteps.Step
	for _, b := range m.conversation.blocks {
		steps = append(steps, b.Steps...)
	}
	steps = append(steps, m.conversation.liveSteps...)
	tool = contextpanel.ToolResultTokens(toolsteps.ToolResults(steps)...)
	return user, agent, tool
}

func (m *RootModel) statusRowState() statusrow.State {
	panelOpen := m.overlays.panel.kind != panelregistry.None
	panelKeepsHint := false
	if panelOpen {
		panelKeepsHint = m.overlays.panel.kind.KeepStatusHint()
	}

	return statusrow.State{
		Width:          m.viewport.width,
		CanonicalWidth: separatorWidth,

		PickerVisible:      m.prompt.picker.Visible(),
		PickerModelMode:    m.prompt.picker.IsModelMode(),
		Streaming:          m.conversation.streaming,
		Queued:             len(m.session.store.StreamQueue) + len(m.session.store.BashQueue),
		BufferEmpty:        strings.TrimSpace(m.prompt.input.Value()) == "",
		ResumePickerActive: m.overlays.resumePicker.Active(),
		BashMode:           m.session.store.BashMode,
		HelpActive:         m.help.Panel.Active(),
		PanelOpen:          panelOpen,
		PanelKeepsHint:     panelKeepsHint,
		ExitConfirm:        m.display.exitConfirm,
		VimBadge:           vimkeys.Badge(m.vimEnabled(), m.vimMode()),
		Compact:            m.compactState(),

		Model: m.session.model,
		StatusModel: func() string {
			sm := m.session.statusModel
			if sm == "" {
				sm = m.session.model
			}
			if m.session.modelWarning != "" {
				return "[mock] " + sm
			}
			return sm
		}(),
		AgentName: m.session.agentName,
		Mode:         m.statusModeLabel(),
		EffortSuffix: m.statusEffortSuffix(),
		TaskSuffix:   m.tasksStatusSuffix(),
		QueueMode:    m.session.store.QueueMode,
		// Reference agy built-in status row does not display agentState (" · working" / " · tool_use")
		// by default — it stays "auto · Gemini 3.8 Flash · high" throughout the turn.
		// Optional opt-in via PIGASUS_STATUS_SHOW_AGENT_STATE.
		AgentState: func() string {
			if os.Getenv("PIGASUS_STATUS_SHOW_AGENT_STATE") == "1" || os.Getenv("PIGASUS_STATUS_SHOW_AGENT_STATE") == "true" {
				return m.agentState()
			}
			return ""
		}(),
		// Reference agy 1.4.7/1.5.0 status row does not display context percentage by default
		// ("auto · Gemini 3.8 Flash · high"). Optional opt-in via PIGASUS_STATUS_SHOW_CONTEXT.
		ContextUsedPercent: func() float64 {
			if os.Getenv("PIGASUS_STATUS_SHOW_CONTEXT") == "1" || os.Getenv("PIGASUS_STATUS_SHOW_CONTEXT") == "true" {
				return m.contextUsedPercent()
			}
			return 0
		}(),
	}
}

func (m *RootModel) compactState() *statusrow.Compact {
	if m.session.store.CompactStart.IsZero() {
		return nil
	}
	return &statusrow.Compact{
		Start:        m.session.store.CompactStart,
		End:          m.session.store.CompactEnd,
		TokensBefore: m.session.store.CompactTokensBefore,
		TokensAfter:  m.session.store.CompactTokensAfter,
		SpinnerFrame: m.session.store.CompactSpinnerFrame,
	}
}

func (m *RootModel) panelChangelogLines() []string {
	spec, _ := m.getPanelRegistry().ViewerSpec(m.overlays.panel.kind)
	rows, start, end := m.viewerWindow(spec)
	return changelog.PanelLines(rows, start, end, spec.Total)
}

func (m *RootModel) usageViewerSpec() tuiviewer.Spec {
	if m.viewport.height > 0 {
		return usagepanel.Spec(m.usageState())
	}
	return usagepanel.StaticSpec()
}

func (m *RootModel) changelogViewerSpec() tuiviewer.Spec {
	return tuiviewer.Spec{
		Lines:   changelog.Document(m.viewport.width, m.viewport.height),
		Visible: changelog.VisibleRows(m.viewport.height),
		Total:   changelog.TotalLines,
	}
}

func (m *RootModel) viewerWindow(spec tuiviewer.Spec) (rows []string, start, end int) {
	r, s, e, clamped := tuiviewer.Window(spec, m.overlays.panel.window)
	m.overlays.panel.window = clamped
	return r, s, e
}

func (m *RootModel) panelUsageLinesREF() []string {
	lines, window := usagepanel.Lines(m.usageState(), m.overlays.panel.window)
	m.overlays.panel.window = window
	return lines
}

func (m *RootModel) panelUsageLines() []string {
	if m.viewport.height > 0 {
		return m.panelUsageLinesREF()
	}
	spec, _ := m.getPanelRegistry().ViewerSpec(m.overlays.panel.kind)
	rows, start, end := m.viewerWindow(spec)
	return usagepanel.StaticLines(rows, start, end, spec.Total)
}

type usageQuotaTickMsg = usagepanel.QuotaTickMsg

const usageQuotaLoadingTicks = usagepanel.QuotaLoadingTicks

func quotaStubRefills(m *RootModel) {
	now := time.Now()
	m.overlays.panel.quota.loadedAt = now
	m.overlays.panel.quota.refreshG = now.Add(168 * time.Hour)
	m.overlays.panel.quota.refreshO = now.Add(48 * time.Hour)
	m.overlays.panel.quota.state = quotaLoaded
}

func (m *RootModel) usageState() usagepanel.State {
	return usagepanel.State{
		Email:    m.session.email,
		Width:    m.viewport.width,
		Height:   m.viewport.height,
		Loading:  m.overlays.panel.quota.state == quotaLoading,
		Summary:  m.overlays.panel.quota.summary,
		RefreshG: m.overlays.panel.quota.refreshG,
		RefreshO: m.overlays.panel.quota.refreshO,
		Frame:    m.display.quotaFrame,
	}
}

func (m *RootModel) armRenameFocus() {
	if m.overlays.panel.rn == nil {
		return
	}
	cmd := m.overlays.panel.rn.Focus()
	if cmd != nil && m.turn.prog != nil {
		m.turn.prog.Send(cmd)
	}
}

func (m *RootModel) AppendSubAgentResponse(line string) {
	m.conversation.response = append(m.conversation.response, line)
}

// SubAgentQuestion
func (m *RootModel) SubAgentQuestionsManager() *questions.Manager {
	return m.askQuestion.manager
}
func (m *RootModel) ResetInput() { m.prompt.input.Reset() }
func (m *RootModel) newSubAgentQuestionPanel() *subagentquestionpanel.Panel {
	return panels.NewSubAgentQuestionPanel(m)
}

// Mention
func (m *RootModel) InputValue() string                     { return m.prompt.input.Value() }
func (m *RootModel) SetInputValue(val string)               { m.prompt.input.SetValue(val) }
func (m *RootModel) TerminalWidth() int                     { return m.viewport.width }
func (m *RootModel) TerminalHeight() int                    { return m.viewport.height }
func (m *RootModel) newMentionPicker() *mentionpicker.Panel { return panels.NewMentionPicker(m) }

// MentionList makes the @ picker read the remote workspace in a
// --remote-agent session. The picker is built before the flag injection, so
// the decision is made per call rather than at construction.
func (m *RootModel) MentionList() func(string) ([]atfiles.Entry, error) {
	return func(root string) ([]atfiles.Entry, error) {
		if m != nil && m.session.remoteAgent && m.remote.files != nil {
			return m.remote.files.List(root)
		}
		return atfiles.List(root)
	}
}

func (m *RootModel) IsInputY(y int) bool { return m.prompt.IsInputY(y) }

// SlashPicker
func (m *RootModel) AltScreenActive() bool { return m.viewport.altScreen }
func (m *RootModel) SlashScopeContext() slashcommands.ScopeContext {
	return m.slashScopeContext()
}
func (m *RootModel) slashScopeContext() slashcommands.ScopeContext {
	eng := m.turnEngine()
	name, caps := agentturn.CapabilitiesOf(eng)
	return slashcommands.ScopeContext{
		EngineName:   name,
		Capabilities: caps,
	}
}
func (m *RootModel) EngineSlashCommands() []slashcommands.Command {
	return m.engineSlashCommands()
}
func (m *RootModel) engineSlashCommands() []slashcommands.Command {
	var out []slashcommands.Command
	// In AGY 1.5.2: In --remote-agent sessions, skills from connected remote/cloud plugins
	// appear in the `/` menu, message-beginning completions, and `/skills`.
	if remotes := skills.RemoteSkills(); len(remotes) > 0 {
		for _, s := range remotes {
			name := "/" + s.Name
			out = append(out, slashcommands.Command{
				Name:        name,
				Description: s.Description,
				Source:      slashcommands.SourceSkill,
				Badge:       "remote",
			})
		}
	}

	eng := m.turnEngine()
	if eng == nil {
		return out
	}
	lister, ok := eng.(agentturn.CommandLister)
	if !ok {
		return out
	}
	cmds := lister.AvailableCommands()
	if len(cmds) == 0 {
		return out
	}
	for _, c := range cmds {
		name := c.Name
		if !strings.HasPrefix(name, "/") {
			name = "/" + name
		}
		desc := c.Description
		if c.InputHint != "" {
			desc += " " + c.InputHint
		}
		engName, _ := agentturn.CapabilitiesOf(eng)
		badge := "agent"
		if engName == "acp" || strings.Contains(engName, "acp") {
			badge = "acp"
		}
		out = append(out, slashcommands.Command{
			Name:        name,
			Description: desc,
			Source:      slashcommands.SourceAgent,
			Badge:       badge,
		})
	}
	return out
}
func (m *RootModel) newSlashPicker() *slashpicker.Panel {
	return panels.NewSlashPicker(m)
}

// Voice
func (m *RootModel) InsertDraft(text string) {
	if m.prompt.input.Value() == "" {
		m.prompt.input.SetValue(text)
	} else {
		m.prompt.input.InsertString(text)
	}
}
func (m *RootModel) ConnectVoice() error {
	if m.audio.connect != nil {
		return m.audio.connect()
	}
	if micAddr := os.Getenv("ANTIGRAVITY_MIC"); micAddr != "" {
		conn, err := net.DialTimeout("tcp", micAddr, 2*time.Second)
		if err != nil {
			return fmt.Errorf("cannot reach the mic server at %s (tcp): %w", micAddr, err)
		}
		_ = conn.Close()
	}
	return nil
}

// deadcode: staged (voice connection seam tested in slash_content_test.go)
func (m *RootModel) connectVoice() error {
	return m.ConnectVoice()
}
func (m *RootModel) newVoiceSurface() *voice.Surface { return panels.NewVoiceSurface(m) }

// Rewind
func (m *RootModel) RewindSteps() []rewind.Step {
	if sup := m.session.sup(); sup != nil {
		return sup.RewindSteps()
	}
	return nil
}
func (m *RootModel) RewindPickStep(index int) int {
	sup := m.session.sup()
	if sup == nil {
		return 0
	}
	r := sup.RewindToStep(index)
	if r.Removed <= 0 {
		return 0
	}
	m.slashRespond("/rewind", rewind.StepEcho(r))
	return r.Removed
}
func (m *RootModel) rewindPanel() *rewindpanel.Panel {
	return m.rewind.Panel(m)
}

// ---- Merged from slash_panel_args.go, slash_panel_routing.go, slash_panel_wiring.go,
// slash_panel_keymap.go, slash_panels.go, and slash_regression.go ----

func (m *RootModel) openSlashPanelArgs(cmd, args string) bool {
	if canonical, ok := slashcommands.Canonical(cmd); ok {
		cmd = canonical
	}
	// Remote sessions answer these from the proxy, not from a local panel:
	// /permissions reads the remote preset and /plugin manages the remote
	// catalogue.
	if m.session.remoteAgent && (cmd == "permissions" || cmd == "plugin") {
		return false
	}
	// /diff reads the proxy's turn diff; a failure is reported instead of
	// opening a panel backed by the client's own git tree.
	if m.session.remoteAgent && cmd == "diff" {
		if err := m.prepareRemoteDiff(); err != nil {
			m.slashRespond("/diff", "error: "+err.Error())
			return true
		}
	}
	if eng := m.turnEngine(); eng != nil {
		name, _ := agentturn.CapabilitiesOf(eng)
		if name != "pigasus" && (cmd == "tasks" || cmd == "agents") {
			return false
		}
	}
	if cmd == "add-dir" && args != "" {
		return false
	}
	if cmd == "codesearch" && strings.TrimSpace(args) == "" {
		return false
	}
	if cmd == "effort" && args != "" {
		return false
	}
	if cmd == "model" && strings.TrimSpace(args) != "" {
		return false
	}
	if cmd == "plugin" && strings.TrimSpace(args) != "" {
		return false
	}
	if cmd == "skills" && strings.TrimSpace(args) != "" {
		return false
	}
	if cmd == "projects" && strings.TrimSpace(args) != "" {
		return false
	}
	if !m.openSlashPanel(cmd) {
		return false
	}
	if m.overlays.panel.kind == panelregistry.CodeSearch {
		m.codesearchPanel().Start(args)
	}
	return true
}

func (m *RootModel) currentEffort() int {
	switch word := cliflags.StringValue(m.session.flags, "effort"); word {
	case "low", "medium", "high":
		return effortpanel.EffortIndexForName(word)
	}
	return effortCursorForLabel(m.session.model)
}

func (m *RootModel) currentEffortName() string {
	switch word := strings.ToLower(strings.TrimSpace(cliflags.StringValue(m.session.flags, "effort"))); word {
	case "low", "medium", "high", "xhigh", "max":
		return word
	}
	if tier := models.TierFromLabel(m.session.model); tier != "" {
		return tier
	}
	return effortpanel.EffortLevelName(m.currentEffort())
}

func (m *RootModel) panelView() string {
	if m.overlays.panel.kind == panelregistry.None {
		return ""
	}
	var b strings.Builder
	for _, l := range m.panelLines() {
		b.WriteString(l)
		b.WriteString("\n")
	}
	if m.overlays.panel.kind.TrailingBlank() {
		b.WriteString("\n")
	}
	return b.String()
}

func (m *RootModel) panelLines() []string {
	return m.getPanelRegistry().Lines(m.overlays.panel.kind)
}

func (m *RootModel) closePanel() tea.Cmd {
	if m.overlays.panel.kind == panelregistry.None {
		return nil
	}
	if m.overlays.panel.kind == panelregistry.Help {
		if m.help.Panel != nil && m.help.Panel.Active() {
			m.help.Panel.Cancel()
		}
	}
	cmd := m.overlays.panel.cmd
	notice := m.overlays.panel.kind.ExitNotice()
	m.overlays.panel.csp.Cancel()
	m.overlays.panel = slashPanel{}
	m.reloadVimSettings()
	if notice && cmd != "" {
		m.conversation.response = append(m.conversation.response, tuilib.SlashEchoLines(cmd, "Exited "+cmd+" command")...)
	}
	return nil
}

func (m *RootModel) closePanelSilent() tea.Cmd {
	if m.overlays.panel.kind == panelregistry.Help {
		if m.help.Panel != nil && m.help.Panel.Active() {
			m.help.Panel.Cancel()
		}
	}
	m.overlays.panel.csp.Cancel()
	m.overlays.panel = slashPanel{}
	m.reloadVimSettings()
	return nil
}

func (m *RootModel) handleNavAndEnterKey(key tea.Key, n int) (bool, tea.Cmd, bool) {
	if m.overlays.panel.kind == panelregistry.Config {
		if handled, cmd := m.configPanel().HandleKey(key); handled {
			return true, cmd, true
		}
	}
	switch action := tuilib.ClassifyListNavKey(key); action {
	case tuilib.NavUp, tuilib.NavDown, tuilib.NavPageUp, tuilib.NavPageDown,
		tuilib.NavTop, tuilib.NavBottom:
		m.overlays.panel.cursor = tuiviewport.PanelCursor(m.overlays.panel.cursor, action, n)
		return true, nil, true
	case tuilib.NavCancel:
		return true, m.closePanel(), true
	}
	return false, nil, false
}

func (m *RootModel) handlePanelKey(key tea.Key) (bool, tea.Cmd) {
	if m.overlays.panel.kind == panelregistry.None {
		return false, nil
	}
	if handled, cmd, claimed := m.panelRegistry().HandleKey(m.overlays.panel.kind, key); claimed {
		if m.overlays.panel.kind == panelregistry.Tasks && m.overlays.panel.tp != nil {
			m.overlays.panel.cursor = m.overlays.panel.tp.Cursor()
		}
		return handled, cmd
	}
	n := m.panelRegistry().RowCount(m.overlays.panel.kind)
	if m.overlays.panel.kind == panelregistry.Model {
		if handled, cmd := m.modelPanel().HandleKey(key); handled {
			return true, cmd
		}
	}
	if handled, cmd, ok := m.handleNavAndEnterKey(key, n); ok {
		return handled, cmd
	}
	switch key.Code {
	case tea.KeyEsc:
		return true, m.closePanel()
	case tea.KeyEnter, tea.KeyTab, tea.KeyBackspace:
		return true, nil
	}
	if key.Mod == 0 && key.Text != "" {
		return true, nil
	}
	return false, nil
}

func listPanelKind(k panelregistry.Kind) bool {
	return k.IsList()
}

func (m *RootModel) panelRegistry() *panelregistry.Registry {
	reg := m.getPanelRegistry()
	if !reg.Wired() {
		m.wirePanelRegistry(reg)
	}
	return reg
}

func (m *RootModel) wirePanelRegistry(reg *panelregistry.Registry) {
	reg.Wire(panelregistry.Wiring{
		Rows: map[panelregistry.Kind]func() int{
			panelregistry.Tasks:       func() int { return m.tasksPanel().RowCount() },
			panelregistry.Effort:      func() int { return m.effortPanel().RowCount() },
			panelregistry.Hooks:       func() int { return m.hooksPanel().RowCount() },
			panelregistry.Permissions: func() int { return m.permissionsPanel().RowCount() },
			panelregistry.Agents:      func() int { return m.agentsPanel().RowCount() },
			panelregistry.Credits:     func() int { return m.creditsPanel().RowCount() },
			panelregistry.Feedback:    func() int { return m.feedbackPanel().RowCount() },
			panelregistry.Rewind:      func() int { return m.rewindPanel().RowCount() },
			panelregistry.Model:       func() int { return m.modelPanel().RowCount() },
			panelregistry.Config:      func() int { return m.configPanel().RowCount() },
			panelregistry.Plugin:      func() int { return m.pluginPanel().RowCount() },
			panelregistry.Skills:      func() int { return m.skillsPanel().RowCount() },
			panelregistry.Provider:    func() int { return m.providerPanel().RowCount() },
			panelregistry.Accounts:    func() int { return m.accountsPanel().RowCount() },
		},
		Claims: map[panelregistry.Kind]func(tea.Key) (bool, tea.Cmd, bool){
			panelregistry.CodeSearch: func(key tea.Key) (bool, tea.Cmd, bool) {
				return m.codesearchPanel().HandleKey(key)
			},
		},
		Seams: panelregistry.Seams{
			Close: m.closePanel,
			ViewerKey: func(key tea.Key) (bool, tea.Cmd) {
				return m.handleViewerPanelKey(key)
			},
		},
	})
}

const (
	panelGray166  = "\x1b[38;5;166m"
	panelGray166C = "\x1b[39m"
)

type slashPanel struct {
	kind   panelregistry.Kind
	cmd    string
	cursor int
	window int
	ag     *agentspanel.Panel
	fb     *feedbackpanel.Panel
	cr     *creditspanel.Panel
	plg    *pluginsui.Panel
	rn     *renamepanel.Panel
	ad     *adddirpanel.Panel
	vw     *viewerpanel.Panel
	csp    *codesearchpanel.Panel
	quota  quotaState
	prov   *providerpanel.Panel
	acct   *accountspanel.Panel
	cfg    *configpanel.Panel
	mdl    *modelpicker.Panel
	sk     *skillsui.Panel
	perm   *permissionspanel.Panel
	hk     *hookspanel.Panel
	tp     *taskspanel.Panel
	ef     *effortpanel.Panel
	proj   *projectspanel.Panel
}

type quotaState struct {
	state    int
	loadedAt time.Time
	refreshG time.Time
	refreshO time.Time
	summary  *auth.QuotaSummary
	fetchErr string
}

const (
	quotaLoading = iota
	quotaLoaded
)

func (m *RootModel) initOpenedSlashPanel(kind panelregistry.Kind) {
	m.initOpenedSlashPanelData(kind)
}

var panelInitializers = map[panelregistry.Kind]func(m *RootModel){
	panelregistry.Provider: func(m *RootModel) {
		m.overlays.panel.prov = m.newProviderPanel()
		m.overlays.panel.prov.Reload()
		m.overlays.panel.prov.Notice("")
	},
	panelregistry.Config:      func(m *RootModel) { m.overlays.panel.cfg = m.newConfigPanel() },
	panelregistry.Model:       func(m *RootModel) { m.overlays.panel.mdl = m.newModelPanel() },
	panelregistry.Plugin:      func(m *RootModel) { m.overlays.panel.plg = m.newPluginPanel() },
	panelregistry.Skills:      func(m *RootModel) { m.overlays.panel.sk = m.newSkillsPanel() },
	panelregistry.Permissions: func(m *RootModel) { m.overlays.panel.perm = m.newPermissionsPanel() },
	panelregistry.Hooks:       func(m *RootModel) { m.overlays.panel.hk = m.newHooksPanel() },
	panelregistry.Agents:      func(m *RootModel) { m.overlays.panel.ag = m.newAgentsPanel() },
	panelregistry.Feedback:    func(m *RootModel) { m.overlays.panel.fb = m.newFeedbackPanel() },
	panelregistry.Credits:     func(m *RootModel) { m.overlays.panel.cr = m.newCreditsPanel() },
	panelregistry.Tasks:       func(m *RootModel) { m.overlays.panel.tp = m.newTasksPanel() },
	panelregistry.Effort:      func(m *RootModel) { m.overlays.panel.ef = m.newEffortPanel() },
	panelregistry.CodeSearch:  func(m *RootModel) { m.overlays.panel.csp = m.newCodesearchPanel() },
	panelregistry.Rewind:      func(m *RootModel) { m.rewind.Reopen(m) },
	panelregistry.Diff:        func(m *RootModel) { m.diff.Reopen(m) },
	panelregistry.Rename: func(m *RootModel) {
		m.overlays.panel.rn = m.newRenamePanel()
		m.armRenameFocus()
	},
	panelregistry.AddDir:   func(m *RootModel) { m.overlays.panel.ad = m.newAddDirPanel() },
	panelregistry.Projects: func(m *RootModel) { m.overlays.panel.proj = m.newProjectsPanel() },
	panelregistry.Accounts: func(m *RootModel) {
		m.overlays.panel.acct = m.newAccountsPanel()
		m.overlays.panel.acct.Reload()
		m.overlays.panel.acct.ClearNotice()
		if m.overlays.panel.cmd == "/pool" {
			m.overlays.panel.acct.SetTab(accountspanel.TabPool)
		}
		m.overlays.panel.acct.ReloadPolicy()
	},
}

func (m *RootModel) initOpenedSlashPanelData(kind panelregistry.Kind) {
	if kind == panelregistry.Usage && m.viewport.height > 0 {
		m.overlays.panel.quota = quotaState{state: quotaLoading}
	}
	if kind.IsViewer() {
		m.initViewerViewport()
	}
	if initFn, ok := panelInitializers[kind]; ok {
		initFn(m)
	}
}

func (m *RootModel) openSlashPanel(cmd string) bool {
	kind, ok := slashcommands.PanelFor(cmd)
	if !ok {
		return false
	}
	if cmdInfo, found := slashcommands.Find(cmd); found {
		sctx := m.slashScopeContext()
		if !slashcommands.IsAvailable(cmdInfo, sctx) {
			return false
		}
	}
	canonical := cmd
	if c, ok := slashcommands.Canonical(cmd); ok {
		canonical = c
	}
	m.overlays.panel = slashPanel{kind: kind, cmd: "/" + canonical}
	m.initOpenedSlashPanel(kind)
	return true
}

func effortCursorForLabel(label string) int {
	l := strings.ToLower(label)
	switch {
	case strings.Contains(l, "(low)"):
		return 0
	case strings.Contains(l, "(medium)"):
		return 1
	}
	return 2
}

func (m *RootModel) panelCurrentModelLabel() string {
	if lbl := strings.TrimSpace(m.session.model); lbl != "" {
		return lbl
	}
	lbl, _ := modelcatalog.ResolveEffectiveModel("")
	return lbl
}

func (m *RootModel) statusEffortSuffix() string {
	if !modelpicker.HasEffortTiers(m.session.statusModel) {
		return ""
	}
	return " \u00b7 " + m.currentEffortName()
}

func (m *RootModel) applyEffortSelection(level int) tea.Cmd {
	name := models.TierNameAt(m.effortTiers(), level)
	if name == "" {
		name = effortpanel.EffortLevelName(level)
	}
	return m.applyEffortLevel(name, level)
}

func (m *RootModel) applyEffortLevel(name string, level int) tea.Cmd {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		name = effortpanel.EffortLevelName(level)
	}
	model := m.effortModel()
	if !modelcatalog.EffortAdjustableFor(model) {
		m.slashRespond("/effort", models.NotAdjustableMessage(models.BaseLabel(model)))
		return nil
	}
	before := model
	label := modelcatalog.EffortLabelFor(model, name)
	changed := label != "" && label != before

	if m.session.flags != nil {
		_ = m.session.flags.Set("effort", name)
	}

	if m.overlays.panel.kind == panelregistry.Effort {
		m.overlays.panel = slashPanel{}
	}
	if !changed {
		return nil
	}
	if strings.TrimSpace(m.session.model) == "" {
		m.session.model = before
		m.session.statusModel = tuilib.StripTier(before)
	}
	m.session.model = label
	m.session.statusModel = tuilib.StripTier(label)
	m.syncBackendForModel()
	_ = settings.SetField("model", label)
	m.conversation.response = append(m.conversation.response, tuilib.SlashEchoLines("/effort", "Effort set to "+name)...)
	return nil
}

var vcsDiffFunc = diffaction.VCSDiff

var gitHeadFunc = func(dir string) (string, error) {
	return diffaction.RunGit(dir, "diff", "HEAD")
}

func diffFeedback(workspace string, maxCells int) string {
	diffaction.VCSDiffFunc = vcsDiffFunc
	diffaction.GitHeadFuncSeam = gitHeadFunc
	return diffaction.DiffFeedback(workspace, maxCells)
}

type editorFinishedMsg struct {
	err      error
	tempPath string
}

func (m *RootModel) editPromptCmd() tea.Cmd {
	name, args, notice, err := editorkit.ResolveEditor(settings.String("editor"))
	if err != nil {
		m.conversation.response = append(m.conversation.response, "Failed to resolve editor: "+err.Error())
		return nil
	}
	if _, err := exec.LookPath(name); err != nil {
		m.conversation.response = append(m.conversation.response, "Failed to resolve editor: "+err.Error())
		return nil
	}
	if notice != "" {
		m.conversation.response = append(m.conversation.response, notice)
	}
	path, err := editorkit.PromptScratchFile(m.prompt.input.Value())
	if err != nil {
		m.conversation.response = append(m.conversation.response, "Failed to create a temporary file: "+err.Error())
		return nil
	}
	c := exec.Command(name, append(args, path)...)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return editorFinishedMsg{err: err, tempPath: path}
	})
}

func (m *RootModel) getPanelRegistry() *panelregistry.Registry {
	if m.overlays.panelReg == nil {
		m.overlays.panelReg = m.buildPanelRegistry()
	}
	return m.overlays.panelReg
}

func (m *RootModel) Panel(k panelregistry.Kind) any {
	switch k {
	case panelregistry.Help:
		return m.helpPanel()
	case panelregistry.Tasks:
		return m.tasksPanel()
	case panelregistry.Effort:
		return m.effortPanel()
	case panelregistry.Hooks:
		return m.hooksPanel()
	case panelregistry.Permissions:
		return m.permissionsPanel()
	case panelregistry.Agents:
		return m.agentsPanel()
	case panelregistry.Credits:
		return m.creditsPanel()
	case panelregistry.Feedback:
		return m.feedbackPanel()
	case panelregistry.Rewind:
		return m.rewindPanel()
	case panelregistry.Model:
		return m.modelPanel()
	case panelregistry.Config:
		return m.configPanel()
	case panelregistry.Plugin:
		return m.pluginPanel()
	case panelregistry.Skills:
		return m.skillsPanel()
	case panelregistry.Diff:
		return m.diffPanel()
	case panelregistry.AddDir:
		return m.addDirPanel()
	case panelregistry.Rename:
		return m.renamePanel()
	case panelregistry.Provider:
		return m.providerPanel()
	case panelregistry.Accounts:
		return m.accountsPanel()
	case panelregistry.Projects:
		return m.projectsPanel()
	case panelregistry.CodeSearch:
		return m.codesearchPanel()
	default:
		return nil
	}
}

func (m *RootModel) projectsPanel() *projectspanel.Panel {
	if m.overlays.panel.proj == nil {
		m.overlays.panel.proj = m.newProjectsPanel()
	}
	return m.overlays.panel.proj
}

func (m *RootModel) newProjectsPanel() *projectspanel.Panel {
	return panels.NewProjectsPanel(m)
}

func (m *RootModel) SwitchProject(proj coreprojects.LocalProject) tea.Cmd {
	for _, f := range proj.Folders {
		tuilib.AddDirFeedback(f)
	}
	hostactions.HandleClear(m.hostActionSeams(), "")
	m.slashRespond("/projects", fmt.Sprintf("Switched to project %q.", proj.Name))
	return m.closePanel()
}

func (m *RootModel) HomeDir() string {
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	return ""
}

func (m *RootModel) WorkspaceDir() string {
	if m != nil && m.session.cwd != "" {
		return m.session.cwd
	}
	return ""
}

func (m *RootModel) PanelUsageLines() []string {
	return m.panelUsageLines()
}

func (m *RootModel) UsageViewerSpec() tuiviewer.Spec {
	return m.usageViewerSpec()
}

func (m *RootModel) PanelChangelogLines() []string {
	return m.panelChangelogLines()
}

func (m *RootModel) ChangelogViewerSpec() tuiviewer.Spec {
	return m.changelogViewerSpec()
}

func (m *RootModel) buildPanelRegistry() *panelregistry.Registry {
	return panels.BuildRegistry(m)
}
