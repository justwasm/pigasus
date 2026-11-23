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
	"github.com/charmbracelet/x/ansi"

	"github.com/justwasm/pigasus/agentlink"
	"github.com/justwasm/pigasus/endpoints"
	"github.com/justwasm/pigasus/internal/app/agentturn"
	"github.com/justwasm/pigasus/internal/app/prompt/agyprompt"
	"github.com/justwasm/pigasus/internal/app/session/convstore"
	"github.com/justwasm/pigasus/internal/app/session/setup"
	"github.com/justwasm/pigasus/internal/app/session/slashactions"
	browsersubagent "github.com/justwasm/pigasus/internal/app/subagent/browser"
	"github.com/justwasm/pigasus/internal/app/subagenttools"
	"github.com/justwasm/pigasus/internal/app/tools/browser"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/core/cliflags"
	"github.com/justwasm/pigasus/internal/core/confhome"
	"github.com/justwasm/pigasus/internal/core/interaction"
	"github.com/justwasm/pigasus/internal/core/policy/permissions"
	"github.com/justwasm/pigasus/internal/core/settings"
	"github.com/justwasm/pigasus/internal/features/auth"
	"github.com/justwasm/pigasus/internal/features/models/modelctx"
	"github.com/justwasm/pigasus/internal/infra/remote/persistentrc"
	"github.com/justwasm/pigasus/internal/infra/remote/rclink"
	"github.com/justwasm/pigasus/internal/ui/editor/editorkit"
	"github.com/justwasm/pigasus/internal/ui/editor/vimkeys"
	"github.com/justwasm/pigasus/internal/ui/mdrender"
	"github.com/justwasm/pigasus/internal/ui/toolsteps"
	"github.com/justwasm/pigasus/internal/ui/views/agentspanel"
	"github.com/justwasm/pigasus/internal/ui/views/codesearchpanel"
	"github.com/justwasm/pigasus/internal/ui/views/contextpanel"
	"github.com/justwasm/pigasus/internal/ui/views/historypanel"
	"github.com/justwasm/pigasus/internal/ui/views/loginui"
	"github.com/justwasm/pigasus/internal/ui/views/narration"
	"github.com/justwasm/pigasus/internal/ui/views/permissionprompt"
	"github.com/justwasm/pigasus/internal/ui/views/statusline"
	"github.com/justwasm/pigasus/internal/ui/views/statusrow"
	"github.com/justwasm/pigasus/internal/ui/views/terminalpanel"
	"github.com/justwasm/pigasus/internal/ui/views/themepreview"
	"github.com/justwasm/pigasus/internal/ui/views/usagepanel"
	"github.com/justwasm/pigasus/palette"
	"github.com/justwasm/pigasus/vim"

	atfiles "github.com/justwasm/pigasus/internal/app/prompt/mentions/files"
	"github.com/justwasm/pigasus/internal/app/session/rewind"
	subagent "github.com/justwasm/pigasus/internal/app/subagent/core"
	"github.com/justwasm/pigasus/internal/app/subagent/questions"
	taskregistry "github.com/justwasm/pigasus/internal/app/task"
	"github.com/justwasm/pigasus/internal/app/toolrunner"
	"github.com/justwasm/pigasus/internal/cli"
	coreprojects "github.com/justwasm/pigasus/internal/core/cliflags/projects"
	"github.com/justwasm/pigasus/internal/core/lifecycle"
	"github.com/justwasm/pigasus/internal/features/models"
	"github.com/justwasm/pigasus/internal/features/models/catalog"
	plugins "github.com/justwasm/pigasus/internal/features/plugins/core"
	"github.com/justwasm/pigasus/internal/features/tools/clipboard"
	"github.com/justwasm/pigasus/internal/features/tools/skills"
	"github.com/justwasm/pigasus/internal/infra/backend/backendselect"
	"github.com/justwasm/pigasus/internal/rcaudio"
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
	"github.com/justwasm/pigasus/internal/ui/views/logspanel"
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
	voicebackend "github.com/justwasm/pigasus/internal/voice"
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
	reg := m.tasks.reg
	if m.tasks.ctrl != nil && m.tasks.ctrl.Registry() != nil {
		reg = m.tasks.ctrl.Registry()
	}
	if reg != nil && reg.CountRunning() > 0 {
		return true
	}
	if m.overlays.panel.kind == panelregistry.Tasks {
		if m.overlays.panel.tp != nil && m.overlays.panel.tp.Detail() {
			t := m.overlays.panel.tp.DetailTask()
			return t != nil && t.State == taskregistry.StateRunning
		}
		return reg != nil && reg.CountRunning() > 0
	}
	if m.overlays.panel.kind == panelregistry.Agents {
		if m.overlays.panel.ag != nil && m.overlays.panel.ag.DetailMode() {
			sa := m.overlays.panel.ag.DetailSubagent()
			return sa != nil && strings.ToLower(sa.State) == "running"
		}
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
		CycleMode:                m.cycleModeLabel(),
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
	as := m.statusLine.AreaState(statusrow.Render(m.statusRowState()))
	if m.display.transientNotice != "" {
		as.Notice = m.display.transientNotice
	}
	return as
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
func (m *RootModel) hasActiveSwarm() bool {
	if m == nil || m.swarm.dismissed {
		return false
	}
	if m.swarm.item != nil {
		return true
	}
	if m.swarm.Board != nil && (m.swarm.Board.PostCount() > 0 || m.swarm.Board.Title() != "" || len(m.swarm.Board.Members()) > 0) {
		return true
	}
	return false
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
func (m *RootModel) LookupTask(id string) *taskregistry.Task {
	if m.tasks.reg != nil {
		if t := m.tasks.reg.Get(id); t != nil {
			return t
		}
	}
	if m.tasks.ctrl != nil && m.tasks.ctrl.Registry() != nil {
		return m.tasks.ctrl.Registry().Get(id)
	}
	return nil
}
func (m *RootModel) runningSubagents() []*subagent.Subagent {
	var out []*subagent.Subagent
	seen := make(map[string]bool)
	if m.tasks.subagentManager != nil {
		for _, sa := range m.tasks.subagentManager.List() {
			if sa != nil {
				seen[sa.ID] = true
				out = append(out, sa)
			}
		}
	}
	reg := m.tasks.reg
	if reg == nil && m.tasks.ctrl != nil {
		reg = m.tasks.ctrl.Registry()
	}
	if reg != nil {
		for _, t := range reg.List() {
			if t != nil && t.Kind == taskregistry.KindSubagent && !seen[t.ID] {
				seen[t.ID] = true
				sa := &subagent.Subagent{
					ID:     t.ID,
					Name:   t.Name,
					Title:  t.Name,
					State:  string(t.State),
					Model:  t.Model,
					Effort: t.Effort,
					Result: t.Output,
					Err:    t.Err,
				}
				out = append(out, sa)
			}
		}
	}
	return out
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
// statusModeLabel is the agent mode the built-in status row displays:
// "auto", "plan", "accept-edits", or "" while the session has no configured mode.
func (m *RootModel) statusModeLabel() string {
	return strings.TrimSpace(m.session.mode)
}

// cycleModeLabel is the agent mode reported in the external statusline JSON
// script's `cycle_mode` field — omitted when the session runs in the mode it
// started in.
func (m *RootModel) cycleModeLabel() string {
	return modeLabelFrom(m.session.mode, m.session.startMode)
}

func modeLabelFrom(mode, start string) string {
	mode = strings.TrimSpace(mode)
	if mode == "" || mode == strings.TrimSpace(start) {
		return ""
	}
	return mode
}

// promptPlaceholder is the composer's placeholder for a session: the live
// mode's banner.
func (m *RootModel) promptPlaceholder(mode string) string {
	return permissions.ModeBanner(strings.TrimSpace(mode))
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
func (m *RootModel) IsRemoteAgent() bool               { return m != nil && m.session.remoteAgent }
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
	if m.overlays.panel.kind == panelregistry.Logs {
		if key.Mod == 0 && (key.Code == 'f' || key.Code == 'F' || key.Text == "f" || key.Text == "F") {
			m.overlays.panel.logsFollow = !m.overlays.panel.logsFollow
			if m.overlays.panel.logsFollow {
				m.overlays.panel.window = tuiviewer.MaxOffset(m.logsViewerSpec())
			}
			m.invalidateViewCache()
			return true, nil
		}
		if key.Mod == 0 && (key.Code == 'r' || key.Code == 'R' || key.Text == "r" || key.Text == "R") {
			if m.overlays.panel.logsFollow {
				m.overlays.panel.window = tuiviewer.MaxOffset(m.logsViewerSpec())
			}
			m.invalidateViewCache()
			return true, nil
		}
	}
	prevOffset := m.overlays.panel.window
	handled, cmd, offset := m.viewerPanel().HandleKey(key, m.overlays.panel.window)
	m.overlays.panel.window = offset
	if m.overlays.panel.kind == panelregistry.Logs {
		switch tuilib.ClassifyViewerNavKey(key) {
		case tuilib.NavUp, tuilib.NavPageUp, tuilib.NavHalfPageUp, tuilib.NavTop:
			m.overlays.panel.logsFollow = false
		case tuilib.NavBottom:
			m.overlays.panel.logsFollow = true
		case tuilib.NavDown, tuilib.NavPageDown, tuilib.NavHalfPageDown:
			if offset >= tuiviewer.MaxOffset(m.logsViewerSpec()) {
				m.overlays.panel.logsFollow = true
			} else if offset < prevOffset {
				m.overlays.panel.logsFollow = false
			}
		}
	}
	return handled, cmd
}

func (m *RootModel) scrollViewerPanel(delta int) {
	m.overlays.panel.window = m.viewerPanel().Scroll(delta, m.overlays.panel.window)
	if m.overlays.panel.kind == panelregistry.Logs {
		if delta < 0 {
			m.overlays.panel.logsFollow = false
		} else if delta > 0 && m.overlays.panel.window >= tuiviewer.MaxOffset(m.logsViewerSpec()) {
			m.overlays.panel.logsFollow = true
		}
	}
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
	ctxMax := modelctx.PublishedContextWindow(m.session.model)
	if ctxMax <= 0 {
		ctxMax = contextpanel.MaxContextTokens
	}
	st := contextpanel.State{
		Model:               m.contextModel(),
		ContextMax:          ctxMax,
		Width:               m.viewport.width,
		CompactJustHappened: m.session.store.CompactJustHappened,
	}
	switch {
	case m.conversation.streaming:
		st.Mode = contextpanel.ModeStreaming
		st.User, st.Agent, st.Tool = m.streamingTokenCounts()
	case m.session.messageCount() == 0:
		st.Mode = contextpanel.ModeEmpty
		st.User, st.Agent, st.Tool = 0, 0, 0
	default:
		st.Mode = contextpanel.ModeLive
		st.User, st.Agent, st.Tool = m.sessionTokenCounts()
	}
	base, rules, skills, subagents := m.promptCategoryTokens()
	st.SystemPrompt = base - rules
	if st.SystemPrompt <= 0 && m.session.agentBody == "" {
		st.SystemPrompt = 4500
	}
	st.Rules = rules
	if st.Rules <= 0 && m.session.agentBody == "" {
		st.Rules = 4400
	}
	st.Skills = skills
	if st.Skills <= 0 && m.session.agentBody == "" {
		st.Skills = 10200
	}
	st.Subagents = subagents
	if st.Subagents <= 0 && m.session.agentBody == "" {
		st.Subagents = 706
	}
	tools := m.wireToolDecls()
	st.SystemTools = contextpanel.ToolTokenEstimate(tools)
	if st.SystemTools <= 0 {
		st.SystemTools = 7100
	}
	st.SystemMessages = contextpanel.SystemMessageTokens(m.SessionMessages())
	if st.SystemMessages <= 0 {
		st.SystemMessages = 13500
	}
	resumeTokens := contextpanel.CheckpointBufferTokens(m.SessionMessages())
	st.User -= resumeTokens
	if st.User < 0 {
		st.User = 0
	}
	if m.session.store.CompactTokensBefore > 0 {
		st.CheckpointBuffer = m.session.store.CompactTokensBefore
	} else {
		st.CheckpointBuffer = resumeTokens
	}
	st.TotalCheckpoints, st.ActiveCheckpointIndex, st.ActiveCheckpointStart, st.ActiveCheckpointEnd, st.HistoricalCheckpoints = m.checkpointStats()
	st.FileSections = m.collectContextFileSections()
	return st
}

func (m *RootModel) checkpointStats() (total, activeIdx, startStep, endStep, historicalCount int) {
	steps := convstore.BuildSteps(m.trajectoryFacts())
	for _, s := range steps {
		if s.Type == convstore.StepCheckpoint {
			total++
			activeIdx = total
			startStep = s.IncludedStart
			endStep = s.IncludedEnd
		}
	}
	if total == 0 {
		if m.session.store.CompactTokensAfter > 0 || m.session.store.CompactTokensBefore > 0 {
			total = 1
			activeIdx = 1
			startStep = 1
			endStep = m.session.store.CompactStepsSummarized
			if endStep <= 0 {
				endStep = len(steps)
			}
			if endStep < startStep {
				endStep = startStep
			}
		}
	}
	if startStep <= 0 {
		startStep = 1
	}
	if endStep < startStep {
		endStep = startStep
	}
	if total > 1 {
		historicalCount = total - 1
	}
	return total, activeIdx, startStep, endStep, historicalCount
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
	user, agent, tool = m.sessionTokenCounts()
	user = contextpanel.AdjustStreamingUserTokens(m.SessionMessages(), user)
	hasCompacted := m.session.store.CompactJustHappened ||
		m.session.store.CompactTokensAfter > 0 ||
		m.conversation.compactBlockIdx > 0 ||
		contextpanel.CheckpointBufferTokens(m.SessionMessages()) > 0
	if hasCompacted {
		agent += len(m.conversation.liveReplyBuf) / 4
	} else {
		completed := atomic.LoadInt64(&m.session.store.SessionCompletionTokens)
		lastTurn := atomic.LoadInt64(&m.session.store.LastTurnCompletionTokens)
		agent = contextpanel.StreamingAgentTokens(completed, lastTurn, len(m.conversation.liveReplyBuf))
	}
	return user, agent, tool
}

func (m *RootModel) wireToolDecls() []backend.ToolDecl {
	if len(m.tools.decls) > 0 {
		return append([]backend.ToolDecl(nil), m.tools.decls...)
	}
	return toolrunner.AdvertisedToolDecls(m.session.agentBody, m.tools.catalog)
}

func (m *RootModel) sessionTokenCounts() (user, agent, tool int) {
	user = contextpanel.UserMessageTokens(m.SessionMessages())

	hasCompacted := m.session.store.CompactJustHappened ||
		m.session.store.CompactTokensAfter > 0 ||
		m.conversation.compactBlockIdx > 0 ||
		contextpanel.CheckpointBufferTokens(m.SessionMessages()) > 0

	if hasCompacted {
		agent = contextpanel.AgentMessageTokens(m.SessionMessages())
	} else {
		completed := atomic.LoadInt64(&m.session.store.SessionCompletionTokens)
		lastTurn := atomic.LoadInt64(&m.session.store.LastTurnCompletionTokens)
		agent = contextpanel.AgentTokens(completed, lastTurn)
	}

	var steps []toolsteps.Step
	startBlock := 0
	if hasCompacted && m.conversation.compactBlockIdx > 0 {
		startBlock = m.conversation.compactBlockIdx
		if startBlock > len(m.conversation.blocks) {
			startBlock = len(m.conversation.blocks)
		}
	}
	for _, b := range m.conversation.blocks[startBlock:] {
		steps = append(steps, b.Steps...)
	}
	startLiveStep := 0
	if hasCompacted && m.conversation.compactStepIdx > 0 {
		startLiveStep = m.conversation.compactStepIdx
		if startLiveStep > len(m.conversation.liveSteps) {
			startLiveStep = len(m.conversation.liveSteps)
		}
	}
	steps = append(steps, m.conversation.liveSteps[startLiveStep:]...)
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
		AgentName:    m.session.agentName,
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
		IsGeminiAPIKey: strings.TrimSpace(os.Getenv("GEMINI_API_KEY")) != "" || endpoints.CurrentGeminiAPIKey() != "",
	}
}

func (m *RootModel) compactState() *statusrow.Compact {
	if m.session.store.CompactStart.IsZero() {
		return nil
	}
	if !m.session.store.CompactEnd.IsZero() && time.Since(m.session.store.CompactEnd) >= compactNoticeTTL {
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

func (m *RootModel) logsState() logspanel.State {
	return logspanel.LoadState(m.viewport.width, m.viewport.height, m.overlays.panel.logsFollow)
}

func (m *RootModel) logsViewerSpec() tuiviewer.Spec {
	return logspanel.Spec(m.logsState())
}

func (m *RootModel) panelLogsLines() []string {
	st := m.logsState()
	spec := logspanel.Spec(st)
	if m.overlays.panel.logsFollow {
		m.overlays.panel.window = tuiviewer.MaxOffset(spec)
	}
	rows, start, end := m.viewerWindow(spec)
	return logspanel.PanelLines(st, rows, start, end, spec.Total)
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
func (m *RootModel) InputValue() string { return m.prompt.input.Value() }
func (m *RootModel) SetInputValue(val string) {
	m.prompt.input.SetValue(val)
	m.prompt.SetInputCursorOffset(len(val))
	m.prompt.input.CursorEnd()
	if m.prompt.vimEng != nil {
		m.prompt.vimEng.SetTextKeepingPending(val)
		m.prompt.vimEng.Row = len(m.prompt.vimEng.Lines) - 1
		if m.prompt.vimEng.Row >= 0 && m.prompt.vimEng.Row < len(m.prompt.vimEng.Lines) {
			m.prompt.vimEng.Col = len([]rune(m.prompt.vimEng.Lines[m.prompt.vimEng.Row]))
		}
		m.syncVimInput()
	}
}
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
	p := panels.NewSlashPicker(m)
	p.SetTerminalSource(m.terminalSlashCommands)
	return p
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
	_ = m.audio.startDictationCapture()
	return nil
}

func (m *RootModel) FinishDictation() tea.Cmd {
	pcm := m.audio.stopDictationCapture()
	if len(pcm) == 0 {
		return nil
	}
	transcriber := m.audio.transcriber
	if transcriber == nil {
		transcriber = rcaudio.NewCloudCodeTranscriber()
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		text, err := transcriber.Transcribe(ctx, pcm, "audio/pcm;rate=16000")
		return voice.DictationDoneMsg{
			Text: text,
			Err:  err,
		}
	}
}

func (m *RootModel) CancelDictation() {
	m.audio.cancelDictationCapture()
}

func (m *RootModel) IsLiveVoiceAvailable() bool {
	if tok, err := auth.LoadTokenUnified(); err == nil && tok != nil && tok.Token.AccessToken != "" {
		return true
	}
	if endpoints.CurrentGeminiAPIKey() != "" {
		return true
	}
	if endpoints.Current().Voice != endpoints.DefaultVoice {
		return true
	}
	if os.Getenv("PIGASUS_MOCK_VOICE") != "" || os.Getenv("OPENAGY_MOCK_VOICE") != "" {
		return true
	}
	return false
}

func (m *RootModel) StartLiveVoice() error {
	if !m.IsLiveVoiceAvailable() {
		return errors.New("Live mode isn't available in this session.")
	}

	voiceName := m.audio.voice
	if voiceName == "" {
		voiceName = "Finn"
	}
	liveModel := endpoints.CurrentGeminiLiveModel()
	if liveModel == "" {
		liveModel = voicebackend.DefaultGeminiLiveModel
	}
	ws := m.session.cwd
	if ws == "" {
		ws, _ = os.Getwd()
	}
	sysPrompt := fmt.Sprintf(`You are Antigravity's real-time spoken voice agent running in workspace: %s.

Primary behavioral guidelines:
1. Spoken persona: Speak concisely, naturally, warmly, and helpfully in 1-2 spoken sentences. Never read out raw markdown tables, verbose code blocks, URLs, or UUIDs.
2. Thinker agent handoff: For any coding, file inspection, editing, terminal command execution, tests, git operations, web searches, or complex analysis tasks, you MUST call 'call_thinker_agent'.
3. Never refuse a request or claim you lack tools or capabilities. Speak one brief commitment sentence first (e.g. "To check the files, I'll examine the workspace directory." or "好的，我来帮你查看项目下的文件。"), and in the SAME turn call 'call_thinker_agent' with the user's full request and context.
4. When Thinker progress notes or answers arrive, summarize the key takeaway to the user naturally in 1-2 spoken sentences.
5. If the user explicitly asks to hang up or say goodbye, speak a brief farewell and call 'hang_up'.`, ws)

	msgs := m.SessionMessages()
	if len(msgs) > 0 {
		var hist strings.Builder
		hist.WriteString("\n\nActive conversation history prior to starting this voice session:\n")
		start := 0
		if len(msgs) > 30 {
			start = len(msgs) - 30
		}
		for _, msg := range msgs[start:] {
			role := "User"
			if msg.Role == "assistant" {
				role = "Assistant"
			}
			content := strings.TrimSpace(msg.Content)
			if content != "" {
				hist.WriteString(fmt.Sprintf("%s: %s\n", role, content))
			}
		}
		sysPrompt += hist.String()
	}

	cfg := voicebackend.LiveSessionConfig{
		Voice:        voiceName,
		Model:        liveModel,
		SystemPrompt: sysPrompt,
	}

	apiKey := endpoints.CurrentGeminiAPIKey()

	voiceEndpoint := endpoints.Current().Voice
	var backend voicebackend.LiveBackend

	if mockMode := os.Getenv("PIGASUS_MOCK_VOICE"); mockMode != "" || os.Getenv("OPENAGY_MOCK_VOICE") != "" {
		mb := voicebackend.NewMockLiveBackend()
		pcm24k := make([]byte, 4800)
		switch mockMode {
		case "interim":
			interimText := os.Getenv("PIGASUS_MOCK_INTERIM")
			if interimText == "" {
				interimText = "what is this project?"
			}
			mb.SetCustomEvents([]*voicebackend.LiveServerEvent{
				{UserSpeech: interimText, Interim: true},
			})
		case "speech", "simulate":
			mb.SetCustomEvents([]*voicebackend.LiveServerEvent{
				{UserSpeech: "what is this project?", Interim: false},
				{VoiceTranscript: "This is Pigasus, an extensible clean-room AI agent engine.", Audio: pcm24k, TurnComplete: true},
			})
		}
		backend = mb
	} else if apiKey != "" {
		backend = voicebackend.NewGeminiLiveBackend(apiKey)
	} else if tok, err := auth.LoadTokenUnified(); err == nil && tok != nil && tok.Token.AccessToken != "" {
		fallback := voicebackend.NewGeminiLiveBackendWithBearer(tok.Token.AccessToken)
		backend = voicebackend.NewAICodeLiveBackendWithFallback(tok.Token.AccessToken, fallback)
	} else if voiceEndpoint != endpoints.DefaultVoice {
		backend = voicebackend.NewGeminiLiveBackend("")
	} else {
		return errors.New("Live mode isn't available in this session. Sign in with 'pigasus login' or configure a Gemini API key.")
	}

	sess := voicebackend.NewNativeLiveSession(backend, &tuiLiveHandler{m: m})
	sess.SetDuplex(m.audio.duplex)
	if len(msgs) > 0 {
		var initialTurns []voicebackend.LiveTurnRecord
		for _, msg := range msgs {
			role := "User"
			if msg.Role == "assistant" {
				role = "Agent"
			}
			content := strings.TrimSpace(msg.Content)
			if content != "" {
				initialTurns = append(initialTurns, voicebackend.LiveTurnRecord{Role: role, Content: content})
			}
		}
		if len(initialTurns) > 0 {
			sess.SetConversationHistory(initialTurns)
		}
	}
	if err := sess.Start(context.Background(), cfg); err != nil {
		return err
	}
	m.audio.liveSession = sess
	if m.session.Domain() != nil {
		m.session.Domain().SetLiveVoiceRouter(sess)
	}
	return nil
}

func (m *RootModel) StopLiveVoice() error {
	if m.session.Domain() != nil {
		m.session.Domain().SetLiveVoiceRouter(nil)
	}
	if m.audio.liveSession != nil {
		_ = m.audio.liveSession.HangUp()
		m.audio.liveSession = nil
	}
	return nil
}

func (m *RootModel) ToggleLiveMute() bool {
	if m.audio.liveSession != nil {
		m.audio.muted = m.audio.liveSession.ToggleMute()
		return m.audio.muted
	}
	m.audio.muted = !m.audio.muted
	return m.audio.muted
}

func (m *RootModel) IsLiveMuted() bool {
	if m.audio.liveSession != nil {
		return m.audio.liveSession.IsMuted()
	}
	return m.audio.muted
}

func (m *RootModel) IsLiveSpeaking() bool {
	if m.audio.liveSession != nil {
		return m.audio.liveSession.Speaking()
	}
	return false
}

func (m *RootModel) IsPromptEmpty() bool {
	return strings.TrimSpace(m.prompt.input.Value()) == ""
}

func (m *RootModel) IsLiveThinking() bool {
	return m.audio.thinking
}

type tuiLiveHandler struct {
	m *RootModel
}

func (h *tuiLiveHandler) OnUserSpeech(text string, interim bool) {
	if h.m == nil || h.m.turn.prog == nil {
		return
	}
	if interim {
		h.m.turn.prog.Send(voiceUserSpeechInterimMsg{text: text})
	} else if text != "" {
		h.m.turn.prog.Send(voiceUserSpeechInterimMsg{text: ""})
		h.m.turn.prog.Send(voiceUserSpeechMsg{text: text})
	}
}

func (h *tuiLiveHandler) OnAgentSpeech(text string) {
	if text != "" && h.m != nil && h.m.turn.prog != nil {
		h.m.turn.prog.Send(voiceAgentDoneMsg{text: text})
	}
}

func (h *tuiLiveHandler) OnAgentSpeechDelta(delta string) {
	if delta != "" && h.m != nil && h.m.turn.prog != nil {
		h.m.turn.prog.Send(voiceAgentDeltaMsg{delta: delta})
	}
}

func (h *tuiLiveHandler) OnTurnComplete() {}

func (h *tuiLiveHandler) OnInterrupted() {
	if h.m != nil && h.m.turn.prog != nil {
		h.m.turn.prog.Send(voiceInterruptedMsg{})
	}
}

func (h *tuiLiveHandler) OnToolCall(call voicebackend.LiveToolCall) {}

func (h *tuiLiveHandler) OnThinkerHandoff(invocationID string, task string) {
	if h.m != nil && h.m.turn.prog != nil && strings.TrimSpace(task) != "" {
		h.m.turn.prog.Send(voiceThinkerHandoffMsg{
			invocationID: invocationID,
			task:         task,
		})
	}
}

func (h *tuiLiveHandler) OnThinkerCancel(invocationID string) {
	if h.m == nil {
		return
	}
	activeID, cancelled := h.m.audio.cancelActiveThinker()
	if invocationID == "" {
		invocationID = activeID
	}
	if sess := h.m.audio.liveSession; sess != nil && sess.IsActive() {
		_ = sess.SendTaskStopped()
		if invocationID != "" {
			_ = sess.CompleteHandoff(invocationID, map[string]any{"cancelled": true})
		}
	}
	if cancelled && h.m.turn.prog != nil {
		h.m.turn.prog.Send(voiceThinkerDoneMsg{
			invocationID: invocationID,
			err:          context.Canceled,
		})
	}
}

func formatPermissionForVoice(req permissionprompt.Request) string {
	tool := strings.TrimSpace(req.Tool)
	cmd := strings.TrimSpace(req.Command)
	desc := strings.TrimSpace(req.Description)
	switch {
	case tool != "" && cmd != "":
		return fmt.Sprintf("Approve %s: %s?", tool, cmd)
	case desc != "" && desc != tool:
		return fmt.Sprintf("Approve %s (%s)?", tool, desc)
	case tool != "":
		return fmt.Sprintf("Approve %s?", tool)
	default:
		return "Approve tool execution?"
	}
}

func (m *RootModel) notifyVoiceWaitingOnUser(question string) {
	if m == nil || m.audio.liveSession == nil || !m.audio.liveSession.IsActive() {
		return
	}
	question = strings.TrimSpace(question)
	if question == "" {
		question = "Waiting for your confirmation."
	}
	_ = m.audio.liveSession.SendWaitingOnUser(question)
}

// RunThinkerTask executes a Thinker turn using the active turn engine and tools.
func (m *RootModel) RunThinkerTask(task string, onProgress func(string)) (string, error) {
	return m.RunThinkerTaskWithContext(context.Background(), task, onProgress)
}

// RunThinkerTaskWithContext executes a Thinker turn using the active turn engine and tools under ctx.
func (m *RootModel) RunThinkerTaskWithContext(ctx context.Context, task string, onProgress func(string)) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	bridge := NewEngineBridge(BridgeOptions{
		Engine:    m.turnEngine(),
		Program:   m.turn.prog,
		PromptMgr: DefaultPermissionPrompts,
		OnUsage:   m.recordTurnUsage,
		OnFrame: func(f agentlink.Frame) {
			if f.Tool != nil && onProgress != nil {
				onProgress(voicebackend.HumanizeTool(f.Tool.Name, f.Tool.Args))
			}
		},
		OnPermissionPrompt: func(req permissionprompt.Request) {
			m.notifyVoiceWaitingOnUser(formatPermissionForVoice(req))
		},
		HookStart:    m.hookSessionStart,
		HookPre:      m.hookPreInvocation,
		HookStop:     m.hookStop,
		HookPost:     m.hookPostInvocation,
		SessionEpoch: func() uint64 { return m.session.epoch() },
	})

	turnCtx, endTurn := bridge.turns.Begin(ctx)
	defer endTurn()
	turnCtx = interaction.WithHost(turnCtx, m.turnInteractionHost())

	var talkerCtx string
	if m.audio.liveSession != nil {
		turns := m.audio.liveSession.ConversationHistory()
		var sb strings.Builder
		for _, t := range turns {
			role := strings.ToLower(t.Role)
			if strings.Contains(role, "user") {
				role = "user"
			} else {
				role = "talker"
			}
			sb.WriteString(fmt.Sprintf("%s: %q\n", role, t.Content))
		}
		talkerCtx = sb.String()
	}

	content := fmt.Sprintf(`<voice_instructions>
With <talker_agent_context>, a live voice handles casual chat, sends tasks here, and speaks your reply. Without it, reply in markdown.

1. Share one persona with the voice: continue from its last line with no filler, restatements, or mentions of the voice or instructions, correcting any hedge or small-talk slip cleanly.
2. Fulfill any running task and the new request unless it cancels, replaces, or narrows the first; on stop, cancel, or ignore, stop immediately and reply briefly.
3. Act only on the current request, using prior lines only as context.

<talker_agent_context> lists timestamped user and talker lines oldest first (trailing hyphen marks cutoffs), matched in order to attached audio/wav clips. Check audio if speech looks misheard or clipped, honor self-corrections, and ask one short question instead of guessing unclear targets. Claim finished or logged only after tool success, treat the chat screen as a live canvas (rendering interactive generative UI, visuals, diagrams, tables, cards, code, printouts, or clickable links directly on screen whenever asked), and state key facts in brief prose with requested lists, shortlinks, URLs, slash commands, caveats, options, and colleague names.
</voice_instructions>
<talker_agent_context>
%s</talker_agent_context>
User request: %s`, talkerCtx, task)

	userMsg := backend.Message{
		Role:        "user",
		Content:     content,
		WireContent: backend.WrapUserContent(content, catalog.FormatModelLabel(m.turnModel()), m.session.wireSettingsPending, time.Now()),
	}

	msgs := append([]backend.Message(nil), m.SessionMessages()...)
	msgs = append(msgs, userMsg)

	rawMsg := bridge.runTurnOn(turnCtx, m.turnModel(), msgs)
	var finalAnswer string
	var runErr error

	if done, ok := rawMsg.(toolsteps.TurnDoneMsg); ok {
		if done.Err != nil {
			runErr = done.Err
		} else if done.Reply != "" {
			finalAnswer = done.Reply
		} else if len(done.Steps) > 0 {
			lastStep := done.Steps[len(done.Steps)-1]
			if lastStep.Result != "" {
				finalAnswer = lastStep.Result
			} else {
				finalAnswer = fmt.Sprintf("Completed %d tool steps", len(done.Steps))
			}
		} else {
			finalAnswer = "Task completed successfully."
		}
	} else {
		finalAnswer = "Task completed."
	}

	if runErr != nil {
		return "", runErr
	}

	m.session.appendMessage(userMsg)
	m.session.appendMessage(backend.Message{
		Role:    "assistant",
		Content: finalAnswer,
	})
	return finalAnswer, nil
}

func (h *tuiLiveHandler) OnError(err error) {
	if h.m != nil && err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "insufficient authentication scopes") {
			errMsg = "Google AI Studio live connection rejected (insufficient authentication scopes). Set GEMINI_API_KEY or configure a proxy endpoint in ~/.openagy/config.json."
		}
		h.m.slashRespond("/voice", fmt.Sprintf("Live error: %s", errMsg))
		if h.m.audio.liveSession != nil {
			_ = h.m.audio.liveSession.HangUp()
			h.m.audio.liveSession = nil
		}
		if h.m.audio.Surface != nil && h.m.audio.Surface.Active() {
			h.m.audio.Surface.Discard()
		}
	}
}

func (h *tuiLiveHandler) OnClose() {
	if h.m != nil {
		if h.m.audio.liveSession != nil {
			_ = h.m.audio.liveSession.HangUp()
			h.m.audio.liveSession = nil
		}
		if h.m.audio.Surface != nil && h.m.audio.Surface.Active() {
			_ = h.m.audio.Surface.Stop()
		}
	}
}

func (h *tuiLiveHandler) OnReconnecting(attempt int, maxAttempts int, err error) {
	// Reconnection in live call is handled seamlessly in the background and reflected in status
}

func (h *tuiLiveHandler) OnReconnected() {
	// Reconnected silently without polluting transcript
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
	if cmd == "terminal" {
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
	lines := m.panelLines()
	m.overlays.panelHeight = len(lines)
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(l)
		b.WriteString("\n")
	}
	if m.overlays.panel.kind.TrailingBlank() && !m.isFullViewportPanel() {
		b.WriteString("\n")
		m.overlays.panelHeight++
	}
	return b.String()
}

func (m *RootModel) rawPanelLines() []string {
	return m.getPanelRegistry().Lines(m.overlays.panel.kind)
}

func (m *RootModel) panelLines() []string {
	lines := m.rawPanelLines()
	if sr, sc, er, ec, ok := tuiviewport.SelectionRange(
		m.overlays.panelSelection.SelStartRow, m.overlays.panelSelection.SelStartCol,
		m.overlays.panelSelection.SelEndRow, m.overlays.panelSelection.SelEndCol,
		m.overlays.panelSelection.SelDragging,
	); ok {
		lines = tuiviewport.SelectionPass(lines, sr, sc, er, ec, true)
	}
	return lines
}

func (m *RootModel) panelSelectionText() string {
	sr, sc, er, ec, ok := tuiviewport.SelectionRange(
		m.overlays.panelSelection.SelStartRow, m.overlays.panelSelection.SelStartCol,
		m.overlays.panelSelection.SelEndRow, m.overlays.panelSelection.SelEndCol,
		m.overlays.panelSelection.SelDragging,
	)
	if !ok {
		return ""
	}
	lines := m.rawPanelLines()
	if m.overlays.panel.kind == panelregistry.Terminal {
		return terminalPanelSelectionText(lines, sr, sc, er, ec, len(m.remote.terminalOrder) > 1)
	}
	return tuiviewport.SelectionText(lines, sr, sc, er, ec)
}

func terminalPanelSelectionText(lines []string, sr, sc, er, ec int, hasTabs bool) string {
	if sr < 0 || sr >= len(lines) || er < 0 || er >= len(lines) {
		return ""
	}
	firstBody := 1
	if hasTabs {
		firstBody = 2
	}
	lastBody := len(lines) - 2
	var out []string
	for r := sr; r <= er; r++ {
		if r < firstBody || r > lastBody {
			continue
		}
		plain := ansi.Strip(lines[r])
		rw := ansi.StringWidth(plain)
		innerStart := 2
		innerEnd := rw - 2
		if innerEnd < innerStart {
			continue
		}
		from := innerStart
		to := innerEnd
		if r == sr && sc > from {
			from = sc
		}
		if r == er && ec+1 < to {
			to = ec + 1
		}
		if from > innerEnd {
			from = innerEnd
		}
		if to < innerStart {
			to = innerStart
		}
		if from > to {
			from = to
		}
		seg := ansi.Cut(plain, from, to)
		out = append(out, strings.TrimRight(seg, " "))
	}
	return strings.Join(out, "\n")
}

func (m *RootModel) closePanel() tea.Cmd {
	m.ResetPanelSelection()
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
	m.reloadConfigSettings()
	if m.prompt.vimEng != nil {
		m.prompt.vimEng.Mode = vim.Normal
	}
	if notice && cmd != "" {
		m.conversation.response = append(m.conversation.response, tuilib.SlashEchoLines(cmd, "Exited "+cmd+" command")...)
	}
	return nil
}

func (m *RootModel) reloadConfigSettings() {
	m.reloadVimSettings()
	if cliflags.StringValue(m.session.flags, "mode") == "" {
		if raw, err := settings.Field("agentMode"); err == nil {
			mode := configpanel.RawString(raw)
			if mode == "" && !settings.Has("agentMode") {
				mode = setup.DefaultMode()
			}
			m.session.mode = mode
			if m.session.policy != nil {
				m.session.policy.Mode = mode
			}
			m.prompt.input.Placeholder = m.promptPlaceholder(mode)
		}
	}
}

func (m *RootModel) closePanelSilent() tea.Cmd {
	m.ResetPanelSelection()
	if m.overlays.panel.kind == panelregistry.Help {
		if m.help.Panel != nil && m.help.Panel.Active() {
			m.help.Panel.Cancel()
		}
	}
	m.overlays.panel.csp.Cancel()
	m.overlays.panel = slashPanel{}
	m.reloadConfigSettings()
	if m.prompt.vimEng != nil {
		m.prompt.vimEng.Mode = vim.Normal
	}
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
	if m.overlays.panel.kind == panelregistry.Help {
		_, cmd, handled := m.handleHelpModeNavigation(key)
		return handled, cmd
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
			panelregistry.History:     func() int { return m.historyPanel().RowCount() },
			panelregistry.Terminal:    func() int { return m.terminalPanel().RowCount() },
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
	kind       panelregistry.Kind
	cmd        string
	cursor     int
	window     int
	logsFollow bool
	ag         *agentspanel.Panel
	fb         *feedbackpanel.Panel
	cr         *creditspanel.Panel
	plg        *pluginsui.Panel
	rn         *renamepanel.Panel
	ad         *adddirpanel.Panel
	vw         *viewerpanel.Panel
	csp        *codesearchpanel.Panel
	quota      quotaState
	prov       *providerpanel.Panel
	acct       *accountspanel.Panel
	cfg        *configpanel.Panel
	mdl        *modelpicker.Panel
	sk         *skillsui.Panel
	perm       *permissionspanel.Panel
	hk         *hookspanel.Panel
	tp         *taskspanel.Panel
	ef         *effortpanel.Panel
	proj       *projectspanel.Panel
	ctxp       *contextpanel.Panel
	hist       *historypanel.Panel
	term       *terminalpanel.Panel
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

func (m *RootModel) initOpenedSlashPanelData(kind panelregistry.Kind) {
	if kind == panelregistry.Usage && m.viewport.height > 0 {
		m.overlays.panel.quota = quotaState{state: quotaLoading}
	}
	if kind.IsViewer() {
		m.initViewerViewport()
	}
	if kind == panelregistry.Changelog {
		changelog.TriggerOnlineSync()
	}
	switch kind {
	case panelregistry.Provider:
		m.overlays.panel.prov = m.newProviderPanel()
		m.overlays.panel.prov.Reload()
		m.overlays.panel.prov.Notice("")
	case panelregistry.Config:
		m.overlays.panel.cfg = m.newConfigPanel()
	case panelregistry.Model:
		m.overlays.panel.mdl = m.newModelPanel()
	case panelregistry.Plugin:
		m.overlays.panel.plg = m.newPluginPanel()
	case panelregistry.Skills:
		m.overlays.panel.sk = m.newSkillsPanel()
	case panelregistry.Permissions:
		m.overlays.panel.perm = m.newPermissionsPanel()
	case panelregistry.Hooks:
		m.overlays.panel.hk = m.newHooksPanel()
	case panelregistry.Agents:
		m.overlays.panel.ag = m.newAgentsPanel()
	case panelregistry.Feedback:
		m.overlays.panel.fb = m.newFeedbackPanel()
	case panelregistry.Credits:
		m.overlays.panel.cr = m.newCreditsPanel()
	case panelregistry.Tasks:
		m.overlays.panel.tp = m.newTasksPanel()
	case panelregistry.Effort:
		m.overlays.panel.ef = m.newEffortPanel()
	case panelregistry.CodeSearch:
		m.overlays.panel.csp = m.newCodesearchPanel()
	case panelregistry.Rewind:
		m.rewind.Reopen(m)
	case panelregistry.Diff:
		m.diff.Reopen(m)
	case panelregistry.Rename:
		m.overlays.panel.rn = m.newRenamePanel()
		m.armRenameFocus()
	case panelregistry.AddDir:
		m.overlays.panel.ad = m.newAddDirPanel()
	case panelregistry.Context:
		m.overlays.panel.ctxp = m.newContextPanel()
	case panelregistry.Projects:
		m.overlays.panel.proj = m.newProjectsPanel()
	case panelregistry.Accounts:
		m.overlays.panel.acct = m.newAccountsPanel()
		m.overlays.panel.acct.Reload()
		m.overlays.panel.acct.ClearNotice()
		if m.overlays.panel.cmd == "/pool" {
			m.overlays.panel.acct.SetTab(accountspanel.TabPool)
		}
		m.overlays.panel.acct.ReloadPolicy()
	case panelregistry.History:
		m.overlays.panel.hist = m.newHistoryPanel()
	case panelregistry.Terminal:
		m.overlays.panel.term = m.terminalPanel()
	case panelregistry.Logs:
		m.overlays.panel.logsFollow = true
		m.overlays.panel.window = tuiviewer.MaxOffset(m.logsViewerSpec())
		if m.overlays.panel.vw != nil {
			m.overlays.panel.vw.Sync(m.overlays.panel.window)
		}
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
	lbl, _ := catalog.ResolveEffectiveModel("")
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
	if !catalog.EffortAdjustableFor(model) {
		m.slashRespond("/effort", models.NotAdjustableMessage(models.BaseLabel(model)))
		return nil
	}
	before := model
	label := catalog.EffortLabelFor(model, name)
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
	case panelregistry.Context:
		return m.contextPanel()
	case panelregistry.History:
		return m.historyPanel()
	case panelregistry.Terminal:
		return m.terminalPanel()
	default:
		return nil
	}
}

func (m *RootModel) newContextPanel() *contextpanel.Panel {
	return contextpanel.New(contextpanel.Seams{
		State:  func() contextpanel.State { return m.contextState() },
		Height: func() int { return m.viewport.height },
		Width:  func() int { return m.viewport.width },
		Close:  func() tea.Cmd { return m.closePanel() },
	})
}

func (m *RootModel) contextPanel() *contextpanel.Panel {
	if m.overlays.panel.ctxp == nil {
		m.overlays.panel.ctxp = m.newContextPanel()
	}
	return m.overlays.panel.ctxp
}

func (m *RootModel) terminalPanel() *terminalpanel.Panel {
	if sess := m.activeTerminalSession(); sess != nil && sess.panel != nil {
		m.overlays.panel.term = sess.panel
		return sess.panel
	}
	if m.overlays.panel.term == nil {
		m.overlays.panel.term = m.newTerminalPanelForSession(m.remote.terminalID)
	}
	return m.overlays.panel.term
}

func (m *RootModel) newTerminalPanelForSession(sessionID string) *terminalpanel.Panel {
	rows, cols := m.terminalInnerDimensions()
	resolveID := func() string {
		if sessionID != "" {
			return sessionID
		}
		return m.remote.terminalID
	}
	return terminalpanel.New(rows, cols, terminalpanel.Seams{
		WriteInput: func(data []byte) error {
			id := resolveID()
			if svc := m.terminalService(); svc != nil && id != "" {
				ctx, cancel := remoteTerminalContext()
				defer cancel()
				return svc.SendTerminalInput(ctx, id, data)
			}
			return nil
		},
		Resize: func(rows, cols int) error {
			id := resolveID()
			if svc := m.terminalService(); svc != nil && id != "" {
				ctx, cancel := remoteTerminalContext()
				defer cancel()
				return svc.Resize(ctx, id, rows, cols)
			}
			return nil
		},
		Close: func() tea.Cmd {
			id := resolveID()
			if id != "" {
				cmd, _ := m.killTerminalSession(id)
				return cmd
			}
			return m.closePanel()
		},
		Detach: func() tea.Cmd {
			return m.closePanel()
		},
		Title: func() string {
			id := resolveID()
			if id != "" {
				if sess := m.remote.terminals[id]; sess != nil && sess.title != "" {
					return sess.title
				}
				return "Terminal (" + id + ")"
			}
			return "Terminal"
		},
		Cwd: func() string {
			id := resolveID()
			if id != "" {
				if sess := m.remote.terminals[id]; sess != nil && sess.cwd != "" {
					return sess.cwd
				}
			}
			return m.session.cwd
		},
		CopyText: clipboard.Write,
		Tabs:     m.terminalTabs,
		NewTab: func() tea.Cmd {
			_, cmd, err := m.createAndOpenTerminalSession()
			if err != nil {
				m.slashRespond("/terminal", "error: "+err.Error())
				return nil
			}
			return cmd
		},
		SwitchTab: func(id string) tea.Cmd {
			return m.switchTerminalSession(id)
		},
		CloseTab: func(id string) tea.Cmd {
			cmd, _ := m.killTerminalSession(id)
			return cmd
		},
		Rename: func(id, title string) {
			if id == "" {
				id = resolveID()
			}
			if sess := m.remote.terminals[id]; sess != nil {
				sess.title = title
				if sess.panel != nil {
					sess.panel.SetTitle(title)
				}
			}
		},
		Height: func() int { return m.viewport.height },
		Width:  func() int { return m.viewport.width },
	})
}

func (m *RootModel) collectContextFileSections() []contextpanel.FileSection {
	var sections []contextpanel.FileSection
	home, _ := os.UserHomeDir()

	// 1. Artifact files · /artifact
	var artifactEntries []contextpanel.FileEntry
	brainDir := filepath.Join(confhome.Antigravity(), "brain", m.session.conversationID)
	if entries, err := os.ReadDir(brainDir); err == nil {
		for _, e := range entries {
			if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			full := filepath.Join(brainDir, e.Name())
			if fi, err := e.Info(); err == nil && fi.Size() > 0 {
				tokens := int((fi.Size() + 3) / 4)
				artifactEntries = append(artifactEntries, contextpanel.FileEntry{
					Path:   tildeHome(full),
					Tokens: tokens,
				})
			}
		}
	}
	if len(artifactEntries) > 0 {
		sections = append(sections, contextpanel.FileSection{
			Title:   "Artifact files",
			Command: "/artifact",
			Files:   artifactEntries,
		})
	}

	// 2. Skill files · /skill
	var skillEntries []contextpanel.FileEntry
	skillDirs := []string{
		filepath.Join(home, ".gemini", "skills"),
		filepath.Join(confhome.Antigravity(), "skills"),
	}
	for _, sdir := range skillDirs {
		if entries, err := os.ReadDir(sdir); err == nil {
			for _, e := range entries {
				if e.IsDir() {
					skillPath := filepath.Join(sdir, e.Name(), "SKILL.md")
					if fi, err := os.Stat(skillPath); err == nil && fi.Size() > 0 {
						tokens := int((fi.Size() + 3) / 4)
						skillEntries = append(skillEntries, contextpanel.FileEntry{
							Path:   tildeHome(skillPath),
							Tokens: tokens,
						})
						if len(skillEntries) >= 5 {
							break
						}
					}
				}
			}
		}
		if len(skillEntries) >= 5 {
			break
		}
	}
	if len(skillEntries) > 0 {
		sections = append(sections, contextpanel.FileSection{
			Title:   "Skill files",
			Command: "/skill",
			Files:   skillEntries,
		})
	}

	// 3. Knowledge files · /rules
	var ruleEntries []contextpanel.FileEntry
	ws := m.session.cwd
	if ws == "" {
		ws, _ = os.Getwd()
	}
	agentsPath := filepath.Join(ws, "AGENTS.md")
	if fi, err := os.Stat(agentsPath); err == nil && fi.Size() > 0 {
		tokens := int((fi.Size() + 3) / 4)
		ruleEntries = append(ruleEntries, contextpanel.FileEntry{
			Path:   tildeHome(agentsPath),
			Tokens: tokens,
		})
	}
	if len(ruleEntries) > 0 {
		sections = append(sections, contextpanel.FileSection{
			Title:   "Knowledge files",
			Command: "/rules",
			Files:   ruleEntries,
		})
	}

	// 4. System files · auto-loaded
	var systemEntries []contextpanel.FileEntry
	sysCandidates := []string{
		filepath.Join(home, ".gemini", "GEMINI.md"),
		filepath.Join(home, ".gemini", "skills"),
		filepath.Join(home, ".gemini", "antigravity", "skills"),
		filepath.Join(ws, ".agents", "agents"),
	}
	for _, p := range sysCandidates {
		if fi, err := os.Stat(p); err == nil {
			display := tildeHome(p)
			if fi.IsDir() && !strings.HasSuffix(display, "/") {
				display += "/"
			}
			systemEntries = append(systemEntries, contextpanel.FileEntry{
				Path: display,
			})
		}
	}
	if len(systemEntries) > 0 {
		sections = append(sections, contextpanel.FileSection{
			Title:   "System files",
			Command: "auto-loaded",
			Files:   systemEntries,
		})
	}

	return sections
}

func tildeHome(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" || home == "/" {
		return p
	}
	if p == home {
		return "~"
	}
	if strings.HasPrefix(p, home+string(filepath.Separator)) {
		return "~" + p[len(home):]
	}
	return p
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

func (m *RootModel) PanelLogsLines() []string {
	return m.panelLogsLines()
}

func (m *RootModel) LogsViewerSpec() tuiviewer.Spec {
	return m.logsViewerSpec()
}

func (m *RootModel) buildPanelRegistry() *panelregistry.Registry {
	return panels.BuildRegistry(m)
}
