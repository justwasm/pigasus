// panel_keys.go — the /feedback panel's keys.
//
// The panel owns every key it receives: the picker's navigation, the
// "1"-"6" shortcuts that pick a category and continue, the Esc that
// leaves, and the free-text form's editing keys. A key the panel does
// not own is answered with handled=false, so the host's global handlers
// (Ctrl+C) still see it.

package feedbackpanel

import (
	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/ui/editor/keybind"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
)

// HandleKey routes one key to the panel.
//
// The returned bool says whether the panel owned the key; the returned
// command is the host's (the panel itself has none).
func (p *Panel) HandleKey(key tea.Key) (bool, tea.Cmd) {
	if p == nil {
		return false, nil
	}
	if p.stage > 0 {
		return p.handleFormKey(key)
	}
	return p.handlePickerKey(key)
}

// handlePickerKey is the category picker's half.
//
// The navigation is the list navigation the other panels use (the
// tuilib classifier), with one difference: the reference *wraps* — the
// last row's Down lands on the first and the first row's Up lands on
// the last.
func (p *Panel) handlePickerKey(key tea.Key) (bool, tea.Cmd) {
	n := p.RowCount()
	switch tuilib.ClassifyListNavKey(key) {
	case tuilib.NavUp:
		p.cursor = (p.cursor - 1 + n) % n
		return true, nil
	case tuilib.NavDown:
		p.cursor = (p.cursor + 1) % n
		return true, nil
	case tuilib.NavPageUp:
		p.cursor = p.pageCursor(-10, n)
		return true, nil
	case tuilib.NavPageDown:
		p.cursor = p.pageCursor(+10, n)
		return true, nil
	case tuilib.NavTop:
		p.cursor = 0
		return true, nil
	case tuilib.NavBottom:
		p.cursor = n - 1
		return true, nil
	case tuilib.NavCancel:
		return true, p.close()
	}
	// Enter continues to the free-text form with the selected
	// category.
	if key.Code == tea.KeyEnter {
		p.stage = 1
		return true, nil
	}
	// "1"-"6" pick a category and continue in one keypress.
	if key.Mod == 0 && len(key.Text) == 1 && key.Text[0] >= '1' && key.Text[0] <= '6' {
		p.cursor = int(key.Text[0] - '1')
		p.stage = 1
		return true, nil
	}
	// The picker has no text field, but it swallows the keys that
	// would otherwise reach the transcript.
	switch key.Code {
	case tea.KeyTab, tea.KeyBackspace:
		return true, nil
	}
	if key.Mod == 0 && key.Text != "" {
		return true, nil
	}
	return false, nil
}

// handleFormKey is the free-text form's half.
//
// The form is a one-line buffer: printable keys append, backspace
// deletes, enter (or ctrl+s) submits and esc returns to the category
// picker with the draft discarded.
//
// Cycle 221: the clone's Enter submits. The reference's stage-2 form
// is a textarea whose legend advertises ctrl+s (fin_fb_stage2.raw.txt),
// so the reference binds Enter to a newline and ctrl+s to submit; but
// the clone has no upload endpoint and the user can only ever see the
// local-only echo, so a single Enter-submit binding matches the panel's
// UX. ctrl+s is kept as a parallel submit key so muscle memory from the
// reference still works. See docs/cycles/cycle-221/.
func (p *Panel) handleFormKey(key tea.Key) (bool, tea.Cmd) {
	switch {
	case key.Code == tea.KeyEsc:
		p.stage = 0
		p.text = ""
		return true, nil
	case key.Code == tea.KeyEnter:
		p.submit()
		return true, nil
	case key.Code == 's' && key.Mod.Contains(tea.ModCtrl):
		p.submit()
		return true, nil
	case key.Code == tea.KeyBackspace:
		if r := []rune(p.text); len(r) > 0 {
			p.text = string(r[:len(r)-1])
		}
		return true, nil
	}
	if keybind.IsTypedText(key) {
		p.text += key.Text
		return true, nil
	}
	if key.Mod == 0 {
		return true, nil
	}
	return false, nil
}

// pageCursor moves the cursor by delta rows and clamps it into the
// category list.
func (p *Panel) pageCursor(delta, count int) int {
	if count <= 0 {
		return 0
	}
	c := p.cursor + delta
	if c < 0 {
		c = 0
	} else if c >= count {
		c = count - 1
	}
	return c
}
