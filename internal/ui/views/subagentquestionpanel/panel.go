// Package subagentquestionpanel holds the subagent-question picker: the
// overlay that lists a scope's pending questions, the cursors that walk
// the list and the options inside a question, and the answer the user's
// next prompt sends back to the subagent that asked.
//
// # What it is
//
// AGY 1.3.6 changelog line 6:
//
//	"Fixed questions asked by subagents and workflow agents being
//	 lost, which left the subagent waiting forever: the alert now
//	 reads `<agent> has a question` and shows it, and Ctrl+K or `a`
//	 in the subagent's view opens a question picker that sends your
//	 answer to that agent. Headless `-p` runs skip these questions
//	 so the subagent can continue."
//
// The picker shows one row per pending question, named after the scope
// it belongs to: the parent reads as "you", a real subagent renders its
// subagent id verbatim. Enter stages the highlighted question and
// closes the picker; the answer itself is typed into the composer's
// prompt and submitted there.
//
// feature/ask-question-left-right-nav adds Left and Right arrow keys
// that move between the options of the currently-highlighted question
// without committing the selection. The cursor clamps at the
// option-list bounds; Up/Down reset it on a fresh question; Enter
// remains the only commit key.
//
// # Where the panel lives
//
// The panel itself is here: the scope it shows, the open flag, the two
// cursors, the staged answer, the alert rows the status line draws, the
// picker rows the overlay draws, the keys, and the answer the Manager
// receives.
//
// The host keeps the Manager — the session owns it, and the tool seam
// (`tasks.AskQuestion`) enqueues into the same store — and supplies the
// three seams the panel cannot answer itself: where a row is echoed,
// what the composer holds, and how it is cleared.
//
// Unlike the slash panels this one is *not* rebuilt on every open: the
// staged answer outlives the picker's closing, because the answer is
// typed into the composer's prompt and submitted there. The cursors
// are still reset when the picker opens.
package subagentquestionpanel

import (
	"strings"

	subagentquestions "github.com/justwasm/pigasus/internal/app/subagent/questions"
	"github.com/justwasm/pigasus/palette"
)

// Seams are the host's answers the panel cannot answer itself.
//
// Every one of them is optional: a panel built without a seam behaves
// as if the seam were answered with "nothing happened", which keeps the
// package usable from a test without a running TUI.
type Seams struct {
	// Manager is the store the pending questions live in.
	//
	// The host owns it: the session's tool seam enqueues into the same
	// store, so the picker and the model see one list.
	Manager *subagentquestions.Manager
	// Respond echoes a line into the conversation.
	//
	// The panel writes its own lines here: the "no pending questions"
	// notice, the "> Answer for …" prompt, and the confirmation the
	// answer produced.
	Respond func(line string)
	// InputValue returns what the composer holds.
	//
	// The answer is typed into the composer, not into a modal of the
	// panel's own, so the panel reads it through this seam.
	InputValue func() string
	// ResetInput clears the composer.
	//
	// The panel clears it once it has taken the answer, so the next
	// prompt starts from an empty composer.
	ResetInput func()
}

// Panel is the picker.
//
// The zero value is usable: a panel without a Manager has no questions
// to show and answers every key with "nothing happened".
type Panel struct {
	seams Seams
	// scope is the subagent the picker shows. The parent is
	// subagentquestions.ParentKey (the empty string).
	scope string
	// open is true while the overlay is on screen.
	open bool
	// cursor is the highlighted question's row.
	cursor int
	// optionCursor is the highlighted option inside the highlighted
	// question.
	optionCursor int
	// answerTarget is the id of the question the staged answer goes to.
	answerTarget string
}

// New builds the picker. The scope starts at the parent's.
func New(seams Seams) *Panel {
	return &Panel{seams: seams, scope: subagentquestions.ParentKey}
}

// SetScope points the picker at another subagent's questions.
//
// The parent's own questions are subagentquestions.ParentKey (the empty
// string); a real subagent carries the id its `invoke_subagent` result
// surfaced.
func (p *Panel) SetScope(scope string) {
	if p == nil {
		return
	}
	p.scope = scope
}

// Active is true while the picker overlay is open.
func (p *Panel) Active() bool {
	return p != nil && p.open
}

// HasPendingQuestion is true while the picker is on screen OR an answer
// is staged on a queued question.
//
// The host reports it to the terminal's status protocol: a session that
// waits for the user's answer is blocked, not idle.
func (p *Panel) HasPendingQuestion() bool {
	return p != nil && (p.open || p.answerTarget != "")
}

// AnswerTarget is the id of the question the staged answer goes to.
//
// It is empty when no answer is staged.
func (p *Panel) AnswerTarget() string {
	if p == nil {
		return ""
	}
	return p.answerTarget
}

// PendingCount is the number of pending questions on the active scope.
func (p *Panel) PendingCount() int {
	if p == nil || p.seams.Manager == nil {
		return 0
	}
	return p.seams.Manager.PendingCount(p.scope)
}

// Toggle flips the picker into the open state when the active scope has
// at least one pending question, and closes it when it is already open.
// When there is nothing to answer it echoes a friendly line instead.
//
// The Ctrl+K binding toggles; the `a` binding opens.
func (p *Panel) Toggle() {
	if p == nil || p.seams.Manager == nil {
		return
	}
	if p.open {
		p.open = false
		return
	}
	pending := p.seams.Manager.Pending(p.scope)
	if len(pending) == 0 {
		// No questions to answer. Echo a friendly line so the
		// user understands what just happened.
		p.respond(palette.WarningBlueOpen + "  ⎿  " + palette.WarningBlueClose +
			palette.DimGrayOpen + "No pending questions for " +
			scopeLabel(p.scope) + palette.DimGrayClose)
		return
	}
	p.open = true
	p.cursor = 0
	// cycle feature/ask-question-left-right-nav: opening the picker
	// always starts on the first option of the first question, even if
	// the user moved Left/Right on a different question before.
	p.optionCursor = 0
}

// SubmitAnswer sends the composer's content to the staged question.
//
// The picker stages the question when the user presses Enter; the
// answer itself is typed into the composer's prompt and submitted
// there. An empty answer skips the question: the model sees "(skipped)"
// and continues without waiting.
//
// It reports whether the composer was consumed as an answer, so the
// caller can suppress the normal "send prompt" behaviour for that one
// keystroke.
func (p *Panel) SubmitAnswer() bool {
	if p == nil || p.answerTarget == "" || p.seams.Manager == nil {
		return false
	}
	target := p.answerTarget
	answer := strings.TrimSpace(p.inputValue())
	p.resetInput()
	p.answerTarget = ""
	if answer == "" {
		// Empty answer → skip; the model sees "(skipped)" and
		// continues without waiting.
		if err := p.seams.Manager.Skip(p.scope, target); err != nil {
			p.respond(palette.DimGrayOpen + "subagent-question skip: " +
				err.Error() + palette.DimGrayClose)
			return true
		}
		p.respond(palette.DimGrayOpen + "  ⎿  Question skipped." + palette.DimGrayClose)
		return true
	}
	if err := p.seams.Manager.Answer(p.scope, target, answer); err != nil {
		p.respond(palette.DimGrayOpen + "subagent-question answer: " +
			err.Error() + palette.DimGrayClose)
		return true
	}
	p.respond(palette.DimGrayOpen + "  ⎿  Answer sent to " +
		scopeLabel(p.scope) + "." + palette.DimGrayClose)
	return true
}

// pending is the active scope's pending questions.
func (p *Panel) pending() []*subagentquestions.Question {
	if p == nil || p.seams.Manager == nil {
		return nil
	}
	return p.seams.Manager.Pending(p.scope)
}

// respond echoes a line into the conversation.
func (p *Panel) respond(line string) {
	if p == nil || p.seams.Respond == nil {
		return
	}
	p.seams.Respond(line)
}

// inputValue is what the composer holds.
func (p *Panel) inputValue() string {
	if p == nil || p.seams.InputValue == nil {
		return ""
	}
	return p.seams.InputValue()
}

// resetInput clears the composer.
func (p *Panel) resetInput() {
	if p == nil || p.seams.ResetInput == nil {
		return
	}
	p.seams.ResetInput()
}

// scopeLabel is the user-facing name for a subagent scope key. The
// parent ("") reads as "you"; real subagent IDs render verbatim
// (subagenttools.Result.SubagentID already produces
// "subagent-<unix-nano>-<counter>", which is stable and human-readable
// enough).
func scopeLabel(scope string) string {
	if scope == subagentquestions.ParentKey {
		return "you"
	}
	return scope
}
