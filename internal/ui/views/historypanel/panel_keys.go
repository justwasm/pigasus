package historypanel

import (
	tea "charm.land/bubbletea/v2"
	tuiviewport "github.com/justwasm/pigasus/internal/ui/foundations/viewport"
)

// HandleKey handles interactive key events for the prompt history panel.
func (p *Panel) HandleKey(key tea.Key) (bool, tea.Cmd) {
	if p == nil {
		return false, nil
	}

	switch key.Code {
	case tea.KeyEsc:
		if p.query != "" {
			p.query = ""
			p.cursor = 0
			p.window = 0
			return true, nil
		}
		return true, p.close()

	case tea.KeyEnter:
		if entry, ok := p.currentEntry(); ok {
			var cmd tea.Cmd
			if p.seams.Pick != nil {
				cmd = p.seams.Pick(entry.Text)
			}
			return true, cmd
		}
		return true, p.close()

	case tea.KeyTab:
		if p.scope == ScopeWorkspace {
			p.scope = ScopeAll
		} else {
			p.scope = ScopeWorkspace
		}
		p.cursor = 0
		p.window = 0
		return true, nil

	case tea.KeyBackspace:
		if p.query != "" {
			runes := []rune(p.query)
			p.query = string(runes[:len(runes)-1])
			p.cursor = 0
			p.window = 0
		}
		return true, nil

	case tea.KeyUp:
		p.moveCursor(-1)
		return true, nil

	case tea.KeyDown:
		p.moveCursor(1)
		return true, nil

	case tea.KeyPgUp:
		p.moveCursor(-8)
		return true, nil

	case tea.KeyPgDown:
		p.moveCursor(8)
		return true, nil

	case tea.KeyHome:
		p.SetCursor(0)
		return true, nil

	case tea.KeyEnd:
		p.SetCursor(p.RowCount() - 1)
		return true, nil
	}

	if key.Mod == 0 {
		if key.Text != "" {
			p.query += key.Text
			p.cursor = 0
			p.window = 0
			return true, nil
		}
		if key.Code == ' ' {
			p.query += " "
			p.cursor = 0
			p.window = 0
			return true, nil
		}
	}

	return false, nil
}

func (p *Panel) moveCursor(delta int) {
	count := p.RowCount()
	if count == 0 {
		p.cursor = 0
		p.window = 0
		return
	}
	nav := tuiviewport.ListNav{Cursor: p.cursor}
	if delta < 0 {
		nav.MoveUp(-delta, count, 0)
	} else {
		nav.MoveDown(delta, count, 0)
	}
	p.SetCursor(nav.Cursor)
}
