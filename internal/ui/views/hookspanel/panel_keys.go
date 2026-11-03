// panel_keys.go — the /hooks panel's keys.
//
// The panel owns every key it receives: the type list's ↑/↓/Enter, the
// matcher list's navigation, and the form's typing and field switch. A
// key the panel does not own is answered with handled=false, so the
// host's global handlers (Ctrl+C) still see it.

package hookspanel

import (
	tea "charm.land/bubbletea/v2"
)

// HandleKey routes one key to the panel's current stage.
//
// The returned bool says whether the panel owned the key; the returned
// command is the host's (the panel itself has none).
func (p *Panel) HandleKey(key tea.Key) (bool, tea.Cmd) {
	if p == nil {
		return false, nil
	}
	switch p.stage {
	case StageMatchers:
		return p.handleMatcherKey(key)
	case StageForm:
		return p.handleFormKey(key)
	}
	return p.handleTypeKey(key)
}

// handleTypeKey is the type list's keys.
//
// ↑/↓ clamp (the reference does not wrap here), Enter opens the type's
// matcher list and Esc leaves the panel.
func (p *Panel) handleTypeKey(key tea.Key) (bool, tea.Cmd) {
	switch key.Code {
	case tea.KeyUp:
		if p.cursor > 0 {
			p.cursor--
		}
		return true, nil
	case tea.KeyDown:
		if p.cursor < len(types)-1 {
			p.cursor++
		}
		return true, nil
	case tea.KeyEnter:
		p.idx = p.cursor
		p.stage = StageMatchers
		p.cursor = 0
		return true, nil
	case tea.KeyEsc:
		return true, p.close()
	}
	if key.Mod == 0 && key.Text != "" {
		return true, nil
	}
	return false, nil
}

// handleMatcherKey is the matcher list's keys.
//
// Esc goes back to the type list, with the cursor on the type the user
// came from.
func (p *Panel) handleMatcherKey(key tea.Key) (bool, tea.Cmd) {
	n := selectableRows(p.idx)
	switch key.Code {
	case tea.KeyUp:
		if p.cursor > 0 {
			p.cursor--
		}
		return true, nil
	case tea.KeyDown:
		if p.cursor < n-1 {
			p.cursor++
		}
		return true, nil
	case tea.KeyEnter:
		if p.cursor == 0 {
			p.stage = StageForm
			p.field = 0
			p.match = ""
			p.name = ""
		}
		return true, nil
	case tea.KeyEsc:
		p.stage = StageTypes
		p.cursor = p.idx
		return true, nil
	}
	if key.Mod == 0 {
		return true, nil
	}
	return false, nil
}

// handleFormKey is the form's keys.
//
// Tab switches the focused field of a lifecycle hook's form, backspace
// deletes a character and the plain keys type into the focused buffer.
func (p *Panel) handleFormKey(key tea.Key) (bool, tea.Cmd) {
	switch key.Code {
	case tea.KeyEsc:
		p.stage = StageMatchers
		p.cursor = 0
		return true, nil
	case tea.KeyTab:
		if !IsTool(p.idx) {
			p.field = (p.field + 1) % 3
		}
		return true, nil
	case tea.KeyBackspace:
		switch {
		case IsTool(p.idx):
			if r := []rune(p.match); len(r) > 0 {
				p.match = string(r[:len(r)-1])
			}
		case p.field == 0:
			if r := []rune(p.name); len(r) > 0 {
				p.name = string(r[:len(r)-1])
			}
		}
		return true, nil
	}
	if key.Mod == 0 && key.Text != "" {
		switch {
		case IsTool(p.idx):
			p.match += key.Text
		case p.field == 0:
			p.name += key.Text
		}
		return true, nil
	}
	if key.Mod == 0 {
		return true, nil
	}
	return false, nil
}
