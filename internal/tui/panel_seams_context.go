package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/endpoints"
	"github.com/justwasm/pigasus/internal/app/artifacts"
	"github.com/justwasm/pigasus/internal/app/prompt/agyprompt"
	"github.com/justwasm/pigasus/internal/app/session/convstore"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/core/compaction"
	"github.com/justwasm/pigasus/internal/core/confhome"
	"github.com/justwasm/pigasus/internal/features/models/modelctx"
	"github.com/justwasm/pigasus/internal/ui/editor/vimkeys"
	"github.com/justwasm/pigasus/internal/ui/toolsteps"
	"github.com/justwasm/pigasus/internal/ui/views/contextpanel"
	"github.com/justwasm/pigasus/internal/ui/views/statusline"
	"github.com/justwasm/pigasus/internal/ui/views/statusrow"
	"github.com/justwasm/pigasus/internal/ui/views/terminalpanel"
	"github.com/justwasm/pigasus/internal/ui/views/usagepanel"

	"github.com/justwasm/pigasus/internal/app/toolrunner"
	coreprojects "github.com/justwasm/pigasus/internal/core/cliflags/projects"
	"github.com/justwasm/pigasus/internal/core/wirelog"
	"github.com/justwasm/pigasus/internal/features/tools/clipboard"
	"github.com/justwasm/pigasus/internal/tui/panels"
	tuiviewer "github.com/justwasm/pigasus/internal/ui/foundations/viewer"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/internal/ui/views/changelog"
	"github.com/justwasm/pigasus/internal/ui/views/hostactions"
	"github.com/justwasm/pigasus/internal/ui/views/debugpanel"
	projectspanel "github.com/justwasm/pigasus/internal/ui/views/projects"
)

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
		ArtifactCount:            len(m.Artifacts()),
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
	return statusline.ContextUsedPercent(prompt, m.statusContextWindow(model))
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

func (m *RootModel) ContextState() contextpanel.State {
	return m.contextState()
}

// Artifacts is the conversation's artifact list, read from the brain
// directory the artifact layer writes its sidecars into.
//
// It is the same call checkpointSnapshot makes for the resume block, so the
// /artifact panel and a resumed conversation's checkpoint cannot list
// different artifacts. An empty conversation id yields nil, which the panel
// renders as its empty frame.
func (m *RootModel) Artifacts() []compaction.ArtifactRef {
	return artifacts.List(m.session.conversationID)
}

func (m *RootModel) contextState() contextpanel.State {
	ctxMax := modelctx.PublishedContextWindow(m.session.model)
	if ctxMax <= 0 {
		ctxMax = contextpanel.MaxContextTokens
	}
	acTokens, acSpec := modelctx.AutoCompactWindow(m.session.model)
	st := contextpanel.State{
		Model:                   m.contextModel(),
		ContextMax:              ctxMax,
		Width:                   m.viewport.width,
		CompactJustHappened:     m.session.store.CompactJustHappened,
		AutoCompactTokens:       acTokens,
		AutoCompactOverrideSpec: acSpec,
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
	// Each count below is measured where the host can measure it. When it
	// cannot — a run mode with no prompt built, a transport that reports no
	// tool declarations — the row falls back to a built-in figure, and the
	// flag says so: the panel renders it with a "~" so a number that never
	// changes is not read as a measurement.
	base, rules, skills, subagents := m.promptCategoryTokens()
	st.SystemPrompt = base - rules
	if st.SystemPrompt <= 0 && m.session.agentBody == "" {
		st.SystemPrompt = 4500
		st.SystemPromptEstimated = true
	}
	st.Rules = rules
	if st.Rules <= 0 && m.session.agentBody == "" {
		st.Rules = 4400
		st.RulesEstimated = true
	}
	st.Skills = skills
	if st.Skills <= 0 && m.session.agentBody == "" {
		st.Skills = 10200
		st.SkillsEstimated = true
	}
	st.Subagents = subagents
	if st.Subagents <= 0 && m.session.agentBody == "" {
		st.Subagents = 706
		st.SubagentsEstimated = true
	}
	tools := m.wireToolDecls()
	st.SystemTools = contextpanel.ToolTokenEstimate(tools)
	if st.SystemTools <= 0 {
		st.SystemTools = 7100
		st.SystemToolsEstimated = true
	}
	st.SystemMessages = contextpanel.SystemMessageTokens(m.SessionMessages())
	if st.SystemMessages <= 0 {
		st.SystemMessages = 13500
		st.SystemMessagesEstimated = true
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
		m.conversation.compactedToolSteps > 0 ||
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
		m.conversation.compactedToolSteps > 0 ||
		contextpanel.CheckpointBufferTokens(m.SessionMessages()) > 0

	if hasCompacted {
		agent = contextpanel.AgentMessageTokens(m.SessionMessages())
	} else {
		completed := atomic.LoadInt64(&m.session.store.SessionCompletionTokens)
		lastTurn := atomic.LoadInt64(&m.session.store.LastTurnCompletionTokens)
		agent = contextpanel.AgentTokens(completed, lastTurn)
	}

	// The tool results the model actually carries are the steps after the
	// latest compaction's boundary: the summary stands in for everything
	// before it. The steps are read in chronological order — the finalised
	// blocks first, the turn in flight last — and the boundary is a step
	// count, so a compaction that landed mid-turn keeps the tools the turn
	// ran afterwards.
	steps := m.conversationSteps()
	if hasCompacted {
		if skip := m.conversation.compactedToolSteps; skip > 0 {
			if skip > len(steps) {
				skip = len(steps)
			}
			steps = steps[skip:]
		}
	}
	tool = contextpanel.ToolResultTokens(toolsteps.ToolResults(steps)...)
	return user, agent, tool
}

// conversationSteps returns the conversation's tool steps in chronological
// order: the finalised blocks', then the turn in flight's.
//
// The panel and the compaction boundary both read the conversation through
// here, so the two cannot disagree about what "the steps" are.
func (m *RootModel) conversationSteps() []toolsteps.Step {
	var steps []toolsteps.Step
	for _, b := range m.conversation.blocks {
		steps = append(steps, b.Steps...)
	}
	steps = append(steps, m.conversation.liveSteps...)
	return steps
}

// markCompacted records the tool steps the latest compaction replaced with
// its summary, so the panel stops counting them.
//
// It is called at the compaction itself — from both the manual /compact and
// the automatic checkpoint — and the count is a prefix of
// conversationSteps(), which is why it survives the turn's finalisation:
// finalising moves steps from liveSteps into blocks without changing how
// many there are.
//
// It returns the tool-result token count of the steps it is about to take out
// of the active context, which is what a compaction banner's "before" figure
// has to add. Returning it is what makes the pair atomic: a caller that took
// the count first and then called this could silently measure the
// post-compaction conversation instead, and no type would catch it.
func (m *RootModel) markCompacted() (compactedToolTokens int) {
	steps := m.conversationSteps()
	m.conversation.compactedToolSteps = len(steps)
	return contextpanel.ToolResultTokens(toolsteps.ToolResults(steps)...)
}

// forgetConversationState drops the per-conversation state the host keeps
// beside the session.
//
// Both fields describe *a conversation*: the compaction boundary counts its
// steps, and the live row is a row index inside its transcript. They are
// meaningless — and actively harmful — on the conversation that replaces it:
// a boundary carried over hides the new conversation's tools, and a stale row
// index makes the next compacting tick write into an unrelated row.
//
// Every path that starts a conversation afresh calls this, so the two cannot
// drift into being reset in different places.
func (m *RootModel) forgetConversationState() {
	m.conversation.compactedToolSteps = 0
	m.conversation.compactRowIdx = 0
	m.conversation.compactRowLive = false
	// A compaction running for the conversation being left behind must not
	// keep the new conversation's status row suppressed.
	m.session.store.CompactLiveInTranscript = false
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
	// A manual /compact draws its own live row inside the conversation, so
	// the status row stays out of it: one compaction must not appear twice.
	if m.session.store.CompactLiveInTranscript {
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

// logsState reads the log view's facts.
//
// LoadState tails the session log file, and the panel asks for the state
// more than once per repaint (the frame, the scroll bound, the row
// count). Doing that as a disk read every time meant the panel touched
// the filesystem several times per wheel notch. The memo below is keyed
// by size and mtime, so a file that has grown is re-read and one that has
// not costs a stat.
func (m *RootModel) logsState() debugpanel.State {
	return debugpanel.LoadState(m.viewport.width, m.viewport.height, m.overlays.panel.debugFollow)
}

// InvalidateLogsTail drops the memoised log tail.
//
// The refresh tick calls this: it is the one event that should make the
// panel look at the file again.
func (m *RootModel) InvalidateLogsTail() {
	debugpanel.InvalidateTail()
}

func (m *RootModel) debugViewerSpec() tuiviewer.Spec {
	return debugpanel.Spec(m.logsState())
}

// debugPanelHeight is how many rows the /logs frame may draw.
//
// It is the terminal height minus the status area, because the status
// area is drawn under the panel and the frame is clipped to the
// terminal: a panel told the whole height would draw its last rows off
// the bottom and lose its header to the clip. The height seam and the
// panel's own rectangle both read this one number.
// debugPanelHeight is how many rows the /logs frame may draw.
//
// It is memoised per frame. Computing it runs the status row's render,
// which resolves the endpoint overrides and the API key — each of which
// stats the config file. The panel asks for the height a dozen times per
// repaint (every layout, every bodyStart, the wheel's bound), so the
// memo turns a dozen stats into one. The stat itself remains: the file
// is the user's to edit, and caching it past a frame would be a stale
// status row rather than a slow frame.
func (m *RootModel) debugPanelHeight() int {
	if m.overlays.panel.debugHeightValid {
		return m.overlays.panel.debugHeight
	}
	h := m.viewport.height
	if h > 0 && m.statusLine.visible != "off" {
		h -= len(statusline.AreaRows(m.statusAreaState()))
	}
	if h <= 0 {
		h = 0
	} else if h < 1 {
		h = 1
	}
	m.overlays.panel.debugHeight = h
	m.overlays.panel.debugHeightValid = true
	return h
}

// debugPanel is the /logs surface: one full-viewport panel with the log
// file, the request and the trajectory.
//
// The panel is built once and kept for the lifetime of the session. It
// used to be rebuilt on every open, which is fine for a read-only viewer
// but not for a surface a user leaves mid-trajectory with folds set: the
// folds and the cursor are the panel's, so the panel is too.
func (m *RootModel) debugPanel() *debugpanel.Panel {
	if m.overlays.panel.debugp == nil {
		m.overlays.panel.debugp = debugpanel.New(debugpanel.Seams{
			Logs: func() debugpanel.State { return m.logsState() },
			Wire: func() debugpanel.WireState { return m.contextWireState() },
			Trajectory: func() debugpanel.TrajectoryState {
				return m.contextTrajectoryState()
			},
			Requests: func() debugpanel.RequestState {
				return debugpanel.RequestState{
					Records: wirelog.Shared().Records(),
					Width:   m.viewport.width,
				}
			},
			Context: func() debugpanel.ContextState { return m.debugContextState() },
			Hydrate: func(index int64) tea.Cmd { return m.contextHydrateCmd(index) },
			Read:    func(id string) tea.Cmd { return m.debugReaderCmd(id) },
			Copy:    func(text string) tea.Cmd { return clipboardCopyCmd(text) },
			Close:   func() tea.Cmd { return m.closePanel() },
			Width:   func() int { return m.viewport.width },
			Height:  func() int { return m.debugPanelHeight() },
			Follow:  func() bool { return m.overlays.panel.debugFollow },
			SetFollow: func(v bool) {
				m.overlays.panel.debugFollow = v
				m.invalidateViewCache()
			},
		})
	}
	return m.overlays.panel.debugp
}

// debugContextState is the Context view's facts: what the provider reported,
// what the recorder measured, and the window they have to fit in.
//
// Every number here is read from the place that owns it. The occupancy is
// the last turn's *reported* prompt tokens — not the session total, which is
// billing and grows without bound, and not the estimator, which is the
// number this view exists to check. The measured sizes are the recorder's,
// because it is the only thing in the process that counted the bytes that
// actually left.
func (m *RootModel) debugContextState() debugpanel.ContextState {
	st := debugpanel.ContextState{
		Model:  m.contextModel(),
		Width:  m.viewport.width,
		Messages: m.session.messageCount(),
		Streaming: m.conversation.streaming,
	}
	st.WindowTokens = m.statusContextWindow(m.contextModel())
	st.AutoCompactTokens, st.AutoCompactSpec = modelctx.AutoCompactWindow(m.contextModel())

	st.LastPromptTokens = int(atomic.LoadInt64(&m.session.store.LastTurnPromptTokens))
	st.LastCompletionTokens = int(atomic.LoadInt64(&m.session.store.LastTurnCompletionTokens))
	st.SessionPromptTokens, st.SessionCompletionTokens = m.sessionTokenTotals()
	st.Compactions = compaction.History()

	// The newest exchange is the request the last turn put on the wire, and
	// its body is the only size in this report that was measured rather
	// than estimated.
	recs := wirelog.Shared().Records()
	if n := len(recs); n > 0 {
		last := recs[n-1]
		st.Wire = &last
	}
	return st
}

func (m *RootModel) panelDebugLines() []string {
	// The height memo covers one repaint, and this is where a repaint
	// starts: the panel snapshots the size it is drawn for, so the host's
	// answer has to be recomputed before it does. Clearing here rather
	// than in a tick keeps the memo correct no matter what caused the
	// draw — a key, a wheel, a resize or a refresh.
	m.overlays.panel.debugHeightValid = false
	return m.debugPanel().Lines()
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

func (m *RootModel) PanelDebugLines() []string {
	return m.panelDebugLines()
}

func (m *RootModel) DebugViewerSpec() tuiviewer.Spec {
	return m.debugViewerSpec()
}
