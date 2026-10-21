// Tests for renderUserText (cycle 174). Cycle 194 removed the
// cycle-167 /learn and /goal byte-exact echo tests because those
// commands are now prompt-sending shortcuts that do not echo user-
// typed text; see the comment block at the bottom of this file and
// slash_cycle194_test.go for the replacement test suite.
//
// The tests in this file still pin two properties:
//   - markdown markers in user-typed text are rendered as ANSI
//     escapes (the TUI transcript's terminal turns them on),
//   - plain text round-trips unchanged (so a future caller that
//     does echo user markdown produces byte-stable output and does
//     not break any downstream snapshot tests).
package main

import (
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/mdrender"
)

// TestRenderUserText_Empty returns the empty string unchanged.
func TestRenderUserText_Empty(t *testing.T) {
	if got := renderUserText("", 0); got != "" {
		t.Errorf("renderUserText(\"\") = %q, want \"\"", got)
	}
}

// TestRenderUserText_PlainText passes through unchanged: goldmark is a
// no-op for text containing none of the markdown markers, so the
// cycle-167 echo strings ("  Captured learning: <plain text>") byte-
// match what they produced before cycle 174.
func TestRenderUserText_PlainText(t *testing.T) {
	cases := []string{
		"the API endpoint is down",
		"check foo.bar.com:8080 next",
		"remember: 42 is the answer",
	}
	for _, s := range cases {
		t.Run(s, func(t *testing.T) {
			if got := renderUserText(s, 0); got != s {
				t.Errorf("renderUserText(%q) = %q, want unchanged", s, got)
			}
		})
	}
}

// TestRenderUserText_Bold emits the bold ANSI on/off escapes for `**`
// runs. The exact escape codes are defined by internal/mdrender (cycle
// 168, refined in 174); we assert against the public mdrender.Render
// output rather than hard-coding the bytes.
//
// Cycle glamour-migration: the closing escape is \x1b[m (full
// reset) rather than the cycle-175 walker's \x1b[22m. Pin the
// open escape and the emphasised text; the close is whatever
// glamour chose.
func TestRenderUserText_Bold(t *testing.T) {
	src := "**Important**: api down"
	got := renderUserText(src, 0)
	// Reference: render the same source via mdrender.Render (no ANSI)
	// and confirm RenderANSI inserts the bold open/close pair around
	// the emphasised span.
	plain, err := mdrender.Render(src)
	if err != nil {
		t.Fatalf("mdrender.Render: %v", err)
	}
	if !strings.Contains(plain, "Important") {
		t.Fatalf("plain render lost the emphasised word: %q", plain)
	}
	// ANSI form must wrap the bold span with the open escape.
	// Cycle-175 walker used ESC[1m; glamour uses the same.
	if !strings.Contains(got, "\x1b[1m") {
		t.Errorf("expected bold open ANSI, got %q", got)
	}
	// The plain text inside the bold span should still contain
	// "Important".
	if !strings.Contains(got, "Important") {
		t.Errorf("expected 'Important' in rendered output: %q", got)
	}
}

// TestRenderUserText_Italic emits the italic ANSI escapes for `*` runs.
//
// Cycle glamour-migration: glamour closes with \x1b[m (full
// reset), not the cycle-175 walker's \x1b[23m (italic-only
// close). Pin the open escape and the emphasised text.
func TestRenderUserText_Italic(t *testing.T) {
	got := renderUserText("api is *down* right now", 0)
	if !strings.Contains(got, "\x1b[3m") {
		t.Errorf("expected italic open ANSI, got %q", got)
	}
	if !strings.Contains(got, "down") {
		t.Errorf("expected 'down' in rendered output: %q", got)
	}
}

// TestRenderUserText_Heading renders a "# Foo" line; the walker emits
// "# Foo" with a trailing blank line for the heading break.
func TestRenderUserText_Heading(t *testing.T) {
	got := renderUserText("# Important", 0)
	// The walker keeps the "# " prefix in plain text mode; we just
	// assert the heading text is present (the package guarantees that)
	// and that the output does not contain the raw "<h1>" HTML that
	// goldmark would have emitted if we forgot to use the AST walker.
	if !strings.Contains(got, "Important") {
		t.Errorf("rendered heading missing %q: %q", "Important", got)
	}
	if strings.Contains(got, "<h1>") {
		t.Errorf("HTML tag leaked into terminal output: %q", got)
	}
}

// TestRenderUserText_NoLeadingNewline confirms the cycle 174 fix to
// the walker: previously every output started with a stray "\n"
// because firstPara defaulted to false. After the fix, plain text
// round-trips with no leading newline.
func TestRenderUserText_NoLeadingNewline(t *testing.T) {
	got := renderUserText("plain text", 0)
	if strings.HasPrefix(got, "\n") {
		t.Errorf("renderUserText has a leading newline: %q", got)
	}
}

// TestRenderUserText_NoTrailingNewline confirms the helper strips the
// single trailing newline mdrender.RenderANSI appends; the TUI
// transcript is a []string, and a phantom blank row would shift every
// subsequent row down by one.
func TestRenderUserText_NoTrailingNewline(t *testing.T) {
	got := renderUserText("plain text", 0)
	if strings.HasSuffix(got, "\n") {
		t.Errorf("renderUserText left a trailing newline: %q", got)
	}
}

// TestRunLearn_RendersMarkdown and friends (cycle-167 byte-exact
// tests for /learn + /goal echo strings) were removed in cycle 194.
// The cycle-194 handlers for /learn, /goal, /plan and /grill-me are
// prompt-sending shortcuts that echo the slash token and start a
// model stream, so the cycle-167 fixed-string echoes ("  Captured
// learning: …", "  Goal mode: …", "  Goal cleared.") no longer apply.
// Replacement tests live in slash_cycle194_test.go:
//
//   - TestRunGoal_SendsPrompt
//   - TestRunGoal_NoArgsAlsoSendsPrompt
//   - TestRunPlan_SendsPrompt
//   - TestRunLearn_SendsPrompt
//   - TestRunGrillMe_SendsPrompt
//
// renderUserText itself is unchanged — the helper is still used by
// any future command that echoes user-typed markdown into the
// transcript.
