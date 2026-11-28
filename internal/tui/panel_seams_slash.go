package tui

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/app/agentturn"
	"github.com/justwasm/pigasus/internal/app/session/convstore"
	"github.com/justwasm/pigasus/internal/app/session/setup"
	"github.com/justwasm/pigasus/internal/app/session/slashactions"
	browsersubagent "github.com/justwasm/pigasus/internal/app/subagent/browser"
	"github.com/justwasm/pigasus/internal/app/subagenttools"
	"github.com/justwasm/pigasus/internal/app/tools/browser"
	"github.com/justwasm/pigasus/internal/core/cliflags"
	"github.com/justwasm/pigasus/internal/core/confhome"
	"github.com/justwasm/pigasus/internal/core/policy/permissions"
	"github.com/justwasm/pigasus/internal/core/settings"
	"github.com/justwasm/pigasus/internal/features/auth"
	"github.com/justwasm/pigasus/internal/infra/remote/persistentrc"
	"github.com/justwasm/pigasus/internal/infra/remote/rclink"
	"github.com/justwasm/pigasus/internal/ui/editor/editorkit"
	"github.com/justwasm/pigasus/internal/ui/views/agentspanel"
	"github.com/justwasm/pigasus/internal/ui/views/artifactpanel"
	"github.com/justwasm/pigasus/internal/ui/views/codesearchpanel"
	"github.com/justwasm/pigasus/internal/ui/views/contextpanel"
	"github.com/justwasm/pigasus/internal/ui/views/historypanel"
	"github.com/justwasm/pigasus/internal/ui/views/debugpanel"
	"github.com/justwasm/pigasus/internal/ui/views/narration"
	"github.com/justwasm/pigasus/internal/ui/views/terminalpanel"
	"github.com/justwasm/pigasus/vim"

	"github.com/justwasm/pigasus/internal/features/models"
	"github.com/justwasm/pigasus/internal/features/models/catalog"
	"github.com/justwasm/pigasus/internal/tui/panels"
	tuiviewer "github.com/justwasm/pigasus/internal/ui/foundations/viewer"
	tuiviewport "github.com/justwasm/pigasus/internal/ui/foundations/viewport"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	"github.com/justwasm/pigasus/internal/ui/slashcommands"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/internal/ui/views/accountspanel"
	"github.com/justwasm/pigasus/internal/ui/views/adddirpanel"
	"github.com/justwasm/pigasus/internal/ui/views/changelog"
	"github.com/justwasm/pigasus/internal/ui/views/configpanel"
	"github.com/justwasm/pigasus/internal/ui/views/creditspanel"
	diffaction "github.com/justwasm/pigasus/internal/ui/views/diff/action"
	"github.com/justwasm/pigasus/internal/ui/views/effortpanel"
	"github.com/justwasm/pigasus/internal/ui/views/feedbackpanel"
	"github.com/justwasm/pigasus/internal/ui/views/hookspanel"
	"github.com/justwasm/pigasus/internal/ui/views/modelpicker"
	"github.com/justwasm/pigasus/internal/ui/views/permissionspanel"
	"github.com/justwasm/pigasus/internal/ui/views/pluginsui"
	projectspanel "github.com/justwasm/pigasus/internal/ui/views/projects"
	"github.com/justwasm/pigasus/internal/ui/views/providerpanel"
	"github.com/justwasm/pigasus/internal/ui/views/renamepanel"
	"github.com/justwasm/pigasus/internal/ui/views/skillsui"
	"github.com/justwasm/pigasus/internal/ui/views/taskspanel"
	"github.com/justwasm/pigasus/internal/ui/views/viewerpanel"
)

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
	// /debug is one surface with several views, so an argument names a view
	// instead of opening a panel of its own: `/debug wire` lands on the
	// request view, `/debug traj` on the trajectory. A word that names no
	// view opens the log view rather than swallowing the command.
	debugView, debugWantView := debugpanel.ViewLogs, false
	if cmd == "debug" || cmd == "logs" || cmd == "log" {
		if v, ok := debugpanel.ViewByName(args); ok {
			debugView, debugWantView = v, true
		}
	}
	if !m.openSlashPanel(cmd) {
		return false
	}
	if m.overlays.panel.kind == panelregistry.Debug {
		if debugWantView {
			m.debugPanel().SetView(debugView)
		}
		return true
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
	// A numbered viewer's leading cells are the line numbers, and they
	// are the viewer's, not the document's. They are dropped from every
	// row of the copy: a drag from the left edge otherwise pastes "   3
	//      " in front of each line, and the copy stops being the
	// document. The first and last rows keep the columns the pointer
	// chose, shifted by the gutter they lost.
	if g := m.panelSelectionGutter(); g > 0 {
		return tuiviewport.SelectionTextWithoutGutter(lines, sr, sc, er, ec, g)
	}
	return tuiviewport.SelectionText(lines, sr, sc, er, ec)
}

// panelSelectionGutter is how many leading cells of the panel's rows are
// a gutter rather than the document, or 0 when the panel has none.
//
// The answer comes from the panel through the registry, like every other
// question the host asks a panel: a switch on the kind here would be a
// second place that knows which panels exist.
func (m *RootModel) panelSelectionGutter() int {
	if h, ok := m.getPanelRegistry().Get(m.overlays.panel.kind); ok {
		if gr, ok := h.(panelregistry.GutterReporter); ok {
			return gr.SelectionGutter()
		}
	}
	return 0
}

func terminalPanelSelectionText(lines []string, sr, sc, er, ec int, hasTabs bool) string {
	return terminalpanel.ExtractSelectionText(lines, sr, sc, er, ec, hasTabs)
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
			// The settings file stores the mode as a string; the session
			// carries it typed. The conversion is the boundary.
			mode := permissions.AgentMode(configpanel.RawString(raw))
			if mode == permissions.ModeDefault && !settings.Has("agentMode") {
				mode = permissions.AgentMode(setup.DefaultMode())
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
	// /logs owns its own view, cursor and folds, so a key it answers
	// changes what the next frame must draw. The view cache cannot see
	// into the panel, so the panel's answer is what invalidates it —
	// without this the frame stays at the view the user left.
	if m.overlays.panel.kind == panelregistry.Debug {
		handled, cmd := m.debugPanel().HandleKey(key)
		if handled {
			m.invalidateViewCache()
		}
		return handled, cmd
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
	debugFollow bool
	// debugHeight is the /logs frame's usable height, memoised for the
	// length of one repaint. Computing it renders the status row, which
	// stats the endpoints config; the panel asks a dozen times per frame
	// (see debugPanelHeight).
	debugHeight      int
	debugHeightValid bool
	ag              *agentspanel.Panel
	fb              *feedbackpanel.Panel
	cr              *creditspanel.Panel
	plg             *pluginsui.Panel
	rn              *renamepanel.Panel
	ad              *adddirpanel.Panel
	vw              *viewerpanel.Panel
	csp             *codesearchpanel.Panel
	quota           quotaState
	prov            *providerpanel.Panel
	acct            *accountspanel.Panel
	cfg             *configpanel.Panel
	mdl             *modelpicker.Panel
	sk              *skillsui.Panel
	perm            *permissionspanel.Panel
	hk              *hookspanel.Panel
	tp              *taskspanel.Panel
	ef              *effortpanel.Panel
	proj            *projectspanel.Panel
	ctxp            *contextpanel.Panel
	debugp           *debugpanel.Panel
	// ctxHydrated holds the cold steps the /context panel asked the store
	// for. The panel does the asking; the host owns the map, because the
	// store read is the host's and the panel is rebuilt on every open.
	ctxHydrated map[int64]convstore.Step
	art         *artifactpanel.Panel
	hist        *historypanel.Panel
	term        *terminalpanel.Panel
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
	case panelregistry.Artifact:
		m.overlays.panel.art = m.newArtifactPanel()
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
	case panelregistry.Debug:
		m.overlays.panel.debugFollow = true
		m.overlays.panel.window = tuiviewer.MaxOffset(m.debugViewerSpec())
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

// statusEffortSuffix is the " · <tier>" segment the status row appends
// after the model's label.
//
// The label and the suffix are two halves of one answer — catalog's
// StatusLabel answers both — so the row cannot show "Claude Sonnet 4.6"
// with a stray " · high" beside it, nor strip a configured model's own
// "(1017)" tag and replace it with a level it never had.
//
// The question is asked of the *session's* label rather than of
// statusModel: the level that decides the answer lives in the very
// bracket the status row drops ("GPT-OSS 120B (Medium)").
func (m *RootModel) statusEffortSuffix() string {
	label := strings.TrimSpace(m.session.model)
	if label == "" {
		label = m.session.statusModel
	}
	if label != "" {
		if _, tier := catalog.StatusLabel(label); tier == "" {
			return ""
		}
	}
	// A session that names no model runs the reference's own default,
	// which publishes tiers.
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
	case panelregistry.Debug:
		return m.debugPanel()
	case panelregistry.Artifact:
		return m.artifactPanel()
	case panelregistry.History:
		return m.historyPanel()
	case panelregistry.Terminal:
		return m.terminalPanel()
	default:
		return nil
	}
}

func (m *RootModel) buildPanelRegistry() *panelregistry.Registry {
	return panels.BuildRegistry(m)
}
