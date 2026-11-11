package viewport

import (
	"strings"

	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/palette"
)

// Window represents a mathematically safe sub-range [start, end] within a collection of length total.
// Its invariants are strictly guaranteed at construction:
// 0 <= start <= end <= total.
type Window struct {
	start int
	end   int
	total int
}

// NewWindow creates a Window safely clamped to [0, total].
// Even if offset < 0, count < 0, or offset+count exceeds total or overflows,
// the returned Window is guaranteed to satisfy 0 <= start <= end <= total.
func NewWindow(total, offset, count int) Window {
	if total <= 0 || count <= 0 {
		return Window{start: 0, end: 0, total: max(0, total)}
	}
	start := tuilib.ClampInt(offset, 0, total)
	end := start + count
	if end > total || end < start {
		end = total
	}
	return Window{
		start: start,
		end:   end,
		total: total,
	}
}

// ScrollWindow computes a Window of up to visible elements, clamping scroll to [0, max(0, total-visible)].
// This ensures the window never displays fewer elements than visible (unless total < visible)
// and never slices out of bounds.
func ScrollWindow(total, scroll, visible int) Window {
	if total <= 0 || visible <= 0 {
		return Window{total: max(0, total)}
	}
	maxScroll := max(0, total-visible)
	start := tuilib.ClampInt(scroll, 0, maxScroll)
	return NewWindow(total, start, visible)
}

// Start returns the lower bound (inclusive) of the window.
func (w Window) Start() int { return w.start }

// End returns the upper bound (exclusive) of the window.
func (w Window) End() int { return w.end }

// Len returns the number of elements in the window.
func (w Window) Len() int { return w.end - w.start }

// Total returns the total size the window was constructed against.
func (w Window) Total() int { return w.total }

// IsEmpty reports whether the window contains 0 elements.
func (w Window) IsEmpty() bool { return w.start >= w.end }

// Slice extracts the subslice corresponding to this Window from s.
// It is guaranteed by type construction never to panic.
func Slice[T any](s []T, w Window) []T {
	if len(s) == 0 || w.start >= len(s) || w.start >= w.end {
		return nil
	}
	end := min(len(s), w.end)
	return s[w.start:end]
}

// SliceRows is a convenience helper for string slices.
func (w Window) SliceRows(s []string) []string {
	return Slice(s, w)
}

// ScrollPosition maintains a scroll offset with auto-scroll affinity.
type ScrollPosition struct {
	Offset     int
	AutoScroll bool
}

// Layout computes the visible Window for a given total count and available viewport rows,
// updating Offset and AutoScroll internally.
func (sp *ScrollPosition) Layout(total, avail int) Window {
	if total <= 0 || avail <= 0 {
		sp.Offset = 0
		return Window{total: max(0, total)}
	}
	maxScroll := max(0, total-avail)
	if sp.AutoScroll || sp.Offset >= maxScroll {
		sp.Offset = maxScroll
		sp.AutoScroll = true
	} else {
		sp.Offset = tuilib.ClampInt(sp.Offset, 0, maxScroll)
		if sp.Offset >= maxScroll {
			sp.AutoScroll = true
		}
	}
	return NewWindow(total, sp.Offset, avail)
}

// ScrollBy updates Offset by delta and manages AutoScroll affinity.
func (sp *ScrollPosition) ScrollBy(delta, total, avail int) {
	maxScroll := max(0, total-avail)
	next := sp.Offset + delta
	if next >= maxScroll {
		sp.Offset = maxScroll
		sp.AutoScroll = true
	} else {
		sp.Offset = max(0, next)
		sp.AutoScroll = false
	}
}

// ComputeVisibleWindow extracts and clips the visible slice of rows.
func ComputeVisibleWindow(rows []string, prefix []int, scroll, availScroll int, autoScroll bool) ([]string, int, bool) {
	if len(rows) == 0 || availScroll <= 0 {
		return nil, 0, autoScroll
	}
	sp := ScrollPosition{Offset: scroll, AutoScroll: autoScroll}
	win := sp.Layout(len(rows), availScroll)
	visible := append([]string(nil), win.SliceRows(rows)...)
	visible = ClipToScreenRowsPrefix(visible, prefix, win.Start(), availScroll)
	for len(visible) < availScroll {
		visible = append(visible, "")
	}
	return visible, sp.Offset, sp.AutoScroll
}

// FormatVisibleRows attaches sticky/pinned headers and pads rows to effective width.
// agy 1.4.10: sticky header occupies 2 rows (prompt row + separator rule line).
func FormatVisibleRows(visible []string, effective int, pinnedPromptText, stickyBarRow string, altScreen bool) []string {
	if altScreen {
		if pinnedPromptText != "" {
			sticky := []string{tuilib.PadTo(pinnedPromptText, effective), palette.Rule(effective)}
			visible = append(sticky, visible...)
		} else if stickyBarRow != "" {
			sticky := []string{tuilib.PadTo(stickyBarRow, effective), palette.Rule(effective)}
			visible = append(sticky, visible...)
		}
	}
	out := make([]string, len(visible))
	for i, row := range visible {
		out[i] = tuilib.PadTo(row, effective)
	}
	return out
}

// CapAtHeldLines caps the scrollback split point so that heldLines at the end of the buffer
// stay in the live viewport.
//
// In agy 1.4.12: incomplete markdown table rows stay in the live view until the table is
// complete, so they reach scrollback with their final column widths.
func CapAtHeldLines(splitPoint, totalLines, heldLines int) int {
	if heldLines <= 0 {
		return splitPoint
	}
	maxSplit := totalLines - heldLines
	if maxSplit < 0 {
		maxSplit = 0
	}
	if splitPoint > maxSplit {
		return maxSplit
	}
	return splitPoint
}

// IncompleteTableLines returns the number of trailing rows that belong to an
// unfinished markdown table. If the trailing rows do not contain an unclosed table,
// it returns 0.
//
// A table is considered unfinished if its trailing rows contain table delimiters ('|')
// and have not been followed by a blank line or non-table block terminator.
func IncompleteTableLines(rows []string) int {
	if len(rows) == 0 {
		return 0
	}
	count := 0
	for i := len(rows) - 1; i >= 0; i-- {
		line := strings.TrimSpace(rows[i])
		if line == "" {
			// A blank line closes an open table.
			break
		}
		if strings.ContainsRune(line, '|') {
			count++
		} else {
			// A non-blank line without '|' means rows above are either not a table
			// or this line broke out of it.
			break
		}
	}
	return count
}
