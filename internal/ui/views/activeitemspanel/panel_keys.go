// panel_keys.go — the active-items block's keys.
//
// The block owns the keyboard while it has the focus: ↑/↓ move the
// cursor, Esc gives the focus back to the prompt, Enter opens the row's
// detail view in /tasks, and any other key gives the focus back to the
// prompt as well.
//
// 【反编译】 model.ActiveItemsModel.Update (0x102c9e904) is the
// reference's own: a key the block does not own ends the block's focus
// and reaches the prompt, so a letter the user types lands in the
// composer.

package activeitemspanel

import (
	tea "charm.land/bubbletea/v2"

	taskregistry "github.com/justwasm/pigasus/internal/app/task"
)

// HandleKey owns the keys the block receives while it has the focus.
//
// It answers (handled, cmd). A key the block does not own is *not*
// handled — the block gives the focus back to the prompt and the key
// carries on to the composer.
func (p *Panel) HandleKey(key tea.Key) (bool, tea.Cmd) {
	if p == nil || !p.focused {
		return false, nil
	}
	items := p.items()
	total := len(items)
	if total == 0 {
		p.focused = false
		return false, nil
	}

	isUp := key.Code == tea.KeyUp || (p.vimEnabled() && (key.Code == 'k' || key.Text == "k") && key.Mod == 0)
	isDown := key.Code == tea.KeyDown || (p.vimEnabled() && (key.Code == 'j' || key.Text == "j") && key.Mod == 0)

	switch {
	case isUp:
		if p.cursor > 0 {
			p.cursor--
			p.adjustScroll(total, p.height())
		} else {
			// Defocus the block and return the focus to the prompt.
			p.focused = false
		}
		return true, nil
	case isDown:
		if p.cursor < total-1 {
			p.cursor++
			p.adjustScroll(total, p.height())
		}
		return true, nil
	case key.Code == tea.KeyEsc:
		p.focused = false
		return true, func() tea.Msg { return MsgDefocused{} }
	case key.Code == tea.KeyEnter && key.Mod == 0:
		// Plain Enter on the selected item opens its detail view in
		// /tasks. A modified Enter (shift/alt/ctrl) is the composer's
		// newline chord, so it falls through to the default branch and
		// gives the focus back instead.
		if p.cursor >= 0 && p.cursor < total {
			selected := items[p.cursor]
			isSubagent := selected.Kind == taskregistry.KindSubagent
			p.openDetail(selected.ID, isSubagent)
			p.focused = false
			return true, func() tea.Msg {
				return MsgOpenDetail{ID: selected.ID, IsSubagent: isSubagent}
			}
		}
		return true, nil
	default:
		// Any other key immediately returns the focus to the prompt
		// editor, matching agy 1.4.12 ActiveItemsModel.Update
		// (0x102c9e904).
		p.focused = false
		return false, nil
	}
}
