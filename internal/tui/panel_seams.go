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
	"github.com/justwasm/pigasus/internal/core/logts"
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

// PanelHostSeams adapts runMode to the panel builders in internal/cliapp/panels.

// Tasks
func (m *runMode) TaskRegistry() *taskregistry.Registry  { return m.engine.taskReg }
func (m *runMode) KillSelectedTask(t *taskregistry.Task) { m.killSelectedTask(t) }
func (m *runMode) TerminalSize() (int, int)              { return m.viewport.width, m.viewport.height }
func (m *runMode) ClosePanel() tea.Cmd                   { return m.closePanel() }
func (m *runMode) newTasksPanel() *taskspanel.Panel      { return panels.NewTasksPanel(m) }
func (m *runMode) tasksPanel() *taskspanel.Panel {
	if m.overlays.panel.tp == nil {
		m.overlays.panel.tp = m.newTasksPanel()
	}
	return m.overlays.panel.tp
}

func (m *runMode) killSelectedTask(t *taskregistry.Task) {
	if t == nil {
		return
	}
	mgr := m.engine.taskSubagentManager
	if mgr == nil {
		mgr = subagent.NewManager()
	}
	taskregistry.KillByID(m.engine.taskReg, t.ID, mgr)
}

func (m *runMode) openTasksDetail(id string, isSubagent bool) {
	m.openSlashPanel("tasks")
	if m.overlays.panel.tp != nil {
		m.overlays.panel.tp.OpenTaskDetail(id, isSubagent)
	}
	m.viewport.activeItems.SetFocus(false)
}

func (m *runMode) tasksStatusSuffix() string {
	taskPart := ""
	reg := m.engine.taskReg
	if m.engine.taskCtrl != nil && m.engine.taskCtrl.Registry() != nil {
		reg = m.engine.taskCtrl.Registry()
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

func (m *runMode) startTasksSubscription() {
	if m.engine.taskCtrl != nil {
		m.engine.taskCtrl.StartSubscription()
		return
	}
	if m.engine.taskReg == nil || m.engine.tasksSubscribed {
		return
	}
	ch, cancel := m.engine.taskReg.Subscribe()
	m.engine.tasksSubCancel = cancel
	m.engine.tasksSubscribed = true
	go func() {
		for ev := range ch {
			_ = ev
			atomic.StoreInt32(&m.engine.tasksDirty, 1)
		}
	}()
}

func (m *runMode) consumeTasksDirty() bool {
	if m.engine.taskCtrl != nil {
		return m.engine.taskCtrl.ConsumeDirty()
	}
	return atomic.SwapInt32(&m.engine.tasksDirty, 0) == 1
}

type tasksTickMsg struct{}

func armTasksTick() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return tasksTickMsg{} })
}

// Config
func (m *runMode) RefreshSeasonalTheme() { m.refreshSeasonalTheme() }
func (m *runMode) SlashRespond(cmd, msg string, extra ...string) {
	m.slashRespond(cmd, msg, extra...)
}
func (m *runMode) SeasonalNow() time.Time { return seasonalNow() }
func (m *runMode) ConfigOverrideFor(row configpanel.Row) (string, bool) {
	return m.configOverrideFor(row)
}
func (m *runMode) ApplyThemeLive(scheme string) tea.Cmd { return m.applyThemeLive(scheme) }
func (m *runMode) ApplyAltScreenLive(stored any)        { m.applyAltScreenLive(stored) }
func (m *runMode) ApplyVerbosityLive(verbosity string)  { m.applyVerbosityLive(verbosity) }
func (m *runMode) newConfigPanel() *configpanel.Panel   { return panels.NewConfigPanel(m) }
func (m *runMode) configPanel() *configpanel.Panel {
	if m.overlays.panel.cfg == nil {
		m.overlays.panel.cfg = m.newConfigPanel()
	}
	return m.overlays.panel.cfg
}

func (m *runMode) applyThemeLive(scheme string) tea.Cmd {
	palette.SetScheme(scheme)
	mdrender.SetColorScheme(scheme)
	promptColor, placeholderColor := themepreview.ColorsFor(scheme)
	if m != nil {
		m.viewport.colorScheme = scheme
		st := m.composer.input.Styles()
		promptStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(promptColor))
		placeholderStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(placeholderColor))
		st.Focused.Prompt = promptStyle
		st.Focused.Placeholder = placeholderStyle
		st.Blurred.Prompt = promptStyle
		st.Blurred.Placeholder = placeholderStyle
		m.composer.input.SetStyles(st)
		m.rerenderTranscriptForTheme()
	}
	return tea.ClearScreen
}

func (m *runMode) applyAltScreenLive(stored any) {
	m.viewport.altScreen = altScreenModeValue(stored)
}

func (m *runMode) applyVerbosityLive(verbosity string) {
	if m == nil {
		return
	}
	m.engine.verbosity = verbosity
	m.viewport.expandedUserToggled = false
	m.rerenderBlocks()
	m.rerenderLiveSteps()
}

func (m *runMode) configOverrideFor(row configpanel.Row) (string, bool) {
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
func (m *runMode) newPermissionsPanel() *permissionspanel.Panel { return panels.NewPermissionsPanel(m) }
func (m *runMode) permissionsPanel() *permissionspanel.Panel {
	if m.overlays.panel.perm == nil {
		m.overlays.panel.perm = m.newPermissionsPanel()
	}
	return m.overlays.panel.perm
}

// Hooks
func (m *runMode) newHooksPanel() *hookspanel.Panel { return panels.NewHooksPanel(m) }
func (m *runMode) hooksPanel() *hookspanel.Panel {
	if m.overlays.panel.hk == nil {
		m.overlays.panel.hk = m.newHooksPanel()
	}
	return m.overlays.panel.hk
}

// Plugins
var pluginWatcherPtr = &plugins.WatcherPtr

func (m *runMode) newPluginPanel() *pluginsui.Panel { return panels.NewPluginPanel(m) }
func (m *runMode) pluginPanel() *pluginsui.Panel {
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
func (m *runMode) OpenURL(url string) error             { return openBrowser(url) }
func (m *runMode) newCreditsPanel() *creditspanel.Panel { return panels.NewCreditsPanel(m) }
func (m *runMode) creditsPanel() *creditspanel.Panel {
	if m.overlays.panel.cr == nil {
		m.overlays.panel.cr = m.newCreditsPanel()
	}
	return m.overlays.panel.cr
}

// AddDir
func (m *runMode) ClosePanelSilent() tea.Cmd { return m.closePanelSilent() }
func (m *runMode) SubmitAddDir(path string) tea.Cmd {
	msg := tuilib.AddDirFeedback(path)
	m.slashRespond("/add-dir", msg)
	return m.closePanelSilent()
}
func (m *runMode) newAddDirPanel() *adddirpanel.Panel { return panels.NewAddDirPanel(m) }
func (m *runMode) addDirPanel() *adddirpanel.Panel {
	if m.overlays.panel.ad == nil {
		m.overlays.panel.ad = m.newAddDirPanel()
	}
	return m.overlays.panel.ad
}

// Feedback
func (m *runMode) SubmitFeedback() {
	m.submitFeedback()
}

func (m *runMode) submitFeedback() {
	m.overlays.panel = slashPanel{}
	m.slashRespond("/feedback", "Feedback recorded (openagy has no upload endpoint; saved locally only)")
}
func (m *runMode) newFeedbackPanel() *feedbackpanel.Panel { return panels.NewFeedbackPanel(m) }
func (m *runMode) feedbackPanel() *feedbackpanel.Panel {
	if m.overlays.panel.fb == nil {
		m.overlays.panel.fb = m.newFeedbackPanel()
	}
	return m.overlays.panel.fb
}

// Skills
func (m *runMode) SkillsCatalog() []skillsui.SkillInfo { return slashSkillCatalog() }
func (m *runMode) TerminalOSC8() bool                  { return m.viewport.hyperlinkCaps.OSC8 }
func (m *runMode) newSkillsPanel() *skillsui.Panel     { return panels.NewSkillsPanel(m) }
func (m *runMode) skillsPanel() *skillsui.Panel {
	if m.overlays.panel.sk == nil {
		m.overlays.panel.sk = m.newSkillsPanel()
	}
	return m.overlays.panel.sk
}

// Provider
func (m *runMode) OpenBrowser(url string) error           { return cli.OpenBrowser(url) }
func (m *runMode) ClipboardRead() string                  { return clipboardReadText() }
func (m *runMode) ClipboardWrite(text string) error       { return clipboardWrite(text) }
func (m *runMode) CurrentModelLabel() string              { return m.panelCurrentModelLabel() }
func (m *runMode) newProviderPanel() *providerpanel.Panel { return panels.NewProviderPanel(m) }
func (m *runMode) providerPanel() *providerpanel.Panel {
	if m.overlays.panel.prov == nil {
		m.overlays.panel.prov = m.newProviderPanel()
		m.overlays.panel.prov.Reload()
	}
	return m.overlays.panel.prov
}

// Rename
func (m *runMode) ConversationName() string { return m.session.conversationName }
func (m *runMode) SubmitRename(name string) tea.Cmd {
	next, msg := tuilib.RenameFeedback(name, m.session.conversationName)
	if msg != "" {
		m.slashRespond("/rename", msg)
	}
	if next != "" {
		m.session.conversationName = next
	}
	return m.closePanelSilent()
}
func (m *runMode) newRenamePanel() *renamepanel.Panel { return panels.NewRenamePanel(m) }
func (m *runMode) renamePanel() *renamepanel.Panel {
	if m.overlays.panel.rn == nil {
		m.overlays.panel.rn = m.newRenamePanel()
	}
	return m.overlays.panel.rn
}

func (m *runMode) SessionTierFor(email string) (string, bool) {
	if m == nil || !m.session.tierFetched {
		return "", false
	}
	if !strings.EqualFold(m.session.tierAccount, email) {
		return "", false
	}
	return m.session.tier, true
}

func (m *runMode) startAccountLogin() tea.Cmd {
	lm := loginui.New()
	lm.Width = m.viewport.width
	lm.Height = m.viewport.height
	m.overlays.login = lm
	return lm.Init()
}

func (m *runMode) ApplyActiveAccount(email string) {
	m.session.email = email
	m.forgetSessionTier()
	if m.engine.backendFor != nil {
		if b := m.engine.backendFor(m.session.model); b != nil {
			m.engine.backend = b
		}
	}
}

func (m *runMode) SignOutActiveAccount() {
	m.session.email = ""
}

func (m *runMode) forgetSessionTier() {
	m.session.tier = ""
	m.session.tierAccount = ""
	m.session.tierFetched = false
}

// plannerResponses returns the assistant ("planner") responses in order, which
// is what /copy indexes. The reference's command description calls them
// "planner response[s]".
func (m *runMode) plannerResponses() []string {
	if m.session.supervisor != nil && len(m.session.supervisor.Messages()) > 0 {
		return slashactions.PlannerResponses(m.session.supervisor.Messages())
	}
	return slashactions.PlannerResponses(m.engine.session)
}

// deadcode: staged (test seam for slash_actions_test.go)
func resolveCopy(args string, responses []string) (string, string) {
	return slashactions.ResolveCopy(args, responses)
}

// conversationTitle is the text /title names. The reference's value is
// unrecovered; a /rename name wins, then the first user message, then the
// program name — 【推测】.
func (m *runMode) conversationTitle() string {
	fallback := ""
	if len(os.Args) > 0 {
		fallback = os.Args[0]
	}
	sess := m.engine.session
	if m.session.supervisor != nil && len(m.session.supervisor.Messages()) > 0 {
		sess = m.session.supervisor.Messages()
	}
	return slashactions.ConversationTitle(m.session.conversationName, sess, fallback)
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

var browserAgentFactory = func(m *runMode) (browser.BrowserSubagentProvider, func(), error) {
	return m.newBrowserAgent()
}

var browserProbe = func(ctx context.Context) error {
	_, err := browsersubagent.Probe(ctx)
	return err
}

const browserAgentTimeout = 10 * time.Minute

func (m *runMode) runBrowserCommand(args string) (bool, tea.Cmd) {
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

func (m *runMode) runBrowserTask(runner browser.BrowserSubagentProvider, closeRunner func(), task string) tea.Cmd {
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

func (m *runMode) updateBrowserAgentDoneMsg(msg browserAgentDoneMsg) {
	if msg.err != nil {
		m.engine.response = append(m.engine.response,
			panelGray166+msg.err.Error()+panelGray166C,
			"",
		)
		return
	}
	answer := strings.TrimRight(strings.TrimSpace(msg.answer), "\n")
	if answer == "" {
		m.engine.response = append(m.engine.response,
			panelGray238+"(the browser agent returned no answer)"+panelGray238C,
			"",
		)
		return
	}
	for _, line := range strings.Split(answer, "\n") {
		m.engine.response = append(m.engine.response, line)
	}
	m.engine.response = append(m.engine.response, "")
}

func (m *runMode) newBrowserAgent() (browser.BrowserSubagentProvider, func(), error) {
	if m.engine.catalog == nil {
		return nil, nil, errors.New("the browser tools are not available in this session")
	}
	if m.session.agentBody != "" {
		return nil, nil, errors.New("this session runs an agent body, which has no browser agent")
	}
	if m.engine.backend == nil {
		return nil, nil, errors.New("no model is selected")
	}
	if err := browserProbe(context.Background()); err != nil {
		return nil, nil, err
	}
	workspace := m.session.cwd
	if workspace == "" {
		workspace = defaultWorkspaceForRunMode()
	}
	agentDir := confhome.Antigravity()
	provider := agentturn.NewBackendProviderAdapter(m.engine.backend, m.session.model)
	runner := browsersubagent.New(subagenttools.Deps{
		Provider: provider,
		Model:    m.session.model,
		CWD:      workspace,
		AgentDir: agentDir,
	}, browsersubagent.Tools(m.engine.catalog))
	return runner, func() {}, nil
}

const rcURLIndent = "     "
const rcURLDefaultWidth = 80

func (m *runMode) rcURLChunkWidth() int {
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

func narrationSessionStart(m *runMode) time.Time {
	if !m.session.narrationStart.IsZero() {
		return m.session.narrationStart
	}
	return time.Time{}
}

func narrationFeedback(m *runMode, args string) (text, echo string, mutedNow, already bool) {
	text, echo, mutedNow, already = narration.Feedback(args, narrationSessionStart(m), m.session.narrationOff)
	if mutedNow {
		m.session.narrationOff = true
	}
	return text, echo, mutedNow, already
}

func (m *runMode) handleNarrationSlash(args string) (bool, tea.Cmd) {
	text, _, mutedNow, already := narrationFeedback(m, args)
	if mutedNow || already {
		return true, nil
	}
	if text != "" {
		for _, ln := range strings.Split(text, "\n") {
			m.engine.response = append(m.engine.response, panelGray238+ln+panelGray238C)
		}
		m.engine.response = append(m.engine.response, "")
	}
	return true, nil
}

func (m *runMode) sessionTokenTotals() (prompt, completion int) {
	if m == nil {
		return 0, 0
	}
	return int(atomic.LoadInt64(&m.session.store.SessionPromptTokens)),
		int(atomic.LoadInt64(&m.session.store.SessionCompletionTokens))
}

func (m *runMode) recordTurnUsage(meta *backend.ChunkMeta) {
	if m == nil || meta == nil {
		return
	}
	atomic.AddInt64(&m.session.store.SessionPromptTokens, int64(meta.PromptTokens))
	atomic.AddInt64(&m.session.store.SessionCompletionTokens, int64(meta.CompletionTokens))
	atomic.StoreInt64(&m.session.store.LastTurnPromptTokens, int64(meta.PromptTokens))
	atomic.StoreInt64(&m.session.store.LastTurnCompletionTokens, int64(meta.CompletionTokens))
}

func (m *runMode) externalStateFor() *statusline.State {
	if m == nil {
		return nil
	}
	return m.viewport.statusLineExternalState
}

func (m *runMode) ensureExternalState() *statusline.State {
	if m == nil {
		return nil
	}
	if m.viewport.statusLineExternalState == nil {
		m.viewport.statusLineExternalState = &statusline.State{}
	}
	return m.viewport.statusLineExternalState
}

func (m *runMode) runExternalStatusLineTick() {
	if m == nil {
		return
	}
	statusCfg := m.viewport.statusLineCfg
	titleCfg := m.viewport.titleCfg
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

func (m *runMode) maybeArmStatusLineTick() tea.Cmd {
	if m == nil || m.session.store.StatusLineTickArmed {
		return nil
	}
	if !m.statusLineScriptActive() && !m.viewport.titleCfg.Active() {
		return nil
	}
	m.session.store.StatusLineTickArmed = true
	return armExternalStatusLineTick()
}

func (m *runMode) externalRenderOK() bool {
	if m == nil {
		return false
	}
	if m.viewport.statusLine == "off" {
		return false
	}
	if !m.statusLineScriptActive() {
		return false
	}
	state := m.externalStateFor()
	if state == nil {
		return false
	}
	out, _ := state.Current()
	return strings.TrimSpace(out) != ""
}

func (m *runMode) externalRenderedForRow() (string, error) {
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

func (m *runMode) buildStatusLinePayload() statusline.Payload {
	model := m.session.statusModel
	if model == "" {
		model = m.session.model
	}
	contextWindow := 0
	if model != "" {
		contextWindow = modelctx.ContextWindowFor(model)
	}
	taskCount := 0
	if m.engine.taskReg != nil {
		taskCount = m.engine.taskReg.CountRunning()
	}
	prompt, completion := m.sessionTokenTotals()
	return statusline.PayloadFrom(statusline.PayloadState{
		Cwd:                      m.session.cwd,
		ConversationID:           m.session.conversationID,
		ConversationTitle:        m.conversationTitle(),
		Model:                    m.session.model,
		Effort:                   m.currentEffortName(),
		AgentName:                m.session.agentName,
		CycleMode:                m.session.mode,
		PlanTier:                 m.session.tier,
		Email:                    m.session.email,
		TerminalWidth:            m.viewport.width,
		Streaming:                m.engine.streaming,
		ToolInFlight:             m.toolInFlight(),
		TurnFailed:               m.turnFailed(),
		SandboxEnabled:           m.engine.toolSandbox.Enabled,
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

func (m *runMode) agentState() string {
	state := statusline.AgentState(m.engine.streaming, m.toolInFlight(), m.turnFailed())
	if state == statusline.StateIdle {
		return ""
	}
	return state
}

func (m *runMode) toolInFlight() bool {
	return m.engine.activeTool != ""
}

func (m *runMode) turnFailed() bool {
	return m.engine.lastTurnOutcome == tea.ProgramStateError
}

func (m *runMode) pendingInputCount() int {
	n := len(m.session.store.StreamQueue)
	if m.session.store.QueuedPrompt != "" {
		n++
	}
	return n
}

func (m *runMode) subagentInfos() []statusline.SubagentInfo {
	if m.engine.subagentRegistry == nil {
		return nil
	}
	return statusline.Subagents(m.engine.subagentRegistry.States())
}

func (m *runMode) vimModeName() string {
	if !m.vimEnabled() {
		return ""
	}
	return statusline.VimModeName(m.vimMode())
}

func (m *runMode) contextUsedPercent() float64 {
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
func (m *runMode) contextUsage() float64 {
	if m == nil {
		return 0
	}
	return m.contextUsedPercent()
}

type StatusLinePayloadWithPlan struct {
	statusline.Payload
	Plan string `json:"plan,omitempty"`
}

func (m *runMode) buildStatusLinePayloadDoc() any {
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
func (m *runMode) statusLinePlanProgress() string {
	return m.enginePlan()
}

func (m *runMode) reloadStatusLineConfig() {
	if cfg, ok := statusline.LoadConfig(); ok {
		m.viewport.statusLineCfg = cfg
		m.viewport.statusLineCommand = cfg.Command
		m.viewport.statusLineExternal = cfg.Active()
	} else {
		m.viewport.statusLineCfg = statusline.Config{}
		m.viewport.statusLineCommand = ""
		m.viewport.statusLineExternal = false
	}
	if cfg, ok := statusline.LoadTitleConfig(); ok {
		m.viewport.titleCfg = cfg
		m.viewport.titleCommand = cfg.Command
		if cfg.Active() {
			m.viewport.title = "on"
		}
	} else {
		m.viewport.titleCfg = statusline.Config{}
		m.viewport.titleCommand = ""
	}
}

func (m *runMode) statusLineScriptActive() bool {
	if m.viewport.statusLineCfg.Active() {
		return true
	}
	return m.viewport.statusLineExternal
}

func (m *runMode) statusAreaState() statusline.AreaState {
	return statusline.AreaState{
		Builtin: statusrow.Render(m.statusRowState()),
		Active:  m.statusLineScriptActive(),
		Config:  m.viewport.statusLineCfg,
		State:   m.externalStateFor(),
		LogPath: confhome.CLILogPath(logts.FormatLogTS()),
	}
}

func (m *runMode) windowTitleText() string {
	if m.viewport.titleCfg.Active() {
		if title := m.titleStoreFor().Get(); title != "" {
			return title
		}
	}
	if m.viewport.title == "on" {
		return m.conversationTitle()
	}
	return ""
}

// DiffTurnCount
func (m *runMode) diffTurnCount() int {
	return m.DiffTurnCount()
}
func (m *runMode) SessionCwd() string             { return m.session.cwd }
func (m *runMode) DiffTurnCount() int             { return m.diffTurnCount() }
func (m *runMode) newDiffPanel() *diffpanel.Panel { return panels.NewDiffPanel(m) }
func (m *runMode) diffPanel() *diffpanel.Panel {
	if m.overlays.panel.df == nil {
		m.overlays.panel.df = m.newDiffPanel()
	}
	return m.overlays.panel.df
}

func (m *runMode) effortTiers() []string {
	return catalog.EffortTiersFor(m.effortModel())
}

func (m *runMode) effortModel() string {
	if label := strings.TrimSpace(m.session.model); label != "" && catalog.ModelAvailable(label) {
		return label
	}
	if effective, _ := catalog.ResolveEffectiveModel(""); strings.TrimSpace(effective) != "" {
		return effective
	}
	return catalog.DefaultModel
}

func (m *runMode) EffortTiers() []string {
	return m.effortTiers()
}

func (m *runMode) CurrentEffortName() string {
	return m.currentEffortName()
}
func (m *runMode) ApplyEffortSelection(level int) tea.Cmd { return m.applyEffortSelection(level) }
func (m *runMode) newEffortPanel() *effortpanel.Panel     { return panels.NewEffortPanel(m) }
func (m *runMode) effortPanel() *effortpanel.Panel {
	if m.overlays.panel.ef == nil {
		m.overlays.panel.ef = m.newEffortPanel()
	}
	return m.overlays.panel.ef
}

// Help
func (m *runMode) ModelName() string             { return m.session.model }
func (m *runMode) TierName() string              { return m.session.tier }
func (m *runMode) UserEmail() string             { return m.session.email }
func (m *runMode) ConversationID() string        { return m.session.conversationID }
func (m *runMode) AppendResponse(lines []string) { m.engine.response = append(m.engine.response, lines...) }
func (m *runMode) SelectCommand(cmd string) {
	m.composer.input.SetValue(cmd)
	m.composer.input.CursorEnd()
	m.composer.picker.Reset()
	m.composer.promptHistory.Reset()
	m.ResetInputSelection()
}
func (m *runMode) newHelpPanel() *helppanel.Panel { return panels.NewHelpPanel(m) }

// ActiveItems
func (m *runMode) OpenTasksDetail(id string, isSubagent bool)   { m.openTasksDetail(id, isSubagent) }
func (m *runMode) newActiveItemsPanel() *activeitemspanel.Panel { return panels.NewActiveItemsPanel(m) }

// Accounts
func (m *runMode) StartAccountLogin() tea.Cmd             { return m.startAccountLogin() }
func (m *runMode) FetchPlanTierCmd() tea.Cmd              { return m.fetchPlanTierCmd() }
func (m *runMode) FetchQuotaCmd(email string) tea.Cmd     { return fetchAccountQuotaCmd(email) }
func (m *runMode) newAccountsPanel() *accountspanel.Panel { return panels.NewAccountsPanel(m) }
func (m *runMode) accountsPanel() *accountspanel.Panel {
	if m.overlays.panel.acct == nil {
		m.overlays.panel.acct = m.newAccountsPanel()
	}
	return m.overlays.panel.acct
}

// Agents
func (m *runMode) ActiveAgentName() string                { return m.session.agentName }
func (m *runMode) RunningSubagents() []*subagent.Subagent { return m.runningSubagents() }
func (m *runMode) HasSessionTurns() bool                  { return len(m.engine.session) > 0 }
func (m *runMode) ApplyAgentSwitch(name, body string) {
	m.applyAgentSwitch(name, body)
	m.overlays.panel = slashPanel{}
}
func (m *runMode) KillAgentsSubagent(sa *subagent.Subagent) bool { return m.killAgentsSubagent(sa) }
func (m *runMode) PendingSubagentAuthorization(id string) (lifecycle.PendingApproval, bool) {
	return m.pendingSubagentAuthorization(id)
}
func (m *runMode) AnswerSubagentAuthorization(id string, approved bool) bool {
	return m.answerSubagentAuthorization(id, approved)
}
func (m *runMode) AgentsWorkspace() string { return m.panelAgentsWorkspace() }
func (m *runMode) newAgentsPanel() *agentspanel.Panel {
	return panels.NewAgentsPanel(m)
}
func (m *runMode) agentsPanel() *agentspanel.Panel {
	if m.overlays.panel.ag == nil {
		m.overlays.panel.ag = m.newAgentsPanel()
	}
	return m.overlays.panel.ag
}
func (m *runMode) runningSubagents() []*subagent.Subagent {
	mgr := m.engine.taskSubagentManager
	if mgr == nil {
		return nil
	}
	return mgr.List()
}
func (m *runMode) killAgentsSubagent(sa *subagent.Subagent) bool {
	mgr := m.engine.taskSubagentManager
	if mgr == nil || sa == nil {
		return false
	}
	return mgr.KillRunning(sa.ID)
}
func (m *runMode) pendingSubagentAuthorization(subagentID string) (lifecycle.PendingApproval, bool) {
	mgr := m.engine.taskSubagentManager
	if mgr == nil {
		return lifecycle.PendingApproval{}, false
	}
	return mgr.PendingAuthorization(subagentID)
}
func (m *runMode) answerSubagentAuthorization(approvalID string, approve bool) bool {
	mgr := m.engine.taskSubagentManager
	if mgr == nil {
		return false
	}
	return mgr.AnswerAuthorization(approvalID, approve)
}
func (m *runMode) panelAgentsWorkspace() string {
	workspace := m.session.cwd
	if workspace == "" {
		workspace, _ = os.Getwd()
	}
	return workspace
}

// Codesearch
func (m *runMode) SearchRoot() string    { return m.session.cwd }
func (m *runMode) SearchStreaming() bool { return m.engine.streamingSearch }
func (m *runMode) CodesearchReadFile(path string) ([]string, string, bool) {
	return m.codesearchReadFile(path)
}
func (m *runMode) CodesearchOpenEditor(path string, line int) tea.Cmd {
	return m.codesearchOpenEditor(path, line)
}
func (m *runMode) CodesearchSendComments(text string) tea.Cmd {
	return m.codesearchSendComments(text)
}
func (m *runMode) newCodesearchPanel() *codesearchpanel.Panel {
	return panels.NewCodesearchPanel(m)
}
func (m *runMode) codesearchPanel() *codesearchpanel.Panel {
	if m.overlays.panel.csp == nil {
		m.overlays.panel.csp = m.newCodesearchPanel()
	}
	return m.overlays.panel.csp
}
func (m *runMode) codesearchReadFile(path string) ([]string, string, bool) {
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
func (m *runMode) codesearchPath(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(m.session.cwd, path)
}
func (m *runMode) codesearchOpenEditor(path string, line int) tea.Cmd {
	name, args, notice, err := editorkit.ResolveEditor(settings.String("editor"))
	if err != nil {
		m.engine.response = append(m.engine.response, "Failed to resolve editor: "+err.Error())
		return nil
	}
	if _, err := exec.LookPath(name); err != nil {
		m.engine.response = append(m.engine.response, "Failed to resolve editor: "+err.Error())
		return nil
	}
	if notice != "" {
		m.engine.response = append(m.engine.response, notice)
	}
	full := m.codesearchPath(path)
	c := exec.Command(name, append(args, editorLineArgs(name, full, line)...)...)
	c.Dir = m.session.cwd
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return editorFinishedMsg{err: err}
	})
}
func (m *runMode) codesearchSendComments(text string) tea.Cmd {
	closed := m.closePanel()
	if strings.TrimSpace(text) == "" {
		return closed
	}
	return tea.Batch(closed, m.startModelTurn(text))
}

// Model
func (m *runMode) ApplyModel(label string) {
	m.session.model = label
	m.session.statusModel = tuilib.StripTier(label)
	m.syncBackendForModel()
}
func (m *runMode) CustomModelRows() []modelpicker.Row { return m.engineModelRows() }
func (m *runMode) engineModelRows() []modelpicker.Row {
	if len(m.engine.engineModelRowsCache) > 0 {
		return m.engine.engineModelRowsCache
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
	m.engine.engineModelRowsCache = rows
	return rows
}

func (m *runMode) syncBackendForModel() {
	if m.engine.backendFor == nil || strings.TrimSpace(m.session.model) == "" {
		return
	}
	if b := m.engine.backendFor(m.session.model); b != nil {
		m.engine.backend = b
	}
	m.session.wireSettingsPending = true
}
func (m *runMode) newModelPanel() *modelpicker.Panel { return panels.NewModelPanel(m) }
func (m *runMode) modelPanel() *modelpicker.Panel {
	if m.overlays.panel.mdl == nil {
		m.overlays.panel.mdl = m.newModelPanel()
	}
	return m.overlays.panel.mdl
}

// Viewer
func (m *runMode) ViewerSpec() (tuiviewer.Spec, bool) {
	return m.getPanelRegistry().ViewerSpec(m.overlays.panel.kind)
}
func (m *runMode) ViewerPageRows(spec tuiviewer.Spec) int {
	if m.overlays.panel.kind == panelregistry.Changelog {
		return changelog.PageRows(spec, m.viewport.height)
	}
	return spec.Visible
}
func (m *runMode) ViewerWidth() int                   { return m.viewerVPWidth() }
func (m *runMode) newViewerPanel() *viewerpanel.Panel { return panels.NewViewerPanel(m) }
func (m *runMode) viewerPanel() *viewerpanel.Panel {
	if m.overlays.panel.vw == nil {
		p := m.newViewerPanel()
		p.Open()
		m.overlays.panel.vw = p
	}
	return m.overlays.panel.vw
}

func (m *runMode) initViewerViewport() {
	if m.overlays.panel.vw == nil {
		m.overlays.panel.vw = m.newViewerPanel()
	}
	m.overlays.panel.vw.Open()
}

func (m *runMode) viewerVPWidth() int {
	if m.viewport.width > 0 {
		return m.viewport.width
	}
	return 80
}

func (m *runMode) handleViewerPanelKey(key tea.Key) (bool, tea.Cmd) {
	handled, cmd, offset := m.viewerPanel().HandleKey(key, m.overlays.panel.window)
	m.overlays.panel.window = offset
	return handled, cmd
}

func (m *runMode) listResumeConversations() ([]convstore.ConversationMeta, error) {
	return convstore.ListConversations()
}

func (m *runMode) renameResumeConversation(meta convstore.ConversationMeta) {
	m.session.conversationName = meta.Title
	m.openRenamePanel()
}

func (m *runMode) openRenamePanel() {
	m.overlays.panel.kind = panelregistry.Rename
	m.overlays.panel.cmd = "/rename"
	m.overlays.panel.rn = m.newRenamePanel()
	m.armRenameFocus()
}

func (m *runMode) ContextState() contextpanel.State {
	return m.contextState()
}

func (m *runMode) contextState() contextpanel.State {
	st := contextpanel.State{
		Model: m.contextModel(),
		Width: m.viewport.width,
	}
	switch {
	case m.engine.streaming:
		st.Mode = contextpanel.ModeStreaming
		st.User, st.Agent, st.Tool = m.streamingTokenCounts()
	case len(m.engine.session) == 0:
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
		st.SystemMessages = contextpanel.SystemMessageTokens(m.engine.session)
	}
	st.CheckpointBuffer = contextpanel.CheckpointBufferTokens(m.engine.session)
	st.User -= st.CheckpointBuffer
	if st.User < 0 {
		st.User = 0
	}
	return st
}

func (m *runMode) promptCategoryTokens() (base, rules, skills, subagents int) {
	base, skills, subagents = m.promptSectionTokens()
	opts := m.promptOptions()
	rules = agyprompt.SectionTokenCount("user_rules", agyprompt.SectionBody(opts, "user_rules"))
	return base, rules, skills, subagents
}

func (m *runMode) contextModel() string {
	if m.session.model != "" {
		return m.session.model
	}
	return defaultModel
}

func (m *runMode) streamingTokenCounts() (user, agent, tool int) {
	user, _, tool = m.sessionTokenCounts()
	user = contextpanel.AdjustStreamingUserTokens(m.engine.session, user)
	completed := atomic.LoadInt64(&m.session.store.SessionCompletionTokens)
	lastTurn := atomic.LoadInt64(&m.session.store.LastTurnCompletionTokens)
	agent = contextpanel.StreamingAgentTokens(completed, lastTurn, len(m.engine.liveReplyBuf))
	return user, agent, tool
}

func (m *runMode) wireToolDecls() []backend.ToolDecl {
	return append([]backend.ToolDecl(nil), m.engine.tools...)
}

func (m *runMode) sessionTokenCounts() (user, agent, tool int) {
	user = contextpanel.UserMessageTokens(m.engine.session)
	completed := atomic.LoadInt64(&m.session.store.SessionCompletionTokens)
	lastTurn := atomic.LoadInt64(&m.session.store.LastTurnCompletionTokens)
	agent = contextpanel.AgentTokens(completed, lastTurn)
	var steps []toolsteps.Step
	for _, b := range m.engine.blocks {
		steps = append(steps, b.Steps...)
	}
	steps = append(steps, m.engine.liveSteps...)
	tool = contextpanel.ToolResultTokens(toolsteps.ToolResults(steps)...)
	return user, agent, tool
}

func (m *runMode) statusRowState() statusrow.State {
	panelOpen := m.overlays.panel.kind != panelregistry.None
	panelKeepsHint := false
	if panelOpen {
		panelKeepsHint = m.overlays.panel.kind.KeepStatusHint()
	}

	return statusrow.State{
		Width:          m.viewport.width,
		CanonicalWidth: separatorWidth,

		PickerVisible:      m.composer.picker.Visible(),
		PickerModelMode:    m.composer.picker.IsModelMode(),
		Streaming:          m.engine.streaming,
		Queued:             len(m.session.store.StreamQueue) + len(m.session.store.BashQueue),
		BufferEmpty:        strings.TrimSpace(m.composer.input.Value()) == "",
		ResumePickerActive: m.overlays.resumePicker.Active(),
		BashMode:           m.session.store.BashMode,
		HelpActive:         m.viewport.helpPanel.Active(),
		PanelOpen:          panelOpen,
		PanelKeepsHint:     panelKeepsHint,
		ExitConfirm:        m.engine.exitConfirm,
		VimBadge:           vimkeys.Badge(m.vimEnabled(), m.vimMode()),
		Compact:            m.compactState(),

		Model:        m.session.model,
		StatusModel:  m.session.statusModel,
		AgentName:    m.session.agentName,
		Mode:         m.session.mode,
		EffortSuffix: m.statusEffortSuffix(),
		TaskSuffix:   m.tasksStatusSuffix(),
		QueueMode:    m.session.store.QueueMode,
		AgentState:   m.agentState(),
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

func (m *runMode) compactState() *statusrow.Compact {
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

func (m *runMode) panelChangelogLines() []string {
	spec, _ := m.getPanelRegistry().ViewerSpec(m.overlays.panel.kind)
	rows, start, end := m.viewerWindow(spec)
	return changelog.PanelLines(rows, start, end, spec.Total)
}

func (m *runMode) usageViewerSpec() tuiviewer.Spec {
	if m.viewport.height > 0 {
		return usagepanel.Spec(m.usageState())
	}
	return usagepanel.StaticSpec()
}

func (m *runMode) changelogViewerSpec() tuiviewer.Spec {
	return tuiviewer.Spec{
		Lines:   changelog.Document(m.viewport.width, m.viewport.height),
		Visible: changelog.VisibleRows(m.viewport.height),
		Total:   changelog.TotalLines,
	}
}

func (m *runMode) viewerWindow(spec tuiviewer.Spec) (rows []string, start, end int) {
	r, s, e, clamped := tuiviewer.Window(spec, m.overlays.panel.window)
	m.overlays.panel.window = clamped
	return r, s, e
}

func (m *runMode) panelUsageLinesREF() []string {
	lines, window := usagepanel.Lines(m.usageState(), m.overlays.panel.window)
	m.overlays.panel.window = window
	return lines
}

func (m *runMode) panelUsageLines() []string {
	if m.viewport.height > 0 {
		return m.panelUsageLinesREF()
	}
	spec, _ := m.getPanelRegistry().ViewerSpec(m.overlays.panel.kind)
	rows, start, end := m.viewerWindow(spec)
	return usagepanel.StaticLines(rows, start, end, spec.Total)
}

type usageQuotaTickMsg = usagepanel.QuotaTickMsg

const usageQuotaLoadingTicks = usagepanel.QuotaLoadingTicks

func quotaStubRefills(m *runMode) {
	now := time.Now()
	m.overlays.panel.quota.loadedAt = now
	m.overlays.panel.quota.refreshG = now.Add(168 * time.Hour)
	m.overlays.panel.quota.refreshO = now.Add(48 * time.Hour)
	m.overlays.panel.quota.state = quotaLoaded
}

func (m *runMode) usageState() usagepanel.State {
	return usagepanel.State{
		Email:    m.session.email,
		Width:    m.viewport.width,
		Height:   m.viewport.height,
		Loading:  m.overlays.panel.quota.state == quotaLoading,
		Summary:  m.overlays.panel.quota.summary,
		RefreshG: m.overlays.panel.quota.refreshG,
		RefreshO: m.overlays.panel.quota.refreshO,
		Frame:    m.engine.quotaFrame,
	}
}

func (m *runMode) armRenameFocus() {
	if m.overlays.panel.rn == nil {
		return
	}
	cmd := m.overlays.panel.rn.Focus()
	if cmd != nil && m.engine.prog != nil {
		m.engine.prog.Send(cmd)
	}
}

func (m *runMode) AppendSubAgentResponse(line string) {
	m.engine.response = append(m.engine.response, line)
}

// SubAgentQuestion
func (m *runMode) SubAgentQuestionsManager() *questions.Manager {
	return m.engine.subAgentQuestions
}
func (m *runMode) ResetInput() { m.composer.input.Reset() }
func (m *runMode) newSubAgentQuestionPanel() *subagentquestionpanel.Panel {
	return panels.NewSubAgentQuestionPanel(m)
}

// Mention
func (m *runMode) InputValue() string                     { return m.composer.input.Value() }
func (m *runMode) SetInputValue(val string)               { m.composer.input.SetValue(val) }
func (m *runMode) TerminalWidth() int                     { return m.viewport.width }
func (m *runMode) TerminalHeight() int                    { return m.viewport.height }
func (m *runMode) newMentionPicker() *mentionpicker.Panel { return panels.NewMentionPicker(m) }

func (m *runMode) IsInputY(y int) bool { return m.isInputY(y) }

// SlashPicker
func (m *runMode) AltScreenActive() bool { return m.viewport.altScreen }
func (m *runMode) SlashScopeContext() slashcommands.ScopeContext {
	return m.slashScopeContext()
}
func (m *runMode) slashScopeContext() slashcommands.ScopeContext {
	eng := m.turnEngine()
	name, caps := agentturn.CapabilitiesOf(eng)
	return slashcommands.ScopeContext{
		EngineName:   name,
		Capabilities: caps,
	}
}
func (m *runMode) EngineSlashCommands() []slashcommands.Command {
	return m.engineSlashCommands()
}
func (m *runMode) engineSlashCommands() []slashcommands.Command {
	eng := m.turnEngine()
	if eng == nil {
		return nil
	}
	lister, ok := eng.(agentturn.CommandLister)
	if !ok {
		return nil
	}
	cmds := lister.AvailableCommands()
	if len(cmds) == 0 {
		return nil
	}
	out := make([]slashcommands.Command, 0, len(cmds))
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
func (m *runMode) newSlashPicker() *slashpicker.Panel {
	return panels.NewSlashPicker(m)
}

// Voice
func (m *runMode) InsertDraft(text string) {
	if m.composer.input.Value() == "" {
		m.composer.input.SetValue(text)
	} else {
		m.composer.input.InsertString(text)
	}
}
func (m *runMode) ConnectVoice() error {
	if m.viewport.voiceConnectFn != nil {
		return m.viewport.voiceConnectFn()
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
func (m *runMode) connectVoice() error {
	return m.ConnectVoice()
}
func (m *runMode) newVoiceSurface() *voice.Surface { return panels.NewVoiceSurface(m) }

// Rewind
func (m *runMode) RewindSteps() []rewind.Step {
	if m.session.supervisor != nil {
		return m.session.supervisor.RewindSteps()
	}
	return rewind.Steps(m.engine.session)
}
func (m *runMode) RewindPickStep(index int) int {
	var r rewind.Result
	if m.session.supervisor != nil {
		r = m.session.supervisor.RewindToStep(index)
		m.engine.session = r.Session
	} else {
		r = rewind.ToStep(m.engine.session, index)
		m.engine.session = r.Session
	}
	if r.Removed <= 0 {
		return 0
	}
	m.slashRespond("/rewind", rewind.StepEcho(r))
	return r.Removed
}
func (m *runMode) newRewindPanel() *rewindpanel.Panel { return panels.NewRewindPanel(m) }
func (m *runMode) rewindPanel() *rewindpanel.Panel {
	if m.overlays.panel.rw == nil {
		m.overlays.panel.rw = m.newRewindPanel()
	}
	return m.overlays.panel.rw
}

// ---- Merged from slash_panel_args.go, slash_panel_routing.go, slash_panel_wiring.go,
// slash_panel_keymap.go, slash_panels.go, and slash_regression.go ----

func (m *runMode) openSlashPanelArgs(cmd, args string) bool {
	if canonical, ok := slashcommands.Canonical(cmd); ok {
		cmd = canonical
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

func (m *runMode) currentEffort() int {
	switch word := cliflags.StringValue(m.session.flags, "effort"); word {
	case "low", "medium", "high":
		return effortpanel.EffortIndexForName(word)
	}
	return effortCursorForLabel(m.session.model)
}

func (m *runMode) currentEffortName() string {
	switch word := strings.ToLower(strings.TrimSpace(cliflags.StringValue(m.session.flags, "effort"))); word {
	case "low", "medium", "high", "xhigh", "max":
		return word
	}
	if tier := models.TierFromLabel(m.session.model); tier != "" {
		return tier
	}
	return effortpanel.EffortLevelName(m.currentEffort())
}

func (m *runMode) panelView() string {
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

func (m *runMode) panelLines() []string {
	return m.getPanelRegistry().Lines(m.overlays.panel.kind)
}

func (m *runMode) closePanel() tea.Cmd {
	cmd := m.overlays.panel.cmd
	notice := m.overlays.panel.kind.ExitNotice()
	m.overlays.panel.csp.Cancel()
	m.overlays.panel = slashPanel{}
	m.reloadVimSettings()
	if notice {
		m.engine.response = append(m.engine.response, tuilib.SlashEchoLines(cmd, "Exited "+cmd+" command")...)
	}
	return nil
}

func (m *runMode) closePanelSilent() tea.Cmd {
	m.overlays.panel.csp.Cancel()
	m.overlays.panel = slashPanel{}
	m.reloadVimSettings()
	return nil
}

func (m *runMode) handleNavAndEnterKey(key tea.Key, n int) (bool, tea.Cmd, bool) {
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

func (m *runMode) handlePanelKey(key tea.Key) (bool, tea.Cmd) {
	if m.overlays.panel.kind == panelregistry.None {
		return false, nil
	}
	if handled, cmd, claimed := m.panelRegistry().HandleKey(m.overlays.panel.kind, key); claimed {
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

func (m *runMode) panelRegistry() *panelregistry.Registry {
	reg := m.getPanelRegistry()
	if !reg.Wired() {
		m.wirePanelRegistry(reg)
	}
	return reg
}

func (m *runMode) wirePanelRegistry(reg *panelregistry.Registry) {
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
	df     *diffpanel.Panel
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
	rw     *rewindpanel.Panel
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

func (m *runMode) initOpenedSlashPanel(kind panelregistry.Kind) {
	m.initOpenedSlashPanelData(kind)
}

var panelInitializers = map[panelregistry.Kind]func(m *runMode){
	panelregistry.Provider: func(m *runMode) {
		m.overlays.panel.prov = m.newProviderPanel()
		m.overlays.panel.prov.Reload()
		m.overlays.panel.prov.Notice("")
	},
	panelregistry.Config:      func(m *runMode) { m.overlays.panel.cfg = m.newConfigPanel() },
	panelregistry.Model:       func(m *runMode) { m.overlays.panel.mdl = m.newModelPanel() },
	panelregistry.Plugin:      func(m *runMode) { m.overlays.panel.plg = m.newPluginPanel() },
	panelregistry.Skills:      func(m *runMode) { m.overlays.panel.sk = m.newSkillsPanel() },
	panelregistry.Permissions: func(m *runMode) { m.overlays.panel.perm = m.newPermissionsPanel() },
	panelregistry.Hooks:       func(m *runMode) { m.overlays.panel.hk = m.newHooksPanel() },
	panelregistry.Agents:      func(m *runMode) { m.overlays.panel.ag = m.newAgentsPanel() },
	panelregistry.Feedback:    func(m *runMode) { m.overlays.panel.fb = m.newFeedbackPanel() },
	panelregistry.Credits:     func(m *runMode) { m.overlays.panel.cr = m.newCreditsPanel() },
	panelregistry.Tasks:       func(m *runMode) { m.overlays.panel.tp = m.newTasksPanel() },
	panelregistry.Effort:      func(m *runMode) { m.overlays.panel.ef = m.newEffortPanel() },
	panelregistry.CodeSearch:  func(m *runMode) { m.overlays.panel.csp = m.newCodesearchPanel() },
	panelregistry.Rewind:      func(m *runMode) { m.overlays.panel.rw = m.newRewindPanel() },
	panelregistry.Diff:        func(m *runMode) { m.overlays.panel.df = m.newDiffPanel() },
	panelregistry.Rename: func(m *runMode) {
		m.overlays.panel.rn = m.newRenamePanel()
		m.armRenameFocus()
	},
	panelregistry.AddDir:   func(m *runMode) { m.overlays.panel.ad = m.newAddDirPanel() },
	panelregistry.Projects: func(m *runMode) { m.overlays.panel.proj = m.newProjectsPanel() },
	panelregistry.Accounts: func(m *runMode) {
		m.overlays.panel.acct = m.newAccountsPanel()
		m.overlays.panel.acct.Reload()
		m.overlays.panel.acct.ClearNotice()
		if m.overlays.panel.cmd == "/pool" {
			m.overlays.panel.acct.SetTab(accountspanel.TabPool)
		}
		m.overlays.panel.acct.ReloadPolicy()
	},
}

func (m *runMode) initOpenedSlashPanelData(kind panelregistry.Kind) {
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

func (m *runMode) openSlashPanel(cmd string) bool {
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

func (m *runMode) panelCurrentModelLabel() string {
	if lbl := strings.TrimSpace(m.session.model); lbl != "" {
		return lbl
	}
	lbl, _ := modelcatalog.ResolveEffectiveModel("")
	return lbl
}

func (m *runMode) statusEffortSuffix() string {
	if !modelpicker.HasEffortTiers(m.session.statusModel) {
		return ""
	}
	return " \u00b7 " + m.currentEffortName()
}

func (m *runMode) applyEffortSelection(level int) tea.Cmd {
	name := models.TierNameAt(m.effortTiers(), level)
	if name == "" {
		name = effortpanel.EffortLevelName(level)
	}
	return m.applyEffortLevel(name, level)
}

func (m *runMode) applyEffortLevel(name string, level int) tea.Cmd {
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
	m.engine.response = append(m.engine.response, tuilib.SlashEchoLines("/effort", "Effort set to "+name)...)
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

func (m *runMode) editPromptCmd() tea.Cmd {
	name, args, notice, err := editorkit.ResolveEditor(settings.String("editor"))
	if err != nil {
		m.engine.response = append(m.engine.response, "Failed to resolve editor: "+err.Error())
		return nil
	}
	if _, err := exec.LookPath(name); err != nil {
		m.engine.response = append(m.engine.response, "Failed to resolve editor: "+err.Error())
		return nil
	}
	if notice != "" {
		m.engine.response = append(m.engine.response, notice)
	}
	path, err := editorkit.PromptScratchFile(m.composer.input.Value())
	if err != nil {
		m.engine.response = append(m.engine.response, "Failed to create a temporary file: "+err.Error())
		return nil
	}
	c := exec.Command(name, append(args, path)...)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return editorFinishedMsg{err: err, tempPath: path}
	})
}

func (m *runMode) getPanelRegistry() *panelregistry.Registry {
	if m.overlays.panelReg == nil {
		m.overlays.panelReg = m.buildPanelRegistry()
	}
	return m.overlays.panelReg
}

func (m *runMode) Panel(k panelregistry.Kind) any {
	switch k {
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

func (m *runMode) projectsPanel() *projectspanel.Panel {
	if m.overlays.panel.proj == nil {
		m.overlays.panel.proj = m.newProjectsPanel()
	}
	return m.overlays.panel.proj
}

func (m *runMode) newProjectsPanel() *projectspanel.Panel {
	return panels.NewProjectsPanel(m)
}

func (m *runMode) SwitchProject(proj coreprojects.LocalProject) tea.Cmd {
	for _, f := range proj.Folders {
		tuilib.AddDirFeedback(f)
	}
	hostactions.HandleClear(m.hostActionSeams(), "")
	m.slashRespond("/projects", fmt.Sprintf("Switched to project %q.", proj.Name))
	return m.closePanel()
}

func (m *runMode) HomeDir() string {
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	return ""
}

func (m *runMode) WorkspaceDir() string {
	if m != nil && m.session.cwd != "" {
		return m.session.cwd
	}
	return ""
}

func (m *runMode) PanelUsageLines() []string {
	return m.panelUsageLines()
}

func (m *runMode) UsageViewerSpec() tuiviewer.Spec {
	return m.usageViewerSpec()
}

func (m *runMode) PanelChangelogLines() []string {
	return m.panelChangelogLines()
}

func (m *runMode) ChangelogViewerSpec() tuiviewer.Spec {
	return m.changelogViewerSpec()
}

func (m *runMode) buildPanelRegistry() *panelregistry.Registry {
	return panels.BuildRegistry(m)
}
