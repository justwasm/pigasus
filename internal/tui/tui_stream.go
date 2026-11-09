package tui

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	sp "charm.land/bubbles/v2/spinner"
	tas "charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/justwasm/pigasus/actor"
	"github.com/justwasm/pigasus/agentlink"
	"github.com/justwasm/pigasus/internal/app/agenttools"
	"github.com/justwasm/pigasus/internal/app/agentturn"
	"github.com/justwasm/pigasus/internal/app/artifacts"
	"github.com/justwasm/pigasus/internal/app/prompt/agyprompt"
	"github.com/justwasm/pigasus/internal/app/session"
	"github.com/justwasm/pigasus/internal/app/session/convstore"
	"github.com/justwasm/pigasus/internal/app/session/setup"
	browsersubagent "github.com/justwasm/pigasus/internal/app/subagent/browser"
	builtinsubagent "github.com/justwasm/pigasus/internal/app/subagent/builtin"
	subagentquestions "github.com/justwasm/pigasus/internal/app/subagent/questions"
	"github.com/justwasm/pigasus/internal/app/subagenttools"
	taskregistry "github.com/justwasm/pigasus/internal/app/task"
	"github.com/justwasm/pigasus/internal/app/toolrunner"
	"github.com/justwasm/pigasus/internal/app/tools"
	"github.com/justwasm/pigasus/internal/app/tools/browser"
	"github.com/justwasm/pigasus/internal/app/tools/shell"
	"github.com/justwasm/pigasus/internal/app/tools/surface"
	"github.com/justwasm/pigasus/internal/app/tools/tasks"
	"github.com/justwasm/pigasus/internal/app/tuistore"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/core/cliflags"
	"github.com/justwasm/pigasus/internal/core/compaction"
	confhome "github.com/justwasm/pigasus/internal/core/confhome"
	"github.com/justwasm/pigasus/internal/core/interaction"
	"github.com/justwasm/pigasus/internal/core/policy/permissions"
	"github.com/justwasm/pigasus/internal/core/policy/sandbox"
	"github.com/justwasm/pigasus/internal/core/settings"
	"github.com/justwasm/pigasus/internal/core/slashparse"
	accounts "github.com/justwasm/pigasus/internal/features/accounts/core"
	"github.com/justwasm/pigasus/internal/features/auth"
	"github.com/justwasm/pigasus/internal/features/checkpoint"
	modelcatalog "github.com/justwasm/pigasus/internal/features/models/catalog"
	"github.com/justwasm/pigasus/internal/features/plugins/hooks"
	backendselect "github.com/justwasm/pigasus/internal/infra/backend/select"
	"github.com/justwasm/pigasus/internal/ui/editor/composer"
	"github.com/justwasm/pigasus/internal/ui/editor/keybind"
	"github.com/justwasm/pigasus/internal/ui/foundations/history"
	"github.com/justwasm/pigasus/internal/ui/mdrender"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	"github.com/justwasm/pigasus/internal/ui/resumepicker"
	uirows "github.com/justwasm/pigasus/internal/ui/rows"
	"github.com/justwasm/pigasus/internal/ui/slashcommands"
	"github.com/justwasm/pigasus/internal/ui/toolsteps"
	"github.com/justwasm/pigasus/internal/ui/transcript"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/internal/ui/tuitools"
	"github.com/justwasm/pigasus/internal/ui/views/loginui"
	"github.com/justwasm/pigasus/internal/ui/views/modelargpopup"
	"github.com/justwasm/pigasus/internal/ui/views/modelpicker"
	"github.com/justwasm/pigasus/internal/ui/views/statusrow"
	"github.com/justwasm/pigasus/internal/ui/views/themepreview"
	"github.com/justwasm/pigasus/internal/ui/views/usagepanel"
	"github.com/justwasm/pigasus/palette"
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
		Effort:                  m.currentEffortName(),
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

	// thoughtStart is when the first thinking delta arrived, and
	// compactStart when the compaction began.
	thoughtStart time.Time
	compactStart time.Time

	// thoughtSent records that the thought row was already opened.
	thoughtSent bool

	// meta is the turn's token accounting.
	meta *backend.ChunkMeta

	// err is the turn's terminal failure.
	err error
}

// text renders a text delta.
func (c *turnCollector) text(s string) {
	if s == "" {
		return
	}
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
		if c.prog != nil {
			c.prog.Send(backendChunkMsg(n.Text))
		}
	case agentlink.NoticeSteerApplied:
		if c.prog != nil {
			c.prog.Send(steerAppliedMsg{instruction: n.Text})
		}
	}
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

	return func() tea.Msg {
		defer endTurn()
		if unlock != nil {
			defer unlock()
		}
		return bridge.runTurnOn(turnCtx, model, msgs)
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
		summary, err := auth.FetchQuotaSummary(ctx)
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
	opts.Skills = agyprompt.DiscoverSkills(agyprompt.AgySkillPaths(agentDir, m.session.cwd))
	opts.PlanningMode = (m.session.mode == permissions.ModePlan)
	if m.session.agentBody == "" {
		// The browser agent is one of the built-in subagent types, and it
		// comes before the user's own agents: the reference's list puts
		// its built-ins first (the 1.4.6 capture lists self, research and
		// image-generator ahead of the user's alpha and zeta).
		opts.Subagents = append(opts.Subagents, browsersubagent.Subagent())
		opts.Subagents = append(opts.Subagents, setup.CustomSubagents()...)
	}
	return opts
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
	isCompactedResumption := m.session.messageCount() == 1 && hasLast &&
		lastMsg.Role == "user" &&
		strings.HasPrefix(lastMsg.Content, "# Resuming from a compaction")

	if !agentturn.IsEmptyAssistantTurn(msg.Reply, nil, msg.Reasoning) {
		// If a compaction just replaced the session with a ResumingBlock during this turn,
		// the assistant's reply was already captured in the compacted summary.
		if !isCompactedResumption {
			if m.conversation.liveReplyIdx >= 0 && m.conversation.liveReplyIdx < m.session.messageCount() {
				m.session.setMessageContent(m.conversation.liveReplyIdx, msg.Reply)
			} else {
				m.session.appendMessage(backend.Message{Role: "assistant", Content: msg.Reply})
			}
		}
	}
	// The turn's tool calls join the trajectory before the save: the
	// calls are what the rehydration pass reads back.
	m.recordTrajectoryCalls(msg.Steps)
	m.flushPendingCheckpoints()
	// A turn the user cancelled with Esc leaves no error row behind. The
	// reference answers a cancelled turn with the composer's interrupted
	// hint instead (【反编译】 agy 1.4.8's `(*PromptModel).interruptedLine`),
	// and the turn's own failure is the cancellation itself.
	if msg.Err != nil && !m.conversation.turnInterrupted {
		if message, ok := agentturn.TurnErrorMessage(msg.Err); ok {
			m.conversation.response = append(m.conversation.response, toolsteps.ErrorRow(message))
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
	m.conversation.liveTextLines = 0
	m.conversation.liveReplyBuf = ""
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

// ---- tui_rootmodel_agent.go ----

func pickerHiddenCommands() []string {
	if !auth.IsBusinessAccount() {
		return nil
	}
	return []string{"/remote-control"}
}

func (m *RootModel) Init() tea.Cmd {
	cmds := []tea.Cmd{tas.Blink, armTasksTick(), m.fetchPlanTierCmd()}
	if m.statusLineScriptActive() || m.statusLine.titleCfg.Active() {
		m.session.store.StatusLineTickArmed = true
		cmds = append(cmds, armExternalStatusLineTick())
	}
	return tea.Batch(cmds...)
}

func (m *RootModel) cycleMode() {
	next := permissions.NextMode(m.session.mode)
	m.session.mode = next
	if m.session.policy != nil {
		m.session.policy.Mode = next
	}
	m.prompt.input.Placeholder = permissions.ModeBanner(next)
	m.prompt.modeHintDismissed = false
}

func (m *RootModel) enableTools(decls []backend.ToolDecl, runner backend.ToolRunner) {
	m.tools.decls = decls
	m.tools.runner = runner
}

func (m *RootModel) moveWord(back bool) bool {
	seams := m.composerSeams()
	if back {
		return composer.HandleWordLeft(seams)
	}
	return composer.HandleWordRight(seams)
}

func agentToolRegistry(workspace, agentBody string) *tools.Registry {
	catalog := surface.RuntimeCatalog(workspace)
	if agentBody != "" {
		return agenttools.Registry(catalog)
	}
	return catalog
}

func (m *RootModel) registerSubagentQuestionTool(catalog *tools.Registry) {
	if m.session.agentBody != "" {
		return
	}
	catalog.Register(&tasks.AskQuestion{
		Host:              m.turnInteractionHost(),
		SubAgentQuestions: m.askQuestion.manager,
		SubAgentID:        subagentquestions.ParentKey,
		OnAsk:             m.requestAskQuestion,
	})
}

// requestAskQuestion sends the open request to the running program.
//
// The tool calls this from the turn's goroutine. With no program (unit
// tests, print mode) there is no UI to open, so it is a no-op.
func (m *RootModel) requestAskQuestion(scope string) {
	if m == nil || m.turn.prog == nil {
		return
	}
	m.turn.prog.Send(askQuestionPendingMsg{scope: scope})
}

func (m *RootModel) applyAgentSwitch(name, body string) {
	m.saveSession()
	m.session.agentName = name
	m.session.agentBody = body
	workspace := m.session.cwd
	if workspace == "" {
		workspace, _ = os.Getwd()
	}
	if m.session.policy != nil {
		catalog := agentToolRegistry(workspace, body)
		m.registerSubagentQuestionTool(catalog)
		m.enableTools(toolrunner.AdvertisedToolDecls(body, catalog), &permissionRunner{
			inner:   &toolrunner.RegistryRunner{Reg: catalog},
			policy:  m.session.policy,
			sandbox: m.tools.sandbox,
			hook:    m.hookToolGate,
			post:    m.hookPostToolUse,
		})
	}
	m.conversation.response = nil
	m.session.bumpEpoch()
	if sup := m.session.sup(); sup != nil {
		sup.Clear()
	}
	m.conversation.blocks = nil
	m.conversation.liveSteps = nil
	m.conversation.activeTool = ""
	m.session.autoTitle = ""
	m.session.conversationID = newConversationID()
	// The conversation changed, so the session did too: the documented
	// SessionStart event fires for the new one.
	m.session.hookSessionStarted = false
	// The definitions belong to the conversation that just ended.
	m.agents.defs = subagenttools.NewStore()
	m.resetTrajectory()
}

// tuiSilenceLogger is the destination for the recovered subagent debug-log lines.
var tuiSilenceLogger = log.New(io.Discard, "", 0)

func (m *RootModel) subAgentExtras(_ context.Context, be backend.Client, modelLabel string) ([]actor.Tool, func()) {
	if be == nil || m.session.agentBody != "" {
		return nil, func() {}
	}
	workspace := m.session.cwd
	if workspace == "" {
		workspace = defaultWorkspaceForRootModel()
	}
	agentDir := confhome.Antigravity()
	provider := agentturn.NewBackendProviderAdapter(be, modelLabel)
	catalogue := allCatalogActorTools(m.tools.catalog)
	if len(catalogue) == 0 && m.tools.catalog != nil {
		catalogue = browsersubagent.BrowserTools(m.tools.catalog)
	}
	subDeps := subagenttools.Deps{
		Provider:          provider,
		Backend:           be,
		Model:             modelLabel,
		Effort:            m.currentEffortName(),
		CWD:               workspace,
		AgentDir:          agentDir,
		Definitions:       m.agents.defs,
		Tasks:             m.tasks.reg,
		Registry:          m.ensureSubagentRegistry(),
		ConversationID:    m.session.conversationID,
		PermissionGateFor: m.subagentGateFactory,
		SystemPrompt:      m.systemPrompt,
		Catalogue:         catalogue,
		Questions:         m.askQuestion.manager,
		OnAsk:             m.requestAskQuestion,
	}
	subTools := subagenttools.Tools(subDeps)
	subDir := filepath.Join(agentDir, "subagents")
	_, _ = subagenttools.RehydrateSubagents(subDir, subDeps.Registry, m.tasks.reg, subDeps)
	if m.tools.catalog != nil {
		browser.SetBrowserSubagentProvider(browsersubagent.New(subagenttools.Deps{
			Provider: provider,
			Backend:  be,
			Model:    modelLabel,
			CWD:      workspace,
			AgentDir: agentDir,
		}, browsersubagent.Tools(m.tools.catalog)))
		reg := builtinsubagent.Default()
		if err := browsersubagent.InstallBuiltin(reg, subDeps, m.tools.catalog); err != nil {
			tuiSilenceLogger.Printf("[tui] browser built-in: %v", err)
		} else {
			subagenttools.SetBuiltinSubagentRegistry(reg)
		}
	}
	return subTools, func() {}
}

func defaultWorkspaceForRootModel() string {
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return ""
}

func allCatalogActorTools(reg *tools.Registry) []actor.Tool {
	if reg == nil {
		return nil
	}
	var out []actor.Tool
	for _, name := range reg.List() {
		tool, ok := reg.Get(name)
		if !ok || tool == nil {
			continue
		}
		var params map[string]any
		desc := tool.Name()
		if raw := tool.Schema(); len(raw) > 0 {
			_ = json.Unmarshal(raw, &params)
			if d, ok := params["description"].(string); ok && d != "" {
				desc = d
			}
		}
		t := tool
		out = append(out, actor.NewFuncTool(t.Name(), desc, func(ctx context.Context, input string) (string, error) {
			res, err := t.Run(ctx, json.RawMessage(input))
			if err != nil {
				return "", err
			}
			if s, ok := res.(string); ok {
				return s, nil
			}
			b, _ := json.Marshal(res)
			return string(b), nil
		}, actor.WithParameters(params)))
	}
	return out
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

func resolveRootModelDefaults(b backend.Client, email, model, tier, cwd string) (string, string, string, string) {
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
	if model != "" && !backendselect.IsReferenceModel(model, modelpicker.ResolveCatalogueID) {
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

func newRootModelBase(b backend.Client, email, model, tier, cwd string, altScreen bool) *RootModel {
	initConvID := newConversationID()
	domainSess := session.New(session.Config{
		ID:    initConvID,
		Model: model,
		CWD:   cwd,
	})
	m := &RootModel{
		session: sessionState{
			domain:                domainSess,
			email:                 email,
			model:                 model,
			statusModel:           tuilib.StripTier(model),
			tier:                  tier,
			cwd:                   cwd,
			store:                 *tuistore.New(),
			conversationID:        initConvID,
			initialConversationID: initConvID,
			narrationStart:        time.Now(),
			wireSettingsPending:   true,
			hooks:                 hooks.NewManager(),
		},
		turn: TurnService{
			backend: b,
		},
		display: DisplayModel{
			streamingSearch: true,
			verbosity:       settings.String("verbosity"),
		},
		agents: AgentsModel{
			defs:     domainSess.SubagentDefs(),
			registry: domainSess.SubagentRegistry(),
			router:   defaultRouter,
		},
		tasks: TasksModel{
			reg:             tools.SharedTaskRegistry(),
			ctrl:            taskregistry.NewController(tools.SharedTaskRegistry()),
			subagentManager: tools.SharedSubagentManager(),
			inbox:           agentturn.NewTaskInbox(),
		},
		viewport: uiState{
			altScreen:          altScreen,
			autoScroll:         true,
			rowsCacheDirty:     true,
			pinnedPromptRowIdx: -1,
		},
		overlays: OverlayModel{
			activeBtwIndex: -1,
		},
		conversation: ConversationModel{
			replyStart:        -1,
			liveReplyIdx:      -1,
			liveThoughtRunIdx: -1,
		},
		askQuestion: AskQuestionModel{
			manager: subagentquestions.NewManager(false),
		},
	}
	m.prompt.picker = m.newSlashPicker()
	m.activeItems.Panel = m.newActiveItemsPanel()
	m.prompt.atFilePicker = m.newMentionPicker()
	m.overlays.resumePicker = resumepicker.New(resumepicker.Seams{
		CurrentConversationID: func() string { return m.session.conversationID },
		CurrentWorkspace:      func() string { return m.session.cwd },
		ListConversations:     m.listResumeConversations,
		ResumeConversation:    m.resumeConversation,
		RenameConversation:    m.renameResumeConversation,
	})
	m.askQuestion.panel = m.newSubAgentQuestionPanel()
	m.help.Panel = m.newHelpPanel()
	m.audio.Surface = m.newVoiceSurface()
	return m
}

func initRootModelInput(m *RootModel) {
	m.prompt.input = tas.New()
	m.prompt.input.SetVirtualCursor(false)
	m.prompt.input.Prompt = inputPrompt
	m.prompt.input.SetPromptFunc(2, func(info tas.PromptInfo) string {
		if info.LineNumber == 0 {
			if m.session.store.BashMode {
				return "! "
			}
			return "> "
		}
		return "  "
	})
	m.prompt.input.Placeholder = ""
	m.prompt.input.ShowLineNumbers = false
	m.prompt.input.SetWidth(inputWidth)
	m.prompt.input.SetHeight(1)
	m.prompt.input.MaxHeight = 24
	m.prompt.input.CharLimit = 0
	m.prompt.input.EndOfBufferCharacter = ' '
	m.prompt.input.DynamicHeight = true
	m.prompt.input.MinHeight = 1

	// The prompt wears the selected scheme's accent and the placeholder
	// its dim foreground; the colours follow the scheme, so they are
	// resolved from it rather than hard-coded.
	initScheme := settings.String("colorScheme")
	// The session wears the configured scheme from the first frame, so
	// the whole interface is painted in it — the reference resolves
	// every colour from the selected scheme.
	m.viewport.colorScheme = initScheme
	promptColor, placeholderColor := themepreview.ColorsFor(initScheme)
	promptBlueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(promptColor))
	placeholderStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(placeholderColor))
	st := m.prompt.input.Styles()
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
	m.prompt.input.SetStyles(st)
	m.prompt.input.Focus()
	if initScheme != "" {
		m.applyThemeLive(initScheme)
	}
}

// newRootModel constructs the v1.3.5 binary 1:1 run mode.
func newRootModel(b backend.Client, email, model, tier, cwd string, altScreen bool) *RootModel {
	pickerHidden := pickerHiddenCommands()
	email, model, tier, cwd = resolveRootModelDefaults(b, email, model, tier, cwd)
	m := newRootModelBase(b, email, model, tier, cwd, altScreen)
	if settings.String("queuedMessages") == "send-immediately" {
		m.session.store.QueueMode = false
	} else {
		m.session.store.QueueMode = true
	}

	if kb, err := keybind.Load(keybind.DefaultPath()); err == nil {
		m.prompt.keybindings = kb
	}
	m.startTasksSubscription()
	initRootModelInput(m)

	m.conversation.spinner = sp.New(sp.WithSpinner(sp.Dot))
	// 【一手】 agy 1.4.7: with a seasonal event switched on the
	// streaming spinner is the event's own — the Halloween event shows
	// a four-frame moon phase ("◐  Generating…").
	m.refreshSeasonalTheme()
	if len(pickerHidden) > 0 {
		m.prompt.picker.SetHiddenCommands(pickerHidden...)
	}
	// The "/model <query>" popup filters the catalogue the CLI can
	// select: the reference's own models plus the ones the user
	// configured.
	m.prompt.picker.SetModelCatalogueSource(modelargpopup.Rows)
	return m
}

// ---- tui_turn_blocks.go ----

// transcriptDocument hands the host's transcript state to the ui tier.
//
// The host owns the rows: the document is the shape the transcript's
// mechanics work on, and the host takes the result back through
// applyTranscriptDocument.
func (m *RootModel) transcriptDocument() transcript.Document {
	return transcript.Document{
		Lines:             m.conversation.response,
		Blocks:            m.conversation.blocks,
		Assistants:        m.conversation.assistants,
		UserBlocks:        m.conversation.userBlocks,
		Runs:              m.conversation.liveStepRuns,
		LiveStart:         m.conversation.liveStart,
		ReplyStart:        m.conversation.replyStart,
		LiveThoughtRunIdx: m.conversation.liveThoughtRunIdx,
		LiveTextLines:     m.conversation.liveTextLines,
		LiveReplyBuf:      m.conversation.liveReplyBuf,
		StreamedText:      m.conversation.streamedText,
	}
}

// applyTranscriptDocument takes the transcript's state back.
func (m *RootModel) applyTranscriptDocument(doc transcript.Document) {
	m.conversation.response = doc.Lines
	m.conversation.blocks = doc.Blocks
	m.conversation.assistants = doc.Assistants
	m.conversation.userBlocks = doc.UserBlocks
	m.conversation.liveStepRuns = doc.Runs
	m.conversation.liveStart = doc.LiveStart
	m.conversation.replyStart = doc.ReplyStart
	m.conversation.liveThoughtRunIdx = doc.LiveThoughtRunIdx
	m.conversation.liveTextLines = doc.LiveTextLines
	m.conversation.liveReplyBuf = doc.LiveReplyBuf
	m.conversation.streamedText = doc.StreamedText
}

// transcriptOptions are the host's own render facts, as the transcript's
// mechanics see them.
func (m *RootModel) transcriptOptions(msg toolsteps.TurnDoneMsg) transcript.Options {
	opts := m.repaintOptions()
	opts.EmptyTurn = agentturn.IsEmptyAssistantTurn(msg.Reply, nil, msg.Reasoning)
	return opts
}

// repaintOptions are the host's own render facts, as the transcript's
// repaint engine sees them.
//
// The viewport's height is handed over as a lookup rather than as a
// number: the engine asks for it only when it has to decide how many of
// the blocks it repaints, and the host's answer is a cached
// measurement.
func (m *RootModel) repaintOptions() transcript.Options {
	verb := m.currentVerbosity()
	return transcript.Options{
		Width:          m.viewport.width,
		Height:         m.viewport.height,
		Verbosity:      verb,
		Expanded:       m.effectiveExpanded(verb),
		ViewportHeight: m.viewportHeight,
		Streaming:      m.conversation.streaming,
		Thought: transcript.LiveThought{
			Active: m.conversation.thinking,
			Text:   m.conversation.liveThinking,
			Since:  m.conversation.thinkingSince,
		},
	}
}

// toggleTrajectoryExpanded flips the trajectory-wide expanded flag,
// clears the scrollback by snapping the viewport to the active turn,
// and repaints the tool-step blocks under the active verbosity.
func (m *RootModel) toggleTrajectoryExpanded() {
	m.viewport.expandedUserToggled = true
	m.viewport.expanded = !m.isExpanded()
	m.viewport.scroll = 0
	m.viewport.autoScroll = true
	m.viewport.rowsCacheDirty = true
	m.viewport.viewCacheValid = false
	m.rerenderBlocks()
	m.rerenderLiveSteps()
}

// rerenderBlocks repaints the transcript's tool-step blocks under the
// active verbosity and expansion.
func (m *RootModel) rerenderBlocks() {
	m.viewport.rowsCacheDirty = true
	doc := m.transcriptDocument()
	doc.RerenderBlocks(m.repaintOptions())
	m.applyTranscriptDocument(doc)
}

// ensureVisibleBlocksExpanded repaints the blocks inside the visible
// window, so a block that scrolls into view is drawn under the current
// verbosity and expansion.
func (m *RootModel) ensureVisibleBlocksExpanded(avail int) {
	doc := m.transcriptDocument()
	if doc.EnsureVisibleBlocksExpanded(m.repaintOptions(), avail, m.viewport.scroll, m.viewport.autoScroll) {
		m.viewport.rowsCacheDirty = true
		m.viewport.viewCacheValid = false
	}
	m.applyTranscriptDocument(doc)
}

// rerenderTranscriptForTheme repaints all user prompts, assistant replies,
// and tool step blocks across m.conversation.response under the active colour scheme.
//
// 【反编译】 agy 1.4.8 model.(*ConversationModel).handleSettingsChanges
// calls store.(*Manager).RerenderAll when the colour scheme changes,
// repainting the entire transcript with the new scheme's colours.
func (m *RootModel) rerenderTranscriptForTheme() {
	if m == nil {
		return
	}
	m.viewport.rowsCacheDirty = true

	doc := m.transcriptDocument()
	doc.RerenderForTheme(m.repaintOptions())
	m.applyTranscriptDocument(doc)

	// Invalidate view caches so viewport redraws completely
	m.viewport.rowsCacheDirty = true
	m.viewport.rowsCache = wrapSlot{}
	m.viewport.rowsView = composedRows{}
}

// reflowTranscript re-renders and re-wraps the entire transcript (markdown replies,
// tool step blocks, turn separators, in-flight streaming replies) when the terminal width changes.
//
// 【反编译】 agy 1.4.12 ConversationModel.handleResizeRedraw (0x102bcbb70)
// calls buildRenderedItems (0x102bbc2d0) on terminal resize, recalculating and
// re-rendering all message items to reflow properly for the new column width.
func (m *RootModel) reflowTranscript(width int) {
	if m == nil || width <= 0 {
		return
	}
	m.viewport.width = width
	mdrender.SetCurrentWidth(width)
	m.viewport.rowsCacheDirty = true
	m.viewport.viewCacheValid = false
	m.viewport.extraRowsValid = false

	doc := m.transcriptDocument()
	doc.Reflow(m.repaintOptions())
	m.applyTranscriptDocument(doc)

	// Invalidate viewport caches and rebuild wraps for the new width.
	m.viewport.rowsCacheDirty = true
	m.viewport.rowsCache = wrapSlot{}
	m.viewport.rowsCacheN1 = wrapSlot{}
	m.viewport.rowsView = composedRows{}
	m.viewport.viewCacheValid = false
	m.viewport.cachedHeaderKey = headerCacheKey{}
}

// syncLiveThoughtStep paints the turn's live thought row.
func (m *RootModel) syncLiveThoughtStep() {
	doc := m.transcriptDocument()
	if doc.SyncLiveThoughtStep(m.repaintOptions()) {
		m.viewport.rowsCacheDirty = true
	}
	m.applyTranscriptDocument(doc)
}

// finishLiveThinking closes the turn's live thought: the running row
// becomes the finished thought.
func (m *RootModel) finishLiveThinking() {
	if !m.conversation.thinking {
		return
	}
	// The options are taken while the thought is still live: the
	// finished thought is painted from the thought that was running.
	opts := m.repaintOptions()
	m.conversation.ClearThinking()

	doc := m.transcriptDocument()
	if doc.FinishLiveThinking(opts) {
		m.viewport.rowsCacheDirty = true
	}
	m.applyTranscriptDocument(doc)
}

// rerenderLiveSteps repaints the in-flight step rows: the live thought
// and the tool batches the model is still running.
func (m *RootModel) rerenderLiveSteps() {
	doc := m.transcriptDocument()
	if doc.RerenderLiveSteps(m.repaintOptions()) {
		m.viewport.rowsCacheDirty = true
	}
	m.applyTranscriptDocument(doc)
}

// appendCompactResult renders the completion of a PiG-driven
// compaction. The reference (agy 1.4.5) draws a centred
// "Conversation compacted" rule plus a centred stats line instead of a
// transcript bullet; the same two rows are used here.
//
// PiG's end event only carries the token counts, so the stats line
// omits the step count and the summary size the reference reports
// (both are passed as "absent"). See
// docs/cycles/auto-compact-parity/captures.md.
func (m *RootModel) appendCompactResult(msg compactDoneMsg) {
	if msg.Err != nil {
		m.conversation.response = append(m.conversation.response,
			uirows.NoticeCompact("Compact failed: "+msg.Err.Error()),
			"",
		)
		return
	}
	stats := compaction.StatsRow(m.viewport.width, 0, msg.TokensBefore, msg.TokensAfter, -1, -1)
	if msg.Aborted {
		stats += " (aborted)"
	}
	m.conversation.response = append(m.conversation.response,
		compaction.DividerRow(m.viewport.width),
		stats,
		"",
	)
}

var separator = palette.Rule(separatorWidth)

// separator returns the horizontal rule drawn between the banner and the
// prompt. Cycle 119: when the terminal width is known the rule is drawn at
// that width rather than the fixed 220 columns, so it never wraps.
func (m *RootModel) separator() string {
	if m.viewport.width > 0 {
		return palette.Rule(m.viewport.width)
	}
	return separator
}

// promptRule returns the rule that frames the prompt box.
//
// 【一手】 agy 1.4.7: with a seasonal event switched on the prompt box's
// two rules become the event's own — a gradient rule with face motifs in
// it (out/changelog-147/ref/seasonal-prompt.txt) — while the rule that
// separates the queued messages from the transcript stays plain.
func (m *RootModel) promptRule(top bool) string {
	if m.viewport.seasonal == nil {
		return m.separator()
	}
	return m.viewport.seasonal.Border.Rule(m.viewport.width, top)
}

const inputPrompt = "> "

// inputWidth is the textarea render width.
const inputWidth = 219

// statusRow is the status area's single row: the custom script's output
// when one is active, the built-in template otherwise.
func (m *RootModel) statusRow() string {
	if out, _ := m.externalRenderedForRow(); out != "" {
		return out
	}
	return statusrow.Render(m.statusRowState())
}

// ---- tui_task_notify.go ----

// tui_task_notify.go — cycle 309: the delivery of a background task's
// notification into the turn loop.
//
// The reference tells the model that a background task is over: the
// notification is an agent message, and the agent picks it up and carries
// on with its work. The clone's seam is the same, in three pieces:
//
//   - the tools layer fires the notification when the command is gone
//     (internal/tools/support.go);
//   - the inbox holds it until the next turn (internal/agent/task_inbox.go);
//   - the TUI drains the inbox into the session and starts a turn of its
//     own when the agent is idle.
//
// The inbox lives on the run mode, not on the agent.Loop: the TUI builds a
// fresh Loop for every turn, so a per-loop queue would drop every
// notification that arrives between turns — which is when a background task
// finishes.
//
// The transcript's own rows for a notification are the ui tier's
// (internal/ui/rows.NotificationRows); this file owns the delivery, not
// the shape of the rows that show it.

// taskNotificationMsg carries the fact that a task notification is waiting.
//
// The payload is not in the message: the notification is in the inbox, and
// the handler drains it. One message per notification would race the
// inbox's own ordering.
type taskNotificationMsg struct{}

// installTaskNotifier wires the tools layer's notification hook to the
// session's inbox.
//
// The hook is process-wide (internal/shell.SetTaskNotifier), so the last
// run mode to install it owns the notifications. That is the TUI's case:
// there is one run mode per process.
func (m *RootModel) installTaskNotifier() {
	if m.tasks.ctrl != nil {
		shell.SetTaskNotifier(func(n shell.TaskNotification) {
			m.tasks.ctrl.Inbox().Add(taskregistry.TaskNotification{
				TaskID:   n.TaskID,
				Title:    n.Title,
				Body:     n.Body,
				LogPath:  n.LogPath,
				Priority: n.Priority,
				At:       n.At,
			})
			if m.turn.prog != nil {
				m.turn.prog.Send(taskNotificationMsg{})
			}
		})
		return
	}
	if m.tasks.inbox == nil {
		m.tasks.inbox = agentturn.NewTaskInbox()
	}
	shell.SetTaskNotifier(func(n shell.TaskNotification) {
		m.tasks.inbox.Add(n)
		if m.turn.prog != nil {
			m.turn.prog.Send(taskNotificationMsg{})
		}
	})
}

// hasPendingTaskNotifications reports whether any background task or subagent
// notification is waiting to be processed.
func (m *RootModel) hasPendingTaskNotifications() bool {
	if m.tasks.ctrl != nil && m.tasks.ctrl.Inbox().Len() > 0 {
		return true
	}
	if m.tasks.inbox != nil && m.tasks.inbox.Len() > 0 {
		return true
	}
	return false
}

// updateTaskNotificationMsg handles a task notification that arrived.
//
// The notification is drained into the session by the next turn's message
// builder. When the agent is idle, the TUI starts a turn right away: the
// reference's agent picks the message up and carries on with its work
// without waiting for the user to type something.
func (m *RootModel) updateTaskNotificationMsg() tea.Cmd {
	if !m.hasPendingTaskNotifications() {
		return nil
	}
	if m.conversation.streaming {
		// A turn is in flight: the notification waits for the next one.
		// The turn's end starts a turn of its own for it.
		return nil
	}
	if next := m.dequeueNextPrompt(); next != "" {
		return m.flushNextQueued(next)
	}
	return m.startNotificationTurn()
}

// startNotificationTurn starts a turn that hands the pending task
// notifications to the model.
func (m *RootModel) startNotificationTurn() tea.Cmd {
	m.conversation.liveStart = len(m.conversation.response)
	m.conversation.replyStart = -1
	m.conversation.liveReplyIdx = -1
	m.conversation.liveSteps = nil
	m.conversation.liveStepRuns = nil
	m.conversation.activeTool = ""
	m.conversation.thinking = false
	m.conversation.liveTextLines = 0
	m.conversation.liveReplyBuf = ""
	m.conversation.streamedText = false
	m.conversation.liveBatch = 0
	m.conversation.batchBreak = false
	m.conversation.anyToolRun = false
	m.conversation.spinFrame = 0
	m.conversation.streaming = true
	m.clearLastTurnOutcome()
	tickCmd := func() tea.Msg { return m.conversation.spinner.Tick() }
	return tea.Batch(m.streamPrompt(""), tickCmd)
}

// takeTaskNotifications removes the pending notifications from the inbox and
// returns them, oldest first.
func (m *RootModel) takeTaskNotifications() []shell.TaskNotification {
	var out []shell.TaskNotification
	if m.tasks.ctrl != nil {
		for _, n := range m.tasks.ctrl.Inbox().Drain() {
			out = append(out, shell.TaskNotification{
				TaskID:   n.TaskID,
				Title:    n.Title,
				Body:     n.Body,
				LogPath:  n.LogPath,
				Priority: n.Priority,
				At:       n.At,
			})
		}
	}
	if m.tasks.inbox != nil {
		out = append(out, m.tasks.inbox.Drain()...)
	}
	return out
}

// drainTaskNotifications moves the pending notifications into the session
// and renders them in the transcript.
//
// The rows go in before the turn's live block: the drain happens while the
// turn is being set up, so the notification belongs to the conversation
// before the model's answer to it. If a user turn was already appended to
// the session, the notifications are placed BEFORE the user turn so the user
// prompt remains the active request.
func (m *RootModel) drainTaskNotifications() {
	items := m.takeTaskNotifications()
	if len(items) == 0 {
		return
	}
	rows := 0
	trailingUser := false
	var userMsg backend.Message
	if last, ok := m.session.lastMessage(); ok && last.Role == "user" {
		trailingUser = true
		userMsg = last
		m.session.truncateMessages(m.session.messageCount() - 1)
	}

	for _, item := range items {
		msg := agentturn.TaskNotificationMessage(item)
		m.session.appendMessage(msg)
		lines := uirows.NotificationRows(item)
		if trailingUser && len(m.conversation.userBlocks) > 0 {
			ub := m.conversation.userBlocks[len(m.conversation.userBlocks)-1]
			idx := ub.Start
			if idx >= 0 && idx <= len(m.conversation.response) {
				m.conversation.response = append(m.conversation.response[:idx], append(lines, m.conversation.response[idx:]...)...)
				m.conversation.userBlocks[len(m.conversation.userBlocks)-1].Start += len(lines)
			} else {
				m.conversation.response = append(m.conversation.response, lines...)
			}
		} else {
			m.conversation.response = append(m.conversation.response, lines...)
		}
		rows += len(lines)
	}

	if trailingUser {
		m.session.appendMessage(userMsg)
	}

	// The turn's live block starts after the notification's rows.
	if m.conversation.liveStart >= 0 && m.conversation.liveStart+rows <= len(m.conversation.response) {
		m.conversation.liveStart += rows
	}
}

// ---- tui_subagent_async.go ----

// tui_subagent_async.go — the TUI's side of the asynchronous subagent.
//
// The reference's `invoke_subagent` does not wait: it creates the
// subagent's conversation, sends it the task, and returns an
// acknowledgement. The subagent runs in the background and reports back as
// a message to the parent — the same `<SYSTEM_MESSAGE>` envelope a
// background task's completion notification uses.
//
// 【一手】 the 1.4.4 live capture (docs/cycles/subagent-async-framing-capture/deliverable.md):
// the acknowledgement names the new conversation, and the delivery arrives
// as a user-role content part in the parent's *next* invocation.
//
// Three pieces make that work in the TUI:
//
//   - the registry, which holds the live children (the conversation's own
//     scope: "Manage the direct subagents of this conversation");
//   - the notifier, which puts a child's message into the session's inbox;
//   - the permission gate, which judges the child's tool calls under the
//     conversation's one policy.
func (m *RootModel) ensureSubagentRegistry() *subagenttools.Registry {
	if m.agents.registry != nil && m.agents.registry.ParentID() == m.session.conversationID {
		m.agents.registry.SetNotify(m.notifySubagentMessage)
		return m.agents.registry
	}
	if m.agents.registry != nil {
		// The conversation changed: the old children belong to it.
		m.agents.registry.Close()
	}
	m.agents.registry = subagenttools.NewRegistry(m.session.conversationID)
	m.agents.registry.SetNotify(m.notifySubagentMessage)
	if dom := m.DomainSession(); dom != nil {
		dom.SetSubagentRegistry(m.agents.registry)
	}
	return m.agents.registry
}

// notifySubagentMessage hands a subagent's message to the session.
//
// It travels the way a background task's notification travels: into the
// session's inbox, which the next turn drains into the conversation. The
// text the model reads is the reference's own envelope, with the
// subagent's conversation id as the sender.
func (m *RootModel) notifySubagentMessage(sender, content string) {
	if m.tasks.ctrl != nil {
		m.tasks.ctrl.Inbox().Add(taskregistry.TaskNotification{
			TaskID:   sender,
			Body:     content,
			Priority: shell.TaskPriorityHigh,
			At:       time.Now(),
		})
		if m.turn.prog != nil {
			m.turn.prog.Send(taskNotificationMsg{})
		}
		return
	}
	if m.tasks.inbox == nil {
		m.tasks.inbox = agentturn.NewTaskInbox()
	}
	m.tasks.inbox.Add(shell.TaskNotification{
		TaskID:   sender,
		Body:     content,
		Priority: shell.TaskPriorityHigh,
		At:       time.Now(),
	})
	if m.turn.prog != nil {
		m.turn.prog.Send(taskNotificationMsg{})
	}
}

// subagentPermissionGate judges a subagent's tool calls.
//
// The policy is the conversation's one policy — the same *permissions.Policy
// the parent's own calls go through — so a rule the user wrote for the
// parent governs the subagent too. The call is judged under both of the
// tool's names (the catalogue's and the declaration's), because the
// subagent calls the catalogue's spelling while the parent calls the
// declaration's.
//
// The sandbox is the parent's, with the subagent's own worktree added: a
// subagent that runs in its own directory must be allowed to work in it.
type subagentPermissionGate struct {
	policy   *permissions.Policy
	sandbox  sandbox.Sandbox
	worktree string
}

// Decide reports whether the call may run.
func (g *subagentPermissionGate) Decide(_ context.Context, toolName string, args json.RawMessage) subagenttools.GateDecision {
	sb := g.sandbox
	if sb.Enabled && g.worktree != "" {
		extra := make([]string, 0, len(sb.ExtraPaths)+1)
		extra = append(extra, sb.ExtraPaths...)
		extra = append(extra, g.worktree)
		sb.ExtraPaths = extra
	}
	decision := toolrunner.Decide(g.policy, sb, backend.ToolCall{Name: toolName, Arguments: args})
	return subagenttools.GateDecision{Allow: decision.Allow, Reason: decision.Reason}
}

// subagentGateFactory builds the gate for a subagent that runs in the given
// directory.
func (m *RootModel) subagentGateFactory(worktree string) subagenttools.BeforeToolCallGate {
	return &subagentPermissionGate{
		policy:   m.session.policy,
		sandbox:  m.tools.sandbox,
		worktree: worktree,
	}
}

// ---- tui_prompt_submit.go ----

// slashCommandRegex matches any leading slash command (agy 1.4.12 at 0x3680ea0).
var slashCommandRegex = regexp.MustCompile(`^/([a-zA-Z0-9_-]*)(?:\s|\n|$)`)

// PrependSlashPlanIfNeeded mirrors agy's model.PrependSlashPlanIfNeeded (0x102be4660).
// When the session is in plan mode, if the user prompt does not start with any slash command,
// it prepends "/plan " to the input.
func PrependSlashPlanIfNeeded(prompt string, mode string) string {
	if mode != permissions.ModePlan {
		return prompt
	}
	trimmed := strings.TrimSpace(prompt)
	if slashCommandRegex.MatchString(trimmed) {
		return prompt
	}
	return "/plan " + prompt
}

func (m *RootModel) dispatchSlashLine(buf string) (bool, tea.Cmd) {
	if !strings.HasPrefix(buf, "/") {
		return false, nil
	}
	// Cycle multi-account-switch: the account may have been switched
	// outside the TUI (the `agy accounts switch` command, a sign-in in
	// another terminal). Re-read the shared credential before the
	// command runs, so a panel that shows the account (/usage) shows the
	// current one.
	syncCmd := m.syncAccountIdentity()
	if cmdName, cmdArgs := slashparse.Split(buf); m.openSlashPanelArgs(cmdName, cmdArgs) {
		m.prompt.input.Reset()
		if m.prompt.undoMgr != nil {
			m.prompt.undoMgr.Clear()
		}
		m.ResetInputSelection()
		m.prompt.promptHistory.Reset()
		m.prompt.picker.Reset()
		m.prompt.atFilePicker.Reset()
		if m.overlays.panel.kind == panelregistry.Usage && m.viewport.height > 0 {
			m.display.quotaFrame = 0
			return true, tea.Batch(syncCmd, usagepanel.QuotaTick(), m.fetchQuotaSummaryCmd())
		}
		if m.overlays.panel.kind == panelregistry.Accounts {
			// The panel queries every account's quota, so the user
			// does not have to open /usage for the numbers.
			return true, tea.Batch(syncCmd, m.accountsPanel().QueueQuota())
		}
		if m.overlays.panel.kind == panelregistry.CodeSearch && m.viewport.height > 0 {
			// Cycle 381: the panel owns the tick; the host only arms it.
			return true, tea.Batch(syncCmd, m.codesearchPanel().Tick())
		}
		return true, syncCmd
	}
	if handled, cmd := m.handleSlashCommand(buf); handled {
		return true, tea.Batch(syncCmd, cmd)
	}
	return false, nil
}

func (m *RootModel) submitBashMode(cmd string) (bool, tea.Cmd) {
	m.session.store.BashMode = false
	m.prompt.input.Reset()
	if m.prompt.undoMgr != nil {
		m.prompt.undoMgr.Clear()
	}
	m.ResetInputSelection()
	m.prompt.promptHistory.Reset()
	m.prompt.picker.Reset()
	m.prompt.atFilePicker.Reset()
	m.prompt.modeHintDismissed = true
	if history.IsBlank(cmd) {
		return true, nil
	}
	m.pushHistory("!" + cmd)
	if m.conversation.streaming {
		// A running *shell* command is preempted: the reference stops it,
		// keeps what it printed, and runs the new one immediately (【一手】
		// `!top` then `!pwd`). Only a model turn queues — there the command
		// waits behind the turn and shows as `▸ <cmd>`.
		if m.session.store.BashRunning() {
			return true, m.preemptBashCommand(cmd)
		}
		m.session.store.BashQueue = append(m.session.store.BashQueue, cmd)
		return true, nil
	}
	return true, m.startBashCommand(cmd)
}

// submitVimBashMode runs a prompt that begins with "!" as a shell
// command. The reference's NORMAL-mode "!" inserts the character and
// switches to INSERT mode, so the buffer carries the "!" and the submit
// path is what turns it into a shell command.
//
// The check is scoped to vim mode on purpose: outside it the "!" key
// arms the bash mode directly (and the buffer never carries the "!"),
// and a prompt that merely starts with "!" is an ordinary prompt.
func (m *RootModel) submitVimBashMode(prompt string) (tea.Cmd, bool) {
	if !m.vimEnabled() || !strings.HasPrefix(prompt, "!") {
		return nil, false
	}
	_, cmd := m.submitBashMode(strings.TrimPrefix(prompt, "!"))
	return cmd, true
}

func (m *RootModel) popActiveSteer() (string, bool) {
	if m.turn.activeBridge != nil {
		if msg, ok := m.turn.activeBridge.PopSteer(); ok {
			return msg, true
		}
	}
	if sc, ok := m.turnEngine().(agentturn.SteerController); ok {
		return sc.PopSteer()
	}
	return "", false
}

func (m *RootModel) steerActiveTurn(instruction string) bool {
	instruction = strings.TrimSpace(instruction)
	if instruction == "" {
		return false
	}
	steered := false
	if m.turn.activeBridge != nil {
		if err := m.turn.activeBridge.Steer(instruction); err == nil {
			steered = true
		}
	}
	if !steered {
		if s, ok := m.turnEngine().(agentturn.Steerer); ok {
			if err := s.Steer(instruction); err == nil {
				steered = true
			}
		}
	}
	if !steered {
		return false
	}

	// In agy (0x102a5e230 addSendingMessage):
	// The active step continues generating cleanly without interruption.
	// Record in SendingMessages to render the "▸ <instruction> (Sending message...)" row.
	m.session.store.SendingMessages = append(m.session.store.SendingMessages, instruction)
	m.viewport.rowsCacheDirty = true
	m.viewport.rowsGen++
	return true
}

func (m *RootModel) applySteerToTranscript(instruction string) {
	instruction = strings.TrimSpace(instruction)
	if instruction == "" {
		return
	}

	// Reconcile SendingMessages: remove the applied instruction
	if len(m.session.store.SendingMessages) > 0 {
		idx := -1
		for i, s := range m.session.store.SendingMessages {
			if s == instruction {
				idx = i
				break
			}
		}
		if idx >= 0 {
			m.session.store.SendingMessages = append(m.session.store.SendingMessages[:idx], m.session.store.SendingMessages[idx+1:]...)
		} else {
			m.session.store.SendingMessages = m.session.store.SendingMessages[1:]
		}
	}

	// Deduplicate: do not commit the same instruction if already committed as the latest user block
	if len(m.conversation.userBlocks) > 0 && m.conversation.userBlocks[len(m.conversation.userBlocks)-1].Raw == instruction {
		return
	}

	// Finalize live thinking & tool on preceding step
	if m.conversation.thinking {
		m.finishLiveThinking()
	}
	// Remember the answer streamed before this steer. TurnDone's Reply is
	// the whole turn, but the transcript region below the prompt is only
	// the suffix; finalising with the whole reply would repeat the
	// pre-steer answer (AGY 1.5.3).
	m.conversation.steerReplyPrefix += m.conversation.liveReplyBuf
	m.conversation.activeTool = ""
	m.conversation.replyStart = -1
	m.conversation.liveReplyIdx = -1
	m.conversation.liveTextLines = 0
	m.conversation.liveReplyBuf = ""

	// Commit user prompt bubble into transcript after the finished step
	for len(m.conversation.response) > 0 && strings.TrimSpace(m.conversation.response[len(m.conversation.response)-1]) == "" {
		m.conversation.response = m.conversation.response[:len(m.conversation.response)-1]
	}
	m.conversation.response = append(m.conversation.response, "")
	start := len(m.conversation.response)
	promptLines := transcript.RenderUserPrompt(instruction, m.viewport.width)
	m.conversation.response = append(m.conversation.response, promptLines...)
	m.conversation.userBlocks = append(m.conversation.userBlocks, transcript.UserBlock{
		Start: start,
		Lines: len(promptLines),
		Raw:   instruction,
	})
	m.appendUserTurn(instruction)
	m.conversation.liveStart = len(m.conversation.response)
	m.conversation.liveBatch++
	m.conversation.batchBreak = true
	m.viewport.rowsCacheDirty = true
	m.viewport.rowsGen++
	m.saveSession()
}

func (m *RootModel) updateSteerAppliedMsg(msg steerAppliedMsg) {
	m.applySteerToTranscript(msg.instruction)
}

// steerReplySegments splits a finished turn's reply at the last applied
// steer. The transcript below the steer prompt carries only the suffix;
// the pre-steer text is already above it. Both messages fall back to the
// full reply when no steer split this turn (or the actor's reply is not a
// superset of what was streamed).
func (m *RootModel) steerReplySegments(msg toolsteps.TurnDoneMsg) (docMsg, sessionMsg toolsteps.TurnDoneMsg) {
	prefix := m.conversation.steerReplyPrefix
	if prefix == "" {
		return msg, msg
	}
	suffix, ok := strings.CutPrefix(msg.Reply, prefix)
	if !ok {
		return msg, msg
	}
	docMsg, sessionMsg = msg, msg
	docMsg.Reply = suffix
	sessionMsg.Reply = suffix
	return docMsg, sessionMsg
}

// clearPendingTurnSteers drops steering instructions the actor still
// holds. They were accepted by the engine but the turn ended before a
// step boundary applied them; the TUI is about to send them as the next
// turn's prompt instead.
func (m *RootModel) clearPendingTurnSteers() {
	if m.turn.activeBridge != nil {
		m.turn.activeBridge.ClearSteer()
	}
	if clearer, ok := m.turnEngine().(interface{ ClearSteer() []string }); ok {
		clearer.ClearSteer()
	}
}

func (m *RootModel) checkStreamingQueue(prompt string) (bool, tea.Cmd) {
	if !m.conversation.streaming {
		return false, nil
	}
	if history.IsBlank(prompt) {
		m.prompt.input.Reset()
		m.ResetInputSelection()
		m.prompt.promptHistory.Reset()
		m.prompt.picker.Reset()
		return true, nil
	}

	// Move pending steer to queue: bare /queue or /unsteer
	if prompt == "/queue" || prompt == "/unsteer" {
		if msg, ok := m.popActiveSteer(); ok && msg != "" {
			m.session.store.StreamQueue = append(m.session.store.StreamQueue, msg)
			m.saveSession()
			m.prompt.input.Reset()
			m.ResetInputSelection()
			m.prompt.promptHistory.Reset()
			m.prompt.picker.Reset()
			m.prompt.atFilePicker.Reset()
			m.prompt.modeHintDismissed = true
			return true, nil
		}
	}

	// Explicit /queue <instruction> to force queueing even when QueueMode is off
	if strings.HasPrefix(prompt, "/queue ") {
		inst := strings.TrimSpace(strings.TrimPrefix(prompt, "/queue "))
		if inst != "" {
			m.pushHistory(prompt)
			m.session.store.StreamQueue = append(m.session.store.StreamQueue, inst)
			m.saveSession()
		}
		m.prompt.input.Reset()
		m.ResetInputSelection()
		m.prompt.promptHistory.Reset()
		m.prompt.picker.Reset()
		m.prompt.atFilePicker.Reset()
		m.prompt.modeHintDismissed = true
		return true, nil
	}

	// Promote queued prompt to steer if user inputs bare /steer
	if prompt == "/steer" {
		var inst string
		if m.session.store.QueuedPrompt != "" {
			inst = m.session.store.QueuedPrompt
			m.session.store.QueuedPrompt = ""
		} else if len(m.session.store.StreamQueue) > 0 {
			inst = m.session.store.StreamQueue[0]
			m.session.store.StreamQueue = m.session.store.StreamQueue[1:]
		}
		if inst != "" {
			m.saveSession()
			m.steerActiveTurn(inst)
			m.prompt.input.Reset()
			m.ResetInputSelection()
			m.prompt.promptHistory.Reset()
			m.prompt.picker.Reset()
			m.prompt.atFilePicker.Reset()
			m.prompt.modeHintDismissed = true
			return true, nil
		}
	}

	// Explicit /steer <instruction> command
	if strings.HasPrefix(prompt, "/steer ") {
		inst := strings.TrimSpace(strings.TrimPrefix(prompt, "/steer "))
		if inst != "" {
			m.pushHistory(prompt)
			m.steerActiveTurn(inst)
		}
		m.prompt.input.Reset()
		m.ResetInputSelection()
		m.prompt.promptHistory.Reset()
		m.prompt.picker.Reset()
		m.prompt.atFilePicker.Reset()
		m.prompt.modeHintDismissed = true
		return true, nil
	}

	// In Send Immediately mode (QueueMode is off): submitting plain text steers the active turn
	if !m.session.store.QueueMode && !strings.HasPrefix(prompt, "/") {
		m.pushHistory(prompt)
		if m.steerActiveTurn(prompt) {
			m.prompt.input.Reset()
			m.ResetInputSelection()
			m.prompt.promptHistory.Reset()
			m.prompt.picker.Reset()
			m.prompt.atFilePicker.Reset()
			m.prompt.modeHintDismissed = true
			return true, nil
		}
	}

	if !strings.HasPrefix(prompt, "/") || m.slashQueuesWhileStreaming(prompt) {
		m.pushHistory(prompt)
		m.session.store.StreamQueue = append(m.session.store.StreamQueue, prompt)
		m.viewport.rowsCacheDirty = true
		m.viewport.rowsGen++
		m.prompt.input.Reset()
		m.ResetInputSelection()
		m.prompt.promptHistory.Reset()
		m.prompt.picker.Reset()
		m.prompt.atFilePicker.Reset()
		m.prompt.modeHintDismissed = true
		return true, nil
	}
	return false, nil
}

func (m *RootModel) expandPastedPlaceholders(prompt string) string {
	if len(m.prompt.pastedBlocks) == 0 {
		return prompt
	}
	for placeholder, content := range m.prompt.pastedBlocks {
		if strings.Contains(prompt, placeholder) {
			prompt = strings.ReplaceAll(prompt, placeholder, content)
		}
	}
	return prompt
}

func (m *RootModel) resolveSubmittedPrompt() (string, bool) {
	rawPrompt := m.prompt.input.Value()
	if history.IsBlank(rawPrompt) {
		m.prompt.input.Reset()
		m.ResetInputSelection()
		m.prompt.promptHistory.Reset()
		m.prompt.picker.Reset()
		return "", false
	}
	m.overlays.announceQueue.HideForSession()
	prompt := rawPrompt
	if m.prompt.picker.Visible() {
		if sel := m.prompt.picker.Selected(); sel != nil && strings.IndexByte(prompt, ' ') < 0 && strings.IndexByte(prompt, '\t') < 0 {
			prompt = sel.Name
		}
	}
	// Expand folded [Pasted text #N ...] placeholders into the full pasted text for model submission.
	prompt = m.expandPastedPlaceholders(prompt)
	m.pushHistory(prompt)
	return prompt, true
}

func (m *RootModel) dispatchPromptPrefix(prompt string) (bool, tea.Cmd) {
	if handled, cmd := m.handlePromptAtPrefix(prompt); handled {
		return true, cmd
	}
	if handled, cmd := m.dispatchSlashLine(prompt); handled {
		return true, cmd
	}
	if flag.Lookup("test.v") == nil && !backendselect.HasOAuthToken() && os.Getenv("GEMINI_API_KEY") == "" && os.Getenv("AGY_ADC_AUTH") == "" {
		m.slashRespond("/login", "Authentication required. Please visit the URL to log in:")
		lm := loginui.New()
		lm.Width = m.viewport.width
		lm.Height = m.viewport.height
		m.auth.login = lm
		m.prompt.input.Reset()
		m.ResetInputSelection()
		return true, lm.Init()
	}
	return false, nil
}

func (m *RootModel) resetLiveTurnState(prompt string) {
	m.prompt.input.Reset()
	if m.prompt.undoMgr != nil {
		m.prompt.undoMgr.Clear()
	}
	m.prompt.pastedBlocks = nil
	m.prompt.nextPasteID = 0
	m.ResetInputSelection()
	m.prompt.atFilePicker.Reset()
	m.clearEligibilityNotice()
	m.conversation.streaming = true
	// A new turn replaces the interrupted one: the reference's composer
	// hint belongs to the turn that was cancelled, not to the next one.
	m.conversation.turnInterrupted = false
	m.prompt.promptHistory.Reset()
	m.clearLastTurnOutcome()
	if m.session.autoTitle == "" {
		m.session.autoTitle = convstore.GenerateConversationTitle(prompt)
	}
	for _, row := range uirows.AttachmentRows(m.prompt.pendingMedia, m.prompt.mediaExpanded) {
		m.conversation.response = append(m.conversation.response, row)
	}
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
	promptLines := transcript.RenderUserPrompt(prompt, m.viewport.width)
	m.conversation.response = append(m.conversation.response, promptLines...)
	m.conversation.userBlocks = append(m.conversation.userBlocks, transcript.UserBlock{
		Start: start,
		Lines: len(promptLines),
		Raw:   prompt,
	})
	m.conversation.liveStart = len(m.conversation.response)
	m.conversation.replyStart = -1
	m.conversation.liveReplyIdx = -1
	m.conversation.liveSteps = nil
	m.conversation.liveStepRuns = nil
	m.conversation.activeTool = ""
	m.conversation.thinking = false
	m.conversation.liveThinking = ""
	m.conversation.liveThinkingSnippet = ""
	m.conversation.liveThoughtRunIdx = -1
	showTips := true
	if v, ok := settings.Bool("showTips"); ok {
		showTips = v
	}
	if showTips {
		m.viewport.activeTip = tuitools.RandomTip()
	} else {
		m.viewport.activeTip = ""
	}
	m.conversation.liveTextLines = 0
	m.conversation.liveReplyBuf = ""
	m.conversation.steerReplyPrefix = ""
	m.conversation.lastStreamSave = time.Time{}
	m.conversation.streamedText = false
	m.conversation.liveBatch = 0
	m.conversation.batchBreak = false
	m.conversation.anyToolRun = false
	m.conversation.spinFrame = 0
	m.prompt.picker.Reset()
	m.session.store.ResetCompaction()
}

func (m *RootModel) startModelTurn(prompt string) tea.Cmd {
	// Cycle multi-account-switch: the turn runs as the account the
	// shared credential names right now, not the one the session
	// started with. The transport is rebuilt by the sync when the
	// account changed, and the stream command runs after this Update
	// returns — so it picks the rebuilt transport up.
	syncCmd := m.syncAccountIdentity()
	m.session.conversationStarted = true
	m.drainTaskNotifications()
	m.resetLiveTurnState(prompt)
	m.appendUserTurn(prompt)
	m.saveSession()
	tickCmd := func() tea.Msg { return m.conversation.spinner.Tick() }
	return tea.Batch(m.streamPrompt(prompt), tickCmd, syncCmd)
}

func (m *RootModel) submitPrompt() tea.Cmd {
	m.viewport.inputNotice = ""
	if m.session.store.BashMode {
		_, cmd := m.submitBashMode(m.expandPastedPlaceholders(m.prompt.input.Value()))
		return cmd
	}
	// In vim mode the engine put the "!" into the buffer itself — the
	// reference's NORMAL-mode "!" inserts the character and switches to
	// INSERT mode — so the shell command reaches the submit path with
	// the "!" still in front of it.
	if cmd, ok := m.submitVimBashMode(m.expandPastedPlaceholders(m.prompt.input.Value())); ok {
		return cmd
	}
	// A staged question answer wins over the streaming queue: the turn is
	// parked on the question, so the composer's text is the answer, not a
	// message for the next turn.
	if m.askQuestion.panel.SubmitAnswer() {
		return nil
	}
	if handled, cmd := m.checkStreamingQueue(m.expandPastedPlaceholders(m.prompt.input.Value())); handled {
		return cmd
	}
	// The "/model <query>" popup owns Enter while it is open: the
	// argument either names a model (the command runs) or the highlighted
	// row is completed into the composer. 【一手】 agy 1.4.6, live.
	if handled, cmd := m.submitModelCompletion(); handled {
		return cmd
	}
	prompt, ok := m.resolveSubmittedPrompt()
	if !ok {
		return nil
	}
	prompt = PrependSlashPlanIfNeeded(prompt, m.session.mode)
	if handled, cmd := m.dispatchPromptPrefix(prompt); handled {
		return cmd
	}
	return m.startModelTurn(prompt)
}

func (m *RootModel) appendUserTurn(text string) {
	msg := backend.Message{
		Role:        "user",
		Content:     text,
		WireContent: backend.WrapUserContent(text, m.session.model, m.session.wireSettingsPending, time.Now()),
	}
	// The images the user attached ride along with the turn (agy 1.4.9's
	// image pasting). The transports carry them as image parts, so the
	// model receives them the way the reference sends them.
	if parts := m.mediaImageParts(); len(parts) > 0 {
		msg.Images = parts
		m.prompt.pendingMedia = nil
		m.prompt.mediaExpanded = false
	}
	m.session.appendMessage(msg)
	m.session.wireSettingsPending = false
}

func (m *RootModel) isEngineSlashCommand(cmd string) bool {
	// /agent is a host command whose work the engine performs: it is
	// answered by the dispatcher and forwarded to the engine, so it is
	// not a plain host command.
	if cmd == "agent" {
		return true
	}
	name := cmd
	if canonical, ok := slashcommands.Canonical(cmd); ok {
		name = canonical
	}
	if m.dispatchesCommand(name) {
		return false
	}
	if _, ok := slashcommands.PanelFor(name); ok {
		return false
	}
	for _, dyn := range m.engineSlashCommands() {
		clean := strings.TrimPrefix(dyn.Name, "/")
		if strings.EqualFold(clean, cmd) {
			return true
		}
	}
	return m.turn.engine != nil
}

func (m *RootModel) slashQueuesWhileStreaming(buf string) bool {
	if cmds, _ := slashparse.ParseLeadingCommands(buf); len(cmds) > 1 {
		return true
	}
	cmd, _ := slashparse.Split(buf)
	// Cycle 383 (A-8): the list below names *commands*, so it is
	// stated in the canonical name. "/teamwork" is the alias of
	// "/teamwork-preview" and both queue behind the running turn.
	name := cmd
	if canonical, ok := slashcommands.Canonical(cmd); ok {
		name = canonical
	}
	switch name {
	case "continue", "plan", "goal", "learn", "teamwork-preview", "boost", "grill-me":
		return true
	}
	if _, ok := slashSkillLookup(cmd); ok {
		return true
	}
	if m.isEngineSlashCommand(cmd) {
		return true
	}
	return false
}

func (m *RootModel) abortTurnForLocalCommand() {
	if !m.conversation.streaming {
		return
	}
	m.conversation.discardTurn = true
	if m.turn.activeBridge != nil {
		m.turn.activeBridge.CancelTurn()
	}
	m.conversation.streaming = false
	m.conversation.thinking = false
}

func (m *RootModel) submitAlternate() tea.Cmd {
	if m.askQuestion.panel != nil && m.askQuestion.panel.SubmitAnswer() {
		return nil
	}
	prompt := m.prompt.input.Value()
	if history.IsBlank(prompt) {
		m.prompt.input.Reset()
		m.ResetInputSelection()
		m.prompt.promptHistory.Reset()
		m.prompt.picker.Reset()
		return nil
	}
	// Invert QueueMode temporarily for this submission
	orig := m.session.store.QueueMode
	m.session.store.QueueMode = !orig
	defer func() {
		m.session.store.QueueMode = orig
	}()
	return m.submitPrompt()
}

func (m *RootModel) submitOrQueuePrompt() tea.Cmd {
	return m.submitPrompt()
}

func (m *RootModel) saveSession() {
	var queued []string
	if m.session.store.QueuedPrompt != "" {
		queued = append(queued, m.session.store.QueuedPrompt)
	}
	if len(m.session.store.StreamQueue) > 0 {
		queued = append(queued, m.session.store.StreamQueue...)
	}
	if m.session.conversationID != "" {
		_ = convstore.SaveQueuedPrompts(m.session.conversationID, queued)
	}

	if sup := m.session.sup(); sup != nil {
		_ = sup.Save(m.session.conversationID, m.session.autoTitle)
		return
	}
	if m.session.conversationID == "" || m.session.messageCount() == 0 {
		return
	}
	steps := convstore.BuildSteps(m.trajectoryFacts())
	if len(steps) == 0 {
		return
	}
	if err := convstore.SaveConversation(m.session.conversationID, m.session.cwd, steps); err != nil {
		return
	}
	if m.session.autoTitle != "" {
		_ = convstore.WriteSummariesTitle(m.session.conversationID, m.session.autoTitle)
		_ = convstore.WriteCacheTitle(m.session.conversationID, m.session.autoTitle)
	}
}
