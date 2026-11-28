// panel.go — the /context usage panel's rows.
//
// # What it is
//
// The reference (`agy`) answers `/context` with a usage panel: a grid
// of `□` cells whose right-hand descriptors carry the session's token
// counts, followed by the auto-loaded system files, the "Related:"
// rows and the scroll legend.
//
// The panel has three layouts. The empty session (cycle 190) shows
// the "awaiting generation" placeholder and no categories; the turn in
// flight (cycle 289) shows the user/agent/tool rows only; the
// completed turn (cycle 224) shows every category.
//
// # Where it lives
//
// The rows live here: the three layouts and the shared tail. The host
// gathers the session's facts — the token counts and the model name —
// and hands them over as a State; the panel owns nothing else.
//
// The panel has no state of its own: it is rebuilt on every draw from
// the State the host hands it, so a stale count is never shown.

package contextpanel

import (
	"fmt"

	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/palette"
)

// relatedHint is the reference's own "Related:" hint on a single line.
const relatedHint = `Related: /artifact · /skill · /rewind`

// Mode is the layout the /context panel draws.
//
// The mode is the host's answer to "what does this session look like
// right now" — an empty session, a turn in flight, or a completed
// turn.
type Mode int

const (
	// ModeEmpty is the empty-session layout: the "awaiting generation"
	// placeholder and no categories (cycle 190).
	ModeEmpty Mode = iota

	// ModeStreaming is the in-flight layout: the user/agent/tool rows
	// and one grid cell per non-empty category (cycle 289).
	ModeStreaming

	// ModeLive is the completed-turn layout: every category, including
	// the system ones (cycle 224).
	ModeLive
)

// State is the session's facts the panel's rows are a function of.
//
// Every field is read at draw time, so the panel shows the session as
// it is now and never a stale copy.
type State struct {
	// Model is the session's model display name, e.g. "Gemini 3.8
	// Flash (High)". It is the right-hand descriptor of the title
	// row.
	Model string

	// Width is the terminal's width: the grid paints one cell per
	// five columns.
	Width int

	// ContextMax is the total context window size for the model.
	// When zero or negative, MaxContextTokens (1.0M) is used as default.
	ContextMax int

	// Mode picks the layout.
	Mode Mode

	// The token counts, by category.
	//
	// The first three are the conversation rows: they are always shown.
	User  int
	Agent int
	Tool  int

	// The completed turn's system categories. They are ignored by the
	// empty and the in-flight layouts.
	SystemPrompt   int
	SystemTools    int
	SystemOverhead int

	// The completed turn's customization categories. A category that
	// consumes no tokens is not a row.
	Rules     int
	Skills    int
	Subagents int
	Workflows int
	Plugins   int
	MCPTools  int

	// SystemMessages counts the system-generated messages in the
	// conversation.
	SystemMessages int

	// CheckpointBuffer is the token count the session's compaction
	// checkpoint holds. It is reported outside the grid and is not part
	// of the usage total.
	CheckpointBuffer int

	// CompactJustHappened indicates that a compaction occurred during this or the
	// immediately preceding turn, showing "Estimated after compaction · updates next turn".
	CompactJustHappened bool

	// AutoCompactTokens is the active auto-compaction threshold in tokens (0 = server default).
	AutoCompactTokens int

	// AutoCompactOverrideSpec is the raw AGY_CLI_AUTO_COMPACT_OVERRIDE value when overridden.
	AutoCompactOverrideSpec string

	// Checkpoint telemetry for the Checkpoints (%d) section:
	TotalCheckpoints      int
	ActiveCheckpointIndex int
	ActiveCheckpointStart int
	ActiveCheckpointEnd   int
	HistoricalCheckpoints int

	// FileSections represents the contextual files breakdown (artifacts, skills, rules, system).
	FileSections []FileSection

	// MaxRows is the height limit for scrolling when displayed in the TUI viewport.
	MaxRows int

	// ScrollOffset is the vertical scroll offset in rows.
	ScrollOffset int

	// The *Estimated flags mark a count the host could not measure and
	// filled from a built-in figure. The row shows a leading "~" so the
	// user can tell a constant from a measurement.
	SystemPromptEstimated   bool
	SystemToolsEstimated    bool
	RulesEstimated          bool
	SkillsEstimated         bool
	SubagentsEstimated      bool
	SystemMessagesEstimated bool
}

// TotalUsed returns the unclamped sum of active token categories counted in usage.
func (st State) TotalUsed() int {
	return st.breakdown().TotalUsed()
}

// breakdown is the panel's view of the session's counts: the categories
// the reference reports, in its order.
func (st State) breakdown() Breakdown {
	ctxMax := st.ContextMax
	if ctxMax <= 0 {
		ctxMax = MaxContextTokens
	}
	return Breakdown{
		Model:            st.Model,
		ContextMax:       ctxMax,
		User:             st.User,
		Agent:            st.Agent,
		Tool:             st.Tool,
		SystemPrompt:     st.SystemPrompt,
		SystemTools:      st.SystemTools,
		SystemOverhead:   st.SystemOverhead,
		Rules:            st.Rules,
		Skills:           st.Skills,
		Subagents:        st.Subagents,
		Workflows:        st.Workflows,
		Plugins:          st.Plugins,
		MCPTools:         st.MCPTools,
		SystemMessages:   st.SystemMessages,
		CheckpointBuffer: st.CheckpointBuffer,

		SystemPromptEstimated:   st.SystemPromptEstimated,
		SystemToolsEstimated:    st.SystemToolsEstimated,
		RulesEstimated:          st.RulesEstimated,
		SkillsEstimated:         st.SkillsEstimated,
		SubagentsEstimated:      st.SubagentsEstimated,
		SystemMessagesEstimated: st.SystemMessagesEstimated,
	}
}

// AllLines returns the complete unwindowed rendered lines of the /context panel.
func AllLines(st State) []string {
	var lines []string
	lines = append(lines, Header)
	lines = append(lines, st.gridLines()...)
	lines = append(lines, "")

	if st.TotalCheckpoints > 0 || st.ActiveCheckpointIndex > 0 {
		cpLines := RenderCheckpoints(st.TotalCheckpoints, st.ActiveCheckpointIndex, st.ActiveCheckpointStart, st.ActiveCheckpointEnd, st.HistoricalCheckpoints)
		if len(cpLines) > 0 {
			lines = append(lines, cpLines...)
			lines = append(lines, "")
		}
	}

	if len(st.FileSections) > 0 {
		for _, sec := range st.FileSections {
			if fLines := RenderFileSection(sec); len(fLines) > 0 {
				lines = append(lines, fLines...)
				lines = append(lines, "")
			}
		}
	} else {
		lines = append(lines, defaultSystemFileLines()...)
		lines = append(lines, "")
	}

	lines = append(lines, relatedRows()...)
	lines = append(lines, tuilib.PanelDotHint("  ", "↑/↓", "Scroll", "pgup/pgdown", "Page",
		"ctrl+end", "Bottom", "ctrl+home", "Top", "esc", "Close"))
	return lines
}

func defaultSystemFileLines() []string {
	return []string{
		"System files · auto-loaded",
		" " + palette.DimGrayOpen + "└ ~/.gemini/GEMINI.md\x1b[39m",
		" " + palette.DimGrayOpen + "└ ~/.gemini/skills/\x1b[39m",
		" " + palette.DimGrayOpen + "└ ~/.gemini/antigravity/skills/\x1b[39m",
		" " + palette.DimGrayOpen + "└ .agents/agents/\x1b[39m",
	}
}

// Lines renders the /context usage panel, windowed if MaxRows is set.
//
// The window is the one MaxContextScroll computes: the head stays pinned
// above it and the pager and the two footer rows stay pinned below it, so
// the last body row is reachable exactly when the body is longer than the
// window. The two used to be different sums, which is why a panel that
// looked scrollable could refuse to reach its tail.
func Lines(st State) []string {
	all := AllLines(st)
	if st.MaxRows <= 0 || len(all) <= st.MaxRows {
		return all
	}
	headLen, tailLen := pinnedRows(st)
	body := all[headLen : len(all)-tailLen]

	bodyAvail := bodyRows(st)

	offset := st.ScrollOffset
	if offset < 0 {
		offset = 0
	}
	if max := len(body) - bodyAvail; offset > max {
		offset = max
	}
	if offset < 0 {
		offset = 0
	}
	end := offset + bodyAvail
	if end > len(body) {
		end = len(body)
	}

	visibleBody := body[offset:end]
	pager := fmt.Sprintf("  (%d–%d of %d lines)", offset+1, end, len(all))

	result := make([]string, 0, len(visibleBody)+6)
	result = append(result, all[:headLen]...)
	result = append(result, visibleBody...)
	result = append(result, pager)
	result = append(result, "")
	result = append(result, all[len(all)-tailLen:]...)
	return result
}

// gridLines is the layout's own block: the descriptor rows painted
// over the usage grid.
func (st State) gridLines() []string {
	ctxMax := st.ContextMax
	if ctxMax <= 0 {
		ctxMax = MaxContextTokens
	}
	cols := ColsFor(st.Width)
	switch st.Mode {
	case ModeStreaming:
		cats := st.breakdown().Categories()
		if len(cats) > 3 {
			return st.renderLiveGrid(ctxMax, cols, cats, "Estimated usage (awaiting generation)")
		}
		used, free := ClampedUsage(ctxMax, st.User+st.Agent+st.Tool)
		rows := insertAutoCompactRows(StreamingRows(st.Model, used, free, ctxMax, st.User, st.Agent, st.Tool), st.AutoCompactTokens, st.AutoCompactOverrideSpec)
		totalDots := len(rows) * cols
		streamCats := []Category{
			{Tokens: st.User, Cell: UserGlyph},
			{Tokens: st.Agent, Cell: AgentGlyph},
			{Tokens: st.Tool, Cell: ToolGlyph},
		}
		fill := ProportionalFill(streamCats, ctxMax, totalDots)
		if len(fill) == 0 {
			fill = StreamingFill(st.User, st.Agent, st.Tool)
		}
		return RenderGridLines(rows, fill, cols)
	case ModeEmpty:
		cats := st.breakdown().Categories()
		if len(cats) > 3 {
			return st.renderLiveGrid(ctxMax, cols, cats, "Estimated usage (awaiting generation)")
		}
		rows := insertAutoCompactRows(EmptyRowsWithLimit(st.Model, ctxMax), st.AutoCompactTokens, st.AutoCompactOverrideSpec)
		return RenderGridLines(rows, nil, cols)
	case ModeLive:
		fallthrough
	default:
		preamble := "Token usage by category"
		if st.CompactJustHappened {
			preamble = "Estimated after compaction · updates next turn"
		}
		return st.renderLiveGrid(ctxMax, cols, st.breakdown().Categories(), preamble)
	}
}

func (st State) renderLiveGrid(ctxMax, cols int, cats []Category, preamble string) []string {
	total := 0
	for _, c := range cats {
		total += c.Tokens
	}
	used, free := ClampedUsage(ctxMax, total)
	rows := insertAutoCompactRows(
		LiveRowsWithPreambleAndCheckpoint(st.Model, used, free, ctxMax, cats, preamble, st.CheckpointBuffer),
		st.AutoCompactTokens,
		st.AutoCompactOverrideSpec,
	)
	totalDots := len(rows) * cols
	fill := ProportionalFill(cats, ctxMax, totalDots)
	if len(fill) == 0 {
		fill = LiveFill(cats)
	}
	return RenderGridLines(rows, fill, cols)
}

// TailLines is the shared bottom of every /context state: the
// auto-loaded system files, the Related rows and the scroll legend.
func TailLines() []string {
	rows := []string{""}
	rows = append(rows, defaultSystemFileLines()...)
	rows = append(rows, "")
	rows = append(rows, relatedRows()...)
	return append(rows, tuilib.PanelDotHint("  ", "↑/↓", "Scroll", "pgup/pgdown", "Page",
		"ctrl+end", "Bottom", "ctrl+home", "Top", "esc", "Close"))
}

// relatedRows formats the reference's hint on a single row.
func relatedRows() []string {
	return []string{palette.DimGrayOpen + relatedHint + "\x1b[39m"}
}
