// slash_codesearch_render.go — results list rendering, match styling,
// line numbering, and footer for the /codesearch panel.

package cliapp

import (
	"fmt"
	"regexp"

	"github.com/justwasm/pigasus/internal/codesearch"
)

const (
	csWidthInset  = codesearch.WidthInset
	csFixedRows   = codesearch.FixedRows
	csHeaderOpen  = codesearch.HeaderOpen
	csHeaderReset = codesearch.HeaderReset
	csErrorOpen   = codesearch.ErrorOpen
)

var csSpinnerFrames = codesearch.SpinnerFrames

// csHit is one matching line plus its neighbouring context lines.
//
// The shape lives in internal/codesearch; this alias keeps the panel's
// own vocabulary.
type csHit = codesearch.Hit

func appendFileHits(hits []csHit, rel, content string, re *regexp.Regexp) []csHit {
	return codesearch.AppendFileHits(hits, rel, content, re, csResultCap)
}

func csFill(hits []csHit, start, cursor, budget, width int) (int, []string) {
	return codesearch.Fill(hits, start, cursor, budget, width)
}

func csFit(text string, ms, me, w int) (string, int, int) {
	return codesearch.Fit(text, ms, me, w)
}

// codesearchPanelLines renders the /codesearch panel.
func (m *runMode) codesearchPanelLines() []string {
	cs := &m.panel.cs
	keyboard := panelKeyboardHint("↑/↓", "Navigate", "←/→", "Prev/Next File", "enter", "Open File",
		"ctrl+g", "Open in Editor", "esc", "Close Search")

	if cs.query == "" {
		// Cycle 351: the reference's usage line is exactly
		// "Usage: /codesearch [-F|--literal] <query>" — the 1.4.8 string
		// table carries no "f:<glob>" / "file:<glob>" literal at all
		// (【strings】). The clone still *accepts* those two filters in
		// internal/codesearch, but they are not advertised: the visible
		// line matches the reference byte for byte.
		return []string{
			panelTitleOpen + "Code Search" + panelTitleClose + ":",
			cDimGrayOpen + "  Usage: /codesearch [-F|--literal] <query>" + cDimGrayClose,
			"",
			keyboard,
		}
	}

	title := panelTitleOpen + "Code Search" + panelTitleClose + ": " + cs.args
	lines := []string{title}

	if cs.err != "" {
		lines = append(lines, csErrorOpen+"  Error: "+cs.err+cDimGrayClose)
		return append(lines, "", keyboard)
	}

	state := codesearch.StateDone
	if cs.search != nil {
		state = cs.search.CurrentState()
	}
	hits := len(cs.hits)
	files := len(cs.files)

	statusLine, done := codesearch.StatusRow(state, hits, files, cs.spinner)
	lines = append(lines, statusLine)
	if done {
		return append(lines, "", keyboard)
	}

	rows, first, last := m.codesearchRows()
	lines = append(lines, rows...)
	if first != 0 || last != hits {
		lines = append(lines, cDimGrayOpen+
			fmt.Sprintf("  [%d-%d of %d items]", first+1, last, hits)+cDimGrayClose)
	}
	return append(lines, "", keyboard)
}

// codesearchRows renders the visible window and returns it with the visible
// item range [first, last) — the counter prints first+1 … last.
func (m *runMode) codesearchRows() (rows []string, first, last int) {
	hits := m.panel.cs.hits
	start := m.csWindowStart()
	last, rows = csFill(hits, start, m.panel.cursor, codesearch.ListBudget(m.height), m.width)
	return rows, start, last
}

// csWindowStart clamps the stored window offset to a renderable item index.
func (m *runMode) csWindowStart() int {
	return codesearch.WindowStart(m.panel.window, m.panel.cursor, len(m.panel.cs.hits))
}

// csFollow re-anchors the window so the cursor is inside the rendered range.
func (m *runMode) csFollow() {
	m.panel.window = codesearch.Follow(m.panel.window, m.panel.cursor, m.height, m.width, m.panel.cs.hits)
}
