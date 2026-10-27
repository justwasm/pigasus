// Package themepreview renders the Color Scheme preview card the
// reference shows beside the /config Color Scheme submenu.
//
// The card is a 63-column, 22-line rendering of a small Go diff, wrapped
// in a rounded border, with every token painted in the *hovered* colour
// scheme's palette. It exists so the user can see what a scheme looks
// like before committing to it.
//
// 【一手】 the card was recovered from agy 1.4.8's capture of the
// Color Scheme submenu — the 22-line card beside the eight scheme rows,
// one capture per scheme. Every role the card paints is one of the
// scheme's slots: the frame, the two diff backgrounds, the diff
// markers, the line numbers, the user's line, the heading, the thought,
// the tool/task/error/warning markers, the link, the accent and the dim
// hint. The code tokens are the exception — the reference paints them
// with a palette of its own, recovered as palette.Syntax.
package themepreview

import (
	"strings"

	"github.com/justwasm/pigasus/internal/onboarding"
	"github.com/justwasm/pigasus/palette"
)

// CardWidth is the card's outer width in columns, borders included.
const CardWidth = 63

// CardHeight is the card's outer height in lines, borders included.
const CardHeight = 22

// innerWidth is the width the card's content lines are padded to.
const innerWidth = CardWidth - 2

// innerHeight is the number of content lines between the two borders.
const innerHeight = CardHeight - 2

// The escapes the card writes around its coloured spans.
const (
	resetAll = "\x1b[0m"
	resetFg  = "\x1b[39m"
	resetBg  = "\x1b[49m"
	boldOn   = "\x1b[1m"
	underOn  = "\x1b[4m"
)

// The reference paints the diff backgrounds with the scheme's slots —
// except under the "terminal" scheme, which leaves them empty: there it
// falls back to these two literals.
//
// 【一手】 agy 1.4.8's terminal-scheme card: the removed line's
// background is the terminal index 131 and the added line's is 65.
const (
	fallbackAddBg    = "\x1b[48;5;65m"
	fallbackRemoveBg = "\x1b[48;5;131m"
)

// cardColors holds the SGR opens the card paints its spans with.
type cardColors struct {
	border   string
	body     string
	user     string
	heading  string
	muted    string
	lineNum  string
	addFg    string
	removeFg string
	tool     string
	task     string
	err      string
	warn     string
	link     string
	accent   string
	dim      string
	addBg    string
	removeBg string
	kw       string
	str      string
	fn       string
	typ      string
	vari     string
	kwBold   bool
}

// colorsFor returns the SGR opens the named scheme's card is painted
// with.
//
// 【一手】 each role is the scheme slot the reference reads it from:
// the frame is the border, the diff backgrounds are the add and the
// remove background, the diff markers are the add and the remove
// foreground, the line numbers are the line-number foreground, the
// user's line is the user foreground and the body foreground, the
// heading is the agent foreground, the thought is the muted
// foreground, the four marker rows are the tool, task, error and
// warning foregrounds, the link's value is the link foreground, the
// accent row is the accent and the dim hint is the dim foreground.
//
// The reference writes the colour and the attribute in one escape —
// `38;2;25;103;210;1` for the user's line, `38;2;26;115;232;4` for the
// link — so the opens below carry them together.
func colorsFor(name string) cardColors {
	sc, ok := palette.SchemeByName(name)
	if !ok {
		sc = palette.SolarizedLight
	}
	syn := palette.SyntaxColorsFor(name)
	c := cardColors{
		border:   sc.Border.Seq(false),
		body:     sc.Fg.Seq(false),
		user:     palette.SeqWith(sc.UserFg, "1"),
		heading:  palette.SeqWith(sc.AgentFg, "1"),
		muted:    sc.MutedFg.Seq(false),
		lineNum:  sc.LineNumFg.Seq(false),
		addFg:    sc.AddFg.Seq(false),
		removeFg: sc.RemoveFg.Seq(false),
		tool:     sc.ToolFg.Seq(false),
		task:     sc.TaskFg.Seq(false),
		err:      sc.ErrorFg.Seq(false),
		warn:     sc.WarningFg.Seq(false),
		link:     palette.SeqWith(sc.LinkFg, "4"),
		accent:   palette.SeqWith(sc.Accent, "1"),
		dim:      sc.DimFg.Seq(false),
		addBg:    sc.AddBg.Seq(true),
		removeBg: sc.RemoveBg.Seq(true),
		kw:       palette.SeqWith(syn.Keyword, syn.KeywordAttr),
		str:      syn.String.Seq(false),
		fn:       syn.Function.Seq(false),
		typ:      syn.Type.Seq(false),
		vari:     syn.Variable.Seq(false),
		kwBold:   syn.KeywordAttr == "1",
	}
	if c.addBg == "" {
		c.addBg = fallbackAddBg
	}
	if c.removeBg == "" {
		c.removeBg = fallbackRemoveBg
	}
	return c
}

// spanWriter writes the card's coloured spans.
//
// It repeats the reference's own behaviour of not re-writing an SGR
// that is already in effect: agy's card shows "-  func" in one span
// where the marker and the keyword share a colour, and in two where
// they do not.
type spanWriter struct {
	buf   strings.Builder
	plain strings.Builder
	curFg string
	curBg string
	bgSGR string
}

// newWriter returns a writer whose foreground and background are the
// defaults — the state the card's frame leaves the terminal in.
func newWriter() *spanWriter {
	return &spanWriter{curFg: resetFg, curBg: resetBg}
}

// fg writes a coloured run of text. An empty colour means the default
// foreground.
func (w *spanWriter) fg(sgr, text string) {
	if sgr == "" {
		sgr = resetFg
	}
	if sgr != w.curFg {
		w.buf.WriteString(sgr)
		w.curFg = sgr
	}
	w.buf.WriteString(text)
	w.plain.WriteString(text)
}

// bg sets the background the rest of the line is painted on. The
// background survives a foreground reset, so it is written again after
// a full one.
func (w *spanWriter) bg(sgr string) {
	if sgr == "" {
		sgr = resetBg
	}
	w.bgSGR = sgr
	if sgr != w.curBg {
		w.buf.WriteString(sgr)
		w.curBg = sgr
	}
}

// attr writes an attribute — a bold or an underline — that leaves the
// colour in effect alone.
func (w *spanWriter) attr(seq string) {
	w.buf.WriteString(seq)
}

// body writes a run of body text that follows a bold run.
//
// 【一手】 agy 1.4.8 closes the bold with the body colour's own escape
// — the colour and the normal intensity in one, `38;2;32;33;36;22` —
// and with a full reset when the body colour is the default, as it is
// under the terminal scheme.
func (w *spanWriter) body(sgr, text string) {
	if sgr == "" {
		w.resetAll()
		w.fg("", text)
		return
	}
	w.fg(strings.TrimSuffix(sgr, "m")+";22m", text)
}

// resetAll writes the full reset: it clears the colour, the background
// and the attributes. The line's background goes with it, so it is
// written again.
func (w *spanWriter) resetAll() {
	w.buf.WriteString(resetAll)
	w.curFg = resetFg
	w.curBg = resetBg
	if w.bgSGR != "" && w.bgSGR != resetBg {
		w.buf.WriteString(w.bgSGR)
		w.curBg = w.bgSGR
	}
}

// resetFg writes the default foreground — but only when the foreground
// is not the default already: the reference does not close a span that
// is already closed.
func (w *spanWriter) resetFg() {
	if w.curFg == resetFg {
		return
	}
	w.buf.WriteString(resetFg)
	w.curFg = resetFg
}

// resetBg writes the default background — but only when the background
// is not the default already.
func (w *spanWriter) resetBg() {
	if w.curBg == resetBg {
		return
	}
	w.buf.WriteString(resetBg)
	w.curBg = resetBg
}

// closeBg returns the sequence that closes the background, or the
// empty string when the background is already closed. The diff lines
// close theirs after the padding, so the sequence is written out of
// the writer's order.
func (w *spanWriter) closeBg() string {
	if w.curBg == resetBg {
		return ""
	}
	w.curBg = resetBg
	return resetBg
}

// keyword writes a keyword run. The reference paints the keyword bold
// under the terminal scheme, and closes it with a full reset.
func (w *spanWriter) keyword(sgr, text string, bold bool) {
	w.fg(sgr, text)
	if bold {
		w.resetAll()
	}
}

// pad returns the spaces that fill the card's inner width. A diff line
// keeps its last column outside the background, so it reserves that
// column.
func (w *spanWriter) pad(reserve int) string {
	n := innerWidth - len([]rune(w.plain.String())) - reserve
	if n < 0 {
		n = 0
	}
	return strings.Repeat(" ", n)
}

// Render returns the preview card for scheme as a slice of lines.
//
// The card's 20 content lines are a miniature Go diff: the user's
// greeting, the change, the diff body, and then one line per semantic
// token (thought, tool, task, error, warning, link, accent, dim).
func Render(scheme onboarding.ColorScheme) []string {
	c := colorsFor(scheme.String())

	body := make([]string, 0, innerHeight)

	// The user's line.
	//
	// 【一手】 agy 1.4.8 writes the user's line as the user foreground
	// and the body foreground, and closes the bold with the body
	// colour's own escape — `38;2;25;103;210;1` then
	// `38;2;32;33;36;22`.
	w := newWriter()
	w.fg("", " ")
	w.fg(c.user, "> you: ")
	w.body(c.body, "add a greeting function")
	w.resetFg()
	body = append(body, w.buf.String()+w.pad(0))

	// A blank line.
	body = append(body, strings.Repeat(" ", innerWidth))

	// The heading.
	w = newWriter()
	w.fg("", " ")
	w.attr(boldOn)
	w.fg(c.heading, "  Here's the change:")
	w.resetAll()
	body = append(body, w.buf.String()+w.pad(0))

	// A blank line.
	body = append(body, strings.Repeat(" ", innerWidth))

	// The import line — a context line of the diff.
	w = newWriter()
	w.fg("", "   ")
	w.fg(c.lineNum, "3 ")
	w.resetFg()
	w.fg("", " ")
	w.fg(c.body, "   ")
	w.keyword(c.kw, "import", c.kwBold)
	w.fg(c.body, " ")
	w.fg(c.str, "\"fmt\"")
	w.resetFg()
	body = append(body, w.buf.String()+w.pad(0))

	// The empty line under it — a context line of the diff, so it keeps
	// the indentation the body colour paints.
	w = newWriter()
	w.fg("", "   ")
	w.fg(c.lineNum, "4 ")
	w.resetFg()
	w.fg("", " ")
	w.fg(c.body, "   ")
	w.resetFg()
	body = append(body, w.buf.String()+w.pad(0))

	// The removed line.
	w = newWriter()
	w.fg("", "   ")
	w.fg(c.lineNum, "5 ")
	w.resetFg()
	w.bg(c.removeBg)
	w.fg("", " ")
	w.fg(c.removeFg, "-  ")
	w.keyword(c.kw, "func", c.kwBold)
	w.fg(c.body, " ")
	w.fg(c.fn, "main")
	w.fg(c.body, "() {")
	w.resetFg()
	body = append(body, w.buf.String()+w.pad(1)+w.closeBg()+" ")

	// The added line.
	w = newWriter()
	w.fg("", "   ")
	w.fg(c.lineNum, "5 ")
	w.resetFg()
	w.bg(c.addBg)
	w.fg("", " ")
	w.fg(c.addFg, "+  ")
	w.keyword(c.kw, "func", c.kwBold)
	w.fg(c.body, " ")
	w.fg(c.fn, "greet")
	w.fg(c.body, "(")
	w.fg(c.vari, "name")
	w.fg(c.body, " ")
	w.fg(c.typ, "string")
	w.fg(c.body, ") {")
	w.resetFg()
	body = append(body, w.buf.String()+w.pad(1)+w.closeBg()+" ")

	// The added line's body.
	w = newWriter()
	w.fg("", "   ")
	w.fg(c.lineNum, "6 ")
	w.resetFg()
	w.bg(c.addBg)
	w.fg("", " ")
	w.fg(c.addFg, "+  ")
	w.fg(c.body, "    ")
	w.fg(c.vari, "fmt")
	w.fg(c.body, ".")
	w.fg(c.fn, "Printf")
	w.fg(c.body, "(")
	w.fg(c.str, "\"Hello, %s!\\n\"")
	w.fg(c.body, ", ")
	w.fg(c.vari, "name")
	w.fg(c.body, ")")
	w.resetFg()
	body = append(body, w.buf.String()+w.pad(1)+w.closeBg()+" ")

	// The closing brace.
	w = newWriter()
	w.fg("", "   ")
	w.fg(c.lineNum, "7 ")
	w.resetFg()
	w.fg("", " ")
	w.fg(c.body, "   }")
	w.resetFg()
	body = append(body, w.buf.String()+w.pad(0))

	// A blank line.
	body = append(body, strings.Repeat(" ", innerWidth))

	// The thought.
	w = newWriter()
	w.fg("", " ")
	w.fg(c.muted, "  ▾ Thought Process")
	w.resetFg()
	body = append(body, w.buf.String()+w.pad(0))

	w = newWriter()
	w.fg("", " ")
	w.fg(c.muted, "    I need to add a greeting function. I'll use fmt.Printf.")
	w.resetFg()
	body = append(body, w.buf.String()+w.pad(1)+" ")

	// The marker rows.
	w = newWriter()
	w.fg("", " ")
	w.fg(c.tool, "  ⚙ tool: ")
	w.fg(c.body, "write_file main.go")
	w.resetFg()
	body = append(body, w.buf.String()+w.pad(0))

	w = newWriter()
	w.fg("", " ")
	w.fg(c.task, "  ◉ task: ")
	w.fg(c.body, "Implementing greeting")
	w.resetFg()
	body = append(body, w.buf.String()+w.pad(0))

	w = newWriter()
	w.fg("", " ")
	w.fg(c.err, "  ✗ error: ")
	w.fg(c.body, "compilation failed")
	w.resetFg()
	body = append(body, w.buf.String()+w.pad(0))

	w = newWriter()
	w.fg("", " ")
	w.fg(c.warn, "  ⚠ warning: ")
	w.fg(c.body, "deprecation warning")
	w.resetFg()
	body = append(body, w.buf.String()+w.pad(0))

	// The link.
	w = newWriter()
	w.fg("", " ")
	w.fg(c.body, "  → link: ")
	w.fg(c.link, "file:///path/to/main.go")
	w.resetAll()
	body = append(body, w.buf.String()+w.pad(0))

	// The accent.
	w = newWriter()
	w.fg("", " ")
	w.fg(c.accent, "  ★ accent: ")
	w.body(c.body, "highlighted text")
	w.resetFg()
	body = append(body, w.buf.String()+w.pad(0))

	// The dim hint.
	w = newWriter()
	w.fg("", " ")
	w.fg(c.dim, "  · dim: press Enter to continue")
	w.resetFg()
	body = append(body, w.buf.String()+w.pad(0))

	card := make([]string, 0, CardHeight)
	card = append(card, c.border+"╭"+strings.Repeat("─", innerWidth)+"╮"+resetFg)
	for _, line := range body {
		card = append(card, c.border+"│"+resetFg+line+c.border+"│"+resetFg)
	}
	card = append(card, c.border+"╰"+strings.Repeat("─", innerWidth)+"╯"+resetFg)

	return card
}
