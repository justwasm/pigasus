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

	cellWidths, width, cellToRune := buildCellMetrics(runes)
	if width == 0 {
		return ""
	}

	cellHighlights := byteHighlightsToCell(l.Content, cellWidths, l.Highlights)
	c := &midterm.Canvas{Width: width}
	c.Paint(0, 0, midterm.EmptyFormat)

	for col := 0; col < width; col++ {
		if col < len(cellHighlights) && cellHighlights[col] != 0 {
			c.Paint(0, col, style)
		}
	}
	return renderRegionsString(c, width, cellToRune, runes, len(l.Content)+32)
}

func buildCellMetrics(runes []rune) ([]int, int, []int) {
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
		return nil, 0, nil
	}
	cellToRune := make([]int, width+1)
	cc := 0
	for ri, w := range cellWidths {
		cellToRune[cc] = ri
		cc += w
	}
	cellToRune[width] = len(runes)
	return cellWidths, width, cellToRune
}

func renderRegionsString(c *midterm.Canvas, width int, cellToRune []int, runes []rune, capHint int) string {
	var sb strings.Builder
	sb.Grow(capHint)
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
		sb.WriteString(string(runes[cellToRune[pos]:cellToRune[end]]))
		pos = end
	}
	sb.WriteString("\x1b[0m")
	return sb.String()
}

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
		bStart, bEnd := clampRange(r.Start, r.End, len(content))
		if bEnd <= bStart {
			continue
		}
		startRune, endRune := findRuneByteRange(runes, bStart, bEnd)
		if endRune <= startRune {
			continue
		}
		markCellRange(runeToCell, startRune, endRune, width, out)
	}
	return out
}

func clampRange(start, end, maxLen int) (int, int) {
	if start < 0 {
		start = 0
	}
	if end > maxLen {
		end = maxLen
	}
	return start, end
}

func findRuneByteRange(runes []rune, bStart, bEnd int) (int, int) {
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
	endRune := -1
	bytePos = 0
	for i, r := range runes {
		if bytePos >= bEnd {
			break
		}
		endRune = i + 1
		bytePos += utf8Len(r)
	}
	return startRune, endRune
}

func markCellRange(runeToCell []int, startRune, endRune, width int, out []int) {
	cStart := runeToCell[startRune]
	cEnd := runeToCell[endRune]
	if cEnd <= cStart || cStart >= width {
		return
	}
	if cEnd > width {
		cEnd = width
	}
	for cc := cStart; cc < cEnd; cc++ {
		out[cc] = 1
	}
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
