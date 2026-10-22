package main

import (
	"github.com/justwasm/pigasus/internal/tuilib"
)

// syncRowsView ensures m.rowsView is up to date and returns cached rows, header, and resp.
func (m *runMode) syncRowsView() ([]string, []string, []string) {
	header := m.scrollHeaderRows(m.width)
	resp := m.wrappedResponse(m.width)
	live := m.liveRows(m.width)
	respCounts := m.wrappedResponseCounts(m.width)
	if !m.rowsView.fresh(m.rowsGen, m.width, header, live) {
		m.rowsView.rebuild(m.rowsGen, m.width, header, resp, live, respCounts)
	}
	m.userPromptRows = m.rowsView.prompt
	return m.rowsView.rows, header, resp
}

// updateStickyPrompt calculates and caches the sticky prompt / pinned prompt row.
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

// computeVisibleWindow extracts and clips the visible slice of rows.
func (m *runMode) computeVisibleWindow(rows []string, availScroll int) []string {
	maxScroll := len(rows) - availScroll
	if m.autoScroll {
		m.scroll = maxScroll
	}
	m.scroll = clampInt(m.scroll, 0, maxScroll)
	if m.scroll >= maxScroll {
		m.autoScroll = true
	}
	visible := append([]string(nil), rows[m.scroll:m.scroll+availScroll]...)
	visible = clipToScreenRowsPrefix(visible, m.rowsView.prefix, m.scroll, availScroll)
	for len(visible) < availScroll {
		visible = append(visible, "")
	}
	return visible
}

// formatVisibleRows attaches sticky headers and pads rows to effective width.
func (m *runMode) formatVisibleRows(visible []string, effective int) []string {
	if m.altScreen && m.pinnedPromptText != "" {
		visible = append([]string{padTo(m.pinnedPromptText, effective)}, visible...)
	} else if m.stickyBarRow != "" {
		sticky := []string{padTo(m.stickyBarRow, effective), cRule(effective)}
		visible = append(sticky, visible...)
	}
	out := make([]string, len(visible))
	for i, row := range visible {
		out[i] = padTo(row, effective)
	}
	return out
}

// viewportHeight is the number of rows the transcript viewport may occupy:
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

func padTo(s string, w int) string { return tuilib.PadTo(s, w) }
func clampInt(v, lo, hi int) int { return tuilib.ClampInt(v, lo, hi) }
