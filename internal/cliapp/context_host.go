// context_host.go — the host's side of the /context usage panel.
//
// # Where the panel lives
//
// The panel itself is internal/ui/views/contextpanel: it owns the rows
// — the three layouts (the empty session, the turn in flight and the
// completed turn) and the shared tail. The host supplies the session's
// facts — the token counts, the model name and the terminal's width —
// and nothing else.
//
// The panel has no state of its own: it is rebuilt on every draw from
// the State the host hands it, so a stale count is never shown.

package cliapp

import (
	"sync/atomic"

	"github.com/justwasm/pigasus/internal/app/prompt/agyprompt"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/ui/toolsteps"
	"github.com/justwasm/pigasus/internal/ui/views/contextpanel"
)

// contextState gathers the session's facts the /context panel's rows
// are a function of.
//
// The mode is the host's answer to "what does this session look like
// right now": a turn in flight (cycle 289), an empty session (cycle
// 190), or a completed turn (cycle 224).
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
		// The rules section is part of the prompt the base counts, and
		// the reference reports the two separately, so the rules leave
		// the base.
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
	// The checkpoint's tokens stand in for turns that have already been
	// compacted: the reference tracks them outside the grid and outside
	// the usage total, so they leave the user row.
	st.CheckpointBuffer = contextpanel.CheckpointBufferTokens(m.session)
	st.User -= st.CheckpointBuffer
	if st.User < 0 {
		st.User = 0
	}
	return st
}

// promptCategoryTokens splits the reference prompt into the categories
// the panel reports: the base prompt, the rules, the skills and the
// subagents.
//
// The base, the skills and the subagents come from the host's own
// promptSectionTokens; the rules are measured here, because the panel
// reports them on a row of their own.
func (m *runMode) promptCategoryTokens() (base, rules, skills, subagents int) {
	base, skills, subagents = m.promptSectionTokens()
	opts := m.promptOptions()
	rules = agyprompt.SectionTokenCount("user_rules", agyprompt.SectionBody(opts, "user_rules"))
	return base, rules, skills, subagents
}

// contextModel is the model name the panel's title row carries.
//
// The reference's own default is shown when the session has no model
// yet: the default model the catalogue names (defaultModel).
func (m *runMode) contextModel() string {
	if m.model != "" {
		return m.model
	}
	return defaultModel
}

// streamingTokenCounts is sessionTokenCounts adjusted for the step that
// is still in flight.
func (m *runMode) streamingTokenCounts() (user, agent, tool int) {
	user, _, tool = m.sessionTokenCounts()
	user = contextpanel.AdjustStreamingUserTokens(m.session, user)
	completed := atomic.LoadInt64(&m.store.SessionCompletionTokens)
	lastTurn := atomic.LoadInt64(&m.store.LastTurnCompletionTokens)
	agent = contextpanel.StreamingAgentTokens(completed, lastTurn, len(m.liveReplyBuf))
	return user, agent, tool
}

// wireToolDecls is the session's tool declarations.
func (m *runMode) wireToolDecls() []backend.ToolDecl {
	return append([]backend.ToolDecl(nil), m.tools...)
}

// sessionTokenCounts is the session's user/agent/tool token counts.
func (m *runMode) sessionTokenCounts() (user, agent, tool int) {
	user = contextpanel.UserMessageTokens(m.session)
	completed := atomic.LoadInt64(&m.store.SessionCompletionTokens)
	lastTurn := atomic.LoadInt64(&m.store.LastTurnCompletionTokens)
	agent = contextpanel.AgentTokens(completed, lastTurn)
	// The tool row counts the raw outputs of the session's tool steps:
	// the finished blocks' steps and the live ones.
	var steps []toolsteps.Step
	for _, b := range m.blocks {
		steps = append(steps, b.Steps...)
	}
	steps = append(steps, m.liveSteps...)
	tool = contextpanel.ToolResultTokens(toolsteps.ToolResults(steps)...)
	return user, agent, tool
}
