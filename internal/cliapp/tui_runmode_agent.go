package cliapp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"log"
	"os"
	"time"

	sp "charm.land/bubbles/v2/spinner"
	tas "charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	piagent "github.com/MichaelKinsy/PiG/agent"
	"github.com/MichaelKinsy/PiG/coding"
	"github.com/justwasm/pigasus/internal/accounts"
	"github.com/justwasm/pigasus/internal/agent"
	"github.com/justwasm/pigasus/internal/agenttools"
	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/browsersubagent"
	"github.com/justwasm/pigasus/internal/builtinsubagent"
	"github.com/justwasm/pigasus/internal/hooks"
	"github.com/justwasm/pigasus/internal/keybind"
	"github.com/justwasm/pigasus/internal/modelcatalog"
	"github.com/justwasm/pigasus/internal/permissions"
	"github.com/justwasm/pigasus/internal/piagentbridge"
	"github.com/justwasm/pigasus/internal/subagentquestions"
	"github.com/justwasm/pigasus/internal/subagenttools"
	"github.com/justwasm/pigasus/internal/themepreview"
	"github.com/justwasm/pigasus/internal/toolrunner"
	"github.com/justwasm/pigasus/internal/tools"
	"github.com/justwasm/pigasus/internal/wordmove"
)

func pickerHiddenCommands() []string {
	if !IsBusinessAccount() {
		return nil
	}
	return []string{"/remote-control"}
}

func compactDuration(start, end time.Time) string {
	if start.IsZero() || end.IsZero() {
		return "0s"
	}
	d := end.Sub(start)
	if d < 0 {
		d = 0
	}
	return d.Truncate(time.Second).String()
}

func (m *runMode) Init() tea.Cmd {
	cmds := []tea.Cmd{tas.Blink, armTasksTick(), m.fetchPlanTierCmd()}
	if m.statusLineScriptActive() || m.titleScriptActive() {
		m.statusLineTickArmed = true
		cmds = append(cmds, armExternalStatusLineTick())
	}
	return tea.Batch(cmds...)
}

func (m *runMode) cycleMode() {
	next := permissions.NextMode(m.mode)
	m.mode = next
	if m.policy != nil {
		m.policy.Mode = next
	}
	m.input.Placeholder = permissions.ModeBanner(next)
	m.modeHintDismissed = false
}

func (m *runMode) EnableTools(decls []backend.ToolDecl, runner agent.ToolRunner) {
	m.tools = decls
	m.toolRunner = runner
}

func (m *runMode) moveWordLeft() bool {
	return m.moveWord(true)
}

func (m *runMode) moveWordRight() bool {
	return m.moveWord(false)
}

func (m *runMode) moveWord(back bool) bool {
	row := m.input.Line()
	col := m.input.Column()
	val := m.input.Value()
	var newRow, newCol int
	var moved bool
	if back {
		newRow, newCol, moved = wordmove.MoveLeft(val, row, col)
	} else {
		newRow, newCol, moved = wordmove.MoveRight(val, row, col)
	}
	if !moved {
		return false
	}
	for row > newRow {
		m.input.CursorUp()
		row--
	}
	for row < newRow {
		m.input.CursorDown()
		row++
	}
	m.input.SetCursorColumn(newCol)
	return true
}

func agentToolRegistry(workspace, agentBody string) *tools.Registry {
	catalog := tools.RuntimeCatalog(workspace)
	if agentBody != "" {
		return agenttools.Registry(catalog)
	}
	return catalog
}

func (m *runMode) registerSubagentQuestionTool(catalog *tools.Registry) {
	if m.agentBody != "" {
		return
	}
	catalog.Register(&tools.AskQuestion{
		SubAgentQuestions: m.subAgentQuestions,
		SubAgentID:        subagentquestions.ParentKey,
	})
}

func (m *runMode) applyAgentSwitch(name, body string) {
	m.saveSession()
	m.agentName = name
	m.agentBody = body
	workspace := m.cwd
	if workspace == "" {
		workspace, _ = os.Getwd()
	}
	if m.policy != nil {
		catalog := agentToolRegistry(workspace, body)
		m.registerSubagentQuestionTool(catalog)
		m.EnableTools(advertisedToolDecls(body, catalog), &permissionRunner{
			inner:   &toolrunner.RegistryRunner{Reg: catalog},
			policy:  m.policy,
			sandbox: m.toolSandbox,
		})
	}
	m.response = nil
	m.session = nil
	m.blocks = nil
	m.liveSteps = nil
	m.activeTool = ""
	m.autoTitle = ""
	m.conversationID = newConversationID()
	// The definitions belong to the conversation that just ended.
	m.subagentDefs = subagenttools.NewStore()
	m.resetTrajectory()
}

// tuiSilenceLogger is the destination for the recovered subagent debug-log lines.
var tuiSilenceLogger = log.New(io.Discard, "", 0)

func (m *runMode) subAgentExtras(_ context.Context, be backend.Client, modelLabel string) ([]piagent.AgentTool, func()) {
	if be == nil || m.agentBody != "" {
		return nil, func() {}
	}
	piModel := pigPrintModel(be, modelLabel)
	if piModel == nil {
		return nil, func() {}
	}
	workspace := m.cwd
	if workspace == "" {
		workspace = defaultWorkspaceForRunMode()
	}
	agentDir := auth.AntigravityConfigHome()
	services, err := coding.CreateAgentSessionServices(coding.CreateAgentSessionServicesOptions{
		CWD:      workspace,
		AgentDir: agentDir,
	})
	if err != nil {
		tuiSilenceLogger.Printf("[tui] subagent services: %v", err)
		return nil, func() {}
	}
	subDeps := subagenttools.Deps{
		Services: services,
		Model:    piModel,
		CWD:      workspace,
		AgentDir: agentDir,
		// The conversation's definition store: the definitions live as
		// long as the conversation does, so they survive the per-turn
		// rebuild of the tool set.
		Definitions: m.subagentDefs,
		Tasks:       m.taskReg,
		// The conversation's live-subagent registry: a delegated
		// subagent runs in the background and is reachable by its
		// conversation id until it is killed.
		Registry: m.ensureSubagentRegistry(),
		// The parent's own conversation id. A subagent's message
		// reaches the parent by it, and the parent's messages carry
		// it as the sender.
		ConversationID: m.conversationID,
		// The conversation's one permission policy, applied to the
		// subagent's own tool calls as well.
		PermissionGateFor: m.subagentGateFactory,
		// The parent's own prompt: the reference's `self` type
		// "inherits the parent agent's full configuration including
		// tools, system prompt, and model".
		//
		// It is a function value, so the prompt is built only when a
		// delegation needs it — building it walks the skills
		// directories, and the turn's own prompt is built already.
		SystemPrompt: m.systemPrompt,
		// The parent's tool catalogue: a subagent's tool set is drawn
		// from it, because the reference's grants say which of the
		// parent's tools a subagent may call.
		Catalogue: piagentbridge.Tools(m.catalog),
	}
	subTools := subagenttools.Tools(subDeps)
	// The browser_subagent tool needs the session's model and the
	// browser tools; the runner is installed process-wide, like the MCP
	// pool.
	if m.catalog != nil {
		tools.SetBrowserSubagentProvider(browsersubagent.New(subagenttools.Deps{
			Services: services,
			Model:    piModel,
			CWD:      workspace,
			AgentDir: agentDir,
		}, browsersubagent.Tools(m.catalog)))
		// The browser agent is also a built-in subagent *type*, under
		// the reference's own name: that is how the model reaches it,
		// because the model's twelve wire declarations carry no browser
		// tool (the reference reaches its browser agent the same way).
		//
		// The registry is rebuilt every turn, so the handler carries
		// this turn's model and services — the ones the turn's own
		// subagent tools use.
		reg := builtinsubagent.Default()
		if err := browsersubagent.InstallBuiltin(reg, subDeps, m.catalog); err != nil {
			tuiSilenceLogger.Printf("[tui] browser built-in: %v", err)
		} else {
			subagenttools.SetBuiltinSubagentRegistry(reg)
		}
	}
	closeFn := func() { services.Close() }
	return subTools, closeFn
}

func defaultWorkspaceForRunMode() string {
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return ""
}

// newConversationID returns an RFC-4122-style 8-4-4-4-12 hex UUID
// for the current session.
func newConversationID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		ts := uint64(time.Now().UnixNano())
		for i := 0; i < 16; i++ {
			b[i] = byte(ts >> (uint(i) * 4))
		}
	}
	hex := hex.EncodeToString(b[:])
	return hex[0:8] + "-" + hex[8:12] + "-" + hex[12:16] + "-" + hex[16:20] + "-" + hex[20:32]
}

func resolveRunModeDefaults(b backend.Client, email, model, tier, cwd string) (string, string, string, string) {
	if email == "" {
		email = auth.EffectiveUserEmail()
	}
	if email == "" {
		_, email = accounts.Current()
	}
	if b != nil && model == "" {
		if gb, ok := b.(gatewayBackend); ok {
			if label, ok := modelcatalog.WireLabel(gb.GatewayConfig().ExternalModel); ok {
				model = label
			}
		}
	}
	if model != "" && !isReferenceModel(model) {
		// A configured model keeps its own label: the label has to name
		// the one the endpoint is serving, not collapse to another.
		model = modelcatalog.FormatModelLabel(model)
	}
	if model == "" {
		model = "Gemini 3.8 Flash (High)"
	}
	if cwd == "" {
		if c, err := os.Getwd(); err == nil {
			cwd = c
		} else {
			cwd = "/"
		}
	}
	// The tier is returned as it came in: it stays empty until the
	// server names one, so the banner shows the email alone. A
	// placeholder would flash into the real tier under the user's eyes.
	return email, model, tier, cwd
}

func newRunModeBase(b backend.Client, email, model, tier, cwd string, altScreen bool) *runMode {
	initConvID := newConversationID()
	return &runMode{
		email:                   email,
		model:                   model,
		statusModel:             stripTier(model),
		tier:                    tier,
		cwd:                     cwd,
		backend:                 b,
		altScreen:               altScreen,
		bashRowIdx:              -1,
		taskReg:                 tools.SharedTaskRegistry(),
		subagentDefs:            subagenttools.NewStore(),
		taskSubagentManager:     tools.SharedSubagentManager(),
		subAgentQuestions:       subagentquestions.NewManager(false),
		subAgentQuestionsActive: subagentquestions.ParentKey,
		conversationID:          initConvID,
		initialConversationID:   initConvID,
		autoScroll:              true,
		rowsCacheDirty:          true,
		pinnedPromptRowIdx:      -1,
		streamingSearch:         true,
		replyStart:              -1,
		liveReplyIdx:            -1,
		activeBtwIndex:          -1,
		atRouter:                DefaultRouter,
		hooks:                   hooks.NewManager(),
		narrationStart:          time.Now(),
		wireSettingsPending:     true,
		activeItems:             newActiveItemsModel(),
		titleStore:              &windowTitleStore{},
		taskInbox:               agent.NewTaskInbox(),
	}
}

func initRunModeInput(m *runMode) {
	m.input = tas.New()
	m.input.SetVirtualCursor(false)
	m.input.Prompt = inputPrompt
	m.input.SetPromptFunc(2, func(info tas.PromptInfo) string {
		if info.LineNumber == 0 {
			if m.bashMode {
				return "! "
			}
			return "> "
		}
		return "  "
	})
	m.input.Placeholder = ""
	m.input.ShowLineNumbers = false
	m.input.SetWidth(inputWidth)
	m.input.SetHeight(1)
	m.input.MaxHeight = 24
	m.input.CharLimit = 0
	m.input.EndOfBufferCharacter = ' '
	m.input.DynamicHeight = true
	m.input.MinHeight = 1

	// The prompt wears the selected scheme's accent and the placeholder
	// its dim foreground; the colours follow the scheme, so they are
	// resolved from it rather than hard-coded.
	initScheme := auth.SettingsString("colorScheme")
	// The session wears the configured scheme from the first frame, so
	// the whole interface is painted in it — the reference resolves
	// every colour from the selected scheme.
	m.colorScheme = initScheme
	promptColor, placeholderColor := themepreview.ColorsFor(initScheme)
	promptBlueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(promptColor))
	placeholderStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(placeholderColor))
	st := m.input.Styles()
	st.Focused.Base = lipgloss.NewStyle()
	st.Focused.CursorLine = lipgloss.NewStyle()
	st.Focused.Text = lipgloss.NewStyle()
	st.Focused.Prompt = promptBlueStyle
	st.Focused.Placeholder = placeholderStyle
	st.Focused.EndOfBuffer = lipgloss.NewStyle()
	st.Blurred.Base = lipgloss.NewStyle()
	st.Blurred.Text = lipgloss.NewStyle()
	st.Blurred.Prompt = promptBlueStyle
	st.Blurred.Placeholder = placeholderStyle
	st.Blurred.EndOfBuffer = lipgloss.NewStyle()
	m.input.SetStyles(st)
	m.input.Focus()
	if initScheme != "" {
		m.applyThemeLive(initScheme)
	}
}

func NewRunMode(b backend.Client, email, model, tier, cwd string, altScreen bool) *runMode {
	pickerHidden := pickerHiddenCommands()
	email, model, tier, cwd = resolveRunModeDefaults(b, email, model, tier, cwd)
	m := newRunModeBase(b, email, model, tier, cwd, altScreen)

	if kb, err := keybind.Load(keybind.DefaultPath()); err == nil {
		m.keybindings = kb
	}
	m.startTasksSubscription()
	initRunModeInput(m)

	m.spinner = sp.New(sp.WithSpinner(sp.Dot))
	// 【一手】 agy 1.4.7: with a seasonal event switched on the
	// streaming spinner is the event's own — the Halloween event shows
	// a four-frame moon phase ("◐  Generating…").
	m.refreshSeasonalTheme()
	if len(pickerHidden) > 0 {
		m.picker.SetHiddenCommands(pickerHidden...)
	}
	// The "/model <query>" popup filters the catalogue the CLI can
	// select: the reference's own models plus the ones the user
	// configured.
	m.picker.SetModelCatalogueSource(completionModelRows)
	return m
}
