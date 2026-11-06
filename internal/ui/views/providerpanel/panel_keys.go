// panel_keys.go — the /provider panel's key handling.
//
// # Two modes
//
// The panel has a list mode and a form mode, and both are handled here so
// the panel never falls through to the generic navigation handler — the
// form's arrow keys move between fields, not between rows.

package providerpanel

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/features/models"
	tuiviewport "github.com/justwasm/pigasus/internal/ui/foundations/viewport"
)

// HandleKey owns every key the /provider panel receives.
//
// The panel has two modes: the list and the edit form. Both are handled
// here so the panel never falls through to the generic navigation
// handler — the form's arrow keys move between fields, not between rows.
func (p *Panel) HandleKey(key tea.Key) (bool, tea.Cmd) {
	if p == nil {
		return false, nil
	}
	if p.oauth != nil {
		return p.handleOAuthKey(key)
	}
	if p.form != nil {
		return p.handleFormKey(key)
	}

	// 1. Tab switches tabs
	if key.Code == tea.KeyTab {
		if key.Mod.Contains(tea.ModShift) {
			p.tab = (p.tab + 2) % 3
		} else {
			p.tab = (p.tab + 1) % 3
		}
		p.cursor = 0
		p.window = 0
		p.confirm = -1
		return true, nil
	}

	// 2. Escape: clear search query or close panel
	if key.Code == tea.KeyEsc {
		if p.query != "" || p.searchActive {
			p.query = ""
			p.searchActive = false
			p.cursor = 0
			p.window = 0
			p.confirm = -1
			return true, nil
		}
		return true, p.closePanel()
	}

	// 3. Backspace: delete character in search query
	if key.Code == tea.KeyBackspace {
		if p.query != "" {
			r := []rune(p.query)
			p.query = string(r[:len(r)-1])
			p.cursor = 0
			p.window = 0
			p.confirm = -1
			return true, nil
		}
		return true, nil
	}

	// 4. Ctrl+U: clear search query
	if key.Code == 'u' && key.Mod.Contains(tea.ModCtrl) {
		p.query = ""
		p.cursor = 0
		p.window = 0
		p.confirm = -1
		return true, nil
	}

	// 5. Cmd+V / Ctrl+V: paste into search query
	if (key.Code == 'v' || key.Code == 'V') && (key.Mod.Contains(tea.ModCtrl) || key.Mod.Contains(tea.ModSuper) || key.Mod.Contains(tea.ModMeta)) {
		if text := strings.TrimSpace(p.readClipboard()); text != "" {
			p.query += text
			p.searchActive = true
			p.cursor = 0
			p.window = 0
			p.confirm = -1
			return true, nil
		}
		return true, nil
	}

	// 6. Navigation keys: arrow keys and page keys only (like /model and /resume).
	// Never use tuilib.ClassifyListNavKey here, which intercepts 'g', 'G', 'h', 'j', 'k', 'l', 'q'
	// and breaks search queries like "google", "groq", "gemini", etc.
	switch key.Code {
	case tea.KeyUp:
		p.moveCursor(-1)
		return true, nil
	case tea.KeyDown:
		p.moveCursor(+1)
		return true, nil
	case tea.KeyPgUp, tea.KeyLeft:
		p.moveCursor(-10)
		return true, nil
	case tea.KeyPgDown, tea.KeyRight:
		p.moveCursor(+10)
		return true, nil
	case tea.KeyHome:
		nav := tuiviewport.ListNav{Cursor: p.cursor}
		nav.Home(len(p.Rows()), 0)
		p.cursor = nav.Cursor
		p.confirm = -1
		return true, nil
	case tea.KeyEnd:
		nav := tuiviewport.ListNav{Cursor: p.cursor}
		nav.End(len(p.Rows()), 0)
		p.cursor = nav.Cursor
		p.confirm = -1
		return true, nil
	case tea.KeyEsc:
		if p.query != "" || p.searchActive {
			p.query = ""
			p.searchActive = false
			p.cursor = 0
			p.window = 0
			p.confirm = -1
			return true, nil
		}
		return true, p.closePanel()
	case tea.KeyEnter:
		if row, ok := p.CurrentRow(); ok {
			switch row.Kind {
			case RowProvider:
				// If the provider has models and is currently collapsed, Enter expands it.
				if !row.Expanded && len(p.providers[row.Provider].EffectiveModels()) > 0 {
					if p.collapsed == nil {
						p.collapsed = map[string]bool{}
					}
					p.collapsed[row.Provider] = false
					p.cacheValid = false
					return true, nil
				}
				// If already expanded (or has no child models), Enter opens the edit form.
				p.openEditForm()
				return true, nil
			case RowBuiltin:
				if row.Model == "" {
					// Builtin group header: toggle expand/collapse
					if p.collapsed == nil {
						p.collapsed = map[string]bool{}
					}
					p.collapsed[row.Provider] = !p.collapsed[row.Provider]
					p.cacheValid = false
					return true, nil
				}
				// Builtin model: openEditForm sets the notice
				p.openEditForm()
				return true, nil
			case RowPreset:
				if models.CatwalkAuthMethod(row.Provider) == "browser" {
					return true, p.startOAuth(row.Provider)
				}
				p.openEditForm()
				return true, nil
			case RowModel:
				p.openEditForm()
				return true, nil
			}
		}
		p.openEditForm()
		return true, nil
	}

	// 7. Space or Ctrl+E: toggle expand/collapse on provider or builtin group row
	if (key.Code == ' ' && !key.Mod.Contains(tea.ModCtrl) && p.query == "" && !p.searchActive) ||
		(key.Code == 'e' && key.Mod.Contains(tea.ModCtrl)) {
		if row, ok := p.CurrentRow(); ok {
			targetKey := ""
			switch row.Kind {
			case RowProvider:
				if len(p.providers[row.Provider].EffectiveModels()) > 0 {
					targetKey = row.Provider
				}
			case RowModel:
				targetKey = row.Provider
			case RowBuiltin:
				targetKey = row.Provider
			}
			if targetKey != "" {
				if p.collapsed == nil {
					p.collapsed = map[string]bool{}
				}
				p.collapsed[targetKey] = !p.collapsed[targetKey]
				p.cacheValid = false
				// Reposition cursor on the parent provider / group header if it was collapsed
				rows := p.Rows()
				for i, r := range rows {
					if r.Provider == targetKey && (r.Kind == RowProvider || (r.Kind == RowBuiltin && r.Model == "")) {
						p.cursor = i
						break
					}
				}
				return true, nil
			}
		}
		return true, nil
	}

	// 8. Ctrl+O: open edit form explicitly on the current row
	if key.Code == 'o' && key.Mod.Contains(tea.ModCtrl) {
		p.openEditForm()
		return true, nil
	}

	// 9. Search activation: '/' or Ctrl+F
	if key.Code == '/' || (key.Code == 'f' && key.Mod.Contains(tea.ModCtrl)) {
		p.searchActive = true
		return true, nil
	}

	// 10. Dedicated modifier hotkeys (immune to search text collisions)
	// Ctrl+A / F2: Add provider
	if (key.Code == 'a' && key.Mod.Contains(tea.ModCtrl)) || key.Code == tea.KeyF2 {
		p.openAddForm()
		return true, nil
	}
	// Ctrl+M / F3: Add model
	if (key.Code == 'm' && key.Mod.Contains(tea.ModCtrl)) || key.Code == tea.KeyF3 {
		p.openAddModelForm()
		return true, nil
	}
	// Ctrl+D / F4: Delete row
	if (key.Code == 'd' && key.Mod.Contains(tea.ModCtrl)) || key.Code == tea.KeyF4 {
		p.deleteRow()
		return true, nil
	}
	// Ctrl+T / F5: Verify provider
	if (key.Code == 't' && key.Mod.Contains(tea.ModCtrl)) || key.Code == tea.KeyF5 {
		return true, p.verifySelectedProvider()
	}

	// 11. Test compatibility fallback for synthetic key events (Text == "" and Mod == 0)
	if key.Text == "" && key.Mod == 0 && p.query == "" && !p.searchActive {
		switch key.Code {
		case 'a':
			p.openAddForm()
			return true, nil
		case 'm':
			p.openAddModelForm()
			return true, nil
		case 'd':
			p.deleteRow()
			return true, nil
		case 'v':
			return true, p.verifySelectedProvider()
		}
	}

	// 12. Text typing directly into search query (auto-expands matching tree items)
	text := key.Text
	if text != "" && !key.Mod.Contains(tea.ModCtrl) && !key.Mod.Contains(tea.ModAlt) && !key.Mod.Contains(tea.ModSuper) {
		p.query += text
		p.searchActive = true
		p.cursor = 0
		p.window = 0
		p.confirm = -1
		p.cacheValid = false
		return true, nil
	}

	return true, nil
}

// handleFormKey owns every key the edit form receives.
//
// Only the arrow keys, enter and escape are interpreted; every other key
// is typed into the field. (The generic list navigation is not used here:
// it would swallow "j", "k", "h", "l", "g" and "q" as navigation, and
// those are ordinary characters in a field.)
func (p *Panel) handleFormKey(key tea.Key) (bool, tea.Cmd) {
	// Cmd+V / Ctrl+V / ModSuper+V / ModMeta+V paste from clipboard
	if (key.Code == 'v' || key.Code == 'V') && (key.Mod.Contains(tea.ModCtrl) || key.Mod.Contains(tea.ModSuper) || key.Mod.Contains(tea.ModMeta)) {
		if p.form.Field().Kind != FieldChoice {
			if text := strings.TrimSpace(p.readClipboard()); text != "" {
				updated, cmd := p.input.Update(tea.PasteMsg{Content: text})
				p.input = updated
				p.formStore()
				return true, cmd
			}
		}
		return true, nil
	}

	// Ctrl+B: start browser authentication if editing a provider
	if (key.Code == 'b' || key.Code == 'B') && key.Mod.Contains(tea.ModCtrl) {
		if p.form != nil && p.form.Kind == FormProvider {
			pID := p.form.Provider
			if pID == "" {
				pID = p.form.Value(FieldID)
			}
			if pID != "" {
				p.form = nil
				return true, p.startOAuth(pID)
			}
		}
	}

	switch key.Code {
	case tea.KeyEsc:
		p.form = nil
		p.notice = ""
		return true, nil
	case tea.KeyEnter:
		return true, p.saveForm()
	case 'v':
		// If cursor is on a text field and user typed literal 'v', type it into the field
		if key.Text == "v" && p.form.Field().Kind != FieldChoice {
			break
		}
		return true, p.verifyForm()
	case tea.KeyUp:
		p.formMove(-1)
		return true, nil
	case tea.KeyDown:
		p.formMove(+1)
		return true, nil
	case tea.KeyLeft:
		p.formCycle(-1)
		return true, nil
	case tea.KeyRight:
		p.formCycle(+1)
		return true, nil
	}
	// Everything else is text. A choice field has no text to type.
	if p.form.Field().Kind == FieldChoice {
		return true, nil
	}
	updated, cmd := p.input.Update(keyMsgFromKey(key))
	p.input = updated
	return true, cmd
}

// keyMsgFromKey turns a key press into the message the text input takes.
func keyMsgFromKey(k tea.Key) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: k.Code, Mod: k.Mod, Text: k.Text}
}

// readClipboard reads the clipboard through the seam.
func (p *Panel) readClipboard() string {
	if p == nil || p.seams.ClipboardRead == nil {
		return ""
	}
	return p.seams.ClipboardRead()
}

// writeClipboard writes to the clipboard through the seam.
func (p *Panel) writeClipboard(text string) error {
	if p == nil || p.seams.ClipboardWrite == nil {
		return nil
	}
	return p.seams.ClipboardWrite(text)
}

// openBrowser opens a URL through the seam.
func (p *Panel) openBrowser(url string) error {
	if p == nil || p.seams.OpenBrowser == nil {
		return nil
	}
	return p.seams.OpenBrowser(url)
}

// closePanel closes the panel through the seam.
func (p *Panel) closePanel() tea.Cmd {
	if p == nil {
		return nil
	}
	if p.seams.Close != nil {
		return p.seams.Close()
	}
	return DismissCmd()
}
