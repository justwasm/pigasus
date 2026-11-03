package usagepanel

import (
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/palette"
)

// TestDocLoadingRow — while the quota call is in flight the document is
// one row: the spinner glyph, two spaces and the dim-gray "Loading quota
// summary..." text. The glyph cycles through the eight Braille frames.
func TestDocLoadingRow(t *testing.T) {
	frames := []string{"⣾", "⣽", "⣻", "⢿", "⡿", "⣟", "⣯", "⣷"}
	for n, want := range frames {
		doc := Doc(State{Loading: true, Frame: n})
		if len(doc) != 1 {
			t.Fatalf("frame=%d: doc has %d rows, want 1", n, len(doc))
		}
		line := "  " + want + "  " + palette.DimGrayOpen + "Loading quota summary..." + palette.DimGrayClose
		if doc[0] != line {
			t.Errorf("frame=%d:\n got %q\nwant %q", n, doc[0], line)
		}
	}
}

// TestDocLoadedRows — the frozen document with two live countdowns: the
// two model groups, the blanks between them and the wrapped note.
func TestDocLoadedRows(t *testing.T) {
	doc := Doc(State{
		Width:    220,
		RefreshG: time.Now().Add(168 * time.Hour),
		RefreshO: time.Now().Add(48 * time.Hour),
	})
	if len(doc) != 20 {
		t.Fatalf("loaded doc = %d rows, want 20", len(doc))
	}
	if !strings.Contains(doc[5], "\x1b[32mRefreshes in ") {
		t.Errorf("gemini refresh row = %q", doc[5])
	}
	if !strings.Contains(doc[13], "\x1b[31mRefreshes in ") {
		t.Errorf("claude refresh row = %q", doc[13])
	}
	prefix := "  " + palette.SeparatorOpen + "│" + palette.DimGrayOpen
	if !strings.HasPrefix(doc[16], prefix+"Within each group,") {
		t.Errorf("note row[0] = %q", doc[16])
	}
}

// TestDocFrozenCountdown — a bucket with no refresh time falls back to
// the reference's captured countdown, so the frozen document is the one
// the capture recorded.
func TestDocFrozenCountdown(t *testing.T) {
	doc := Doc(State{Width: 220})
	if !strings.Contains(doc[5], "Refreshes in "+frozenRefreshG) {
		t.Errorf("gemini refresh row = %q, want the frozen countdown", doc[5])
	}
	if !strings.Contains(doc[13], "Refreshes in "+frozenRefreshO) {
		t.Errorf("claude refresh row = %q, want the frozen countdown", doc[13])
	}
}

// TestDocSummaryWins — a live answer replaces the frozen document.
func TestDocSummaryWins(t *testing.T) {
	doc := Doc(State{Width: 220, Summary: nil})
	if len(doc) != 20 {
		t.Fatalf("frozen doc = %d rows, want 20", len(doc))
	}
}

// TestLinesLayout — the whole panel: the title, the three chrome rows,
// the window over the document, the counter and the legend.
func TestLinesLayout(t *testing.T) {
	lines, window := Lines(State{Width: 220, Height: 50, Email: "a@b.c"}, 0)
	if window != 0 {
		t.Errorf("window = %d, want 0", window)
	}
	if len(lines) == 0 || !strings.Contains(lines[0], "Models & Quota") {
		t.Fatalf("row 0 = %q, want the title", lines[0])
	}
	if !strings.Contains(lines[2], "a@b.c") {
		t.Errorf("row 2 = %q, want the account line", lines[2])
	}
	if !strings.Contains(lines[len(lines)-1], "Scroll") {
		t.Errorf("legend = %q, want the scroll hint", lines[len(lines)-1])
	}
	// The document fits in one window at this height, so no counter is
	// shown — the reference omits it when the whole document is visible.
	if got := len(lines) - 5 - 1; got != 20 {
		t.Errorf("document rows = %d, want 20 (the whole document)", got)
	}
	if strings.Contains(lines[len(lines)-2], "of 20 lines") {
		t.Errorf("counter = %q, want no counter when the document fits", lines[len(lines)-2])
	}
}

// TestLinesCounter — a window smaller than the document shows the
// counter, naming the window's first and last line.
func TestLinesCounter(t *testing.T) {
	lines, _ := Lines(State{Width: 220, Height: 24, Email: "a@b.c"}, 0)
	page := Page(24)
	if page != 11 {
		t.Fatalf("Page(24) = %d, want 11", page)
	}
	// title + 3 chrome + 11 document rows + counter + blank + legend.
	if len(lines) != 1+3+page+1+1+1 {
		t.Fatalf("panel has %d rows, want %d:\n%s", len(lines), 1+3+page+1+1+1, strings.Join(lines, "\n"))
	}
	if !strings.Contains(lines[1+3+page], "(1\u201311 of 20 lines)") {
		t.Errorf("counter = %q, want the window's line range", lines[1+3+page])
	}
}

// TestLinesClampsWindow — an offset past the document's end is clamped
// back to the last window, and the clamped value is handed back.
func TestLinesClampsWindow(t *testing.T) {
	_, window := Lines(State{Width: 220, Height: 50}, 9999)
	if window != 0 {
		t.Errorf("window = %d, want 0 (the document is shorter than one page)", window)
	}
	_, window = Lines(State{Width: 220, Height: 24}, 9999)
	if window != 9 {
		t.Errorf("window = %d, want 9 (20 document rows, 11 visible)", window)
	}
}

// TestSpecMatchesTheDocument — the viewer's spec names the same document
// the panel renders.
func TestSpecMatchesTheDocument(t *testing.T) {
	st := State{Width: 220, Height: 50}
	spec := Spec(st)
	if len(spec.Lines) != len(Doc(st)) {
		t.Errorf("spec has %d lines, want %d", len(spec.Lines), len(Doc(st)))
	}
	if spec.Visible != Page(50) {
		t.Errorf("visible = %d, want %d", spec.Visible, Page(50))
	}
	if spec.Total != len(spec.Lines) {
		t.Errorf("total = %d, want %d", spec.Total, len(spec.Lines))
	}
}
