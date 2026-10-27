package cliapp

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/internal/tuilib"
	"github.com/justwasm/pigasus/internal/tuitools"
	"github.com/justwasm/pigasus/internal/tuiviewport"
)

const (
	pinnedPromptJumpHint = tuiviewport.PinnedPromptJumpHint
	selHighlightOpen     = tuiviewport.SelHighlightOpen
	selHighlightClose    = tuiviewport.SelHighlightClose
)

func (m *runMode) transcriptCellAt(x, y int) (row, col int, ok bool) {
	if y < 0 || x < 0 {
		return 0, 0, false
	}
	avail := m.viewportHeight()
	if avail <= 0 {
		return y, x, true
	}
	if y >= avail || x >= m.width {
		return 0, 0, false
	}
	return y, x, true
}

func (m *runMode) selectionRange() (sr, sc, er, ec int, active bool) {
	return tuiviewport.SelectionRange(m.selStartRow, m.selStartCol, m.selEndRow, m.selEndCol, m.selDragging)
}

func (m *runMode) selectionText() string {
	if !m.altScreen {
		return ""
	}
	sr, sc, er, ec, ok := m.selectionRange()
	if !ok {
		return ""
	}
	return tuiviewport.SelectionText(m.transcriptWindow(), sr, sc, er, ec)
}

func (m *runMode) copySelectionCmd(text string) tea.Cmd {
	if text == "" {
		return nil
	}
	return tea.SetClipboard(text)
}

func applySelectionHighlight(row string, sr, sc, er, ec, rowIdx int) string {
	return tuiviewport.ApplySelectionHighlight(row, sr, sc, er, ec, rowIdx)
}

func (m *runMode) selectionPass(rows []string) []string {
	sr, sc, er, ec, ok := m.selectionRange()
	return tuiviewport.SelectionPass(rows, sr, sc, er, ec, ok)
}

func (m *runMode) scrollBy(delta int) {
	if delta == 0 {
		return
	}
	m.autoScroll = false
	m.scroll += delta
	if m.scroll < 0 {
		m.scroll = 0
	}
}

// isUserPromptRow reports whether row is a row that begins a user prompt.
func isUserPromptRow(row string) bool {
	return tuiviewport.IsUserPromptRow(row)
}

// stickyUserPromptRow returns the index (in the full rows array) of the
// user-prompt row that should anchor the sticky header, or -1 if none.
func (m *runMode) stickyUserPromptRow() int {
	return tuiviewport.StickyUserPromptRow(m.userPromptRows, m.autoScroll, m.scroll)
}

// jumpToPinnedPrompt scrolls the viewport so the producing user-prompt row lands at top.
func (m *runMode) jumpToPinnedPrompt() bool {
	if m.pinnedPromptRowIdx < 0 {
		return false
	}
	m.autoScroll = false
	m.scroll = m.pinnedPromptRowIdx
	return true
}

// renderPinnedPrompt wraps the producing user-prompt row in the pinned-prompt header style.
func renderPinnedPrompt(promptRow string, width int) string {
	return tuiviewport.RenderPinnedPrompt(promptRow, width)
}

// gotoTop / gotoBottom are the recovered ctrl+home / ctrl+end bindings.
func (m *runMode) gotoTop() {
	m.autoScroll = false
	m.scroll = 0
}

func (m *runMode) gotoBottom() {
	m.autoScroll = true
	m.scroll = 1 << 30
}

// pageRows is the recovered page-scroll distance.
func (m *runMode) pageRows() int {
	return clampInt(m.vpRows-1, 1, 1<<20)
}

func (m *runMode) halfPageRows() int {
	return clampInt(m.vpRows/2, 1, 1<<20)
}

// isScrollMsg reports whether msg can only move the viewport and never mutates the transcript.
func isScrollMsg(msg tea.Msg) bool {
	return tuiviewport.IsScrollMsg(msg)
}

// handleScrollKey consumes the recovered transcript scroll bindings.
func (m *runMode) handleScrollKey(key tea.Key) bool {
	if !m.altScreen {
		return false
	}
	switch {
	case key.Code == tea.KeyPgUp:
		m.scrollBy(-m.pageRows())
		return true
	case key.Code == tea.KeyPgDown:
		m.scrollBy(m.pageRows())
		return true
	case key.Code == tea.KeyUp && key.Mod.Contains(tea.ModShift):
		m.scrollBy(-m.halfPageRows())
		return true
	case key.Code == tea.KeyDown && key.Mod.Contains(tea.ModShift):
		m.scrollBy(m.halfPageRows())
		return true
	case key.Code == tea.KeyHome && key.Mod.Contains(tea.ModCtrl):
		m.gotoTop()
		return true
	case key.Code == tea.KeyEnd && key.Mod.Contains(tea.ModCtrl):
		m.gotoBottom()
		return true
	}
	return false
}

// handleMouseWheel scrolls the transcript on a wheel event.
func (m *runMode) handleMouseWheel(msg tea.MouseMsg) bool {
	if !m.altScreen {
		return false
	}
	if m.handlePickerWheel(msg.Mouse()) {
		return true
	}
	switch msg.Mouse().Button {
	case tea.MouseWheelUp:
		m.scrollBy(-mouseWheelRows)
		return true
	case tea.MouseWheelDown:
		m.scrollBy(mouseWheelRows)
		return true
	}
	return false
}

type wrapSpan = tuiviewport.WrapSpan
type wrapSlot = tuiviewport.WrapSlot
type composedRows = tuiviewport.ComposedRows

const (
	mouseWheelRows   = 3
	viewOverflowRows = tuiviewport.ViewOverflowRows
)

func viewRows(s string) int                    { return tuilib.ViewRows(s) }
func fitWidth(s string, w int) string          { return tuilib.FitWidth(s, w) }
func fitLines(s string, w int) string          { return tuilib.FitLines(s, w) }
func wrapRows(line string, width int) []string { return tuilib.WrapRows(line, width) }
func cellByteIndex(s string) []int             { return tuilib.CellByteIndex(s) }
func skipEscape(s string, i int) int           { return tuilib.SkipEscape(s, i) }
func padTo(s string, w int) string             { return tuilib.PadTo(s, w) }
func clampInt(v, lo, hi int) int               { return tuilib.ClampInt(v, lo, hi) }

func screenRows(entry string) int {
	return tuiviewport.ScreenRows(entry)
}

func screenRowCounts(rows []string) []int {
	return tuiviewport.ScreenRowCounts(rows)
}

func clipToScreenRows(visible []string, screenCounts []int, scrollOff, maxRows int) []string {
	return tuiviewport.ClipToScreenRows(visible, screenCounts, scrollOff, maxRows)
}

func clipToScreenRowsPrefix(visible []string, prefix []int, scrollOff, maxRows int) []string {
	return tuiviewport.ClipToScreenRowsPrefix(visible, prefix, scrollOff, maxRows)
}

func (m *runMode) scrollHeaderRows(width int) []string {
	var b strings.Builder
	b.WriteString("\n")
	renderBannerWithTheme(&b, m.seasonal, m.email, m.model, m.tier, m.cwd)
	if m.agentWarning != "" {
		b.WriteString("\n")
		b.WriteString(renderAgentWarning(m.agentWarning))
	}
	if m.fallbackFrom != "" && m.fallbackTo != "" {
		b.WriteString("\n")
		b.WriteString(renderWarning(m.fallbackFrom, m.fallbackTo))
	}
	var rows []string
	for _, line := range strings.Split(b.String(), "\n") {
		if width > 0 {
			rows = append(rows, wrapRows(line, width)...)
		} else {
			rows = append(rows, line)
		}
	}
	return rows
}

func (m *runMode) wrappedResponse(width int) []string {
	if width == m.width {
		if m.rowsCacheDirty || !m.rowsCache.Built || m.rowsCache.Width != width {
			if m.rowsCache.Rebuild(m.response, width) {
				m.rowsGen++
			}
			m.rowsCacheDirty = false
		}
		return m.rowsCache.Rows
	}
	if !m.rowsCacheN1.Built || m.rowsCacheN1.Width != width {
		if m.rowsCacheN1.Rebuild(m.response, width) {
			m.rowsGen++
		}
	}
	return m.rowsCacheN1.Rows
}

func (m *runMode) wrappedResponseCounts(width int) []int {
	_ = m.wrappedResponse(width)
	if width == m.width {
		return m.rowsCache.Counts
	}
	return m.rowsCacheN1.Counts
}

func (m *runMode) liveRows(width int) []string {
	if !m.streaming {
		return nil
	}
	var rows []string
	if m.thinking && m.activeTool == "" {
		rows = append(rows, thinkingLiveLine(time.Since(m.thinkingSince)))
	}
	line := m.spinner.View()
	phrase := "Generating"
	if m.anyToolRun {
		phrase = "Working"
	}
	if m.activeTool != "" {
		phrase = m.activeTool
	}
	line += " " + tuitools.SpinnerText(phrase, m.spinFrame)
	return append(rows, wrapRows(line, width)...)
}

func (m *runMode) transcriptRows(width int) []string {
	if m.picker.Visible() {
		return nil
	}
	rows := m.scrollHeaderRows(width)
	for _, line := range m.response {
		rows = append(rows, wrapRows(line, width)...)
	}
	if m.streaming && m.thinking && m.activeTool == "" {
		rows = append(rows, thinkingLiveLine(time.Since(m.thinkingSince)))
	}
	if m.streaming {
		line := m.spinner.View()
		if m.activeTool != "" {
			line += " " + m.activeTool
		} else {
			line += " Generating..."
		}
		rows = append(rows, wrapRows(line, width)...)
	}
	return rows
}

func (m *runMode) syncRowsView() ([]string, []string, []string) {
	header := m.scrollHeaderRows(m.width)
	resp := m.wrappedResponse(m.width)
	live := m.liveRows(m.width)
	respCounts := m.wrappedResponseCounts(m.width)
	if !m.rowsView.Fresh(m.rowsGen, m.width, header, live) {
		m.rowsView.Rebuild(m.rowsGen, m.width, header, resp, live, respCounts)
	}
	m.userPromptRows = m.rowsView.Prompt
	return m.rowsView.Rows, header, resp
}

func (m *runMode) updateStickyPrompt(header, resp []string, avail int) int {
	m.stickyBarRow = ""
	m.pinnedPromptRowIdx = -1
	m.pinnedPromptText = ""
	if idx := m.stickyUserPromptRow(); idx >= 0 {
		respIdx := idx - len(header)
		if respIdx >= 0 && respIdx < len(resp) {
			if m.altScreen {
				m.pinnedPromptRowIdx = idx
				m.pinnedPromptText = renderPinnedPrompt(resp[respIdx], m.width)
			} else {
				m.stickyBarRow = resp[respIdx]
			}
		}
	}
	availScroll := avail
	if m.altScreen && m.pinnedPromptText != "" {
		availScroll -= 1
	} else if m.stickyBarRow != "" {
		availScroll -= 2
	}
	return availScroll
}

func (m *runMode) computeVisibleWindow(rows []string, availScroll int) []string {
	visible, scroll, auto := tuiviewport.ComputeVisibleWindow(rows, m.rowsView.Prefix, m.scroll, availScroll, m.autoScroll)
	m.scroll = scroll
	m.autoScroll = auto
	return visible
}

func (m *runMode) formatVisibleRows(visible []string, effective int) []string {
	return tuiviewport.FormatVisibleRows(visible, effective, m.pinnedPromptText, m.stickyBarRow, m.altScreen)
}

func (m *runMode) viewportHeight() int {
	if m.height <= 0 {
		return 0
	}
	body, _ := m.viewBody(nil)
	extra := viewRows(body)
	budget := m.height
	if m.helpMode {
		extra -= viewRows(renderHelp(m.helpTab, m.model, m.tier, m.cwd, m.email, m.conversationID, m))
		budget += viewOverflowRows
	}
	avail := budget - extra
	if avail < 1 {
		avail = 1
	}
	return avail
}

func (m *runMode) transcriptWindow() []string {
	avail := m.viewportHeight()
	if avail <= 0 {
		m.vpRows = 0
		return m.transcriptRows(0)
	}
	rows, header, resp := m.syncRowsView()
	availScroll := m.updateStickyPrompt(header, resp, avail)
	if len(rows) <= availScroll {
		m.scroll = 0
		m.autoScroll = true
		return rows
	}
	visible := m.computeVisibleWindow(rows, availScroll)
	out := m.formatVisibleRows(visible, m.width)
	m.vpRows = avail
	return out
}
