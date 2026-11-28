// panel_keys.go — the /artifact panel's keys.
//
// # What it is
//
// The panel owns every key it receives while it is open: the list owns the
// cursor, the preview toggle, the editor and the open, and the viewer owns the
// scroll window. A key the panel does not own is not handled, so the host's
// global handlers (Ctrl+C) still see it.
//
// # 【一手】 the bindings
//
// The reference's picker legend is the row cycle 185 captured:
//
//	Keyboard: ↑/↓ Navigate  p preview  enter open  ctrl+g open in editor  esc Dismiss
//
// so ↑/↓ move the cursor, `p` toggles the inline preview, `enter` opens the
// viewer, `ctrl+g` opens the file in the configured editor and `esc` leaves the
// panel. official-docs/cli/artifacts.md adds the review keys (`y`/`n`,
// shift+A/R, `h`/`l`) — those depend on review state the clone does not have,
// so they are not bound here; see the package comment.
//
// The viewer is modal and owns the pager's keys (see panel_viewer.go).

package artifactpanel

import (
	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/ui/tuilib"
)

// HandleKey owns every key the /artifact panel receives.
//
// It answers (handled, cmd). A key the panel does not own is *not* handled, so
// the host's global handlers still see it — that is how Ctrl+C leaves the
// panel.
func (p *Panel) HandleKey(key tea.Key) (bool, tea.Cmd) {
	if p == nil {
		return false, nil
	}
	if p.viewer {
		return p.viewerKey(key)
	}
	return p.listKey(key)
}

// listKey handles a key while the picker is up.
func (p *Panel) listKey(key tea.Key) (bool, tea.Cmd) {
	// Ctrl+C is the one key the panel does not swallow: it is the global
	// "leave" key.
	if isCtrlC(key) {
		return false, nil
	}

	// The list rule owns the movement keys.
	switch action := tuilib.ClassifyListNavKey(key); action {
	case tuilib.NavUp, tuilib.NavDown, tuilib.NavPageUp, tuilib.NavPageDown,
		tuilib.NavTop, tuilib.NavBottom:
		p.cursor = panelCursor(p.cursor, action, p.RowCount())
		return true, nil
	case tuilib.NavCancel:
		// 【一手】 the reference's picker legend binds "esc Dismiss" and
		// nothing else: `q` is not a way out of /artifact (the generic
		// list classifier treats it as a cancel, so it is filtered here).
		if key.Code == tea.KeyEsc {
			return true, p.close()
		}
		return true, nil
	case tuilib.NavSelect:
		// The reference's "enter open": the row's viewer.
		if _, ok := p.Selected(); ok {
			p.viewer = true
			p.window = 0
		}
		return true, nil
	}

	if isCtrlG(key) {
		// The reference's "ctrl+g open in editor".
		if ref, ok := p.Selected(); ok && p.seams.OpenInEditor != nil {
			return true, p.seams.OpenInEditor(ref.Path)
		}
		return true, nil
	}

	if key.Mod == 0 {
		switch key.Code {
		case tea.KeyEnter:
			// The reference's "enter open": the row's viewer.
			if _, ok := p.Selected(); ok {
				p.viewer = true
				p.window = 0
			}
			return true, nil
		case tea.KeyEsc:
			return true, p.close()
		case 'p':
			// The reference's "p preview": the inline preview under
			// the selected row.
			p.preview = !p.preview
			return true, nil
		}
	}

	// Every other key is swallowed: the panel is modal, so a stray letter
	// never reaches the prompt.
	return true, nil
}
