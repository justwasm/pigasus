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
		pending := p.pending()
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
	case tea.KeyEnter:
		pending := p.pending()
		if p.cursor < 0 || p.cursor >= len(pending) {
			return true, nil
		}
		selected := pending[p.cursor]
		// A question with options answers the highlighted option
		// directly. The last row of the option list is the write-in
		// row: selecting it stages the composer answer instead.
		if len(selected.Options) > 0 && p.optionCursor < len(selected.Options) {
			p.AnswerSelected()
			return true, nil
		}
		p.stage()
		return true, func() tea.Msg {
			return MsgQuestionStaged{
				QuestionID: selected.ID,
				Scope:      p.scope,
				Prompt:     selected.Prompt,
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
