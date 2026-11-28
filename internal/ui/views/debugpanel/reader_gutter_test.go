// reader_gutter_test.go — the reader's line gutter and line cursor.
//
// # What these pin
//
// The reader used to be a scrolling document: the keys moved the window,
// there were no line numbers, and the footer counted window offsets. It
// is now a line-cursor viewer in /artifact's shape — numbers in a gutter,
// a `>` on the line the keys act on, and a footer that reports the
// cursor's *number* rather than its offset.
//
// Three things are worth pinning, because each was wrong at least once
// while this was built:
//
//  1. the cursor's number in the footer is the number printed beside it
//  2. the reader's furniture (a part label, a blank separator) carries no
//     number, and the cursor cannot rest on it
//  3. the text starts at the same column on every row, cursor or not
package debugpanel

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	tuiviewer "github.com/justwasm/pigasus/internal/ui/foundations/viewer"
)

// gutterPanel is a panel with a two-part body open: a labelled argument
// block and a labelled result, so the furniture rows exist.
func gutterPanel(t *testing.T) *Panel {
	t.Helper()
	p := New(Seams{
		Width:  func() int { return 100 },
		Height: func() int { return 24 },
	})
	p.SetView(ViewTrajectory)
	p.reader = newReader("traj/1", Body{
		Title: "traj/1 · tool",
		Parts: []ReaderPart{
			{Label: "args", Text: "go build ./..."},
			{Label: "result", Text: "ok\nline two\nline three"},
		},
	})
	return p
}

// readerBodyOf strips the frame down to its body rows and returns them.
func readerBodyOf(p *Panel) []string {
	lines := p.Lines()
	start := 1 + 1 + len(p.readerHeadingLines()) + 1
	end := len(lines) - 2 // counter and legend
	if start >= end {
		return nil
	}
	return lines[start:end]
}

// TestReaderNumbersOnlyTheBody — a part's label and the blank separator
// are the reader's furniture: numbering them would make the numbers
// disagree with the source.
func TestReaderNumbersOnlyTheBody(t *testing.T) {
	p := gutterPanel(t)
	body := readerBodyOf(p)

	var numbered, furniture int
	for _, row := range body {
		plain := ansi.Strip(row)
		if strings.TrimSpace(plain) == "" {
			continue
		}
		if strings.Contains(plain, "args") || strings.Contains(plain, "result") {
			furniture++
			continue
		}
		numbered++
	}
	if furniture != 2 {
		t.Errorf("found %d label rows, want 2 (args and result)", furniture)
	}
	if numbered != 4 {
		t.Errorf("numbered %d rows, want 4 (one arg line and three result lines)", numbered)
	}
}

// TestReaderBodyNumbersAreSequential — the numbers run 1..N with no gap,
// so a line's number is its position in the body it came from.
func TestReaderBodyNumbersAreSequential(t *testing.T) {
	p := gutterPanel(t)
	body := readerBodyOf(p)

	want := 1
	for _, row := range body {
		plain := ansi.Strip(row)
		trimmed := strings.TrimSpace(plain)
		if trimmed == "" || strings.Contains(trimmed, "args") || strings.Contains(trimmed, "result") {
			continue
		}
		num := strings.TrimSpace(plain[tuiviewer.GutterCursorWidth : tuiviewer.GutterCursorWidth+tuiviewer.GutterNumberWidth])
		if num != itoa(want) {
			t.Errorf("row %q carries number %q, want %d", trimmed, num, want)
		}
		want++
	}
}

// TestReaderCursorMarkMatchesTheFooter — the `>` and the footer's L<n>
// must name the same line. They disagreed when the cursor was indexed by
// document position but printed against body-line numbers.
func TestReaderCursorMarkMatchesTheFooter(t *testing.T) {
	p := gutterPanel(t)
	p.Lines()

	for i := 0; i < 4; i++ {
		body := readerBodyOf(p)
		footer := ansi.Strip(p.Lines()[len(p.Lines())-2])

		// Find the cursor's row and the number beside it.
		cursorNum := ""
		for _, row := range body {
			plain := ansi.Strip(row)
			if !strings.HasPrefix(plain, ">") {
				continue
			}
			cursorNum = strings.TrimSpace(plain[tuiviewer.GutterCursorWidth : tuiviewer.GutterCursorWidth+tuiviewer.GutterNumberWidth])
			break
		}
		if cursorNum == "" {
			t.Fatalf("no cursor row in the body; footer = %q", footer)
		}
		if !strings.Contains(footer, "L"+cursorNum) {
			t.Errorf("cursor is on line %s but the footer says %q", cursorNum, footer)
		}

		p.HandleKey(tea.Key{Code: 'j', Text: "j"})
	}
}

// TestReaderCursorSkipsFurnitureRows — the cursor must never land on a
// label row: there is no number there, so the footer would read L0.
func TestReaderCursorSkipsFurnitureRows(t *testing.T) {
	p := gutterPanel(t)
	p.Lines()

	seen := map[string]bool{}
	for i := 0; i < 20; i++ {
		body := readerBodyOf(p)
		footer := ansi.Strip(p.Lines()[len(p.Lines())-2])
		if strings.Contains(footer, "L0") {
			t.Fatalf("the cursor rested on an unnumbered row: footer = %q", footer)
		}
		for _, row := range body {
			plain := ansi.Strip(row)
			if !strings.HasPrefix(plain, ">") {
				continue
			}
			num := strings.TrimSpace(plain[tuiviewer.GutterCursorWidth : tuiviewer.GutterCursorWidth+tuiviewer.GutterNumberWidth])
			if num == "" {
				t.Fatalf("the cursor row has no number: %q", plain)
			}
			seen[num] = true
		}
		p.HandleKey(tea.Key{Code: 'j', Text: "j"})
	}
	if len(seen) < 4 {
		t.Errorf("the cursor only ever reached %d numbered lines, want 4", len(seen))
	}
}

// TestReaderTextColumnIsFixed — every non-blank row's text starts at the
// same column, which is the whole point of a gutter.
func TestReaderTextColumnIsFixed(t *testing.T) {
	p := gutterPanel(t)
	if got, want := len(p.reader.lines(100)), 7; got != want {
		t.Fatalf("the reader rendered %d lines, want %d", got, want)
	}
	want := tuiviewer.GutterIndent(p.reader.bodyLineCount(100))
	for _, row := range readerBodyOf(p) {
		plain := ansi.Strip(row)
		if strings.TrimSpace(plain) == "" {
			continue
		}
		// The gutter is a fixed shape: cursor column, number cell, gap.
		// The text begins right after it, so the cell at `want` is the
		// text's first character and the cells before it are the gutter.
		if len(plain) <= want {
			t.Errorf("row %q is shorter than its own gutter (%d cells)", plain, want)
			continue
		}
		if got := ansi.StringWidth(plain[:want]); got != want {
			t.Errorf("row %q: the gutter is %d cells, want %d", plain, got, want)
		}
	}
}

// TestReaderKeysMoveTheCursorNotTheWindow — a body taller than the
// window: `j` moves the cursor and leaves the window alone until the
// cursor reaches its bottom edge.
func TestReaderKeysMoveTheCursorNotTheWindow(t *testing.T) {
	p := New(Seams{
		Width:  func() int { return 100 },
		Height: func() int { return 14 }, // small window: ~6 body rows
	})
	p.SetView(ViewTrajectory)
	lines := make([]string, 40)
	for i := range lines {
		lines[i] = "body line " + itoa(i+1)
	}
	p.reader = newReader("traj/1", Body{
		Title: "traj/1",
		Parts: []ReaderPart{{Text: strings.Join(lines, "\n")}},
	})
	p.Lines()

	if p.reader.scroll != 0 {
		t.Fatalf("the window starts at %d, want 0", p.reader.scroll)
	}
	before := p.reader.line
	p.HandleKey(tea.Key{Code: 'j', Text: "j"})
	if p.reader.line != before+1 {
		t.Errorf("cursor = %d after j, want %d", p.reader.line, before+1)
	}
	if p.reader.scroll != 0 {
		t.Errorf("the window scrolled to %d on a one-line cursor move inside it", p.reader.scroll)
	}
}

// TestReaderWheelScrollsTheWindow — the wheel is a viewport gesture, and
// it drags the cursor into view rather than leaving it behind.
func TestReaderWheelScrollsTheWindow(t *testing.T) {
	p := New(Seams{
		Width:  func() int { return 100 },
		Height: func() int { return 14 },
	})
	p.SetView(ViewTrajectory)
	lines := make([]string, 40)
	for i := range lines {
		lines[i] = "body line " + itoa(i+1)
	}
	p.reader = newReader("traj/1", Body{
		Title: "traj/1",
		Parts: []ReaderPart{{Text: strings.Join(lines, "\n")}},
	})
	p.Lines()

	p.reader.line = 0
	p.Scroll(5)
	visible := p.readerRows
	if p.reader.scroll == 0 {
		t.Error("the wheel did not move the window")
	}
	if line := p.reader.line; line < p.reader.scroll || line >= p.reader.scroll+visible {
		t.Errorf("the wheel left the cursor at line %d, outside window [%d,%d)",
			line, p.reader.scroll, p.reader.scroll+visible)
	}
	if p.reader.scroll == 0 {
		t.Error("the window snapped back to the top")
	}
}

// itoa is the tests' own integer spelling, so the package's format helpers
// are not exercised by an assertion about their output.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
