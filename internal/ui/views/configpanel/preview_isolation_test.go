package configpanel

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/palette"
)

// TestColorSchemePreviewMovesTheCardOnly is the regression test for the
// four elements the user reported as changing when agy's do not.
//
// 【一手】 agy 1.5.18, measured through tuialign (46_scheme_chrome_fixed),
// arrows in the Color Scheme submenu, light → dark, nothing committed:
//
//	preview card border   #DADCE0 -> #3C4043   (the card previews)
//	banner body text      #5F6368 -> #5F6368   (the chrome does not)
//	composer rules        #DADCE0 -> #DADCE0   (the chrome does not)
//	"Keyboard:" footer    #5F6368 -> #5F6368   (the chrome does not)
//	option marker         #4285F4 -> #4285F4   (the chrome does not)
//
// The panel draws the card from the hovered scheme on its own, so a move
// must change the card and leave the process's scheme — and therefore
// every other row on the screen — exactly where it was.
func TestColorSchemePreviewMovesTheCardOnly(t *testing.T) {
	testHome(t, map[string]any{"colorScheme": "light"})

	p := New(Seams{ApplyTheme: func(scheme string) tea.Cmd {
		t.Errorf("a move inside the submenu applied %q globally", scheme)
		return nil
	}})
	p.SetCursor(rowIndex(t, p, "Color Scheme"))
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if !p.SubmenuOpen() {
		t.Fatal("expected the submenu to be open")
	}

	before, _ := palette.CurrentScheme()
	footerBefore := p.footerBytes(t)

	// Walk from "light" (index 1) to "dark" (index 4).
	for i := 0; i < 3; i++ {
		p.HandleKey(tea.Key{Code: tea.KeyDown})
	}

	after, _ := palette.CurrentScheme()
	if after != before {
		t.Errorf("the process's scheme moved on a preview: %q -> %q", before, after)
	}
	if got := p.footerBytes(t); got != footerBefore {
		t.Errorf("the \"Keyboard:\" footer changed on a preview:\n got %q\nwant %q", got, footerBefore)
	}

	// The card itself must have moved, or the test above proves nothing:
	// a panel that previewed nothing at all would pass every assertion so
	// far.
	if !strings.Contains(p.cardBytes(t), palette.Schemes["dark"].Border.Seq(false)) {
		t.Error("the preview card did not take the hovered scheme's border colour")
	}
}

// cardBytes is the panel's Color Scheme card as it renders now.
func (p *Panel) cardBytes(t *testing.T) string {
	t.Helper()
	row := p.Rows()[rowIndex(t, p, "Color Scheme")]
	return strings.Join(p.colorSchemePreviewLines(row, p.rawDisplayValue(row)), "\n")
}

// footerBytes is the panel's "Keyboard:" legend row as it renders now.
func (p *Panel) footerBytes(t *testing.T) string {
	t.Helper()
	row := p.Rows()[rowIndex(t, p, "Color Scheme")]
	lines := p.colorSchemePreviewLines(row, p.rawDisplayValue(row))
	if len(lines) == 0 {
		t.Fatal("the preview rendered no lines")
	}
	return lines[len(lines)-1]
}
