// interactive.go — the interactive /context usage panel.
//
// The reference (`agy`) answers `/context` with an interactive panel:
// keyboard navigation (↑/↓, pgup/pgdown, home/end, esc) scrolls the
// breakdown and file lists, while mouse dragging selects text and
// copies it to the clipboard.
package contextpanel

import (
	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
)

// Seams are the host-provided facts the interactive panel needs.
type Seams struct {
	State  func() State
	Height func() int
	Width  func() int
	Close  func() tea.Cmd
}

// Panel is the interactive /context usage panel.
type Panel struct {
	seams  Seams
	scroll int
}

// New returns a new interactive /context panel.
func New(seams Seams) *Panel {
	return &Panel{seams: seams}
}

// RowCount returns the total number of lines in the complete breakdown.
func (p *Panel) RowCount() int {
	if p == nil || p.seams.State == nil {
		return 0
	}
	st := p.seams.State()
	return len(AllLines(st))
}

// Lines returns the rendered lines for the panel, respecting viewport height.
func (p *Panel) Lines() []string {
	if p == nil || p.seams.State == nil {
		return nil
	}
	st := p.seams.State()
	if p.seams.Width != nil {
		st.Width = p.seams.Width()
	}
	h := 0
	if p.seams.Height != nil {
		h = p.seams.Height()
	}
	if h > 0 {
		// Budget for inline body lines before footer (reserve room for prompt/status)
		maxRows := h - 4
		if maxRows < 14 {
			maxRows = 14
		}
		st.MaxRows = maxRows
	}
	st.ScrollOffset = p.scroll
	return Lines(st)
}

// HandleKey processes keyboard navigation for the /context panel.
func (p *Panel) HandleKey(key tea.Key) (bool, tea.Cmd) {
	if p == nil {
		return false, nil
	}
	switch tuilib.ClassifyListNavKey(key) {
	case tuilib.NavUp:
		p.scroll--
		if p.scroll < 0 {
			p.scroll = 0
		}
		return true, nil
	case tuilib.NavDown:
		p.scroll++
		total := p.RowCount()
		maxScroll := total - 10
		if maxScroll < 0 {
			maxScroll = 0
		}
		if p.scroll > maxScroll {
			p.scroll = maxScroll
		}
		return true, nil
	case tuilib.NavPageUp:
		p.scroll -= 10
		if p.scroll < 0 {
			p.scroll = 0
		}
		return true, nil
	case tuilib.NavPageDown:
		p.scroll += 10
		total := p.RowCount()
		maxScroll := total - 10
		if maxScroll < 0 {
			maxScroll = 0
		}
		if p.scroll > maxScroll {
			p.scroll = maxScroll
		}
		return true, nil
	case tuilib.NavTop:
		p.scroll = 0
		return true, nil
	case tuilib.NavBottom:
		total := p.RowCount()
		maxScroll := total - 10
		if maxScroll < 0 {
			maxScroll = 0
		}
		p.scroll = maxScroll
		return true, nil
	case tuilib.NavCancel:
		if key.Code == tea.KeyEsc || key.String() == "esc" {
			if p.seams.Close != nil {
				return true, p.seams.Close()
			}
		}
		return true, nil
	}
	return false, nil
}
