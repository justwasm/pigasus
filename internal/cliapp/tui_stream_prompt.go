package cliapp

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/agentlink"
	"github.com/justwasm/pigasus/internal/app/agentturn"
	"github.com/justwasm/pigasus/internal/app/prompt/agyprompt"
	"github.com/justwasm/pigasus/internal/app/session/setup"
	browsersubagent "github.com/justwasm/pigasus/internal/app/subagent/browser"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/core/cliflags"
	"github.com/justwasm/pigasus/internal/core/compaction"
	confhome "github.com/justwasm/pigasus/internal/core/confhome"
	"github.com/justwasm/pigasus/internal/core/policy/permissions"
	accounts "github.com/justwasm/pigasus/internal/features/accounts/core"
	"github.com/justwasm/pigasus/internal/features/auth"
	"github.com/justwasm/pigasus/internal/pigrt"
	"github.com/justwasm/pigasus/internal/ui/toolsteps"
)

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
	msgs := make([]backend.Message, 0, len(m.implicitMsgs)+len(m.session))
	msgs = append(msgs, m.implicitMsgs...)
	msgs = append(msgs, m.session...)
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
	return agentturn.NewLocal(m.localTurnEnv)
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
	}
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
	// The documented hooks bracket the turn: SessionStart fires once
	// per session, before the first model invocation, and Stop fires
	// when the execution loop terminates.
	m.hookSessionStart(ctx)
	m.hookPreInvocation(ctx)
	defer m.hookStop(ctx)
	defer m.hookPostInvocation(ctx)
	frames, err := m.turnEngine().Turn(ctx, agentturn.Turn{
		Model:   m.turnModel(),
		History: agentturn.ToWireMessages(msgs),
	})
	if err != nil {
		return backendDoneMsg{}
	}
	c := &turnCollector{prog: m.prog}
	for f := range frames {
		if f.Usage != nil {
			c.usage(f.Usage)
		}
		switch f.Kind {
		case agentlink.KindText:
			c.text(f.Text)
		case agentlink.KindThought:
			c.thoughtDelta(f.Text)
		case agentlink.KindTool:
			c.tool(f.Tool)
		case agentlink.KindCompact:
			c.compact(f.Compact)
		case agentlink.KindNotice:
			c.notice(f.Notice)
		case agentlink.KindResult:
			c.result(f)
		}
	}
	if c.meta != nil {
		m.recordTurnUsage(c.meta)
	}
	var reasoningTokens int
	if c.meta != nil {
		reasoningTokens = c.meta.ReasoningTokens
	}
	c.steps = toolsteps.AppendThoughtStep(c.steps, c.reasoning.String(), time.Since(c.thoughtStart), reasoningTokens)

	out := c.reply.String()
	if out == "" && len(c.steps) == 0 && c.err == nil {
		return backendDoneMsg{}
	}
	var tm toolsteps.TokenMetrics
	if c.meta != nil {
		tm = toolsteps.TokenMetrics{
			Input:     c.meta.PromptTokens,
			Output:    c.meta.CompletionTokens,
			Reasoning: c.meta.ReasoningTokens,
		}
	}
	return toolsteps.TurnDoneMsg{
		Reply:        out,
		Steps:        c.steps,
		Reasoning:    c.reasoning.String(),
		Err:          c.err,
		TokenMetrics: tm,
	}
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
	opts.Skills = agyprompt.DiscoverSkills(pigrt.AgySkillPaths(agentDir, m.cwd))
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
