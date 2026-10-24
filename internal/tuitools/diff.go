package tuitools

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/aymanbagabas/go-udiff"
	"github.com/justwasm/pigasus/internal/diffsyntax"
	"github.com/justwasm/pigasus/internal/palette"
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
}

// EditDiffRow is one row in a diff hunk.
type EditDiffRow struct {
	Kind byte
	Num  int
	Text string
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
	var lastChangeNew int
	for _, l := range h.Lines {
		text := strings.TrimRight(l.Content, "\r\n")
		switch l.Kind {
		case udiff.Equal:
			d.Rows = append(d.Rows, EditDiffRow{Kind: ' ', Num: newLine, Text: text})
			oldLine++
			newLine++
		case udiff.Delete:
			d.Rows = append(d.Rows, EditDiffRow{Kind: '-', Num: oldLine, Text: text})
			oldLine++
			d.Removed++
		case udiff.Insert:
			d.Rows = append(d.Rows, EditDiffRow{Kind: '+', Num: newLine, Text: text})
			lastChangeNew = newLine
			newLine++
			d.Added++
		}
	}
	if strings.HasSuffix(oldText, "\n") && strings.HasSuffix(newText, "\n") {
		newLines := strings.Split(newText, "\n")
		totalLines := len(newLines)
		if lastChangeNew > 0 && totalLines-lastChangeNew <= udiff.DefaultContextLines && newLine == totalLines {
			d.Rows = append(d.Rows, EditDiffRow{Kind: ' ', Num: newLine, Text: ""})
		}
	}
}

func renderDiffRowWithPath(r EditDiffRow, path string, pad func(string) string) string {
	styledText := r.Text
	if tokens := diffsyntax.TokenizeLine(path, " "+r.Text); len(tokens) > 1 {
		styledText = diffsyntax.RenderInlineTokenStream(tokens[1:])
	}
	gutter := "     " + palette.DiffNumOpen + fmt.Sprintf("%4d", r.Num) + "\x1b[m"
	switch r.Kind {
	case '-':
		return gutter + palette.DiffRemoveTintOpen + palette.ToolRemoveOpen + " -  " + styledText + pad(r.Text) + "\x1b[m"
	case '+':
		return gutter + palette.DiffAddTintOpen + palette.ToolAddOpen + " +  " + styledText + pad(r.Text) + "\x1b[m"
	default:
		return gutter + "    " + styledText
	}
}

func renderDiffRowFallback(r EditDiffRow, pad func(string) string) string {
	switch r.Kind {
	case '-':
		return "     " + palette.DiffStripeOpen + fmt.Sprintf("%4d", r.Num) +
			palette.ToolRemoveOpen + " -  " + r.Text + pad(r.Text) + "\x1b[m"
	case '+':
		return "     " + palette.DiffStripeOpen + fmt.Sprintf("%4d", r.Num) +
			palette.ToolAddOpen + " +  " + r.Text + pad(r.Text) + "\x1b[m"
	default:
		return "     " + palette.DiffNumOpen + fmt.Sprintf("%4d", r.Num) + "\x1b[m    " + r.Text
	}
}

// Lines renders the hunk rows for the diff. It is the method form of
// RenderDiffLines, for callers that hold the diff itself.
func (d *EditDiff) Lines(width int) []string {
	return RenderDiffLines(d, width)
}

// RenderDiffLines renders the hunk rows for EditDiff.
func RenderDiffLines(d *EditDiff, width int) []string {
	if d == nil {
		return nil
	}
	head := palette.DetailBulletOpen + "  \u23bf  \x1b[m" + palette.ToolAddOpen + fmt.Sprintf("+%d", d.Added) + "\x1b[m / " +
		palette.ToolRemoveOpen + fmt.Sprintf("-%d", d.Removed) + "\x1b[m lines"
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
	for _, r := range d.Rows {
		if d.Path != "" {
			out = append(out, renderDiffRowWithPath(r, d.Path, pad))
		} else {
			out = append(out, renderDiffRowFallback(r, pad))
		}
	}
	if n := len(d.Rows); n > 0 {
		if k := d.Rows[n-1].Kind; k == '+' || k == '-' {
			out = append(out, "    ")
		}
	}
	return out
}
