// panel_keys.go — the /skills picker's keys.
//
// The panel owns every key it receives, so the host's generic list
// navigation never sees them: the cursor moves with the arrow keys, the
// detail view scrolls with the pager keys, and a key the panel does not
// own falls through so the global handlers (Ctrl+C) still see it.
//
// The list's own navigation is the reference's: ↑/↓ move the cursor, Enter
// opens the detail view and Esc closes the panel. The leftover list
// navigation — 'j'/'k' navigate, 'g'/'G' jump to the ends, 'q' closes the
// panel, pgup/pgdown page — is the clone's, and the panel answers it
// itself now that it owns the cursor.
//
// The detail view is a document viewer: it takes the pager's keys
// (tuilib.ClassifyViewerNavKey), Esc returns to the list, and every other
// unmodified key is swallowed so it never reaches the prompt.

package skillsui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/ui/tuilib"
)

// HandleKey owns every key the /skills panel receives.
//
// It answers (handled, cmd). A key the panel does not own is *not*
// handled, so the host's global handlers still see it — that is how
// Ctrl+C leaves the panel.
func (p *Panel) HandleKey(key tea.Key) (bool, tea.Cmd) {
	if p == nil {
		return false, nil
	}
	if p.detail {
		return p.handleDetailKey(key)
	}
	return p.handleListKey(key)
}

// handleListKey handles a key while the grouped list is up.
func (p *Panel) handleListKey(key tea.Key) (bool, tea.Cmd) {
	switch tuilib.ClassifyListNavKey(key) {
	case tuilib.NavUp:
		p.moveCursor(false)
		return true, nil
	case tuilib.NavDown:
		p.moveCursor(true)
		return true, nil
	case tuilib.NavPageUp:
		p.pageCursor(-10)
		return true, nil
	case tuilib.NavPageDown:
		p.pageCursor(+10)
		return true, nil
	case tuilib.NavTop:
		p.cursor = 0
		return true, nil
	case tuilib.NavBottom:
		if n := p.RowCount(); n > 0 {
			p.cursor = n - 1
		}
		return true, nil
	case tuilib.NavCancel:
		if p.seams.Close != nil {
			return true, p.seams.Close()
		}
		return true, func() tea.Msg { return MsgDismissed{} }
	case tuilib.NavSelect:
		p.openDetail()
		return true, nil
	}
	// The tail the host's generic panel handler owns: Enter, Tab and
	// Backspace are swallowed, and so is any printable key.
	switch key.Code {
	case tea.KeyEnter, tea.KeyTab, tea.KeyBackspace:
		return true, nil
	}
	if key.Mod == 0 && key.Text != "" {
		return true, nil
	}
	return false, nil
}

// handleDetailKey handles a key while the detail view is up.
func (p *Panel) handleDetailKey(key tea.Key) (bool, tea.Cmd) {
	width, height := p.size()
	vis := SkillsDetailVisible(height)
	page := max(1, vis-1)
	total := len(p.detailDocument(p.selected(), width)) + 1
	maxOffset := max(0, total-vis)

	action := tuilib.ClassifyViewerNavKey(key)
	if action == tuilib.NavCancel {
		p.detail = false
		return true, nil
	}
	if p.applyDetailNav(action, page, maxOffset) {
		return true, nil
	}
	return key.Mod == 0, nil
}

// applyDetailNav applies a viewer navigation action to the detail view's
// scroll window.
//
// It answers false when the action is not a scroll, so the caller can
// fall through.
func (p *Panel) applyDetailNav(action tuilib.NavAction, page, maxOffset int) bool {
	half := max(1, page/2)
	switch action {
	case tuilib.NavUp:
		p.window = ClampSkillsWindow(p.window-1, maxOffset)
	case tuilib.NavDown:
		p.window = ClampSkillsWindow(p.window+1, maxOffset)
	case tuilib.NavHalfPageUp:
		p.window = ClampSkillsWindow(p.window-half, maxOffset)
	case tuilib.NavHalfPageDown:
		p.window = ClampSkillsWindow(p.window+half, maxOffset)
	case tuilib.NavPageUp:
		p.window = ClampSkillsWindow(p.window-page, maxOffset)
	case tuilib.NavPageDown:
		p.window = ClampSkillsWindow(p.window+page, maxOffset)
	case tuilib.NavTop:
		p.window = 0
	case tuilib.NavBottom:
		p.window = maxOffset
	default:
		return false
	}
	return true
}

// moveCursor moves the cursor one skill up or down.
//
// The reference's rule: the cursor ranges over -1 (nothing selected) and
// the catalogue's last index, ↓ from -1 lands on 0, and both ends clamp —
// ↑ on the first skill stays put, it does not clear the selection.
func (p *Panel) moveCursor(down bool) {
	n := p.RowCount()
	if n == 0 {
		p.cursor = -1
		return
	}
	if down {
		if p.cursor < n-1 {
			p.cursor++
		}
		return
	}
	if p.cursor > 0 {
		p.cursor--
	}
}

// pageCursor moves the cursor by a page, clamped into the catalogue.
func (p *Panel) pageCursor(delta int) {
	n := p.RowCount()
	if n <= 0 {
		return
	}
	c := p.cursor + delta
	if c < 0 {
		c = 0
	} else if c >= n {
		c = n - 1
	}
	p.cursor = c
}

// openDetail opens the per-skill detail view.
//
// A cursor that names no skill opens nothing — the reference's Enter on
// an unselected row is a no-op.
func (p *Panel) openDetail() {
	if p.cursor < 0 || p.cursor >= p.RowCount() {
		return
	}
	p.detail = true
}
