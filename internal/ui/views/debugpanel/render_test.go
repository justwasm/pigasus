// render_test.go — the frame's geometry: width, alignment and the
// degradation of the rows that cannot all fit.
//
// The frame's height is pinned in inspect_test.go. These tests pin the
// other axis, which is the one a terminal does not forgive: a row one
// column too wide wraps, and a wrapped row pushes every row below it down
// by one. The host does not clip horizontally, so the panel must.

package debugpanel

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// TestFrameNeverExceedsTheWidth — every row of every view fits.
//
// The widths include the narrow ones where the bar's labels alone fill
// the row: a frame that only fits at 120 columns is a frame that wraps on
// a split pane.
func TestFrameNeverExceedsTheWidth(t *testing.T) {
	for _, width := range []int{40, 60, 80, 100, 120} {
		for _, height := range []int{14, 24} {
			for _, v := range []View{ViewLogs, ViewWire, ViewTrajectory} {
				p := New(Seams{
					Logs: func() State {
						return State{Path: "/tmp/cli.log", Lines: []string{
							"[2026-11-26 00:00:00] [cloudcode] a very long log line that has to wrap somewhere sensible",
						}}
					},
					Wire:       func() WireState { return wireFixture() },
					Trajectory: func() TrajectoryState { return trajFixture() },
					Width:      func() int { return width },
					Height:     func() int { return height },
				})
				p.SetView(v)
				for i, row := range plain(p.Lines()) {
					if w := ansi.StringWidth(row); w > width {
						t.Errorf("view %v at %dx%d: row %d is %d columns: %q",
							v, width, height, i, w, row)
					}
				}
			}
		}
	}
}

// TestTabBarDropsTheHintBeforeALabel — the bar's hint is the first thing
// a narrow terminal loses, and the labels are the last.
func TestTabBarDropsTheHintBeforeALabel(t *testing.T) {
	// The narrow widths are the point: a bar only paints a fragment when
	// the clip lands inside a label, and that happens below 30 columns.
	for _, width := range []int{18, 20, 22, 24, 25, 26, 27, 28, 29, 30, 40, 50, 60, 80} {
		line, spans := TabBarWidth(ViewWire, width)
		if got := ansi.StringWidth(line); got > width {
			t.Errorf("width %d: the bar is %d columns: %q", width, got, ansi.Strip(line))
		}
		// Every label that was painted is clickable.
		painted := ansi.Strip(line)
		for _, span := range spans {
			name := viewNames[span.Index]
			if !strings.Contains(painted, name) {
				t.Errorf("width %d: span %d names %q, which the bar did not paint: %q",
					width, span.Index, name, painted)
			}
			if span.End > ansi.StringWidth(painted) {
				t.Errorf("width %d: span %d ends at %d, past the bar's %d columns",
					width, span.Index, span.End, ansi.StringWidth(painted))
			}
		}
		// And every label that was painted is clickable, in the other
		// direction: a fragment the user can see but not click is the
		// failure this pins. The span for a fragment is dropped (its End
		// is past the width), so checking the spans alone cannot see it —
		// the painted text has to be read.
		if frag := partialLabel(painted); frag != "" {
			t.Errorf("width %d: the bar painted part of a label (%q): %q", width, frag, painted)
		}
	}
	// Wide enough for everything: the hint is there and all three labels
	// are clickable.
	line, spans := TabBarWidth(ViewLogs, 120)
	if !strings.Contains(ansi.Strip(line), "tab to cycle") {
		t.Errorf("at 120 columns the bar lost its hint: %q", ansi.Strip(line))
	}
	if len(spans) != ViewCount() {
		t.Errorf("at 120 columns the bar reported %d spans, want %d", len(spans), ViewCount())
	}
}

// partialLabel returns the last word of a painted bar when it is a
// fragment of one of the view names, or "" when every word on the row is
// whole.
//
// A fragment is what a clip leaves behind: "Wi" is a prefix of "Wire" but
// is not "Wire", and no label on the bar ends with it. Reading the text
// rather than the spans is the point — the span for a fragment is dropped
// as unclickable, so a span-based check would see a clean bar.
func partialLabel(painted string) string {
	fields := strings.Fields(painted)
	if len(fields) == 0 {
		return ""
	}
	last := fields[len(fields)-1]
	for _, name := range viewNames {
		if last == name {
			return ""
		}
		// A word that is a proper prefix of a name is a cut label. The
		// brand and the hint are not view names, so they fall through.
		if strings.HasPrefix(name, last) && last != "" {
			return last
		}
	}
	return ""
}

// TestLegendDropsWholeEntries — a narrow legend loses entries from the
// end rather than cutting the last one mid-word.
func TestLegendDropsWholeEntries(t *testing.T) {
	const full = "↑/↓ scroll  ·  f follow  ·  g/G top/end  ·  tab next view  ·  esc close"
	entries := strings.Split(full, "  ·  ")
	for _, width := range []int{20, 30, 40, 50, 60, 200} {
		got := ansi.Strip(legendRow(full, width))
		if w := ansi.StringWidth(got); w > width {
			t.Errorf("width %d: the legend is %d columns: %q", width, w, got)
		}
		trimmed := strings.TrimSpace(got)
		if trimmed == "" {
			continue // nothing fit, which is a legitimate answer
		}
		// What is left must be a prefix of the entries, whole ones only.
		if !strings.HasPrefix(full, trimmed) {
			t.Errorf("width %d: %q is not a whole-entry prefix of the legend", width, trimmed)
		}
		if strings.HasSuffix(trimmed, "…") {
			t.Errorf("width %d: the legend was truncated mid-entry: %q", width, trimmed)
		}
		_ = entries
	}
	// A wide legend is the legend, unchanged.
	if got := ansi.Strip(legendRow(full, 200)); !strings.HasSuffix(got, "esc close") {
		t.Errorf("a wide legend lost its last entry: %q", got)
	}
}

// TestClickAfterScrollHitsTheDrawnRow — a click acts on the row the user
// can see, not on whatever sits at that offset in the unscrolled list.
//
// The body shows the list from its window, so the frame row has to be
// translated through the window before it is an index into the display
// lines. Skipping that step makes a click on a scrolled tree fold the
// wrong branch — and it folds *something*, so nothing looks broken.
func TestClickAfterScrollHitsTheDrawnRow(t *testing.T) {
	p := New(Seams{
		Logs:       func() State { return State{Path: "/tmp/x.log", Lines: []string{"a"}} },
		Wire:       func() WireState { return wireFixture() },
		Trajectory: func() TrajectoryState { return trajFixture() },
		Width:      func() int { return 100 },
		Height:     func() int { return 14 },
	})
	p.SetView(ViewTrajectory)
	p.Lines()
	p.HandleKey(tea.Key{Code: 'o', Text: "o"}) // open everything: enough lines to scroll
	p.Lines()
	for i := 0; i < 4; i++ {
		p.HandleKey(tea.Key{Code: tea.KeyDown})
	}
	p.Lines()
	if p.trajectory.window == 0 {
		t.Fatalf("the list did not scroll, so this test proves nothing (window=%d)", p.trajectory.window)
	}

	// The row the frame draws on its first body line.
	start := p.bodyStart(p.trajHeadingRows)
	drawn := p.trajectory.lines()[p.trajectory.window]
	wantID := p.trajectory.flattened()[drawn.row].ID

	if handled, _ := p.Click(start, 2); !handled {
		t.Fatal("a click on a visible body row was not handled")
	}
	got := p.trajectory.flattened()[p.trajectory.cursor].ID
	if got != wantID {
		t.Errorf("the click selected %q, but the row drawn there is %q", got, wantID)
	}
}

// TestWireColumnsAlignAcrossGroups — the byte and token figures form one
// column down the whole view, group headings included.
//
// Three groups each sizing their own name column is what made "tools  3
// 3.8 KB" start left of "run_command  2.1 KB": one table of numbers with
// three left edges reads as three unrelated lists.
func TestWireColumnsAlignAcrossGroups(t *testing.T) {
	p := fixture()
	p.SetView(ViewWire)
	p.HandleKey(tea.Key{Code: 'o', Text: "o"})

	bytesCol, tokCol := -1, -1
	for _, row := range plain(p.Lines()) {
		row = strings.TrimRight(row, " ")
		// Only the measured rows: a heading's total and the view's own
		// summary line are not part of the table.
		if !strings.Contains(row, " tok") {
			continue
		}
		if strings.Contains(row, "total") || strings.HasPrefix(strings.TrimSpace(row), "└") {
			continue
		}
		b, tok := columnOfMeasure(row)
		if b < 0 || tok < 0 {
			continue
		}
		if bytesCol < 0 {
			bytesCol, tokCol = b, tok
			continue
		}
		// Both columns are right-aligned, so it is the *end* of each
		// figure that has to agree: "900 B" and "2.9 KB" start at
		// different columns and end at the same one, by design.
		if b != bytesCol {
			t.Errorf("the byte figure ends at %d here, %d above: %q", b, bytesCol, row)
		}
		if tok != tokCol {
			t.Errorf("the token figure ends at %d here, %d above: %q", tok, tokCol, row)
		}
	}
	if bytesCol < 0 {
		t.Fatal("no measured row was rendered")
	}
}

// columnOfMeasure reports where a row's byte figure ends and where its
// token figure ends, or -1 for a row that has neither.
//
// The columns are display cells, not byte offsets: a fold marker is one
// cell and three bytes, so a byte index would report every foldable row
// two columns right of its non-foldable neighbour and call a correctly
// aligned table misaligned.
func columnOfMeasure(row string) (bytesEnd, tokEnd int) {
	i := strings.Index(row, " tok")
	if i < 0 {
		return -1, -1
	}
	tokEnd = ansi.StringWidth(row[:i]) + len(" tok")
	if k := strings.Index(row, " KB"); k >= 0 && k < i {
		return ansi.StringWidth(row[:k]) + len(" KB"), tokEnd
	}
	if k := strings.Index(row, " B "); k >= 0 && k < i {
		return ansi.StringWidth(row[:k]) + len(" B"), tokEnd
	}
	return -1, -1
}

// TestTrajectoryHeadingPluralises — the heading counts in words, not in
// "1 checkpoint(s)".
func TestTrajectoryHeadingPluralises(t *testing.T) {
	one := TrajectoryState{Turns: 1, Checkpoints: 1, ColdChunks: 1, Steps: []TrajStep{{Index: 1}}}
	heading := one.trajectoryHeading()
	for _, bad := range []string{"(s)", "1 turns", "1 steps"} {
		if strings.Contains(heading, bad) {
			t.Errorf("the heading says %q: %q", bad, heading)
		}
	}
	for _, want := range []string{"1 step", "1 turn", "1 checkpoint", "1 cold chunk"} {
		if !strings.Contains(heading, want) {
			t.Errorf("the heading is missing %q: %q", want, heading)
		}
	}

	many := TrajectoryState{Turns: 2, Checkpoints: 3, ColdChunks: 4, Steps: make([]TrajStep, 5)}
	heading = many.trajectoryHeading()
	for _, want := range []string{"5 steps", "2 turns", "3 checkpoints", "4 cold chunks"} {
		if !strings.Contains(heading, want) {
			t.Errorf("the heading is missing %q: %q", want, heading)
		}
	}
}

// TestQClosesLikeEsc — q is the other cancel key every viewer in the tree
// answers, and a panel that claims it without acting looks stuck.
func TestQClosesLikeEsc(t *testing.T) {
	for _, v := range []View{ViewLogs, ViewWire, ViewTrajectory} {
		closed := false
		p := New(Seams{
			Logs:       func() State { return State{Path: "/tmp/x.log", Lines: []string{"a"}} },
			Wire:       func() WireState { return wireFixture() },
			Trajectory: func() TrajectoryState { return trajFixture() },
			Width:      func() int { return 100 },
			Height:     func() int { return 24 },
			Close:      func() tea.Cmd { closed = true; return nil },
		})
		p.SetView(v)
		if handled, _ := p.HandleKey(tea.Key{Code: 'q', Text: "q"}); !handled {
			t.Errorf("view %v: q was not handled", v)
		}
		if !closed {
			t.Errorf("view %v: q did not ask the host to close", v)
		}
	}
}

// TestEmptyGroupsSayEmpty — a group with no members prints "(empty)"
// rather than the shape of a measurement of nothing.
func TestEmptyGroupsSayEmpty(t *testing.T) {
	rows := WireRows(WireState{Width: 100})
	for _, row := range rows {
		text := ansi.Strip(row.Text)
		if strings.Contains(text, "- tok") {
			t.Errorf("an empty group printed a token count: %q", text)
		}
		if !strings.Contains(text, "(empty)") {
			t.Errorf("an empty group did not say so: %q", text)
		}
	}
}
