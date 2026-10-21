// unclosed_fence_test.go — cycle 272 regression coverage for chroma on an
// unterminated fenced code block.
//
// The reference renderer chromas an unclosed fence. Verified by resuming AGY
// 1.4.2 on a stored reply whose final block is an open ```go fence: every
// token comes back solarized (package 166, main 32, println 166, "hi" 36).
// The clone's strip pass matched fences with fencedRe, which needs a closing
// fence, so a partial buffer fell through to glamour's monochrome 4-space
// code path. Since the live streaming render is a render of the accumulated
// (possibly unterminated) buffer, that gap was visible for the whole time a
// code block was streaming. ensureFenceClosed closes the final open fence
// before the strip scan, so the partial body chromas exactly like the
// completed block will once the real closer arrives.
package mdrender

import (
	"strings"
	"testing"
)

func TestEnsureFenceClosed_AppendsCloserOnlyWhenOpen(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"open with body", "```go\npackage main\n", "```go\npackage main\n```"},
		{"open without trailing newline", "```go\npackage main", "```go\npackage main\n```"},
		{"open bare", "```go", "```go\n```"},
		{"closed unchanged", "```go\nx\n```\n", "```go\nx\n```\n"},
		{"two closed unchanged", "```go\na\n```\n\n```py\nb\n```\n", "```go\na\n```\n\n```py\nb\n```\n"},
		{"second of two open", "```go\na\n```\n\n```py\nb\n", "```go\na\n```\n\n```py\nb\n```"},
		{"no fences unchanged", "just text\n", "just text\n"},
		{"mid-line backticks ignored", "text ``` not a fence\n", "text ``` not a fence\n"},
	}
	for _, tc := range cases {
		if got := ensureFenceClosed(tc.in); got != tc.want {
			t.Errorf("%s: ensureFenceClosed(%q) = %q, want %q", tc.name, tc.in, got, tc.want)
		}
	}
}

func TestStripMermaidAndMath_UnclosedFenceIsBlockCode(t *testing.T) {
	src := "```go\npackage main\n\nfunc main() {\n\tprintln(\"hi\")\n"
	cleaned, ph := StripMermaidAndMath(src)
	if len(ph) != 1 {
		t.Fatalf("unclosed fence produced %d placeholders, want 1; cleaned:\n%s", len(ph), cleaned)
	}
	for _, p := range ph {
		if p.Kind != blockCode {
			t.Fatalf("placeholder kind = %v, want blockCode", p.Kind)
		}
		if p.Language != "go" {
			t.Fatalf("placeholder language = %q, want %q", p.Language, "go")
		}
		if !strings.Contains(p.Source, "package main") {
			t.Fatalf("placeholder source lost body: %q", p.Source)
		}
	}
}

func TestANSI_UnclosedFenceChromasLikeClosed(t *testing.T) {
	// Mirror the renderMarkdown / LineANSI pipeline: strip → glamour →
	// restore. (ANSI alone does not strip; the callers do.)
	render := func(src string) string {
		t.Helper()
		cleaned, ph := StripMermaidAndMath(src)
		out, err := ANSI(cleaned, 80)
		if err != nil {
			t.Fatalf("ANSI(%q): %v", src, err)
		}
		return RestoreMermaidAndMath(out, ph)
	}
	// 38;5;166 is chroma solarized-light's keyword colour ("package").
	open := render("```go\npackage main\n")
	if !strings.Contains(open, "38;5;166") {
		t.Fatalf("unclosed fence is not chroma-styled; output:\n%q", open)
	}
	closed := render("```go\npackage main\n```\n")
	if !strings.Contains(closed, "38;5;166") {
		t.Fatalf("closed fence lost its chroma; output:\n%q", closed)
	}
}

// TestANSI_OpenFenceOpenerOnlyNoPanic pins the degenerate live-stream frame:
// the model has emitted just the opening fence and nothing else. The
// synthetic closer must not panic and must not leak a stray ``` line into the
// rendered output.
func TestANSI_OpenFenceOpenerOnlyNoPanic(t *testing.T) {
	for _, src := range []string{"```go", "```go\n", "```", "```\n"} {
		cleaned, ph := StripMermaidAndMath(src)
		out, err := ANSI(cleaned, 80)
		if err != nil {
			t.Fatalf("ANSI(%q): %v", src, err)
		}
		out = RestoreMermaidAndMath(out, ph)
		if strings.Contains(stripAllANSI(out), "```") {
			t.Fatalf("ANSI(%q) leaked a fence marker into the render:\n%q", src, out)
		}
	}
}

// stripAllANSI removes SGR sequences only (enough for the fence-marker check).
func stripAllANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && s[j] != 'm' {
				j++
			}
			if j < len(s) {
				i = j + 1
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}
