package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"
)

func isWriteTool(name string) bool {
	switch name {
	case "write_to_file", "write_file", "create_file":
		return true
	}
	return false
}

// isEditTool reports whether name is an in-place file-edit tool.
func isEditTool(name string) bool {
	switch name {
	case "replace_file_content", "edit_file", "multi_replace_file_content", "sed_file":
		return true
	}
	return false
}

// editDiff is the transcript body of an edit_file step on agy 1.4.3: a
// "+A / -B lines" header followed by a unified hunk with three lines of
// context. Changed rows carry the base3 (#EEE8D5) stripe; context rows do
// not. Measured raw:
//
//	ﾠ⎿ﾠﾠ +1 / -1 lines
//	     1    alpha
//	     3 -  gamma
//	     3 +  GAMMA
//	     4    delta
type editDiff struct {
	path           string
	added, removed int
	rows           []editDiffRow
}

type editDiffRow struct {
	kind byte // ' ', '-', '+'
	num  int
	text string
}

// editDiffFor recovers the old and new file text from the tool arguments and
// the file that is now on disk, then builds the hunk.
func editDiffFor(args json.RawMessage) *editDiff {
	path := argStr(args, "TargetFile", "Path", "AbsolutePath", "path", "file_path", "target_file", "file")
	target := argStr(args, "Target", "TargetContent", "oldString", "old_string", "target")
	replacement := argStr(args, "Replacement", "ReplacementContent", "newString", "new_string", "replacement")
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
	d := buildEditDiff(oldText, newText)
	if d != nil {
		d.path = path
	}
	return d
}

// createDiffFor builds the addition-only diff for a file creation tool.
// Matches AGY 1.4.1 createFilePreview / createFileDiffEntry.
func createDiffFor(args json.RawMessage) *editDiff {
	path := argStr(args, "TargetFile", "Path", "AbsolutePath", "path", "file_path", "target_file", "file")
	content := argStr(args, "content", "Content")
	if content == "" {
		return nil
	}
	d := buildEditDiff("", content)
	if d != nil {
		d.path = path
	}
	return d
}

// buildEditDiff trims the common prefix/suffix and keeps three context lines
// around the change — the reference's exact hunk for a single contiguous
// edit. The trailing empty string of a newline-terminated file is a real
// line to the reference (it renders as line N+1), so the split is kept as is.
func buildEditDiff(oldText, newText string) *editDiff {
	oldLines := strings.Split(oldText, "\n")
	newLines := strings.Split(newText, "\n")
	p := 0
	for p < len(oldLines) && p < len(newLines) && oldLines[p] == newLines[p] {
		p++
	}
	suf := 0
	for suf < len(oldLines)-p && suf < len(newLines)-p &&
		oldLines[len(oldLines)-1-suf] == newLines[len(newLines)-1-suf] {
		suf++
	}
	oldEnd := len(oldLines) - suf
	newEnd := len(newLines) - suf
	if p == oldEnd && p == newEnd {
		return nil
	}
	d := &editDiff{added: newEnd - p, removed: oldEnd - p}
	const ctx = 3
	start := p - ctx
	if start < 0 {
		start = 0
	}
	for i := start; i < p; i++ {
		d.rows = append(d.rows, editDiffRow{' ', i + 1, oldLines[i]})
	}
	for i := p; i < oldEnd; i++ {
		d.rows = append(d.rows, editDiffRow{'-', i + 1, oldLines[i]})
	}
	for i := p; i < newEnd; i++ {
		d.rows = append(d.rows, editDiffRow{'+', i + 1, newLines[i]})
	}
	for i := 0; i < ctx && newEnd+i < len(newLines); i++ {
		d.rows = append(d.rows, editDiffRow{' ', newEnd + i + 1, newLines[newEnd+i]})
	}
	return d
}

// lines renders the hunk. The row prefix is five spaces (the reference moves
// to column 6 before the four-character line number), and the stripe on a
// changed row runs to the right edge, exactly like the reference's \x1b[K
// with the base3 background still active.
func renderDiffRowWithPath(r editDiffRow, path string, pad func(string) string) string {
	styledText := r.text
	if tokens := tokenizeDiffLine(path, " "+r.text); len(tokens) > 1 {
		styledText = renderDiffInlineTokenStream(tokens[1:])
	}
	gutter := "     " + cDiffNumOpen + fmt.Sprintf("%4d", r.num) + "\x1b[m"
	switch r.kind {
	case '-':
		return gutter + cDiffRemoveTintOpen + cToolRemoveOpen + " -  " + styledText + pad(r.text) + "\x1b[m"
	case '+':
		return gutter + cDiffAddTintOpen + cToolAddOpen + " +  " + styledText + pad(r.text) + "\x1b[m"
	default:
		return gutter + "    " + styledText
	}
}

func renderDiffRowFallback(r editDiffRow, pad func(string) string) string {
	switch r.kind {
	case '-':
		return "     " + cDiffStripeOpen + fmt.Sprintf("%4d", r.num) +
			cToolRemoveOpen + " -  " + r.text + pad(r.text) + "\x1b[m"
	case '+':
		return "     " + cDiffStripeOpen + fmt.Sprintf("%4d", r.num) +
			cToolAddOpen + " +  " + r.text + pad(r.text) + "\x1b[m"
	default:
		return "     " + cDiffNumOpen + fmt.Sprintf("%4d", r.num) + "\x1b[m    " + r.text
	}
}

func (d *editDiff) lines(width int) []string {
	head := detailBulletOpen + cToolAddOpen + fmt.Sprintf("+%d", d.added) + "\x1b[m / " +
		cToolRemoveOpen + fmt.Sprintf("-%d", d.removed) + "\x1b[m lines"
	out := []string{head}
	pad := func(text string) string {
		if width <= 0 {
			return ""
		}
		n := width - 13 - utf8.RuneCountInString(text)
		if n <= 0 {
			return ""
		}
		return strings.Repeat(" ", n)
	}
	for _, r := range d.rows {
		if d.path != "" {
			out = append(out, renderDiffRowWithPath(r, d.path, pad))
		} else {
			out = append(out, renderDiffRowFallback(r, pad))
		}
	}
	if n := len(d.rows); n > 0 {
		if k := d.rows[n-1].kind; k == '+' || k == '-' {
			out = append(out, "    ")
		}
	}
	return out
}


