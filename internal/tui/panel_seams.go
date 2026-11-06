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
func (m *runMode) TaskRegistry() *taskregistry.Registry  { return m.taskReg }
func (m *runMode) KillSelectedTask(t *taskregistry.Task) { m.killSelectedTask(t) }
func (m *runMode) TerminalSize() (int, int)              { return m.width, m.height }
func (m *runMode) ClosePanel() tea.Cmd                   { return m.closePanel() }
func (m *runMode) newTasksPanel() *taskspanel.Panel      { return panels.NewTasksPanel(m) }
func (m *runMode) tasksPanel() *taskspanel.Panel {
	if m.panel.tp == nil {
		m.panel.tp = m.newTasksPanel()
	}
	return m.panel.tp
}

func (m *runMode) killSelectedTask(t *taskregistry.Task) {
	if t == nil {
		return
	}
	mgr := m.taskSubagentManager
	if mgr == nil {
		mgr = subagent.NewManager()
	}
	taskregistry.KillByID(m.taskReg, t.ID, mgr)
}

func (m *runMode) openTasksDetail(id string, isSubagent bool) {
	m.openSlashPanel("tasks")
	if m.panel.tp != nil {
		m.panel.tp.OpenTaskDetail(id, isSubagent)
	}
	m.activeItems.SetFocus(false)
}

func (m *runMode) tasksStatusSuffix() string {
	taskPart := ""
	reg := m.taskReg
	if m.taskCtrl != nil && m.taskCtrl.Registry() != nil {
		reg = m.taskCtrl.Registry()
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
	if m.taskCtrl != nil {
		m.taskCtrl.StartSubscription()
		return
	}
	if m.taskReg == nil || m.tasksSubscribed {
		return
	}
	ch, cancel := m.taskReg.Subscribe()
	m.tasksSubCancel = cancel
	m.tasksSubscribed = true
	go func() {
		for ev := range ch {
			_ = ev
			atomic.StoreInt32(&m.tasksDirty, 1)
		}
	}()
}

func (m *runMode) consumeTasksDirty() bool {
	if m.taskCtrl != nil {
		return m.taskCtrl.ConsumeDirty()
	}
	return atomic.SwapInt32(&m.tasksDirty, 0) == 1
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
	if m.panel.cfg == nil {
		m.panel.cfg = m.newConfigPanel()
	}
	return m.panel.cfg
}

func (m *runMode) applyThemeLive(scheme string) tea.Cmd {
	palette.SetScheme(scheme)
	mdrender.SetColorScheme(scheme)
	promptColor, placeholderColor := themepreview.ColorsFor(scheme)
	if m != nil {
		m.colorScheme = scheme
		st := m.input.Styles()
		promptStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(promptColor))
		placeholderStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(placeholderColor))
		st.Focused.Prompt = promptStyle
		st.Focused.Placeholder = placeholderStyle
		st.Blurred.Prompt = promptStyle
		st.Blurred.Placeholder = placeholderStyle
		m.input.SetStyles(st)
		m.rerenderTranscriptForTheme()
	}
	return tea.ClearScreen
}

func (m *runMode) applyAltScreenLive(stored any) {
	m.altScreen = altScreenModeValue(stored)
}

func (m *runMode) applyVerbosityLive(verbosity string) {
	if m == nil {
		return
	}
	m.verbosity = verbosity
	m.expandedUserToggled = false
	m.rerenderBlocks()
	m.rerenderLiveSteps()
}

func (m *runMode) configOverrideFor(row configpanel.Row) (string, bool) {
	if m == nil || m.flags == nil {
		return "", false
	}
	switch row.SettingsKey {
	case "enableTerminalSandbox":
		if cliflags.BoolValue(m.flags, "sandbox") {
			return "(overridden by --sandbox)", true
		}
	case "toolPermission":
		if cliflags.BoolValue(m.flags, "dangerously-skip-permissions") {
			return "(overridden by --dangerously-skip-permissions)", true
		}
	case "agentMode":
		if cliflags.StringValue(m.flags, "mode") != "" {
			return "(overridden by --mode)", true
		}
	case "acp":
		if cliflags.StringValue(m.flags, "acp") != "" {
			return "(overridden by --acp)", true
		}
	}
	return "", false
}

// Permissions
func (m *runMode) newPermissionsPanel() *permissionspanel.Panel { return panels.NewPermissionsPanel(m) }
func (m *runMode) permissionsPanel() *permissionspanel.Panel {
	if m.panel.perm == nil {
		m.panel.perm = m.newPermissionsPanel()
	}
	return m.panel.perm
}

// Hooks
func (m *runMode) newHooksPanel() *hookspanel.Panel { return panels.NewHooksPanel(m) }
func (m *runMode) hooksPanel() *hookspanel.Panel {
	if m.panel.hk == nil {
		m.panel.hk = m.newHooksPanel()
	}
	return m.panel.hk
}

// Plugins
var pluginWatcherPtr = &plugins.WatcherPtr

func (m *runMode) newPluginPanel() *pluginsui.Panel { return panels.NewPluginPanel(m) }
func (m *runMode) pluginPanel() *pluginsui.Panel {
	if m.panel.plg == nil {
		m.panel.plg = m.newPluginPanel()
	}
	return m.panel.plg
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
	if m.panel.cr == nil {
		m.panel.cr = m.newCreditsPanel()
	}
	return m.panel.cr
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
	if m.panel.ad == nil {
		m.panel.ad = m.newAddDirPanel()
	}
	return m.panel.ad
}

// Feedback
func (m *runMode) SubmitFeedback() {
	m.submitFeedback()
}

func (m *runMode) submitFeedback() {
	m.panel = slashPanel{}
	m.slashRespond("/feedback", "Feedback recorded (openagy has no upload endpoint; saved locally only)")
}
func (m *runMode) newFeedbackPanel() *feedbackpanel.Panel { return panels.NewFeedbackPanel(m) }
func (m *runMode) feedbackPanel() *feedbackpanel.Panel {
	if m.panel.fb == nil {
		m.panel.fb = m.newFeedbackPanel()
	}
	return m.panel.fb
}

// Skills
func (m *runMode) SkillsCatalog() []skillsui.SkillInfo { return slashSkillCatalog() }
func (m *runMode) TerminalOSC8() bool                  { return m.hyperlinkCaps.OSC8 }
func (m *runMode) newSkillsPanel() *skillsui.Panel     { return panels.NewSkillsPanel(m) }
func (m *runMode) skillsPanel() *skillsui.Panel {
	if m.panel.sk == nil {
		m.panel.sk = m.newSkillsPanel()
	}
	return m.panel.sk
}

// Provider
func (m *runMode) OpenBrowser(url string) error           { return cli.OpenBrowser(url) }
func (m *runMode) ClipboardRead() string                  { return clipboardReadText() }
func (m *runMode) ClipboardWrite(text string) error       { return clipboardWrite(text) }
func (m *runMode) CurrentModelLabel() string              { return m.panelCurrentModelLabel() }
func (m *runMode) newProviderPanel() *providerpanel.Panel { return panels.NewProviderPanel(m) }
func (m *runMode) providerPanel() *providerpanel.Panel {
	if m.panel.prov == nil {
		m.panel.prov = m.newProviderPanel()
		m.panel.prov.Reload()
	}
	return m.panel.prov
}

// Rename
func (m *runMode) ConversationName() string { return m.conversationName }
func (m *runMode) SubmitRename(name string) tea.Cmd {
	next, msg := tuilib.RenameFeedback(name, m.conversationName)
	if msg != "" {
		m.slashRespond("/rename", msg)
	}
	if next != "" {
		m.conversationName = next
	}
	return m.closePanelSilent()
}
func (m *runMode) newRenamePanel() *renamepanel.Panel { return panels.NewRenamePanel(m) }
func (m *runMode) renamePanel() *renamepanel.Panel {
	if m.panel.rn == nil {
		m.panel.rn = m.newRenamePanel()
	}
	return m.panel.rn
}

func (m *runMode) SessionTierFor(email string) (string, bool) {
	if m == nil || !m.tierFetched {
		return "", false
	}
	if !strings.EqualFold(m.tierAccount, email) {
		return "", false
	}
	return m.tier, true
}

func (m *runMode) startAccountLogin() tea.Cmd {
	lm := loginui.New()
	lm.Width = m.width
	lm.Height = m.height
	m.login = lm
	return lm.Init()
}

func (m *runMode) ApplyActiveAccount(email string) {
	m.email = email
	m.forgetSessionTier()
	if m.backendFor != nil {
		if b := m.backendFor(m.model); b != nil {
			m.backend = b
		}
	}
}

func (m *runMode) SignOutActiveAccount() {
	m.email = ""
}

func (m *runMode) forgetSessionTier() {
	m.tier = ""
	m.tierAccount = ""
	m.tierFetched = false
}

// plannerResponses returns the assistant ("planner") responses in order, which
// is what /copy indexes. The reference's command description calls them
// "planner response[s]".
func (m *runMode) plannerResponses() []string {
	if m.supervisor != nil && len(m.supervisor.Messages()) > 0 {
		return slashactions.PlannerResponses(m.supervisor.Messages())
	}
	return slashactions.PlannerResponses(m.session)
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
	sess := m.session
	if m.supervisor != nil && len(m.supervisor.Messages()) > 0 {
		sess = m.supervisor.Messages()
	}
	return slashactions.ConversationTitle(m.conversationName, sess, fallback)
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
		m.response = append(m.response,
			panelGray166+msg.err.Error()+panelGray166C,
			"",
		)
		return
	}
	answer := strings.TrimRight(strings.TrimSpace(msg.answer), "\n")
	if answer == "" {
		m.response = append(m.response,
			panelGray238+"(the browser agent returned no answer)"+panelGray238C,
			"",
		)
		return
	}
	for _, line := range strings.Split(answer, "\n") {
		m.response = append(m.response, line)
	}
	m.response = append(m.response, "")
}

func (m *runMode) newBrowserAgent() (browser.BrowserSubagentProvider, func(), error) {
	if m.catalog == nil {
		return nil, nil, errors.New("the browser tools are not available in this session")
	}
	if m.agentBody != "" {
		return nil, nil, errors.New("this session runs an agent body, which has no browser agent")
	}
	if m.backend == nil {
		return nil, nil, errors.New("no model is selected")
	}
	if err := browserProbe(context.Background()); err != nil {
		return nil, nil, err
	}
	workspace := m.cwd
	if workspace == "" {
		workspace = defaultWorkspaceForRunMode()
	}
	agentDir := confhome.Antigravity()
	provider := agentturn.NewBackendProviderAdapter(m.backend, m.model)
	runner := browsersubagent.New(subagenttools.Deps{
		Provider: provider,
		Model:    m.model,
		CWD:      workspace,
		AgentDir: agentDir,
	}, browsersubagent.Tools(m.catalog))
	return runner, func() {}, nil
}

const rcURLIndent = "     "
const rcURLDefaultWidth = 80

func (m *runMode) rcURLChunkWidth() int {
	width := m.width
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
	if !m.narrationStart.IsZero() {
		return m.narrationStart
	}
	return time.Time{}
}

func narrationFeedback(m *runMode, args string) (text, echo string, mutedNow, already bool) {
	text, echo, mutedNow, already = narration.Feedback(args, narrationSessionStart(m), m.narrationOff)
	if mutedNow {
		m.narrationOff = true
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
			m.response = append(m.response, panelGray238+ln+panelGray238C)
		}
		m.response = append(m.response, "")
	}
	return true, nil
}

func (m *runMode) sessionTokenTotals() (prompt, completion int) {
	if m == nil {
		return 0, 0
	}
	return int(atomic.LoadInt64(&m.store.SessionPromptTokens)),
		int(atomic.LoadInt64(&m.store.SessionCompletionTokens))
}

func (m *runMode) recordTurnUsage(meta *backend.ChunkMeta) {
	if m == nil || meta == nil {
		return
	}
	atomic.AddInt64(&m.store.SessionPromptTokens, int64(meta.PromptTokens))
	atomic.AddInt64(&m.store.SessionCompletionTokens, int64(meta.CompletionTokens))
	atomic.StoreInt64(&m.store.LastTurnPromptTokens, int64(meta.PromptTokens))
	atomic.StoreInt64(&m.store.LastTurnCompletionTokens, int64(meta.CompletionTokens))
}

func (m *runMode) externalStateFor() *statusline.State {
	if m == nil {
		return nil
	}
	return m.statusLineExternalState
}

func (m *runMode) ensureExternalState() *statusline.State {
	if m == nil {
		return nil
	}
	if m.statusLineExternalState == nil {
		m.statusLineExternalState = &statusline.State{}
	}
	return m.statusLineExternalState
}

func (m *runMode) runExternalStatusLineTick() {
	if m == nil {
		return
	}
	statusCfg := m.statusLineCfg
	titleCfg := m.titleCfg
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
	if m == nil || m.store.StatusLineTickArmed {
		return nil
	}
	if !m.statusLineScriptActive() && !m.titleCfg.Active() {
		return nil
	}
	m.store.StatusLineTickArmed = true
	return armExternalStatusLineTick()
}

func (m *runMode) externalRenderOK() bool {
	if m == nil {
		return false
	}
	if m.statusLine == "off" {
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
	model := m.statusModel
	if model == "" {
		model = m.model
	}
	contextWindow := 0
	if model != "" {
		contextWindow = modelctx.ContextWindowFor(model)
	}
	taskCount := 0
	if m.taskReg != nil {
		taskCount = m.taskReg.CountRunning()
	}
	prompt, completion := m.sessionTokenTotals()
	return statusline.PayloadFrom(statusline.PayloadState{
		Cwd:                      m.cwd,
		ConversationID:           m.conversationID,
		ConversationTitle:        m.conversationTitle(),
		Model:                    m.model,
		Effort:                   m.currentEffortName(),
		AgentName:                m.agentName,
		CycleMode:                m.mode,
		PlanTier:                 m.tier,
		Email:                    m.email,
		TerminalWidth:            m.width,
		Streaming:                m.streaming,
		ToolInFlight:             m.toolInFlight(),
		TurnFailed:               m.turnFailed(),
		SandboxEnabled:           m.toolSandbox.Enabled,
		Quota:                    statusline.QuotaBuckets(m.cachedQuota(m.email), time.Now()),
		TaskCount:                taskCount,
		PendingInputCount:        m.pendingInputCount(),
		Subagents:                m.subagentInfos(),
		VimMode:                  m.vimModeName(),
		SessionPromptTokens:      prompt,
		SessionCompletionTokens:  completion,
		LastTurnPromptTokens:     int(atomic.LoadInt64(&m.store.LastTurnPromptTokens)),
		LastTurnCompletionTokens: int(atomic.LoadInt64(&m.store.LastTurnCompletionTokens)),
		ContextWindowSize:        contextWindow,
		PricingModel:             model,
	})
}

func (m *runMode) agentState() string {
	state := statusline.AgentState(m.streaming, m.toolInFlight(), m.turnFailed())
	if state == statusline.StateIdle {
		return ""
	}
	return state
}

func (m *runMode) toolInFlight() bool {
	return m.activeTool != ""
}

func (m *runMode) turnFailed() bool {
	return m.lastTurnOutcome == tea.ProgramStateError
}

func (m *runMode) pendingInputCount() int {
	n := len(m.store.StreamQueue)
	if m.store.QueuedPrompt != "" {
		n++
	}
	return n
}

func (m *runMode) subagentInfos() []statusline.SubagentInfo {
	if m.subagentRegistry == nil {
		return nil
	}
	return statusline.Subagents(m.subagentRegistry.States())
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
	model := m.statusModel
	if model == "" {
		model = m.model
	}
	if model == "" {
		return 0
	}
	// PromptTokens in the latest turn represents the active context window size.
	// Accumulating session prompt tokens across all turns calculates total billing usage,
	// which would overflow the single-turn context window and result in 3000%+ figures.
	prompt := int(atomic.LoadInt64(&m.store.LastTurnPromptTokens))
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
		m.statusLineCfg = cfg
		m.statusLineCommand = cfg.Command
		m.statusLineExternal = cfg.Active()
	} else {
		m.statusLineCfg = statusline.Config{}
		m.statusLineCommand = ""
		m.statusLineExternal = false
	}
	if cfg, ok := statusline.LoadTitleConfig(); ok {
		m.titleCfg = cfg
		m.titleCommand = cfg.Command
		if cfg.Active() {
			m.title = "on"
		}
	} else {
		m.titleCfg = statusline.Config{}
		m.titleCommand = ""
	}
}

func (m *runMode) statusLineScriptActive() bool {
	if m.statusLineCfg.Active() {
		return true
	}
	return m.statusLineExternal
}

func (m *runMode) statusAreaState() statusline.AreaState {
	return statusline.AreaState{
		Builtin: statusrow.Render(m.statusRowState()),
		Active:  m.statusLineScriptActive(),
		Config:  m.statusLineCfg,
		State:   m.externalStateFor(),
		LogPath: confhome.CLILogPath(logts.FormatLogTS()),
	}
}

func (m *runMode) windowTitleText() string {
	if m.titleCfg.Active() {
		if title := m.titleStoreFor().Get(); title != "" {
			return title
		}
	}
	if m.title == "on" {
		return m.conversationTitle()
	}
	return ""
}

// DiffTurnCount
func (m *runMode) diffTurnCount() int {
	return m.DiffTurnCount()
}
func (m *runMode) SessionCwd() string             { return m.cwd }
func (m *runMode) DiffTurnCount() int             { return m.diffTurnCount() }
func (m *runMode) newDiffPanel() *diffpanel.Panel { return panels.NewDiffPanel(m) }
func (m *runMode) diffPanel() *diffpanel.Panel {
	if m.panel.df == nil {
		m.panel.df = m.newDiffPanel()
	}
	return m.panel.df
}

func (m *runMode) effortTiers() []string {
	return catalog.EffortTiersFor(m.effortModel())
}

func (m *runMode) effortModel() string {
	if label := strings.TrimSpace(m.model); label != "" && catalog.ModelAvailable(label) {
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
	if m.panel.ef == nil {
		m.panel.ef = m.newEffortPanel()
	}
	return m.panel.ef
}

// Help
func (m *runMode) ModelName() string             { return m.model }
func (m *runMode) TierName() string              { return m.tier }
func (m *runMode) UserEmail() string             { return m.email }
func (m *runMode) ConversationID() string        { return m.conversationID }
func (m *runMode) AppendResponse(lines []string) { m.response = append(m.response, lines...) }
func (m *runMode) SelectCommand(cmd string) {
	m.input.SetValue(cmd)
	m.input.CursorEnd()
	m.picker.Reset()
	m.promptHistory.Reset()
	m.store.ResetInputSelection()
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
	if m.panel.acct == nil {
		m.panel.acct = m.newAccountsPanel()
	}
	return m.panel.acct
}

// Agents
func (m *runMode) ActiveAgentName() string                { return m.agentName }
func (m *runMode) RunningSubagents() []*subagent.Subagent { return m.runningSubagents() }
func (m *runMode) HasSessionTurns() bool                  { return len(m.session) > 0 }
func (m *runMode) ApplyAgentSwitch(name, body string) {
	m.applyAgentSwitch(name, body)
	m.panel = slashPanel{}
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
	if m.panel.ag == nil {
		m.panel.ag = m.newAgentsPanel()
	}
	return m.panel.ag
}
func (m *runMode) runningSubagents() []*subagent.Subagent {
	mgr := m.taskSubagentManager
	if mgr == nil {
		return nil
	}
	return mgr.List()
}
func (m *runMode) killAgentsSubagent(sa *subagent.Subagent) bool {
	mgr := m.taskSubagentManager
	if mgr == nil || sa == nil {
		return false
	}
	return mgr.KillRunning(sa.ID)
}
func (m *runMode) pendingSubagentAuthorization(subagentID string) (lifecycle.PendingApproval, bool) {
	mgr := m.taskSubagentManager
	if mgr == nil {
		return lifecycle.PendingApproval{}, false
	}
	return mgr.PendingAuthorization(subagentID)
}
func (m *runMode) answerSubagentAuthorization(approvalID string, approve bool) bool {
	mgr := m.taskSubagentManager
	if mgr == nil {
		return false
	}
	return mgr.AnswerAuthorization(approvalID, approve)
}
func (m *runMode) panelAgentsWorkspace() string {
	workspace := m.cwd
	if workspace == "" {
		workspace, _ = os.Getwd()
	}
	return workspace
}

// Codesearch
func (m *runMode) SearchRoot() string    { return m.cwd }
func (m *runMode) SearchStreaming() bool { return m.streamingSearch }
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
	if m.panel.csp == nil {
		m.panel.csp = m.newCodesearchPanel()
	}
	return m.panel.csp
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
	return filepath.Join(m.cwd, path)
}
func (m *runMode) codesearchOpenEditor(path string, line int) tea.Cmd {
	name, args, notice, err := editorkit.ResolveEditor(settings.String("editor"))
	if err != nil {
		m.response = append(m.response, "Failed to resolve editor: "+err.Error())
		return nil
	}
	if _, err := exec.LookPath(name); err != nil {
		m.response = append(m.response, "Failed to resolve editor: "+err.Error())
		return nil
	}
	if notice != "" {
		m.response = append(m.response, notice)
	}
	full := m.codesearchPath(path)
	c := exec.Command(name, append(args, editorLineArgs(name, full, line)...)...)
	c.Dir = m.cwd
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
	m.model = label
	m.statusModel = tuilib.StripTier(label)
	m.syncBackendForModel()
}
func (m *runMode) CustomModelRows() []modelpicker.Row { return m.engineModelRows() }
func (m *runMode) engineModelRows() []modelpicker.Row {
	if len(m.engineModelRowsCache) > 0 {
		return m.engineModelRowsCache
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
	m.engineModelRowsCache = rows
	return rows
}

func (m *runMode) syncBackendForModel() {
	if m.backendFor == nil || strings.TrimSpace(m.model) == "" {
		return
	}
	if b := m.backendFor(m.model); b != nil {
		m.backend = b
	}
	m.wireSettingsPending = true
}
func (m *runMode) newModelPanel() *modelpicker.Panel { return panels.NewModelPanel(m) }
func (m *runMode) modelPanel() *modelpicker.Panel {
	if m.panel.mdl == nil {
		m.panel.mdl = m.newModelPanel()
	}
	return m.panel.mdl
}

// Viewer
func (m *runMode) ViewerSpec() (tuiviewer.Spec, bool) {
	return m.getPanelRegistry().ViewerSpec(m.panel.kind)
}
func (m *runMode) ViewerPageRows(spec tuiviewer.Spec) int {
	if m.panel.kind == panelregistry.Changelog {
		return changelog.PageRows(spec, m.height)
	}
	return spec.Visible
}
func (m *runMode) ViewerWidth() int                   { return m.viewerVPWidth() }
func (m *runMode) newViewerPanel() *viewerpanel.Panel { return panels.NewViewerPanel(m) }
func (m *runMode) viewerPanel() *viewerpanel.Panel {
	if m.panel.vw == nil {
		p := m.newViewerPanel()
		p.Open()
		m.panel.vw = p
	}
	return m.panel.vw
}

func (m *runMode) initViewerViewport() {
	if m.panel.vw == nil {
		m.panel.vw = m.newViewerPanel()
	}
	m.panel.vw.Open()
}

func (m *runMode) viewerVPWidth() int {
	if m.width > 0 {
		return m.width
	}
	return 80
}

func (m *runMode) handleViewerPanelKey(key tea.Key) (bool, tea.Cmd) {
	handled, cmd, offset := m.viewerPanel().HandleKey(key, m.panel.window)
	m.panel.window = offset
	return handled, cmd
}

func (m *runMode) listResumeConversations() ([]convstore.ConversationMeta, error) {
	return convstore.ListConversations()
}

func (m *runMode) renameResumeConversation(meta convstore.ConversationMeta) {
	m.conversationName = meta.Title
	m.openRenamePanel()
}

func (m *runMode) openRenamePanel() {
	m.panel.kind = panelregistry.Rename
	m.panel.cmd = "/rename"
	m.panel.rn = m.newRenamePanel()
	m.armRenameFocus()
}

func (m *runMode) ContextState() contextpanel.State {
	return m.contextState()
}

func (m *runMode) contextState() contextpanel.State {
	st := contextpanel.State{
		Model: m.contextModel(),
		Width: m.width,
	}
	switch {
	case m.streaming:
		st.Mode = contextpanel.ModeStreaming
		st.User, st.Agent, st.Tool = m.streamingTokenCounts()
	case len(m.session) == 0:
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
		st.SystemMessages = contextpanel.SystemMessageTokens(m.session)
	}
	st.CheckpointBuffer = contextpanel.CheckpointBufferTokens(m.session)
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
	if m.model != "" {
		return m.model
	}
	return defaultModel
}

func (m *runMode) streamingTokenCounts() (user, agent, tool int) {
	user, _, tool = m.sessionTokenCounts()
	user = contextpanel.AdjustStreamingUserTokens(m.session, user)
	completed := atomic.LoadInt64(&m.store.SessionCompletionTokens)
	lastTurn := atomic.LoadInt64(&m.store.LastTurnCompletionTokens)
	agent = contextpanel.StreamingAgentTokens(completed, lastTurn, len(m.liveReplyBuf))
	return user, agent, tool
}

func (m *runMode) wireToolDecls() []backend.ToolDecl {
	return append([]backend.ToolDecl(nil), m.tools...)
}

func (m *runMode) sessionTokenCounts() (user, agent, tool int) {
	user = contextpanel.UserMessageTokens(m.session)
	completed := atomic.LoadInt64(&m.store.SessionCompletionTokens)
	lastTurn := atomic.LoadInt64(&m.store.LastTurnCompletionTokens)
	agent = contextpanel.AgentTokens(completed, lastTurn)
	var steps []toolsteps.Step
	for _, b := range m.blocks {
		steps = append(steps, b.Steps...)
	}
	steps = append(steps, m.liveSteps...)
	tool = contextpanel.ToolResultTokens(toolsteps.ToolResults(steps)...)
	return user, agent, tool
}

func (m *runMode) statusRowState() statusrow.State {
	panelOpen := m.panel.kind != panelregistry.None
	panelKeepsHint := false
	if panelOpen {
		panelKeepsHint = m.panel.kind.KeepStatusHint()
	}

	return statusrow.State{
		Width:          m.width,
		CanonicalWidth: separatorWidth,

		PickerVisible:      m.picker.Visible(),
		PickerModelMode:    m.picker.IsModelMode(),
		Streaming:          m.streaming,
		Queued:             len(m.store.StreamQueue) + len(m.store.BashQueue),
		BufferEmpty:        strings.TrimSpace(m.input.Value()) == "",
		ResumePickerActive: m.resumePicker.Active(),
		BashMode:           m.store.BashMode,
		HelpActive:         m.helpPanel.Active(),
		PanelOpen:          panelOpen,
		PanelKeepsHint:     panelKeepsHint,
		ExitConfirm:        m.exitConfirm,
		VimBadge:           vimkeys.Badge(m.vimEnabled(), m.vimMode()),
		Compact:            m.compactState(),

		Model:        m.model,
		StatusModel:  m.statusModel,
		AgentName:    m.agentName,
		Mode:         m.mode,
		EffortSuffix: m.statusEffortSuffix(),
		TaskSuffix:   m.tasksStatusSuffix(),
		QueueMode:    m.store.QueueMode,
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
	if m.store.CompactStart.IsZero() {
		return nil
	}
	return &statusrow.Compact{
		Start:        m.store.CompactStart,
		End:          m.store.CompactEnd,
		TokensBefore: m.store.CompactTokensBefore,
		TokensAfter:  m.store.CompactTokensAfter,
		SpinnerFrame: m.store.CompactSpinnerFrame,
	}
}

func (m *runMode) panelChangelogLines() []string {
	spec, _ := m.getPanelRegistry().ViewerSpec(m.panel.kind)
	rows, start, end := m.viewerWindow(spec)
	return changelog.PanelLines(rows, start, end, spec.Total)
}

func (m *runMode) usageViewerSpec() tuiviewer.Spec {
	if m.height > 0 {
		return usagepanel.Spec(m.usageState())
	}
	return usagepanel.StaticSpec()
}

func (m *runMode) changelogViewerSpec() tuiviewer.Spec {
	return tuiviewer.Spec{
		Lines:   changelog.Document(m.width, m.height),
		Visible: changelog.VisibleRows(m.height),
		Total:   changelog.TotalLines,
	}
}

func (m *runMode) viewerWindow(spec tuiviewer.Spec) (rows []string, start, end int) {
	r, s, e, clamped := tuiviewer.Window(spec, m.panel.window)
	m.panel.window = clamped
	return r, s, e
}

func (m *runMode) panelUsageLinesREF() []string {
	lines, window := usagepanel.Lines(m.usageState(), m.panel.window)
	m.panel.window = window
	return lines
}

func (m *runMode) panelUsageLines() []string {
	if m.height > 0 {
		return m.panelUsageLinesREF()
	}
	spec, _ := m.getPanelRegistry().ViewerSpec(m.panel.kind)
	rows, start, end := m.viewerWindow(spec)
	return usagepanel.StaticLines(rows, start, end, spec.Total)
}

type usageQuotaTickMsg = usagepanel.QuotaTickMsg

const usageQuotaLoadingTicks = usagepanel.QuotaLoadingTicks

func quotaStubRefills(m *runMode) {
	now := time.Now()
	m.panel.quota.loadedAt = now
	m.panel.quota.refreshG = now.Add(168 * time.Hour)
	m.panel.quota.refreshO = now.Add(48 * time.Hour)
	m.panel.quota.state = quotaLoaded
}

func (m *runMode) usageState() usagepanel.State {
	return usagepanel.State{
		Email:    m.email,
		Width:    m.width,
		Height:   m.height,
		Loading:  m.panel.quota.state == quotaLoading,
		Summary:  m.panel.quota.summary,
		RefreshG: m.panel.quota.refreshG,
		RefreshO: m.panel.quota.refreshO,
		Frame:    m.quotaFrame,
	}
}

func (m *runMode) armRenameFocus() {
	if m.panel.rn == nil {
		return
	}
	cmd := m.panel.rn.Focus()
	if cmd != nil && m.prog != nil {
		m.prog.Send(cmd)
	}
}

func (m *runMode) AppendSubAgentResponse(line string) {
	m.response = append(m.response, line)
}

// SubAgentQuestion
func (m *runMode) SubAgentQuestionsManager() *questions.Manager {
	return m.subAgentQuestions
}
func (m *runMode) ResetInput() { m.input.Reset() }
func (m *runMode) newSubAgentQuestionPanel() *subagentquestionpanel.Panel {
	return panels.NewSubAgentQuestionPanel(m)
}

// Mention
func (m *runMode) InputValue() string                     { return m.input.Value() }
func (m *runMode) SetInputValue(val string)               { m.input.SetValue(val) }
func (m *runMode) TerminalWidth() int                     { return m.width }
func (m *runMode) TerminalHeight() int                    { return m.height }
func (m *runMode) newMentionPicker() *mentionpicker.Panel { return panels.NewMentionPicker(m) }

func (m *runMode) IsInputY(y int) bool { return m.isInputY(y) }

// SlashPicker
func (m *runMode) AltScreenActive() bool { return m.altScreen }
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
	if m.input.Value() == "" {
		m.input.SetValue(text)
	} else {
		m.input.InsertString(text)
	}
}
func (m *runMode) ConnectVoice() error {
	if m.voiceConnectFn != nil {
		return m.voiceConnectFn()
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
	if m.supervisor != nil {
		return m.supervisor.RewindSteps()
	}
	return rewind.Steps(m.session)
}
func (m *runMode) RewindPickStep(index int) int {
	var r rewind.Result
	if m.supervisor != nil {
		r = m.supervisor.RewindToStep(index)
		m.session = r.Session
	} else {
		r = rewind.ToStep(m.session, index)
		m.session = r.Session
	}
	if r.Removed <= 0 {
		return 0
	}
	m.slashRespond("/rewind", rewind.StepEcho(r))
	return r.Removed
}
func (m *runMode) newRewindPanel() *rewindpanel.Panel { return panels.NewRewindPanel(m) }
func (m *runMode) rewindPanel() *rewindpanel.Panel {
	if m.panel.rw == nil {
		m.panel.rw = m.newRewindPanel()
	}
	return m.panel.rw
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
	if m.panel.kind == panelregistry.CodeSearch {
		m.codesearchPanel().Start(args)
	}
	return true
}

func (m *runMode) currentEffort() int {
	switch word := cliflags.StringValue(m.flags, "effort"); word {
	case "low", "medium", "high":
		return effortpanel.EffortIndexForName(word)
	}
	return effortCursorForLabel(m.model)
}

func (m *runMode) currentEffortName() string {
	switch word := strings.ToLower(strings.TrimSpace(cliflags.StringValue(m.flags, "effort"))); word {
	case "low", "medium", "high", "xhigh", "max":
		return word
	}
	if tier := models.TierFromLabel(m.model); tier != "" {
		return tier
	}
	return effortpanel.EffortLevelName(m.currentEffort())
}

func (m *runMode) panelView() string {
	if m.panel.kind == panelregistry.None {
		return ""
	}
	var b strings.Builder
	for _, l := range m.panelLines() {
		b.WriteString(l)
		b.WriteString("\n")
	}
	if m.panel.kind.TrailingBlank() {
		b.WriteString("\n")
	}
	return b.String()
}

func (m *runMode) panelLines() []string {
	return m.getPanelRegistry().Lines(m.panel.kind)
}

func (m *runMode) closePanel() tea.Cmd {
	cmd := m.panel.cmd
	notice := m.panel.kind.ExitNotice()
	m.panel.csp.Cancel()
	m.panel = slashPanel{}
	m.reloadVimSettings()
	if notice {
		m.response = append(m.response, tuilib.SlashEchoLines(cmd, "Exited "+cmd+" command")...)
	}
	return nil
}

func (m *runMode) closePanelSilent() tea.Cmd {
	m.panel.csp.Cancel()
	m.panel = slashPanel{}
	m.reloadVimSettings()
	return nil
}

func (m *runMode) handleNavAndEnterKey(key tea.Key, n int) (bool, tea.Cmd, bool) {
	if m.panel.kind == panelregistry.Config {
		if handled, cmd := m.configPanel().HandleKey(key); handled {
			return true, cmd, true
		}
	}
	switch action := tuilib.ClassifyListNavKey(key); action {
	case tuilib.NavUp, tuilib.NavDown, tuilib.NavPageUp, tuilib.NavPageDown,
		tuilib.NavTop, tuilib.NavBottom:
		m.panel.cursor = tuiviewport.PanelCursor(m.panel.cursor, action, n)
		return true, nil, true
	case tuilib.NavCancel:
		return true, m.closePanel(), true
	}
	return false, nil, false
}

func (m *runMode) handlePanelKey(key tea.Key) (bool, tea.Cmd) {
	if m.panel.kind == panelregistry.None {
		return false, nil
	}
	if handled, cmd, claimed := m.panelRegistry().HandleKey(m.panel.kind, key); claimed {
		return handled, cmd
	}
	n := m.panelRegistry().RowCount(m.panel.kind)
	if m.panel.kind == panelregistry.Model {
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
		m.panel.prov = m.newProviderPanel()
		m.panel.prov.Reload()
		m.panel.prov.Notice("")
	},
	panelregistry.Config:      func(m *runMode) { m.panel.cfg = m.newConfigPanel() },
	panelregistry.Model:       func(m *runMode) { m.panel.mdl = m.newModelPanel() },
	panelregistry.Plugin:      func(m *runMode) { m.panel.plg = m.newPluginPanel() },
	panelregistry.Skills:      func(m *runMode) { m.panel.sk = m.newSkillsPanel() },
	panelregistry.Permissions: func(m *runMode) { m.panel.perm = m.newPermissionsPanel() },
	panelregistry.Hooks:       func(m *runMode) { m.panel.hk = m.newHooksPanel() },
	panelregistry.Agents:      func(m *runMode) { m.panel.ag = m.newAgentsPanel() },
	panelregistry.Feedback:    func(m *runMode) { m.panel.fb = m.newFeedbackPanel() },
	panelregistry.Credits:     func(m *runMode) { m.panel.cr = m.newCreditsPanel() },
	panelregistry.Tasks:       func(m *runMode) { m.panel.tp = m.newTasksPanel() },
	panelregistry.Effort:      func(m *runMode) { m.panel.ef = m.newEffortPanel() },
	panelregistry.CodeSearch:  func(m *runMode) { m.panel.csp = m.newCodesearchPanel() },
	panelregistry.Rewind:      func(m *runMode) { m.panel.rw = m.newRewindPanel() },
	panelregistry.Diff:        func(m *runMode) { m.panel.df = m.newDiffPanel() },
	panelregistry.Rename: func(m *runMode) {
		m.panel.rn = m.newRenamePanel()
		m.armRenameFocus()
	},
	panelregistry.AddDir:   func(m *runMode) { m.panel.ad = m.newAddDirPanel() },
	panelregistry.Projects: func(m *runMode) { m.panel.proj = m.newProjectsPanel() },
	panelregistry.Accounts: func(m *runMode) {
		m.panel.acct = m.newAccountsPanel()
		m.panel.acct.Reload()
		m.panel.acct.ClearNotice()
		if m.panel.cmd == "/pool" {
			m.panel.acct.SetTab(accountspanel.TabPool)
		}
		m.panel.acct.ReloadPolicy()
	},
}

func (m *runMode) initOpenedSlashPanelData(kind panelregistry.Kind) {
	if kind == panelregistry.Usage && m.height > 0 {
		m.panel.quota = quotaState{state: quotaLoading}
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
	m.panel = slashPanel{kind: kind, cmd: "/" + canonical}
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
	if lbl := strings.TrimSpace(m.model); lbl != "" {
		return lbl
	}
	lbl, _ := modelcatalog.ResolveEffectiveModel("")
	return lbl
}

func (m *runMode) statusEffortSuffix() string {
	if !modelpicker.HasEffortTiers(m.statusModel) {
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

	if m.flags != nil {
		_ = m.flags.Set("effort", name)
	}

	if m.panel.kind == panelregistry.Effort {
		m.panel = slashPanel{}
	}
	if !changed {
		return nil
	}
	if strings.TrimSpace(m.model) == "" {
		m.model = before
		m.statusModel = tuilib.StripTier(before)
	}
	m.model = label
	m.statusModel = tuilib.StripTier(label)
	m.syncBackendForModel()
	_ = settings.SetField("model", label)
	m.response = append(m.response, tuilib.SlashEchoLines("/effort", "Effort set to "+name)...)
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
		m.response = append(m.response, "Failed to resolve editor: "+err.Error())
		return nil
	}
	if _, err := exec.LookPath(name); err != nil {
		m.response = append(m.response, "Failed to resolve editor: "+err.Error())
		return nil
	}
	if notice != "" {
		m.response = append(m.response, notice)
	}
	path, err := editorkit.PromptScratchFile(m.input.Value())
	if err != nil {
		m.response = append(m.response, "Failed to create a temporary file: "+err.Error())
		return nil
	}
	c := exec.Command(name, append(args, path)...)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return editorFinishedMsg{err: err, tempPath: path}
	})
}

func (m *runMode) getPanelRegistry() *panelregistry.Registry {
	if m.panelReg == nil {
		m.panelReg = m.buildPanelRegistry()
	}
	return m.panelReg
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
	if m.panel.proj == nil {
		m.panel.proj = m.newProjectsPanel()
	}
	return m.panel.proj
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
	if m != nil && m.cwd != "" {
		return m.cwd
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
