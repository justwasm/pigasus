// fileviewer.go — the /codesearch panel's file viewer.
//
// Enter on a result opens the file: the panel shows the file's lines
// with the matching line highlighted and the window scrolled to it.
//
// The viewer is the reference's interactive file viewer: the keys are
// the pager's own, the cursor moves over the lines, a comment can be
// left on a line and the comments that were not sent yet are collected
// on the "Unsent Comments" screen when the user leaves.
//
// The panel owns the viewer's state: the file's lines, the cursor, the
// matching line, the scroll window, the comments and the two inline
// inputs. The host owns the disk: the panel asks for the file's lines
// through the ReadFile seam and never reads a file itself.

package codesearchpanel

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/features/tools/codesearch"
	"github.com/justwasm/pigasus/internal/ui/foundations/textbuffer"
	tuiviewer "github.com/justwasm/pigasus/internal/ui/foundations/viewer"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/internal/ui/views/linecomments"
	"github.com/justwasm/pigasus/palette"
)

// viewerFixedRows is the file viewer's non-line row count: the file
// header, the rule under it, the counter row, the blank and the
// keyboard legend.
const viewerFixedRows = 5

// fileViewer is the open file: the file's lines, the matching line, the
// cursor, the scroll window, the comments and the inline inputs.
type fileViewer struct {
	// path is the file's path relative to the search root.
	path string
	// lines are the file's lines.
	lines []string
	// line is the matching line, 1-based.
	line int
	// ms and me are the match span inside the matching line, in runes.
	ms, me int
	// offset is the first visible line, 1-based.
	offset int
	// cursor is the line the user's keys act on, 1-based.
	cursor int
	// err is the message shown when the file could not be read.
	err string
	// comments are the comments the user left on the file's lines.
	comments *linecomments.Store
	// comment is the inline comment editor's draft.
	comment textbuffer.Buffer
	// commentLine is the line the open comment editor writes to.
	commentLine int
	// commenting is true while the comment editor is up.
	commenting bool
	// search is the search prompt's query.
	search textbuffer.Buffer
	// searching is true while the search prompt is up.
	searching bool
	// searchLine is the line the last search found, 0 when none.
	searchLine int
	// searchMS and searchME are the search hit's span, in runes.
	searchMS, searchME int
	// notice is the message shown under the file's lines.
	notice string
}

// openViewer opens the highlighted hit's file in the viewer.
//
// The file's lines come from the host: the panel asks for them through
// the ReadFile seam and keeps them. The window opens on the matching
// line, and the cursor starts there.
//
// A panel without the seam does nothing: the Enter key is then the
// seam's absent answer, the same way every other absent seam is.
func (p *Panel) openViewer(hit codesearch.Hit) {
	if p == nil || p.seams.ReadFile == nil {
		return
	}
	v := &fileViewer{
		path:     hit.File,
		line:     hit.Line,
		ms:       hit.MS,
		me:       hit.ME,
		cursor:   hit.Line,
		comments: p.storeFor(hit.File),
		comment:  textbuffer.New(),
		search:   textbuffer.New(),
	}
	lines, message, ok := p.seams.ReadFile(hit.File)
	if ok {
		v.lines = lines
	} else {
		v.err = message
	}
	p.viewer = v
	v.scrollTo(p.viewerBudget())
}

// current is the highlighted hit.
func (p *Panel) current() codesearch.Hit {
	if p == nil || len(p.hits) == 0 {
		return codesearch.Hit{}
	}
	if p.cursor < 0 || p.cursor >= len(p.hits) {
		return p.hits[0]
	}
	return p.hits[p.cursor]
}

// viewerBudget is the number of file lines the window shows.
func (p *Panel) viewerBudget() int {
	_, height := p.size()
	b := height - viewerFixedRows
	if b < 1 {
		b = 1
	}
	return b
}

// maxOffset is the last window offset that still fills the window.
func (v *fileViewer) maxOffset(budget int) int {
	max := len(v.lines) - budget + 1
	if max < 1 {
		max = 1
	}
	return max
}

// scrollTo puts the matching line in the middle of the window.
func (v *fileViewer) scrollTo(budget int) {
	offset := v.line - budget/2
	if offset < 1 {
		offset = 1
	}
	if max := v.maxOffset(budget); offset > max {
		offset = max
	}
	v.offset = offset
}

// moveCursor moves the cursor by delta lines and re-anchors the window.
//
// The cursor is the line the user's keys act on — the line a comment is
// left on — and the window follows it: the cursor sits in the middle of
// the window, and the window's edges clamp it.
func (v *fileViewer) moveCursor(delta, budget int) {
	v.moveCursorTo(v.cursor+delta, budget)
}

// moveCursorTo puts the cursor on a line and re-anchors the window.
func (v *fileViewer) moveCursorTo(line, budget int) {
	if len(v.lines) == 0 {
		v.cursor = 0
		return
	}
	if line < 1 {
		line = 1
	}
	if line > len(v.lines) {
		line = len(v.lines)
	}
	v.cursor = line
	v.anchor(budget)
}

// anchor puts the window so the cursor is visible.
//
// The cursor sits in the middle of the window; the window's edges clamp
// it, so the cursor is on the first or the last row there.
func (v *fileViewer) anchor(budget int) {
	offset := v.cursor - budget/2
	if offset < 1 {
		offset = 1
	}
	if max := v.maxOffset(budget); offset > max {
		offset = max
	}
	v.offset = offset
}

// viewerKey owns the keys of the open file.
//
// The keys are the pager's own: the arrows move the cursor, pgup/pgdown
// page, g and shift+g jump to the ends, c leaves a comment on the
// cursor's line, d drops the comment that line carries, / searches the
// file, Ctrl+G opens the file in the editor and Esc goes back to the
// result list. Every other key is not owned — the host's global handlers
// still see it.
//
// The comment bindings are the letter with no modifier: Ctrl+D is the
// page-down half of the pager and must not delete a comment.
func (p *Panel) viewerKey(key tea.Key) (handled bool, cmd tea.Cmd, ok bool) {
	if p == nil || p.viewer == nil {
		return false, nil, false
	}
	v := p.viewer
	if v.commenting {
		return p.commentKey(key)
	}
	if v.searching {
		return p.searchKey(key)
	}
	switch {
	case plainLetter(key, 'c'):
		// c leaves a comment on the cursor's line.
		p.openComment()
		return true, nil, true
	case plainLetter(key, 'd'):
		// d drops the comment the cursor's line carries.
		p.deleteComment()
		return true, nil, true
	case key.Mod == 0 && key.Code == '/':
		// / searches the open file.
		p.openFind()
		return true, nil, true
	}
	if key.Code == 'g' && key.Mod.Contains(tea.ModCtrl) {
		// Ctrl+G opens the open file in the external editor, at the
		// cursor's line.
		return true, p.openEditor(v.path, v.cursor), true
	}
	switch tuilib.ClassifyListNavKey(key) {
	case tuilib.NavUp:
		v.moveCursor(-1, p.viewerBudget())
	case tuilib.NavDown:
		v.moveCursor(1, p.viewerBudget())
	case tuilib.NavPageUp:
		v.moveCursor(-p.viewerBudget(), p.viewerBudget())
	case tuilib.NavPageDown:
		v.moveCursor(p.viewerBudget(), p.viewerBudget())
	case tuilib.NavTop:
		v.moveCursorTo(1, p.viewerBudget())
	case tuilib.NavBottom:
		v.moveCursorTo(len(v.lines), p.viewerBudget())
	case tuilib.NavCancel:
		// Esc leaves the file: the result list is back, unless there
		// are comments that were not sent yet.
		return p.leaveViewer()
	default:
		return false, nil, false
	}
	return true, nil, true
}

// plainLetter reports whether the key is the given letter, pressed
// without a control or an alt modifier.
//
// Bubbletea reports a shifted letter two ways — the capital rune
// itself, or the small rune with the shift modifier — and both are the
// letter the viewer binds. A control or an alt modifier makes the key
// a command, not the letter.
func plainLetter(key tea.Key, r rune) bool {
	if key.Mod.Contains(tea.ModCtrl) || key.Mod.Contains(tea.ModAlt) {
		return false
	}
	return unicode.ToLower(key.Code) == unicode.ToLower(r)
}

// commentKey owns the keys of the inline comment editor.
//
// The editor is modal: it claims every key under it. Enter saves the
// comment, Esc cancels it, and Alt+Enter (or Shift+Enter) is the
// reference's newline (official-docs/cli/commands/codesearch.md:82).
func (p *Panel) commentKey(key tea.Key) (handled bool, cmd tea.Cmd, ok bool) {
	v := p.viewer
	if v == nil {
		return false, nil, false
	}
	switch key.Code {
	case tea.KeyEnter:
		p.saveComment()
		return true, nil, true
	case tea.KeyEsc:
		p.cancelComment()
		return true, nil, true
	}
	v.comment.HandleKey(key)
	return true, nil, true
}

// searchKey owns the keys of the search prompt.
//
// The prompt is modal: it claims every key under it. Enter runs the
// search, Esc cancels it.
func (p *Panel) searchKey(key tea.Key) (handled bool, cmd tea.Cmd, ok bool) {
	v := p.viewer
	if v == nil {
		return false, nil, false
	}
	switch key.Code {
	case tea.KeyEnter:
		p.runFind()
		return true, nil, true
	case tea.KeyEsc:
		p.cancelFind()
		return true, nil, true
	}
	v.search.HandleKey(key)
	return true, nil, true
}

// openComment opens the inline comment editor for the cursor's line.
//
// A line that already carries a comment is edited: the draft starts with
// the comment's text (the reference's "Add/edit comment on the current
// line", official-docs/cli/commands/diff.md:126).
func (p *Panel) openComment() {
	v := p.viewer
	if v == nil {
		return
	}
	v.commentLine = v.cursor
	v.comment.Reset()
	if c, ok := v.comments.At(v.cursor); ok {
		v.comment.SetValue(c.Body)
	}
	v.commenting = true
}

// saveComment stores the draft against the commented line.
//
// An empty draft is not a comment: the editor closes and the line
// carries what it carried before.
func (p *Panel) saveComment() {
	v := p.viewer
	if v == nil {
		return
	}
	body := strings.TrimSpace(v.comment.Value())
	v.commenting = false
	v.comment.Reset()
	if body == "" {
		return
	}
	v.comments.Add(v.commentLine, body)
}

// cancelComment closes the comment editor and drops the draft.
func (p *Panel) cancelComment() {
	v := p.viewer
	if v == nil {
		return
	}
	v.commenting = false
	v.comment.Reset()
}

// deleteComment drops the comment the cursor's line carries.
func (p *Panel) deleteComment() {
	v := p.viewer
	if v == nil {
		return
	}
	v.comments.Delete(v.cursor)
}

// openFind opens the search prompt.
func (p *Panel) openFind() {
	v := p.viewer
	if v == nil {
		return
	}
	v.search.Reset()
	v.searching = true
}

// cancelFind closes the search prompt without searching.
func (p *Panel) cancelFind() {
	v := p.viewer
	if v == nil {
		return
	}
	v.searching = false
	v.search.Reset()
}

// runFind moves the cursor to the next line that carries the query.
//
// The search starts after the cursor and wraps around the file. A query
// that matches nothing leaves the cursor where it is and says so.
func (p *Panel) runFind() {
	v := p.viewer
	if v == nil {
		return
	}
	query := strings.TrimSpace(v.search.Value())
	v.searching = false
	v.search.Reset()
	if query == "" {
		return
	}
	line, ms, me, ok := v.find(query)
	if !ok {
		v.notice = "No match for " + query
		return
	}
	v.notice = ""
	v.searchLine = line
	v.searchMS, v.searchME = ms, me
	v.moveCursorTo(line, p.viewerBudget())
}

// find returns the next line that carries the query, with the span of
// the match in runes.
//
// The search starts after the cursor and wraps around the file.
func (v *fileViewer) find(query string) (line, ms, me int, ok bool) {
	n := len(v.lines)
	if n == 0 {
		return 0, 0, 0, false
	}
	needle := strings.ToLower(query)
	for i := 1; i <= n; i++ {
		idx := (v.cursor - 1 + i) % n
		text := v.lines[idx]
		col := strings.Index(strings.ToLower(text), needle)
		if col < 0 {
			continue
		}
		return idx + 1,
			utf8.RuneCountInString(text[:col]),
			utf8.RuneCountInString(text[:col+len(query)]),
			true
	}
	return 0, 0, 0, false
}

// span is the highlight of a line: the search hit's span, the search
// match's span, or none.
func (v *fileViewer) span(line int) (ms, me int, isMatch int) {
	if v.searchLine == line {
		return v.searchMS, v.searchME, 1
	}
	if v.line == line {
		return v.ms, v.me, 1
	}
	return 0, 0, 0
}

// inputRows renders the viewer's inline input: the comment editor or the
// search prompt.
//
// The rows are the input's own — the panel owns them — and the buffer
// draws the text with the cursor in it.
func (v *fileViewer) inputRows() []string {
	switch {
	case v.commenting:
		rows := []string{palette.DimGrayOpen +
			fmt.Sprintf("  Comment on line %d:", v.commentLine) + palette.DimGrayClose}
		return append(rows, v.comment.Rows("  > ")...)
	case v.searching:
		rows := []string{palette.DimGrayOpen + "  Search:" + palette.DimGrayClose}
		return append(rows, v.search.Rows("  > ")...)
	}
	return nil
}

// viewerLegend is the reference's own keyboard legend for the file
// viewer.
//
// The doc gives the legend verbatim
// (official-docs/cli/commands/codesearch.md:74-78):
//
//	↑/↓ scroll · pgup/pgdown page · shift+g bottom · g top · c comment · ctrl+g editor · / search
//
// The legend is the panel's own: it is the one place the viewer's keys
// are written down.
func viewerLegend() string {
	return tuilib.PanelDotHint("  ", "↑/↓", "scroll", "pgup/pgdown", "page",
		"shift+g", "bottom", "g", "top", "c", "comment", "ctrl+g", "editor",
		"/", "search")
}

// viewerLines renders the open file.
//
// The rows are the result list's own: the file header, the rule under
// it and the numbered lines, with the cursor's line marked, the match
// highlighted and the comment marker in the gutter.
func (p *Panel) viewerLines() []string {
	v := p.viewer
	keyboard := viewerLegend()
	width, _ := p.size()
	lines := []string{codesearch.HeaderRow(v.path), codesearch.RuleRow(width)}
	if v.err != "" {
		return append(lines, palette.DimGrayOpen+"  "+v.err+palette.DimGrayClose, "", keyboard)
	}
	input := v.inputRows()
	budget := p.viewerBudget() - len(input)
	if budget < 1 {
		budget = 1
	}
	start := v.offset
	if start > v.maxOffset(budget) {
		start = v.maxOffset(budget)
	}
	if start < 1 {
		start = 1
	}
	end := start + budget - 1
	if end > len(v.lines) {
		end = len(v.lines)
	}
	for i := start; i <= end; i++ {
		ms, me, isMatch := v.span(i)
		isCursor := 0
		if i == v.cursor {
			isCursor = 1
		}
		marker := ""
		if _, ok := v.comments.At(i); ok {
			marker = linecomments.Marker
		}
		lines = append(lines, codesearch.MarkedRow(i, v.lines[i-1], ms, me, isMatch, isCursor, width, marker))
	}
	lines = append(lines, input...)
	if v.notice != "" {
		lines = append(lines, palette.DimGrayOpen+"  "+v.notice+palette.DimGrayClose)
	}
	if end < len(v.lines) || start > 1 {
		lines = append(lines, tuiviewer.Counter(start, end, len(v.lines)))
	}
	return append(lines, "", keyboard)
}
