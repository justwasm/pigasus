package tuitools

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/aymanbagabas/go-udiff"
	"github.com/justwasm/pigasus/diffsyntax"
	"github.com/justwasm/pigasus/internal/diffrow"
	"github.com/justwasm/pigasus/palette"
)

// IsWriteTool reports whether name is a file-creation/overwrite tool.
func IsWriteTool(name string) bool {
	switch name {
	case "write_to_file", "write_file", "create_file":
		return true
	}
	return false
}

// IsEditTool reports whether name is an in-place file-edit tool.
func IsEditTool(name string) bool {
	switch name {
	case "replace_file_content", "edit_file", "multi_replace_file_content", "sed_file":
		return true
	}
	return false
}

// EditDiff is the transcript body of an edit/create step.
type EditDiff struct {
	Path           string
	Added, Removed int
	Rows           []EditDiffRow
	// Hunks groups the rows into the changes they came from. The
	// reference prints one summary per change, so the rows have to be
	// grouped back into the changes they came from.
	//
	// 【反编译】 `render.codeActionDetail` walks a list of changes — each
	// one a start line, an end line and the number of added and of
	// removed lines — and prints a summary for each of them before that
	// change's rows.
	Hunks []EditHunk
}

// EditHunk is one change of an edit: the rows it touched and how many
// lines it added and removed.
type EditHunk struct {
	Added, Removed int
	Rows           []EditDiffRow
}

// EditDiffRow is one row in a diff hunk.
type EditDiffRow struct {
	Kind byte
	Num  int
	Text string
	// Hunk is the index of the hunk the row belongs to. The reference
	// prints one summary per change, so the rows have to be grouped
	// back into the changes they came from.
	//
	// 【反编译】 the reference's `render.codeActionDetail` walks a list
	// of changes — each one a start line, an end line and the number of
	// added and of removed lines — and prints a summary for each of
	// them before that change's rows.
	Hunk int
}

// EditDiffFor recovers old and new file text and builds the hunk.
func EditDiffFor(args json.RawMessage) *EditDiff {
	path := ArgStr(args, "TargetFile", "Path", "AbsolutePath", "path", "file_path", "target_file", "file")
	target := ArgStr(args, "Target", "TargetContent", "oldString", "old_string", "target")
	replacement := ArgStr(args, "Replacement", "ReplacementContent", "newString", "new_string", "replacement")
	if target == "" && replacement == "" {
		return nil
	}
	oldText, newText := target, replacement
	if path != "" {
		if b, err := os.ReadFile(path); err == nil {
			cur := string(b)
			switch {
			case replacement != "" && strings.Contains(cur, replacement):
				newText = cur
				oldText = strings.Replace(cur, replacement, target, 1)
			case strings.Contains(cur, target):
				oldText = cur
				newText = strings.Replace(cur, target, replacement, 1)
			}
		}
	}
	d := BuildEditDiff(oldText, newText)
	if d != nil {
		d.Path = path
	}
	return d
}

// CreateDiffFor builds the addition-only diff for a file creation tool.
func CreateDiffFor(args json.RawMessage) *EditDiff {
	path := ArgStr(args, "TargetFile", "Path", "AbsolutePath", "path", "file_path", "target_file", "file")
	content := ArgStr(args, "content", "Content")
	if content == "" {
		return nil
	}
	d := BuildEditDiff("", content)
	if d != nil {
		d.Path = path
	}
	return d
}

// BuildEditDiff generates diff hunks with 3 context lines using go-udiff.
func BuildEditDiff(oldText, newText string) *EditDiff {
	edits := udiff.Lines(oldText, newText)
	if len(edits) == 0 {
		return nil
	}
	u, err := udiff.ToUnifiedDiff("a", "b", oldText, edits, udiff.DefaultContextLines)
	if err != nil || len(u.Hunks) == 0 {
		return nil
	}
	d := &EditDiff{}
	for _, h := range u.Hunks {
		appendHunkRows(d, h, oldText, newText)
	}
	if len(d.Rows) == 0 || (d.Added == 0 && d.Removed == 0) {
		return nil
	}
	return d
}

func appendHunkRows(d *EditDiff, h *udiff.Hunk, oldText, newText string) {
	oldLine := h.FromLine
	newLine := h.ToLine
	hunk := EditHunk{}
	var lastChangeNew int
	for _, l := range h.Lines {
		text := strings.TrimRight(l.Content, "\r\n")
		switch l.Kind {
		case udiff.Equal:
			hunk.Rows = append(hunk.Rows, EditDiffRow{Kind: ' ', Num: newLine, Text: text})
			oldLine++
			newLine++
		case udiff.Delete:
			hunk.Rows = append(hunk.Rows, EditDiffRow{Kind: '-', Num: oldLine, Text: text})
			oldLine++
			hunk.Removed++
			d.Removed++
		case udiff.Insert:
			hunk.Rows = append(hunk.Rows, EditDiffRow{Kind: '+', Num: newLine, Text: text})
			lastChangeNew = newLine
			newLine++
			hunk.Added++
			d.Added++
		}
	}
	if strings.HasSuffix(oldText, "\n") && strings.HasSuffix(newText, "\n") {
		newLines := strings.Split(newText, "\n")
		totalLines := len(newLines)
		if lastChangeNew > 0 && totalLines-lastChangeNew <= udiff.DefaultContextLines && newLine == totalLines {
			hunk.Rows = append(hunk.Rows, EditDiffRow{Kind: ' ', Num: newLine, Text: ""})
		}
	}
	d.Rows = append(d.Rows, hunk.Rows...)
	d.Hunks = append(d.Hunks, hunk)
}

// Lines renders the hunk rows for the diff. It is the method form of
// RenderDiffLines, for callers that hold the diff itself.
func (d *EditDiff) Lines(width int) []string {
	return RenderDiffLines(d, width)
}

// RenderDiffLines renders the diff body for EditDiff.
//
// 【反编译】 the reference's transcript diff is a list of changes, and
// each change prints a summary of its own before its rows:
//
//	⎿  +1 / -1 lines
//	   3    -  gamma
//	   3    +  GAMMA
//
// The summary is one of three forms — "Added %d line%s",
// "Removed %d line%s" and the "+%d / -%d lines" pair — and the rows are
// the reference's `render.codeActionDetail.func2` rows: a right-aligned
// line number in the gutter, a four-character marker column and the
// text.
//
// The clone's hunk plays the reference's change: a contiguous region
// with its own added and removed counts, and its own summary.
//
// A change prints at most MaxDetailLines rows; the rest are counted in
// the reference's "… and %d more lines" marker.
//
// 【一手】 agy 1.4.3's captured diff (docs/cycles/cycle-285-tool-display/
// deliverable.md) shows the same shape: the number in the gutter is
// gray, an unchanged line's text is plain, a changed line's text is
// green or red, and the changed rows carry the reference's stripe.
//
// 【strings】 the reference's inline diff preview carries "syntax
// highlighting for your color scheme" (the 1.4.1 changelog line,
// internal/cli/testdata/changelog_reference.txt:88), so the rows are
// highlighted when the file's language is known.
func RenderDiffLines(d *EditDiff, width int) []string {
	if d == nil {
		return nil
	}
	style := diffrow.TranscriptStyle()
	layout := diffrow.NewLayout(width-diffIndentWidth, maxLineNum(d))
	var out []string
	lastKind := byte(' ')
	for _, hunk := range d.Hunks {
		out = append(out, palette.DetailBulletOpen+"  \u23bf  "+"\x1b[m"+diffSummary(hunk))
		rows := hunk.Rows
		more := 0
		if len(rows) > MaxDetailLines {
			more = len(rows) - MaxDetailLines
			rows = rows[:MaxDetailLines]
		}
		for _, r := range rows {
			out = append(out, diffIndent+diffrow.RenderStyled(layout, style,
				diffrow.Kind(r.Kind), r.Num, r.Text, highlightDiffText(d.Path, r.Text)))
			if more == 0 {
				lastKind = r.Kind
			}
		}
		if more > 0 {
			out = append(out, diffIndent+diffrow.More(layout.NumWidth, more))
		}
	}
	// A changed row is padded to the content width, so the reference's
	// hint — which the transcript hangs on the last row — needs a row of
	// its own. The capture the clone follows puts the hint on the last
	// *unchanged* row (docs/cycles/cycle-285-tool-display/deliverable.md),
	// where it fits.
	if lastKind == '+' || lastKind == '-' {
		out = append(out, "    ")
	}
	return out
}

// highlightDiffText renders a diff row's text with the file's syntax
// colours. A file whose language the highlighter does not know comes
// back unchanged.
func highlightDiffText(path, text string) string {
	if path == "" {
		return text
	}
	tokens := diffsyntax.TokenizeLine(path, " "+text)
	if len(tokens) <= 1 {
		return text
	}
	return diffsyntax.RenderInlineTokenStream(tokens[1:])
}

// diffIndent is the indent the reference's diff rows carry.
//
// 【一手】 agy 1.4.3's captured diff moves the cursor to column six
// before every row (docs/cycles/cycle-285-tool-display/deliverable.md),
// so the line number's field starts at the same column as the detail
// bullet's text — the sixth column of the terminal.
const diffIndent = "     "

// diffIndentWidth is the width of diffIndent.
const diffIndentWidth = 5

// diffSummary renders the reference's per-change summary.
//
// 【反编译】 `render.codeActionDetail` picks one of three forms:
// "Added %d line%s" (0x1034b43b4), "Removed %d line%s" (0x1034dcd2d)
// and, for a change that both adds and removes, the styled "+%d" and
// "-%d" pair joined by " / " and closed by " lines". The first two
// forms are plain text in the reference; only the pair's counts are
// painted.
func diffSummary(hunk EditHunk) string {
	switch {
	case hunk.Removed == 0:
		return diffrow.Summary(hunk.Added, 0)
	case hunk.Added == 0:
		return diffrow.Summary(0, hunk.Removed)
	}
	return palette.ToolAddOpen + fmt.Sprintf("+%d", hunk.Added) + "\x1b[m / " +
		palette.ToolRemoveOpen + fmt.Sprintf("-%d", hunk.Removed) + "\x1b[m lines"
}

// maxLineNum returns the largest line number the diff prints.
func maxLineNum(d *EditDiff) int {
	max := 0
	for _, r := range d.Rows {
		if r.Num > max {
			max = r.Num
		}
	}
	return max
}
