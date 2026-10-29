// Tests for the URL block: the chunking, the hyperlink frame and the
// plain-text fallback.
//
// The rows carry an OSC 8 frame, and the transcript's selection
// (ansi.Strip / ansi.Cut) has to survive one — the last test here is
// the guard for that.
package urlchunk

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/justwasm/pigasus/render"
)

// TestChunks_SplitsAtTheBudget pins the chunking: the chunks are the
// URL, in order, and none of them is wider than the budget.
func TestChunks_SplitsAtTheBudget(t *testing.T) {
	const url = "https://example.com/a?b=c&d=e"
	chunks := Chunks(url, 10)
	var joined string
	for _, c := range chunks {
		if len([]rune(c)) > 10 {
			t.Errorf("a chunk is %d characters wide: %q", len([]rune(c)), c)
		}
		joined += c
	}
	if joined != url {
		t.Errorf("the chunks joined are %q, want %q", joined, url)
	}
	if len(chunks) < 2 {
		t.Errorf("the URL was not split: %d chunk(s)", len(chunks))
	}
}

// TestChunks_Edges pins the edges: an empty URL is one empty chunk and
// a budget below one still makes progress.
func TestChunks_Edges(t *testing.T) {
	chunks := Chunks("", 10)
	if len(chunks) != 1 || chunks[0] != "" {
		t.Errorf("Chunks(\"\", 10) = %q, want one empty chunk", chunks)
	}
	chunks = Chunks("abc", 0)
	if len(chunks) != 3 {
		t.Errorf("Chunks(\"abc\", 0) = %q, want one character per chunk", chunks)
	}
}

// TestHyperlinkChunk_TargetIsTheCompleteURL pins the mobile path: the
// target of a chunk is the whole URL, not the chunk.
func TestHyperlinkChunk_TargetIsTheCompleteURL(t *testing.T) {
	const url = "https://example.com/a?b=c"
	row := HyperlinkChunk(url, "https://exa")
	if !strings.Contains(row, "\x1b]8;;"+url+"\x1b\\") {
		t.Errorf("the row does not open an OSC 8 frame to the complete URL: %q", row)
	}
	if !strings.Contains(row, "\x1b]8;;\x1b\\") {
		t.Errorf("the row does not close the OSC 8 frame: %q", row)
	}
	if !strings.Contains(row, "https://exa") {
		t.Errorf("the row does not carry the chunk: %q", row)
	}
}

// TestRows_FallbackWithoutOSC8 pins the fallback: a terminal that
// does not speak OSC 8 must not see the escape sequences, and it must
// still see the whole URL.
//
// render.Hyperlink's own fallback — "label (url)" — would repeat the
// complete URL on every row; the URL is already on screen, row after
// row, so the plain chunk is the right fallback here.
func TestRows_FallbackWithoutOSC8(t *testing.T) {
	const url = "https://example.com/a?b=c&d=e"
	rows := Rows(url, "  ", 10, render.HyperlinkCaps{})
	var joined string
	for _, row := range rows {
		if strings.Contains(row, "\x1b") {
			t.Errorf("a row carries an escape sequence: %q", row)
		}
		if !strings.HasPrefix(row, "  ") {
			t.Errorf("a row does not carry the indent: %q", row)
		}
		joined += strings.TrimPrefix(row, "  ")
	}
	if joined != url {
		t.Errorf("the rows joined are %q, want %q", joined, url)
	}
}

// TestRows_SurviveTheSelection is the guard for the transcript's
// selection: the OSC 8 frame must not be counted as visible width,
// and stripping the row's escapes must leave the plain chunk.
//
// The selection copies the row's text, so an escape sequence left in
// the copy — or a frame the width calculation counts — would break
// both the copy and the layout.
func TestRows_SurviveTheSelection(t *testing.T) {
	const url = "https://example.com/a?b=c&d=e"
	rows := Rows(url, "     ", len(url), render.HyperlinkCaps{OSC8: true})
	if len(rows) != 1 {
		t.Fatalf("the URL fits on one row: %d row(s)", len(rows))
	}
	row := rows[0]
	if got, want := ansi.StringWidth(row), len("     ")+len(url); got != want {
		t.Errorf("the row is %d cells wide, want %d", got, want)
	}
	if got := ansi.Strip(row); got != "     "+url {
		t.Errorf("the stripped row is %q, want the plain chunk", got)
	}
	// A cut inside the chunk must not cut the OSC 8 frame in half.
	cut := ansi.Cut(row, 0, 12)
	if strings.Count(cut, "\x1b]8;;") != 2 {
		t.Errorf("the cut split the OSC 8 frame: %q", cut)
	}
}
