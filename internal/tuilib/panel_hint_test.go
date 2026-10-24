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
