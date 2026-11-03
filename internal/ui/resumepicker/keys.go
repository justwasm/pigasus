package resumepicker

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/internal/app/session/convstore"
	"github.com/justwasm/pigasus/internal/ui/editor/keybind"
)

// HandleKey handles a key the picker owns.
//
// The command it returns is the picker's own — the search cursor's blink,
// or the session's answer to a conversation the user picked.
func (p *Picker) HandleKey(key tea.Key, height int) tea.Cmd {
	if p == nil || !p.mode {
		return nil
	}
	// The import confirmation is a stage of its own: while it is armed it
	// owns the keys it answers with, and the picker ignores the rest.
	if p.importConfirm != "" {
		p.handleImportConfirm(key)
		return nil
	}
	// The delete confirmation is a stage of its own: while it is armed it
	// owns the keys it answers with, and the picker ignores the rest.
	if p.deleteConfirm != "" {
		p.handleDeleteConfirm(key)
		return nil
	}
	if p.treeActive() {
		return p.handleTreeKeys(key, height)
	}
	rows := p.Filtered()
	total := len(rows)
	if p.tab == 0 && key.Code == 'f' && key.Mod.Contains(tea.ModCtrl) {
		p.toggleGrouped(height)
		return nil
	}
	if p.handleNav(key, total, height) {
		return nil
	}
	if cmd, handled := p.handleActionKeys(key, rows, total); handled {
		return cmd
	}
	if cmd, handled := p.handleDeleteOrRename(key, rows, total); handled {
		return cmd
	}
	if key.Code == 'u' && key.Mod.Contains(tea.ModCtrl) {
		p.query = ""
		p.window, p.cursor = 0, 0
		return nil
	}
	if keybind.IsTypedText(key) {
		p.query += key.Text
		p.window, p.cursor = 0, 0
		p.cursorOn = true
		return BlinkCmd()
	}
	return nil
}

// handleNav handles the picker's navigation keys.
func (p *Picker) handleNav(key tea.Key, total, height int) bool {
	switch key.Code {
	case tea.KeyUp:
		p.listUp(total, height)
		return true
	case tea.KeyDown:
		p.listDown(total, height)
		return true
	case tea.KeyRight:
		p.listPage(total, 1, height)
		return true
	case tea.KeyLeft:
		p.listPage(total, -1, height)
		return true
	case tea.KeyTab:
		p.switchTab()
		return true
	}
	return false
}

// handleActionKeys handles the keys that pick, leave and edit the search
// field.
func (p *Picker) handleActionKeys(key tea.Key, rows []convstore.ConversationMeta, total int) (tea.Cmd, bool) {
	switch key.Code {
	case tea.KeyEsc:
		if p.query != "" {
			p.query = ""
			p.window, p.cursor = 0, 0
			return nil, true
		}
		p.Close()
		return nil, true
	case tea.KeyEnter:
		if total == 0 || p.cursor < 0 || p.cursor >= total {
			p.Close()
			return nil, true
		}
		picked := rows[p.cursor]
		if p.tab != 0 {
			// The Other tab imports the conversation into the CLI's
			// own store instead of resuming it: the desktop app's
			// conversation is not the CLI's yet.
			p.importConfirm = picked.ID
			return nil, true
		}
		p.Close()
		return p.resume(picked), true
	case tea.KeyBackspace:
		if p.query != "" {
			r := []rune(p.query)
			p.query = string(r[:len(r)-1])
			p.window, p.cursor = 0, 0
			p.cursorOn = true
			return BlinkCmd(), true
		}
		return nil, true
	}
	return nil, false
}

// handleDeleteOrRename handles the keys that delete a conversation or
// rename it.
func (p *Picker) handleDeleteOrRename(key tea.Key, rows []convstore.ConversationMeta, total int) (tea.Cmd, bool) {
	if key.Code == tea.KeyF4 {
		if p.tab == 0 && total > 0 && p.cursor >= 0 && p.cursor < total {
			picked := rows[p.cursor]
			if picked.ID != "" {
				// The delete asks first: F4 arms the confirmation
				// and the picker draws "[Delete? (y/n)]" next to
				// the row's title.
				p.deleteConfirm = picked.ID
			}
			return nil, true
		}
	}
	if key.Code == tea.KeyF2 {
		if p.tab == 0 && total > 0 && p.cursor >= 0 && p.cursor < total {
			picked := rows[p.cursor]
			p.mode = false
			p.list = nil
			p.rename(picked)
			return nil, true
		}
	}
	return nil, false
}

// handleDeleteConfirm answers the keys of the armed delete confirmation.
//
// 【一手】 official-docs/cli/commands/resume.md:57 — "Press Enter (or Y)
// to confirm deletion, or Esc (or N) to cancel."
func (p *Picker) handleDeleteConfirm(key tea.Key) {
	switch key.Code {
	case tea.KeyEnter:
		p.confirmDelete()
		return
	case tea.KeyEsc:
		p.deleteConfirm = ""
		return
	}
	if !keybind.IsTypedText(key) {
		return
	}
	switch strings.ToLower(key.Text) {
	case "y":
		p.confirmDelete()
	case "n":
		p.deleteConfirm = ""
	}
}

// confirmDelete deletes the conversation the confirmation is armed for and
// re-reads the store.
//
// The picker stays open: the row the user deleted is gone and the list
// around it is re-read from the store.
func (p *Picker) confirmDelete() {
	id := p.deleteConfirm
	p.deleteConfirm = ""
	if id == "" {
		return
	}
	_ = convstore.DeleteConversation(id)
	p.reload()
	if p.treeActive() {
		p.clampCursor(len(p.treeRows()))
		return
	}
	total := len(p.Filtered())
	if p.cursor >= total && total > 0 {
		p.cursor = total - 1
	}
	if p.window > p.cursor {
		p.window = p.cursor
	}
}

// handleImportConfirm answers the keys of the armed import confirmation.
//
// 【一手】 official-docs/cli/commands/resume.md:69 — "Press Enter (or Y) to
// confirm."
func (p *Picker) handleImportConfirm(key tea.Key) {
	switch key.Code {
	case tea.KeyEnter:
		p.confirmImport()
		return
	case tea.KeyEsc:
		p.importConfirm = ""
		return
	}
	if !keybind.IsTypedText(key) {
		return
	}
	switch strings.ToLower(key.Text) {
	case "y":
		p.confirmImport()
	case "n":
		p.importConfirm = ""
	}
}

// confirmImport imports the conversation the confirmation is armed for and
// resumes it.
//
// The picker closes when the import worked: the conversation is the CLI's
// own now, and the session switches to it. A failed import leaves the
// picker open and draws the reason under the rows.
func (p *Picker) confirmImport() {
	id := p.importConfirm
	p.importConfirm = ""
	if id == "" {
		return
	}
	meta, ok := p.otherMeta(id)
	if !ok {
		return
	}
	imported, err := convstore.ImportConversation(meta)
	if err != nil {
		p.importError = err.Error()
		return
	}
	p.Close()
	p.resume(imported)
}
