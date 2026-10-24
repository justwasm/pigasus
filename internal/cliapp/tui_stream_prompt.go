package cliapp

import (
	"context"
	"errors"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	piagent "github.com/MichaelKinsy/PiG/agent"

	"github.com/justwasm/pigasus/internal/accounts"
	"github.com/justwasm/pigasus/internal/agent"
	"github.com/justwasm/pigasus/internal/agyprompt"
	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/compaction"
	"github.com/justwasm/pigasus/internal/pigrt"
	"github.com/justwasm/pigasus/internal/tuilib"
)

// firstLine returns the first line of s — the text up to the first newline.
// The rule itself now lives in internal/tuilib.
func firstLine(s string) string {
	return tuilib.FirstLine(s)
}

func (m *runMode) prepareStreamTurnMessages() []backend.Message {
	msgs := make([]backend.Message, len(m.session))
	copy(msgs, m.session)
	for i := range msgs {
		if msgs[i].Role != "user" {
			continue
		}
		msgs[i].Content = msgs[i].WireText()
		msgs[i].WireContent = ""
	}
	return msgs
}

func (m *runMode) wrapToolRunner(runner agent.ToolRunner, prog *tea.Program, steps *[]toolStep) agent.ToolRunner {
	if runner == nil {
		return nil
	}
	return &tuiToolRunner{
		inner: runner,
		onStart: func(call backend.ToolCall) {
			if prog != nil {
				prog.Send(toolRunningMsg{step: newRunningStep(call.Name, call.Arguments)})
			}
		},
		onEnd: func(call backend.ToolCall, res string, err error) {
			st := newToolStep(call.Name, call.Arguments, res, err != nil)
			*steps = append(*steps, st)
			if prog != nil {
				prog.Send(toolStepMsg{step: st})
			}
		},
	}
}

type streamCollector struct {
	full      *strings.Builder
	sanitizer *agent.Sanitizer
	done      bool
	meta      **backend.ChunkMeta
	prog      *tea.Program
	runMode   *runMode
}

func (sc *streamCollector) handleChunkError(err error) {
	if ee := AsEligibilityError(err); ee != nil {
		if sc.prog != nil {
			sc.prog.Send(eligibilityNoticeMsg{
				Reason:          ee.Reason,
				VerificationURL: ee.VerificationURL,
			})
		}
		return
	}
	errmsg := err.Error()
	var he *backend.HTTPError
	if errors.As(err, &he) && he.IsScopeMismatch() {
		errmsg = errmsg + " " + backend.OAuthScopeHintMessage
	}
	if sc.prog != nil {
		sc.prog.Send(backendChunkMsg(errmsg))
	}
}

func (sc *streamCollector) handleChunk(chunk backend.StreamChunk) error {
	if chunk.Error != nil {
		sc.handleChunkError(chunk.Error)
		return nil
	}
	if chunk.Meta != nil {
		*sc.meta = chunk.Meta
		sc.runMode.recordTurnUsage(chunk.Meta)
	}
	if chunk.Text != "" {
		sanitized := sc.sanitizer.Push(chunk.Text)
		sc.full.WriteString(sanitized)
		if sc.prog != nil {
			sc.prog.Send(backendChunkMsg(sanitized))
		}
	}
	if chunk.Done && !sc.done {
		sc.done = true
		sc.full.WriteString(sc.sanitizer.Finish())
	}
	return nil
}

func (m *runMode) buildLoopOptions(be backend.Client, model string, decls []backend.ToolDecl, wrapped agent.ToolRunner, extras []piagent.AgentTool, sc *streamCollector) agent.Options {
	return agent.Options{
		Backend:         be,
		SystemPrompt:    m.systemPrompt(),
		Handler:         sc.handleChunk,
		DefaultModel:    model,
		MaxTurns:        1,
		Tools:           decls,
		ToolRunner:      wrapped,
		ExtraAgentTools: extras,
		Effort:          m.currentEffortName(),
		AutoCont:        true,
		OnCompactionStart: func(reason string, startedAt time.Time) {
			if sc.prog != nil {
				sc.prog.Send(compactStartMsg{Reason: reason, StartedAt: startedAt})
			}
		},
		OnCompactionEnd: func(reason string, endedAt time.Time, summary string, tokensBefore, tokensAfter int, aborted bool) {
			if sc.prog != nil {
				sc.prog.Send(compactEndMsg{
					Reason:               reason,
					EndedAt:              endedAt,
					Summary:              summary,
					TokensBefore:         tokensBefore,
					EstimatedTokensAfter: tokensAfter,
					Aborted:              aborted,
				})
			}
		},
		AutoContBanner: func(depth, maxDepth int) {
			if sc.prog != nil {
				sc.prog.Send(autoContBannerMsg{depth: depth, maxDepth: maxDepth})
			}
		},
	}
}

func initThoughtSanitizer(thought *strings.Builder, thoughtStart *time.Time, prog *tea.Program) *agent.Sanitizer {
	sanitizer := agent.NewSanitizer()
	thoughtSent := false
	sanitizer.OnThought = func(t string) {
		if t == "" {
			return
		}
		if thoughtStart.IsZero() {
			*thoughtStart = time.Now()
		}
		thought.WriteString(t)
		if !thoughtSent {
			thoughtSent = true
			if prog != nil {
				prog.Send(thoughtStartMsg{})
			}
		}
	}
	return sanitizer
}

func appendThoughtStep(steps *[]toolStep, thought *strings.Builder, thoughtStart time.Time, meta *backend.ChunkMeta) {
	if thought.Len() == 0 {
		return
	}
	tokens := 0
	if meta != nil {
		tokens = meta.ReasoningTokens
	}
	*steps = append([]toolStep{{
		kind:   stepKindThought,
		name:   "thinking",
		detail: strings.TrimSpace(thought.String()),
		dur:    time.Since(thoughtStart),
		tokens: tokens,
	}}, *steps...)
}

func (m *runMode) runStreamTurnLoop(ctx context.Context, be backend.Client, model string, msgs []backend.Message) tea.Msg {
	var steps []toolStep
	wrapped := m.wrapToolRunner(m.toolRunner, m.prog, &steps)

	var full, thought strings.Builder
	var thoughtStart time.Time
	sanitizer := initThoughtSanitizer(&thought, &thoughtStart, m.prog)
	var meta *backend.ChunkMeta

	extras, closeSub := m.subAgentExtras(ctx, be, model)
	defer closeSub()

	sc := &streamCollector{
		full:      &full,
		sanitizer: sanitizer,
		meta:      &meta,
		prog:      m.prog,
		runMode:   m,
	}

	opts := m.buildLoopOptions(be, model, m.tools, wrapped, extras, sc)
	loop, err := agent.New(opts)
	if err != nil {
		return backendDoneMsg{}
	}
	defer loop.Close()

	sendErr := loop.Send(ctx, agent.TurnInput{Messages: msgs})
	if ctx.Err() != nil {
		sendErr = nil
	}
	appendThoughtStep(&steps, &thought, thoughtStart, meta)

	out := full.String()
	if out == "" && len(steps) == 0 && sendErr == nil {
		return backendDoneMsg{}
	}
	return turnDoneMsg{reply: out, steps: steps, reasoning: thought.String(), err: sendErr}
}

func (m *runMode) streamPrompt(prompt string) tea.Cmd {
	be := m.backend
	model := m.model
	if model == "" {
		model = "claude-4.5-sonnet"
	}
	msgs := m.prepareStreamTurnMessages()

	return func() tea.Msg {
		if be == nil {
			return backendDoneMsg{}
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		m.turnCancel = cancel
		defer func() { m.turnCancel = nil }()

		return m.runStreamTurnLoop(ctx, be, model, msgs)
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
	agentDir := auth.AntigravityConfigHome()
	opts := agyprompt.DefaultOptions(workspace, agentDir, conversationID)
	opts.UserRules = agyprompt.DiscoverUserRules(workspace, agentDir)
	opts.Skills = agyprompt.DiscoverSkills(pigrt.AgySkillPaths(agentDir, m.cwd))
	if m.agentBody == "" {
		opts.Subagents = append(opts.Subagents, customSubagents()...)
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
	skills = promptSectionTokenCount("skills", agyprompt.SectionBody(opts, "skills"))
	subagents = promptSectionTokenCount("subagents", agyprompt.SectionBody(opts, "subagents"))
	base = (len(full) - skills*3 - subagents*3) / 3
	if base < 0 {
		base = 0
	}
	return base, skills, subagents
}

// promptSectionTokenCount measures one complete XML section.
func promptSectionTokenCount(name, body string) int {
	if body == "" {
		return 0
	}
	return (len(body) + 2*len(name) + 2 + 1) / 3
}
