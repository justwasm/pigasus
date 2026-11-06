// Package creditspanel holds the /credits panel: the model's credit
// balance and the two actions the panel offers.
//
// # What it is
//
// The reference (`agy`) answers `/credits` with a small panel: the
// credit balance of the current model and the two actions — "Get More
// AI Credits" and "See Activity".
//
// # Where it lives
//
// The panel itself is here: the two actions, the cursor, the frame and
// the keys. The host keeps only the seam — the way out of the panel.
//
// The panel is rebuilt every time it opens, so a stale cursor is never
// shown.
package creditspanel

import (
	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/palette"
)

// Actions are the reference's two credit actions, in the order the
// panel lists them.
var Actions = [2]string{"Get More AI Credits", "See Activity"}

// Seams are the host's answers the panel cannot answer itself.
//
// Every one of them is optional: a panel built without a seam behaves
// as if the seam were answered with "nothing happened", which keeps the
// package usable from a test without a running TUI.
type Seams struct {
	// Close leaves the panel.
	Close func() tea.Cmd

	// OpenURL opens the external billing/pricing/subscription portal URL.
	OpenURL func(url string) error
}

// Panel is the /credits panel.
//
// The panel owns the whole surface: the two actions, the cursor, the
// frame and the keys. Nothing of it is left in the host but the seams.
type Panel struct {
	seams Seams

	// cursor is the selected action.
	cursor int
}

// New builds the /credits panel.
func New(seams Seams) *Panel {
	return &Panel{seams: seams}
}

// Cursor is the selected action's index.
func (p *Panel) Cursor() int {
	if p == nil {
		return 0
	}
	return p.cursor
}

// SetCursor puts the cursor on the action at the given index.
//
// The index is clamped into the action list, so a stale index cannot
// strand the cursor on an action that is not on screen.
func (p *Panel) SetCursor(idx int) {
	if p == nil {
		return
	}
	if idx < 0 {
		idx = 0
	}
	if idx >= len(Actions) {
		idx = len(Actions) - 1
	}
	p.cursor = idx
}

// RowCount is the number of selectable rows: the two actions.
func (p *Panel) RowCount() int {
	return len(Actions)
}

// close leaves the panel.
func (p *Panel) close() tea.Cmd {
	if p == nil || p.seams.Close == nil {
		return nil
	}
	return p.seams.Close()
}

// ---- the panel's frame ----------------------------------------------------

// The panel's own SGR spans.
//
// The palette's colours follow the selected colour scheme, so the
// scheme-dependent ones are read from the palette at draw time.
const (
	// panelBoldOpen is the bold span the reference puts on the
	// section labels.
	panelBoldOpen = "\x1b[1m"
	// panelBoldClose closes the bold span.
	panelBoldClose = "\x1b[0m"
)

// Lines renders the panel: the credit balance and the two actions.
func (p *Panel) Lines() []string {
	if p == nil {
		return nil
	}
	lines := []string{
		palette.BlueOpen + "Pigasus CLI" + palette.BlueClose + "  " +
			panelBoldOpen + palette.SubtleOpen + "credits" + panelBoldClose,
		panelBoldOpen + palette.SubtleOpen + "Model AI Credits" + panelBoldClose,
		"",
		panelBoldOpen + "Remaining AI Credits" + panelBoldClose + ": " +
			panelBoldOpen + palette.BlueOpen + "AI Credits not enabled (enable in /settings)" + panelBoldClose,
		"",
		panelBoldOpen + palette.SubtleOpen + "Actions" + panelBoldClose,
		"",
	}
	for i, a := range Actions {
		if i == p.cursor {
			lines = append(lines, palette.BlueOpen+"> "+a+palette.BlueClose)
		} else {
			lines = append(lines, "  "+a)
		}
	}
	lines = append(lines, "",
		tuilib.PanelKeyboardHint("↑/↓", "Navigate", "enter", "Select / Toggle", "esc", "Close"))
	return lines
}

// ---- the panel's keys ------------------------------------------------------

// HandleKey routes one key to the panel.
//
// The returned bool says whether the panel owned the key; the returned
// command is the host's (the panel itself has none).
//
// The navigation is the list navigation the other panels use (the tuilib
// classifier); Enter is swallowed, because the reference's actions are
// inert in the clone — the clone has no credits endpoint to act on.
func (p *Panel) HandleKey(key tea.Key) (bool, tea.Cmd) {
	if p == nil {
		return false, nil
	}
	n := p.RowCount()
	switch tuilib.ClassifyListNavKey(key) {
	case tuilib.NavUp:
		if p.cursor > 0 {
			p.cursor--
		}
		return true, nil
	case tuilib.NavDown:
		if p.cursor < n-1 {
			p.cursor++
		}
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
		if p.seams.Close != nil {
			return true, p.seams.Close()
		}
		return true, func() tea.Msg { return MsgClose{} }
	}
	switch key.Code {
	case tea.KeyEnter:
		var targetURL string
		switch p.cursor {
		case 0:
			// "Get More AI Credits" -> pricing and purchase portal
			targetURL = "https://one.google.com/ai-premium"
		case 1:
			// "See Activity" -> subscription and activity portal
			targetURL = "https://myaccount.google.com/subscriptions"
		}
		if p.seams.OpenURL != nil && targetURL != "" {
			_ = p.seams.OpenURL(targetURL)
		}
		return true, func() tea.Msg { return MsgOpenURL{URL: targetURL} }
	case tea.KeyTab, tea.KeyBackspace:
		return true, nil
	}
	if key.Mod == 0 && key.Text != "" {
		return true, nil
	}
	return false, nil
}

// pageCursor moves the cursor by delta rows and clamps it into the
// action list.
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
