// reader.go — the full-text reader the two foldable views open.
//
// # Why a reader and not a taller fold
//
// A fold shows what the row already carries, and the rows carry excerpts:
// the host clips a message to its first line, a step to twelve lines, a
// section to its byte count. Those clips are the right thing for a list —
// a 40 KB tool result must not make the outline unreadable — but they are
// the wrong answer to "show me what this actually says".
//
// So the reader is a second level rather than a bigger first one: the
// list stays an outline, and Enter on a row pulls its body on demand from
// the host (Seams.Read). The panel never holds the whole conversation —
// it asks for one row's body at the moment somebody wants to read it.
//
// # Why the frame is the same frame
//
// The reader draws through the same layout() as the three views: one bar,
// a heading, one blank, the body, a counter and a legend. Reusing the
// geometry rather than writing a second one is what keeps a click, a page
// key and the frame's own height from disagreeing — the failure the
// package comment calls "one arithmetic".
package debugpanel

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	tuiviewer "github.com/justwasm/pigasus/internal/ui/foundations/viewer"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/palette"
)

const (
	// readerMaxLines bounds how much of one body the reader will lay out.
	// A tool result can be a megabyte; wrapping it every frame to scroll
	// forty lines of it would make the panel slow exactly when the body
	// is interesting. Past the bound the reader says how much it left out
	// rather than pretending the body ended.
	readerMaxLines = 4000

	// readerBodyIndent is the body's left margin, in cells. The label row
	// sits one level out from the text it names.
	readerBodyIndent  = 4
	readerLabelIndent = 2

	// readerTabWidth is how wide a tab is drawn. A terminal's own tab
	// stops are not visible to this package, and a body wrapped against
	// guessed stops would overflow the row it was wrapped for.
	readerTabWidth = 4
)

// ReaderPart is one labelled block of a body.
//
// A tool step is two of them — the arguments and the result — because
// they are two documents: pretty-printing the pair as one JSON value
// would fail, and the arguments are the half a reader usually wants.
type ReaderPart struct {
	// Label names the block, e.g. "args". Empty for a body that is one
	// unlabelled document.
	Label string
	// Text is the block's content, as the host stored it.
	Text string
}

// Body is one row's full content, as the host hands it over.
type Body struct {
	// Title names what is being read, e.g. "wire/msg/3 · tool".
	Title string
	// Note is an optional caveat printed under the title.
	Note string
	// Parts are the blocks to print, in order.
	Parts []ReaderPart
}

// ReaderMsg is a body coming back from the host.
//
// The read is a command rather than a plain call because a cold step's
// body is a database read, and the panel is drawn every frame: blocking
// the update loop on the store would stall every other panel with it.
type ReaderMsg struct {
	ID   string
	Body Body
	Err  error
}

// reader is one open body: its text, the format it is shown in, and how
// far it has scrolled.
type reader struct {
	id   string
	body Body
	// format is the rendering the user asked for. A part the format
	// cannot read is shown as it came, and its label says so by omitting
	// the format's name.
	format ReaderFormat
	// structured reports whether any part parses as JSON, which is what
	// makes the format key worth offering.
	structured bool
	// scroll is the window's top line, and line is the cursor's line.
	//
	// They are separate state, exactly as in /artifact: `j`/`k` move the
	// cursor, `g`/`G` go to the document's ends, `ctrl+u`/`ctrl+d` move
	// half a page, and the window follows the cursor only when the cursor
	// would leave it. The wheel moves the window instead, because that is
	// what a wheel means everywhere else in the app.
	scroll int
	line   int
	// layout memo: the wrapped lines are rebuilt only when the width or
	// the format changes, so a scrolling frame is a slice, not a re-wrap.
	cacheWidth  int
	cacheFormat ReaderFormat
	cache       []string
	// docCache holds the gutter-decorated document, so scrolling does not
	// re-number every visible line. It is keyed by the same width and
	// format as the wrap cache, plus the cursor (the gutter marks it).
	docCache  []string
	docWidth  int
	docFormat ReaderFormat
	docLine   int
	docBody   int
	docSet    bool
	// numbers maps each rendered line to the number its gutter prints, so
	// the footer's L<n> and the gutter's n are the same number.
	numbers []int
}

// newReader opens a body for reading.
func newReader(id string, body Body) *reader {
	if body.Title == "" {
		body.Title = id
	}
	r := &reader{id: id, body: body, cacheWidth: -1, cacheFormat: FormatRaw}
	for _, part := range body.Parts {
		if structured(part.Text) {
			r.structured = true
			break
		}
	}
	return r
}

// cycleFormat advances the rendering, and only when there is something to
// reformat: a plain-text result offered "json" would print the same bytes
// under a different name.
func (r *reader) cycleFormat() bool {
	if !r.structured {
		return false
	}
	r.format = r.format.Next()
	// The reformatted text has a different line count — indented JSON has
	// many more lines than the compact source — so the cursor's index no
	// longer points at the same content, or at any content. The next draw
	// clamps it; dropping the memo here is what makes that a clamp rather
	// than a stale document.
	r.docSet = false
	r.cache = nil
	return true
}

// copyText is what the copy key hands the clipboard: the body in the
// format on screen, unwrapped, so it pastes as the document it is rather
// than as the reader's line breaks.
func (r *reader) copyText() string {
	var out []string
	for _, part := range r.body.Parts {
		if part.Label != "" {
			out = append(out, part.Label+":")
		}
		out = append(out, renderPart(part.Text, r.format))
	}
	return strings.Join(out, "\n")
}

// lines renders the body into display lines at the given width.
func (r *reader) lines(width int) []string {
	if r.cacheWidth == width && r.cacheFormat == r.format {
		return r.cache
	}
	var out []string
	for i, part := range r.body.Parts {
		text := renderPart(part.Text, r.format)
		// A label earns its row when it names something: a body of one
		// unlabelled part is its own title.
		if part.Label != "" {
			out = append(out, readerLabelRow(part.Label, text != part.Text, r.format, width))
		}
		if i > 0 && part.Label == "" {
			out = append(out, "")
		}
		for _, line := range wrapBody(text, width-readerBodyIndent) {
			if line == "" {
				// An empty body line stays empty rather than becoming the
				// indent alone: four trailing spaces are invisible to a
				// reader and become part of whatever a selection copies.
				out = append(out, "")
				continue
			}
			out = append(out, strings.Repeat(" ", readerBodyIndent)+line)
		}
		if i < len(r.body.Parts)-1 {
			out = append(out, "")
		}
	}
	if len(out) == 0 {
		out = append(out, strings.Repeat(" ",
			readerBodyIndent)+palette.DimGrayOpen+"(this body is empty)"+palette.DimGrayClose)
	}
	if len(out) > readerMaxLines {
		left := len(out) - readerMaxLines
		out = out[:readerMaxLines]
		out = append(out, strings.Repeat(" ",
			readerBodyIndent)+palette.DimGrayOpen+
			fmt.Sprintf("… %d more lines (y copies the whole body)", left)+
			palette.DimGrayClose)
	}
	r.cacheWidth, r.cacheFormat, r.cache = width, r.format, out
	return out
}

// numberedDoc decorates the reader's lines with a line-number gutter and
// the cursor column, the way /artifact's viewer does.
//
// # Which lines are numbered
//
// Only the body's own text lines. A part's label row ("args · yaml") and
// the blank separator between parts are the reader's furniture, not the
// document's lines: numbering them would make the numbers disagree with
// the source, which is the one thing a line number must not do. So the
// index a user sees is the line's position in the body it came from, and
// switching to JSON renumbers from the reformatted text — which the label
// says out loud.
//
// # Why the cursor lives here
//
// The cursor marks a line the keys act on, so it has to be part of the
// rendered row rather than the window's arithmetic: the same document
// scrolled one line shows the cursor on a different row.
func (r *reader) numberedDoc(width int) []string {
	if r.docSet && r.docWidth == width && r.docFormat == r.format &&
		r.docLine == r.line && r.docBody == len(r.cache) {
		return r.docCache
	}
	lines := r.lines(width)
	bodyTotal := 0
	for _, l := range lines {
		if readerLineIsBody(l) {
			bodyTotal++
		}
	}
	out := make([]string, 0, len(lines))
	bodyNo := 0
	for docIdx, l := range lines {
		if !readerLineIsBody(l) {
			// Furniture keeps the text column so the label does not move
			// when the numbers grow. A blank separator stays blank: the
			// indent would be invisible on screen and would become part of
			// whatever a selection copies.
			if l == "" {
				out = append(out, "")
				continue
			}
			// The label sits one level out from the text it names, so its
			// column is the body's less the difference between the two
			// indents. Trimming all leading space rather than the body's
			// exact indent: a label carries readerLabelIndent, not
			// readerBodyIndent, so a TrimPrefix of the latter silently
			// failed and left the label two cells right of its own body.
			out = append(out, strings.Repeat(" ",
				tuiviewer.GutterIndent(bodyTotal)-(readerBodyIndent-readerLabelIndent))+
				strings.TrimLeft(l, " "))
			continue
		}
		bodyNo++
		// The cursor is held as a document index (r.line), the gutter
		// prints a body-line number (bodyNo). Comparing bodyNo to r.line
		// would compare two different numbering schemes and put the mark
		// on the wrong row the moment the body has a label above it.
		cursor := docIdx == r.line
		mark := ""
		if cursor {
			mark = palette.BlueOpen + ">" + palette.BlueClose
		}
		text := strings.TrimPrefix(l, strings.Repeat(" ", readerBodyIndent))
		out = append(out, tuiviewer.GutterRow(bodyNo, bodyTotal, cursor, mark)+text)
	}
	r.docCache = out
	r.docWidth, r.docFormat, r.docLine, r.docBody = width, r.format, r.line, len(r.cache)
	r.docSet = true
	return out
}

// readerLineIsBody reports whether a rendered reader line is one of the
// body's own text lines rather than the reader's furniture.
//
// The distinguishing marks are the indents the renderer gives them: a body
// line is indented by exactly readerBodyIndent, a label by
// readerLabelIndent, and a part separator is empty.
func readerLineIsBody(line string) bool {
	if line == "" {
		return false
	}
	prefix := strings.Repeat(" ", readerBodyIndent)
	if !strings.HasPrefix(line, prefix) {
		return false
	}
	// A label row is indented less, so it cannot have reached this point;
	// what still has to be excluded is a body line that is *more* indented
	// (nested content), which is still the body and stays numbered.
	return true
}

// bodyLineCount is how many of the rendered lines are numbered.
func (r *reader) bodyLineCount(width int) int {
	n := 0
	for _, l := range r.lines(width) {
		if readerLineIsBody(l) {
			n++
		}
	}
	return n
}

// readerLabelRow names a part, and says which rendering it is in when the
// rendering changed it.
//
// The label is where the honesty about formats lives: a "result" that is
// not JSON keeps its label bare, so a reader can tell a pretty-printed
// argument block from a result the format could not touch.
func readerLabelRow(label string, converted bool, f ReaderFormat, width int) string {
	s := strings.Repeat(" ", readerLabelIndent) + palette.DimGrayOpen + label
	if converted {
		s += "  ·  " + f.Name()
	}
	s += palette.DimGrayClose
	if width > 0 {
		s = ansi.Truncate(s, width, "…")
	}
	return s
}

// wrapBody splits text into lines that fit width cells.
//
// It wraps rather than clips: the reader exists to show text the list
// could not, and a reader that cut every long line at the edge would hide
// exactly the part somebody opened it to see. Tabs are expanded first
// because a terminal's tab stops are not this package's to know.
func wrapBody(text string, width int) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	if width < 8 {
		width = 8
	}
	var out []string
	for _, raw := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		line := expandTabs(raw)
		if ansi.StringWidth(line) <= width {
			out = append(out, line)
			continue
		}
		for _, seg := range strings.Split(ansi.Wrap(line, width, ""), "\n") {
			out = append(out, seg)
		}
	}
	// A wrapped line ends where the break falls, which is usually after a
	// space: leaving it there paints a trailing blank the cursor can sit
	// on and a selection copies. Trim each line's right edge, but not the
	// body's own indentation — a line that is all spaces keeps one so the
	// paragraph break stays visible.
	for i, line := range out {
		if trimmed := strings.TrimRight(line, " "); trimmed != "" {
			out[i] = trimmed
		}
	}
	return out
}

// expandTabs replaces tabs with spaces to the next tab stop, counting
// cells the way the terminal will.
func expandTabs(s string) string {
	if !strings.ContainsRune(s, '\t') {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 8)
	col := 0
	for _, r := range s {
		if r == '\t' {
			n := readerTabWidth - col%readerTabWidth
			b.WriteString(strings.Repeat(" ", n))
			col += n
			continue
		}
		b.WriteRune(r)
		col += ansi.StringWidth(string(r))
	}
	return b.String()
}

// readerHeadingLines renders the reader's heading: what is being read and
// how it is being rendered.
func (p *Panel) readerHeadingLines() []string {
	r := p.reader
	head := "\x1b[1m" + palette.SubtleOpen + "└ " + r.body.Title + "\x1b[0m"
	head += palette.DimGrayOpen
	if r.structured {
		head += "  ·  " + r.format.Name()
	}
	head += palette.DimGrayClose
	lines := []string{head}
	if r.body.Note != "" {
		lines = append(lines, "   "+palette.DimGrayOpen+r.body.Note+palette.DimGrayClose)
	}
	return lines
}

// readerFrame renders the reader through the same geometry the three
// views use.
func (p *Panel) readerFrame() []string {
	r := p.reader
	heading := p.readerHeadingLines()
	lay := p.layoutFor(p.height(), len(heading))
	p.readerHeadingRows = len(heading)

	// The cursor is what the keys act on, so it is clamped against the
	// document and the window is then made to contain it — the same
	// two-state model /artifact's viewer uses.
	// The numbering is computed before the cursor is clamped: the clamp
	// has to know which lines are numbered to keep the cursor off the
	// reader's furniture (a label row prints no number).
	r.numbers = bodyNumbering(r.lines(p.width()))
	clampReaderCursor(r, len(r.lines(p.width())))
	body := r.numberedDoc(p.width())
	total := len(body)
	r.scroll = tuiviewer.WindowForCursor(r.scroll, r.line, lay.visible, total)
	p.readerRows = lay.visible
	rows, start, _, _ := window(body, r.scroll, lay.visible)

	out := p.barLines()
	out = append(out, heading...)
	out = append(out, "")
	out = append(out, rows...)
	out = append(out, padRows(lay.visible-len(rows))...)
	out = append(out, p.footerLines(
		readerCounter(r, start, lay.visible, total),
		ReaderLegendFor(r.structured, strings.HasPrefix(r.id, "req/")),
	)...)
	return out
}

// clampReaderCursor keeps the cursor's line on a numbered line.
//
// The document mixes the body's lines with the reader's furniture (a
// label row, a part separator), and the furniture is not numbered — so a
// cursor resting on it would show "L0" in the footer and a `>` in the
// gutter beside no number at all. The cursor is snapped to the nearest
// numbered line in the direction it came from.
func clampReaderCursor(r *reader, total int) {
	if total == 0 {
		r.line = 0
		return
	}
	if r.line >= total {
		r.line = total - 1
	}
	if r.line < 0 {
		r.line = 0
	}
	if r.isBodyLine(r.line) {
		return
	}
	// Snap forward first (the cursor arrived moving down), then back.
	for i := r.line + 1; i < total; i++ {
		if r.isBodyLine(i) {
			r.line = i
			return
		}
	}
	for i := r.line - 1; i >= 0; i-- {
		if r.isBodyLine(i) {
			r.line = i
			return
		}
	}
	r.line = 0
}

// isBodyLine reports whether the document's line at idx is numbered.
func (r *reader) isBodyLine(idx int) bool {
	if idx < 0 || idx >= len(r.numbers) {
		return false
	}
	return r.numbers[idx] > 0
}

// readerCounter is the reader's position row: the percentage, the
// cursor's line and the visible range, in /artifact's shape.
//
// The cursor's number is its line in the *body*, which is the number the
// gutter prints beside it — so `L4` in the footer and `4` in the gutter
// are the same line rather than two numbering schemes.
func readerCounter(r *reader, start, visible, total int) string {
	end := start + visible - 1
	if end > total {
		end = total
	}
	if total == 0 {
		return "  " + palette.DimGrayOpen + "(this body is empty)" + palette.DimGrayClose
	}
	bodyLine := readerBodyLineOf(r, r.line)
	return "  " + palette.DimGrayOpen +
		tuiviewer.PositionCounter(bodyLine, start, end, total) +
		palette.DimGrayClose
}

// bodyNumbering returns, for each rendered line, the number the gutter
// prints for it — or 0 for the reader's furniture (a label row or a part
// separator), which is not a line of the body.
func bodyNumbering(lines []string) []int {
	out := make([]int, len(lines))
	n := 0
	for i, l := range lines {
		if !readerLineIsBody(l) {
			continue
		}
		n++
		out[i] = n
	}
	return out
}

// readerBodyLineOf maps a document line index to the number the gutter
// prints for it, for the cursor.
func readerBodyLineOf(r *reader, idx int) int {
	if idx >= 0 && idx < len(r.numbers) {
		return r.numbers[idx]
	}
	return 0
}

// ReaderLegend is the reader's key hint.
func ReaderLegend(structured bool) string {
	return ReaderLegendFor(structured, false)
}

// ReaderLegendFor is the reader's key hint, including request-specific copy keys.
func ReaderLegendFor(structured bool, isRequest bool) string {
	// The keys move the line cursor; the wheel scrolls the window. Both
	// are named, because a reader that only mentions one leaves the other
	// to be discovered.
	//
	// Ordered most- to least-important, because legendRow drops entries
	// from the tail: the way out is named before the conveniences, so a
	// narrow terminal loses "y copy" rather than the only key that
	// returns to the list. It used to be last, which meant the 60-column
	// frame advertised four keys and omitted the exit.
	entries := []string{"↑/↓ line", "esc close", "g/G top/end"}
	if structured {
		entries = append(entries, "t format")
	}
	if isRequest {
		entries = append(entries, "c curl", "C md")
	}
	entries = append(entries, "y copy")
	return strings.Join(entries, "  ·  ")
}

// readerKey handles every key while a body is open.
//
// Esc and q back out one level — they close the reader, not the panel —
// because the reader was opened from the list and the list is where the
// user was. A key that closed the panel from here would lose the cursor
// position they had been reading through.
func (p *Panel) readerKey(key tea.Key) (bool, tea.Cmd) {
	if key.Mod == 0 {
		switch key.Text {
		case "t", "T":
			p.reader.cycleFormat()
			return true, nil
		case "y", "Y":
			if p.seams.Copy != nil {
				return true, p.seams.Copy(p.reader.copyText())
			}
			return true, nil
		case "c":
			if p.seams.Copy != nil && p.reader != nil {
				if r, ok := p.findRequestRecordByID(p.reader.id); ok {
					return true, p.seams.Copy(CurlCommand(r))
				}
			}
			return true, nil
		case "C":
			if p.seams.Copy != nil && p.reader != nil {
				if r, ok := p.findRequestRecordByID(p.reader.id); ok {
					return true, p.seams.Copy(MarkdownExchange(r))
				}
			}
			return true, nil
		}
	}
	visible := p.readerRows
	if visible < 1 {
		visible = p.layoutFor(p.height(), p.readerHeadingRows).visible
	}
	// Two different bounds, and confusing them is what pinned the cursor
	// to the top of a short body:
	//
	//	lastLine  the last line the cursor may reach (document length - 1)
	//	maxScroll the last window offset (document length - visible)
	//
	// A body shorter than the window has maxScroll 0 but lastLine > 0, so
	// clamping the cursor to maxScroll forbade moving it at all.
	doc := p.reader.numberedDoc(p.width())
	lastLine := 0
	if n := len(doc); n > 0 {
		lastLine = n - 1
	}
	step := visible - 1
	if step < 1 {
		step = 1
	}
	// The keys move the *cursor*; the wheel (Scroll) moves the window.
	// That split is /artifact's, and it is what makes the reader usable
	// for reading a specific line rather than only for scrolling prose.
	switch tuilib.ClassifyViewerNavKey(key) {
	case tuilib.NavUp:
		p.reader.line = clampOffset(p.reader.line-1, lastLine)
		return true, nil
	case tuilib.NavDown:
		p.reader.line = clampOffset(p.reader.line+1, lastLine)
		return true, nil
	case tuilib.NavPageUp:
		p.reader.line = clampOffset(p.reader.line-step, lastLine)
		return true, nil
	case tuilib.NavPageDown:
		p.reader.line = clampOffset(p.reader.line+step, lastLine)
		return true, nil
	case tuilib.NavHalfPageUp:
		p.reader.line = clampOffset(p.reader.line-step/2, lastLine)
		return true, nil
	case tuilib.NavHalfPageDown:
		p.reader.line = clampOffset(p.reader.line+step/2, lastLine)
		return true, nil
	case tuilib.NavTop:
		p.reader.line = 0
		return true, nil
	case tuilib.NavBottom:
		p.reader.line = lastLine
		return true, nil
	case tuilib.NavCancel:
		p.reader = nil
		return true, nil
	}
	// The bar is still drawn, so the keys it names must still work:
	// leaving the reader and acting on the key is what a click on the bar
	// does too.
	if p.chromeKey(key) {
		p.reader = nil
		return true, nil
	}
	return false, nil
}

// read asks the host for a row's full body.
//
// The pending ID is remembered so a second Enter on the same row does not
// queue a second read, and so an answer that arrives after the user moved
// on is recognised as stale rather than opening a reader nobody asked
// for. With no host seam the row's own detail is all there is, and the
// reader shows that rather than refusing to open.
func (p *Panel) read(id string, fallback []string) tea.Cmd {
	if p == nil {
		return nil
	}
	// A Requests row's body is the record the panel already holds, so it
	// opens without asking the host: the store is the body, and a second
	// read through a seam would be the same bytes with a round trip
	// between them.
	// A Context row's body is the provenance of its number, which the panel
	// builds from the state it already holds: the same reason the Requests
	// rows open without a seam.
	if strings.HasPrefix(id, "ctx/") {
		p.reader = newReader(id, ContextBody(id, p.ctxState))
		return nil
	}
	if seq, ok := requestRowSeq(id); ok {
		for _, r := range p.reqState.Records {
			if r.Seq == seq {
				p.reader = newReader(id, RequestsBody(r))
				return nil
			}
		}
		// The record aged out of the ring between the draw and the read.
		// Say so rather than opening an empty body.
		p.reader = newReader(id, Body{
			Title: id,
			Parts: []ReaderPart{{Text: "this exchange is no longer in the buffer"}},
		})
		return nil
	}
	if p.seams.Read == nil {
		p.reader = newReader(id, Body{Parts: []ReaderPart{{Text: strings.Join(fallback, "\n")}}})
		return nil
	}
	if p.readPending == id {
		return nil
	}
	p.readPending = id
	cmd := p.seams.Read(id)
	if cmd == nil {
		// A host that answers nothing is a host that has nothing: leaving
		// the ID pending would make every later Enter on the row a no-op.
		p.readPending = ""
	}
	return cmd
}

// ApplyReader opens the body the host answered with.
//
// The answer is accepted only when it is for the row the panel asked
// about. A panel with nothing pending has nothing in flight, so an answer
// arriving then is one the user has already moved past — and applying it
// would replace the body they are reading with a row they left.
func (p *Panel) ApplyReader(msg ReaderMsg) bool {
	if p == nil {
		return false
	}
	if msg.ID != p.readPending {
		return false
	}
	p.readPending = ""
	body := msg.Body
	switch {
	case msg.Err != nil:
		body = Body{
			Title: msg.ID,
			Parts: []ReaderPart{{Text: "could not read this body: " + msg.Err.Error()}},
		}
	case len(body.Parts) == 0:
		body = Body{
			Title: msg.ID,
			Parts: []ReaderPart{{Text: "(this row has no body stored)"}},
		}
	}
	p.reader = newReader(msg.ID, body)
	return true
}

// ReaderOpen reports whether a body is being read, for the host's tests.
func (p *Panel) ReaderOpen() bool {
	return p != nil && p.reader != nil
}
