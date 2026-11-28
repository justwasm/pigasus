// trajectoryview.go — the panel's Trajectory view: every step of the
// conversation.
//
// # What it answers
//
// "What has this session actually done." The transcript shows the turns
// as they were rendered; it does not show the steps a compaction folded
// into a summary, the thinking that preceded a reply, or the raw
// arguments a tool was called with. Those are the conversation's steps,
// and they survive in the store after the transcript has moved on.
//
// # Where the steps come from
//
// `convstore.BuildSteps` is the one place that assembles them — the
// compaction path, the rewind picker and the workflow tool all read it,
// and this view reads the same list. The step's own fields carry what
// the view prints: `ToolPrefix` + `ToolSummary` are the collapsed line
// the reference itself stored (`render_info`), `ToolArgs` and
// `ToolResult` are the body, `Thinking` is the reasoning. Nothing here
// re-derives a summary from a raw string; that would be a second
// answer to "what does this step say".
//
// # Folding
//
// A step's collapsed line is what the store already abbreviated, and its
// body is what the store kept verbatim. A tool result of 40 KB is one
// fold away, not in the way — which is the difference between a
// trajectory a user can read and a log dump.
package debugpanel

import (
	"fmt"
	"strings"

	"github.com/justwasm/pigasus/internal/core/tokenfmt"
	"github.com/justwasm/pigasus/palette"
)

// TrajKind classifies a step for the view's markers.
type TrajKind string

const (
	TrajUser       TrajKind = "user"
	TrajThought    TrajKind = "think"
	TrajReply      TrajKind = "reply"
	TrajTool       TrajKind = "tool"
	TrajCheckpoint TrajKind = "checkpoint"
	TrajNotice     TrajKind = "notice"
	TrajError      TrajKind = "error"
)

// TrajStep is one step of the conversation, as the view prints it.
type TrajStep struct {
	// Index is the step's own index in the trajectory.
	Index int64
	Kind  TrajKind
	// Summary is the collapsed one-line form.
	Summary string
	// Detail is the body shown when the step is open. A step whose body
	// lives in cold storage carries none until the host hydrates it, and
	// sets ColdTo say so.
	Detail []string
	// Tokens is the step's share of the conversation, when the store or
	// the accounting knows it; 0 leaves the column empty.
	Tokens int
	// Cold marks a step whose body was folded out of memory.
	Cold bool
	// ColdNote describes what hydration would read, e.g. "2.8 KB on disk".
	ColdNote string
	// Folded marks a step a checkpoint already summarized: it is no
	// longer in the context window, and saying so is the point.
	Folded bool
}

// TrajTurn groups the steps belonging to one user-model exchange.
type TrajTurn struct {
	// Index is the turn's 1-based sequence number.
	Index int
	// UserPrompt is the first line of the prompt that began the turn.
	UserPrompt string
	// Steps are the steps that occurred within this turn.
	Steps []TrajStep
	// Tokens is the sum of known tokens across steps in this turn.
	Tokens int
	// Tools is the number of tool calls in this turn.
	Tools int
	// Folded marks a turn where all steps were summarized into a checkpoint.
	Folded bool
}

// TrajectoryState is everything the Trajectory view prints.
type TrajectoryState struct {
	TurnsList   []TrajTurn
	Steps       []TrajStep
	Turns       int
	Checkpoints int
	ColdChunks  int
	// Note is an optional caveat under the heading.
	Note string
	// Width is the terminal width, for clipping the bodies.
	Width int
}

// trajectoryHeading counts the steps by kind, so the heading says what
// is in the list rather than only how long it is.
//
// The counts are taken from whichever list the view is rendering: a
// grouped trajectory carries its steps inside its turns, and reading only
// the flat slice would head a full list with "0 steps".
func (st TrajectoryState) trajectoryHeading() string {
	steps := st.Steps
	turns := st.Turns
	if len(st.TurnsList) > 0 {
		turns = len(st.TurnsList)
		if len(steps) == 0 {
			for _, turn := range st.TurnsList {
				steps = append(steps, turn.Steps...)
			}
		}
	}
	var folded, cold int
	for _, s := range steps {
		if s.Folded {
			folded++
		}
		if s.Cold {
			cold++
		}
	}
	// Every count is pluralised by countLabel, so the heading never prints
	// "1 checkpoint(s)" — the same parenthesised hedge an unset field gets,
	// which is exactly what makes a reader distrust the numbers next to it.
	parts := []string{countLabel(len(steps), "step")}
	if folded > 0 {
		parts = append(parts, fmt.Sprintf("%d folded", folded))
	}
	if turns > 0 {
		parts = append(parts, countLabel(turns, "turn"))
	}
	if st.Checkpoints > 0 {
		parts = append(parts, countLabel(st.Checkpoints, "checkpoint"))
	}
	if st.ColdChunks > 0 {
		parts = append(parts, countLabel(st.ColdChunks, "cold chunk"))
	}
	return strings.Join(parts, "  ·  ")
}

// TrajectoryHeaderLines renders the view's heading.
func TrajectoryHeaderLines(st TrajectoryState) []string {
	head := "\x1b[1m" + palette.SubtleOpen + "└ Trajectory" + "\x1b[0m"
	head += palette.DimGrayOpen + "  ·  " + st.trajectoryHeading() + palette.DimGrayClose
	lines := []string{head}
	if st.Note != "" {
		lines = append(lines, "   "+palette.DimGrayOpen+st.Note+palette.DimGrayClose)
	}
	return lines
}

// trajMarker is the glyph a step's kind carries.
//
// The glyphs are read from the palette at call time rather than cached in
// package vars, so a colour-scheme change is picked up without this
// package registering a refresh hook of its own.
func trajMarker(k TrajKind) string {
	switch k {
	case TrajUser:
		return palette.UserFgOpen + "◉" + palette.UserFgClose
	case TrajReply:
		return "\x1b[1m" + palette.SubtleOpen + "◇\x1b[0m"
	case TrajThought:
		return palette.DimGrayOpen + "◆" + palette.DimGrayClose
	case TrajTool:
		return "\x1b[1m" + palette.SubtleOpen + "●\x1b[0m"
	case TrajCheckpoint:
		return palette.DimGrayOpen + "⊠" + palette.DimGrayClose
	case TrajError:
		return "\x1b[38;2;220;80;80m✗\x1b[39m"
	default:
		return palette.SubtleOpen + "⛁" + palette.DimGrayClose
	}
}

// TrajectoryRows renders the steps as foldable rows.
//
// When TurnsList is set, each turn is a top-level foldable row, and each
// step inside the turn is an indented child row. When a turn is closed,
// its steps collapse; when open, its steps are selectable and can themselves
// be folded. If TurnsList is empty, steps are rendered as flat rows.
func TrajectoryRows(st TrajectoryState) []FoldRow {
	if len(st.TurnsList) > 0 {
		rows := make([]FoldRow, 0, len(st.TurnsList))
		for _, turn := range st.TurnsList {
			rows = append(rows, turnRow(turn, st.Width))
		}
		return rows
	}
	rows := make([]FoldRow, 0, len(st.Steps))
	for _, s := range st.Steps {
		rows = append(rows, trajRow(s, st.Width))
	}
	return rows
}

// CountLabel renders n of a kind, singular when there is one.
//
// It is exported because the host counts the same things one level down,
// in the reader's heading: a second pluralisation there would print
// "1 steps" the day the two spellings drifted apart.
func CountLabel(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// countLabel is the package's spelling of CountLabel, kept short because
// it is called inside format strings.
func countLabel(n int, word string) string { return CountLabel(n, word) }

func turnRow(turn TrajTurn, width int) FoldRow {
	turnIdx := palette.DimGrayOpen + fmt.Sprintf("[Turn %2d] ", turn.Index) + palette.DimGrayClose
	glyph := "\x1b[1m" + palette.SubtleOpen + "◇\x1b[0m "

	prompt := turn.UserPrompt
	if prompt == "" {
		prompt = "(session start)"
	}
	const maxPrompt = 36
	if len(prompt) > maxPrompt {
		prompt = prompt[:maxPrompt-3] + "..."
	}

	parts := []string{countLabel(len(turn.Steps), "step")}
	if turn.Tools > 0 {
		parts = append(parts, countLabel(turn.Tools, "tool"))
	}
	info := palette.DimGrayOpen + "  ·  " + strings.Join(parts, " · ") + palette.DimGrayClose

	tokens := ""
	if turn.Tokens > 0 {
		tokens = palette.DimGrayOpen + padLeft(tokenfmt.K(turn.Tokens)+" tok", 10) + palette.DimGrayClose
	}

	text := turnIdx + glyph + prompt + info + tokens
	if turn.Folded {
		text += "  " + palette.DimGrayOpen + "(summarized)" + palette.DimGrayClose
	}

	children := make([]FoldRow, 0, len(turn.Steps))
	for _, s := range turn.Steps {
		children = append(children, trajRow(s, width))
	}

	return FoldRow{
		ID:       fmt.Sprintf("traj/turn/%d", turn.Index),
		Text:     text,
		Children: children,
	}
}

func trajRow(s TrajStep, width int) FoldRow {
	idx := palette.DimGrayOpen + fmt.Sprintf("[#%3d] ", s.Index) + palette.DimGrayClose
	label := trajMarker(s.Kind) + " " + kindColumn(s.Kind) + " " + s.Summary

	tokens := ""
	if s.Tokens > 0 {
		tokens = palette.DimGrayOpen + padLeft(tokenfmt.K(s.Tokens)+" tok", 9) + palette.DimGrayClose
	}

	row := FoldRow{
		ID:   fmt.Sprintf("traj/%d", s.Index),
		Text: idx + label + tokens,
		Cold: s.Cold,
	}

	// A folded step says so on its own line, once: the point of the
	// marker is that the step is no longer in the window.
	if s.Folded {
		row.Text += "  " + palette.DimGrayOpen + "(summarized)" + palette.DimGrayClose
	}

	switch {
	case s.Cold:
		note := s.ColdNote
		if note == "" {
			note = "body not in memory"
		}
		row.Detail = []string{clipPreview("cold · "+note+" · enter to load", width)}
	case len(s.Detail) > 0:
		for _, d := range s.Detail {
			row.Detail = append(row.Detail, clipPreview(d, width))
		}
	}
	return row
}

// kindColumn pads the kind to a fixed width so the summaries align.
func kindColumn(k TrajKind) string {
	name := string(k)
	for len(name) < 10 {
		name += " "
	}
	return palette.DimGrayOpen + name + palette.DimGrayClose
}

// TrajLegend is the view's key hint.
//
// The entries are ordered most- to least-important, because a narrow
// terminal drops them from the tail: closing the surface has to survive a
// split pane, and cycling to the next view does not.
func TrajLegend() string {
	return "enter open/read  ·  v read full  ·  esc close  ·  o expand  ·  p collapse  ·  tab next view"
}

// WireLegend is the Wire view's key hint, ordered like TrajLegend's.
func WireLegend() string {
	return "enter open/read  ·  v read full  ·  esc close  ·  s size/order  ·  o expand  ·  p collapse  ·  tab next view"
}
