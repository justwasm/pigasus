// statusline_payload.go — the host's side of the status line's payload.
//
// The document itself is internal/ui/views/statusline's: it is the
// reference's `types.StatusLineData`, and every member name, its order and
// its omit rules are a wire contract (the official example scripts
// published at github.com/google-antigravity/antigravity-cli/examples read
// the members by name with jq). The host keeps the facts the document
// cannot know — the workspace, the conversation, the model, the account, the
// token counters, the quota — and hands them over as a State.
//
// Evidence:
//   - 【一手】 agy 1.4.7 was run with a capture script bound to
//     `statusLine.command`; the payloads it produced are the fixture in
//     internal/ui/views/statusline/payload_test.go.
//   - 【反编译】 the JSON tags were read out of the binary's string table
//     (`types.StatusLineData` and its member structs).
package cliapp

import (
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"

	modelctx "github.com/justwasm/pigasus/internal/features/models/ctx"
	"github.com/justwasm/pigasus/internal/ui/views/statusline"
)

// buildStatusLinePayload renders the document the scripts read.
//
// The document is the package's; this is where the host's facts are
// collected. The state is filled in one place, so a fact the payload
// carries has exactly one reader on this side.
func (m *runMode) buildStatusLinePayload() statusline.Payload {
	// The status row's model label is the base name of the selected model;
	// it is the model the context window and the pricing are read from.
	model := m.statusModel
	if model == "" {
		model = m.model
	}
	// An unknown model has no context window.
	contextWindow := 0
	if model != "" {
		contextWindow = modelctx.ContextWindowFor(model)
	}
	// The number of running background tasks.
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

// agentState is the session's agent state — the payload's `agent_state`
// member and the status row's state cell.
//
// The vocabulary is the statusline package's (the reference's own names)
// and the facts are the host's: a streaming turn, a tool call inside it
// and the last turn's failure. The payload and the row read this one
// function, so the two never disagree.
func (m *runMode) agentState() string {
	state := statusline.AgentState(m.streaming, m.toolInFlight(), m.turnFailed())
	if state == statusline.StateIdle {
		// The idle state is the row's default; it needs no cell.
		return ""
	}
	return state
}

// toolInFlight reports whether a tool call is running inside the
// streaming turn.
//
// The active tool is the host's own: it is set when a tool call starts
// and cleared when the call ends.
func (m *runMode) toolInFlight() bool {
	return m.activeTool != ""
}

// turnFailed reports whether the last turn ended in an error.
//
// The recorded outcome is the host's: the turn records how it ended
// (tea.ProgramStateDone, tea.ProgramStateError) and the report to the
// terminal reads the same fact.
func (m *runMode) turnFailed() bool {
	return m.lastTurnOutcome == tea.ProgramStateError
}

// pendingInputCount is the number of queued user messages.
//
// The queue mode's own prompt counts as one: it is the next prompt the
// session runs. The bash queue does not — those are shell commands, not
// messages.
func (m *runMode) pendingInputCount() int {
	n := len(m.store.StreamQueue)
	if m.store.QueuedPrompt != "" {
		n++
	}
	return n
}

// subagentInfos is the conversation's subagent list in the payload's
// shape.
//
// The registry is the host's; the wire vocabulary is the statusline
// package's.
func (m *runMode) subagentInfos() []statusline.SubagentInfo {
	if m.subagentRegistry == nil {
		return nil
	}
	return statusline.Subagents(m.subagentRegistry.States())
}

// vimModeName is the vim editing mode's name for the payload's `vim`
// member.
//
// The member is present only when vim editor mode is enabled, so the
// name is empty when the editor mode is not vim. The engine is not built
// in that case: the host keeps its "no engine without vim" invariant.
func (m *runMode) vimModeName() string {
	if !m.vimEnabled() {
		return ""
	}
	return statusline.VimModeName(m.vimMode())
}

// contextUsedPercent is the share of the context window the prompt
// tokens take.
//
// The arithmetic is the statusline package's (the payload's
// `used_percentage` member is the same number) and the status row's
// context cell shows the same value, so the formula has one owner.
func (m *runMode) contextUsedPercent() float64 {
	model := m.statusModel
	if model == "" {
		model = m.model
	}
	if model == "" {
		return 0
	}
	prompt, _ := m.sessionTokenTotals()
	return statusline.ContextUsedPercent(prompt, modelctx.ContextWindowFor(model))
}

// contextUsage returns the context window token usage percentage.
func (m *runMode) contextUsage() float64 {
	if m == nil {
		return 0
	}
	return m.contextUsedPercent()
}

// StatusLinePayloadWithPlan wraps statusline.Payload to include execution plan progress if present.
type StatusLinePayloadWithPlan struct {
	statusline.Payload
	Plan string `json:"plan,omitempty"`
}

// buildStatusLinePayloadDoc returns the status line payload document,
// including execution plan progress when available from the active engine.
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

// statusLinePlanProgress returns the active engine's plan progress for status line consumers.
func (m *runMode) statusLinePlanProgress() string {
	return m.enginePlan()
}

// reloadStatusLineConfig re-reads the `statusLine` and `title` blocks from
// settings.json and refreshes the caches.
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

// statusLineScriptActive reports whether a custom status-line script is
// configured and enabled.
func (m *runMode) statusLineScriptActive() bool {
	if m.statusLineCfg.Active() {
		return true
	}
	return m.statusLineExternal
}
