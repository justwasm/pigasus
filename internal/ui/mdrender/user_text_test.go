package mdrender

import (
	"strings"
	"testing"
)

// TestRenderUserText_Empty returns the empty string unchanged.
func TestRenderUserText_Empty(t *testing.T) {
	if got := UserText("", 0); got != "" {
		t.Errorf("UserText(\"\") = %q, want \"\"", got)
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
			if got := UserText(s, 0); got != s {
				t.Errorf("UserText(%q) = %q, want unchanged", s, got)
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
	got := UserText(src, 0)
	plain, err := Render(src)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(plain, "Important") {
		t.Fatalf("plain render lost the emphasised word: %q", plain)
	}
	if !strings.Contains(got, "\x1b[1m") {
		t.Errorf("expected bold open ANSI, got %q", got)
	}
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
	got := UserText("api is *down* right now", 0)
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
	got := UserText("# Important", 0)
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
	got := UserText("plain text", 0)
	if strings.HasPrefix(got, "\n") {
		t.Errorf("UserText has a leading newline: %q", got)
	}
}

// TestRenderUserText_NoTrailingNewline confirms the helper strips the
// single trailing newline mdrender.RenderANSI appends; the TUI
// transcript is a []string, and a phantom blank row would shift every
// subsequent row down by one.
func TestRenderUserText_NoTrailingNewline(t *testing.T) {
	got := UserText("plain text", 0)
	if strings.HasSuffix(got, "\n") {
		t.Errorf("UserText left a trailing newline: %q", got)
	}
}
