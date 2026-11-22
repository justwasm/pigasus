package mdrender_test

import (
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/ui/mdrender"
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

func TestReplacePluginCards(t *testing.T) {
	// Uninstalled / raw marketplace catalog ID:
	uninstalledRaw := `Here is a suggestion: <plugin-card catalog-id="12345"></plugin-card>`
	got1 := mdrender.ReplacePluginCards(uninstalledRaw)
	want1 := "Here is a suggestion: ⚠ Open /plugin to install or sign in to this plugin."
	if got1 != want1 {
		t.Errorf("ReplacePluginCards(uninstalled) = %q, want %q", got1, want1)
	}

	tripleSlash := `Suggestion: <plugin-card catalog-id="market/pkg/plugin"></plugin-card>`
	got2 := mdrender.ReplacePluginCards(tripleSlash)
	want2 := "Suggestion: ⚠ Open /plugin to install or sign in to this plugin."
	if got2 != want2 {
		t.Errorf("ReplacePluginCards(tripleSlash) = %q, want %q", got2, want2)
	}

	// Installed plugin name:
	installed := `<plugin-card catalog-id="github"></plugin-card>`
	got3 := mdrender.ReplacePluginCards(installed)
	want3 := "⚠ The github plugin needs you to sign in. Open /plugin to sign in."
	if got3 != want3 {
		t.Errorf("ReplacePluginCards(installed) = %q, want %q", got3, want3)
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
