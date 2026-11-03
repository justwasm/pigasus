// Package statusrow draws the TUI's status row — the single line under
// the prompt that carries the session's model, its mode, its effort
// tier, the running tasks and the hint the user needs right now.
//
// # What it is
//
// The row is the reference's own:
//
//	? for shortcuts                    Gemini 3.8 Flash · high · 2 tasks
//	esc to cancel                      auto · MiniMax M3
//	press ctrl+c again to exit         MiniMax M3
//	⠿  Compacting context… (12s)        MiniMax M3
//
// The left segment is the hint, the right segment is the session's
// identity, and the space between them is the padding. The rules that
// decide the hint are the reference's:
//
//  1. The slash-command popup is up: "esc to cancel" — except in the
//     "/model <query>" mode, which leaves the segment empty.
//  2. A turn is streaming: "esc to cancel".
//  3. Messages are queued: "  Press up to edit queued messages".
//  4. The composer holds something, or the /resume picker is up: no
//     hint at all.
//  5. The bash mode is armed: " activated bash mode · esc to cancel".
//
// Over the hint sit the vim badge, the /help overlay's and the modal
// panels' clearing of the segment, the two-step Ctrl+C confirmation
// and the compaction label — in that order, each one replacing the
// previous.
//
// The right segment ends with the status cells: the agent state, the
// context window's usage and the remaining AI credits. Each one is
// omitted when its fact is absent.
//
// # Where it lives
//
// The row is here: the segments, the rules, the padding arithmetic and
// the compaction label. The host keeps the facts — the model, the
// mode, the effort tier, the running tasks, the compaction state, the
// composer and the pickers — and hands them over as a State.
//
// The row is a view: it takes the facts and writes the line. It has no
// keys and no writes of its own.
package statusrow

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/justwasm/pigasus/palette"
)

// State is what the status row needs to draw itself.
//
// Every field is a fact the host owns; the row decides what the facts
// look like together.
type State struct {
	// Width is the terminal's width in cells.
	//
	// Zero means "unknown": the row lays itself out in the canonical
	// width the host passes in CanonicalWidth.
	Width int
	// CanonicalWidth is the width the row uses when the terminal's
	// width is unknown.
	//
	// It is the same width the session's rules are drawn at, so the
	// host passes its own constant: one number, one owner.
	CanonicalWidth int

	// ---- the left segment ----

	// PickerVisible is true while the slash-command popup is up.
	PickerVisible bool
	// PickerModelMode is true while the popup is in the "/model <query>"
	// mode, which drops the cancel hint.
	PickerModelMode bool
	// Streaming is true while a turn streams.
	Streaming bool
	// Queued is the number of queued prompts and queued shell commands.
	Queued int
	// BufferEmpty is true when the composer holds nothing but
	// whitespace.
	BufferEmpty bool
	// ResumePickerActive is true while the /resume picker is up.
	ResumePickerActive bool
	// BashMode is true while the bash mode ("!") is armed.
	BashMode bool
	// HelpActive is true while the /help overlay is up.
	HelpActive bool
	// PanelOpen is true while a modal panel is open.
	PanelOpen bool
	// PanelKeepsHint is true when the open panel keeps the row's hint.
	//
	// Only the /codesearch panel does.
	PanelKeepsHint bool
	// ExitConfirm is true while the two-step Ctrl+C exit confirmation is
	// armed.
	ExitConfirm bool
	// VimBadge is the vim mode's badge, or "" when vim mode is off or in
	// Normal mode.
	VimBadge string
	// Compact is the running (or finished) compaction, or nil.
	Compact *Compact

	// ---- the right segment ----

	// Model is the session's model.
	Model string
	// StatusModel is the model's display name. Empty falls back to
	// Model.
	StatusModel string
	// AgentName is the --agent's name, or "" for the built-in agent.
	AgentName string
	// Mode is the session's mode ("auto", "plan", "accept-edits").
	Mode string
	// EffortSuffix is the " · <effort>" segment.
	EffortSuffix string
	// TaskSuffix is the " · N tasks" segment.
	TaskSuffix string
	// QueueMode is true while the queue mode is on.
	QueueMode bool
	// AgentState is the session's agent state — "working", "tool_use" or
	// "error".
	//
	// The vocabulary is the payload's (the reference's own names) and the
	// idle state renders nothing: the row's default needs no label.
	AgentState string
	// ContextUsedPercent is the share of the context window the prompt
	// tokens take, as a percentage.
	//
	// Zero renders nothing: a session with no context window has no
	// percentage to show.
	ContextUsedPercent float64
	// Credits is the remaining AI-credit balance.
	Credits int
	// CreditsKnown reports whether the AI-credit balance is known.
	//
	// It is a separate fact from Credits because a balance of zero is a
	// balance, and an unknown balance is not a zero.
	CreditsKnown bool
}

// Compact is the state of a context compaction.
//
// A compaction that has not ended yet has no End; the label then
// carries the elapsed time up to now.
type Compact struct {
	// Start is when the compaction began.
	Start time.Time
	// End is when it finished. The zero time means "still running".
	End time.Time
	// TokensBefore is the token count before the compaction.
	TokensBefore int
	// TokensAfter is the token count after it. Zero while the
	// compaction runs.
	TokensAfter int
	// SpinnerFrame is the compaction spinner's frame.
	SpinnerFrame int
}

// Render draws the built-in status row.
//
// The row is the left segment, the padding and the right segment. The
// padding is at least one cell, so the two segments never touch.
func Render(s State) string {
	left, leftPlain := leftSegment(s)
	right := rightSegment(s)

	width := s.CanonicalWidth
	if s.Width > 0 {
		width = s.Width
	}
	padLen := width - utf8.RuneCountInString(leftPlain) - utf8.RuneCountInString(right.modePlain) - right.rightPlainLen
	if padLen < 1 {
		padLen = 1
	}
	return left + strings.Repeat(" ", padLen) + right.styled
}

// Static draws the status row for the non-tty render path.
//
// The static row is the reference's own: the "? for shortcuts" hint on
// the left, the model and the literal " · high" on the right, and a
// padding of the canonical width between them. It carries no mode, no
// effort tier and no tasks — the path that draws it has no session to
// read them from.
//
// The colour is a literal, not the palette's: the reference's
// non-tty path paints the row in its own grey, and the two greys are
// not the same value.
func Static(model string, canonicalWidth int) string {
	const staticGreyOpen = "\033[38;2;154;160;166m"
	const staticGreyClose = "\033[39m"
	const rightPrefix = " · high" // " · high" (7 runes; 8 bytes UTF-8)

	leftPlain := "? for shortcuts"
	left := staticGreyOpen + leftPlain + staticGreyClose
	right := staticGreyOpen + model + rightPrefix + staticGreyClose
	pad := strings.Repeat(" ", canonicalWidth-utf8.RuneCountInString(leftPlain)-utf8.RuneCountInString(model)-utf8.RuneCountInString(rightPrefix))
	return left + pad + right
}

// ---- the segments -------------------------------------------------------

// leftSegment is the row's left half: the styled text and its plain
// length (the padding is computed on the plain text).
func leftSegment(s State) (string, string) {
	leftPlain := leftHint(s)
	left := ""
	if leftPlain != "" {
		left = palette.DimGrayOpen + leftPlain + palette.DimGrayClose
	}

	if s.VimBadge != "" {
		badgeStyled := palette.DimGrayOpen + s.VimBadge + palette.DimGrayClose
		if leftPlain != "" {
			leftPlain = s.VimBadge + " " + leftPlain
			left = badgeStyled + " " + left
		} else {
			leftPlain = s.VimBadge
			left = badgeStyled
		}
	}
	if s.HelpActive || (s.PanelOpen && !s.PanelKeepsHint) {
		leftPlain, left = "", ""
	}
	if s.ExitConfirm {
		leftPlain = "press ctrl+c again to exit"
		left = palette.DimGrayOpen + leftPlain + palette.DimGrayClose
	}
	if s.Compact != nil {
		label := compactLabel(*s.Compact)
		leftPlain = label
		left = palette.DimGrayOpen + label + palette.DimGrayClose
	}
	return left, leftPlain
}

// leftHint is the plain text of the left segment.
//
// The rules, in order: a visible picker says "esc to cancel" (except
// the /model popup, which drops the hint); a streaming turn says the
// same; queued messages ask for "up"; a non-empty buffer clears the
// hint entirely; bash mode replaces it with its own.
func leftHint(s State) string {
	leftPlain := "? for shortcuts"

	if s.PickerVisible {
		leftPlain = "esc to cancel"
		// The reference's "/model <query>" popup drops the cancel
		// hint: its status row keeps the left segment empty.
		//
		// 【一手】 agy 1.4.6, live.
		if s.PickerModelMode {
			leftPlain = ""
		}
	}
	if s.Streaming && !s.PickerVisible && leftPlain != "" {
		leftPlain = "esc to cancel"
	}
	if s.Queued > 0 && !s.PickerVisible {
		leftPlain = "  Press up to edit queued messages"
	}
	if (!s.BufferEmpty || s.ResumePickerActive) && !s.PickerVisible {
		leftPlain = ""
	}
	if s.BashMode && !s.PickerVisible && !s.HelpActive {
		leftPlain = " activated bash mode · esc to cancel"
	}
	return leftPlain
}

// rightInfo is the row's right half.
type rightInfo struct {
	// styled is the segment as it is written.
	styled string
	// modePlain is the mode label's plain text, counted on its own for
	// the padding: the mode is painted in the mode's colour, so its
	// length is not the styled segment's length.
	modePlain string
	// rightPlainLen is the plain length of the segment after the mode
	// label.
	rightPlainLen int
}

// rightSegment is the row's right half: the mode, the model, the
// effort tier and the running tasks.
func rightSegment(s State) rightInfo {
	statusModel := s.StatusModel
	if statusModel == "" {
		statusModel = s.Model
	}
	modePlain, modeStyled := "", ""
	if s.Mode != "" {
		modePlain = s.Mode + " · "
		modeStyled = palette.StatusModeColor(s.Mode) + s.Mode + palette.DimGrayOpen + " · "
	}

	cells := statusCells(s)
	right, agentPartPlain := buildRight(statusModel, s, cells)

	rightPlainLen := utf8.RuneCountInString(statusModel) + utf8.RuneCountInString(s.EffortSuffix) + utf8.RuneCountInString(s.TaskSuffix)
	if s.AgentName != "" {
		rightPlainLen = utf8.RuneCountInString(agentPartPlain)
	} else {
		// The built-in case counts the status cells but not the queue
		// mode's suffix — the reference's own asymmetry, which the
		// golden pins.
		rightPlainLen += utf8.RuneCountInString(cells)
	}

	return rightInfo{
		styled:        modeStyled + right,
		modePlain:     modePlain,
		rightPlainLen: rightPlainLen,
	}
}

// buildRight is the right segment's body: the model (with the effort
// tier and the running tasks), or the --agent's name around it.
//
// The cells are the status cells the segment ends with.
//
// The second return value is the agent part's plain text, which the
// padding counts instead of the model's.
func buildRight(statusModel string, s State, cells string) (string, string) {
	if s.AgentName != "" {
		agentPartPlain := s.AgentName + " (" + statusModel + s.EffortSuffix + s.TaskSuffix + ")  /agents"
		if s.QueueMode {
			agentPartPlain += " · Queue mode"
		}
		agentPartPlain += cells
		return palette.DimGrayOpen + agentPartPlain + palette.DimGrayClose, agentPartPlain
	}
	right := palette.DimGrayOpen + statusModel + s.EffortSuffix + s.TaskSuffix
	if s.QueueMode {
		right += " · Queue mode"
	}
	right += cells
	right += palette.DimGrayClose
	return right, ""
}

// statusCells are the status cells the right segment carries after the
// session's identity.
//
// The reference's built-in row carries the mode, the model and the effort
// tier and nothing else (【一手】 agy 1.4.7's own status row: "auto ·
// Gemini 3.8 Flash · high"). The cells below are the clone's own reading
// of the documentation, which asks the status line for the agent state,
// the context usage and the credit balance
// (official-docs/cli/statusline.md:13,148 and official-docs/cli/credits.md:11).
//
// Every cell is omitted when its fact is absent: a cell without a fact
// would be a guess.
func statusCells(s State) string {
	var b strings.Builder
	if s.AgentState != "" {
		b.WriteString(" · ")
		b.WriteString(s.AgentState)
	}
	if s.ContextUsedPercent > 0 {
		b.WriteString(fmt.Sprintf(" · %.1f%%", s.ContextUsedPercent))
	}
	if s.CreditsKnown {
		b.WriteString(fmt.Sprintf(" · AI Credits: %d", s.Credits))
	}
	return b.String()
}
