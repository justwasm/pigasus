package tui

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
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
	"github.com/justwasm/pigasus/internal/app/prompt/agyprompt"
	"github.com/justwasm/pigasus/internal/app/session/convstore"
	"github.com/justwasm/pigasus/internal/app/session/setup"
	"github.com/justwasm/pigasus/internal/app/session/supervisor"
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
	"github.com/justwasm/pigasus/internal/core/policy/permissions"
	"github.com/justwasm/pigasus/internal/core/policy/sandbox"
	"github.com/justwasm/pigasus/internal/core/settings"
	"github.com/justwasm/pigasus/internal/core/slashparse"
	accounts "github.com/justwasm/pigasus/internal/features/accounts/core"
	"github.com/justwasm/pigasus/internal/features/auth"
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
	"github.com/justwasm/pigasus/internal/ui/views/statusline"
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
func (m *runMode) prepareStreamTurnMessages() []backend.Message {
	// A notification that arrived after the turn was set up still has to
	// reach the model: it joins the session here.
	m.drainTaskNotifications()
	// A `!` command that finished since the last turn reaches the model
	// with this one.
	m.collectImplicitMessages()
	var msgs []backend.Message
	if m.supervisor != nil {
		msgs = m.supervisor.WireMessages()
	} else {
		msgs = make([]backend.Message, 0, len(m.implicitMsgs)+len(m.session))
		msgs = append(msgs, m.implicitMsgs...)
		msgs = append(msgs, m.session...)
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
func (m *runMode) turnEngine() agentturn.Engine {
	if m.engine != nil {
		return m.engine
	}
	m.engine = agentturn.NewLocal(m.localTurnEnv)
	return m.engine
}

// localTurnEnv builds the environment one local turn runs in.
//
// The environment is built per turn: the transport, the prompt and the
// tool set belong to the session, and the session changes them between
// two turns — the user switches the model, a plugin installs a tool, the
// workspace moves.
func (m *runMode) localTurnEnv(ctx context.Context, model string) (agentturn.Env, func()) {
	be := m.backend
	extras, closeSub := m.subAgentExtras(ctx, be, model)
	return agentturn.Env{
		Backend:      be,
		Model:        model,
		SystemPrompt: m.systemPrompt(),
		Tools:        m.tools,
		ToolRunner:   m.toolRunner,
		ExtraTools:   extras,
		Effort:       m.currentEffortName(),
	}, closeSub
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

// runTurn drives one turn and renders it.
//
// The engine is the turn's host: the local one runs the turn here, the
// remote one runs it on a server. Both answer with the same frames, so
// this function does not know which one it is talking to.
func (m *runMode) runTurn(ctx context.Context, msgs []backend.Message) tea.Msg {
	if tl, ok := m.turnLock.(interface {
		TryLock() bool
		Unlock()
	}); ok && tl != nil {
		if !tl.TryLock() {
			return toolsteps.TurnDoneMsg{
				Err: errors.New("a remote-control turn is currently running"),
			}
		}
		defer tl.Unlock()
	}

	bridge := NewEngineBridge(BridgeOptions{
		Engine:    m.turnEngine(),
		Program:   m.prog,
		PromptMgr: DefaultPermissionPrompts,
		OnUsage:   m.recordTurnUsage,
		OnFrame: func(f agentlink.Frame) {
			if rec, ok := m.inprocessDisp.(interface {
				RecordExternalTurnFrame(string, string, string, string, string, string, bool)
			}); ok && rec != nil {
				var toolName, toolArgs, toolResult string
				var toolErr bool
				if f.Tool != nil {
					toolName = f.Tool.Name
					toolArgs = f.Tool.Args
					toolResult = f.Tool.Result
					toolErr = f.Tool.Err
				}
				rec.RecordExternalTurnFrame(m.conversationID, string(f.Kind), f.Text, toolName, toolArgs, toolResult, toolErr)
			}
		},
		HookStart: m.hookSessionStart,
		HookPre:   m.hookPreInvocation,
		HookStop:  m.hookStop,
		HookPost:  m.hookPostInvocation,
	})
	m.activeBridge = bridge
	defer func() { m.activeBridge = nil }()
	return bridge.RunTurn(ctx, m.turnModel(), msgs)
}

// turnModel is the model the session's turns run under.
func (m *runMode) turnModel() string {
	if m.model != "" {
		return m.model
	}
	return "claude-4.5-sonnet"
}

func (m *runMode) streamPrompt(prompt string) tea.Cmd {
	// The messages are prepared on the Update goroutine: the preparation
	// drains the task inbox and the implicit trajectory, and both belong
	// to the session.
	msgs := m.prepareStreamTurnMessages()

	return func() tea.Msg {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		m.turnCancel = cancel
		defer func() { m.turnCancel = nil }()

		return m.runTurn(ctx, msgs)
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
func (m *runMode) fetchPlanTierCmd() tea.Cmd {
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
func (m *runMode) fetchQuotaSummaryCmd() tea.Cmd {
	cached := m.cachedQuota(m.email)
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
	TokensBefore         int
	EstimatedTokensAfter int
	Aborted              bool
}

// systemPrompt builds the Antigravity CLI system prompt for this run mode.
func (m *runMode) systemPrompt() string {
	opts := m.promptOptions()
	if m.agentBody != "" {
		return agyprompt.BuildAgent(m.agentBody, opts)
	}
	return agyprompt.Build(opts)
}

// promptOptions assembles the reference prompt inputs.
func (m *runMode) promptOptions() agyprompt.Options {
	workspace := m.cwd
	if workspace == "" {
		workspace = defaultWorkspaceForRunMode()
	}
	conversationID := m.conversationID
	if conversationID == "" {
		conversationID = newConversationID()
	}
	agentDir := confhome.Antigravity()
	opts := agyprompt.DefaultOptions(workspace, agentDir, conversationID)
	if m.flags != nil {
		for _, dir := range cliflags.GetterStringValues(m.flags, "add-dir") {
			if abs, err := filepath.Abs(dir); err == nil {
				opts.Workspaces = append(opts.Workspaces, abs)
			}
		}
	}
	opts.UserRules = agyprompt.DiscoverUserRules(workspace, agentDir)
	opts.Skills = agyprompt.DiscoverSkills(agyprompt.AgySkillPaths(agentDir, m.cwd))
	opts.PlanningMode = (m.mode == permissions.ModePlan)
	if m.agentBody == "" {
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
func (m *runMode) promptSectionTokens() (base, skills, subagents int) {
	opts := m.promptOptions()
	if m.agentBody != "" {
		full := agyprompt.BuildAgent(m.agentBody, opts)
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

func (m *runMode) resetDiscardedTurn() {
	m.discardTurn = false
	m.liveSteps = nil
	m.liveStepRuns = nil
	m.activeTool = ""
	m.streaming = false
	m.thinking = false
	m.liveThinking = ""
	m.liveThinkingSnippet = ""
	m.liveThoughtRunIdx = -1
	m.activeTip = ""
	m.replyStart = -1
	m.liveReplyIdx = -1
	m.liveTextLines = 0
	m.liveReplyBuf = ""
	m.lastStreamSave = time.Time{}
	m.streamedText = false
	m.liveBatch = 0
	m.batchBreak = false
}

func (m *runMode) dequeueNextPrompt() string {
	var queued []string
	if m.store.QueuedPrompt != "" {
		queued = append(queued, m.store.QueuedPrompt)
		m.store.QueuedPrompt = ""
	}
	if len(m.store.StreamQueue) > 0 {
		queued = append(queued, m.store.StreamQueue...)
		m.store.StreamQueue = nil
	}
	if len(queued) == 0 {
		return ""
	}
	if m.conversationID != "" {
		_ = convstore.SaveQueuedPrompts(m.conversationID, nil)
	}
	return strings.Join(queued, "\n")
}

func (m *runMode) recordTurnCompletion(msg toolsteps.TurnDoneMsg) {
	m.liveSteps = nil
	m.liveStepRuns = nil
	m.activeTool = ""
	m.streaming = false
	m.thinking = false
	m.liveThinking = ""
	m.liveThinkingSnippet = ""
	m.liveThoughtRunIdx = -1
	m.activeTip = ""
	m.replyStart = -1
	m.liveTextLines = 0
	m.liveReplyBuf = ""
	m.store.SendingMessages = nil
	m.lastStreamSave = time.Time{}
	m.streamedText = false
	m.liveBatch = 0
	m.batchBreak = false
	// The turn's outcome is a fact the session rests in: the status
	// line's `agent_state` member reports it ("error" after a failed
	// turn) and the terminal's own program status reads the same fact.
	//
	// A turn the user cancelled with Esc is not a failure: the
	// cancellation is the turn's own outcome.
	if msg.Err != nil && !m.turnInterrupted {
		m.recordTurnOutcome(tea.ProgramStateError)
	} else {
		m.recordTurnOutcome(tea.ProgramStateDone)
	}

	if !agentturn.IsEmptyAssistantTurn(msg.Reply, nil, msg.Reasoning) {
		if m.liveReplyIdx >= 0 && m.liveReplyIdx < len(m.session) {
			m.session[m.liveReplyIdx].Content = msg.Reply
			if m.supervisor != nil {
				m.supervisor.Replace(m.session)
			}
		} else {
			m.session = append(m.session, backend.Message{Role: "assistant", Content: msg.Reply})
			if m.supervisor != nil {
				m.supervisor.Append(backend.Message{Role: "assistant", Content: msg.Reply})
			}
		}
	}
	// The turn's tool calls join the trajectory before the save: the
	// calls are what the rehydration pass reads back.
	m.recordTrajectoryCalls(msg.Steps)
	// A turn the user cancelled with Esc leaves no error row behind. The
	// reference answers a cancelled turn with the composer's interrupted
	// hint instead (【反编译】 agy 1.4.8's `(*PromptModel).interruptedLine`),
	// and the turn's own failure is the cancellation itself.
	if msg.Err != nil && !m.turnInterrupted {
		if message, ok := agentturn.TurnErrorMessage(msg.Err); ok {
			m.response = append(m.response, toolsteps.ErrorRow(message))
		}
	}
	m.replyStart = -1
	m.liveReplyIdx = -1
	m.saveSession()
	if len(m.response) > 0 {
		m.response = append(m.response, "")
	}
}

func (m *runMode) flushNextQueued(nextQueued string) tea.Cmd {
	if nextQueued == "" {
		if len(m.store.BashQueue) > 0 {
			next := m.store.BashQueue[0]
			m.store.BashQueue = m.store.BashQueue[1:]
			return m.startBashCommand(next)
		}
		// A background task that finished while the turn was running has
		// a notification to deliver. The reference's agent picks it up
		// and carries on without waiting for the user.
		if m.taskInbox != nil && m.taskInbox.Len() > 0 {
			return m.startNotificationTurn()
		}
		return nil
	}
	if handled, cmd := m.dispatchSlashLine(nextQueued); handled {
		return cmd
	}
	m.drainTaskNotifications()
	for len(m.response) > 0 && strings.TrimSpace(m.response[len(m.response)-1]) == "" {
		m.response = m.response[:len(m.response)-1]
	}
	sep := transcript.TurnSeparator(m.width)
	if len(m.response) == 0 {
		m.response = append(m.response, sep)
	} else {
		m.response = append(m.response, "", sep)
	}
	start := len(m.response)
	promptLines := transcript.RenderUserPrompt(nextQueued, m.width)
	m.response = append(m.response, promptLines...)
	m.userBlocks = append(m.userBlocks, transcript.UserBlock{
		Start: start,
		Lines: len(promptLines),
		Raw:   nextQueued,
	})
	m.liveStart = len(m.response)
	m.replyStart = -1
	m.liveReplyIdx = -1
	m.liveSteps = nil
	m.liveStepRuns = nil
	m.activeTool = ""
	m.thinking = false
	m.liveTextLines = 0
	m.liveReplyBuf = ""
	m.lastStreamSave = time.Time{}
	m.streamedText = false
	m.liveBatch = 0
	m.batchBreak = false
	m.anyToolRun = false
	m.spinFrame = 0
	m.appendUserTurn(nextQueued)
	m.saveSession()
	m.streaming = true
	m.turnInterrupted = false
	m.clearLastTurnOutcome()
	tickCmd := func() tea.Msg { return m.spinner.Tick() }
	return tea.Batch(m.streamPrompt(nextQueued), tickCmd)
}

func (m *runMode) updateTurnDoneMsg(msg toolsteps.TurnDoneMsg) (tea.Model, tea.Cmd) {
	m.inputNotice = ""
	if m.discardTurn {
		m.resetDiscardedTurn()
		return m, tea.ClearScreen
	}
	// Promote any unapplied steer messages that arrived before turn completion into the queue
	// so they are executed in the next turn as promised by the architecture design.
	if len(m.store.SendingMessages) > 0 {
		pending := append([]string(nil), m.store.SendingMessages...)
		m.store.SendingMessages = nil
		for _, instr := range pending {
			m.store.StreamQueue = append(m.store.StreamQueue, instr)
		}
		m.saveSession()
	}
	nextQueued := m.dequeueNextPrompt()
	doc := m.transcriptDocument()
	if doc.FinaliseTurnReply(msg, m.transcriptOptions(msg)) {
		m.rowsCacheDirty = true
	}
	m.applyTranscriptDocument(doc)
	m.recordTurnCompletion(msg)

	// If /goal mode is active, check the outcome of this turn.
	if m.activeGoal != nil {
		completed, cancelled := m.activeGoal.CheckOutcome(msg.Reply)
		if completed || cancelled {
			m.activeGoal = nil
		} else {
			// If the user manually queued a prompt, that takes priority and pauses the goal loop.
			if nextQueued == "" {
				m.activeGoal.ContinuationCount++
				reminderPrompt := m.activeGoal.BuildReminderMessage()
				return m, tea.Batch(m.startModelTurn(reminderPrompt), tea.ClearScreen)
			}
		}
	}

	queueCmd := m.flushNextQueued(nextQueued)
	return m, tea.Batch(queueCmd, tea.ClearScreen)
}

func (m *runMode) updateToolRunningMsg(msg toolsteps.ToolRunningMsg) {
	if m.thinking {
		m.finishLiveThinking()
	}
	m.activeTool = tuitools.RunningSpinnerText(msg.Step.Name)
	m.anyToolRun = true
	m.streaming = true

	// The transcript owns the batch arithmetic: the running call either
	// joins the current batch or starts a new one, and the rows of the
	// batch are repainted. The host keeps the batch bookkeeping.
	step := msg.Step
	doc := m.transcriptDocument()
	batch, batchBreak := doc.AppendRunningStep(&step, m.liveBatch, m.batchBreak, m.repaintOptions())
	m.applyTranscriptDocument(doc)
	m.liveBatch = batch
	m.batchBreak = batchBreak
	m.rowsCacheDirty = true
}

func (m *runMode) updateToolStepMsg(msg toolsteps.ToolStepMsg) {
	if m.thinking {
		m.finishLiveThinking()
	}
	m.activeTool = ""
	m.replyStart = -1
	m.liveTextLines = 0
	m.liveReplyBuf = ""

	// The finished call takes the running call's place in the batch,
	// and the batch's rows are repainted with the block the batch now
	// holds. A call that had no running predecessor is appended as a
	// run of its own.
	step := msg.Step
	doc := m.transcriptDocument()
	opts := m.repaintOptions()
	if !doc.FinishLiveStep(&step, opts) {
		batch, batchBreak := doc.AppendLiveStep(&step, m.liveBatch, m.batchBreak, opts)
		m.liveBatch = batch
		m.batchBreak = batchBreak
	}
	m.applyTranscriptDocument(doc)
	m.liveSteps = append(m.liveSteps, step)
	m.rowsCacheDirty = true

	// If steer instructions were sent while this tool step was executing,
	// apply them now to the transcript at this tool step completion boundary!
	if len(m.store.SendingMessages) > 0 {
		pending := append([]string(nil), m.store.SendingMessages...)
		m.store.SendingMessages = nil
		for _, instr := range pending {
			m.applySteerToTranscript(instr)
		}
	}
}

func (m *runMode) updateBackendChunkMsg(msg backendChunkMsg) {
	if m.thinking {
		m.finishLiveThinking()
	}
	chunk := string(msg)
	if chunk == "" || m.discardTurn {
		return
	}
	// The cancelled turn's own stream error — the loop reports the
	// cancellation as a chunk — is not part of the answer either. The
	// reference's cancelled turn shows the composer's interrupted hint and
	// nothing else.
	if m.turnInterrupted && chunk == context.Canceled.Error() {
		return
	}
	if m.replyStart < 0 {
		m.response = transcript.BlankBefore(m.response)
		m.replyStart = len(m.response)
		m.liveTextLines = 0
		m.liveReplyBuf = ""
	}
	m.liveReplyBuf += chunk
	m.streamedText = true
	m.batchBreak = true
	if m.liveReplyIdx < 0 {
		m.session = append(m.session, backend.Message{Role: "assistant", Content: chunk})
		m.liveReplyIdx = len(m.session) - 1
		if m.supervisor != nil {
			m.supervisor.Append(backend.Message{Role: "assistant", Content: chunk})
		}
	} else {
		m.session[m.liveReplyIdx].Content += chunk
		if m.supervisor != nil {
			m.supervisor.Replace(m.session)
		}
	}
	styled := mdrender.RenderMarkdown(m.liveReplyBuf, m.width)
	lines := strings.Split(styled, "\n")
	m.response = transcript.ReplaceRange(m.response, m.replyStart, m.replyStart+m.liveTextLines, lines)
	m.liveTextLines = len(lines)
	m.streaming = true
	m.saveSessionStreamDebounced()
}

func (m *runMode) saveSessionStreamDebounced() {
	now := time.Now()
	// Persist the first chunk immediately so initial step is written to disk,
	// then throttle subsequent disk writes to at most once every 1.5 seconds.
	if m.lastStreamSave.IsZero() || now.Sub(m.lastStreamSave) >= 1500*time.Millisecond {
		m.saveSession()
		m.lastStreamSave = now
	}
}

func (m *runMode) updateStreamingMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case toolsteps.TurnDoneMsg:
		mod, cmd := m.updateTurnDoneMsg(msg)
		return mod, cmd, true
	case toolsteps.ToolRunningMsg:
		m.updateToolRunningMsg(msg)
		return m, nil, true
	case toolsteps.ThoughtStartMsg:
		if !m.thinking {
			m.thinking = true
			m.thinkingSince = time.Now()
			m.liveThinking = ""
			m.liveThinkingSnippet = ""
			m.liveThoughtRunIdx = -1
		}
		m.streaming = true
		return m, nil, true
	case toolsteps.ThoughtChunkMsg:
		if !m.thinking {
			m.thinking = true
			m.thinkingSince = time.Now()
		}
		m.streaming = true
		m.liveThinking += msg.Delta
		m.liveThinkingSnippet = toolsteps.ThinkingLiveSnippet(m.liveThinking)
		if m.isExpanded() {
			m.syncLiveThoughtStep()
		}
		return m, nil, true
	case autoContBannerMsg:
		banner := "↻  auto-continuing… (retry " + fmt.Sprintf("%d", msg.depth) + " of " + fmt.Sprintf("%d", msg.maxDepth) + ")"
		m.response = append(m.response, banner)
		if m.streaming {
			m.streaming = false
		}
		return m, nil, true
	case eligibilityNoticeMsg:
		m.setEligibilityNotice(msg.Reason, msg.VerificationURL)
		return m, nil, true
	case toolsteps.ToolStepMsg:
		m.updateToolStepMsg(msg)
		return m, nil, true
	case taskNotificationMsg:
		// A background task finished. When the agent is idle the
		// notification is delivered in a turn of its own; when a turn is
		// in flight it waits for the next one.
		return m, m.updateTaskNotificationMsg(), true
	case browserAgentDoneMsg:
		// The browser agent behind the /browser slash command answered.
		m.updateBrowserAgentDoneMsg(msg)
		return m, nil, true
	case backendChunkMsg:
		m.updateBackendChunkMsg(msg)
		return m, nil, true
	case steerAppliedMsg:
		m.updateSteerAppliedMsg(msg)
		return m, nil, true
	case backendDoneMsg:
		m.replyStart = -1
		m.liveReplyIdx = -1
		m.streaming = false
		m.thinking = false
		m.saveSession()
		if len(m.response) > 0 {
			m.response = append(m.response, "")
		}
		nextQueued := m.dequeueNextPrompt()
		if queueCmd := m.flushNextQueued(nextQueued); queueCmd != nil {
			return m, tea.Batch(tea.ClearScreen, queueCmd), true
		}
		return m, tea.ClearScreen, true
	}
	return m, nil, false
}

// ---- tui_runmode_agent.go ----

func pickerHiddenCommands() []string {
	if !auth.IsBusinessAccount() {
		return nil
	}
	return []string{"/remote-control"}
}

func (m *runMode) Init() tea.Cmd {
	cmds := []tea.Cmd{tas.Blink, armTasksTick(), m.fetchPlanTierCmd()}
	if m.statusLineScriptActive() || m.titleCfg.Active() {
		m.store.StatusLineTickArmed = true
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

func (m *runMode) enableTools(decls []backend.ToolDecl, runner backend.ToolRunner) {
	m.tools = decls
	m.toolRunner = runner
}

func (m *runMode) moveWord(back bool) bool {
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

func (m *runMode) registerSubagentQuestionTool(catalog *tools.Registry) {
	if m.agentBody != "" {
		return
	}
	catalog.Register(&tasks.AskQuestion{
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
		m.enableTools(toolrunner.AdvertisedToolDecls(body, catalog), &permissionRunner{
			inner:   &toolrunner.RegistryRunner{Reg: catalog},
			policy:  m.policy,
			sandbox: m.toolSandbox,
			hook:    m.hookToolGate,
			post:    m.hookPostToolUse,
		})
	}
	m.response = nil
	m.session = nil
	if m.supervisor != nil {
		m.supervisor.Clear()
	}
	m.blocks = nil
	m.liveSteps = nil
	m.activeTool = ""
	m.autoTitle = ""
	m.conversationID = newConversationID()
	// The conversation changed, so the session did too: the documented
	// SessionStart event fires for the new one.
	m.hookSessionStarted = false
	// The definitions belong to the conversation that just ended.
	m.subagentDefs = subagenttools.NewStore()
	m.resetTrajectory()
}

// tuiSilenceLogger is the destination for the recovered subagent debug-log lines.
var tuiSilenceLogger = log.New(io.Discard, "", 0)

func (m *runMode) subAgentExtras(_ context.Context, be backend.Client, modelLabel string) ([]actor.Tool, func()) {
	if be == nil || m.agentBody != "" {
		return nil, func() {}
	}
	workspace := m.cwd
	if workspace == "" {
		workspace = defaultWorkspaceForRunMode()
	}
	agentDir := confhome.Antigravity()
	provider := agentturn.NewBackendProviderAdapter(be, modelLabel)
	subDeps := subagenttools.Deps{
		Provider:          provider,
		Model:             modelLabel,
		CWD:               workspace,
		AgentDir:          agentDir,
		Definitions:       m.subagentDefs,
		Tasks:             m.taskReg,
		Registry:          m.ensureSubagentRegistry(),
		ConversationID:    m.conversationID,
		PermissionGateFor: m.subagentGateFactory,
		SystemPrompt:      m.systemPrompt,
		Catalogue:         browsersubagent.BrowserTools(m.catalog),
	}
	subTools := subagenttools.Tools(subDeps)
	subDir := filepath.Join(agentDir, "subagents")
	_, _ = subagenttools.RehydrateSubagents(subDir, subDeps.Registry, m.taskReg, subDeps)
	if m.catalog != nil {
		browser.SetBrowserSubagentProvider(browsersubagent.New(subagenttools.Deps{
			Provider: provider,
			Model:    modelLabel,
			CWD:      workspace,
			AgentDir: agentDir,
		}, browsersubagent.Tools(m.catalog)))
		reg := builtinsubagent.Default()
		if err := browsersubagent.InstallBuiltin(reg, subDeps, m.catalog); err != nil {
			tuiSilenceLogger.Printf("[tui] browser built-in: %v", err)
		} else {
			subagenttools.SetBuiltinSubagentRegistry(reg)
		}
	}
	return subTools, func() {}
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

func newRunModeBase(b backend.Client, email, model, tier, cwd string, altScreen bool) *runMode {
	initConvID := newConversationID()
	m := &runMode{
		sessionState: sessionState{
			email:                 email,
			model:                 model,
			statusModel:           tuilib.StripTier(model),
			tier:                  tier,
			cwd:                   cwd,
			store:                 *tuistore.New(),
			supervisor:            supervisor.New(cwd),
			conversationID:        initConvID,
			initialConversationID: initConvID,
			narrationStart:        time.Now(),
			wireSettingsPending:   true,
			hooks:                 hooks.NewManager(),
		},
		engineState: engineState{
			backend:             b,
			taskReg:             tools.SharedTaskRegistry(),
			taskCtrl:            taskregistry.NewController(tools.SharedTaskRegistry()),
			subagentDefs:        subagenttools.NewStore(),
			taskSubagentManager: tools.SharedSubagentManager(),
			subAgentQuestions:   subagentquestions.NewManager(false),
			streamingSearch:     true,
			replyStart:          -1,
			liveReplyIdx:        -1,
			liveThoughtRunIdx:   -1,
			atRouter:            defaultRouter,
			taskInbox:           agentturn.NewTaskInbox(),
			verbosity:           settings.String("verbosity"),
		},
		uiState: uiState{
			altScreen:          altScreen,
			autoScroll:         true,
			rowsCacheDirty:     true,
			pinnedPromptRowIdx: -1,
			titleStore:         &statusline.TitleStore{},
		},
		overlayState: overlayState{
			activeBtwIndex: -1,
		},
	}
	m.picker = m.newSlashPicker()
	m.activeItems = m.newActiveItemsPanel()
	m.atFilePicker = m.newMentionPicker()
	m.resumePicker = resumepicker.New(resumepicker.Seams{
		CurrentConversationID: func() string { return m.conversationID },
		ListConversations:     m.listResumeConversations,
		ResumeConversation:    m.resumeConversation,
		RenameConversation:    m.renameResumeConversation,
	})
	m.subAgentQuestionPanel = m.newSubAgentQuestionPanel()
	m.helpPanel = m.newHelpPanel()
	m.voiceSurface = m.newVoiceSurface()
	return m
}

func initRunModeInput(m *runMode) {
	m.input = tas.New()
	m.input.SetVirtualCursor(false)
	m.input.Prompt = inputPrompt
	m.input.SetPromptFunc(2, func(info tas.PromptInfo) string {
		if info.LineNumber == 0 {
			if m.store.BashMode {
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
	initScheme := settings.String("colorScheme")
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

// newRunMode constructs the v1.3.5 binary 1:1 run mode.
func newRunMode(b backend.Client, email, model, tier, cwd string, altScreen bool) *runMode {
	pickerHidden := pickerHiddenCommands()
	email, model, tier, cwd = resolveRunModeDefaults(b, email, model, tier, cwd)
	m := newRunModeBase(b, email, model, tier, cwd, altScreen)
	if settings.String("queuedMessages") == "send-immediately" {
		m.store.QueueMode = false
	} else {
		m.store.QueueMode = true
	}

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
	m.picker.SetModelCatalogueSource(modelargpopup.Rows)
	return m
}

// ---- tui_turn_blocks.go ----

// transcriptDocument hands the host's transcript state to the ui tier.
//
// The host owns the rows: the document is the shape the transcript's
// mechanics work on, and the host takes the result back through
// applyTranscriptDocument.
func (m *runMode) transcriptDocument() transcript.Document {
	return transcript.Document{
		Lines:             m.response,
		Blocks:            m.blocks,
		Assistants:        m.assistants,
		UserBlocks:        m.userBlocks,
		Runs:              m.liveStepRuns,
		LiveStart:         m.liveStart,
		ReplyStart:        m.replyStart,
		LiveThoughtRunIdx: m.liveThoughtRunIdx,
		LiveTextLines:     m.liveTextLines,
		LiveReplyBuf:      m.liveReplyBuf,
		StreamedText:      m.streamedText,
	}
}

// applyTranscriptDocument takes the transcript's state back.
func (m *runMode) applyTranscriptDocument(doc transcript.Document) {
	m.response = doc.Lines
	m.blocks = doc.Blocks
	m.assistants = doc.Assistants
	m.userBlocks = doc.UserBlocks
	m.liveStepRuns = doc.Runs
	m.liveStart = doc.LiveStart
	m.replyStart = doc.ReplyStart
	m.liveThoughtRunIdx = doc.LiveThoughtRunIdx
	m.liveTextLines = doc.LiveTextLines
	m.liveReplyBuf = doc.LiveReplyBuf
	m.streamedText = doc.StreamedText
}

// transcriptOptions are the host's own render facts, as the transcript's
// mechanics see them.
func (m *runMode) transcriptOptions(msg toolsteps.TurnDoneMsg) transcript.Options {
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
func (m *runMode) repaintOptions() transcript.Options {
	verb := m.currentVerbosity()
	return transcript.Options{
		Width:          m.width,
		Height:         m.height,
		Verbosity:      verb,
		Expanded:       m.effectiveExpanded(verb),
		ViewportHeight: m.viewportHeight,
		Streaming:      m.streaming,
		Thought: transcript.LiveThought{
			Active: m.thinking,
			Text:   m.liveThinking,
			Since:  m.thinkingSince,
		},
	}
}

// toggleTrajectoryExpanded flips the trajectory-wide expanded flag,
// clears the scrollback by snapping the viewport to the active turn,
// and repaints the tool-step blocks under the active verbosity.
func (m *runMode) toggleTrajectoryExpanded() {
	m.expandedUserToggled = true
	m.expanded = !m.isExpanded()
	m.scroll = 0
	m.autoScroll = true
	m.rowsCacheDirty = true
	m.viewCacheValid = false
	m.rerenderBlocks()
	m.rerenderLiveSteps()
}

// rerenderBlocks repaints the transcript's tool-step blocks under the
// active verbosity and expansion.
func (m *runMode) rerenderBlocks() {
	m.rowsCacheDirty = true
	doc := m.transcriptDocument()
	doc.RerenderBlocks(m.repaintOptions())
	m.applyTranscriptDocument(doc)
}

// ensureVisibleBlocksExpanded repaints the blocks inside the visible
// window, so a block that scrolls into view is drawn under the current
// verbosity and expansion.
func (m *runMode) ensureVisibleBlocksExpanded(avail int) {
	doc := m.transcriptDocument()
	if doc.EnsureVisibleBlocksExpanded(m.repaintOptions(), avail, m.scroll, m.autoScroll) {
		m.rowsCacheDirty = true
		m.viewCacheValid = false
	}
	m.applyTranscriptDocument(doc)
}

// rerenderTranscriptForTheme repaints all user prompts, assistant replies,
// and tool step blocks across m.response under the active colour scheme.
//
// 【反编译】 agy 1.4.8 model.(*ConversationModel).handleSettingsChanges
// calls store.(*Manager).RerenderAll when the colour scheme changes,
// repainting the entire transcript with the new scheme's colours.
func (m *runMode) rerenderTranscriptForTheme() {
	if m == nil {
		return
	}
	m.rowsCacheDirty = true

	doc := m.transcriptDocument()
	doc.RerenderForTheme(m.repaintOptions())
	m.applyTranscriptDocument(doc)

	// Invalidate view caches so viewport redraws completely
	m.rowsCacheDirty = true
	m.rowsCache = wrapSlot{}
	m.rowsView = composedRows{}
}

// reflowTranscript re-renders and re-wraps the entire transcript (markdown replies,
// tool step blocks, turn separators, in-flight streaming replies) when the terminal width changes.
//
// 【反编译】 agy 1.4.12 ConversationModel.handleResizeRedraw (0x102bcbb70)
// calls buildRenderedItems (0x102bbc2d0) on terminal resize, recalculating and
// re-rendering all message items to reflow properly for the new column width.
func (m *runMode) reflowTranscript(width int) {
	if m == nil || width <= 0 {
		return
	}
	m.width = width
	mdrender.SetCurrentWidth(width)
	m.rowsCacheDirty = true
	m.viewCacheValid = false
	m.extraRowsValid = false

	doc := m.transcriptDocument()
	doc.Reflow(m.repaintOptions())
	m.applyTranscriptDocument(doc)

	// Invalidate viewport caches and rebuild wraps for the new width.
	m.rowsCacheDirty = true
	m.rowsCache = wrapSlot{}
	m.rowsCacheN1 = wrapSlot{}
	m.rowsView = composedRows{}
	m.viewCacheValid = false
	m.cachedHeaderKey = headerCacheKey{}
}

// syncLiveThoughtStep paints the turn's live thought row.
func (m *runMode) syncLiveThoughtStep() {
	doc := m.transcriptDocument()
	if doc.SyncLiveThoughtStep(m.repaintOptions()) {
		m.rowsCacheDirty = true
	}
	m.applyTranscriptDocument(doc)
}

// finishLiveThinking closes the turn's live thought: the running row
// becomes the finished thought.
func (m *runMode) finishLiveThinking() {
	if !m.thinking {
		return
	}
	// The options are taken while the thought is still live: the
	// finished thought is painted from the thought that was running.
	opts := m.repaintOptions()
	m.thinking = false
	m.liveThinking = ""
	m.liveThinkingSnippet = ""

	doc := m.transcriptDocument()
	if doc.FinishLiveThinking(opts) {
		m.rowsCacheDirty = true
	}
	m.applyTranscriptDocument(doc)
}

// rerenderLiveSteps repaints the in-flight step rows: the live thought
// and the tool batches the model is still running.
func (m *runMode) rerenderLiveSteps() {
	doc := m.transcriptDocument()
	if doc.RerenderLiveSteps(m.repaintOptions()) {
		m.rowsCacheDirty = true
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
func (m *runMode) appendCompactResult(msg compactDoneMsg) {
	if msg.Err != nil {
		m.response = append(m.response,
			uirows.NoticeCompact("Compact failed: "+msg.Err.Error()),
			"",
		)
		return
	}
	stats := compaction.StatsRow(m.width, 0, msg.TokensBefore, msg.TokensAfter, -1, -1)
	if msg.Aborted {
		stats += " (aborted)"
	}
	m.response = append(m.response,
		compaction.DividerRow(m.width),
		stats,
		"",
	)
}

var separator = palette.Rule(separatorWidth)

// separator returns the horizontal rule drawn between the banner and the
// prompt. Cycle 119: when the terminal width is known the rule is drawn at
// that width rather than the fixed 220 columns, so it never wraps.
func (m *runMode) separator() string {
	if m.width > 0 {
		return palette.Rule(m.width)
	}
	return separator
}

// promptRule returns the rule that frames the prompt box.
//
// 【一手】 agy 1.4.7: with a seasonal event switched on the prompt box's
// two rules become the event's own — a gradient rule with face motifs in
// it (out/changelog-147/ref/seasonal-prompt.txt) — while the rule that
// separates the queued messages from the transcript stays plain.
func (m *runMode) promptRule(top bool) string {
	if m.seasonal == nil {
		return m.separator()
	}
	return m.seasonal.Border.Rule(m.width, top)
}

const inputPrompt = "> "

// inputWidth is the textarea render width.
const inputWidth = 219

// statusRow is the status area's single row: the custom script's output
// when one is active, the built-in template otherwise.
func (m *runMode) statusRow() string {
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
func (m *runMode) installTaskNotifier() {
	if m.taskCtrl != nil {
		shell.SetTaskNotifier(func(n shell.TaskNotification) {
			m.taskCtrl.Inbox().Add(taskregistry.TaskNotification{
				TaskID:   n.TaskID,
				Title:    n.Title,
				Body:     n.Body,
				LogPath:  n.LogPath,
				Priority: n.Priority,
				At:       n.At,
			})
			if m.prog != nil {
				m.prog.Send(taskNotificationMsg{})
			}
		})
		return
	}
	if m.taskInbox == nil {
		m.taskInbox = agentturn.NewTaskInbox()
	}
	shell.SetTaskNotifier(func(n shell.TaskNotification) {
		m.taskInbox.Add(n)
		if m.prog != nil {
			m.prog.Send(taskNotificationMsg{})
		}
	})
}

// updateTaskNotificationMsg handles a task notification that arrived.
//
// The notification is drained into the session by the next turn's message
// builder. When the agent is idle, the TUI starts a turn right away: the
// reference's agent picks the message up and carries on with its work
// without waiting for the user to type something.
func (m *runMode) updateTaskNotificationMsg() tea.Cmd {
	hasItems := false
	if m.taskCtrl != nil {
		hasItems = m.taskCtrl.Inbox().Len() > 0
	} else if m.taskInbox != nil {
		hasItems = m.taskInbox.Len() > 0
	}
	if !hasItems {
		return nil
	}
	if m.streaming {
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
func (m *runMode) startNotificationTurn() tea.Cmd {
	m.liveStart = len(m.response)
	m.replyStart = -1
	m.liveReplyIdx = -1
	m.liveSteps = nil
	m.liveStepRuns = nil
	m.activeTool = ""
	m.thinking = false
	m.liveTextLines = 0
	m.liveReplyBuf = ""
	m.streamedText = false
	m.liveBatch = 0
	m.batchBreak = false
	m.anyToolRun = false
	m.spinFrame = 0
	m.streaming = true
	m.clearLastTurnOutcome()
	tickCmd := func() tea.Msg { return m.spinner.Tick() }
	return tea.Batch(m.streamPrompt(""), tickCmd)
}

// takeTaskNotifications removes the pending notifications from the inbox and
// returns them, oldest first.
func (m *runMode) takeTaskNotifications() []shell.TaskNotification {
	if m.taskCtrl != nil {
		items := m.taskCtrl.Inbox().Drain()
		if len(items) == 0 {
			return nil
		}
		out := make([]shell.TaskNotification, len(items))
		for i, n := range items {
			out[i] = shell.TaskNotification{
				TaskID:   n.TaskID,
				Title:    n.Title,
				Body:     n.Body,
				LogPath:  n.LogPath,
				Priority: n.Priority,
				At:       n.At,
			}
		}
		return out
	}
	if m.taskInbox == nil {
		return nil
	}
	return m.taskInbox.Drain()
}

// drainTaskNotifications moves the pending notifications into the session
// and renders them in the transcript.
//
// The rows go in before the turn's live block: the drain happens while the
// turn is being set up, so the notification belongs to the conversation
// before the model's answer to it. If a user turn was already appended to
// the session, the notifications are placed BEFORE the user turn so the user
// prompt remains the active request.
func (m *runMode) drainTaskNotifications() {
	items := m.takeTaskNotifications()
	if len(items) == 0 {
		return
	}
	rows := 0
	trailingUser := len(m.session) > 0 && m.session[len(m.session)-1].Role == "user"
	var userMsg backend.Message
	if trailingUser {
		userMsg = m.session[len(m.session)-1]
		m.session = m.session[:len(m.session)-1]
	}

	for _, item := range items {
		msg := agentturn.TaskNotificationMessage(item)
		m.session = append(m.session, msg)
		if m.supervisor != nil {
			m.supervisor.Append(msg)
		}
		lines := uirows.NotificationRows(item)
		if trailingUser && len(m.userBlocks) > 0 {
			ub := m.userBlocks[len(m.userBlocks)-1]
			idx := ub.Start
			if idx >= 0 && idx <= len(m.response) {
				m.response = append(m.response[:idx], append(lines, m.response[idx:]...)...)
				m.userBlocks[len(m.userBlocks)-1].Start += len(lines)
			} else {
				m.response = append(m.response, lines...)
			}
		} else {
			m.response = append(m.response, lines...)
		}
		rows += len(lines)
	}

	if trailingUser {
		m.session = append(m.session, userMsg)
	}

	// The turn's live block starts after the notification's rows.
	if m.liveStart >= 0 && m.liveStart+rows <= len(m.response) {
		m.liveStart += rows
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
func (m *runMode) ensureSubagentRegistry() *subagenttools.Registry {
	if m.subagentRegistry != nil && m.subagentRegistry.ParentID() == m.conversationID {
		return m.subagentRegistry
	}
	if m.subagentRegistry != nil {
		// The conversation changed: the old children belong to it.
		m.subagentRegistry.Close()
	}
	m.subagentRegistry = subagenttools.NewRegistry(m.conversationID)
	m.subagentRegistry.SetNotify(m.notifySubagentMessage)
	return m.subagentRegistry
}

// notifySubagentMessage hands a subagent's message to the session.
//
// It travels the way a background task's notification travels: into the
// session's inbox, which the next turn drains into the conversation. The
// text the model reads is the reference's own envelope, with the
// subagent's conversation id as the sender.
func (m *runMode) notifySubagentMessage(sender, content string) {
	if m.taskCtrl != nil {
		m.taskCtrl.Inbox().Add(taskregistry.TaskNotification{
			TaskID:   sender,
			Body:     content,
			Priority: shell.TaskPriorityHigh,
		})
		if m.prog != nil {
			m.prog.Send(taskNotificationMsg{})
		}
		return
	}
	if m.taskInbox == nil {
		m.taskInbox = agentturn.NewTaskInbox()
	}
	m.taskInbox.Add(shell.TaskNotification{
		TaskID:   sender,
		Body:     content,
		Priority: shell.TaskPriorityHigh,
	})
	if m.prog != nil {
		m.prog.Send(taskNotificationMsg{})
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
func (m *runMode) subagentGateFactory(worktree string) subagenttools.BeforeToolCallGate {
	return &subagentPermissionGate{
		policy:   m.policy,
		sandbox:  m.toolSandbox,
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

func (m *runMode) dispatchSlashLine(buf string) (bool, tea.Cmd) {
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
		m.input.Reset()
		if m.undoMgr != nil {
			m.undoMgr.Clear()
		}
		m.store.ResetInputSelection()
		m.promptHistory.Reset()
		m.picker.Reset()
		m.atFilePicker.Reset()
		if m.panel.kind == panelregistry.Usage && m.height > 0 {
			m.quotaFrame = 0
			return true, tea.Batch(syncCmd, usagepanel.QuotaTick(), m.fetchQuotaSummaryCmd())
		}
		if m.panel.kind == panelregistry.Accounts {
			// The panel queries every account's quota, so the user
			// does not have to open /usage for the numbers.
			return true, tea.Batch(syncCmd, m.accountsPanel().QueueQuota())
		}
		if m.panel.kind == panelregistry.CodeSearch && m.height > 0 {
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

func (m *runMode) submitBashMode(cmd string) (bool, tea.Cmd) {
	m.store.BashMode = false
	m.input.Reset()
	if m.undoMgr != nil {
		m.undoMgr.Clear()
	}
	m.store.ResetInputSelection()
	m.promptHistory.Reset()
	m.picker.Reset()
	m.atFilePicker.Reset()
	m.modeHintDismissed = true
	if history.IsBlank(cmd) {
		return true, nil
	}
	m.pushHistory("!" + cmd)
	if m.streaming {
		// A running *shell* command is preempted: the reference stops it,
		// keeps what it printed, and runs the new one immediately (【一手】
		// `!top` then `!pwd`). Only a model turn queues — there the command
		// waits behind the turn and shows as `▸ <cmd>`.
		if m.store.BashCancel != nil {
			return true, m.preemptBashCommand(cmd)
		}
		m.store.BashQueue = append(m.store.BashQueue, cmd)
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
func (m *runMode) submitVimBashMode(prompt string) (tea.Cmd, bool) {
	if !m.vimEnabled() || !strings.HasPrefix(prompt, "!") {
		return nil, false
	}
	_, cmd := m.submitBashMode(strings.TrimPrefix(prompt, "!"))
	return cmd, true
}

func (m *runMode) popActiveSteer() (string, bool) {
	if m.activeBridge != nil {
		if msg, ok := m.activeBridge.PopSteer(); ok {
			return msg, true
		}
	}
	if sc, ok := m.turnEngine().(agentturn.SteerController); ok {
		return sc.PopSteer()
	}
	return "", false
}

func (m *runMode) steerActiveTurn(instruction string) bool {
	instruction = strings.TrimSpace(instruction)
	if instruction == "" {
		return false
	}
	steered := false
	if m.activeBridge != nil {
		if err := m.activeBridge.Steer(instruction); err == nil {
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
	m.store.SendingMessages = append(m.store.SendingMessages, instruction)
	m.rowsCacheDirty = true
	m.rowsGen++
	return true
}

func (m *runMode) applySteerToTranscript(instruction string) {
	instruction = strings.TrimSpace(instruction)
	if instruction == "" {
		return
	}

	// Reconcile SendingMessages: remove the applied instruction
	if len(m.store.SendingMessages) > 0 {
		idx := -1
		for i, s := range m.store.SendingMessages {
			if s == instruction {
				idx = i
				break
			}
		}
		if idx >= 0 {
			m.store.SendingMessages = append(m.store.SendingMessages[:idx], m.store.SendingMessages[idx+1:]...)
		} else {
			m.store.SendingMessages = m.store.SendingMessages[1:]
		}
	}

	// Deduplicate: do not commit the same instruction if already committed as the latest user block
	if len(m.userBlocks) > 0 && m.userBlocks[len(m.userBlocks)-1].Raw == instruction {
		return
	}

	// Finalize live thinking & tool on preceding step
	if m.thinking {
		m.finishLiveThinking()
	}
	m.activeTool = ""
	m.replyStart = -1
	m.liveReplyIdx = -1
	m.liveTextLines = 0
	m.liveReplyBuf = ""

	// Commit user prompt bubble into transcript after the finished step
	for len(m.response) > 0 && strings.TrimSpace(m.response[len(m.response)-1]) == "" {
		m.response = m.response[:len(m.response)-1]
	}
	m.response = append(m.response, "")
	start := len(m.response)
	promptLines := transcript.RenderUserPrompt(instruction, m.width)
	m.response = append(m.response, promptLines...)
	m.userBlocks = append(m.userBlocks, transcript.UserBlock{
		Start: start,
		Lines: len(promptLines),
		Raw:   instruction,
	})
	m.appendUserTurn(instruction)
	m.liveStart = len(m.response)
	m.liveBatch++
	m.batchBreak = true
	m.rowsCacheDirty = true
	m.rowsGen++
	m.saveSession()
}

func (m *runMode) updateSteerAppliedMsg(msg steerAppliedMsg) {
	m.applySteerToTranscript(msg.instruction)
}

func (m *runMode) checkStreamingQueue(prompt string) (bool, tea.Cmd) {
	if !m.streaming {
		return false, nil
	}
	if history.IsBlank(prompt) {
		m.input.Reset()
		m.store.ResetInputSelection()
		m.promptHistory.Reset()
		m.picker.Reset()
		return true, nil
	}

	// Move pending steer to queue: bare /queue or /unsteer
	if prompt == "/queue" || prompt == "/unsteer" {
		if msg, ok := m.popActiveSteer(); ok && msg != "" {
			m.store.StreamQueue = append(m.store.StreamQueue, msg)
			m.saveSession()
			m.input.Reset()
			m.store.ResetInputSelection()
			m.promptHistory.Reset()
			m.picker.Reset()
			m.atFilePicker.Reset()
			m.modeHintDismissed = true
			return true, nil
		}
	}

	// Explicit /queue <instruction> to force queueing even when QueueMode is off
	if strings.HasPrefix(prompt, "/queue ") {
		inst := strings.TrimSpace(strings.TrimPrefix(prompt, "/queue "))
		if inst != "" {
			m.pushHistory(prompt)
			m.store.StreamQueue = append(m.store.StreamQueue, inst)
			m.saveSession()
		}
		m.input.Reset()
		m.store.ResetInputSelection()
		m.promptHistory.Reset()
		m.picker.Reset()
		m.atFilePicker.Reset()
		m.modeHintDismissed = true
		return true, nil
	}

	// Promote queued prompt to steer if user inputs bare /steer
	if prompt == "/steer" {
		var inst string
		if m.store.QueuedPrompt != "" {
			inst = m.store.QueuedPrompt
			m.store.QueuedPrompt = ""
		} else if len(m.store.StreamQueue) > 0 {
			inst = m.store.StreamQueue[0]
			m.store.StreamQueue = m.store.StreamQueue[1:]
		}
		if inst != "" {
			m.saveSession()
			m.steerActiveTurn(inst)
			m.input.Reset()
			m.store.ResetInputSelection()
			m.promptHistory.Reset()
			m.picker.Reset()
			m.atFilePicker.Reset()
			m.modeHintDismissed = true
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
		m.input.Reset()
		m.store.ResetInputSelection()
		m.promptHistory.Reset()
		m.picker.Reset()
		m.atFilePicker.Reset()
		m.modeHintDismissed = true
		return true, nil
	}

	// In Send Immediately mode (QueueMode is off): submitting plain text steers the active turn
	if !m.store.QueueMode && !strings.HasPrefix(prompt, "/") {
		m.pushHistory(prompt)
		if m.steerActiveTurn(prompt) {
			m.input.Reset()
			m.store.ResetInputSelection()
			m.promptHistory.Reset()
			m.picker.Reset()
			m.atFilePicker.Reset()
			m.modeHintDismissed = true
			return true, nil
		}
	}

	if !strings.HasPrefix(prompt, "/") || m.slashQueuesWhileStreaming(prompt) {
		m.pushHistory(prompt)
		m.store.StreamQueue = append(m.store.StreamQueue, prompt)
		m.rowsCacheDirty = true
		m.rowsGen++
		m.input.Reset()
		m.store.ResetInputSelection()
		m.promptHistory.Reset()
		m.picker.Reset()
		m.atFilePicker.Reset()
		m.modeHintDismissed = true
		return true, nil
	}
	return false, nil
}

func (m *runMode) expandPastedPlaceholders(prompt string) string {
	if len(m.pastedBlocks) == 0 {
		return prompt
	}
	for placeholder, content := range m.pastedBlocks {
		if strings.Contains(prompt, placeholder) {
			prompt = strings.ReplaceAll(prompt, placeholder, content)
		}
	}
	return prompt
}

func (m *runMode) resolveSubmittedPrompt() (string, bool) {
	rawPrompt := m.input.Value()
	if history.IsBlank(rawPrompt) {
		m.input.Reset()
		m.store.ResetInputSelection()
		m.promptHistory.Reset()
		m.picker.Reset()
		return "", false
	}
	m.announceQueue.HideForSession()
	prompt := rawPrompt
	if m.picker.Visible() {
		if sel := m.picker.Selected(); sel != nil && strings.IndexByte(prompt, ' ') < 0 && strings.IndexByte(prompt, '\t') < 0 {
			prompt = sel.Name
		}
	}
	// Expand folded [Pasted text #N ...] placeholders into the full pasted text for model submission.
	prompt = m.expandPastedPlaceholders(prompt)
	m.pushHistory(prompt)
	return prompt, true
}

func (m *runMode) dispatchPromptPrefix(prompt string) (bool, tea.Cmd) {
	if handled, cmd := m.handlePromptAtPrefix(prompt); handled {
		return true, cmd
	}
	if handled, cmd := m.dispatchSlashLine(prompt); handled {
		return true, cmd
	}
	if flag.Lookup("test.v") == nil && !backendselect.HasOAuthToken() && os.Getenv("GEMINI_API_KEY") == "" && os.Getenv("AGY_ADC_AUTH") == "" {
		m.slashRespond("/login", "Authentication required. Please visit the URL to log in:")
		lm := loginui.New()
		lm.Width = m.width
		lm.Height = m.height
		m.login = lm
		m.input.Reset()
		m.store.ResetInputSelection()
		return true, lm.Init()
	}
	return false, nil
}

func (m *runMode) resetLiveTurnState(prompt string) {
	m.input.Reset()
	if m.undoMgr != nil {
		m.undoMgr.Clear()
	}
	m.pastedBlocks = nil
	m.nextPasteID = 0
	m.store.ResetInputSelection()
	m.atFilePicker.Reset()
	m.clearEligibilityNotice()
	m.streaming = true
	// A new turn replaces the interrupted one: the reference's composer
	// hint belongs to the turn that was cancelled, not to the next one.
	m.turnInterrupted = false
	m.promptHistory.Reset()
	m.clearLastTurnOutcome()
	if m.autoTitle == "" {
		m.autoTitle = convstore.GenerateConversationTitle(prompt)
	}
	for _, row := range uirows.AttachmentRows(m.pendingMedia, m.mediaExpanded) {
		m.response = append(m.response, row)
	}
	for len(m.response) > 0 && strings.TrimSpace(m.response[len(m.response)-1]) == "" {
		m.response = m.response[:len(m.response)-1]
	}
	sep := transcript.TurnSeparator(m.width)
	if len(m.response) == 0 {
		m.response = append(m.response, sep)
	} else {
		m.response = append(m.response, "", sep)
	}
	start := len(m.response)
	promptLines := transcript.RenderUserPrompt(prompt, m.width)
	m.response = append(m.response, promptLines...)
	m.userBlocks = append(m.userBlocks, transcript.UserBlock{
		Start: start,
		Lines: len(promptLines),
		Raw:   prompt,
	})
	m.liveStart = len(m.response)
	m.replyStart = -1
	m.liveReplyIdx = -1
	m.liveSteps = nil
	m.liveStepRuns = nil
	m.activeTool = ""
	m.thinking = false
	m.liveThinking = ""
	m.liveThinkingSnippet = ""
	m.liveThoughtRunIdx = -1
	showTips := true
	if v, ok := settings.Bool("showTips"); ok {
		showTips = v
	}
	if showTips {
		m.activeTip = tuitools.RandomTip()
	} else {
		m.activeTip = ""
	}
	m.liveTextLines = 0
	m.liveReplyBuf = ""
	m.lastStreamSave = time.Time{}
	m.streamedText = false
	m.liveBatch = 0
	m.batchBreak = false
	m.anyToolRun = false
	m.spinFrame = 0
	m.picker.Reset()
	m.store.ResetCompaction()
}

func (m *runMode) startModelTurn(prompt string) tea.Cmd {
	// Cycle multi-account-switch: the turn runs as the account the
	// shared credential names right now, not the one the session
	// started with. The transport is rebuilt by the sync when the
	// account changed, and the stream command runs after this Update
	// returns — so it picks the rebuilt transport up.
	syncCmd := m.syncAccountIdentity()
	m.conversationStarted = true
	m.drainTaskNotifications()
	m.resetLiveTurnState(prompt)
	m.appendUserTurn(prompt)
	m.saveSession()
	tickCmd := func() tea.Msg { return m.spinner.Tick() }
	return tea.Batch(m.streamPrompt(prompt), tickCmd, syncCmd)
}

func (m *runMode) submitPrompt() tea.Cmd {
	m.inputNotice = ""
	if m.store.BashMode {
		_, cmd := m.submitBashMode(m.expandPastedPlaceholders(m.input.Value()))
		return cmd
	}
	// In vim mode the engine put the "!" into the buffer itself — the
	// reference's NORMAL-mode "!" inserts the character and switches to
	// INSERT mode — so the shell command reaches the submit path with
	// the "!" still in front of it.
	if cmd, ok := m.submitVimBashMode(m.expandPastedPlaceholders(m.input.Value())); ok {
		return cmd
	}
	if handled, cmd := m.checkStreamingQueue(m.expandPastedPlaceholders(m.input.Value())); handled {
		return cmd
	}
	if m.subAgentQuestionPanel.SubmitAnswer() {
		return nil
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
	prompt = PrependSlashPlanIfNeeded(prompt, m.mode)
	if handled, cmd := m.dispatchPromptPrefix(prompt); handled {
		return cmd
	}
	return m.startModelTurn(prompt)
}

func (m *runMode) appendUserTurn(text string) {
	msg := backend.Message{
		Role:        "user",
		Content:     text,
		WireContent: backend.WrapUserContent(text, m.model, m.wireSettingsPending, time.Now()),
	}
	// The images the user attached ride along with the turn (agy 1.4.9's
	// image pasting). The transports carry them as image parts, so the
	// model receives them the way the reference sends them.
	if parts := m.mediaImageParts(); len(parts) > 0 {
		msg.Images = parts
		m.pendingMedia = nil
		m.mediaExpanded = false
	}
	if m.supervisor != nil {
		m.supervisor.Append(msg)
	}
	m.session = append(m.session, msg)
	m.wireSettingsPending = false
}

func (m *runMode) isEngineSlashCommand(cmd string) bool {
	if cmd == "agent" {
		return true
	}
	name := cmd
	if canonical, ok := slashcommands.Canonical(cmd); ok {
		name = canonical
	}
	if _, ok := slashHandlers[name]; ok {
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
	return m.engine != nil
}

func (m *runMode) slashQueuesWhileStreaming(buf string) bool {
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

func (m *runMode) abortTurnForLocalCommand() {
	if !m.streaming {
		return
	}
	m.discardTurn = true
	if m.turnCancel != nil {
		m.turnCancel()
		m.turnCancel = nil
	}
	m.streaming = false
	m.thinking = false
}

func (m *runMode) submitAlternate() tea.Cmd {
	if m.subAgentQuestionPanel != nil && m.subAgentQuestionPanel.SubmitAnswer() {
		return nil
	}
	prompt := m.input.Value()
	if history.IsBlank(prompt) {
		m.input.Reset()
		m.store.ResetInputSelection()
		m.promptHistory.Reset()
		m.picker.Reset()
		return nil
	}
	// Invert QueueMode temporarily for this submission
	orig := m.store.QueueMode
	m.store.QueueMode = !orig
	defer func() {
		m.store.QueueMode = orig
	}()
	return m.submitPrompt()
}

func (m *runMode) submitOrQueuePrompt() tea.Cmd {
	return m.submitPrompt()
}

func (m *runMode) saveSession() {
	var queued []string
	if m.store.QueuedPrompt != "" {
		queued = append(queued, m.store.QueuedPrompt)
	}
	if len(m.store.StreamQueue) > 0 {
		queued = append(queued, m.store.StreamQueue...)
	}
	if m.conversationID != "" {
		_ = convstore.SaveQueuedPrompts(m.conversationID, queued)
	}

	if m.supervisor != nil {
		_ = m.supervisor.Save(m.conversationID, m.autoTitle)
		return
	}
	if m.conversationID == "" || len(m.session) == 0 {
		return
	}
	steps := convstore.BuildSteps(m.trajectoryFacts())
	if len(steps) == 0 {
		return
	}
	if err := convstore.SaveConversation(m.conversationID, m.cwd, steps); err != nil {
		return
	}
	if m.autoTitle != "" {
		_ = convstore.WriteSummariesTitle(m.conversationID, m.autoTitle)
		_ = convstore.WriteCacheTitle(m.conversationID, m.autoTitle)
	}
}
