// panel_keys.go — the /permissions panel's keys.
//
// The panel owns every key it receives: the scope picker's ↑/↓/Enter,
// the tab view's navigation, its switch keys and its row actions
// (a/d/e/backspace, and Ctrl+G as the alias of e), and the form's
// typing. A key the panel does not own is answered with handled=false,
// so the host's global handlers (Ctrl+C) still see it.

package permissionspanel

import (
	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/core/policy/permissions"
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
	case StageLists:
		return p.handleListKey(key)
	case StageForm:
		return p.handleFormKey(key)
	}
	return p.handleScopeKey(key)
}

// handleScopeKey is the scope picker's keys.
//
// ↑/↓ wrap (probe_verify2.py: Up at Project lands on Global).
func (p *Panel) handleScopeKey(key tea.Key) (bool, tea.Cmd) {
	switch key.Code {
	case tea.KeyUp:
		p.cursor--
		if p.cursor < 0 {
			p.cursor = len(scopes) - 1
		}
		return true, nil
	case tea.KeyDown:
		p.cursor = (p.cursor + 1) % len(scopes)
		return true, nil
	case tea.KeyEnter:
		if p.cursor < 0 || p.cursor >= len(scopes) {
			p.cursor = 0
		}
		p.scope = p.cursor
		p.stage = StageLists
		p.tab = 0
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

// handleListKey is the tab view's keys.
func (p *Panel) handleListKey(key tea.Key) (bool, tea.Cmd) {
	n := len(p.readList(p.scope, p.tab))
	switch key.Code {
	case tea.KeyUp:
		if p.cursor > 0 {
			p.cursor--
		}
		return true, nil
	case tea.KeyDown:
		if n > 0 && p.cursor < n-1 {
			p.cursor++
		}
		return true, nil
	case tea.KeyLeft:
		p.tab = (p.tab + 2) % 3
		p.cursor = 0
		return true, nil
	case tea.KeyRight:
		p.tab = (p.tab + 1) % 3
		p.cursor = 0
		return true, nil
	case tea.KeyEsc:
		p.stage = StageScopes
		p.cursor = p.scope
		return true, nil
	}
	if p.handleRowActionKey(key, n) {
		return true, nil
	}
	if key.Mod == 0 {
		return true, nil
	}
	return false, nil
}

// handleRowActionKey is the tab view's row actions: delete ('d' or
// backspace), add ('a') and edit ('e' or Ctrl+G).
func (p *Panel) handleRowActionKey(key tea.Key, n int) bool {
	if key.Code == tea.KeyBackspace || (key.Mod == 0 && key.Text == "d") {
		if n > 0 && p.cursor >= 0 && p.cursor < n {
			list := p.readList(p.scope, p.tab)
			list = append(list[:p.cursor], list[p.cursor+1:]...)
			p.writeList(p.scope, p.tab, list)
			if p.cursor >= len(list) && p.cursor > 0 {
				p.cursor--
			}
		}
		return true
	}
	if key.Mod == 0 && key.Text == "a" {
		p.form = FormAdd
		p.edit = -1
		p.text = ""
		p.errMsg = ""
		p.stage = StageForm
		return true
	}
	// The reference's own alias: "Press E (or Ctrl + G) to edit the
	// highlighted rule" (official-docs/cli/commands/permissions.md:41).
	// Ctrl+G arrives as the rune 'g' with the Ctrl modifier, so the
	// code is the test and the text is not.
	if (key.Mod == 0 && key.Text == "e") ||
		(key.Code == 'g' && key.Mod.Contains(tea.ModCtrl)) {
		list := p.readList(p.scope, p.tab)
		p.errMsg = ""
		if n > 0 && p.cursor >= 0 && p.cursor < len(list) {
			p.form = FormEdit
			p.edit = p.cursor
			p.text = list[p.cursor]
		} else {
			p.form = FormAdd
			p.edit = -1
			p.text = ""
		}
		p.stage = StageForm
		return true
	}
	return false
}

// handleFormKey is the add/edit form's keys.
func (p *Panel) handleFormKey(key tea.Key) (bool, tea.Cmd) {
	switch key.Code {
	case tea.KeyEsc:
		p.stage = StageLists
		p.text = ""
		p.errMsg = ""
		return true, nil
	case tea.KeyEnter:
		if msg := permissions.ValidateGrant(p.text); msg != "" {
			p.errMsg = msg
			return true, nil
		}
		list := p.readList(p.scope, p.tab)
		if p.form == FormEdit && p.edit >= 0 && p.edit < len(list) {
			// Edit = delete-then-append, i.e. the rule moves to the end
			// (probe_lists.py / the checkpoint's measured behaviour).
			list = append(list[:p.edit], list[p.edit+1:]...)
		}
		list = append(list, p.text)
		p.writeList(p.scope, p.tab, list)
		p.text = ""
		p.errMsg = ""
		p.stage = StageLists
		return true, nil
	case tea.KeyBackspace:
		if r := []rune(p.text); len(r) > 0 {
			p.text = string(r[:len(r)-1])
		}
		return true, nil
	}
	if key.Mod == 0 && key.Text != "" {
		p.text += key.Text
		return true, nil
	}
	if key.Mod == 0 {
		return true, nil
	}
	return false, nil
}
