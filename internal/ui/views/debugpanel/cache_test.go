// cache_test.go — the panel's per-frame memos must not outlive their
// inputs.
//
// The panel caches three things to keep a wheel notch cheap: the host's
// Wire and Trajectory facts, the fold list's rendered lines, and the
// clipped rows. Every one of them is a place where a missed invalidation
// shows the user a stale frame — a step list that never grows, a fold
// that does not open, a sort that does not reorder. These tests pin the
// invalidations rather than the speed, because speed without correctness
// is a bug that looks like a feature.
package debugpanel

import (
	"strings"
	"testing"
)

// countingTrajectory is a host whose facts change on demand, and which
// counts how often it was asked.
type countingTrajectory struct {
	calls int
	steps []TrajStep
}

func (c *countingTrajectory) seam() TrajectoryState {
	c.calls++
	return TrajectoryState{Width: 100, Steps: c.steps}
}

// TestTrajectoryFactsAreFetchedOncePerFrame — the memo exists because the
// host answers twice per repaint (the frame and the row count).
func TestTrajectoryFactsAreFetchedOncePerFrame(t *testing.T) {
	c := &countingTrajectory{steps: []TrajStep{
		{Index: 1, Kind: TrajTool, Summary: "one"},
	}}
	p := New(Seams{
		Trajectory: c.seam,
		Width:      func() int { return 100 },
		Height:     func() int { return 24 },
	})
	p.SetView(ViewTrajectory)

	p.Lines()
	afterFirst := c.calls
	p.RowCount()
	if c.calls != afterFirst {
		t.Errorf("the row count asked the host again (%d -> %d); one frame is one answer",
			afterFirst, c.calls)
	}
}

// TestInvalidateMakesTheNextFrameSeeNewFacts — a background turn adds a
// step the panel cannot see. Without Invalidate the memo would answer
// with the old list forever.
func TestInvalidateMakesTheNextFrameSeeNewFacts(t *testing.T) {
	c := &countingTrajectory{steps: []TrajStep{
		{Index: 1, Kind: TrajTool, Summary: "one"},
	}}
	p := New(Seams{
		Trajectory: c.seam,
		Width:      func() int { return 100 },
		Height:     func() int { return 24 },
	})
	p.SetView(ViewTrajectory)
	p.Lines()

	c.steps = append(c.steps, TrajStep{Index: 2, Kind: TrajTool, Summary: "two"})
	p.Invalidate()

	// Draw the frame: that is what asks the host for the facts again. A
	// test that read the list without drawing would be asserting about a
	// frame the user never saw.
	p.Lines()
	if !strings.Contains(plainLines(p.trajectory.lines()), "two") {
		t.Error("the new step never appeared; Invalidate did not drop the memo")
	}
}

// TestSortToggleRebuildsTheRows — `s` changes the order, and the memo
// covers the facts rather than the order, so the toggle has to bump.
func TestSortToggleRebuildsTheRows(t *testing.T) {
	p := New(Seams{
		Wire: func() WireState {
			return WireState{Width: 100, SortByBytes: false, Messages: []WireMessage{
				{Index: 1, Role: "user", Bytes: 100, Tokens: 30},
				{Index: 2, Role: "user", Bytes: 9000, Tokens: 3000},
			}}
		},
		Width:  func() int { return 100 },
		Height: func() int { return 24 },
	})
	p.SetView(ViewWire)
	p.Lines()

	first := plainLines(p.wire.lines())
	p.sortByBytes = !p.sortByBytes
	p.Invalidate()
	p.Lines()
	second := plainLines(p.wire.lines())

	if first == second {
		t.Error("toggling the sort did not change the order; the memo was not dropped")
	}
}

// TestFoldSurvivesARebuild — the fold state is keyed by row ID, so a new
// frame over the same rows keeps the user's folds.
func TestFoldSurvivesARebuild(t *testing.T) {
	p := New(Seams{
		Wire:   func() WireState { return wireFixture() },
		Width:  func() int { return 100 },
		Height: func() int { return 24 },
	})
	p.SetView(ViewWire)
	p.Lines()

	// Collapse everything, then force a rebuild.
	p.wire.collapseAll()
	before := len(p.wire.lines())
	p.Invalidate()
	p.Lines()
	after := len(p.wire.lines())

	if before != after {
		t.Errorf("the list changed from %d to %d lines across a rebuild; the fold was lost", before, after)
	}
}

// plainLines joins rendered lines for a substring assertion.
func plainLines(lines []listLine) string {
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(stripStyles(l.text))
		b.WriteString("\n")
	}
	return b.String()
}
