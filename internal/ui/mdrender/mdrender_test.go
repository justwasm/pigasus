package mdrender

import (
	"bytes"
	"strings"
	"testing"
)

// renderCalls exercises Render on src and returns the produced text.
func renderCalls(t *testing.T, src string) string {
	t.Helper()
	got, err := Render(src)
	if err != nil {
		t.Fatalf("Render(%q) error: %v", src, err)
	}
	return got
}

// assertContains fails if needle is not a substring of got.
func assertContains(t *testing.T, got, needle string) {
	t.Helper()
	if !strings.Contains(got, needle) {
		t.Errorf("Render output missing %q\nfull output:\n%s", needle, got)
	}
}

// assertNotContains fails if needle is a substring of got.
func assertNotContains(t *testing.T, got, needle string) {
	t.Helper()
	if strings.Contains(got, needle) {
		t.Errorf("Render output unexpectedly contains %q\nfull output:\n%s", needle, got)
	}
}

// TestRender_Empty confirms the empty-input short-circuit.
func TestRender_Empty(t *testing.T) {
	got, err := Render("")
	if err != nil {
		t.Fatalf("Render empty: %v", err)
	}
	if got != "" {
		t.Errorf("Render empty = %q, want \"\"", got)
	}
}

// TestRender_Headings verifies "#" through "######" prefixes are emitted.
func TestRender_Headings(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []string // substrings that must appear
	}{
		{"h1", "# Title", []string{"# Title"}},
		{"h2", "## Subtitle", []string{"## Subtitle"}},
		{"h3", "### Section", []string{"### Section"}},
		{"h6", "###### Tiny", []string{"###### Tiny"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := renderCalls(t, tt.src)
			for _, w := range tt.want {
				assertContains(t, got, w)
			}
		})
	}
}

// TestRender_Paragraphs confirms paragraph breaks are preserved.
func TestRender_Paragraphs(t *testing.T) {
	src := "First paragraph.\n\nSecond paragraph."
	got := renderCalls(t, src)
	assertContains(t, got, "First paragraph.")
	assertContains(t, got, "Second paragraph.")
	// Blank line between paragraphs.
	if !strings.Contains(got, "First paragraph.\n\nSecond paragraph.") {
		t.Errorf("paragraph break lost:\n%s", got)
	}
}

// TestRender_Emphasis checks *emph* and **strong** markers.
func TestRender_Emphasis(t *testing.T) {
	src := "This is *italic* and **bold** text."
	got := renderCalls(t, src)
	assertContains(t, got, "_italic_")
	assertContains(t, got, "*bold*")
}

// TestRender_Links checks inline link rendering.
func TestRender_Links(t *testing.T) {
	src := "Visit [the docs](https://example.com) for more."
	got := renderCalls(t, src)
	assertContains(t, got, "the docs")
	assertContains(t, got, "https://example.com")
}

// TestRender_FileLinkPercentEncoded confirms the cycle file-link-percent-encoded-anchor
// integration: a markdown link whose destination contains a raw `#L<n>`
// fragment is rendered with the fragment percent-encoded as `%23L<n>`.
// The path component is left verbatim.
func TestRender_FileLinkPercentEncoded(t *testing.T) {
	src := "See [src/foo.go:12](src/foo.go#L12) for details."
	got := renderCalls(t, src)
	assertContains(t, got, "src/foo.go%23L12")
	assertNotContains(t, got, "src/foo.go#L12")
	// The label is unchanged.
	assertContains(t, got, "src/foo.go:12")

	// Already-encoded form is idempotent (a defensive guarantee that
	// calling NormalizeFileLink on an already-normalised URL is safe).
	got2 := renderCalls(t, "Same: [a.go](a.go%23L9).")
	assertContains(t, got2, "a.go%23L9")
	assertNotContains(t, got2, "a.go#L9")
}

// TestRender_FileLinkHoverMarkerSurvives confirms the cycle
// separate-links-hover integration: the `?h=<index>` hover-card
// disambiguator added by render.FormatFileLinkAt /
// render.MarkdownFileLinkAt survives the markdown renderer unchanged.
// The renderer's *ast.Link handler only normalises `#L<n>` fragments
// via render.NormalizeFileLink — query strings are not touched, so
// the marker rides through to the terminal. This is what makes the
// "distinct hover cards per link instance" property work end to end
// (AGY 1.3.3 changelog line 60).
func TestRender_FileLinkHoverMarkerSurvives(t *testing.T) {
	// Two identical-looking links at index 0 and 1 — the URLs
	// differ only in the marker suffix, and both must come out the
	// other side intact.
	src := "Same file twice: " +
		"[src/foo.go:12](src/foo.go%23L12?h=0) and " +
		"[src/foo.go:12](src/foo.go%23L12?h=1)."
	got := renderCalls(t, src)

	// Both markers survive the renderer.
	assertContains(t, got, "?h=0")
	assertContains(t, got, "?h=1")

	// The percent-encoded anchor is still there (line-59 guarantee
	// is not regressed).
	assertContains(t, got, "src/foo.go%23L12")

	// No raw `#L<n>` slipped back in.
	assertNotContains(t, got, "src/foo.go#L12")
	// No double-encoding: the `?` survives, not `%3F`.
	assertNotContains(t, got, "%3Fh=")
}

// TestRender_FileLinkHoverMarkerANSI confirms the marker also
// survives RenderANSI (the colour-mode renderer that agent
// responses and the transcript viewer use). The renderer's
// *ast.Link handler is the same code path in both modes, so this is
// mostly a regression guard against a future refactor that splits
// the two paths.
func TestRender_FileLinkHoverMarkerANSI(t *testing.T) {
	var buf bytes.Buffer
	src := "Click [a.go:1](a.go%23L1?h=0)."
	if err := RenderANSI(src, &buf); err != nil {
		t.Fatalf("RenderANSI: %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, "a.go%23L1?h=0") {
		t.Errorf("marker lost in ANSI render:\n%s", got)
	}
	if !strings.Contains(got, "\033[3m") {
		t.Errorf("link not italicised:\n%s", got)
	}
}

// TestRender_AutoLink checks bare URLs are emitted with angle brackets.
func TestRender_AutoLink(t *testing.T) {
	src := "See <https://example.org> for details."
	got := renderCalls(t, src)
	assertContains(t, got, "<https://example.org>")
}

// TestRender_CodeSpan checks inline backtick code.
func TestRender_CodeSpan(t *testing.T) {
	src := "Run `go test ./...` to verify."
	got := renderCalls(t, src)
	assertContains(t, got, "`go test ./...`")
}

// TestRender_FencedCodeBlock checks ``` code blocks.
func TestRender_FencedCodeBlock(t *testing.T) {
	src := "```go\nfunc main() {}\n```"
	got := renderCalls(t, src)
	assertContains(t, got, "    func main() {}")
}

// TestRender_IndentedCodeBlock checks 4-space-indented code blocks.
func TestRender_IndentedCodeBlock(t *testing.T) {
	src := "    line one\n    line two"
	got := renderCalls(t, src)
	assertContains(t, got, "    line one")
	assertContains(t, got, "    line two")
}

// TestRender_UnorderedList checks unordered-list bullet markers. Cycle
// 175: the walker now emits U+2022 BULLET instead of `- ` for
// unordered items, matching the cycle-155 reference render.
func TestRender_UnorderedList(t *testing.T) {
	src := "- one\n- two\n- three"
	got := renderCalls(t, src)
	assertContains(t, got, "\u2421 one")
	assertContains(t, got, "\u2421 two")
	assertContains(t, got, "\u2421 three")
}

// TestRender_OrderedList checks "1." ordered-list markers.
func TestRender_OrderedList(t *testing.T) {
	src := "1. first\n2. second\n3. third"
	got := renderCalls(t, src)
	assertContains(t, got, "1. first")
	assertContains(t, got, "1. second")
	// We don't track per-item index, so all items share "1.".
	assertContains(t, got, "1. third")
}

// TestRender_NestedList checks list indentation reflects nesting.
// Cycle 175: bullet is now U+2022 BULLET (matching cycle-155 reference)
// rather than the raw `- ` marker.
func TestRender_NestedList(t *testing.T) {
	src := "- top\n  - nested\n- bottom"
	got := renderCalls(t, src)
	// Two-space indent before the nested bullet.
	if !strings.Contains(got, "  \u2421 nested") {
		t.Errorf("nested list indent lost:\n%s", got)
	}
}

// TestRender_Blockquote confirms blockquote markers.
func TestRender_Blockquote(t *testing.T) {
	src := "> A wise person\n> once said"
	got := renderCalls(t, src)
	assertContains(t, got, "> A wise person")
}

// TestRender_ThematicBreak confirms "---" rendering.
func TestRender_ThematicBreak(t *testing.T) {
	got := renderCalls(t, "above\n\n---\n\nbelow")
	assertContains(t, got, "above")
	assertContains(t, got, "---")
	assertContains(t, got, "below")
}

// TestRender_HTMLDropped confirms raw HTML is stripped.
func TestRender_HTMLDropped(t *testing.T) {
	got := renderCalls(t, "hello <em>world</em>!")
	assertNotContains(t, got, "<em>")
	assertNotContains(t, got, "</em>")
	assertContains(t, got, "hello")
	assertContains(t, got, "world")
}

// TestRender_TrailingNewline confirms the output always ends with \n.
func TestRender_TrailingNewline(t *testing.T) {
	tests := []string{
		"plain text",
		"# heading",
		"- item",
		"para one\n\npara two",
	}
	for _, src := range tests {
		got, err := Render(src)
		if err != nil {
			t.Fatalf("Render(%q): %v", src, err)
		}
		if !strings.HasSuffix(got, "\n") {
			t.Errorf("Render(%q) missing trailing newline: %q", src, got)
		}
		if strings.HasSuffix(got, "\n\n") {
			t.Errorf("Render(%q) has double trailing newline: %q", src, got)
		}
	}
}

// TestRender_TopLevelIntegration exercises a realistic tool-output blob.
func TestRender_TopLevelIntegration(t *testing.T) {
	src := `## Result

The run produced **3** warnings:

- ` + "`go vet ./...`" + ` flagged unused import
- Cyclomatic complexity above threshold
- Missing license header

> Investigate before merging.

See <https://example.com/lint> for the full report.

` + "```" + `
$ go vet ./...
./main.go:1:1: warning
` + "```" + `
`
	got, err := Render(src)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	// Spot-check each major element is present.
	for _, want := range []string{
		"## Result",
		"3",
		"`go vet ./...`",
		// Cycle 175: unordered-list walker now emits U+2022
		// BULLET instead of `- ` (matches cycle-155 reference).
		"\u2421 Cyclomatic complexity",
		"> Investigate",
		"<https://example.com/lint>",
		"    $ go vet ./...",
	} {
		assertContains(t, got, want)
	}
}

// TestRenderANSI_EmitsEscapes verifies RenderANSI writes SGR sequences.
func TestRenderANSI_EmitsEscapes(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderANSI("# Heading", &buf); err != nil {
		t.Fatalf("RenderANSI: %v", err)
	}
	got := buf.String()
	// \033[1m opens bold, \033[22m closes.
	if !strings.Contains(got, "\033[1m") {
		t.Errorf("ANSI output missing bold-open:\n%s", got)
	}
	if !strings.Contains(got, "\033[22m") {
		t.Errorf("ANSI output missing bold-close:\n%s", got)
	}
	// The heading text should still be present.
	assertContains(t, got, "Heading")
}

// TestRenderANSI_EmphasisItalic verifies italic escapes for *emphasis*.
func TestRenderANSI_EmphasisItalic(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderANSI("this is *italic* text", &buf); err != nil {
		t.Fatalf("RenderANSI: %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, "\033[3m") {
		t.Errorf("ANSI output missing italic-open:\n%s", got)
	}
	if !strings.Contains(got, "\033[23m") {
		t.Errorf("ANSI output missing italic-close:\n%s", got)
	}
}

// TestRenderANSI_LinkItalic verifies link labels get italic escapes.
func TestRenderANSI_LinkItalic(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderANSI("[text](https://example.com)", &buf); err != nil {
		t.Fatalf("RenderANSI: %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, "https://example.com") {
		t.Errorf("link URL missing:\n%s", got)
	}
	if !strings.Contains(got, "\033[3m") {
		t.Errorf("link not italicised:\n%s", got)
	}
}

// TestRenderANSI_Empty confirms the empty-input short-circuit.
func TestRenderANSI_Empty(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderANSI("", &buf); err != nil {
		t.Fatalf("RenderANSI empty: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("RenderANSI empty wrote %q, want no bytes", buf.String())
	}
}

// TestRender_NoANSI verifies plain Render never emits ANSI escapes.
func TestRender_NoANSI(t *testing.T) {
	src := "# Heading\n\n*emph* and **strong** [link](https://e.com)"
	got := renderCalls(t, src)
	if strings.Contains(got, "\033[") {
		t.Errorf("Render emitted ANSI escape in plain mode:\n%s", got)
	}
}
