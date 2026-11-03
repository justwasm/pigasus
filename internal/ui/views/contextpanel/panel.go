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
	"strings"

	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/palette"
)

// relatedHint is the reference's own "Related:" hint, byte for byte.
//
// The reference's string table carries the *two characters* `\` and `n`
// between the three commands
// (`Related: /artifact \n /skill \n /rewind` — a string of the
// reference's own table; the surviving pruned index of that table,
// out/local-macos-1.4.11/strings-index.txt, carries the head
// "Related: /artifact" and, in the context window around it, the tail
// " /skill " and " /rewind", while the full dump is retired), so the hint
// is three rows and not one. The raw string
// literal below keeps that separator exactly as the reference stores it.
const relatedHint = `Related: /artifact \n /skill \n /rewind`

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
}

// breakdown is the panel's view of the session's counts: the categories
// the reference reports, in its order.
func (st State) breakdown() Breakdown {
	return Breakdown{
		Model:            st.Model,
		ContextMax:       MaxContextTokens,
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
	}
}

// Lines renders the /context usage panel.
//
// The panel is the title row, the layout's grid, the checkpoint-buffer
// row and the shared tail.
//
// The checkpoint row is outside the grid: the reference tracks the
// checkpoint's tokens separately, so the summarized history they stand
// in for does not inflate the grid's picture.
func Lines(st State) []string {
	lines := append([]string{Header}, st.gridLines()...)
	if st.CheckpointBuffer > 0 {
		lines = append(lines, CheckpointRow(st.CheckpointBuffer))
	}
	return append(lines, TailLines()...)
}

// gridLines is the layout's own block: the descriptor rows painted
// over the usage grid.
func (st State) gridLines() []string {
	switch st.Mode {
	case ModeStreaming:
		return StreamingGridLines(st.Model, st.Width, MaxContextTokens, st.User, st.Agent, st.Tool)
	case ModeLive:
		return LiveGridLines(st.Model, st.Width, MaxContextTokens, st.breakdown().Categories())
	default:
		return EmptyGridLines(st.Model, st.Width)
	}
}

// TailLines is the shared bottom of every /context state: the
// auto-loaded system files, the Related rows and the scroll legend.
//
// The three layouts differ in their grid only — the rows below the
// grid are the same in all of them.
func TailLines() []string {
	rows := []string{
		"",
		"System files · auto-loaded",
		" " + palette.DimGrayOpen + "└ ~/.gemini/GEMINI.md\x1b[39m",
		" " + palette.DimGrayOpen + "└ ~/.gemini/skills/\x1b[39m",
		" " + palette.DimGrayOpen + "└ ~/.gemini/antigravity/skills/\x1b[39m",
		" " + palette.DimGrayOpen + "└ .agents/agents/\x1b[39m",
		"",
	}
	rows = append(rows, relatedRows()...)
	return append(rows, tuilib.PanelDotHint("  ", "↑/↓", "Scroll", "pgup/pgdown", "Page",
		"ctrl+end", "Bottom", "ctrl+home", "Top", "esc", "Close"))
}

// relatedRows splits the reference's hint on its own two-character
// separator and paints each row dim.
//
// The separator is the literal `\n` the reference's string carries, so
// the split keeps the reference's spaces: the first row ends with a
// space and the other two start with one.
func relatedRows() []string {
	parts := strings.Split(relatedHint, `\n`)
	rows := make([]string, 0, len(parts))
	for _, p := range parts {
		rows = append(rows, palette.DimGrayOpen+p+"\x1b[39m")
	}
	return rows
}
