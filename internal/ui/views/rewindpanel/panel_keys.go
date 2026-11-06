// panel_keys.go — the /rewind picker's keys.
//
// The panel owns every key it receives: the arrows move its own
// cursor, Enter rewinds and Esc leaves. A key the panel does not own
// is reported as unhandled, so the host's global handlers (Ctrl+C)
// still see it.

package rewindpanel

import (
	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/ui/tuilib"
)

// HandleKey routes a key to the picker.
//
// The picker owns the navigation: ↑/↓ move the cursor over the turns,
// pgup/pgdown jump by ten, home/end jump to the ends, Enter rewinds
// and Esc leaves.
func (p *Panel) HandleKey(key tea.Key) (bool, tea.Cmd) {
	if p == nil {
		return false, nil
	}
	switch key.Code {
	case tea.KeyEnter:
		if cmd := p.pick(); cmd != nil {
			return true, cmd
		}
		return true, p.close()
	}
	switch tuilib.ClassifyListNavKey(key) {
	case tuilib.NavUp:
		p.moveCursor(-1)
		return true, nil
	case tuilib.NavDown:
		p.moveCursor(1)
		return true, nil
	case tuilib.NavPageUp:
		p.moveCursor(-10)
		return true, nil
	case tuilib.NavPageDown:
		p.moveCursor(+10)
		return true, nil
	case tuilib.NavTop:
		p.setCursor(0)
		return true, nil
	case tuilib.NavBottom:
		p.setCursor(p.RowCount() - 1)
		return true, nil
	case tuilib.NavCancel:
		return true, p.close()
	}
	return false, nil
}
