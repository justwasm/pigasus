// panel_keys.go — the /codesearch panel's keys.
//
// The panel owns the navigation of a live result list: ↑/↓ move the
// cursor over the matches and ←/→ jump to the previous/next file's
// first match.
//
// The third answer is ok: it is false when the panel does not own the
// key. The usage form, the "No results" form and a pattern error own
// nothing — the host's generic list handling takes them, so Ctrl+C
// still leaves the panel. A settled search leaves the Esc to the host
// too: the panel closes silently, the way every other panel does.
//
// Enter and Ctrl+G open the highlighted result: Enter opens it in the
// panel's own file viewer, at the matching line, and Ctrl+G opens it
// in the external editor (official-docs/cli/commands/codesearch.md:36-37).
//
// The file viewer owns the comment flow: c opens the inline comment
// editor, d drops the comment the cursor's line carries, / searches the
// file and Esc leaves the file — which opens the "Unsent Comments"
// screen when the file carries comments that were not sent yet
// (official-docs/cli/commands/codesearch.md:80-100).

package codesearchpanel

import (
	tea "charm.land/bubbletea/v2"
)

// HandleKey owns the keys of a live /codesearch result list.
//
// It answers (handled, cmd, ok). A key the panel does not own is *not*
// handled and *not* owned — the host's global handlers still see it.
func (p *Panel) HandleKey(key tea.Key) (handled bool, cmd tea.Cmd, ok bool) {
	if p == nil || len(p.hits) == 0 {
		return false, nil, false
	}
	if p.unsent != nil {
		// The "Unsent Comments" screen is modal: it claims every key
		// under it, the way the reference's own overlay does.
		return p.unsentKey(key)
	}
	if p.viewer != nil {
		// The file viewer owns the keys while a file is open.
		return p.viewerKey(key)
	}
	switch key.Code {
	case tea.KeyUp:
		p.moveCursor(-1)
	case tea.KeyDown:
		p.moveCursor(1)
	case tea.KeyLeft:
		p.moveFile(-1)
	case tea.KeyRight:
		p.moveFile(1)
	case tea.KeyEsc:
		// Esc during a live search cancels the rg child and closes the
		// panel. When the search has already settled the host's own Esc
		// closes it.
		if p.running() {
			p.cancelSearch()
			return true, p.close(), true
		}
		return false, nil, false
	case tea.KeyEnter:
		// Enter opens the highlighted result in the file viewer, at
		// the matching line.
		p.openViewer(p.current())
		return true, nil, true
	default:
		if key.Code == 'g' && key.Mod.Contains(tea.ModCtrl) {
			// Ctrl+G opens the highlighted result in the external
			// editor.
			hit := p.current()
			return true, p.openEditor(hit.File, hit.Line), true
		}
		return false, nil, false
	}
	return true, nil, true
}

// moveCursor moves the item cursor by delta, clamped to the result set,
// and scrolls the window to keep it visible.
func (p *Panel) moveCursor(delta int) {
	total := len(p.hits)
	c := p.cursor + delta
	if c < 0 {
		c = 0
	}
	if c > total-1 {
		c = total - 1
	}
	p.cursor = c
	p.follow()
}

// moveFile moves the cursor to the previous/next file's first item.
func (p *Panel) moveFile(delta int) {
	hits := p.hits
	start := p.fileStart(p.cursor)
	if delta < 0 {
		if start == 0 {
			p.cursor = 0
		} else {
			p.cursor = p.fileStart(start - 1)
		}
	} else {
		i := start
		for i < len(hits) && hits[i].File == hits[start].File {
			i++
		}
		if i >= len(hits) {
			i = len(hits) - 1
		}
		p.cursor = i
	}
	p.follow()
}

// fileStart returns the index of the first item of the file that owns
// item i.
func (p *Panel) fileStart(i int) int {
	hits := p.hits
	if i < 0 || i >= len(hits) {
		return 0
	}
	for i > 0 && hits[i-1].File == hits[i].File {
		i--
	}
	return i
}
