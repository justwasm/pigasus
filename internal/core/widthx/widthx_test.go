package widthx_test

import (
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/core/widthx"
)

func TestVisibleWidth(t *testing.T) {
	cases := []struct {
		input string
		want  int
	}{
		{"", 0},
		{"hello", 5},
		{"\x1b[31mhello\x1b[0m", 5},
		{"\x1b[1;32;48;5;123mworld\x1b[0m", 5},
		{"\x1b]8;;https://example.com\x07link\x1b]8;;\x07", 4},
		{"a\tb", 5}, // tab is 3 spaces
		{"你好世界", 8}, // 4 CJK chars = 8 cols
		{"hello 你好", 10},
		{"🎉🚀", 4}, // 2 emojis = 4 cols
	}

	for _, tc := range cases {
		got := widthx.VisibleWidth(tc.input)
		if got != tc.want {
			t.Errorf("VisibleWidth(%q) = %d, want %d", tc.input, got, tc.want)
		}
	}
}

func TestStripAnsi(t *testing.T) {
	input := "\x1b[1;31mBold Red\x1b[0m \x1b]8;;https://test.com\x07Link\x1b]8;;\x07"
	want := "Bold Red Link"
	got := widthx.StripAnsi(input)
	if got != want {
		t.Errorf("StripAnsi(%q) = %q, want %q", input, got, want)
	}
}

func TestAnsiCodeTracker(t *testing.T) {
	tr := &widthx.AnsiCodeTracker{}
	tr.Process("\x1b[1;38;5;196m") // Bold Red
	active := tr.ActiveCodes()
	if !strings.Contains(active, "1") || !strings.Contains(active, "38;5;196") {
		t.Errorf("ActiveCodes() = %q, want bold and color 196", active)
	}

	reset := tr.LineEndReset()
	if reset != "\x1b[0m" {
		t.Errorf("LineEndReset() = %q, want \\x1b[0m", reset)
	}
}

func TestWrapTextWithAnsi(t *testing.T) {
	// 1. Plain text wrapping
	text := "the quick brown fox jumps over the lazy dog"
	lines := widthx.WrapTextWithAnsi(text, 15)
	if len(lines) < 3 {
		t.Fatalf("expected at least 3 wrapped lines, got %d: %v", len(lines), lines)
	}
	for _, l := range lines {
		if widthx.VisibleWidth(l) > 15 {
			t.Errorf("line %q exceeds width 15 (width=%d)", l, widthx.VisibleWidth(l))
		}
	}

	// 2. Colored text wrapping: verify color carries over and resets at line ends
	colored := "\x1b[32m" + text + "\x1b[0m"
	colLines := widthx.WrapTextWithAnsi(colored, 15)
	if len(colLines) < 3 {
		t.Fatalf("expected at least 3 colored wrapped lines, got %d: %v", len(colLines), colLines)
	}

	for i, l := range colLines {
		if widthx.VisibleWidth(l) > 15 {
			t.Errorf("colored line %d %q exceeds width 15", i, l)
		}
		// In wrapped lines (except last), end of line should have reset
		if i < len(colLines)-1 {
			if !strings.HasSuffix(l, "\x1b[0m") {
				t.Errorf("line %d %q should end with \\x1b[0m to avoid color bleed", i, l)
			}
		}
		// Subsequent lines should re-open the green color
		if i > 0 {
			if !strings.HasPrefix(l, "\x1b[32m") {
				t.Errorf("line %d %q should start with \\x1b[32m to preserve color", i, l)
			}
		}
	}

	// 3. Long unbroken word
	longWord := "supercalifragilisticexpialidocious"
	broken := widthx.WrapTextWithAnsi(longWord, 10)
	if len(broken) < 4 {
		t.Errorf("expected long word to be broken into >=4 lines, got %d: %v", len(broken), broken)
	}
}

func TestTruncateToWidth(t *testing.T) {
	s := "Hello, this is a long sentence that must be clipped"
	res := widthx.Truncate(s, 20, "...")
	if widthx.VisibleWidth(res) > 20 {
		t.Errorf("Truncate result %q exceeds width 20 (width=%d)", res, widthx.VisibleWidth(res))
	}
	if !strings.HasSuffix(res, "...") {
		t.Errorf("expected result to end with '...', got %q", res)
	}

	// Colored text truncation
	col := "\x1b[31mRed text that goes on and on and on\x1b[0m"
	resCol := widthx.Truncate(col, 15, "…")
	if widthx.VisibleWidth(resCol) > 15 {
		t.Errorf("Truncate colored result exceeds width 15: %q", resCol)
	}
	if !strings.HasSuffix(resCol, "\x1b[0m") {
		t.Errorf("expected truncated colored string to close ANSI escape: %q", resCol)
	}
}
