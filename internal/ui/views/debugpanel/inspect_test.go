// inspect_test.go — the /logs surface: its three views, the keys and the
// clicks that move between them, and the frame's own arithmetic.
//
// Every test here builds a panel from hand-made facts: no host, no
// session, no log file. What is pinned is the surface's own behaviour.

package debugpanel

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// fixture builds a panel over a small log, a wire and a trajectory.
func fixture() *Panel {
	logLines := make([]string, 40)
	for i := range logLines {
		logLines[i] = "[2026-11-26 00:00:0" + string(rune('0'+i%10)) + "] [cloudcode] entry-" + string(rune('A'+i%26))
	}
	// The follow state is the host's, so the fixture keeps one: a constant
	// seam would hide whether the panel's own keys actually paused the
	// tail.
	follow := true
	return New(Seams{
		Logs: func() State {
			return State{Path: "/tmp/cli.log", Lines: logLines, Width: 100, Height: 24, SizeBytes: 4096}
		},
		Wire:       func() WireState { return wireFixture() },
		Trajectory: func() TrajectoryState { return trajFixture() },
		Width:      func() int { return 100 },
		Height:     func() int { return 24 },
		Follow:     func() bool { return follow },
		SetFollow:  func(v bool) { follow = v },
	})
}

// plain strips the styling so an assertion reads the text, not the bytes.
func plain(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = ansi.Strip(l)
	}
	return out
}

// listFor is the fold list a view keeps its cursor and folds in. The
// panel's own views read the same fields; the tests name them so an
// assertion can count a view's lines without re-deriving which field the
// panel uses.
func (p *Panel) listFor(v View) *foldList {
	if v == ViewWire {
		return &p.wire
	}
	return &p.trajectory
}

// TestSurfaceStartsOnTheSummaryView — the surface opens on its index.
//
// It opened on the log file while the log file was what the command meant.
// The command is now a diagnostic surface with six views, and the thing to
// land on is the one that says what the other five hold. The log file is
// still one keypress away (2), and TestTheSurfaceReachesEveryView pins that
// every view — the log file included — is reachable from the landing view.
func TestSurfaceStartsOnTheSummaryView(t *testing.T) {
	p := fixture()
	if p.View() != ViewSummary {
		t.Errorf("the surface opened on %v, want the Summary view", p.View())
	}
	joined := strings.Join(plain(p.Lines()), "\n")
	if !strings.Contains(joined, "Summary") {
		t.Errorf("the summary view lost its heading:\n%s", joined)
	}
	// The landing view must name the views it indexes, or it is not an
	// index: a reader has to be able to see that a log file exists from
	// here, and that it is one keypress away.
	for i := 0; i < ViewCount(); i++ {
		if !strings.Contains(joined, View(i).Name()) {
			t.Errorf("the bar does not name view %q:\n%s", View(i).Name(), joined)
		}
	}
}

// TestSurfaceFrameFitsTheHeight — the frame is exactly as tall as the
// terminal gives it.
//
// This is the bug the /logs move fixed: the caller clips a full-viewport
// frame from the top, so a frame one row too tall loses its own view bar.
func TestSurfaceFrameFitsTheHeight(t *testing.T) {
	for _, height := range []int{12, 20, 24, 40} {
		for _, v := range []View{ViewLogs, ViewWire, ViewTrajectory} {
			p := New(Seams{
				Logs:       func() State { return State{Path: "/tmp/x.log", Lines: []string{"a", "b"}} },
				Wire:       func() WireState { return wireFixture() },
				Trajectory: func() TrajectoryState { return trajFixture() },
				Width:      func() int { return 100 },
				Height:     func() int { return height },
			})
			p.SetView(v)
			if got := len(p.Lines()); got != height {
				t.Errorf("view %v at height %d drew %d rows, want exactly %d",
					v, height, got, height)
			}
		}
	}
}

// TestSurfaceTabAndArrowsCycleViews — the bar's own hint names ←/→, so
// those keys must work as well as tab.
func TestSurfaceTabAndArrowsCycleViews(t *testing.T) {
	p := fixture()
	// The steps are derived from the view order rather than written out:
	// adding a view must not require editing a literal here, and the
	// assertion that matters is "tab advances by one and wraps".
	for i := 0; i < ViewCount(); i++ {
		want := p.View().Next()
		if handled, _ := p.HandleKey(tea.Key{Code: tea.KeyTab}); !handled {
			t.Fatalf("tab was not handled at step %d", i)
		}
		if p.View() != want {
			t.Fatalf("tab %d: view = %v, want %v", i, p.View(), want)
		}
	}
	if p.View() != ViewSummary {
		t.Fatalf("%d tabs landed on %v, want the view it started on", ViewCount(), p.View())
	}
	// The bar's hint names ←/→ as well as tab, so the arrows must cycle the
	// same list in both directions.
	if handled, _ := p.HandleKey(tea.Key{Code: tea.KeyRight}); !handled || p.View() != ViewLogs {
		t.Fatalf("right arrow: view = %v, want %v", p.View(), ViewLogs)
	}
	if handled, _ := p.HandleKey(tea.Key{Code: tea.KeyLeft}); !handled || p.View() != ViewSummary {
		t.Fatalf("left arrow: view = %v, want %v", p.View(), ViewSummary)
	}
	// Shift+tab is the same walk backwards, and must wrap at the first view.
	if handled, _ := p.HandleKey(tea.Key{Code: tea.KeyTab, Mod: tea.ModShift}); !handled || p.View() != View(ViewCount()-1) {
		t.Fatalf("shift+tab: view = %v, want the last view %v", p.View(), View(ViewCount()-1))
	}
}

// TestSurfaceNumberKeysJump — 1..6 land on a view directly, one key per
// view in bar order.
//
// The keys are derived from the bar rather than listed, so adding a view
// cannot leave a number that lands nowhere: view i+1 is key i+1.
func TestSurfaceNumberKeysJump(t *testing.T) {
	want := make(map[string]View, ViewCount())
	for i := 0; i < ViewCount(); i++ {
		want[fmt.Sprintf("%d", i+1)] = View(i)
	}
	for text, want := range want {
		p := fixture()
		p.SetView(ViewTrajectory)
		p.HandleKey(tea.Key{Code: rune(text[0]), Text: text})
		if p.View() != want {
			t.Errorf("key %q = %v, want %v", text, p.View(), want)
		}
	}
}

// TestSurfaceClickOnTheBarSwitchesView — a click on a label selects it,
// and the hit region is the one the bar painted.
func TestSurfaceClickOnTheBarSwitchesView(t *testing.T) {
	p := fixture()
	p.Lines() // draw once so the bar records its hit regions

	for _, span := range p.barSpans {
		p.SetView(ViewLogs)
		p.Lines()
		handled, _ := p.Click(0, span.Start)
		if !handled {
			t.Fatalf("a click at column %d was not handled", span.Start)
		}
		if p.View() != View(span.Index) {
			t.Errorf("a click at column %d landed on %v, want %v",
				span.Start, p.View(), View(span.Index))
		}
	}
	// A click past the last label is not a tab.
	p.SetView(ViewLogs)
	if handled, _ := p.Click(0, 500); handled {
		t.Error("a click outside every label was handled as a tab")
	}
}

// TestSurfaceClickOnARowFoldsIt — a click on a foldable body row acts on
// that row, which is what a click on a tree row means.
//
// The press and the release are two steps: the press places the cursor so
// it can also anchor a drag, and the release runs the fold. A press that
// folded here would consume the drag before it began, which is how the
// body's text became undraggable.
func TestSurfaceClickOnARowFoldsIt(t *testing.T) {
	p := fixture()
	p.SetView(ViewTrajectory)
	p.Lines() // draw once: the row list, the cursor and the heading all exist

	start := p.bodyStart(len(TrajectoryHeaderLines(p.trajState)))
	// Find the first body line that is a foldable row. The display lines
	// are not one per row (an open row adds detail lines under itself), so
	// the foldable row's display index is what the click has to use, and
	// the click's row is that index offset by the frame's body start.
	lines := p.trajectory.lines()
	offset := -1
	var id string
	for i := 0; i < len(lines); i++ {
		if lines[i].detail {
			continue
		}
		r := p.trajectory.flattened()[lines[i].row]
		if r.Foldable() {
			offset, id = i, r.ID
			break
		}
	}
	if offset < 0 {
		t.Skip("the fixture has no foldable row in its first frame")
	}
	wasOpen := p.trajectory.open[id]

	handled, _ := p.Click(start+offset, 2)
	if !handled {
		t.Fatal("a press on a body row was not handled")
	}
	// The press only places the cursor: the fold waits for the release.
	if p.trajectory.flattened()[p.trajectory.cursor].ID != id {
		t.Errorf("the press left the cursor on %q, want the pressed row %q",
			p.trajectory.flattened()[p.trajectory.cursor].ID, id)
	}
	if got := p.trajectory.open[id]; got != wasOpen {
		t.Fatalf("the press folded row %q; a press must not fold, or it eats the drag", id)
	}
	// The release runs the action the press armed.
	if handled, _ := p.Act(); !handled {
		t.Fatal("the release was not handled")
	}
	if got := p.trajectory.open[id]; got == wasOpen {
		t.Errorf("the release left row %q at open=%v, want it toggled", id, wasOpen)
	}
}

// TestSurfaceScrollMovesTheVisibleView — the wheel moves whichever view
// is up, and the log view's tail follows the offset.
func TestSurfaceScrollMovesTheVisibleView(t *testing.T) {
	p := fixture()
	p.SetView(ViewLogs)
	p.Lines()
	// Following pins the view to the tail, so the wheel's first job is to
	// take it off the tail. Assert the offset the panel now holds, which
	// is what the next frame draws from.
	if !p.Scroll(-3) {
		t.Fatal("the wheel was not handled on the log view")
	}
	if p.following() {
		t.Error("a wheel-up did not leave the tail")
	}
	if p.scroll == p.maxLogScroll() {
		t.Error("a wheel-up left the offset at the tail")
	}

	p.SetView(ViewTrajectory)
	p.Lines()
	// The wheel scrolls the viewport, not the cursor: the window moves
	// and the cursor is dragged along only when it would leave the
	// window. That is the transcript's gesture, and the one /artifact
	// uses — a wheel that moved the selection felt sticky here.
	//
	// This fixture's trajectory is shorter than the window, so there is
	// nothing to scroll; the assertion here is that the wheel is claimed
	// and does not move the selection. TestWheelScrollsWindowNotCursor
	// covers the moving case with a list long enough to have one.
	beforeCursor := p.trajectory.cursor
	if !p.Scroll(1) {
		t.Error("the wheel was not handled on the trajectory view")
	}
	if p.trajectory.cursor != beforeCursor {
		t.Errorf("the wheel moved the cursor from %d to %d; it must only drag it "+
			"when the cursor would leave the window", beforeCursor, p.trajectory.cursor)
	}
}

// TestSurfaceFoldKeys — o opens everything, p closes everything, and the
// two foldable views keep their own folds.
//
// The assertion counts the list's own lines rather than the frame's: the
// frame is padded to the terminal's height, so a fold changes the list's
// content, not the frame's length.
func TestSurfaceFoldKeys(t *testing.T) {
	for _, v := range []View{ViewWire, ViewTrajectory} {
		p := fixture()
		p.SetView(v)
		p.Lines() // the first draw sets the default fold state

		list := p.listFor(v)
		p.HandleKey(tea.Key{Code: 'p', Text: "p"})
		collapsed := len(list.lines())
		p.HandleKey(tea.Key{Code: 'o', Text: "o"})
		opened := len(list.lines())
		if opened <= collapsed {
			t.Errorf("view %v: o left %d lines, want more than the collapsed %d",
				v, opened, collapsed)
		}
		p.HandleKey(tea.Key{Code: 'p', Text: "p"})
		if got := len(list.lines()); got != collapsed {
			t.Errorf("view %v: p left %d lines, want the %d it started at", v, got, collapsed)
		}
	}
}

// TestSurfaceCopyStripsEscapes — y hands over pasteable text.
func TestSurfaceCopyStripsEscapes(t *testing.T) {
	var copied string
	p := New(Seams{
		Logs:       func() State { return State{} },
		Trajectory: func() TrajectoryState { return trajFixture() },
		Width:      func() int { return 100 },
		Height:     func() int { return 24 },
		Copy:       func(text string) tea.Cmd { copied = text; return nil },
	})
	p.SetView(ViewTrajectory)
	p.Lines()
	if handled, _ := p.HandleKey(tea.Key{Code: 'y', Text: "y"}); !handled {
		t.Fatal("y was not handled")
	}
	if copied == "" {
		t.Fatal("y copied nothing")
	}
	if strings.Contains(copied, "\x1b") {
		t.Errorf("the copied text carries escapes: %q", copied)
	}
}

// TestSurfaceEscCloses — Esc asks the host to close, in every view.
func TestSurfaceEscCloses(t *testing.T) {
	for _, v := range []View{ViewLogs, ViewWire, ViewTrajectory} {
		closed := false
		p := New(Seams{
			Logs:       func() State { return State{} },
			Wire:       func() WireState { return wireFixture() },
			Trajectory: func() TrajectoryState { return trajFixture() },
			Width:      func() int { return 100 },
			Height:     func() int { return 24 },
			Close:      func() tea.Cmd { closed = true; return nil },
		})
		p.SetView(v)
		if handled, _ := p.HandleKey(tea.Key{Code: tea.KeyEsc}); !handled {
			t.Errorf("view %v: Esc was not handled", v)
		}
		if !closed {
			t.Errorf("view %v: Esc did not ask the host to close", v)
		}
	}
}

// TestSurfaceEmptySeamsDegrade — a panel with no host renders a frame in
// every view rather than panicking.
func TestSurfaceEmptySeamsDegrade(t *testing.T) {
	p := New(Seams{})
	for i := 0; i < ViewCount(); i++ {
		v := View(i)
		p.SetView(v)
		if len(p.Lines()) == 0 {
			t.Errorf("view %v rendered nothing with no seams", v)
		}
	}
}

// TestViewByName — the words `/logs` accepts after the command.
func TestViewByName(t *testing.T) {
	yes := map[string]View{
		"wire": ViewWire, "payload": ViewWire,
		"traj": ViewTrajectory, "trajectory": ViewTrajectory, "steps": ViewTrajectory,
		"logs": ViewLogs, "log": ViewLogs,
		"requests": ViewRequests, "request": ViewRequests, "req": ViewRequests,
		"context": ViewContext, "ctx": ViewContext, "window": ViewContext,
	}
	for word, want := range yes {
		if got, ok := ViewByName(word); !ok || got != want {
			t.Errorf("ViewByName(%q) = (%v, %v), want (%v, true)", word, got, ok, want)
		}
	}
	if _, ok := ViewByName("nonsense"); ok {
		t.Error("ViewByName accepted a word that names no view")
	}
}

// TestViewWordsAreDisjoint pins the property that made the alias sets
// change: a word must name exactly one view, or `/logs request` and
// `/logs requests` silently mean different things.
func TestViewWordsAreDisjoint(t *testing.T) {
	words := []string{"logs", "log", "file", "wire", "payload", "next", "prompt",
		"trajectory", "traj", "steps", "history",
		"requests", "request", "reqs", "req", "http", "exchanges",
		"context", "ctx", "window", "tokens"}
	seen := make(map[string]bool, len(words))
	for _, w := range words {
		if seen[w] {
			t.Errorf("%q is listed twice; one spelling must name one view", w)
		}
		seen[w] = true
		if _, ok := ViewByName(w); !ok {
			t.Errorf("%q is listed but resolves to no view", w)
		}
	}
	// Every accepted spelling is in the list above, so the check above is
	// exhaustive rather than a sample.
	for _, w := range append(words, "nonsense", "wires", "requestsx") {
		if _, ok := ViewByName(w); ok && !seen[w] {
			t.Errorf("%q resolves to a view but is not in the disjointness list", w)
		}
	}
}

// TestSurfaceFollowToggles — f pauses and resumes the tail.
func TestSurfaceFollowToggles(t *testing.T) {
	follow := true
	p := New(Seams{
		Logs:      func() State { return State{Path: "/tmp/x.log", Lines: []string{"a"}} },
		Width:     func() int { return 100 },
		Height:    func() int { return 24 },
		Follow:    func() bool { return follow },
		SetFollow: func(v bool) { follow = v },
	})
	// The tail belongs to the log view, so f is the log view's key: the
	// panel now lands on Summary, and f there would be nothing.
	p.SetView(ViewLogs)
	p.HandleKey(tea.Key{Code: 'f', Text: "f"})
	if follow {
		t.Error("f did not pause the tail")
	}
	p.HandleKey(tea.Key{Code: 'f', Text: "f"})
	if !follow {
		t.Error("a second f did not resume the tail")
	}
}
