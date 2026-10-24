package cliapp

import (
	"fmt"
	"sync/atomic"

	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/contextpanel"
	"github.com/justwasm/pigasus/internal/rewindpanel"
)

// contextGridRow renders one row of the /context panel's 44-cell `□`
// grid with the right-side descriptor plain (default fg). The cells use
// dim gray 241, matching ppref.context.empty.raw.txt rows 15–21. There
// is no SGR between the last cell and the descriptor (so the right-side
// text inherits the default fg, which is also what REF does).
func contextGridRow(text string) string {
	return contextpanel.GridRow(text)
}

// contextGridRowWidth is the terminal-width-aware form: the reference
// paints one `□` cell per five columns (24 at 120x40, 40 at 200x50, 44
// at 220x50). A zero width keeps the historical 220-column shape for
// tests and callers that have not measured a window yet.
func contextGridRowWidth(text string, pad, width int) string {
	return contextpanel.GridRowWidth(text, pad, width)
}

// contextGridRowPadded is contextGridRow with explicit padding width.
// REF uses 5 spaces for most rows and 6 for the `(0.0%)` row, which
// shifts the percentage one column further right (ppref row 16).
func contextGridRowPadded(text string, pad int) string {
	return contextpanel.GridRowPadded(text, pad)
}

func (m *runMode) contextGridRow(text string) string {
	return contextGridRowWidth(text, 5, m.width)
}

func (m *runMode) contextGridRowPadded(text string, pad int) string {
	return contextGridRowWidth(text, pad, m.width)
}

// panelContextLines renders the /context usage panel. The layout was
// recovered from /tmp/agycmp/panels/ppref.context.empty.raw.txt on an
// empty session; cycle 224 (TODO §4 follow-up to cycle 190) extends
// it to non-empty sessions.
func (m *runMode) panelContextLines() []string {
	model := m.model
	if model == "" {
		model = "Gemini 3.8 Flash (High)"
	}
	// Cycle 289: a turn in flight has a third layout. 1.4.3 still shows
	// "Estimated usage (awaiting generation)", only the user/agent/tool
	// rows, and one grid cell per non-empty category — the system
	// categories appear once the turn completes.
	if m.streaming {
		return m.panelContextStreamingLines(model, contextpanel.MaxContextTokens)
	}
	if len(m.session) == 0 {
		return m.panelContextEmptyLines(model)
	}
	return m.panelContextLiveLines(model, contextpanel.MaxContextTokens)
}

// contextTailLines is the shared bottom of every /context state: the
// auto-loaded system files, the Related row and the key hint.
func contextTailLines() []string {
	return []string{
		"",
		"System files · auto-loaded",
		"\x1b[38;2;101;123;131m └ ~/.gemini/GEMINI.md\x1b[39m",
		"\x1b[38;2;101;123;131m └ ~/.gemini/skills/\x1b[39m",
		"\x1b[38;2;101;123;131m └ ~/.gemini/antigravity/skills/\x1b[39m",
		"\x1b[38;2;101;123;131m └ .agents/agents/\x1b[39m",
		"",
		"\x1b[38;2;101;123;131mRelated: /artifact · /skill · /rewind\x1b[39m",
		panelDotHint("  ", "↑/↓", "Scroll", "pgup/pgdown", "Page",
			"ctrl+end", "Bottom", "ctrl+home", "Top", "esc", "Close"),
	}
}

// panelContextEmptyLines is the cycle-190 layout verbatim.
func (m *runMode) panelContextEmptyLines(model string) []string {
	lines := append([]string{contextpanel.Header}, contextpanel.EmptyGridLines(model, m.width)...)
	return append(lines, contextTailLines()...)
}

type contextUsageRow = contextpanel.Row

func streamingContextRows(model string, used, free, contextMax, user, agent, tool int) []contextUsageRow {
	return contextpanel.StreamingRows(model, used, free, contextMax, user, agent, tool)
}

// panelContextStreamingLines is the in-flight layout.
func (m *runMode) panelContextStreamingLines(model string, contextMax int) []string {
	user, agent, tool := m.streamingTokenCounts()
	lines := append([]string{contextpanel.Header}, contextpanel.StreamingGridLines(model, m.width, contextMax, user, agent, tool)...)
	return append(lines, contextTailLines()...)
}

// streamingTokenCounts is sessionTokenCounts adjusted for the step that is still in flight.
func (m *runMode) streamingTokenCounts() (user, agent, tool int) {
	user, _, tool = m.sessionTokenCounts()
	user = contextpanel.AdjustStreamingUserTokens(m.session, user)
	completed := atomic.LoadInt64(&m.sessionCompletionTokens)
	lastTurn := atomic.LoadInt64(&m.lastTurnCompletionTokens)
	agent = contextpanel.StreamingAgentTokens(completed, lastTurn, len(m.liveReplyBuf))
	return user, agent, tool
}

func contextLiveFill(sysPrompt, user, agent, tool, sysTools, skills, subagents int) []string {
	return contextpanel.LiveFill(sysPrompt, user, agent, tool, sysTools, skills, subagents)
}

func contextLiveRows(model string, used, free, contextMax int, user, agent, tool, sysPrompt, sysTools, skills, subagents int) []contextUsageRow {
	return contextpanel.LiveRows(model, used, free, contextMax, user, agent, tool, sysPrompt, sysTools, skills, subagents)
}

// panelContextLiveLines is the non-empty branch.
func (m *runMode) panelContextLiveLines(model string, contextMax int) []string {
	user, agent, tool := m.sessionTokenCounts()
	sysPrompt, skills, subagents := m.promptSectionTokens()
	sysTools := m.toolTokenEstimate()
	lines := append([]string{contextpanel.Header}, contextpanel.LiveGridLines(
		model, m.width, contextMax, user, agent, tool, sysPrompt, sysTools, skills, subagents,
	)...)
	return append(lines, contextTailLines()...)
}

func (m *runMode) toolTokenEstimate() int {
	return contextpanel.ToolTokenEstimate(m.wireToolDecls())
}

func (m *runMode) wireToolDecls() []backend.ToolDecl {
	return append([]backend.ToolDecl(nil), m.tools...)
}

func contextTokenLabel(n int) string {
	return contextpanel.TokenLabel(n)
}

func contextGridRowFilled(text string, pad, cols int, fill []string, offset int) string {
	return contextpanel.GridRowFilled(text, pad, cols, fill, offset)
}

func (m *runMode) sessionTokenCounts() (user, agent, tool int) {
	user = contextpanel.UserMessageTokens(m.session)
	completed := atomic.LoadInt64(&m.sessionCompletionTokens)
	lastTurn := atomic.LoadInt64(&m.lastTurnCompletionTokens)
	agent = contextpanel.AgentTokens(completed, lastTurn)
	var results []string
	for _, b := range m.blocks {
		for _, s := range b.steps {
			if s.kind == stepKindTool {
				results = append(results, s.result)
			}
		}
	}
	for _, s := range m.liveSteps {
		if s.kind == stepKindTool {
			results = append(results, s.result)
		}
	}
	tool = contextpanel.ToolResultTokens(results...)
	return user, agent, tool
}

// rewindStep is one row in /rewind's picker. userIdx is the m.session
// index of the user message that anchors the step; endIdx is the slice
// index AFTER the last message the step owns (one past the trailing
// assistant / tool-result chain).
type rewindStep = rewindpanel.Step

// rewindSteps breaks a session into turn steps starting with a user message.
func rewindSteps(messages []backend.Message) []rewindStep {
	return rewindpanel.Steps(messages)
}

// rewindStepHeight is the vertical budget /rewind's picker consumes at
// the current terminal height: title (1) + blank (1) + rows + blank (1)
// + counter (1) + keyboard (1) = 5 chrome rows around the list. Matches
// the cycle-189 height-less default of 17 visible rows; falls back to
// the same when m.height is 0 (test runs, alt-screen-not-yet-settled).
func rewindStepHeight(h int) int {
	return rewindpanel.StepHeight(h)
}

func rewindClampWindow(m *runMode, total, visible int) int {
	if m.panel.window < 0 {
		m.panel.window = 0
	}
	if m.panel.cursor < 0 {
		m.panel.cursor = 0
	}
	if max := total - visible; m.panel.window > max && max >= 0 {
		m.panel.window = max
	}
	if m.panel.cursor > total-1 {
		m.panel.cursor = total - 1
	}
	return m.panel.window
}

func rewindRenderStepRows(steps []rewindStep, cursor int) []string {
	return rewindpanel.RenderStepRows(steps, cursor, cBlueOpen, cBlueClose, panelGray238, panelGray238C)
}

// panelRewindLines renders the /rewind picker. Cycle 189 shipped the
// empty-state form (literal `(current)` row); cycle 222 fills in the
// non-empty form — one row per user-anchored turn, newest first, with
// the cursor row in brand blue and a `(current)` annotation on the
// most recent turn.
func (m *runMode) panelRewindLines() []string {
	steps := rewindSteps(m.session)
	if len(steps) == 0 {
		return []string{
			panelTitleOpen + "Rewind Conversation" + panelTitleClose,
			"",
			cBlueOpen + "> (current)" + cBlueClose,
			"",
			panelKeyboardHint("↑/↓", "Navigate", "enter", "Select", "esc", "Cancel"),
		}
	}
	visible := rewindStepHeight(m.height)
	total := len(steps)
	if visible > total {
		visible = total
	}
	window := rewindClampWindow(m, total, visible)
	allRows := rewindRenderStepRows(steps, m.panel.cursor)

	lines := []string{
		panelTitleOpen + "Rewind Conversation" + panelTitleClose,
		"",
	}
	lines = append(lines, allRows[window:window+visible]...)
	lines = append(lines, "")
	if total > visible {
		lines = append(lines, rewindStepCounter(window+1, window+visible, total))
	}
	lines = append(lines, panelKeyboardHint("↑/↓", "Navigate",
		"enter", "Select", "esc", "Cancel"))
	return lines
}

// rewindStepCounter renders the dim gray "(a–b of K turns)" counter the
// /rewind picker shows in its footer when the conversation has more steps
// than fit in the visible window. Same colour code as viewerCounter (line
// 2164) but the noun is "turns" — the picker never has documents.
func rewindStepCounter(start, end, total int) string {
	return rewindpanel.StepCounter(start, end, total, cDimGrayOpen, cDimGrayClose)
}

// panelRewindPickStep is the cycle-222 Enter handler for /rewind. The
// safe stub the cycle brief allowed: the picker emits a `Rewound to
// step N` line via slashRespond's REF-style echo format, the panel
// closes silently (same exit-notice=false set as /mcp /feedback), and
// m.session is truncated so a follow-up prompt no longer carries the
// dropped user+assistant pair. Doing the transcript edit (instead of a
// no-op) keeps the existing /rewind slash command and the picker in
// step — rewindSession pops only the trailing pair, while this routine
// pops trailing pairs until the picked step's user message is the
// trailing user message. Returns the trimmed count for the echo line.
func (m *runMode) panelRewindPickStep() int {
	steps := rewindSteps(m.session)
	if len(steps) == 0 {
		// Empty form — Enter is a no-op (matches cycle-189's "no rewind
		// target" comment). Esc still handles silent close.
		return 0
	}
	cur := m.panel.cursor
	if cur < 0 || cur >= len(steps) {
		cur = 0
	}
	// The picked step's NEW end boundary — keep everything up to and
	// including the step's trailing assistant / tool-result chain (one
	// past the user message). EndIdx is the slice index immediately
	// after the last message the step owns (the trailing assistant
	// message in the simple user/assistant case).
	target := steps[cur].EndIdx
	removed := len(m.session) - target
	if removed <= 0 {
		// Already at or before the picked step (cursor on the current
		// turn, which is the default). No-op: no truncation, no echo.
		return 0
	}
	m.session = m.session[:target]
	if cur == 0 {
		m.slashRespond("/rewind",
			fmt.Sprintf("Rewound to step %d (current, no messages removed)", cur+1))
	} else {
		m.slashRespond("/rewind",
			fmt.Sprintf("Rewound to step %d (%d message(s) removed)", cur+1, removed))
	}
	return removed
}
