// Package diffrow renders a diff body the way the reference does.
//
// 【反编译】 the reference's *transcript* edit diff is built by
// `render.codeActionDetail` (0x1028fff40) and rendered one row at a time
// by `render.codeActionDetail.func2` (0x102912630). A row is
//
//	<gutter><marker><text>
//
// where
//
//   - the gutter is `fmt.Sprintf("%*d", numWidth, num)` — the line
//     number right-aligned in `numWidth` columns. The format string is
//     the three bytes "%*d" at 0x1033bf730;
//   - the marker column is four characters wide: " +  " for an added
//     line, " -  " for a removed one and "    " for an unchanged one.
//
// The three bodies come from three format strings in the binary:
// `"+  %s"` (0x1033d1ad8), `"-  %s"` (0x1033d1add) and `"    %s"`
// (0x1033df8e9). The added and removed bodies carry one leading space
// more than the context body — `render.codeActionDetail.func2`
// concatenates a one-byte " " (0x103dd22f8) in front of them — so the
// text starts in the same column in all three.
//
// The gutter is painted in the row's *line-number* style and the body in
// the row's *content* style — five styles in all, the ones the
// reference's style set carries as `DiffLineNum`, `DiffAddLineNum`,
// `DiffRemoveLineNum`, `DiffAdd` and `DiffRemove` (see
// docs/cycles/cycle-323-tui-render-style-scheme-table/deliverable.md).
// The added and removed bodies are padded with spaces to the content
// width; the context body is neither padded nor styled.
//
// 【反编译】 the reference's row renderer does not highlight the text it
// is handed: `codeActionDetail.func2` calls nothing but `fmt.Sprintf`,
// `strings.Repeat`, `utf8.RuneCountInString`, `runtime.concatstring2`
// and `lipgloss.Style.Render`. The text arrives already styled — the
// inline diff preview carries "syntax highlighting for your color
// scheme" (the 1.4.1 changelog line,
// internal/cli/testdata/changelog_reference.txt:88) — so the row
// renderer only has to measure the *plain* text to pad it.
//
// 【反编译】 the widths come out of `render.codeActionDetail`: the
// gutter width is max(4, digits) and the content width is
// max(1, width-numWidth-2), so a row is one column narrower than the
// width it was given.
package row

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/justwasm/pigasus/palette"
)

// Kind is the kind of a diff row.
type Kind byte

const (
	// Context is an unchanged line.
	Context Kind = ' '
	// Add is an added line.
	Add Kind = '+'
	// Remove is a removed line.
	Remove Kind = '-'
)

// Row is one line of a diff body.
type Row struct {
	// Kind is the row's kind.
	Kind Kind
	// Num is the number the reference prints in the gutter: the new
	// file's line number for a context or an added row, the old file's
	// for a removed one.
	Num int
	// Text is the line's content, without its marker.
	Text string
}

// Layout carries the two widths the reference computes for a diff body.
type Layout struct {
	// NumWidth is the width of the gutter's line number.
	NumWidth int
	// ContentWidth is the width an added or removed body is padded to.
	ContentWidth int
}

// NewLayout computes the reference's two widths.
//
// 【反编译】 `render.codeActionDetail` takes the gutter width as
// max(4, digits) and the content width as max(1, width-numWidth-2).
func NewLayout(width, maxLine int) Layout {
	numWidth := 4
	if d := digits(maxLine); d > numWidth {
		numWidth = d
	}
	content := width - numWidth - 2
	if content < 1 {
		content = 1
	}
	return Layout{NumWidth: numWidth, ContentWidth: content}
}

// digits counts the decimal digits of n.
func digits(n int) int {
	if n < 0 {
		n = -n
	}
	d := 1
	for n >= 10 {
		n /= 10
		d++
	}
	return d
}

// Gutter renders the reference's gutter — `fmt.Sprintf("%*d", numWidth, num)`.
func (l Layout) Gutter(num int) string {
	return fmt.Sprintf("%*d", l.NumWidth, num)
}

// Marker renders the reference's four-character marker column.
//
// 【反编译】 the added and removed bodies carry a leading space the
// context body does not need, so the text starts in the same column in
// all three.
func Marker(k Kind) string {
	switch k {
	case Add:
		return " +  "
	case Remove:
		return " -  "
	}
	return "    "
}

// Summary renders the reference's per-change summary.
//
// 【反编译】 `render.codeActionDetail` picks one of three forms:
// "Added %d line%s" (0x1034b43b4), "Removed %d line%s" (0x1034dcd2d)
// — the plural "s" only when the count is not one — and, for a change
// that both adds and removes, the "+%d" (0x1033bf754) and "-%d"
// (0x1033bf757) pair joined by " / " (0x1033bf19c) and closed by
// " lines" (0x1033df8e3).
//
// The first two forms are plain text in the reference — only the counts
// of the third form are painted — so Summary returns them unstyled and
// the caller paints the pair itself.
func Summary(added, removed int) string {
	switch {
	case removed == 0:
		return fmt.Sprintf("Added %d line%s", added, plural(added))
	case added == 0:
		return fmt.Sprintf("Removed %d line%s", removed, plural(removed))
	}
	return fmt.Sprintf("+%d / -%d lines", added, removed)
}

// plural returns the plural "s" for a count that is not one.
func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// More renders the reference's truncation marker.
//
// 【反编译】 the reference formats it with "%*s    … and %d more lines"
// (0x10358a97d): the indent, four spaces, an ellipsis and the number of
// rows it did not show.
//
// 【推测】 the indent's own source is not recoverable — the reference
// reads it from a state field this cycle did not pin — so the clone
// passes the gutter's width, which puts the ellipsis in the column the
// rows' text starts in.
func More(indent, n int) string {
	return fmt.Sprintf("%*s    … and %d more lines", indent, "", n)
}

// Style carries the escape sequences the row renderer paints with.
//
// The colours are the ones agy 1.4.3's captured diff carries
// (docs/cycles/cycle-285-tool-display/deliverable.md): the line number
// is gray, an unchanged line's text is plain, a changed line's number
// carries the stripe and its text is green or red.
type Style struct {
	// NumOpen and NumClose paint the gutter of an unchanged row.
	NumOpen, NumClose string
	// AddBg and RemoveBg paint a changed row — the reference's stripe
	// in the transcript, the `/diff` file view's tint in the panel.
	// They run from the gutter across the padded body.
	AddBg, RemoveBg string
	// AddOpen paints an added row's body; RemoveOpen paints a removed
	// row's body.
	AddOpen, RemoveOpen string
	// RowClose closes a changed row.
	RowClose string
}

// TranscriptStyle is the style of the reference's *transcript* edit diff.
//
// 【一手】 agy 1.4.3's captured diff shows the line number in gray, the
// changed rows' numbers on the stripe and the changed rows' text in
// green or red. The stripe is the changed row's *background* — the
// capture's `#eee8d5`, which is the reference's own add and remove
// background in the solarized light scheme — and it runs across the
// whole row, because the capture erases to the end of the line
// (`\x1b[K`) and the 1.4.8 binary pads the row to the same width.
//
// The add and remove *tint* the reference paints in its `/diff` file
// view ("Added syntax highlighting to the `/diff` file view, with token
// colors for each color scheme and a background tint on added and
// removed lines", internal/cli/testdata/changelog_reference.txt:127)
// belongs to that view, not to the transcript's diff.
func TranscriptStyle() Style {
	return Style{
		NumOpen:    palette.DiffNumOpen,
		NumClose:   "\x1b[m",
		AddBg:      palette.DiffStripeOpen,
		RemoveBg:   palette.DiffStripeOpen,
		AddOpen:    palette.ToolAddOpen,
		RemoveOpen: palette.ToolRemoveOpen,
		RowClose:   "\x1b[m",
	}
}

// FileViewStyle is the style of the reference's `/diff` file view.
//
// 【strings】 the `/diff` file view paints "a background tint on added
// and removed lines" (changelog_reference.txt:127) — and the tint is the
// one the reference picks from the terminal's own background brightness
// (`model.diffTerminalTints`, the four tints it keeps in __DATA's bss).
//
// The tint runs from the gutter across the whole row, so it is the
// *background* the row is painted on; the text keeps the syntax
// colours the file view highlights it with.
func FileViewStyle() Style {
	s := TranscriptStyle()
	s.AddBg = palette.DiffNumOpen + palette.DiffAddTintOpen
	s.RemoveBg = palette.DiffNumOpen + palette.DiffRemoveTintOpen
	return s
}

// Render writes one row of a diff body.
func Render(l Layout, s Style, r Row) string {
	return RenderStyled(l, s, r.Kind, r.Num, r.Text, r.Text)
}

// RenderStyled writes one row whose body text is already styled.
//
// plain is the row's plain text and is what the content width is
// measured against; styled is what the row shows. Passing the same
// string twice renders the row unhighlighted.
//
// 【反编译】 `render.codeActionDetail.func2` builds a row out of two
// renders: the gutter — `fmt.Sprintf("%*d", numWidth, num)` — in the
// row's line-number style, and the body in the row's content style. An
// unchanged row's body is the plain text behind a four-character marker
// column; an added or a removed row's body is a space, the marker and
// the text, padded with spaces to the content width.
func RenderStyled(l Layout, s Style, k Kind, num int, plain, styled string) string {
	switch k {
	case Add:
		return s.AddBg + l.Gutter(num) +
			s.AddOpen + padBody("+  ", styled, plain, l.ContentWidth) + s.RowClose
	case Remove:
		return s.RemoveBg + l.Gutter(num) +
			s.RemoveOpen + padBody("-  ", styled, plain, l.ContentWidth) + s.RowClose
	}
	return s.NumOpen + l.Gutter(num) + s.NumClose + Marker(Context) + styled
}

// padBody builds a changed row's body: a space, the three-character
// marker and the text, padded with spaces to the content width.
//
// 【反编译】 the reference measures the body in runes
// (`utf8.RuneCountInString`) and pads it with `strings.Repeat(" ", …)`,
// so the padding counts the *visible* characters and not the escape
// sequences the styled text carries.
func padBody(marker, styled, plain string, width int) string {
	body := " " + marker + styled
	n := utf8.RuneCountInString(marker + plain)
	if width <= 0 || n >= width {
		return body
	}
	return body + strings.Repeat(" ", width-n)
}
