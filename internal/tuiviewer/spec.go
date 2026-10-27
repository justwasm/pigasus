package tuiviewer

import (
	"fmt"

	"github.com/justwasm/pigasus/palette"
)

// Spec describes a scrollable documentary panel.
type Spec struct {
	Lines   []string
	Visible int
	Total   int
}

// Window computes the visible slice of lines and the 1-based start/end numbers for a window offset.
func Window(spec Spec, windowOffset int) (rows []string, start, end, clampedOffset int) {
	n := len(spec.Lines)
	vis := spec.Visible
	if vis < 1 {
		vis = 1
	}
	maxOffset := n - vis
	if maxOffset < 0 {
		maxOffset = 0
	}
	clamped := windowOffset
	if clamped < 0 {
		clamped = 0
	}
	if clamped > maxOffset {
		clamped = maxOffset
	}
	hi := clamped + vis
	if hi > n {
		hi = n
	}
	rows = spec.Lines[clamped:hi]
	start = clamped + 1
	end = clamped + vis
	if end > n {
		end = n
	}
	if end < start {
		end = start
	}
	return rows, start, end, clamped
}

// Counter renders the dim gray "(a–b of K lines)" footer row.
func Counter(start, end, total int) string {
	return palette.DimGrayOpen + fmt.Sprintf("  (%d\u2013%d of %d lines)", start, end, total) + palette.DimGrayClose
}

// MaxOffset computes the maximum scroll offset for a Spec.
func MaxOffset(spec Spec) int {
	vis := spec.Visible
	if vis < 1 {
		vis = 1
	}
	maxOffset := len(spec.Lines) - vis
	if maxOffset < 0 {
		return 0
	}
	return maxOffset
}
