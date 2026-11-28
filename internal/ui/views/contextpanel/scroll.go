// scroll.go — the Context view's scroll bound, in one place.
//
// # What it is
//
// The panel used to carry two answers to "how far may the body scroll":
// the render reserved a body budget of its own and the key handler
// clamped against `total-10`, so a body shorter than ten rows could not
// scroll at all and a long one stopped before its own tail. This file is
// the single answer, and both the render and the keys read it.
//
// # What it is not
//
// It is not a window arithmetic: internal/ui/foundations/viewer owns that,
// and the three viewer panels use it. The Context view's document has its
// own shape — a pinned head, a pinned legend and a pager row — which is
// why it clips rather than windows. The bound is still one number, and it
// is this one.
package contextpanel

// linesChromeRows is the number of rows the window adds around the body:
// the pager and the blank row above the pinned footer.
const linesChromeRows = 2

// pinnedTailRows is the number of rows every layout ends with: the
// "Related:" hint and the scroll legend. They stay put while the body
// scrolls, which is what makes them a legend.
const pinnedTailRows = 2

// pinnedRows returns how many document rows the window keeps out of the
// scroll: the head above the body and the footer below it.
//
// The head is the title row: the panel has one view and no bar above it,
// which is the reference's own layout.
func pinnedRows(st State) (headLen, tailLen int) {
	headLen = 1 // the title row
	return headLen, pinnedTailRows
}

// bodyRows returns how many body rows a window of MaxRows can show.
func bodyRows(st State) int {
	if st.MaxRows <= 0 {
		return 0
	}
	head, tail := pinnedRows(st)
	avail := st.MaxRows - (head + tail + linesChromeRows)
	if avail < 1 {
		avail = 1
	}
	return avail
}

// bodyLen returns how many document rows the body has.
func bodyLen(st State) int {
	head, tail := pinnedRows(st)
	n := len(AllLines(st)) - head - tail
	if n < 0 {
		return 0
	}
	return n
}

// MaxContextScroll returns how far the Context view's body may scroll.
//
// The bound is the body's length minus the body rows the window can show.
// A document that fits answers zero, which is what makes the up/down keys
// a no-op on a short panel instead of a jump to a phantom offset.
func MaxContextScroll(st State) int {
	if st.MaxRows <= 0 {
		return 0
	}
	all := len(AllLines(st))
	if all <= st.MaxRows {
		return 0
	}
	n := bodyLen(st) - bodyRows(st)
	if n < 0 {
		return 0
	}
	return n
}
