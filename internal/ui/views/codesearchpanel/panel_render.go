// panel_render.go — the /codesearch panel's rows.
//
// The frame is the reference's own:
//
//	Code Search: needle
//	Found 5 matches in 4 files.  [Enter on row to open]
//	a.txt
//	────────────
//	  2 │ line2 needle here
//	  3 │ line4
//
// The title echoes the verbatim argument tail; the status line counts
// the matches and the files; the rows are a file header, a rule and
// the matching line with its two context lines; the footer is the
// keyboard legend.
//
// 【strings】 the usage line is exactly
// "Usage: /codesearch [-F|--literal] <query>" — the 1.4.8 string table
// carries no "f:<glob>" / "file:<glob>" literal at all. The clone still
// *accepts* those two filters, but it does not advertise them: the
// visible line matches the reference byte for byte.

package codesearchpanel

import (
	"fmt"

	"github.com/justwasm/pigasus/internal/features/tools/codesearch"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/internal/ui/views/linecomments"
	"github.com/justwasm/pigasus/palette"
)

// The panel's own SGR spans.
//
// The palette's colours follow the selected colour scheme, so the
// scheme-dependent ones are read from the palette at draw time.
const (
	// panelTitleOpen is the reference's own title span: bold on the
	// default foreground.
	panelTitleOpen = "\x1b[1m\x1b[39m"
	// panelTitleClose closes the title's bold span.
	panelTitleClose = "\x1b[0m"
)

// Lines renders the panel.
func (p *Panel) Lines() []string {
	if p == nil {
		return nil
	}
	if p.unsent != nil {
		// The "Unsent Comments" screen is up: it owns the rows.
		return p.unsentLines()
	}
	if p.viewer != nil {
		// A file is open: the viewer owns the rows.
		return p.viewerLines()
	}
	keyboard := tuilib.PanelKeyboardHint("↑/↓", "Navigate", "←/→", "Prev/Next File",
		"enter", "Open File", "ctrl+g", "Open in Editor", "esc", "Close Search")

	if p.query == "" {
		return []string{
			panelTitleOpen + "Code Search" + panelTitleClose + ":",
			palette.DimGrayOpen + "  Usage: /codesearch [-F|--literal] <query>" + palette.DimGrayClose,
			"",
			keyboard,
		}
	}

	title := panelTitleOpen + "Code Search" + panelTitleClose + ": " + p.args
	lines := []string{title}

	if p.err != "" {
		lines = append(lines, codesearch.ErrorOpen+"  Error: "+p.err+palette.DimGrayClose)
		return append(lines, "", keyboard)
	}

	state := codesearch.StateDone
	if p.search != nil {
		state = p.search.CurrentState()
	}
	hits := len(p.hits)
	files := len(p.files)

	statusLine, done := codesearch.StatusRow(state, hits, files, p.spinner)
	lines = append(lines, statusLine)
	if done {
		return append(lines, "", keyboard)
	}

	rows, first, last := p.rows()
	lines = append(lines, rows...)
	if first != 0 || last != hits {
		lines = append(lines, palette.DimGrayOpen+
			fmt.Sprintf("  [%d-%d of %d items]", first+1, last, hits)+palette.DimGrayClose)
	}
	return append(lines, "", keyboard)
}

// unsentLines renders the "Unsent Comments" screen.
//
// The screen's rows are the shared package's: the header, the pending
// comments in the reference's wire format and the hint that names the
// three keys that answer the screen.
func (p *Panel) unsentLines() []string {
	if p == nil || p.unsent == nil {
		return nil
	}
	return linecomments.ScreenRows(p.unsent.file, p.unsent.comments)
}

// rows renders the visible window and returns it with the visible item
// range [first, last) — the counter prints first+1 … last.
func (p *Panel) rows() (rows []string, first, last int) {
	width, height := p.size()
	start := p.windowStart()
	last, rows = codesearch.Fill(p.hits, start, p.cursor, codesearch.ListBudget(height), width)
	return rows, start, last
}

// windowStart clamps the stored window offset to a renderable item
// index.
func (p *Panel) windowStart() int {
	return codesearch.WindowStart(p.window, p.cursor, len(p.hits))
}

// follow re-anchors the window so the cursor is inside the rendered
// range.
func (p *Panel) follow() {
	width, height := p.size()
	p.window = codesearch.Follow(p.window, p.cursor, height, width, p.hits)
}
