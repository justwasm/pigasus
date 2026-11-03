// panel_keys.go — the /effort panel's keys.
//
// The panel is a horizontal slider, so the arrow keys move the marker
// and Enter applies the tier it sits on.
//
// The panel owns every key it receives while it is open: a key the panel
// does not own is *not* handled, so the host's global handlers (Ctrl+C)
// still see it.

package effortpanel

import (
	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/ui/tuilib"
)

// pageTiers is how many tiers a page key moves the marker.
//
// The slider has three places, so a page always lands on one of the
// ends: the clone's own page size, the same one the other panels use.
const pageTiers = 10

// HandleKey owns every key the /effort panel receives.
//
// It answers (handled, cmd). A key the panel does not own is *not*
// handled, so the host's global handlers still see it — that is how
// Ctrl+C leaves the panel.
func (p *Panel) HandleKey(key tea.Key) (bool, tea.Cmd) {
	if p == nil {
		return false, nil
	}
	switch tuilib.ClassifyListNavKey(key) {
	case tuilib.NavLeft:
		p.moveCursor(-1)
		return true, nil
	case tuilib.NavRight:
		p.moveCursor(+1)
		return true, nil
	case tuilib.NavUp, tuilib.NavDown:
		// The slider is horizontal: ↑/↓ do nothing.
		return true, nil
	case tuilib.NavPageUp:
		p.moveCursor(-pageTiers)
		return true, nil
	case tuilib.NavPageDown:
		p.moveCursor(+pageTiers)
		return true, nil
	case tuilib.NavTop:
		p.SetCursor(EffortLow)
		return true, nil
	case tuilib.NavBottom:
		// The bottom of the bar is the model's heaviest level, which is
		// not always the reference's own three ("high" is the third of
		// five for a model that publishes "xhigh" and "max").
		p.SetCursor(len(p.tiers()) - 1)
		return true, nil
	case tuilib.NavSelect:
		return true, p.apply()
	case tuilib.NavCancel:
		return true, p.close()
	}
	switch key.Code {
	case tea.KeyEnter, tea.KeyTab, tea.KeyBackspace:
		return true, nil
	}
	if key.Mod == 0 && key.Text != "" {
		return true, nil
	}
	return false, nil
}
