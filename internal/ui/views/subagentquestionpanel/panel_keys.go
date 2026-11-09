// panel_keys.go — the picker's keys.
//
// The picker is modal: while it is open it claims every key it
// receives, so nothing reaches the composer or the host's global
// handlers. Only the keys the picker owns change its state — Esc
// closes it, ↑/↓ walk the questions, ←/→ walk the options inside the
// highlighted question, Enter stages the answer.
//
// The host's Ctrl+K binding runs before this one, so the picker can be
// closed with Ctrl+K even while it is open.

package subagentquestionpanel

import (
	tea "charm.land/bubbletea/v2"

	subagentquestions "github.com/justwasm/pigasus/internal/app/subagent/questions"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/palette"
)

// HandleKey handles a key while the picker is open.
//
// It reports whether the key was handled. While the picker is open
// every key is handled; a closed picker handles none.
func (p *Panel) HandleKey(key tea.Key) bool {
	handled, _ := p.HandleKeyMsg(key)
	return handled
}

// HandleKeyMsg handles a key while the picker is open and emits Elm messages.
func (p *Panel) HandleKeyMsg(key tea.Key) (bool, tea.Cmd) {
	if p == nil || !p.open {
		return false, nil
	}
	// Ctrl+K belongs to the host: it toggles the picker even while the
	// picker is open, so the panel must not swallow it.
	if key.Code == 'k' && key.Mod.Contains(tea.ModCtrl) {
		return false, nil
	}

	if p.writeInActive {
		switch key.Code {
		case tea.KeyEsc:
			p.writeInActive = false
			p.writeInInput.Blur()
			return true, nil
		case tea.KeyEnter:
			p.commitWriteIn()
			return true, nil
		default:
			var cmd tea.Cmd
			p.writeInInput, cmd = p.writeInInput.Update(tea.KeyPressMsg(key))
			return true, cmd
		}
	}

	pending := p.pending()
	var cur *subagentquestions.Question
	if p.cursor >= 0 && p.cursor < len(pending) {
		cur = pending[p.cursor]
	}

	switch key.Code {
	case tea.KeyEsc:
		// Esc skips the highlighted question: the question's Wait
		// returns the reference's "User Skipped" text, so the model
		// or subagent continues instead of waiting forever.
		p.SkipCurrent()
		return true, func() tea.Msg { return MsgDismissed{} }
	case tea.KeyUp:
		if p.cursor > 0 {
			p.cursor--
		}
		p.optionCursor = 0
		return true, nil
	case tea.KeyDown:
		if p.cursor < len(pending)-1 {
			p.cursor++
		}
		p.optionCursor = 0
		return true, nil
	case tea.KeyLeft:
		p.moveOption(-1)
		return true, nil
	case tea.KeyRight:
		p.moveOption(1)
		return true, nil
	case tea.KeySpace, 'x':
		if cur != nil && cur.IsMultiSelect {
			if p.optionCursor < len(cur.Options) {
				p.toggleOption(cur.ID, p.optionCursor)
				return true, nil
			}
			p.activateWriteIn(cur)
			return true, nil
		} else if cur != nil && p.optionCursor >= len(cur.Options) {
			p.activateWriteIn(cur)
			return true, nil
		}
	case 'e':
		if cur != nil && p.optionCursor >= len(cur.Options) {
			p.activateWriteIn(cur)
			return true, nil
		}
	case tea.KeyEnter:
		if cur == nil {
			return true, nil
		}
		// A question with multi-select:
		if cur.IsMultiSelect {
			if p.optionCursor >= len(cur.Options) && p.writeInText[cur.ID] == "" {
				p.activateWriteIn(cur)
				return true, nil
			}
			p.submitMultiSelect(cur)
			return true, nil
		}

		// A single-select question with options answers the highlighted option
		// directly. The last row of the option list is the write-in
		// row: selecting it stages the composer answer instead (or submits
		// if typed).
		if len(cur.Options) > 0 && p.optionCursor < len(cur.Options) {
			p.AnswerSelected()
			return true, nil
		}
		if p.writeInText[cur.ID] != "" {
			p.answer(cur, p.writeInText[cur.ID])
			return true, nil
		}
		p.stage()
		return true, func() tea.Msg {
			return MsgQuestionStaged{
				QuestionID: cur.ID,
				Scope:      p.scope,
				Prompt:     cur.Prompt,
			}
		}
	}
	return true, nil
}

// close dismisses the picker without answering.
func (p *Panel) close() {
	p.open = false
	p.cursor = 0
	p.optionCursor = 0
	p.writeInActive = false
	p.writeInInput.Blur()
}

// moveOption walks the options of the highlighted question.
//
// cycle feature/ask-question-left-right-nav: Left and Right arrow keys
// navigate between the options of the currently-highlighted question
// without committing the selection. The cursor clamps at the bounds of
// the option list, and a question without options is inert.
func (p *Panel) moveOption(delta int) {
	pending := p.pending()
	if p.cursor < 0 || p.cursor >= len(pending) {
		return
	}
	cur := pending[p.cursor]
	// The option cursor may rest on the write-in row after the last
	// real option, so the upper bound is len(options), not len-1.
	if len(cur.Options) == 0 {
		if p.optionCursor != 0 {
			p.optionCursor = 0
		}
		return
	}
	newPos := p.optionCursor + delta
	if newPos >= 0 && newPos <= len(cur.Options) {
		p.optionCursor = newPos
	}
}

// stage hands the highlighted question to the answer flow.
//
// The picker closes; the answer is typed into the composer's prompt and
// submitted there, which is why the staged answer outlives the picker.
func (p *Panel) stage() {
	pending := p.pending()
	if p.cursor < 0 || p.cursor >= len(pending) {
		return
	}
	selected := pending[p.cursor]
	p.answerTarget = selected.ID
	p.close()
	p.respond(palette.WarningBlueOpen + "> " + palette.WarningBlueClose +
		palette.WarningBlueOpen + "Answer for " +
		scopeLabel(p.scope) + ": " + palette.WarningBlueClose +
		palette.DimGrayOpen + tuilib.FirstLine(selected.Prompt) + palette.DimGrayClose)
}
