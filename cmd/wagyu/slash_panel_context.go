package main

import (
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/justwasm/pigasus/internal/backend"
)

// contextGridRow renders one row of the /context panel's 44-cell `□`
// grid with the right-side descriptor plain (default fg). The cells use
// dim gray 241, matching ppref.context.empty.raw.txt rows 15–21. There
// is no SGR between the last cell and the descriptor (so the right-side
// text inherits the default fg, which is also what REF does).
func contextGridRow(text string) string {
	return contextGridRowPadded(text, 5)
}

// contextGridRowWidth is the terminal-width-aware form: the reference
// paints one `□` cell per five columns (24 at 120x40, 40 at 200x50, 44
// at 220x50). A zero width keeps the historical 220-column shape for
// tests and callers that have not measured a window yet.
func contextGridRowWidth(text string, pad, width int) string {
	cols := width / 5
	if width <= 0 {
		cols = 44
	}
	if cols < 1 {
		cols = 1
	}
	return contextGridRowPaddedCols(text, pad, cols)
}

// contextGridRowPadded is contextGridRow with explicit padding width.
// REF uses 5 spaces for most rows and 6 for the `(0.0%)` row, which
// shifts the percentage one column further right (ppref row 16).
func contextGridRowPadded(text string, pad int) string {
	return contextGridRowPaddedCols(text, pad, 44)
}

func contextGridRowPaddedCols(text string, pad, cols int) string {
	var b strings.Builder
	for i := 0; i < cols; i++ {
		if i > 0 {
			b.WriteString(" ")
		}
		b.WriteString("\x1b[38;2;101;123;131m□\x1b[39m")
	}
	for i := 0; i < pad; i++ {
		b.WriteString(" ")
	}
	b.WriteString(text)
	return b.String()
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
	const contextMax = 1_000_000 // 1.0M-token window (REF keeps the magic number in the panel).
	// Cycle 289: a turn in flight has a third layout. 1.4.3 still shows
	// "Estimated usage (awaiting generation)", only the user/agent/tool
	// rows, and one grid cell per non-empty category — the system
	// categories appear once the turn completes.
	if m.streaming {
		return m.panelContextStreamingLines(model, contextMax)
	}
	if len(m.session) == 0 {
		return m.panelContextEmptyLines(model)
	}
	return m.panelContextLiveLines(model, contextMax)
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
	lines := []string{
		"\x1b[1m\x1b[38;2;88;110;117m└ Context Usage\x1b[0m",
		m.contextGridRow(model + " · 0/1.0M tokens"),
		m.contextGridRowPadded("(0.0%)", 6),
		m.contextGridRow("\x1b[1m\x1b[38;2;88;110;117mEstimated usage (awaiting generation)\x1b[0m"),
		m.contextGridRow("\x1b[1m\x1b[38;5;26m◉\x1b[0m User messages: 0 tokens (0.0%)"),
		m.contextGridRow("\x1b[38;2;133;153;0m◉\x1b[39m Agent responses: 0 tokens (0.0%)"),
		m.contextGridRow("\x1b[1m\x1b[38;5;172m◉\x1b[0m Tool calls: 0 tokens (0.0%)"),
		m.contextGridRow("\x1b[38;2;101;123;131m□\x1b[39m Free space: 1.0M (100.0%)"),
	}
	return append(lines, contextTailLines()...)
}

// panelContextStreamingLines is the in-flight layout.
func (m *runMode) panelContextStreamingLines(model string, contextMax int) []string {
	user, agent, tool := m.streamingTokenCounts()
	used := user + agent + tool
	if used > contextMax {
		used = contextMax
	}
	free := contextMax - used
	if free < 0 {
		free = 0
	}
	pct := func(x int) string {
		if x <= 0 {
			return "(0.0%)"
		}
		return fmt.Sprintf("(%.1f%%)", float64(x)/float64(contextMax)*100)
	}
	fill := make([]string, 0, 3)
	if user > 0 {
		fill = append(fill, "\x1b[38;2;38;139;210m◉\x1b[39m")
	}
	if agent > 0 {
		fill = append(fill, "\x1b[38;2;133;153;0m◉\x1b[39m")
	}
	if tool > 0 {
		fill = append(fill, "\x1b[38;5;172m◉\x1b[39m")
	}
	rows := []struct {
		text string
		pad  int
	}{
		{fmt.Sprintf("%s · %s/1.0M tokens", model, contextTokenLabel(used)), 5},
		{pct(used), 6},
		{"\x1b[1m\x1b[38;2;88;110;117mEstimated usage (awaiting generation)\x1b[0m", 5},
		{fmt.Sprintf("\x1b[1m\x1b[38;5;26m◉\x1b[0m User messages: %s tokens %s", contextTokenLabel(user), pct(user)), 5},
		{fmt.Sprintf("\x1b[38;2;133;153;0m◉\x1b[39m Agent responses: %s tokens %s", contextTokenLabel(agent), pct(agent)), 5},
		{fmt.Sprintf("\x1b[1m\x1b[38;5;172m◉\x1b[0m Tool calls: %s tokens %s", contextTokenLabel(tool), pct(tool)), 5},
		{fmt.Sprintf("\x1b[38;2;101;123;131m□\x1b[39m Free space: 1.0M %s", pct(free)), 5},
	}
	cols := m.width / 5
	if m.width <= 0 {
		cols = 44
	}
	if cols < 1 {
		cols = 1
	}
	out := []string{"\x1b[1m\x1b[38;2;88;110;117m└ Context Usage\x1b[0m"}
	for i, r := range rows {
		out = append(out, contextGridRowFilled(r.text, r.pad, cols, fill, i*cols))
	}
	return append(out, contextTailLines()...)
}

// streamingTokenCounts is sessionTokenCounts adjusted for the step that is still in flight.
func (m *runMode) streamingTokenCounts() (user, agent, tool int) {
	user, _, tool = m.sessionTokenCounts()
	for i := len(m.session) - 1; i >= 0; i-- {
		msg := m.session[i]
		if msg.Role != "user" {
			continue
		}
		wire := msg.WireContent
		if wire == "" {
			wire = backend.WrapUserContent(msg.Content, "", false, time.Now())
		}
		user -= backend.UserContentTokenCount(wire)
		if n := len(msg.Content) / 4; n > 1 {
			user += n
		} else {
			user += 1
		}
		break
	}
	if user < 0 {
		user = 0
	}
	completed := int(atomic.LoadInt64(&m.sessionCompletionTokens) -
		atomic.LoadInt64(&m.lastTurnCompletionTokens))
	if completed < 0 {
		completed = 0
	}
	agent = completed + len(m.liveReplyBuf)/4
	return user, agent, tool
}

// panelContextLiveLines is the non-empty branch.
func (m *runMode) panelContextLiveLines(model string, contextMax int) []string {
	user, agent, tool := m.sessionTokenCounts()
	sysPrompt, skills, subagents := m.promptSectionTokens()
	sysTools := m.toolTokenEstimate()
	used := user + agent + tool + sysPrompt + sysTools + skills + subagents
	if used > contextMax {
		used = contextMax
	}
	free := contextMax - used
	if free < 0 {
		free = 0
	}
	pct := func(x int) string {
		if x <= 0 {
			return "(0.0%)"
		}
		return fmt.Sprintf("(%.1f%%)", float64(x)/float64(contextMax)*100)
	}

	type gridCat struct {
		tokens int
		cell   string
	}
	gray := func(glyph string) string { return "\x1b[38;2;88;110;117m" + glyph + "\x1b[39m" }
	cats := []gridCat{
		{sysPrompt, gray("◉")},
		{user, "\x1b[38;2;38;139;210m◉\x1b[39m"},
		{agent, "\x1b[38;2;133;153;0m◉\x1b[39m"},
		{tool, "\x1b[38;5;172m◉\x1b[39m"},
		{sysTools, gray("◉")},
		{skills, gray("◉")},
		{subagents, gray("◉")},
	}
	var fill []string
	for _, c := range cats {
		if c.tokens <= 0 {
			continue
		}
		n := (c.tokens + 3999) / 4000
		if n < 1 {
			n = 1
		}
		for i := 0; i < n; i++ {
			fill = append(fill, c.cell)
		}
	}

	rows := []struct {
		text string
		pad  int
	}{
		{fmt.Sprintf("%s · %s/1.0M tokens", model, contextTokenLabel(used)), 5},
		{pct(used), 6},
		{"\x1b[1m\x1b[38;2;88;110;117mToken usage by category\x1b[0m", 5},
		{fmt.Sprintf("\x1b[1m\x1b[38;5;26m◉\x1b[0m User messages: %s tokens %s", contextTokenLabel(user), pct(user)), 5},
		{fmt.Sprintf("\x1b[38;2;133;153;0m◉\x1b[39m Agent responses: %s tokens %s", contextTokenLabel(agent), pct(agent)), 5},
		{fmt.Sprintf("\x1b[1m\x1b[38;5;172m◉\x1b[0m Tool calls: %s tokens %s", contextTokenLabel(tool), pct(tool)), 5},
		{fmt.Sprintf("%s System prompt: %s tokens %s", gray("⛁"), contextTokenLabel(sysPrompt), pct(sysPrompt)), 5},
		{fmt.Sprintf("%s System tools: %s tokens %s", gray("⛁"), contextTokenLabel(sysTools), pct(sysTools)), 5},
		{fmt.Sprintf("%s Skills: %s tokens %s", gray("⛁"), contextTokenLabel(skills), pct(skills)), 5},
		{fmt.Sprintf("%s Subagents: %s tokens %s", gray("⛁"), contextTokenLabel(subagents), pct(subagents)), 5},
		{fmt.Sprintf("\x1b[38;2;101;123;131m□\x1b[39m Free space: 1.0M %s", pct(free)), 5},
	}
	cols := m.width / 5
	if m.width <= 0 {
		cols = 44
	}
	if cols < 1 {
		cols = 1
	}
	out := make([]string, 0, len(rows)+8)
	out = append(out, "\x1b[1m\x1b[38;2;88;110;117m└ Context Usage\x1b[0m")
	for i, r := range rows {
		out = append(out, contextGridRowFilled(r.text, r.pad, cols, fill, i*cols))
	}
	out = append(out, contextTailLines()...)
	return out
}

func (m *runMode) toolTokenEstimate() int {
	b := backend.WireToolsJSON(m.wireToolDecls())
	return len(b) / 3
}

func (m *runMode) wireToolDecls() []backend.ToolDecl {
	return append([]backend.ToolDecl(nil), m.tools...)
}

func contextTokenLabel(n int) string {
	if n >= 999_950 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
	if n >= 1000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return strconv.Itoa(n)
}

func contextGridRowFilled(text string, pad, cols int, fill []string, offset int) string {
	var b strings.Builder
	for i := 0; i < cols; i++ {
		if i > 0 {
			b.WriteString(" ")
		}
		if idx := offset + i; idx >= 0 && idx < len(fill) {
			b.WriteString(fill[idx])
		} else {
			b.WriteString("\x1b[38;2;101;123;131m□\x1b[39m")
		}
	}
	for i := 0; i < pad; i++ {
		b.WriteString(" ")
	}
	b.WriteString(text)
	return b.String()
}

func (m *runMode) sessionTokenCounts() (user, agent, tool int) {
	for _, msg := range m.session {
		if msg.Role != "user" {
			continue
		}
		wire := msg.WireContent
		if wire == "" {
			wire = backend.WrapUserContent(msg.Content, "", false, time.Now())
		}
		user += backend.UserContentTokenCount(wire)
	}
	agent = int(atomic.LoadInt64(&m.sessionCompletionTokens) -
		atomic.LoadInt64(&m.lastTurnCompletionTokens))
	if agent < 0 {
		agent = 0
	}
	for _, b := range m.blocks {
		for _, s := range b.steps {
			if s.kind == stepKindTool {
				tool += backend.ToolResultTokenCount(s.result)
			}
		}
	}
	for _, s := range m.liveSteps {
		if s.kind == stepKindTool {
			tool += backend.ToolResultTokenCount(s.result)
		}
	}
	return user, agent, tool
}
