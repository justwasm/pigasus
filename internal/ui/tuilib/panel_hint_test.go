package tuilib

import (
	"strings"
	"testing"
)

func TestPanelKeyboardHint(t *testing.T) {
	hint := PanelKeyboardHint("esc", "Close", "enter", "Submit")
	if !strings.HasPrefix(hint, "\x1b[1mKeyboard: \x1b[0m") {
		t.Errorf("expected Keyboard prefix, got %q", hint)
	}
	if !strings.Contains(hint, "esc") || !strings.Contains(hint, "Close") {
		t.Errorf("missing key/label in hint: %q", hint)
	}
}

func TestPanelDotHint(t *testing.T) {
	hint := PanelDotHint("  ", "esc", "Close", "enter", "Submit")
	if !strings.HasPrefix(hint, "  ") {
		t.Errorf("expected lead spaces, got %q", hint)
	}
	if !strings.Contains(hint, " · ") {
		t.Errorf("expected dot separator in hint: %q", hint)
	}
}

func TestSlashEchoLines(t *testing.T) {
	lines := SlashEchoLines("/rename", "Exited /rename command")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}
	if !strings.Contains(lines[0], "> /rename") {
		t.Errorf("line 0 mismatch: %q", lines[0])
	}
	if !strings.Contains(lines[1], "Exited /rename command") {
		t.Errorf("line 1 mismatch: %q", lines[1])
	}
}

// TestSlashEchoLinesBytes pins the echo block's bytes: the header, the
// notice row, the trailing blank. The colours are the palette's own
// default scheme values.
func TestSlashEchoLinesBytes(t *testing.T) {
	const (
		warningBlueOpen  = "\x1b[38;2;38;139;210;1m"
		warningBlueClose = "\x1b[0m"
		subtleOpen       = "\x1b[38;2;88;110;117m"
		subtleClose      = "\x1b[39m"
	)
	want := []string{
		warningBlueOpen + "> /rename" + warningBlueClose,
		warningBlueOpen + "  \u23bf  " + warningBlueClose + subtleOpen + "Exited /rename command" + subtleClose,
		"",
	}
	got := SlashEchoLines("/rename", "Exited /rename command")
	if len(got) != len(want) {
		t.Fatalf("SlashEchoLines = %q, want %q", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("row %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestSlashEchoLinesExtraRows pins that the extra rows sit between the
// notice row and the trailing blank — the order the reference's own
// multi-line echo block has.
func TestSlashEchoLinesExtraRows(t *testing.T) {
	lines := SlashEchoLines("/voice", "Listening", "line one", "line two")
	if len(lines) != 5 {
		t.Fatalf("expected 5 lines, got %d: %q", len(lines), lines)
	}
	if lines[2] != "line one" || lines[3] != "line two" {
		t.Errorf("the extra rows are not in order: %q", lines)
	}
	if lines[4] != "" {
		t.Errorf("the last row is not the blank one: %q", lines[4])
	}
	if !strings.Contains(lines[1], "Listening") {
		t.Errorf("row 1 mismatch: %q", lines[1])
	}
}
