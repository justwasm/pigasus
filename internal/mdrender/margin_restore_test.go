package mdrender

// margin_restore_test.go — cycle 271. The strip/restore pipeline splices
// chroma output into a rendered placeholder paragraph. Glamour applies the
// document margin (two spaces) to the paragraph's first line only, so the
// spliced body's later lines used to lose it and a code block rendered as
// "    package main" followed by "  func main() {}" instead of "    …".
// RestoreMermaidAndMath now reuses the placeholder line's visible margin as
// the prefix for every body line.

import (
	"strings"
	"testing"
)

func TestRestoreMermaidAndMath_CodeBlockCarriesMarginOnEveryLine(t *testing.T) {
	src := "before\n\n```go\npackage main\n\nfunc main() {}\n```\n\nafter\n"
	stripped, ph := StripMermaidAndMath(src)
	rendered, err := ANSI(stripped, 119)
	if err != nil {
		t.Fatalf("ANSI: %v", err)
	}
	out := stripANSI(RestoreMermaidAndMath(rendered, ph))
	for _, want := range []string{
		"\n    package main",
		"\n    func main() {}",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("restored code block missing %q; output:\n%s", want, out)
		}
	}
}

func TestVisiblePrefix(t *testing.T) {
	for _, tc := range []struct {
		in, want string
	}{
		{"  \x1b[38;5;23m", "  "},
		{"    ", "    "},
		{"  │ ", ""},
		{"  - ", ""},
		{"", ""},
	} {
		if got := visiblePrefix(tc.in); got != tc.want {
			t.Errorf("visiblePrefix(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
