// panel_test.go — the /rename panel's own tests.
//
// The panel is the subject here: it is built directly, with the seams
// a test can answer, and the assertions read its own rows. The host's
// wiring (the panel's rebuild on open, the transcript block, the
// conversation name) is tested in internal/cliapp.

package renamepanel

import (
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// sgrRe matches the SGR spans the panel's rows carry.
var sgrRe = regexp.MustCompile("\x1b\\[[0-9;]*m")

// plain strips the SGR spans from a rendered row.
func plain(s string) string {
	return sgrRe.ReplaceAllString(s, "")
}

// testPanel builds a panel with recording seams.
func testPanel(seed string, submitted *string, closed *bool) *Panel {
	return New(Seams{
		Seed: func() string { return seed },
		Submit: func(name string) tea.Cmd {
			if submitted != nil {
				*submitted = name
			}
			return nil
		},
		Close: func() tea.Cmd {
			if closed != nil {
				*closed = true
			}
			return nil
		},
	})
}

// TestPanelSeed: the input opens with the seed and takes the
// keystrokes.
func TestPanelSeed(t *testing.T) {
	p := testPanel("vacation planning", nil, nil)
	if got := p.Value(); got != "vacation planning" {
		t.Errorf("value = %q, want %q", got, "vacation planning")
	}
	if !p.Focused() {
		t.Error("the panel must open focused — the first keystroke would be dropped")
	}
}

// TestPanelEmptySeed: a conversation without a name opens an empty
// input, and the placeholder is what the row shows.
func TestPanelEmptySeed(t *testing.T) {
	p := testPanel("", nil, nil)
	if got := p.Value(); got != "" {
		t.Errorf("value = %q, want empty", got)
	}
	lines := p.Lines()
	if !strings.Contains(plain(lines[2]), "type a name and press enter") {
		t.Errorf("placeholder missing for the empty value: %q", plain(lines[2]))
	}
}

// TestPanelRows: the frame is the title, a blank, the input row, a
// blank and the keyboard legend.
func TestPanelRows(t *testing.T) {
	p := testPanel("trip", nil, nil)
	lines := p.Lines()
	if len(lines) != 5 {
		t.Fatalf("got %d lines, want 5 (title, blank, input, blank, hint): %v",
			len(lines), lines)
	}
	if !strings.Contains(plain(lines[0]), "Rename Conversation") {
		t.Errorf("title line missing: %q", plain(lines[0]))
	}
	if !strings.Contains(plain(lines[2]), "Rename:") {
		t.Errorf("input row missing the prompt: %q", plain(lines[2]))
	}
	if !strings.Contains(plain(lines[4]), "enter") ||
		!strings.Contains(plain(lines[4]), "Rename") {
		t.Errorf("keyboard hint missing 'enter Rename': %q", plain(lines[4]))
	}
}

// TestPanelCharLimit: the bubbles textinput silently drops characters
// past the limit, so a 300-character value becomes a 200-character one.
func TestPanelCharLimit(t *testing.T) {
	p := testPanel("", nil, nil)
	p.SetValue(strings.Repeat("a", 300))
	if got := len([]rune(p.Value())); got != limit {
		t.Errorf("value length = %d, want %d (the limit)", got, limit)
	}
}

// TestPanelKeyNavigation: the cursor keys pass through the textinput
// — the hand-rolled buffer this panel replaced dropped them.
func TestPanelKeyNavigation(t *testing.T) {
	p := testPanel("", nil, nil)
	p.SetValue("hello world")
	if got := p.Position(); got != 11 {
		t.Fatalf("cursor = %d, want 11 (the end of the value)", got)
	}
	for i := 0; i < 2; i++ {
		handled, _ := p.HandleKey(tea.Key{Code: tea.KeyLeft})
		if !handled {
			t.Fatalf("Left #%d not handled", i+1)
		}
	}
	if got := p.Position(); got != 9 {
		t.Errorf("after 2× Left, cursor = %d, want 9", got)
	}
	if got := p.Value(); got != "hello world" {
		t.Errorf("value = %q, want unchanged", got)
	}
	handled, _ := p.HandleKey(tea.Key{Code: tea.KeyHome})
	if !handled {
		t.Fatal("Home not handled")
	}
	if got := p.Position(); got != 0 {
		t.Errorf("after Home, cursor = %d, want 0", got)
	}
}

// TestPanelKeyWordDelete: Ctrl+W deletes the word before the cursor.
func TestPanelKeyWordDelete(t *testing.T) {
	p := testPanel("", nil, nil)
	p.SetValue("hello world foo")
	handled, _ := p.HandleKey(tea.Key{Code: 'w', Mod: tea.ModCtrl})
	if !handled {
		t.Fatal("Ctrl+W not handled")
	}
	if got := p.Value(); got != "hello world " {
		t.Errorf("after the word delete, value = %q, want %q", got, "hello world ")
	}
}

// TestPanelKeyEnterSubmits: Enter hands the typed name to the host.
func TestPanelKeyEnterSubmits(t *testing.T) {
	var submitted string
	p := testPanel("trip", &submitted, nil)
	p.SetValue("summer trip 2026")
	handled, _ := p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if !handled {
		t.Fatal("Enter not handled")
	}
	if submitted != "summer trip 2026" {
		t.Errorf("submitted = %q, want %q", submitted, "summer trip 2026")
	}
}

// TestPanelKeyEscCloses: Esc leaves the panel without submitting.
func TestPanelKeyEscCloses(t *testing.T) {
	var submitted string
	closed := false
	p := testPanel("trip", &submitted, &closed)
	handled, _ := p.HandleKey(tea.Key{Code: tea.KeyEsc})
	if !handled {
		t.Fatal("Esc not handled")
	}
	if !closed {
		t.Error("Esc must close the panel")
	}
	if submitted != "" {
		t.Errorf("Esc must not submit; submitted = %q", submitted)
	}
}

// TestPanelPaste: a pasted name is inserted, never submitted.
func TestPanelPaste(t *testing.T) {
	var submitted string
	p := testPanel("trip", &submitted, nil)
	p.HandlePaste(" in 2026")
	if got := p.Value(); got != "trip in 2026" {
		t.Errorf("value = %q, want %q", got, "trip in 2026")
	}
	if submitted != "" {
		t.Errorf("a paste must not submit; submitted = %q", submitted)
	}
}
