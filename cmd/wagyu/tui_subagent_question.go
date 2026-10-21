// tui_subagent_question.go — cycle subagent-question-picker.
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
// This file owns the TUI side: the picker overlay, the alert
// rendering in the status row, and the Ctrl+K / `a` keybindings.
// The data model lives in internal/subagentquestions; the print
// path's wiring lives in print_pig.go.
//
// Three things the picker must do:
//  1. List the active scope's pending questions, with the
//     picker showing "<agent> has a question · <prompt>" rows.
//  2. Select one and open the freeform answer textarea; submit
//     sends the answer to that question's Manager.
//  3. Surface a persistent badge in the status row, even when
//     the picker is closed, so the user knows the subagent is
//     waiting.
//
// The alert is the simplest piece: every render of viewBody
// re-reads m.subAgentQuestions.AllPending() and writes one row
// per pending question, named after m.subAgentQuestionsActive.
// The picker overlay mirrors the resume picker shape (rows +
// keyboard hints); Esc closes it without answering.
//
// feature/ask-question-left-right-nav adds Left and Right arrow
// keys that move between the options of the currently-highlighted
// question without committing the selection. The cursor clamps
// at the option-list bounds; Up/Down reset it on a fresh question;
// Enter remains the only commit key.
//
// The "active scope" defaults to subagentquestions.ParentKey
// (the parent's own questions) — the TUI does not yet render
// per-subagent transcripts, so Ctrl+K opens the parent's
// picker. Cycle 229 will plumb per-subagent transcript views
// and let the picker switch scope.
package main

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/subagentquestions"
)

// openSubAgentQuestionPicker flips the picker into the open
// state when there is at least one pending question on the
// active scope, and surfaces a tiny "no pending questions"
// echo otherwise. The Ctrl+K / `a` bindings both call this —
// when the picker is already open, Ctrl+K closes it; when
// closed, Ctrl+K opens it. `a` is open-only (the changelog
// line says "or `a` in the subagent's view opens a question
// picker", not toggle).
//
// Returns nil so the keybinding site can keep its existing
// return shape; the picker state is mutated in place and
// View() picks it up on the next render.
func (m *runMode) openSubAgentQuestionPicker() tea.Cmd {
	if m.subAgentQuestions == nil {
		return nil
	}
	if m.subAgentQuestionPickerOpen {
		m.subAgentQuestionPickerOpen = false
		return nil
	}
	pending := m.subAgentQuestions.Pending(m.subAgentQuestionsActive)
	if len(pending) == 0 {
		// No questions to answer. Echo a friendly line so the
		// user understands what just happened.
		m.response = append(m.response,
			cWarningBlueOpen+"  \u23bf  "+cWarningBlueClose+
				cDimGrayOpen+"No pending questions for "+
				subAgentQuestionScopeLabel(m.subAgentQuestionsActive)+
				cDimGrayClose)
		return nil
	}
	m.subAgentQuestionPickerOpen = true
	m.subAgentQuestionPickerCursor = 0
	// cycle feature/ask-question-left-right-nav: opening the
	// picker always starts on the first option of the first
	// question, even if the user previously moved Left/Right on
	// a different question.
	m.subAgentQuestionPickerOptionCursor = 0
	m.subAgentQuestionAlert = subAgentQuestionFirstAlert(m.subAgentQuestionsActive, pending)
	return nil
}

// handleSubAgentQuestionPickerKey is the keyboard handler for
// the question picker. Returns nil because the picker is
// synchronous (no async cmd needed).
//
// Cycle feature/ask-question-left-right-nav: Left and Right arrow
// keys navigate between the options of the currently-highlighted
// question without committing the selection. The previously
// Up/Down-only navigation stays, and Up/Down now also reset the
// option cursor so each fresh question begins on its first
// option. The commit semantic (Enter stages the question and
// closes the picker for freeform answer entry) is unchanged.
func (m *runMode) handleSubAgentQuestionPickerKey(key tea.Key) tea.Cmd {
	switch key.Code {
	case tea.KeyEsc:
		m.subAgentQuestionPickerOpen = false
		m.subAgentQuestionPickerCursor = 0
		m.subAgentQuestionPickerOptionCursor = 0
		return nil
	case tea.KeyUp:
		if m.subAgentQuestionPickerCursor > 0 {
			m.subAgentQuestionPickerCursor--
		}
		// A new question means a fresh option cursor — the
		// Options list belongs to the previous question and
		// would otherwise bleed across rows.
		m.subAgentQuestionPickerOptionCursor = 0
		return nil
	case tea.KeyDown:
		pending := m.subAgentQuestions.Pending(m.subAgentQuestionsActive)
		if m.subAgentQuestionPickerCursor < len(pending)-1 {
			m.subAgentQuestionPickerCursor++
		}
		m.subAgentQuestionPickerOptionCursor = 0
		return nil
	case tea.KeyLeft:
		// cycle feature/ask-question-left-right-nav: Left does
		// not commit. It only moves the option cursor of the
		// currently-highlighted question one option to the
		// left, clamped at 0. A question with no options is
		// inert — the cursor stays put.
		pending := m.subAgentQuestions.Pending(m.subAgentQuestionsActive)
		if m.subAgentQuestionPickerCursor < 0 ||
			m.subAgentQuestionPickerCursor >= len(pending) {
			return nil
		}
		cur := pending[m.subAgentQuestionPickerCursor]
		if len(cur.Options) == 0 {
			return nil
		}
		if m.subAgentQuestionPickerOptionCursor > 0 {
			m.subAgentQuestionPickerOptionCursor--
		}
		return nil
	case tea.KeyRight:
		// cycle feature/ask-question-left-right-nav: Right
		// is the mirror of Left. Clamped at the last option,
		// inert when the question has no options, and never
		// commits.
		pending := m.subAgentQuestions.Pending(m.subAgentQuestionsActive)
		if m.subAgentQuestionPickerCursor < 0 ||
			m.subAgentQuestionPickerCursor >= len(pending) {
			return nil
		}
		cur := pending[m.subAgentQuestionPickerCursor]
		if len(cur.Options) == 0 {
			return nil
		}
		if m.subAgentQuestionPickerOptionCursor < len(cur.Options)-1 {
			m.subAgentQuestionPickerOptionCursor++
		}
		return nil
	case tea.KeyEnter:
		pending := m.subAgentQuestions.Pending(m.subAgentQuestionsActive)
		if m.subAgentQuestionPickerCursor < 0 || m.subAgentQuestionPickerCursor >= len(pending) {
			return nil
		}
		selected := pending[m.subAgentQuestionPickerCursor]
		// Stage the answer on the runMode; the input handler
		// reads subAgentQuestionAnswerTarget on the next
		// submit. Today the answer textarea uses the existing
		// m.input textarea (the user types into the prompt)
		// — cycle 229 work replaces this with a dedicated
		// modal.
		m.subAgentQuestionAnswerTarget = selected.ID
		m.subAgentQuestionAnswerBuffer = ""
		m.subAgentQuestionPickerOpen = false
		m.subAgentQuestionPickerCursor = 0
		m.subAgentQuestionPickerOptionCursor = 0
		m.response = append(m.response,
			cWarningBlueOpen+"> "+cWarningBlueClose+
				cWarningBlueOpen+"Answer for "+
				subAgentQuestionScopeLabel(m.subAgentQuestionsActive)+
				": "+cWarningBlueClose+
				cDimGrayOpen+firstLine(selected.Prompt)+cDimGrayClose)
		return nil
	}
	return nil
}

// submitSubAgentQuestionAnswer reads m.subAgentQuestionAnswerTarget
// + m.input.Value(), submits the answer to the Manager, and resets
// the input. Called by the regular prompt-submit path when the
// answer target is non-empty (the picker sets it; the input flow
// consumes it once).
//
// The function returns whether the input was consumed as an
// answer so the caller can suppress the normal "send prompt"
// behaviour for that single keystroke.
func (m *runMode) submitSubAgentQuestionAnswer() bool {
	if m.subAgentQuestionAnswerTarget == "" || m.subAgentQuestions == nil {
		return false
	}
	target := m.subAgentQuestionAnswerTarget
	answer := strings.TrimSpace(m.input.Value())
	m.input.Reset()
	m.subAgentQuestionAnswerTarget = ""
	m.subAgentQuestionAnswerBuffer = ""
	if answer == "" {
		// Empty answer → skip; the model sees "(skipped)" and
		// continues without waiting.
		if err := m.subAgentQuestions.Skip(m.subAgentQuestionsActive, target); err != nil {
			m.response = append(m.response,
				cDimGrayOpen+"subagent-question skip: "+err.Error()+cDimGrayClose)
			return true
		}
		m.response = append(m.response,
			cDimGrayOpen+"  \u23bf  Question skipped."+cDimGrayClose)
		return true
	}
	if err := m.subAgentQuestions.Answer(m.subAgentQuestionsActive, target, answer); err != nil {
		m.response = append(m.response,
			cDimGrayOpen+"subagent-question answer: "+err.Error()+cDimGrayClose)
		return true
	}
	m.response = append(m.response,
		cDimGrayOpen+"  \u23bf  Answer sent to "+
			subAgentQuestionScopeLabel(m.subAgentQuestionsActive)+"."+cDimGrayClose)
	return true
}

// subAgentQuestionScopeLabel is the user-facing name for a
// subagent scope key. The parent ("") reads as "you"; real
// subagent IDs render verbatim (subagenttools.Result.SubagentID
// already produces "subagent-<unix-nano>-<counter>", which is
// stable and human-readable enough).
func subAgentQuestionScopeLabel(scope string) string {
	if scope == subagentquestions.ParentKey {
		return "you"
	}
	return scope
}

// subAgentQuestionFirstAlert is the message the status row
// renders when at least one question is pending. The format
// matches the recovered AGY changelog wording:
//
//	"<agent> has a question"
//
// where <agent> is the scope label (parent → "you"; subagent
// → its subagent ID).
func subAgentQuestionFirstAlert(scope string, pending []*subagentquestions.Question) string {
	if len(pending) == 0 {
		return ""
	}
	return fmt.Sprintf("%s has a question",
		subAgentQuestionScopeLabel(scope))
}

// subAgentQuestionAlertRows is the slice of status-row
// fragments viewBody appends when the picker is closed. Each
// pending question on each scope renders one row, sorted by
// scope (parent first, then subagents in alphabetical order)
// so the badge is stable across renders.
func (m *runMode) subAgentQuestionAlertRows() []string {
	if m.subAgentQuestions == nil {
		return nil
	}
	all := m.subAgentQuestions.AllPending()
	if len(all) == 0 {
		return nil
	}
	scopes := make([]string, 0, len(all))
	for s := range all {
		scopes = append(scopes, s)
	}
	// Sort so the parent ("") renders first, then subagents
	// in alphabetical order. Empty string sorts first in the
	// natural string ordering, so a plain sort gets the
	// right shape.
	// (kept stable to avoid map iteration randomness in tests)
	stableSortStrings(scopes)
	var rows []string
	for _, scope := range scopes {
		pending := all[scope]
		rows = append(rows,
			cWarningBlueOpen+"  \u23bf  "+cWarningBlueClose+
				cDimGrayOpen+subAgentQuestionScopeLabel(scope)+
				" has a question \u00b7 "+
				firstLine(pending[0].Prompt)+cDimGrayClose)
	}
	return rows
}

// subAgentQuestionPickerRows renders the picker overlay. The
// rows are appended after the prompt and before the status
// row so the picker sits directly above the status indicator
// (matching the resume picker's shape).
//
// Cycle feature/ask-question-left-right-nav: when the
// highlighted question has a non-empty Options list, the
// options are rendered as an indented sub-list directly under
// the row. Each option keeps a leading marker that follows the
// question's highlight, and the option cursor moves with
// Left/Right (with bounds clamping). The surrounding picker's
// shape (one row per pending question, hint row at the bottom)
// is unchanged.
func (m *runMode) subAgentQuestionPickerRows() []string {
	if !m.subAgentQuestionPickerOpen || m.subAgentQuestions == nil {
		return nil
	}
	pending := m.subAgentQuestions.Pending(m.subAgentQuestionsActive)
	scope := subAgentQuestionScopeLabel(m.subAgentQuestionsActive)
	var rows []string
	rows = append(rows,
		cWarningBlueOpen+"Pending questions for "+scope+cWarningBlueClose,
		"",
	)
	if len(pending) == 0 {
		rows = append(rows,
			cDimGrayOpen+"  No pending questions."+cDimGrayClose)
	} else {
		for i, q := range pending {
			marker := "  "
			if i == m.subAgentQuestionPickerCursor {
				marker = cBlueOpen + "> " + cWarningBlueClose
			}
			prompt := firstLine(q.Prompt)
			if len(prompt) > 60 {
				prompt = prompt[:57] + "..."
			}
			rows = append(rows,
				marker+cDimGrayOpen+prompt+cDimGrayClose)
			// feature/ask-question-left-right-nav: render
			// the options sub-list under the highlighted
			// question only. Free-form questions (no
			// Options) skip this pass entirely.
			if i != m.subAgentQuestionPickerCursor {
				continue
			}
			for oi, opt := range q.Options {
				optRune := "  "
				if oi == m.subAgentQuestionPickerOptionCursor {
					optRune = cBlueOpen + "› " + cDimGrayClose
				}
				row := "    " + optRune + firstLine(opt)
				rows = append(rows, row+cDimGrayClose)
			}
		}
	}
	rows = append(rows,
		"",
		cDimGrayOpen+"Keyboard: "+cDimGrayClose+
			cBlueOpen+"\u2191/\u2193"+cWarningBlueClose+
			cDimGrayOpen+" Navigate  "+cDimGrayClose+
			cBlueOpen+"\u2190/\u2192"+cWarningBlueClose+
			cDimGrayOpen+" Options  "+cDimGrayClose+
			cBlueOpen+"enter"+cWarningBlueClose+
			cDimGrayOpen+" Answer  "+cDimGrayClose+
			cBlueOpen+"esc"+cWarningBlueClose+
			cDimGrayOpen+" Cancel"+cDimGrayClose,
	)
	return rows
}

// stableSortStrings is a tiny stdlib wrapper kept here so this
// file compiles standalone without dragging in slices.go.
// Used only for the scope ordering in subAgentQuestionAlertRows.
func stableSortStrings(s []string) {
	// Insertion sort — scopes rarely exceed a handful of entries.
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}
