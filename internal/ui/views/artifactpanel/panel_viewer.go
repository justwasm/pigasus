// panel_viewer.go — the /artifact detail viewer: its rows and its keys.
//
// # What it is
//
// The viewer is the reference's second layer: the selected artifact's text,
// full-screen, with a **line cursor** in a gutter, a counter under the window
// and a two-row footer. It is the same surface as the /changelog markdown
// viewer, but it has its own cursor.
//
// # 【一手】 the captured frames
//
// The shapes below come from a live capture of agy 1.5.17 over a real
// conversation (scratch/capture-artifact-viewer.sh, MODE=real). At the top of a
// 102-line file:
//
//	 artifact-picker-and-viewer.md
//	>   1      /artifact: the picker and the detail viewer
//	    2
//	    3     The previous change made /artifact stop answering "No artifacts"
//	  ...
//	   28      # │ Break                                                             │ Where
//	  [0%  L1  1-28/102]
//
//	  c comment · ctrl+g editor · t outline · l hide lines · / search · g/G top/bottom · ctrl+u/ctrl+d half page
//	  esc close
//
// After `j j j` the cursor has moved and the window has not:
//
//	>   4     unconditionally. That was only half the job: the panel drew a flat, static
//	  ...
//	  [0%  L4  1-28/102]
//
// After `G`:
//
//	> 102     byte for byte.
//	  [100%  L102  75-102/102]
//
// So the cursor and the window are **separate state**: `j`/`k` move the line
// cursor, `g`/`G` go to the document's ends, `ctrl+u`/`ctrl+d` move half a page
// (the footer's own words), and the window follows the cursor only when the
// cursor would leave it.
//
// # Deliberately not painted
//
// The footer's `c comment`, `t outline`, `l hide lines` and `/ search` are the
// reference's review and navigation features. The clone has no comment store,
// outline model or search over artifacts, so printing those keys would
// advertise actions that do nothing (AGENTS.md §13). Only the keys this panel
// implements are named; see footerKeys.
package artifactpanel

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	tuiviewer "github.com/justwasm/pigasus/internal/ui/foundations/viewer"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/palette"
)

// The gutter's geometry lives in internal/ui/foundations/viewer, because
// the /debug reader draws the same one and a second copy would drift.
//
// 【一手】 measured from the capture: the cursor column plus a right-aligned
// number in three columns —
//
//	>   1      （`>`, three spaces, `1`, then five spaces before the text）
//	> 102      （`>`, one space, `102`, then five spaces）
//	    2      （space, three spaces, `2`, then five spaces）
//
// so the text always starts at column 10. A number wider than three digits
// grows the cell rather than being truncated.
const (
	gutterWidth = tuiviewer.GutterNumberWidth
	gutterGap   = tuiviewer.GutterGap
)

// documentRows renders an artifact's text as the viewer's document: one row
// per line, each with its line number in the gutter.
//
// The cursor column is filled in by the caller (viewerLines), because the
// cursor is the panel's state and a document row is not.
func documentRows(body string, readable bool) []string {
	if !readable {
		return []string{"  " + palette.DimGrayOpen + "(cannot read this artifact)" + palette.DimGrayClose}
	}
	body = strings.TrimRight(body, "\n")
	if body == "" {
		return []string{"  " + palette.DimGrayOpen + "(this artifact is empty)" + palette.DimGrayClose}
	}
	src := strings.Split(body, "\n")
	rows := make([]string, 0, len(src))
	for i, line := range src {
		rows = append(rows, palette.DimGrayOpen+lineNumber(i+1, len(src))+palette.DimGrayClose+
			strings.Repeat(" ", gutterGap)+expandTabs(line))
	}
	return rows
}

// lineNumber renders a line number right-aligned in the gutter's number cell.
//
// The cell arithmetic is viewer.LineNumber's: the /debug reader draws the
// same gutter, and two implementations of "how wide is three digits" is
// how the two surfaces drift apart.
func lineNumber(n, total int) string {
	return tuiviewer.LineNumber(n, total)
}

// expandTabs expands a document row's tabs so the gutter's alignment survives
// them.
//
// A tab's width depends on the column it starts at, which the terminal would
// resolve differently once the gutter's SGR spans are in the row; expanding it
// here keeps the text at the column the gutter left for it.
func expandTabs(line string) string {
	return tuiviewer.ExpandTabs(line, 8)
}

// viewerLines renders the open detail viewer: the file name as the title, the
// window over the document with the cursor column filled in, the counter and
// the footer.
//
// The window is clamped and follows the cursor on every draw, so a file that
// shrank cannot leave the window past its end and the cursor cannot sit off
// screen.
func (p *Panel) viewerLines() []string {
	ref, ok := p.Selected()
	if !ok {
		// The list emptied under the cursor: fall back to the list rather
		// than draw a viewer over nothing.
		p.viewer = false
		p.window = 0
		p.line = 0
		return p.listLines()
	}
	spec := p.viewerSpec()
	p.clampCursor(spec)
	p.window = windowForCursor(p.window, p.line, spec.Visible, spec.Total)

	rows, start, end, clamped := tuiviewer.Window(spec, p.window)
	p.window = clamped

	lines := make([]string, 0, len(rows)+5)
	lines = append(lines, " "+panelTitleOpen+viewerTitle(ref)+panelTitleClose)
	for i, row := range rows {
		lines = append(lines, p.cursorMark(start-1+i)+row)
	}
	lines = append(lines,
		counter(p.line+1, start, end, spec.Total),
		"",
		footerKeys(),
		footerEsc(),
	)
	return lines
}

// viewerTitle is the viewer's title row text.
//
// 【一手】 the capture shows the **filename with its extension**, one leading
// space before it — not the stem the picker's row shows.
func viewerTitle(ref Artifact) string {
	if name := ref.FileName(); name != "" {
		return name
	}
	return ref.Path
}

// cursorMark is the viewer's cursor column.
//
// 【一手】 the capture's rows are `>   1     <text>` and `    2     <text>`:
// one `>` or space in the cursor column, the number right-aligned in three,
// five spaces, then the text. The title row carries a leading space of its own
// (see viewerLines), which is why the body rows do not.
func (p *Panel) cursorMark(docRow int) string {
	if docRow == p.line {
		return palette.BlueOpen + ">" + palette.BlueClose
	}
	return " "
}

// counter renders the line under the window.
//
// 【一手】 the capture's shape is `[<pct>%  L<cursor line>  <start>-<end>/<total>]`
// with two leading spaces, the percent tracking the window and `L` tracking the
// cursor. That split is the one thing that makes the `j j j` frame legible:
// `[0%  L4  1-28/102]`.
//
// The arithmetic is viewer.PositionCounter's — the /debug reader draws the same
// row — and only the styling and the leading spaces are this panel's.
func counter(cursorLine, start, end, total int) string {
	return "  " + palette.DimGrayOpen +
		tuiviewer.PositionCounter(cursorLine, start, end, total) +
		palette.DimGrayClose
}

// footerKeys is the footer's first row: the keys this panel implements.
//
// 【一手】 the reference's row is
// "c comment · ctrl+g editor · t outline · l hide lines · / search · g/G top/bottom · ctrl+u/ctrl+d half page".
// The clone has no comment store, outline or artifact search, so those four are
// absent here rather than printed dead — see the package comment. What remains
// is exactly what panel_keys.go and this file bind.
func footerKeys() string {
	return tuilib.PanelDotHint("  ", "ctrl+g", "editor", "g/G", "top/bottom",
		"ctrl+u/ctrl+d", "half page")
}

// footerEsc is the footer's second row.
func footerEsc() string {
	return tuilib.PanelDotHint("  ", "esc", "close")
}

// clampCursor keeps the line cursor inside the document.
func (p *Panel) clampCursor(spec tuiviewer.Spec) {
	if p.line < 0 {
		p.line = 0
	}
	if spec.Total > 0 && p.line >= spec.Total {
		p.line = spec.Total - 1
	}
	if spec.Total == 0 {
		p.line = 0
	}
}

// windowForCursor returns the scroll offset that keeps the cursor's line
// visible.
//
// 【一手】 the window follows the cursor only when the cursor would leave it —
// the `j j j` frame shows the cursor moving to L4 with the range still `1-28`.
// The scroll is the minimum that satisfies that, so moving inside the window
// does not scroll at all.
//
// The rule is viewer.WindowForCursor's, shared with the /debug reader.
func windowForCursor(window, line, visible, total int) int {
	return tuiviewer.WindowForCursor(window, line, visible, total)
}

// viewerKey handles a key while the detail viewer is open.
//
// It answers (handled, cmd). The viewer is modal: every key it receives is
// swallowed so a stray letter never reaches the prompt — except Ctrl+C, the
// global "leave" key, which is handed back to the host.
func (p *Panel) viewerKey(key tea.Key) (handled bool, cmd tea.Cmd) {
	if isCtrlC(key) {
		return false, nil
	}
	spec := p.viewerSpec()
	p.clampCursor(spec)
	visible := spec.Visible
	if visible < 1 {
		visible = 1
	}
	half := visible / 2
	if half < 1 {
		half = 1
	}

	// The footer's "ctrl+g editor" works from either layer.
	if isCtrlG(key) {
		if ref, ok := p.Selected(); ok && p.seams.OpenInEditor != nil {
			return true, p.seams.OpenInEditor(ref.Path)
		}
		return true, nil
	}

	switch {
	case key.Code == tea.KeyEsc:
		// Back to the list, keeping the picker's cursor on the row that
		// was opened.
		p.viewer = false
		p.window = 0
		p.line = 0
		return true, nil
	case key.String() == "ctrl+u":
		p.line -= half
	case key.String() == "ctrl+d":
		p.line += half
	case key.Code == tea.KeyUp || key.String() == "k":
		p.line--
	case key.Code == tea.KeyDown || key.String() == "j":
		p.line++
	case key.Code == tea.KeyHome || key.String() == "g":
		p.line = 0
	case key.Code == tea.KeyEnd || key.String() == "G" ||
		(key.Code == 'g' && key.Mod.Contains(tea.ModShift)):
		p.line = spec.Total - 1
	}

	p.clampCursor(spec)
	p.window = windowForCursor(p.window, p.line, visible, spec.Total)
	return true, nil
}
