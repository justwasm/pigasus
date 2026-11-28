// Package statusrow draws the TUI's status row — the single line under
// the prompt that carries the session's model, its mode, its effort
// tier, the running tasks and the hint the user needs right now.
//
// # What it is
//
// The row is the reference's own:
//
//	? for shortcuts                    Gemini 3.8 Flash · high · 2 tasks
//	esc to cancel                      plan · MiniMax M3
//	press ctrl+c again to exit         MiniMax M3
//	⠿  Compacting context… (12s)        MiniMax M3
//
// Only the stored-empty default draws no prefix — the row is just the model.
// The three real modes each name themselves, `auto` included, so that every
// stop of the shift+tab cycle is visibly distinct. Which modes carry one is
// palette.StatusModePrefix's question.
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
	// OutOfCredits reports whether the account is out of credits.
	OutOfCredits bool
	// IsGeminiAPIKey reports whether the active connection uses GEMINI_API_KEY.
	IsGeminiAPIKey bool
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
		if s.Compact.End.IsZero() || s.BufferEmpty {
			label := compactLabel(*s.Compact)
			leftPlain = label
			left = palette.DimGrayOpen + label + palette.DimGrayClose
		}
	}
	return left, leftPlain
}

// hintDefault is the row's hint when no rule claims it.
const hintDefault = "? for shortcuts"

// hintRule is one entry in the hint's precedence chain.
//
// when decides whether the rule claims the segment, and text is what it
// writes when it does. An empty text is a real answer — it clears the
// segment — which is why the chain is "first rule that matches wins" rather
// than "first non-empty text wins".
//
// text is a func rather than a constant because a rule's wording can depend
// on the state that made it match: the picker's hint is the cancel hint,
// except in the /model popup, where it is nothing. Keeping the refinement
// inside its own rule is what stops a broad rule above it from answering
// first.
type hintRule struct {
	// name is the rule's own label, so a test can say which rule won
	// instead of only which string came out.
	name string
	when func(State) bool
	text func(State) string
}

// hintText wraps a constant as a hintRule's text.
func hintText(s string) func(State) string {
	return func(State) string { return s }
}

// hintRules is the hint's precedence, highest first.
//
// The chain used to be a sequence of assignments — each `if` overwriting
// leftPlain — so precedence was encoded in the *order of the statements* and
// the last matching rule won. That reads bottom-up, and adding a rule meant
// deciding where in the middle to insert it. Here the slice is the
// precedence: the first matching rule wins, and a new rule goes where it
// belongs in the list rather than in the middle of a function body.
//
// The relative order is the reference's, and it is not arbitrary:
//
//	bash mode > non-empty buffer > queued > streaming > picker > /model picker
//
// Bash mode outranks the buffer because arming "!" and then typing a command
// still has to say the mode is armed — the command is what bash mode is for.
// The buffer outranks everything else below it because a composer with text
// in it needs no hint: the user is already typing. Queued outranks streaming
// because a queued prompt is the more specific thing to say. The /model
// picker's empty answer is the least specific, so it loses to all of them.
//
// Every rule below the first is written to be independent of the others, so
// one can be read on its own. The picker guard is repeated rather than
// hoisted into a shared prefix: hoisting it is what made the old version hard
// to read, and the repetition is what lets a rule stand alone.
//
// 【一手】 agy 1.4.6, live: the "/model <query>" popup keeps the left segment
// empty rather than showing the cancel hint.
var hintRules = []hintRule{
	{
		name: "bash-mode",
		when: func(s State) bool { return s.BashMode && !s.PickerVisible && !s.HelpActive },
		text: hintText(" activated bash mode · esc to cancel"),
	},
	{
		name: "composer-has-text",
		when: func(s State) bool { return (!s.BufferEmpty || s.ResumePickerActive) && !s.PickerVisible },
		text: hintText(""),
	},
	{
		name: "queued-prompts",
		when: func(s State) bool { return s.Queued > 0 && !s.PickerVisible },
		text: hintText("  Press up to edit queued messages"),
	},
	{
		name: "streaming",
		when: func(s State) bool { return s.Streaming && !s.PickerVisible },
		text: hintText("esc to cancel"),
	},
	{
		name: "picker",
		when: func(s State) bool { return s.PickerVisible },
		// A picker says "esc to cancel" — except the /model popup, which
		// leaves the segment empty. The two are one rule rather than two
		// entries because the popup is a refinement of the picker, not a
		// thing that can be true on its own: PickerModelMode without
		// PickerVisible is not a state the host produces, and splitting
		// them let a broader picker rule above answer first.
		text: pickerHint,
	},
}

// pickerHint is the picker rule's text: the cancel hint, or empty for the
// /model popup.
func pickerHint(s State) string {
	if s.PickerModelMode {
		return ""
	}
	return "esc to cancel"
}

// leftHint is the plain text of the left segment: the first rule that claims
// the segment, or hintDefault when none does.
func leftHint(s State) string {
	for _, r := range hintRules {
		if r.when(s) {
			return r.text(s)
		}
	}
	return hintDefault
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
	if prefix := palette.StatusModePrefix(s.Mode); prefix != "" {
		// The mode is a prefix only when it has a word: the stored-empty
		// default draws nothing, `plan`, `accept-edits` and `auto` draw
		// "<mode> · ". Only the default is anonymous, so no shift+tab stop
		// renders the same as another.
		//
		// The mode word takes the mode's colour and the separator that
		// follows it goes back to the dim foreground: agy paints
		// accept-edits as #859900 and the "· " after it as #657B83.
		//
		// The close is StatusModeClose, not DimGrayOpen: auto's word is
		// opened bold, and a colour-only close would leave the bold on the
		// separator and on the model behind it — the row would render the
		// model bold in auto mode and plain in the other two.
		modePlain = prefix
		modeStyled = palette.StatusModeColor(s.Mode) + s.Mode + palette.StatusModeClose(s.Mode) + " · "
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
// tier and nothing else. The cells below are the clone's own reading of
// the documentation, which asks the status line for the agent state, the
// context usage and the credit balance
// (official-docs/cli/statusline.md:13,148 and official-docs/cli/credits.md:11).
//
// 【一手】 This comment used to quote agy 1.4.7's row as "auto · Gemini 3.8
// Flash · high". Re-measured on agy 1.5.18 (tuialign scene 49), the
// default and auto modes draw NO prefix: the row is "Gemini 3.8 Flash ·
// high", and only plan and accept-edits add "<mode> · ". The 1.4.7 quote
// is therefore either a transcription error or a behaviour that changed;
// either way, do not cite it as evidence that auto carries a prefix.
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
	// AGY 1.5.14: The status line shows AI credits only when you have a positive
	// balance (> 0) and omits the permanent red "AI: Out of credits" warning
	// when credit balance is 0 while normal model quota is still available.
	if s.CreditsKnown && !s.IsGeminiAPIKey && s.Credits > 0 {
		b.WriteString(fmt.Sprintf(" · AI Credits: %d", s.Credits))
	}
	return b.String()
}
