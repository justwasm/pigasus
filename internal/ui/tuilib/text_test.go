package tuilib

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestTextUtilities(t *testing.T) {
	if got := ViewRows("hello\nworld"); got != 2 {
		t.Errorf("ViewRows = %d, want 2", got)
	}
	if got := ViewRows("hello\nworld\n"); got != 2 {
		t.Errorf("ViewRows = %d, want 2", got)
	}
	if got := ClampInt(10, 0, 5); got != 5 {
		t.Errorf("ClampInt = %d, want 5", got)
	}
	if got := ClampInt(-1, 0, 5); got != 0 {
		t.Errorf("ClampInt = %d, want 0", got)
	}
	if got := ClampInt(0, 0, -5); got != 0 {
		t.Errorf("ClampInt with inverted bounds = %d, want 0", got)
	}
	wrapped := WrapRows("abc def", 3)
	if len(wrapped) != 3 {
		t.Errorf("WrapRows length = %d, want 3", len(wrapped))
	}
}

func TestASCIIANSIWidthAndPadFit(t *testing.T) {
	cases := []struct {
		in     string
		wantW  int
		wantOK bool
	}{
		{"", 0, true},
		{"hello world", 11, true},
		{"\x1b[38;2;38;139;210mhello\x1b[0m", 5, true},
		{"\x1b[1m\x1b[32mok\x1b[39m\x1b[0m!", 3, true},
		{"中文", 0, false},
		{"\x1b]8;;https://example.com\x1b\\link\x1b]8;;\x1b\\", 0, false},
		{"tab\there", 0, false},
		{"\x1b[\nhello", 0, false},
		{"\x1b[\x1b[31mhi\x1b[m", 0, false},
	}
	for _, tc := range cases {
		w, ok := asciiANSIWidth(tc.in)
		if ok != tc.wantOK || (ok && w != tc.wantW) {
			t.Errorf("asciiANSIWidth(%q) = (%d, %v), want (%d, %v)", tc.in, w, ok, tc.wantW, tc.wantOK)
		}
		if got, want := StringWidth(tc.in), ansi.StringWidth(tc.in); got != want {
			t.Errorf("StringWidth(%q) = %d, want %d", tc.in, got, want)
		}
	}

	if got := PadTo("\x1b[31mhi\x1b[0m", 5); got != "\x1b[31mhi\x1b[0m   " {
		t.Errorf("PadTo CSI = %q", got)
	}
	if got := PadTo("\x1b[31mhello\x1b[0m", 5); got != "\x1b[31mhello\x1b[0m" {
		t.Errorf("PadTo exact CSI = %q", got)
	}
	if got := FitWidth("\x1b[31mhello\x1b[0m", 5); got != "\x1b[31mhello\x1b[0m" {
		t.Errorf("FitWidth exact CSI = %q", got)
	}
	if got := PadTo("中文", 6); got != "中文  " {
		t.Errorf("PadTo CJK = %q", got)
	}
}
