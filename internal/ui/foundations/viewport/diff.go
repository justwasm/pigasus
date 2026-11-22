// diff.go — terminal row-level dirty mask zero-copy diff rendering engine.
//
// In high-frequency terminal rendering (e.g. LLM token streaming, status bar ticks,
// or subagent activity monitors), redrawing the entire screen creates terminal
// flicker, high CPU usage, and bandwidth bloat over PTY/SSH connections.
//
// LineHashGrid and LineDiffEngine provide:
// 1. Zero-copy / low-allocation row hash calculation (FNV-1a 64-bit).
// 2. Fine-grained row-level Dirty Mask tracking.
// 3. Selective ANSI cursor positioning (CUP: \x1b[<row>;<col>H) and Line Clear (\x1b[K)
//    so only dirty lines generate terminal escape sequences.
// 4. Instant fast-path return when 0 lines changed.

package viewport

import (
	"bytes"
	"strconv"
)

// FNV-1a 64-bit constants for line hashing.
const (
	fnvOffset64 = 14695981039346656037
	fnvPrime64  = 1099511628211
)

// HashLine computes a fast 64-bit FNV-1a hash of a single text line.
func HashLine(s string) uint64 {
	var h uint64 = fnvOffset64
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= fnvPrime64
	}
	return h
}

// HashLineBytes computes a fast 64-bit FNV-1a hash of a byte slice without string allocation.
func HashLineBytes(b []byte) uint64 {
	var h uint64 = fnvOffset64
	for i := 0; i < len(b); i++ {
		h ^= uint64(b[i])
		h *= fnvPrime64
	}
	return h
}

// LineHashGrid maintains the cached row hashes and dirty mask flags for a viewport grid.
type LineHashGrid struct {
	Width      int
	Height     int
	hashes     []uint64
	dirty      []bool
	dirtyCount int
}

// NewLineHashGrid initializes a new LineHashGrid for a given viewport dimension.
func NewLineHashGrid(width, height int) *LineHashGrid {
	if height < 0 {
		height = 0
	}
	if width < 0 {
		width = 0
	}
	g := &LineHashGrid{
		Width:  width,
		Height: height,
		hashes: make([]uint64, height),
		dirty:  make([]bool, height),
	}
	// Initial state: all lines dirty so initial frame renders fully.
	g.MarkAllDirty()
	return g
}

// SetLine sets the hash for row, updating dirty status if the hash changed.
func (g *LineHashGrid) SetLine(row int, text string) bool {
	if row < 0 || row >= len(g.hashes) {
		return false
	}
	h := HashLine(text)
	if g.hashes[row] != h {
		g.hashes[row] = h
		if !g.dirty[row] {
			g.dirty[row] = true
			g.dirtyCount++
		}
		return true
	}
	return false
}

// SetLineBytes sets the hash for row from a byte slice with zero allocation.
func (g *LineHashGrid) SetLineBytes(row int, b []byte) bool {
	if row < 0 || row >= len(g.hashes) {
		return false
	}
	h := HashLineBytes(b)
	if g.hashes[row] != h {
		g.hashes[row] = h
		if !g.dirty[row] {
			g.dirty[row] = true
			g.dirtyCount++
		}
		return true
	}
	return false
}

// IsDirty reports whether row is marked dirty.
func (g *LineHashGrid) IsDirty(row int) bool {
	if row < 0 || row >= len(g.dirty) {
		return false
	}
	return g.dirty[row]
}

// MarkDirty marks a specific row as dirty.
func (g *LineHashGrid) MarkDirty(row int) {
	if row >= 0 && row < len(g.dirty) && !g.dirty[row] {
		g.dirty[row] = true
		g.dirtyCount++
	}
}

// MarkAllDirty marks all rows as dirty (e.g. for a forced redraw).
func (g *LineHashGrid) MarkAllDirty() {
	for i := range g.dirty {
		g.dirty[i] = true
	}
	g.dirtyCount = len(g.dirty)
}

// ClearDirty resets the dirty mask for all rows.
func (g *LineHashGrid) ClearDirty() {
	for i := range g.dirty {
		g.dirty[i] = false
	}
	g.dirtyCount = 0
}

// DirtyCount returns the number of currently dirty rows.
func (g *LineHashGrid) DirtyCount() int {
	return g.dirtyCount
}

// DirtyIndices returns a slice of row indices currently marked dirty.
func (g *LineHashGrid) DirtyIndices() []int {
	if g.dirtyCount == 0 {
		return nil
	}
	out := make([]int, 0, g.dirtyCount)
	for i, d := range g.dirty {
		if d {
			out = append(out, i)
		}
	}
	return out
}

// RowHash returns the cached hash for row.
func (g *LineHashGrid) RowHash(row int) uint64 {
	if row < 0 || row >= len(g.hashes) {
		return 0
	}
	return g.hashes[row]
}

// Resize updates the dimensions and re-allocates row buffers if needed.
func (g *LineHashGrid) Resize(width, height int) {
	if height < 0 {
		height = 0
	}
	if width < 0 {
		width = 0
	}
	g.Width = width
	if height == g.Height {
		return
	}
	newHashes := make([]uint64, height)
	newDirty := make([]bool, height)
	copy(newHashes, g.hashes)
	for i := range newDirty {
		newDirty[i] = true // New or shifted lines start dirty
	}
	g.Height = height
	g.hashes = newHashes
	g.dirty = newDirty
	g.dirtyCount = height
}

// Reset clears all hashes and marks all lines dirty.
func (g *LineHashGrid) Reset() {
	for i := range g.hashes {
		g.hashes[i] = 0
		g.dirty[i] = true
	}
	g.dirtyCount = len(g.dirty)
}

// LineDiffResult describes the outcome of a frame diff operation.
type LineDiffResult struct {
	Changed      bool
	DirtyCount   int
	DirtyIndices []int
	TotalRows    int
}

// LineDiffEngine coordinates dirty mask diffing and ANSI escape sequence generation.
type LineDiffEngine struct {
	grid      *LineHashGrid
	prevLines []string
	originRow int  // 1-based top row offset for ANSI CUP (default 1)
	originCol int  // 1-based left column offset for ANSI CUP (default 1)
	clearEOL  bool // whether to append \x1b[K to clear line tail
	buf       bytes.Buffer
}

// NewLineDiffEngine creates a diff engine for the given dimensions.
func NewLineDiffEngine(width, height int) *LineDiffEngine {
	return &LineDiffEngine{
		grid:      NewLineHashGrid(width, height),
		originRow: 1,
		originCol: 1,
		clearEOL:  true,
	}
}

// SetOrigin configures the 1-based terminal origin coordinate (CUP offset).
func (e *LineDiffEngine) SetOrigin(row, col int) {
	if row < 1 {
		row = 1
	}
	if col < 1 {
		col = 1
	}
	e.originRow = row
	e.originCol = col
}

// SetClearEOL configures whether Erase-in-Line (\x1b[K) is appended to updated lines.
func (e *LineDiffEngine) SetClearEOL(enabled bool) {
	e.clearEOL = enabled
}

// Grid returns the underlying LineHashGrid.
func (e *LineDiffEngine) Grid() *LineHashGrid {
	return e.grid
}

// Diff compares newLines with the previous frame, updating the dirty mask.
func (e *LineDiffEngine) Diff(newLines []string) LineDiffResult {
	n := len(newLines)
	if n != e.grid.Height {
		e.grid.Resize(e.grid.Width, n)
	}

	e.grid.ClearDirty()

	for i := 0; i < n; i++ {
		h := HashLine(newLines[i])
		if i >= len(e.prevLines) || h != e.grid.hashes[i] || newLines[i] != e.prevLines[i] {
			e.grid.hashes[i] = h
			e.grid.dirty[i] = true
			e.grid.dirtyCount++
		}
	}

	return LineDiffResult{
		Changed:      e.grid.dirtyCount > 0,
		DirtyCount:   e.grid.dirtyCount,
		DirtyIndices: e.grid.DirtyIndices(),
		TotalRows:    n,
	}
}

// RenderDiff diffs newLines against the previous frame and returns the minimal ANSI
// sequence that updates ONLY the changed rows. If nothing changed, returns "".
func (e *LineDiffEngine) RenderDiff(newLines []string) string {
	b := e.RenderDiffBytes(newLines)
	if len(b) == 0 {
		return ""
	}
	return string(b)
}

// RenderDiffBytes performs diffing and returns ANSI escape bytes for dirty rows.
// Returns nil if no rows changed.
func (e *LineDiffEngine) RenderDiffBytes(newLines []string) []byte {
	res := e.Diff(newLines)
	if !res.Changed {
		return nil
	}

	e.buf.Reset()
	// Pre-size buffer estimate: ~32 bytes overhead per dirty line plus line length
	e.buf.Grow(res.DirtyCount * 48)

	for _, row := range res.DirtyIndices {
		termRow := e.originRow + row
		termCol := e.originCol

		// Emit ANSI Cursor Position (CUP): \x1b[<row>;<col>H
		e.buf.WriteString("\x1b[")
		e.buf.Write(strconv.AppendInt(nil, int64(termRow), 10))
		e.buf.WriteByte(';')
		e.buf.Write(strconv.AppendInt(nil, int64(termCol), 10))
		e.buf.WriteByte('H')

		// Emit line content
		e.buf.WriteString(newLines[row])

		// Emit Erase in Line (\x1b[K) to wipe trailing characters from previous wider content
		if e.clearEOL {
			e.buf.WriteString("\x1b[K")
		}
	}

	// Update cached previous lines
	if cap(e.prevLines) < len(newLines) {
		e.prevLines = make([]string, len(newLines))
	} else {
		e.prevLines = e.prevLines[:len(newLines)]
	}
	copy(e.prevLines, newLines)

	return e.buf.Bytes()
}

// ForceRedraw marks all rows dirty so the next RenderDiff will output the complete frame.
func (e *LineDiffEngine) ForceRedraw() {
	e.grid.MarkAllDirty()
	e.prevLines = nil
}

// Reset clears all previous line cache and marks the engine dirty.
func (e *LineDiffEngine) Reset() {
	e.grid.Reset()
	e.prevLines = nil
	e.buf.Reset()
}
