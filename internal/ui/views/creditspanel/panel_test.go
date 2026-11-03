// panel_test.go — the /credits panel's own half.
//
// Cycle 381 moved the panel out of internal/cliapp: the two actions,
// the cursor, the frame and the keys are the package's, and the host
// keeps only the seam (the way out of the panel). The frame already
// matched the reference byte for byte (cycle 207), so the test moves
// with the code it exercises.
//
// The tests build the panel directly — there is no runMode behind it —
// so a seam is a closure over a test value.

package creditspanel

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	termcolor "github.com/justwasm/pigasus/internal/ui/render/color"
	"github.com/justwasm/pigasus/palette"
)

// ---------------------------------------------------------------- helpers

// stripSGR removes every ANSI SGR sequence.
func stripSGR(s string) string {
	return termcolor.StripSGR(s)
}

// plainLines strips the SGR spans off a rendered row slice.
func plainLines(rows []string) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, stripSGR(r))
	}
	return out
}

// assertLines compares a rendered row slice with the expected rows.
func assertLines(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("line count: got %d, want %d\n got: %q\nwant: %q",
			len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d:\n got: %q\nwant: %q", i, got[i], want[i])
		}
	}
}

// newTestPanel builds a /credits panel whose way out is recorded.
func newTestPanel() (*Panel, *bool) {
	closed := false
	p := New(Seams{Close: func() tea.Cmd { closed = true; return nil }})
	return p, &closed
}

// ---------------------------------------------------------------- tests

// TestCreditsLayout pins the frame: the balance block and the two
// actions.
func TestCreditsLayout(t *testing.T) {
	p, _ := newTestPanel()
	assertLines(t, plainLines(p.Lines()), []string{
		"Pigasus CLI  credits",
		"Model AI Credits",
		"",
		"Remaining AI Credits: AI Credits not enabled (enable in /settings)",
		"",
		"Actions",
		"",
		"> Get More AI Credits",
		"  See Activity",
		"",
		"Keyboard: ↑/↓ Navigate  enter Select / Toggle  esc Close",
	})
	p.SetCursor(1)
	got := plainLines(p.Lines())
	if got[7] != "  Get More AI Credits" || got[8] != "> See Activity" {
		t.Errorf("cursor 1 rows = %q / %q", got[7], got[8])
	}
}

// TestCreditsLayoutStyled pins the exact bytes of the frame: the
// reference's dim grey is never closed on the label rows, and the
// selected action carries the blue spans.
func TestCreditsLayoutStyled(t *testing.T) {
	p, _ := newTestPanel()
	assertLines(t, p.Lines(), []string{
		palette.BlueOpen + "Pigasus CLI" + palette.BlueClose + "  " +
			"\x1b[1m" + palette.SubtleOpen + "credits" + "\x1b[0m",
		"\x1b[1m" + palette.SubtleOpen + "Model AI Credits" + "\x1b[0m",
		"",
		"\x1b[1mRemaining AI Credits\x1b[0m: " +
			"\x1b[1m" + palette.BlueOpen + "AI Credits not enabled (enable in /settings)" + "\x1b[0m",
		"",
		"\x1b[1m" + palette.SubtleOpen + "Actions" + "\x1b[0m",
		"",
		palette.BlueOpen + "> Get More AI Credits" + palette.BlueClose,
		"  See Activity",
		"",
		"\x1b[1mKeyboard: \x1b[0m" + palette.BlueOpen + "↑/↓" + palette.BlueClose + " " +
			palette.DimGrayOpen + "Navigate" + palette.DimGrayClose +
			"  " + palette.BlueOpen + "enter" + palette.BlueClose + " " +
			palette.DimGrayOpen + "Select / Toggle" + palette.DimGrayClose +
			"  " + palette.BlueOpen + "esc" + palette.BlueClose + " " +
			palette.DimGrayOpen + "Close" + palette.DimGrayClose,
	})
}

// TestCreditsNavigation pins the panel's own cursor: the navigation
// clamps at both ends and the Esc leaves the panel.
func TestCreditsNavigation(t *testing.T) {
	p, closed := newTestPanel()
	up := tea.Key{Code: tea.KeyUp}
	if handled, _ := p.HandleKey(up); !handled || p.Cursor() != 0 {
		t.Errorf("Up at 0 should clamp: handled=%v cursor=%d", handled, p.Cursor())
	}
	for i := 0; i < 10; i++ {
		p.HandleKey(tea.Key{Code: tea.KeyDown})
	}
	if p.Cursor() != 1 {
		t.Errorf("Down should clamp at the last action: cursor=%d want 1", p.Cursor())
	}
	// Enter is inert — the clone has no credits endpoint to act on.
	if handled, _ := p.HandleKey(tea.Key{Code: tea.KeyEnter}); !handled {
		t.Error("enter should be swallowed")
	}
	if *closed {
		t.Error("enter must not close the panel")
	}
	// Esc leaves.
	if handled, _ := p.HandleKey(tea.Key{Code: tea.KeyEsc}); !handled {
		t.Fatal("esc not handled")
	}
	if !*closed {
		t.Error("esc did not close the panel")
	}
}

// TestCreditsCursorClamped pins the accessor: a stale index cannot
// strand the cursor on an action that is not on screen.
func TestCreditsCursorClamped(t *testing.T) {
	p, _ := newTestPanel()
	p.SetCursor(9)
	if p.Cursor() != 1 {
		t.Errorf("cursor = %d, want 1 (clamped)", p.Cursor())
	}
	p.SetCursor(-3)
	if p.Cursor() != 0 {
		t.Errorf("cursor = %d, want 0 (clamped)", p.Cursor())
	}
}

// TestCreditsRowCount pins the row count the host's navigation uses.
func TestCreditsRowCount(t *testing.T) {
	p, _ := newTestPanel()
	if p.RowCount() != 2 {
		t.Errorf("row count = %d, want 2", p.RowCount())
	}
	if len(Actions) != 2 || !strings.Contains(Actions[0], "Get More") {
		t.Errorf("actions = %q", Actions)
	}
}
