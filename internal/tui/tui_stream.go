package tui

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/agentlink"
	"github.com/justwasm/pigasus/configdir"
	"github.com/justwasm/pigasus/endpoints"
	"github.com/justwasm/pigasus/internal/app/agentturn"
	"github.com/justwasm/pigasus/internal/app/artifacts"
	"github.com/justwasm/pigasus/internal/app/prompt/agyprompt"
	"github.com/justwasm/pigasus/internal/app/session/convstore"
	"github.com/justwasm/pigasus/internal/app/session/setup"
	"github.com/justwasm/pigasus/internal/app/subagenttools"
	"github.com/justwasm/pigasus/internal/app/tools/shell"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/core/cliflags"
	"github.com/justwasm/pigasus/internal/core/clilog"
	"github.com/justwasm/pigasus/internal/core/compaction"
	"github.com/justwasm/pigasus/internal/core/confhome"
	"github.com/justwasm/pigasus/internal/core/interaction"
	"github.com/justwasm/pigasus/internal/core/policy/permissions"
	"github.com/justwasm/pigasus/internal/core/settings"
	accounts "github.com/justwasm/pigasus/internal/features/accounts/core"
	"github.com/justwasm/pigasus/internal/features/auth"
	"github.com/justwasm/pigasus/internal/features/checkpoint"
	"github.com/justwasm/pigasus/internal/features/models"
	modelcatalog "github.com/justwasm/pigasus/internal/features/models/catalog"
	"github.com/justwasm/pigasus/internal/features/personas"
	"github.com/justwasm/pigasus/internal/features/tools/skills/pigasusskills"
	"github.com/justwasm/pigasus/internal/ui/toolsteps"
	"github.com/justwasm/pigasus/internal/ui/transcript"
	"github.com/justwasm/pigasus/internal/ui/views/contextpanel"
)

// ---- tui_stream_prompt.go ----

// prepareStreamTurnMessages turns the session into the messages the turn
// hands to the model.
//
// A user turn keeps the reference's <USER_REQUEST> envelope (appendUserTurn
// builds it). A system notice — a background task's notification — does
// not: the reference's own lead-in says the message was not sent by the
// user, so it travels in the user-content position without the envelope
// the reference wraps around what the user typed.
//
// The implicit trajectory's messages come first: the reference's cumulative
// prompt carries the persistent context ahead of the turn's own messages,
// and the `!` commands the user ran outside the conversation are part of
// that context (see tui_cumulative.go).
func (m *RootModel) prepareStreamTurnMessages() []backend.Message {
	// A notification that arrived after the turn was set up still has to
	// reach the model: it joins the session here.
	m.drainTaskNotifications()
	// A `!` command that finished since the last turn reaches the model
	// with this one.
	m.collectImplicitMessages()
	var msgs []backend.Message
	if sup := m.session.sup(); sup != nil {
		msgs = sup.WireMessages()
	} else {
		msgs = m.SessionMessages()
	}
	for i := range msgs {
		switch msgs[i].Role {
		case "user":
			msgs[i].Content = msgs[i].WireText()
			msgs[i].WireContent = ""
		case "system":
			msgs[i].Role = "user"
			msgs[i].Content = msgs[i].WireText()
			msgs[i].WireContent = ""
		}
	}
	return msgs
}

// turnEngine is the engine the session's turns run on.
//
// A session that is not attached to a server runs its turns in this
// process; a session that is attached runs them on the server. Both
// answer with the same frames, so the transcript does not know which one
// it is rendering.
//
// A run mode built by hand — a test, a snapshot — has no engine yet, and
// gets the local one.
func (m *RootModel) turnEngine() agentturn.Engine {
	if m.turn.engine != nil {
		return m.turn.engine
	}
	m.turn.engine = agentturn.NewLocal(m.localTurnEnv)
	return m.turn.engine
}

// localTurnEnv builds the environment one local turn runs in.
//
// The environment is built per turn: the transport, the prompt and the
// tool set belong to the session, and the session changes them between
// two turns — the user switches the model, a plugin installs a tool, the
// workspace moves.
func (m *RootModel) localTurnEnv(ctx context.Context, model string) (agentturn.Env, func()) {
	be := m.turn.backend
	extras, closeSub := m.subAgentExtras(ctx, be, model)
	// Read the live sections here, on the caller's goroutine: the engine
	// consumes the closure on the turn's goroutine, and the registries below
	// are the TUI's own state.
	snapshot := m.checkpointSnapshot()
	priorRequests := m.lastCheckpointRequests()
	effort := m.currentEffortName()
	execModel := ""
	if _, plannerEffort, em, ok := modelcatalog.ResolvePlanExecute(model); ok {
		if plannerEffort != "" {
			effort = plannerEffort
		}
		execModel = em
	}
	return agentturn.Env{
		Backend: be,
		BackendFor: func(name string) backend.Client {
			if m.turn.backendFor != nil {
				return m.turn.backendFor(name)
			}
			return nil
		},
		Model:                   model,
		SystemPrompt:            m.systemPrompt(),
		Tools:                   m.tools.decls,
		ToolRunner:              m.tools.runner,
		ExtraTools:              extras,
		Effort:                  effort,
		ExecutionModel:          execModel,
		CheckpointSnapshot:      func() checkpoint.Snapshot { return snapshot },
		CheckpointPriorRequests: func() []string { return priorRequests },
	}, closeSub
}

// lastCheckpointRequests returns the request list the most recent persisted
// checkpoint carried.
//
// After a manual compaction the session's only user message is the resume
// block, so a later checkpoint must not rebuild the list from the session;
// the reference keeps the list in the checkpoint and carries it forward.
func (m *RootModel) lastCheckpointRequests() []string {
	var out []string
	for _, step := range convstore.BuildSteps(m.trajectoryFacts()) {
		if step.Type == convstore.StepCheckpoint && len(step.UserRequests) > 0 {
			out = step.UserRequests
		}
	}
	return out
}

// checkpointSnapshot supplies the resume block's dynamic sections.
//
// The sections describe live session state, so they are read when the turn
// starts, not cached on the engine: a background task started by the previous
// turn and a subagent spawned minutes ago must both appear in the next
// checkpoint. A conversation with no artifacts, tasks or subagents yields
// empty sections, and the renderer omits them.
func (m *RootModel) checkpointSnapshot() checkpoint.Snapshot {
	convID := m.session.conversationID
	var subagents *compaction.SubagentSection
	if convID != "" {
		if reg := m.agents.registry; reg != nil && reg.ParentID() == convID {
			subagents = reg.SnapshotSection(subagenttools.DefaultMaxSubagentSnapshots)
		}
	}
	return checkpoint.Snapshot{
		Artifacts:    artifacts.List(convID),
		RunningTasks: shell.RunningTaskSnapshots(convID),
		Subagents:    subagents,
	}
}

// turnCollector turns a turn's frames into the rows the transcript draws.
//
// The frames are the protocol's, not the transcript's: a turn that ran
// here and a turn that ran on a server produce the same frames, and this
// collector is the one place that knows how a frame becomes a row. One
// collector serves both, so the two paths cannot render differently.
type turnCollector struct {
	// prog is the running program the rows are posted to. It is nil
	// when there is no program — the collector still counts the turn.
	prog *tea.Program

	// reply is the answer as it arrived, and reasoning the thinking.
	reply     strings.Builder
	reasoning strings.Builder

	// steps are the tool calls the turn made, in order.
	steps []toolsteps.Step

	// thoughtStart is when the current thinking burst began, thoughtElapsed
	// accumulates completed thinking bursts in this turn, and compactStart
	// is when the compaction began.
	thoughtStart   time.Time
	thoughtElapsed time.Duration
	compactStart   time.Time

	// thoughtSent records that the thought row was already opened.
	thoughtSent bool

	// meta is the turn's token accounting.
	meta *backend.ChunkMeta

	// err is the turn's terminal failure.
	err error
}

func (c *turnCollector) freezeThinking() {
	if c.thoughtStart.IsZero() {
		return
	}
	if d := time.Since(c.thoughtStart); d > 0 {
		c.thoughtElapsed += d
	}
	c.thoughtStart = time.Time{}
}

func (c *turnCollector) thinkingDuration() time.Duration {
	d := c.thoughtElapsed
	if !c.thoughtStart.IsZero() {
		if cur := time.Since(c.thoughtStart); cur > 0 {
			d += cur
		}
	}
	return d
}

// text renders a text delta.
func (c *turnCollector) text(s string) {
	if s == "" {
		return
	}
	c.freezeThinking()
	c.reply.WriteString(s)
	if c.prog != nil {
		c.prog.Send(backendChunkMsg(s))
	}
}

// thought renders a reasoning delta.
//
// The reference shows one thought row per turn, not one per delta: the
// first delta opens the row, the rest fill it.
func (c *turnCollector) thoughtDelta(s string) {
	if s == "" {
		return
	}
	c.reasoning.WriteString(s)
	if c.thoughtStart.IsZero() {
		c.thoughtStart = time.Now()
	}
	if !c.thoughtSent {
		c.thoughtSent = true
		if c.prog != nil {
			c.prog.Send(toolsteps.ThoughtStartMsg{})
		}
	}
	if c.prog != nil {
		c.prog.Send(toolsteps.ThoughtChunkMsg{Delta: s})
	}
}

// tool renders a tool call's start, or its outcome.
//
// The call is reported twice: once when it starts, so the transcript can
// paint the row while the tool runs, and once when it ends, with the
// tool's own output.
func (c *turnCollector) tool(t *agentlink.Tool) {
	if t == nil {
		return
	}
	c.freezeThinking()
	args := json.RawMessage(t.Args)
	switch t.Phase {
	case agentlink.ToolStart:
		if c.prog != nil {
			c.prog.Send(toolsteps.ToolRunningMsg{Step: toolsteps.NewRunningStep(t.Name, args)})
		}
	case agentlink.ToolEnd:
		step := toolsteps.NewToolStep(t.Name, args, t.Result, t.Err)
		c.steps = append(c.steps, step)
		if c.prog != nil {
			c.prog.Send(toolsteps.ToolStepMsg{Step: step})
		}
	}
}

// compact renders a compaction's start, or its outcome.
//
// The duration is the server's own: the client cannot measure how long a
// compaction took on the other side of the wire.
func (c *turnCollector) compact(cm *agentlink.Compact) {
	if cm == nil {
		return
	}
	switch cm.Phase {
	case agentlink.CompactStart:
		c.compactStart = time.Now()
		if c.prog != nil {
			c.prog.Send(compactStartMsg{Reason: cm.Reason, StartedAt: c.compactStart})
		}
	case agentlink.CompactEnd:
		ended := time.Now()
		if !c.compactStart.IsZero() && cm.ElapsedMS > 0 {
			ended = c.compactStart.Add(time.Duration(cm.ElapsedMS) * time.Millisecond)
		}
		if c.prog != nil {
			c.prog.Send(compactEndMsg{
				Reason:               cm.Reason,
				EndedAt:              ended,
				Summary:              cm.Summary,
				Resume:               cm.Resume,
				Steps:                cm.StepsSummarized,
				KeepRecent:           cm.KeepRecent,
				Strategy:             cm.Strategy,
				SummaryModel:         cm.SummaryModel,
				Sync:                 cm.Sync,
				TokensBefore:         cm.TokensBefore,
				EstimatedTokensAfter: cm.TokensAfter,
				Aborted:              cm.Aborted,
			})
		}
	}
}

// notice renders an out-of-band notice.
func (c *turnCollector) notice(n *agentlink.Notice) {
	if n == nil {
		return
	}
	switch n.Kind {
	case agentlink.NoticeAutoCont:
		if c.prog != nil {
			c.prog.Send(autoContBannerMsg{depth: n.Depth, maxDepth: n.MaxDepth})
		}
	case agentlink.NoticeEligibility:
		if c.prog != nil {
			c.prog.Send(eligibilityNoticeMsg{Reason: n.Text, VerificationURL: n.URL})
		}
	case agentlink.NoticeError:
		if c.err == nil && n.Text != "" {
			c.err = errors.New(n.Text)
		}
	case agentlink.NoticeSteerApplied:
		if c.prog != nil {
			c.prog.Send(steerAppliedMsg{instruction: n.Text})
		}
	case agentlink.NoticeModelRetry:
		if c.prog != nil {
			c.prog.Send(modelRetryStatusMsg{
				attempt:       n.Attempt,
				maxAttempts:   n.MaxAttempts,
				retryAtUnixMS: n.RetryAtUnixMS,
				reason:        clilog.Sanitize(n.Text),
			})
		}
	case agentlink.NoticeFailover:
		if c.prog != nil {
			c.prog.Send(failoverNoticeMsg{
				fromTarget: n.FromTarget,
				toTarget:   n.ToTarget,
				model:      n.Model,
				text:       n.Text,
			})
		}
	}
}

type failoverNoticeMsg struct {
	fromTarget string
	toTarget   string
	model      string
	text       string
}

type modelRetryStatusMsg struct {
	attempt       int
	maxAttempts   int
	retryAtUnixMS int64
	reason        string
}

type steerAppliedMsg struct {
	instruction string
}

// usage records token accounting from an incoming agentlink frame or turn result.
func (c *turnCollector) usage(u *agentlink.Usage) {
	if u == nil {
		return
	}
	c.meta = &backend.ChunkMeta{
		PromptTokens:     u.PromptTokens,
		CompletionTokens: u.CompletionTokens,
		TotalTokens:      u.TotalTokens,
		ReasoningTokens:  u.ReasoningTokens,
	}
}

// result ends the turn.
//
// The result carries the whole answer, including the tail the stream
// held back, so it replaces what the text deltas accumulated. The token
// accounting rides on it too.
func (c *turnCollector) result(f agentlink.Frame) {
	if f.Usage != nil {
		c.usage(f.Usage)
	}
	if f.Error != "" {
		c.err = errors.New(f.Error)
	}
	if f.Result == nil {
		return
	}
	if f.Result.Reply != "" {
		c.reply.Reset()
		c.reply.WriteString(f.Result.Reply)
	}
	if f.Result.Reasoning != "" {
		c.reasoning.Reset()
		c.reasoning.WriteString(f.Result.Reasoning)
	}
}

// newTurnBridge builds the bridge that runs one turn. The bridge is a
// host translation seam: it converts the engine's frames into tea messages,
// owns the turn's Tier-4 cancellation runtime and reports usage.
func (m *RootModel) newTurnBridge() *EngineBridge {
	return NewEngineBridge(BridgeOptions{
		Engine:    m.turnEngine(),
		Program:   m.turn.prog,
		PromptMgr: DefaultPermissionPrompts,
		OnUsage:   m.recordTurnUsage,
		OnFrame: func(f agentlink.Frame) {
			if m.overlays.wireHUD != nil {
				m.overlays.wireHUD.RecordFirstToken()
				m.overlays.wireHUD.RecordChunk(len(f.Text))
			}
			if rec := m.remote.inprocessDisp; rec != nil {
				var toolName, toolArgs, toolResult string
				var toolErr bool
				if f.Tool != nil {
					toolName = f.Tool.Name
					toolArgs = f.Tool.Args
					toolResult = f.Tool.Result
					toolErr = f.Tool.Err
				}
				rec.RecordExternalTurnFrame(m.session.conversationID, string(f.Kind), f.Text, toolName, toolArgs, toolResult, toolErr)
			}
		},
		HookStart:    m.hookSessionStart,
		HookPre:      m.hookPreInvocation,
		HookStop:     m.hookStop,
		HookPost:     m.hookPostInvocation,
		SessionEpoch: func() uint64 { return m.session.epoch() },
	})
}

// turnModel is the model the session's turns run under.// turnModel is the model the session's turns run under.
func (m *RootModel) turnModel() string {
	if m.session.domain != nil && m.session.domain.Model() != "" {
		return m.session.domain.Model()
	}
	if m.session.model != "" {
		return m.session.model
	}
	return "gemini-3.8-flash-high"
}

func (m *RootModel) streamPrompt(prompt string) tea.Cmd {
	// The messages are prepared on the Update goroutine: the preparation
	// drains the task inbox and the implicit trajectory, and both belong
	// to the session.
	msgs := m.prepareStreamTurnMessages()
	model := m.turnModel()

	if m.session.remoteAgent && !models.IsRemoteAgentSupported(model) {
		return func() tea.Msg {
			return toolsteps.TurnDoneMsg{
				Err: errors.New(models.RemoteAgentUnsupportedMessage(model)),
			}
		}
	}

	var unlock func()
	if tl := m.remote.turnLock; tl != nil {
		if !tl.TryLock() {
			return func() tea.Msg {
				return toolsteps.TurnDoneMsg{
					Err: errors.New("a remote-control turn is currently running"),
				}
			}
		}
		unlock = tl.Unlock
	}

	// The bridge and its Tier-4 runtime are created on the Update
	// goroutine, so a cancel that arrives before the Cmd starts still
	// finds a running turn. The runtime is the single cancellation
	// authority.
	bridge := m.newTurnBridge()
	m.turn.activeBridge = bridge
	turnCtx, endTurn := bridge.turns.Begin(context.Background())
	turnCtx = interaction.WithHost(turnCtx, m.turnInteractionHost())
	if m.overlays.wireHUD != nil {
		m.overlays.wireHUD.RecordStreamStart(model)
	}

	return func() tea.Msg {
		defer endTurn()
		if unlock != nil {
			defer unlock()
		}
		res := bridge.runTurnOn(turnCtx, model, msgs)
		if m.overlays.wireHUD != nil {
			m.overlays.wireHUD.RecordStreamDone()
		}
		return res
	}
}

type backendChunkMsg []byte

// planTierMsg carries the tier the server named and the account it
// belongs to.
//
// The account is part of the message because the tier belongs to an
// account, not to the session: a session that signs in as another
// account must not keep the previous account's tier.
type planTierMsg struct {
	tier  string
	email string
}

// fetchPlanTierCmd asks the server for the active account's plan tier
// and caches the answer in the registry.
//
// The tier belongs to the account behind the shared credential, so it
// is cached under that account: the /accounts panel then shows the
// tier of every account that has been active at least once, without
// querying the inactive ones.
//
// The credential is read *once*: the token that answers the request
// and the email the answer is filed under have to come from the same
// credential. Reading them twice would file one account's tier under
// another account's name whenever a switch lands between the two
// reads.
//
// It is a method because the cache write needs the registry, and the
// registry write happens in the goroutine the command runs in.
func (m *RootModel) fetchPlanTierCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		tok, email := accounts.Current()
		if tok == nil || tok.Token.AccessToken == "" {
			return planTierMsg{}
		}
		tier := auth.FetchPlanTierWithToken(ctx, tok.Token.AccessToken)
		if tier != "" && email != "" {
			_ = accounts.SetPlanTier(email, tier)
		}
		return planTierMsg{tier: tier, email: email}
	}
}

var fetchQuotaSummaryFn = auth.FetchQuotaSummary

type quotaSummaryMsg struct {
	summary *auth.QuotaSummary
	err     error
}

// fetchQuotaSummaryCmd fetches the active account's quota summary.
//
// A fresh snapshot is reused when there is one: the /accounts panel
// queries every account's quota, so the /usage panel that follows it
// shows the same numbers instead of querying the same account a second
// time.
func (m *RootModel) fetchQuotaSummaryCmd() tea.Cmd {
	cached := m.cachedQuota(m.session.email)
	return func() tea.Msg {
		if cached != nil {
			return quotaSummaryMsg{summary: cached}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		summary, err := fetchQuotaSummaryFn(ctx)
		return quotaSummaryMsg{summary: summary, err: err}
	}
}

// refreshQuotaWhileStreamingCmd forces a fresh quota fetch during long turns
// (>= 60s) so /usage, /quota, and custom statusLine commands stay current.
func (m *RootModel) refreshQuotaWhileStreamingCmd() tea.Cmd {
	if strings.TrimSpace(os.Getenv("GEMINI_API_KEY")) != "" || endpoints.CurrentGeminiAPIKey() != "" {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		summary, err := fetchQuotaSummaryFn(ctx)
		return quotaSummaryMsg{summary: summary, err: err}
	}
}

type backendDoneMsg struct{}

type autoContBannerMsg struct {
	depth    int
	maxDepth int
}

type compactDoneMsg = compaction.ManualDoneMsg

type compactStartMsg struct {
	Reason    string
	StartedAt time.Time
}

type compactEndMsg struct {
	Reason               string
	EndedAt              time.Time
	Summary              string
	Resume               string
	Steps                int
	KeepRecent           int
	Strategy             string
	SummaryModel         string
	Sync                 bool
	TokensBefore         int
	EstimatedTokensAfter int
	Aborted              bool
}

// systemPrompt builds the Antigravity CLI system prompt for this run mode.
func (m *RootModel) systemPrompt() string {
	opts := m.promptOptions()
	if m.session.agentBody != "" {
		return agyprompt.BuildAgent(m.session.agentBody, opts)
	}
	return agyprompt.Build(opts)
}

// promptOptions assembles the reference prompt inputs.
func (m *RootModel) promptOptions() agyprompt.Options {
	workspace := m.session.cwd
	if workspace == "" {
		workspace = defaultWorkspaceForRootModel()
	}
	conversationID := m.session.conversationID
	if conversationID == "" {
		conversationID = newConversationID()
	}
	agentDir := confhome.Antigravity()
	opts := agyprompt.DefaultOptions(workspace, agentDir, conversationID)
	if m.session.flags != nil {
		for _, dir := range cliflags.GetterStringValues(m.session.flags, "add-dir") {
			if abs, err := filepath.Abs(dir); err == nil {
				opts.Workspaces = append(opts.Workspaces, abs)
			}
		}
	}
	opts.UserRules = agyprompt.DiscoverUserRules(workspace, agentDir)
	skillPaths := agyprompt.AgySkillPaths(agentDir, m.session.cwd)
	if dir := pigasusskills.SkillsDir(configdir.Dir()); dir != "" {
		skillPaths = append(skillPaths, dir)
	}
	opts.Skills = agyprompt.DiscoverSkills(skillPaths)
	opts.Sandboxed = cliflags.BoolValue(m.session.flags, "sandbox")
	opts.PlanningMode = (m.session.mode == permissions.ModePlan)
	opts.Persona = m.Persona()
	opts.ProductSkin = m.ProductSkin()
	if m.session.agentBody == "" {
		opts.Subagents = append(opts.Subagents, setup.CustomSubagents()...)
	}
	return opts
}

// Persona reports the active onboarding persona profile for this session.
func (m *RootModel) Persona() string {
	if m == nil {
		return personas.PersonaEngineering
	}
	if m.session.persona != "" {
		return m.session.persona
	}
	if m.session.productSkin == personas.ProductSkinNonTechnical {
		return personas.PersonaKnowledgeWork
	}
	return personas.PersonaEngineering
}

// ProductSkin reports the active product skin tier (1: DEVELOPER, 2: NON_TECHNICAL).
func (m *RootModel) ProductSkin() int {
	if m == nil {
		return personas.ProductSkinDeveloper
	}
	if m.session.productSkin != 0 {
		return m.session.productSkin
	}
	if pInfo, ok := personas.Resolve(m.Persona()); ok {
		return pInfo.Skin
	}
	return personas.ProductSkinDeveloper
}

// IsKnowledgeWork reports whether the session operates under Knowledge Work mode.
func (m *RootModel) IsKnowledgeWork() bool {
	return m.ProductSkin() == personas.ProductSkinNonTechnical || m.Persona() == personas.PersonaKnowledgeWork
}

// SetPersona sets the active persona, synchronizing skin tier and persistence.
func (m *RootModel) SetPersona(name string) (personas.PersonaInfo, bool) {
	pInfo, ok := personas.Resolve(name)
	if !ok {
		return personas.PersonaInfo{}, false
	}
	m.session.persona = pInfo.ID
	m.session.productSkin = pInfo.Skin
	settings.SetString("persona", pInfo.ID)
	settings.SetString("productSkin", personas.SkinWire(pInfo.Skin))
	settings.SetField("project4kMode", pInfo.Skin)
	if js, err := auth.LoadJetboxState(); err == nil {
		js.ProductSkin = personas.SkinWire(pInfo.Skin)
		js.OnboardingPersonaID = pInfo.ID
		_ = auth.SaveJetboxState(js)
	}
	return pInfo, true
}

// SetProductSkin sets the active product skin tier, synchronizing persona and persistence.
func (m *RootModel) SetProductSkin(skin int) bool {
	if skin != personas.ProductSkinDeveloper && skin != personas.ProductSkinNonTechnical {
		return false
	}
	m.session.productSkin = skin
	if skin == personas.ProductSkinNonTechnical && (m.session.persona == "" || m.session.persona == personas.PersonaEngineering) {
		m.session.persona = personas.PersonaKnowledgeWork
	} else if skin == personas.ProductSkinDeveloper && m.session.persona == personas.PersonaKnowledgeWork {
		m.session.persona = personas.PersonaEngineering
	}
	settings.SetString("productSkin", personas.SkinWire(skin))
	settings.SetField("project4kMode", skin)
	if js, err := auth.LoadJetboxState(); err == nil {
		js.ProductSkin = personas.SkinWire(skin)
		_ = auth.SaveJetboxState(js)
	}
	return true
}

// promptSectionTokens splits the reference prompt into the three
// categories 1.4.3's /context panel reports separately.
func (m *RootModel) promptSectionTokens() (base, skills, subagents int) {
	opts := m.promptOptions()
	if m.session.agentBody != "" {
		full := agyprompt.BuildAgent(m.session.agentBody, opts)
		return len(full) / 3, 0, 0
	}
	full := agyprompt.Build(opts)
	skills = agyprompt.SectionTokenCount("skills", agyprompt.SectionBody(opts, "skills"))
	subagents = agyprompt.SectionTokenCount("subagents", agyprompt.SectionBody(opts, "subagents"))
	base = (len(full) - skills*3 - subagents*3) / 3
	if base < 0 {
		base = 0
	}
	return base, skills, subagents
}

// ---- tui_update_stream.go ----

func (m *RootModel) resetDiscardedTurn() {
	m.conversation.discardTurn = false
	m.conversation.liveSteps = nil
	m.conversation.liveStepRuns = nil
	m.conversation.activeTool = ""
	m.conversation.streaming = false
	m.conversation.thinking = false
	m.conversation.liveThinking = ""
	m.conversation.liveThinkingSnippet = ""
	m.conversation.liveThoughtRunIdx = -1
	m.viewport.activeTip = ""
	m.conversation.replyStart = -1
	m.conversation.liveReplyIdx = -1
	m.conversation.liveTextLines = 0
	m.conversation.liveReplyBuf = ""
	m.conversation.lastStreamSave = time.Time{}
	m.conversation.streamedText = false
	m.conversation.liveBatch = 0
	m.conversation.batchBreak = false
}

func (m *RootModel) dequeueNextPrompt() string {
	var queued []string
	if m.session.store.QueuedPrompt != "" {
		queued = append(queued, m.session.store.QueuedPrompt)
		m.session.store.QueuedPrompt = ""
	}
	if len(m.session.store.StreamQueue) > 0 {
		queued = append(queued, m.session.store.StreamQueue...)
		m.session.store.StreamQueue = nil
	}
	if len(queued) == 0 {
		return ""
	}
	if m.session.conversationID != "" {
		_ = convstore.SaveQueuedPrompts(m.session.conversationID, nil)
	}
	return strings.Join(queued, "\n")
}

func (m *RootModel) recordTurnCompletion(msg toolsteps.TurnDoneMsg) {
	m.conversation.liveSteps = nil
	m.conversation.liveStepRuns = nil
	m.conversation.activeTool = ""
	m.conversation.streaming = false
	m.conversation.thinking = false
	m.conversation.liveThinking = ""
	m.conversation.liveThinkingSnippet = ""
	m.conversation.liveThoughtRunIdx = -1
	m.viewport.activeTip = ""
	m.conversation.replyStart = -1
	m.conversation.liveTextLines = 0
	m.conversation.liveReplyBuf = ""
	m.conversation.voiceReplyStart = -1
	m.conversation.voiceReplyBuf = ""
	m.conversation.voiceTextLines = 0
	m.conversation.steerReplyPrefix = ""
	m.session.store.SendingMessages = nil
	m.conversation.lastStreamSave = time.Time{}
	m.conversation.streamedText = false
	m.conversation.liveBatch = 0
	m.conversation.batchBreak = false
	// The turn's outcome is a fact the session rests in: the status
	// line's `agent_state` member reports it ("error" after a failed
	// turn) and the terminal's own program status reads the same fact.
	//
	// A turn the user cancelled with Esc is not a failure: the
	// cancellation is the turn's own outcome.
	if msg.Err != nil && !m.conversation.turnInterrupted {
		m.recordTurnOutcome(tea.ProgramStateError)
	} else {
		m.recordTurnOutcome(tea.ProgramStateDone)
	}

	lastMsg, hasLast := m.session.lastMessage()
	isCompactedResumption := m.session.messageCount() >= 1 && hasLast &&
		lastMsg.Role == "user" &&
		contextpanel.IsResumeBlock(lastMsg.Content)

	if !agentturn.IsEmptyAssistantTurn(msg.Reply, nil, msg.Reasoning) {
		if m.conversation.liveReplyIdx >= 0 && m.conversation.liveReplyIdx < m.session.messageCount() {
			m.session.setMessageContent(m.conversation.liveReplyIdx, msg.Reply)
		} else {
			m.session.appendMessage(backend.Message{Role: "assistant", Content: msg.Reply})
		}
	}
	if m.conversation.compactHappenedThisTurn {
		m.session.store.CompactJustHappened = true
		m.conversation.compactHappenedThisTurn = false
		m.conversation.compactBlockIdx = len(m.conversation.blocks)
		m.conversation.compactStepIdx = 0
	} else {
		m.session.store.CompactJustHappened = false
	}
	// The turn's tool calls join the trajectory before the save: the
	// calls are what the rehydration pass reads back.
	m.recordTrajectoryCalls(msg.Steps)
	m.flushPendingCheckpoints()
	// A turn the user cancelled with Esc leaves no error row behind, except when
	// canceled in the pre-send window which aborts the send and shows
	// "Message not sent: canceled before it was sent" (AGY 1.5.14).
	if msg.Err != nil {
		clilog.Logf("tui", "turn failed: %v", msg.Err)
		if strings.Contains(msg.Err.Error(), "canceled before it was sent") {
			m.conversation.response = append(m.conversation.response, toolsteps.ErrorRow("Message not sent: canceled before it was sent"))
		} else if !m.conversation.turnInterrupted {
			if message, ok := agentturn.TurnErrorMessage(msg.Err); ok {
				m.conversation.response = append(m.conversation.response, toolsteps.ErrorRow(message))
				if logPath := clilog.Path(); logPath != "" {
					m.conversation.response = append(m.conversation.response, cDimGrayOpen+"  ⎿  See /logs or "+logPath+cDimGrayClose)
				}
			}
		}
	}
	m.conversation.replyStart = -1
	m.conversation.liveReplyIdx = -1
	m.saveSession()
	if isCompactedResumption {
		savedSteps := convstore.BuildSteps(m.trajectoryFacts())
		if sup := m.session.sup(); sup != nil {
			sup.SetSeededConversation(savedSteps, m.session.messageCount())
		}
	}
	if len(m.conversation.response) > 0 {
		m.conversation.response = append(m.conversation.response, "")
	}
}

func (m *RootModel) flushNextQueued(nextQueued string) tea.Cmd {
	if nextQueued == "" {
		if len(m.session.store.BashQueue) > 0 {
			next := m.session.store.BashQueue[0]
			m.session.store.BashQueue = m.session.store.BashQueue[1:]
			return m.startBashCommand(next)
		}
		// A background task that finished while the turn was running has
		// a notification to deliver. The reference's agent picks it up
		// and carries on without waiting for the user.
		if m.hasPendingTaskNotifications() {
			return m.startNotificationTurn()
		}
		return nil
	}
	if handled, cmd := m.dispatchSlashLine(nextQueued); handled {
		return cmd
	}
	if m.isLiveVoiceActive() {
		return m.startLiveVoiceTurn(nextQueued)
	}
	m.drainTaskNotifications()
	for len(m.conversation.response) > 0 && strings.TrimSpace(m.conversation.response[len(m.conversation.response)-1]) == "" {
		m.conversation.response = m.conversation.response[:len(m.conversation.response)-1]
	}
	sep := transcript.TurnSeparator(m.viewport.width)
	if len(m.conversation.response) == 0 {
		m.conversation.response = append(m.conversation.response, sep)
	} else {
		m.conversation.response = append(m.conversation.response, "", sep)
	}
	start := len(m.conversation.response)
	promptLines := transcript.RenderUserPrompt(nextQueued, m.viewport.width)
	m.conversation.response = append(m.conversation.response, promptLines...)
	m.conversation.userBlocks = append(m.conversation.userBlocks, transcript.UserBlock{
		Start: start,
		Lines: len(promptLines),
		Raw:   nextQueued,
	})
	m.conversation.liveStart = len(m.conversation.response)
	m.conversation.replyStart = -1
	m.conversation.liveReplyIdx = -1
	m.conversation.liveSteps = nil
	m.conversation.liveStepRuns = nil
	m.conversation.activeTool = ""
	m.conversation.thinking = false
	m.conversation.thinkingSince = time.Time{}
	m.conversation.thinkingElapsed = 0
	m.conversation.toolSeq = 0
	m.conversation.phaseTimer.reset()
	m.conversation.turnStartedAt = time.Now()
	m.conversation.lastQuotaRefreshAt = time.Time{}
	m.conversation.quotaRefreshInFlight = false
	m.conversation.stepTray.reset()
	m.conversation.liveTextLines = 0
	m.conversation.liveReplyBuf = ""
	m.conversation.voiceReplyStart = -1
	m.conversation.voiceReplyBuf = ""
	m.conversation.voiceTextLines = 0
	m.conversation.steerReplyPrefix = ""
	m.conversation.lastStreamSave = time.Time{}
	m.conversation.streamedText = false
	m.conversation.liveBatch = 0
	m.conversation.batchBreak = false
	m.conversation.anyToolRun = false
	m.conversation.spinFrame = 0
	m.appendUserTurn(nextQueued)
	m.saveSession()
	m.conversation.streaming = true
	m.conversation.turnInterrupted = false
	m.clearLastTurnOutcome()
	tickCmd := func() tea.Msg { return m.conversation.spinner.Tick() }
	return tea.Batch(m.streamPrompt(nextQueued), tickCmd)
}
