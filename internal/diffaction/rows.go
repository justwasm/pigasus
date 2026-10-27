// Package diffaction rows.go — the /diff file view's row rendering.
package diffaction

import (
	"strings"

	"github.com/justwasm/pigasus/diffsyntax"
	"github.com/justwasm/pigasus/internal/difffold"
	"github.com/justwasm/pigasus/internal/diffrow"
	"github.com/justwasm/pigasus/palette"
)

// RowStyler renders a git diff body in the reference's row model.
//
// 【strings】 the reference's `/diff` file view carries "syntax
// highlighting to the `/diff` file view, with token colors for each
// color scheme and a background tint on added and removed lines. With
// the `Terminal` color scheme the CLI detects whether your terminal has a
// light or dark background and picks matching colors"
// (internal/cli/testdata/changelog_reference.txt:127).
//
// 【反编译】 the rows are the reference's `render.codeActionDetail.func2`
// rows: a right-aligned line number in the gutter, a four-character
// marker column and the text. The added and removed rows are padded to
// the content width, so the tint spans the whole row — that padding is
// what the reference's `strings.Repeat(" ", …)` is for.
//
// The hunk header keeps the reference's `DiffHunk` style, which the
// style table maps to the scheme's line-number colour
// (docs/cycles/cycle-323-tui-render-style-scheme-table/deliverable.md).
type RowStyler struct {
	langFile string
	layout   diffrow.Layout
	style    diffrow.Style
	oldLine  int
	newLine  int
}

// NewRowStyler returns a styler for a git diff body.
//
// maxLine is the largest line number the body prints — it decides how
// wide the gutter's number field is.
func NewRowStyler(langFile string, width, maxLine int) *RowStyler {
	return &RowStyler{
		langFile: langFile,
		layout:   diffrow.NewLayout(width, maxLine),
		style:    diffrow.FileViewStyle(),
	}
}

// MaxLineNumber returns the largest line number the diff's hunks print.
func MaxLineNumber(hunks []difffold.Hunk) int {
	max := 0
	for _, h := range hunks {
		head, ok := diffrow.ParseHunkHeader(h.Header)
		if !ok {
			continue
		}
		if n := head.OldStart + head.OldCount - 1; n > max {
			max = n
		}
		if n := head.NewStart + head.NewCount - 1; n > max {
			max = n
		}
	}
	return max
}

// Style renders one line of the git diff body.
//
// The lines that are not diff rows — the file header ("diff --git",
// "index", "--- a/x", "+++ b/x") and the hunk header ("@@ -1,3 +1,4 @@")
// — pass through with their own style.
func (s *RowStyler) Style(line string) string {
	if line == "" {
		return line
	}
	if strings.HasPrefix(line, "@@") {
		s.resetHunk(line)
		return palette.DiffNumOpen + line + "\x1b[39m"
	}
	if isFileHeader(line) {
		return line
	}
	kind, text := splitRow(line)
	if kind == diffrow.Context && line[0] != ' ' {
		return line
	}
	num := s.newLine
	switch kind {
	case diffrow.Remove:
		num = s.oldLine
		s.oldLine++
	case diffrow.Add:
		s.newLine++
	default:
		s.oldLine++
		s.newLine++
	}
	styled := text
	if s.langFile != "" {
		if tokens := diffsyntax.TokenizeLine(s.langFile, " "+text); len(tokens) > 1 {
			styled = diffsyntax.RenderInlineTokenStream(tokens[1:])
		}
	}
	return diffrow.RenderStyled(s.layout, s.style, kind, num, text, styled)
}

// resetHunk reads the line ranges a hunk header declares and rewinds the
// two line counters to the hunk's first line.
func (s *RowStyler) resetHunk(line string) {
	head, ok := diffrow.ParseHunkHeader(line)
	if !ok {
		return
	}
	s.oldLine = head.OldStart
	s.newLine = head.NewStart
}

// isFileHeader reports whether a line is one of the diff body's file
// header lines rather than a diff row.
func isFileHeader(line string) bool {
	switch {
	case strings.HasPrefix(line, "diff "),
		strings.HasPrefix(line, "index "),
		strings.HasPrefix(line, "--- "),
		strings.HasPrefix(line, "+++ "),
		strings.HasPrefix(line, "new file"),
		strings.HasPrefix(line, "deleted file"),
		strings.HasPrefix(line, "similarity index"),
		strings.HasPrefix(line, "rename "),
		strings.HasPrefix(line, "old mode"),
		strings.HasPrefix(line, "new mode"),
		strings.HasPrefix(line, "Binary files"):
		return true
	}
	return false
}

// splitRow splits a diff row into its kind and its text.
func splitRow(line string) (diffrow.Kind, string) {
	switch line[0] {
	case '+':
		return diffrow.Add, line[1:]
	case '-':
		return diffrow.Remove, line[1:]
	case ' ':
		return diffrow.Context, line[1:]
	}
	return diffrow.Context, line
}
