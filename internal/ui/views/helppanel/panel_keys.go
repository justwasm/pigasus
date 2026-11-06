// panel_keys.go — the overlay's keys and its list navigation.
//
// The overlay is modal: while it is up it claims every key it
// receives, so nothing reaches the composer or the host's global
// handlers. Only the keys the overlay owns change its state — the
// arrows walk the list and the tabs, Home/End jump to the ends, and
// "q" (or Esc) closes it.
//
// The host's Esc handler runs before this one, so the overlay is also
// closed by Esc through the host's own path — which writes the same
// exit echo.

package helppanel

import (
	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/ui/slashcommands"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
)

// Update handles a key while the overlay is open in the Elm/Bubbletea single-direction
// message passing style. It reports whether the key was handled and returns an optional tea.Cmd.
func (p *Panel) Update(key tea.Key) (bool, tea.Cmd) {
	if p == nil || !p.open {
		return false, nil
	}
	var cmd tea.Cmd
	switch tuilib.ClassifyListNavKey(key) {
	case tuilib.NavRight:
		p.nextTab()
	case tuilib.NavLeft:
		p.prevTab()
	case tuilib.NavUp:
		p.listUp(p.listLen())
	case tuilib.NavDown:
		p.listDown(p.listLen())
	case tuilib.NavTop:
		p.listHome()
	case tuilib.NavBottom:
		p.listEnd(p.listLen())
	case tuilib.NavCancel:
		p.open = false
		p.respond(tuilib.SlashEchoLines("/help", "Exited /help command"))
		cmd = DismissCmd(true)
	case tuilib.NavSelect:
		cmd = p.selectCurrentCmd()
	}
	if key.Code == tea.KeyTab {
		p.nextTab()
	}
	return true, cmd
}

// HandleKey handles a key while the overlay is open.
//
// It reports whether the key was handled. While the overlay is open
// every key is handled; a closed overlay handles none.
func (p *Panel) HandleKey(key tea.Key) bool {
	handled, _ := p.Update(key)
	return handled
}

// selectCurrentCmd handles Enter on the active tab and yields the appropriate tea.Cmd.
func (p *Panel) selectCurrentCmd() tea.Cmd {
	if p == nil || !p.open {
		return nil
	}
	if p.tab == 1 {
		cmds := slashcommands.Sorted()
		if p.cursor >= 0 && p.cursor < len(cmds) {
			cmd := cmds[p.cursor].Name
			p.Close()
			if p.seams.SelectCommand != nil {
				p.seams.SelectCommand(cmd)
			}
			return SelectCommandCmd(cmd)
		}
	}
	p.Close()
	return DismissCmd(false)
}

// nextTab moves to the next tab, wrapping at the end.
//
// The window and the cursor reset: the reference does not preserve a
// per-tab scroll position.
func (p *Panel) nextTab() {
	n := len(p.tabs())
	if n == 0 {
		return
	}
	p.tab = (p.tab + 1) % n
	p.window, p.cursor = 0, 0
}

// prevTab moves to the previous tab, wrapping at the start.
//
// The window and the cursor reset, as they do on nextTab.
func (p *Panel) prevTab() {
	n := len(p.tabs())
	if n == 0 {
		return
	}
	p.tab = (p.tab + n - 1) % n
	p.window, p.cursor = 0, 0
}

// clampWindow keeps the window inside the catalogue after a tab switch
// or a terminal resize. The reference always resets to window 0 on a
// tab switch, so this is a safety net rather than the primary path.
func (p *Panel) clampWindow(total int) {
	if total <= 0 {
		p.window = 0
		p.cursor = 0
		return
	}
	size := p.listWindowSize()
	maxWindow := total - size
	if maxWindow < 0 {
		maxWindow = 0
	}
	if p.window > maxWindow {
		p.window = maxWindow
	}
	if p.window < 0 {
		p.window = 0
	}
	if p.cursor >= total {
		p.cursor = total - 1
	}
	if p.cursor < 0 {
		p.cursor = 0
	}
}

// listDown moves the cursor down one row. When the cursor would leave
// the bottom of the window, the window scrolls one row; when the
// cursor would leave the catalogue, it wraps to the first row and the
// window resets to the top.
func (p *Panel) listDown(total int) {
	if total <= 0 {
		return
	}
	p.clampWindow(total)
	size := p.listWindowSize()
	p.cursor++
	if p.cursor >= total {
		p.cursor = 0
		p.window = 0
		return
	}
	if p.cursor >= p.window+size {
		p.window = p.cursor - size + 1
	}
}

// listUp moves the cursor up one row, scrolling the window up when
// the cursor leaves the top edge and wrapping to the last row when it
// would leave the catalogue.
func (p *Panel) listUp(total int) {
	if total <= 0 {
		return
	}
	p.clampWindow(total)
	size := p.listWindowSize()
	p.cursor--
	if p.cursor < 0 {
		p.cursor = total - 1
		p.window = total - size
		if p.window < 0 {
			p.window = 0
		}
		return
	}
	if p.cursor < p.window {
		p.window = p.cursor
	}
}

// listHome jumps to the first row and resets the window to the top.
func (p *Panel) listHome() {
	p.cursor = 0
	p.window = 0
}

// listEnd jumps to the last row and scrolls the window to the bottom.
func (p *Panel) listEnd(total int) {
	if total <= 0 {
		return
	}
	p.cursor = total - 1
	p.window = total - p.listWindowSize()
	if p.window < 0 {
		p.window = 0
	}
}
