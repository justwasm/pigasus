// diff.go — the reference's diff block, computed.
//
// The reference's edit tools report what they changed as a *diff block*: a
// range of the old file and the lines that replace it. The protocol's
// `ActionEditFile.diff_block` carries it, and the tool's own result text
// carries the same diff in unified form.
//
// # The shape
//
// A block is a range of the old file — StartLine is its first line,
// EndLine its exclusive end — and the lines that replace it, each marked
// with what happened to it:
//
//	LINE_ACTION_NONE    the line is on both sides
//	LINE_ACTION_DELETE  the line is only in the old file
//	LINE_ACTION_INSERT  the line is only in the new file
//
// The captured block for a one-word edit in a four-line file (一手 — the
// reference's own frame, cycle 307):
//
//	startLine 0, endLine 4
//	  alpha  LINE_ACTION_NONE
//	  beta   LINE_ACTION_DELETE
//	  BETA   LINE_ACTION_INSERT
//	  gamma  LINE_ACTION_NONE
//	          LINE_ACTION_NONE
//
// The block's lines are the *old* file's lines, with the inserted ones
// interleaved where they belong. EndLine counts the old lines the block
// covers, so the block above covers old lines 0 to 3.
//
// # The context window
//
// The reference's diff widens the change by three unchanged lines on each
// side — its own result text says so: "It shows up to 3 lines of unchanged
// lines before and after the modified lines."
//
// # The diff itself
//
// The diff is github.com/aymanbagabas/go-udiff's — the library
// internal/tuitools and internal/diffaction already diff with. The
// reference computes a real line diff, so a file with several separated
// changes produces several blocks, and the protocol's `diff_block` field
// is a repeated one: it carries them all.
package harnesstools

import (
	"fmt"
	"strings"

	"github.com/aymanbagabas/go-udiff"
)

// diffContextLines is the number of unchanged lines the reference's diff
// keeps on each side of a change.
const diffContextLines = 3

// Diff computes the diff blocks that turn the old file into the new one.
//
// The second result is false when the two files are the same — there is
// no block to report then.
func Diff(old, updated []string) ([]DiffBlock, bool) {
	if equalLines(old, updated) {
		return nil, false
	}
	oldText := linesToText(old)
	newText := linesToText(updated)
	edits := udiff.Lines(oldText, newText)
	if len(edits) == 0 {
		return nil, false
	}
	unified, err := udiff.ToUnifiedDiff("a", "b", oldText, edits, diffContextLines)
	if err != nil || len(unified.Hunks) == 0 {
		return nil, false
	}
	blocks := make([]DiffBlock, 0, len(unified.Hunks))
	for _, hunk := range unified.Hunks {
		blocks = append(blocks, blockFromHunk(hunk))
	}
	return blocks, true
}

// linesToText encodes a line list the way the diff library reads it: one
// line per entry, each terminated by a newline.
//
// The clone's line model is "split the content on \n and keep the empty
// tail", so a file that ends with a newline has one line more than it has
// text lines — and the reference's captured block lists that empty tail
// as a line of its own. Encoding every line with its own newline keeps
// that tail a line instead of dropping it the way a plain text split
// would.
func linesToText(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	var b strings.Builder
	for _, line := range lines {
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

// blockFromHunk turns one hunk of the unified diff into the reference's
// diff block: the range of the old file the hunk covers, and the old
// lines with the inserted ones in place.
func blockFromHunk(hunk *udiff.Hunk) DiffBlock {
	block := DiffBlock{StartLine: hunk.FromLine - 1}
	for _, l := range hunk.Lines {
		text := strings.TrimRight(l.Content, "\r\n")
		switch l.Kind {
		case udiff.Delete:
			block.Lines = append(block.Lines, DiffLine{Text: text, Action: LineDelete})
		case udiff.Insert:
			block.Lines = append(block.Lines, DiffLine{Text: text, Action: LineInsert})
		default:
			block.Lines = append(block.Lines, DiffLine{Text: text, Action: LineNone})
		}
	}
	block.EndLine = block.StartLine + block.oldCount()
	return block
}

// Unified renders the block as a unified-diff hunk.
//
// The reference's result text carries the hunk between its own markers:
//
//	@@ -1,4 +1,4 @@
//	 alpha
//	-beta
//	+BETA
//	 gamma
//
// The header counts the old and the new lines the hunk covers, and the
// lines carry the marker their action implies: a space, a minus or a plus.
func (b DiffBlock) Unified() string {
	return unifiedDiff([]DiffBlock{b})
}

// unifiedDiff renders the blocks as a unified diff — one hunk per block.
//
// The header counts the old and the new lines the hunk covers. The new
// side's start drifts from the old side's as the blocks before it added
// or removed lines, so the drift is accumulated.
func unifiedDiff(blocks []DiffBlock) string {
	var out strings.Builder
	drift := 0
	for _, b := range blocks {
		oldCount, newCount := b.counts()
		fmt.Fprintf(&out, "@@ -%d,%d +%d,%d @@\n",
			b.StartLine+1, oldCount, b.StartLine+1+drift, newCount)
		for _, line := range b.Lines {
			switch line.Action {
			case LineDelete:
				out.WriteString("-")
			case LineInsert:
				out.WriteString("+")
			default:
				out.WriteString(" ")
			}
			out.WriteString(line.Text)
			out.WriteString("\n")
		}
		drift += newCount - oldCount
	}
	return out.String()
}

// counts returns how many old and how many new lines the block covers.
func (b DiffBlock) counts() (oldCount, newCount int) {
	for _, line := range b.Lines {
		switch line.Action {
		case LineDelete:
			oldCount++
		case LineInsert:
			newCount++
		default:
			oldCount++
			newCount++
		}
	}
	return oldCount, newCount
}

// oldCount returns how many old lines the block covers.
func (b DiffBlock) oldCount() int {
	oldCount, _ := b.counts()
	return oldCount
}

// equalLines reports whether two line lists are the same.
func equalLines(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
