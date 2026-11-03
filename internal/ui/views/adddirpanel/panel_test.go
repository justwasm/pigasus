// panel_test.go — the /add-dir panel's own tests.
//
// The panel is the subject here: it is built directly, with the seams
// a test can answer, and the assertions read its own rows. The host's
// wiring (the trust store the list comes from, the transcript block)
// is tested in internal/cliapp.

package adddirpanel

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

// plainLines strips the SGR spans from every row.
func plainLines(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = plain(l)
	}
	return out
}

// testPanel builds a panel over a fixed trusted list.
func testPanel(trusted []string, submitted *string, closed *bool) *Panel {
	return New(Seams{
		Trusted: func() []string { return trusted },
		Submit: func(path string) tea.Cmd {
			if submitted != nil {
				*submitted = path
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

// TestPanelEmptyLayout: with no trusted workspace the heading
// "No trusted workspaces yet." replaces the per-entry rows.
func TestPanelEmptyLayout(t *testing.T) {
	p := testPanel(nil, nil, nil)
	plain := plainLines(p.Lines())
	want := []string{
		"Add Directory to Workspace",
		"",
		"  No trusted workspaces yet.",
		"",
		"  Path:  ",
		"",
		"Keyboard: enter Add Directory  esc Cancel",
	}
	if len(plain) != len(want) {
		t.Fatalf("line count = %d, want %d\nplain:\n%s",
			len(plain), len(want), strings.Join(plain, "\n"))
	}
	for i, w := range want {
		if plain[i] != w {
			t.Errorf("row %d:\n got: %q\nwant: %q", i, plain[i], w)
		}
	}
	// The reverse-video cursor block sits at the end of the path row.
	if !strings.HasSuffix(p.Lines()[4], searchCursorBlock) {
		t.Errorf("the path row must end in the cursor block: %q", plain[4])
	}
}

// TestPanelTrustedListed: the trusted list is drawn verbatim.
func TestPanelTrustedListed(t *testing.T) {
	p := testPanel([]string{"/Users/foo/a", "/Users/foo/b"}, nil, nil)
	plain := plainLines(p.Lines())
	if len(plain) != 9 {
		t.Fatalf("line count = %d, want 9\nplain:\n%s",
			len(plain), strings.Join(plain, "\n"))
	}
	if plain[2] != "  Trusted workspaces:" {
		t.Errorf("row 2 = %q, want the heading", plain[2])
	}
	if plain[3] != "  /Users/foo/a" {
		t.Errorf("row 3 = %q, want the first trusted path", plain[3])
	}
	if plain[4] != "  /Users/foo/b" {
		t.Errorf("row 4 = %q, want the second trusted path", plain[4])
	}
	if !strings.Contains(plain[6], "  Path:") {
		t.Errorf("row 6 = %q, want the path input", plain[6])
	}
	if plain[8] != "Keyboard: enter Add Directory  esc Cancel" {
		t.Errorf("row 8 = %q, want the keyboard hint", plain[8])
	}
}

// TestPanelTypingFillsBuffer: every printable rune appends to the
// buffer, and the path row repaints after every keystroke.
func TestPanelTypingFillsBuffer(t *testing.T) {
	p := testPanel(nil, nil, nil)
	for _, r := range []rune("/tmp/foo") {
		handled, _ := p.HandleKey(tea.Key{Text: string(r)})
		if !handled {
			t.Errorf("the key %q was not handled", string(r))
		}
	}
	if got := p.Text(); got != "/tmp/foo" {
		t.Errorf("the buffer = %q, want %q", got, "/tmp/foo")
	}
	if !strings.Contains(plainLines(p.Lines())[4], "Path: /tmp/foo") {
		t.Errorf("the path row lost the typed text: %q", plainLines(p.Lines())[4])
	}
}

// TestPanelBackspace pops the last rune, and pops nothing at all on an
// empty buffer.
func TestPanelBackspace(t *testing.T) {
	p := testPanel(nil, nil, nil)
	p.SetText("/tmp/foo")
	handled, _ := p.HandleKey(tea.Key{Code: tea.KeyBackspace})
	if !handled {
		t.Fatal("backspace not handled")
	}
	if got := p.Text(); got != "/tmp/fo" {
		t.Errorf("the buffer = %q, want %q", got, "/tmp/fo")
	}
	p.SetText("")
	p.HandleKey(tea.Key{Code: tea.KeyBackspace})
	if got := p.Text(); got != "" {
		t.Errorf("backspace on an empty buffer = %q, want empty", got)
	}
}

// TestPanelEnterSubmits: Enter hands the typed path to the host.
func TestPanelEnterSubmits(t *testing.T) {
	var submitted string
	p := testPanel(nil, &submitted, nil)
	p.SetText("/tmp/foo")
	handled, _ := p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if !handled {
		t.Fatal("Enter not handled")
	}
	if submitted != "/tmp/foo" {
		t.Errorf("submitted = %q, want %q", submitted, "/tmp/foo")
	}
}

// TestPanelEscCloses: Esc leaves the panel without submitting.
func TestPanelEscCloses(t *testing.T) {
	var submitted string
	closed := false
	p := testPanel(nil, &submitted, &closed)
	p.SetText("/tmp/foo")
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

// TestPanelModifiedKeyNotHandled: a modified key the panel does not use
// is not handled, so the host's global handlers still see it.
func TestPanelModifiedKeyNotHandled(t *testing.T) {
	p := testPanel(nil, nil, nil)
	handled, cmd := p.HandleKey(tea.Key{Code: 'a', Mod: tea.ModCtrl})
	if handled {
		t.Error("Ctrl+A must not be handled by the panel")
	}
	if cmd != nil {
		t.Errorf("cmd = %v, want nil", cmd)
	}
}
