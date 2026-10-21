// Package canvas adds optional highlight regions to chat-transcript
// lines by routing the rendering through vito/midterm's Canvas.
//
// Cycle vito-canvas: the wagyu clone renders chat transcript bodies
// as raw ANSI strings (cmd/wagyu/conversation.go:renderMarkdown). The
// AGY 1.4.2 reference binary uses vito/midterm's Canvas for the same
// surface (verified: 175 vito/midterm symbols in `funcs.txt`,
// including Canvas.Paint, Canvas.Regions, Canvas.Insert), which lets
// the reference mix *partial repaints* and *search highlights* without
// rebuilding the line. This package layers a tiny, transcript-friendly
// surface over vito/midterm so that:
//
//   - existing call sites keep their current behaviour (no highlights
//     ⇒ line content passes through unchanged, but Render still emits
//     a trailing SGR reset to match what Canvas emits);
//   - future chat-search cycles can thread a []Range slice through
//     the TUI without rewriting the renderer.
//
// The Canvas is cell-indexed, not byte-indexed, so we measure display
// width with go-runewidth (already a direct dep; CJK / emoji width
// = 2). Highlights are clamped to [0, len(content)]; overlapping
// ranges collapse by virtue of Canvas.Paint's neighbour-coalescing
// (Format equality is field-wise so Bold+Reverse applied twice over
// the same cells merges into a single region).
//
// Public API:
//
//   - Line{Content, Highlights}.Render() — default highlight style
//   - Line.RenderWithStyle(midterm.Format) — caller-chosen style
//   - Range{Start, End} — half-open [start,end) byte ranges
//
// Both Render methods are nil-safe (empty Highlights, empty Content,
// zero-width ranges — all covered).
package canvas

import (
	"strings"

	"github.com/mattn/go-runewidth"

	"github.com/vito/midterm"
)

// Range is a half-open byte range [Start, End) inside Line.Content.
// Render clamps Start to 0 and End to len(Content); a Range whose
// clamped form is empty is silently skipped.
type Range struct {
	Start, End int
}

// Line is one transcript line plus optional highlight ranges. An
// empty Highlights slice produces the no-highlight path (the line
// content passes through unchanged modulo the trailing SGR reset
// that Canvas emits — see Render).
type Line struct {
	Content    string
	Highlights []Range
}

// defaultHighlight is what Render() applies to highlighted cells:
// bold + reverse. Reverse is what /search highlights conventionally
// use in the reference binary; bold stacks on top so single-attribute
// terminals still show a distinguishable cell.
var defaultHighlight = midterm.Format{
	Properties: midterm.BoldBit | midterm.ReverseBit,
}

// Render returns the ANSI-coded line, applying the default highlight
// style (bold + reverse) to any highlighted cells. Empty Highlights
// ⇒ the content passes through unchanged modulo a trailing SGR
// reset; empty Content ⇒ "".
func (l Line) Render() string {
	return l.RenderWithStyle(defaultHighlight)
}

// RenderWithStyle returns the ANSI-coded line, applying `style` to
// cells that fall inside any Highlight range. Empty Content ⇒ "".
// Empty Highlights ⇒ content passes through (still with the trailing
// SGR reset that Canvas emits on every rendered row).
func (l Line) RenderWithStyle(style midterm.Format) string {
	if l.Content == "" {
		return ""
	}
	runes := []rune(l.Content)
	if len(runes) == 0 {
		return ""
	}

	// Cell positions are not a 1:1 with rune indices (CJK runes
	// span 2 cells). Pre-compute cellWidths (per-rune cell width),
	// runeWidth (total cells), and cellToRune (cell → rune index,
	// so we can slice contentRunes by cell boundary).
	cellWidths := make([]int, len(runes))
	width := 0
	for i, r := range runes {
		w := runewidth.RuneWidth(r)
		if w < 0 {
			w = 0
		}
		cellWidths[i] = w
		width += w
	}
	if width == 0 {
		return ""
	}
	cellToRune := make([]int, width+1)
	cc := 0
	for ri, w := range cellWidths {
		cellToRune[cc] = ri
		cc += w
	}
	cellToRune[width] = len(runes)

	// Convert Highlights (byte ranges) into cell ranges so we can
	// walk cells in order and Paint the right ones. We don't paint
	// contiguous ranges in bulk: Paint's neighbour-coalescing does
	// the merging for us and produces a clean Canvas.Regions(0).
	cellHighlights := byteHighlightsToCell(l.Content, cellWidths, l.Highlights)

	// Bootstrap the Canvas WITHOUT pre-sizing it. We let Paint
	// create the row on first call (Canvas defaults to Width=0,
	// which means each Paint is a single-cell operation; the
	// resulting row is rebuilt each time Paint sees a new cell).
	// To get a single contiguous EmptyFormat region, we instead
	// size the canvas to width and paint just the trailing
	// headroom cell, which forces a single row of size=width.
	c := &midterm.Canvas{Width: width}
	// Touch col=0 once with EmptyFormat so Region(0) yields exactly
	// one "padding then highlight" pattern. We paint the
	// highlighted cells next; their same-format grow path will
	// extend the bootstrap region by the trailing width.
	c.Paint(0, 0, midterm.EmptyFormat)

	for col := 0; col < width; col++ {
		if col < len(cellHighlights) && cellHighlights[col] != 0 {
			c.Paint(0, col, style)
		}
	}

	// Walk Regions(0), emit format codes interleaved with the
	// content slice. The trailing SGR reset is required so any
	// styling we set doesn't bleed into whatever the caller
	// appends next on the same line.
	var sb strings.Builder
	sb.Grow(len(l.Content) + 32)
	pos := 0
	lastFmt := midterm.Format{}
	for region := range c.Regions(0) {
		if pos >= width {
			break
		}
		end := pos + region.Size
		if end > width {
			end = width
		}
		if end <= pos {
			pos = end
			continue
		}
		if region.F != lastFmt {
			sb.WriteString(region.F.RenderFgBg(nil, nil))
			lastFmt = region.F
		}
		// cellToRune[pos]/[end] give the rune indices that span the
		// cell range [pos, end).
		sb.WriteString(string(runes[cellToRune[pos]:cellToRune[end]]))
		pos = end
	}
	sb.WriteString("\x1b[0m")
	return sb.String()
}

// byteHighlightsToCell converts a list of byte-half-open highlight
// ranges over `content` into a per-cell boolean slice, the slice
// length being the line's total display width. Byte ranges are
// clamped to [0, len(content)]; zero-width or fully-outside ranges
// are dropped. cellWidths[i] is the cell width of the i-th rune.
func byteHighlightsToCell(content string, cellWidths []int, ranges []Range) []int {
	if len(ranges) == 0 {
		return nil
	}
	width := 0
	for _, w := range cellWidths {
		width += w
	}
	if width == 0 {
		return nil
	}

	// Cumulative cell position per rune, plus a sentry at len(runes).
	runes := []rune(content)
	runeToCell := make([]int, len(runes)+1)
	cc := 0
	for i, w := range cellWidths {
		runeToCell[i] = cc
		cc += w
	}
	runeToCell[len(runes)] = width

	out := make([]int, width)
	for _, r := range ranges {
		if r.End <= r.Start {
			continue
		}
		bStart := r.Start
		if bStart < 0 {
			bStart = 0
		}
		bEnd := r.End
		if bEnd > len(content) {
			bEnd = len(content)
		}
		if bEnd <= bStart {
			continue
		}

		// Find the first rune whose starting byte offset is >= bStart;
		// the highlight begins at this rune's cell position.
		startRune := -1
		bytePos := 0
		for i, r := range runes {
			if bytePos >= bStart {
				startRune = i
				break
			}
			bytePos += utf8Len(r)
		}
		if startRune < 0 {
			startRune = len(runes)
		}

		// Find one past the last rune whose starting byte offset is < bEnd;
		// the highlight ends at the end of that rune's cells.
		endRune := -1
		bytePos = 0
		for i, r := range runes {
			if bytePos >= bEnd {
				break
			}
			endRune = i + 1
			bytePos += utf8Len(r)
		}
		if endRune < 0 || endRune <= startRune {
			continue
		}

		cStart := runeToCell[startRune]
		cEnd := runeToCell[endRune]
		if cEnd <= cStart {
			continue
		}
		if cStart >= width {
			continue
		}
		if cEnd > width {
			cEnd = width
		}
		for cc := cStart; cc < cEnd; cc++ {
			out[cc] = 1
		}
	}
	return out
}

// utf8Len returns the byte length of the UTF-8 encoding of r. Kept
// local to avoid pulling encoding/utf8 for one helper; runewidth
// is already a direct dep.
func utf8Len(r rune) int {
	switch {
	case r < 0x80:
		return 1
	case r < 0x800:
		return 2
	case r < 0x10000:
		return 3
	default:
		return 4
	}
}
