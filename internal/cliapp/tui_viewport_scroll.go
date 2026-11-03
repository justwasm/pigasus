package cliapp

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	tuiviewport "github.com/justwasm/pigasus/internal/ui/foundations/viewport"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/internal/ui/tuitools"
	"github.com/justwasm/pigasus/palette"
)

const selHighlightOpen = tuiviewport.SelHighlightOpen

func (m *runMode) selectionText() string {
	if !m.altScreen {
		return ""
	}
	sr, sc, er, ec, ok := tuiviewport.SelectionRange(m.store.SelStartRow, m.store.SelStartCol, m.store.SelEndRow, m.store.SelEndCol, m.store.SelDragging)
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

func (m *runMode) selectionPass(rows []string) []string {
	sr, sc, er, ec, ok := tuiviewport.SelectionRange(m.store.SelStartRow, m.store.SelStartCol, m.store.SelEndRow, m.store.SelEndCol, m.store.SelDragging)
	return tuiviewport.SelectionPass(rows, sr, sc, er, ec, ok)
}

func (m *runMode) hasSelection() bool {
	_, _, _, _, ok := tuiviewport.SelectionRange(m.store.SelStartRow, m.store.SelStartCol, m.store.SelEndRow, m.store.SelEndCol, m.store.SelDragging)
	return ok || m.store.SelDragging
}

func (m *runMode) scrollBy(delta int) {
	if delta == 0 {
		return
	}
	if delta > 0 && m.autoScroll {
		// Already parked at the bottom; continuing to scroll downwards cannot
		// scroll past the end of the transcript. Keep autoScroll active so
		// sticky headers never flash during downward inertia scroll.
		return
	}
	oldScroll := m.scroll
	oldAutoScroll := m.autoScroll
	m.scroll += delta
	if m.scroll < 0 {
		m.scroll = 0
		m.autoScroll = false
	} else if m.vpRows > 0 && len(m.rowsView.Rows) > 0 {
		maxScroll := len(m.rowsView.Rows) - m.vpRows
		if maxScroll < 0 {
			maxScroll = 0
		}
		if m.scroll >= maxScroll {
			m.scroll = maxScroll
			m.autoScroll = true
		} else {
			m.autoScroll = false
		}
	} else {
		m.autoScroll = false
	}
	if m.scroll != oldScroll || m.autoScroll != oldAutoScroll {
		m.viewCacheValid = false
	}
}

// jumpToPinnedPrompt scrolls the viewport so the producing user-prompt row lands at top.
func (m *runMode) jumpToPinnedPrompt() bool {
	if m.pinnedPromptRowIdx < 0 {
		return false
	}
	m.autoScroll = false
	rows, _, _ := m.syncRowsView()
	m.scroll = tuiviewport.PromptTurnStart(rows, m.pinnedPromptRowIdx)
	m.viewCacheValid = false
	return true
}

// gotoTop / gotoBottom are the recovered ctrl+home / ctrl+end bindings.
func (m *runMode) gotoTop() {
	if m.scroll == 0 && !m.autoScroll {
		return
	}
	m.autoScroll = false
	m.scroll = 0
	m.viewCacheValid = false
}

func (m *runMode) gotoBottom() {
	if m.autoScroll {
		return
	}
	m.autoScroll = true
	m.scroll = 1 << 30
	m.viewCacheValid = false
}

// handleScrollKey consumes the recovered transcript scroll bindings.
//
// The page-scroll distances are the recovered ones: a page is the
// viewport's height less one row, and a half page is half of it.
func (m *runMode) handleScrollKey(key tea.Key) bool {
	if !m.altScreen {
		return false
	}
	switch {
	case key.Code == tea.KeyPgUp:
		m.scrollBy(-tuilib.ClampInt(m.vpRows-1, 1, 1<<20))
		return true
	case key.Code == tea.KeyPgDown:
		m.scrollBy(tuilib.ClampInt(m.vpRows-1, 1, 1<<20))
		return true
	case key.Code == tea.KeyUp && key.Mod.Contains(tea.ModShift):
		m.scrollBy(-tuilib.ClampInt(m.vpRows/2, 1, 1<<20))
		return true
	case key.Code == tea.KeyDown && key.Mod.Contains(tea.ModShift):
		m.scrollBy(tuilib.ClampInt(m.vpRows/2, 1, 1<<20))
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
	if m.picker.HandleWheel(msg.Mouse()) {
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

type wrapSlot = tuiviewport.WrapSlot
type composedRows = tuiviewport.ComposedRows

const (
	mouseWheelRows   = 3
	viewOverflowRows = tuiviewport.ViewOverflowRows
)

func (m *runMode) currentHeaderKey(width int) headerCacheKey {
	return headerCacheKey{
		width:        width,
		seasonal:     m.seasonal,
		email:        m.email,
		model:        m.model,
		tier:         m.tier,
		cwd:          m.cwd,
		agentWarning: m.agentWarning,
		fallbackFrom: m.fallbackFrom,
		fallbackTo:   m.fallbackTo,
	}
}

func (m *runMode) scrollHeaderRows(width int) []string {
	key := m.currentHeaderKey(width)
	if m.cachedHeaderKey == key && len(m.cachedHeaderRows) > 0 && !m.rowsCacheDirty {
		return m.cachedHeaderRows
	}
	var b strings.Builder
	b.WriteString("\n")
	renderBannerWithTheme(&b, m.seasonal, m.email, m.model, m.tier, m.cwd)
	if m.agentWarning != "" {
		b.WriteString("\n")
		b.WriteString(tuilib.RenderAgentWarning(m.agentWarning))
	}
	if m.fallbackFrom != "" && m.fallbackTo != "" {
		b.WriteString("\n")
		b.WriteString(tuilib.RenderWarning(m.fallbackFrom, m.fallbackTo))
	}
	var rows []string
	for _, line := range strings.Split(b.String(), "\n") {
		if width > 0 {
			rows = append(rows, tuilib.WrapRows(line, width)...)
		} else {
			rows = append(rows, line)
		}
	}
	m.cachedHeaderRows = rows
	m.cachedHeaderKey = key
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
	line := m.spinner.View()
	phrase := "Generating"
	if m.anyToolRun {
		phrase = "Working"
	}
	if m.thinking && m.activeTool == "" {
		if m.liveThinkingSnippet != "" {
			line += " " + palette.SpinnerBaseOpen + m.liveThinkingSnippet + "\x1b[m"
		} else {
			line += " " + tuitools.SpinnerText("Thinking", m.spinFrame)
		}
	} else if m.activeTool != "" {
		line += " " + m.activeTool
	} else {
		line += " " + tuitools.SpinnerText(phrase, m.spinFrame)
	}
	if width > 0 {
		line = tuilib.Truncate(line, width, "...")
	}
	rows = append(rows, line)
	if m.activeTip != "" {
		tipLine := tuitools.FormatTip(m.activeTip)
		if tipLine != "" {
			if width > 0 {
				tipLine = tuilib.Truncate(tipLine, width, "...")
			}
			rows = append(rows, tipLine)
		}
	}
	return rows
}

func (m *runMode) transcriptRows(width int) []string {
	if m.picker.Visible() {
		return nil
	}
	header := m.scrollHeaderRows(width)
	rows := make([]string, 0, len(header)+len(m.response))
	rows = append(rows, header...)
	for _, line := range m.response {
		rows = append(rows, tuilib.WrapRows(line, width)...)
	}
	if m.streaming {
		line := m.spinner.View()
		phrase := "Generating"
		if m.anyToolRun {
			phrase = "Working"
		}
		if m.thinking && m.activeTool == "" {
			if m.liveThinkingSnippet != "" {
				line += " " + m.liveThinkingSnippet
			} else {
				line += " Thinking..."
			}
		} else if m.activeTool != "" {
			line += " " + m.activeTool
		} else {
			line += " " + phrase + "..."
		}
		if width > 0 {
			line = tuilib.Truncate(line, width, "...")
		}
		rows = append(rows, line)
		if m.activeTip != "" {
			tipLine := tuitools.FormatTip(m.activeTip)
			if tipLine != "" {
				if width > 0 {
					tipLine = tuilib.Truncate(tipLine, width, "...")
				}
				rows = append(rows, tipLine)
			}
		}
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

func (m *runMode) updateStickyPrompt(rows, header, resp []string, avail int) int {
	m.stickyBarRow = ""
	m.pinnedPromptRowIdx = -1
	m.pinnedPromptText = ""
	if !m.altScreen || m.autoScroll || avail <= 2 {
		return avail
	}
	if idx := tuiviewport.StickyUserPromptRow(m.userPromptRows, m.autoScroll, m.scroll); idx >= 0 {
		if !tuiviewport.PromptTurnVisible(rows, idx, m.scroll, m.userPromptRows) {
			respIdx := idx - len(header)
			if respIdx >= 0 && respIdx < len(resp) {
				m.pinnedPromptRowIdx = idx
				m.pinnedPromptText = tuiviewport.RenderPinnedPrompt(resp[respIdx], m.width)
			}
		}
	}
	availScroll := avail
	if m.pinnedPromptText != "" {
		availScroll -= 2
	}
	if availScroll < 1 {
		availScroll = 1
	}
	return availScroll
}

func (m *runMode) computeVisibleWindow(rows []string, availScroll int) []string {
	visible, scroll, auto := tuiviewport.ComputeVisibleWindow(rows, m.rowsView.Prefix, m.scroll, availScroll, m.autoScroll)
	m.scroll = scroll
	m.autoScroll = auto
	return visible
}

func (m *runMode) viewportHeight() int {
	if m.height <= 0 {
		return 0
	}
	var extra int
	if m.extraRowsValid && m.cachedExtraW == m.width && m.cachedExtraH == m.height {
		extra = m.cachedExtraRows
	} else {
		body, _ := m.viewBody(nil)
		extra = tuilib.ViewRows(body)
		if m.helpPanel.Active() {
			extra -= tuilib.ViewRows(m.helpPanel.Rows())
		}
		m.cachedExtraRows = extra
		m.cachedExtraW = m.width
		m.cachedExtraH = m.height
		m.extraRowsValid = true
	}
	budget := m.height
	if m.helpPanel.Active() {
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
	m.ensureVisibleBlocksExpanded(avail)
	if avail <= 0 {
		m.vpRows = 0
		return m.transcriptRows(m.width)
	}
	rows, header, resp := m.syncRowsView()
	if len(rows) <= avail {
		m.scroll = 0
		m.autoScroll = true
		m.pinnedPromptText = ""
		m.pinnedPromptRowIdx = -1
		m.stickyBarRow = ""
		m.vpRows = avail
		return rows
	}
	maxScroll := len(rows) - avail
	if maxScroll < 0 {
		maxScroll = 0
	}
	if m.autoScroll || m.scroll >= maxScroll {
		m.scroll = maxScroll
		m.autoScroll = true
		m.pinnedPromptText = ""
		m.pinnedPromptRowIdx = -1
		m.stickyBarRow = ""
		m.vpRows = avail
		if maxScroll > len(rows) {
			maxScroll = len(rows)
		}
		visible := rows[maxScroll:]
		return tuiviewport.FormatVisibleRows(visible, m.width, m.pinnedPromptText, m.stickyBarRow, m.altScreen)
	}
	availScroll := m.updateStickyPrompt(rows, header, resp, avail)
	if availScroll <= 0 {
		m.vpRows = avail
		return nil
	}
	visible := m.computeVisibleWindow(rows, availScroll)
	out := tuiviewport.FormatVisibleRows(visible, m.width, m.pinnedPromptText, m.stickyBarRow, m.altScreen)
	m.vpRows = avail
	return out
}
