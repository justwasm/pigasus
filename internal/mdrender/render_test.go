package mdrender_test

import (
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/mdrender"
	"github.com/justwasm/pigasus/palette"
)

func TestIndent(t *testing.T) {
	if got := mdrender.Indent("", "  "); got != "" {
		t.Errorf("Indent(\"\") = %q, want empty", got)
	}
	in := "one\ntwo\n\nthree"
	want := "  one\n  two\n\n  three"
	if got := mdrender.Indent(in, "  "); got != want {
		t.Errorf("Indent() = %q, want %q", got, want)
	}
}

func TestRenderMarkdown_Empty(t *testing.T) {
	if got := mdrender.RenderMarkdown("", 80); got != "" {
		t.Errorf("RenderMarkdown(\"\") = %q, want empty", got)
	}
}

func TestRenderMarkdown_Bullets(t *testing.T) {
	got := mdrender.RenderMarkdown("- item 1\n- item 2", 80)
	if !strings.Contains(got, "item 1") || !strings.Contains(got, "item 2") {
		t.Errorf("RenderMarkdown() missing bullet items: %q", got)
	}
}

func TestStyleInlineBackticks(t *testing.T) {
	line := "use `go test` here"
	got := mdrender.StyleInlineBackticks(line)
	if !strings.Contains(got, palette.InlineCodeOpen+"go test"+palette.InlineCodeClose) {
		t.Errorf("StyleInlineBackticks(%q) = %q, missing chip escapes", line, got)
	}

	// Unbalanced passthrough
	unbal := "use `go test here"
	if gotUnbal := mdrender.StyleInlineBackticks(unbal); gotUnbal != unbal {
		t.Errorf("unbalanced backticks changed: %q", gotUnbal)
	}
}

func TestLineIsBlankANSI(t *testing.T) {
	if !mdrender.LineIsBlankANSI("   \t  ") {
		t.Error("expected whitespace to be blank")
	}
	if !mdrender.LineIsBlankANSI("\x1b[31m   \x1b[0m") {
		t.Error("expected ANSI whitespace to be blank")
	}
	if mdrender.LineIsBlankANSI("\x1b[31mx\x1b[0m") {
		t.Error("expected text to not be blank")
	}
}

func TestRenderMarkdown_ColorSchemeDark(t *testing.T) {
	orig := mdrender.CurrentColorScheme()
	defer mdrender.SetColorScheme(orig)

	mdrender.SetColorScheme("dark")
	out := mdrender.RenderMarkdown("Hello world", 80)
	// #e8eaed in 24-bit truecolour escape is \x1b[38;2;232;234;237m
	wantFg := "\x1b[38;2;232;234;237m"
	if !strings.Contains(out, wantFg) {
		t.Errorf("expected dark scheme output to contain %q, got: %q", wantFg, out)
	}
}
