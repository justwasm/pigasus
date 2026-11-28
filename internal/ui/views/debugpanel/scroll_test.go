// scroll_test.go — the wheel's own behaviour in the two foldable views.
//
// # What this pins
//
// The wheel scrolls the viewport. It does not move the selection. Those
// are two different gestures and the panel had them confused: the wheel
// called moveCursor, so a notch moved the `>` highlight and the window
// merely followed it. The transcript's wheel moves the viewport, and
// /artifact's does too, so the same notch meant something different in
// /logs than everywhere else — the "not as smooth as main chat" report.
//
// The cursor still has to stay visible (a key acts on it), so it is
// dragged into the window when a scroll would leave it behind. That is
// the second thing these tests pin: dragging happens only when needed.
package debugpanel

import (
	"fmt"
	"testing"
)

// longTrajPanel is a panel whose trajectory is much taller than its
// window, so there is something to scroll.
func longTrajPanel(t *testing.T, steps int) *Panel {
	t.Helper()
	return New(Seams{
		Trajectory: func() TrajectoryState {
			st := TrajectoryState{Width: 100}
			for i := 1; i <= steps; i++ {
				st.Steps = append(st.Steps, TrajStep{
					Index:   int64(i),
					Kind:    TrajTool,
					Summary: fmt.Sprintf("step %d", i),
					Detail:  []string{"body of step " + fmt.Sprint(i)},
				})
			}
			return st
		},
		Width:  func() int { return 100 },
		Height: func() int { return 24 },
	})
}

// TestWheelScrollsWindowNotCursor — a notch moves the window and the next
// frame draws from it.
//
// The cursor follows only when the scroll would leave it off screen, so
// the assertion is that it is *visible*, not that it never moves: a
// scroll of 3 from the very first row does drag it, because the cursor
// would otherwise be above the window.
func TestWheelScrollsWindowNotCursor(t *testing.T) {
	p := longTrajPanel(t, 60)
	p.SetView(ViewTrajectory)
	p.Lines() // establishes the window and the row count

	if p.trajectory.window != 0 {
		t.Fatalf("the window starts at %d, want 0", p.trajectory.window)
	}
	first := ansiStrip(p.Lines()[bodyTopFor(p, p.trajHeadingRows)])
	if !p.Scroll(3) {
		t.Fatal("the wheel was not handled")
	}
	if got := p.trajectory.window; got != 3 {
		t.Errorf("window = %d after a 3-row wheel, want 3", got)
	}
	visible := p.bodyRows(p.trajHeadingRows)
	line := p.trajectory.lineOfCursor(p.trajectory.lines())
	if line < p.trajectory.window || line >= p.trajectory.window+visible {
		t.Errorf("the cursor's line %d is outside the window [%d,%d) after the wheel",
			line, p.trajectory.window, p.trajectory.window+visible)
	}

	second := ansiStrip(p.Lines()[bodyTopFor(p, p.trajHeadingRows)])
	if first == second {
		t.Error("the frame did not change after the wheel; the window is not feeding the render")
	}
}

// TestWheelLeavesAVisibleCursorAlone — when the cursor is already inside
// the window, scrolling does not move it. This is the half that makes the
// wheel feel like scrolling rather than like selecting.
func TestWheelLeavesAVisibleCursorAlone(t *testing.T) {
	p := longTrajPanel(t, 60)
	p.SetView(ViewTrajectory)
	p.Lines()

	// Park the cursor in the middle of the window, well away from both
	// edges, so a one-row scroll cannot push it out.
	visible := p.bodyRows(p.trajHeadingRows)
	p.trajectory.toCursor(false)
	for i := 0; i < visible/2; i++ {
		p.trajectory.moveCursor(1)
	}
	p.Lines()
	before := p.trajectory.cursor

	p.Scroll(1)
	if p.trajectory.cursor != before {
		t.Errorf("cursor moved %d -> %d on a one-row scroll while it was mid-window",
			before, p.trajectory.cursor)
	}
}

// TestWheelUpScrollsBack — the window comes back, which is the direction
// the report was about ("scrolling up on a long Wire body").
func TestWheelUpScrollsBack(t *testing.T) {
	p := longTrajPanel(t, 60)
	p.SetView(ViewTrajectory)
	p.Lines()

	p.Scroll(5)
	if got := p.trajectory.window; got != 5 {
		t.Fatalf("window = %d after scrolling down 5, want 5", got)
	}
	if !p.Scroll(-2) {
		t.Fatal("the wheel-up was not handled")
	}
	if got := p.trajectory.window; got != 3 {
		t.Errorf("window = %d after scrolling up 2, want 3", got)
	}
}

// TestWheelDoesNotRunPastTheEnd — the window stops at the last page
// instead of scrolling into empty space.
func TestWheelDoesNotRunPastTheEnd(t *testing.T) {
	p := longTrajPanel(t, 20)
	p.SetView(ViewTrajectory)
	p.Lines()

	for i := 0; i < 200; i++ {
		p.Scroll(5)
	}
	visible := p.bodyRows(p.trajHeadingRows)
	total := len(p.trajectory.lines())
	if want := total - visible; p.trajectory.window != want {
		t.Errorf("window = %d at the end, want %d (total %d, visible %d)",
			p.trajectory.window, want, total, visible)
	}
}

// TestWheelDragsTheCursorIntoTheWindow — the cursor must stay somewhere a
// key can act on it, so a scroll that would leave it behind drags it.
func TestWheelDragsTheCursorIntoTheWindow(t *testing.T) {
	p := longTrajPanel(t, 60)
	p.SetView(ViewTrajectory)
	p.Lines()

	// Put the cursor at the top, then scroll far enough that it would be
	// left above the window.
	p.trajectory.toCursor(false)
	visible := p.bodyRows(p.trajHeadingRows)
	p.Scroll(visible + 5)

	line := p.trajectory.lineOfCursor(p.trajectory.lines())
	if line < p.trajectory.window || line >= p.trajectory.window+visible {
		t.Errorf("the cursor's line %d is outside the window [%d,%d); "+
			"the wheel must keep the selection visible",
			line, p.trajectory.window, p.trajectory.window+visible)
	}
}

// TestWheelIsANoOpAtTheTop — a wheel-up with nothing above reports that
// nothing moved, so the host does not repaint for nothing.
func TestWheelIsANoOpAtTheTop(t *testing.T) {
	p := longTrajPanel(t, 60)
	p.SetView(ViewTrajectory)
	p.Lines()

	if p.trajectory.window != 0 {
		t.Fatalf("the window starts at %d, want 0", p.trajectory.window)
	}
	visible := p.bodyRows(p.trajHeadingRows)
	if p.trajectory.scrollWindow(-1, visible) {
		t.Error("scrollWindow(-1) at the top reported a move")
	}
}

// TestKeysStillMoveTheCursor — the wheel's change must not have taken the
// cursor keys with it.
func TestKeysStillMoveTheCursor(t *testing.T) {
	p := longTrajPanel(t, 60)
	p.SetView(ViewTrajectory)
	p.Lines()

	before := p.trajectory.cursor
	p.trajectory.moveCursor(1)
	if p.trajectory.cursor != before+1 {
		t.Errorf("cursor = %d after j, want %d", p.trajectory.cursor, before+1)
	}
}

// bodyTopFor is the frame index the body's first row is drawn at.
func bodyTopFor(p *Panel, headingRows int) int {
	return p.bodyStart(headingRows)
}

// ansiStrip removes styling so an assertion compares text.
func ansiStrip(s string) string { return stripStyles(s) }
