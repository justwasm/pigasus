// history.go — up/down arrow prompt history navigation for the run mode.
//
// Cycle 71: historyUp/historyDown operate on m.input (charm.land/bubbles/v2
// textarea) instead of the hand-rolled []rune buffer. m.input.SetValue()
// rewrites the textarea's content, which automatically resets the cursor
// to the end of the inserted text.
//
// Cycle trim-empty-history-messages: pushHistory and submitPrompt treat
// whitespace-only prompts as empty. The pre-existing prompt == "" guard
// was bypassed by a single space, which then added an empty "  >   "
// row to the transcript, an empty user message to the session, and an
// entry to m.history — confusing Up/Down navigation (the user landed on
// a row with no text) and shifting scroll offsets by exactly one row
// per accidental submit.
package main

import (
	"strings"

	tuilib "github.com/justwasm/pigasus/internal/tuilib"

	"github.com/justwasm/pigasus/internal/convstore"
)

// HistoryCap is the maximum number of prompts remembered by the run mode.
const HistoryCap = 100

// isBlank reports whether s is empty or contains only whitespace runes.
// Used by pushHistory and submitPrompt to drop whitespace-only prompts
// before they reach m.history / m.session / m.response / history.jsonl.
//
// Cycle trim-empty-history-messages: a single space (or any run of
// Unicode whitespace) used to slip past the prompt == "" guard and
// produced empty entries in three places at once. AGY 1.2.7's fix is
// to reject blank inputs the same way it rejects "".
func isBlank(s string) bool {
	return strings.TrimSpace(s) == ""
}

// pushHistory adds a non-empty, non-whitespace prompt to m.history,
// deduping against the previous entry and trimming the slice to
// HistoryCap when oversized.
//
// Cycle trim-empty-history-messages: a prompt of one or more spaces
// is treated as empty and skipped. The history list, transcript, and
// history.jsonl would otherwise all receive empty entries — wasted
// screen rows and confusing Up/Down landings.
//
// Surrounding whitespace is trimmed before the dedupe check and the
// append so a follow-up "  hello  " after "hello" collapses to the
// same entry instead of producing a near-duplicate. The trimmed form
// is what reaches history.jsonl and the Up/Down buffer.
func (m *runMode) pushHistory(prompt string) {
	trimmed := strings.TrimSpace(prompt)
	if trimmed == "" {
		return
	}
	if len(m.history) > 0 && m.history[len(m.history)-1] == trimmed {
		return
	}
	m.history = append(m.history, trimmed)
	if len(m.history) > HistoryCap {
		m.history = m.history[len(m.history)-HistoryCap:]
	}
	// Persist to history.jsonl in agy-compatible format so the user
	// can navigate history from either binary.
	_ = convstore.AppendHistoryEntry(convstore.HistoryEntry{
		Display:        trimmed,
		Workspace:      m.cwd,
		ConversationID: m.conversationID,
	})
}

// loadHistory seeds m.history from the persistent history.jsonl store.
// Cycle 286: agy shares prompt history across all sessions globally.
// When a fresh CLI session starts, pressing Up immediately recalls
// the most recent prompt from earlier sessions. If m.history is already
// populated (e.g. by a test), loadHistory preserves the existing entries.
func (m *runMode) loadHistory() {
	if len(m.history) > 0 {
		return
	}
	m.history = convstore.LoadHistory("", HistoryCap)
}

// historyUp navigates one slot back.
func (m *runMode) historyUp() {
	if len(m.history) == 0 {
		return
	}
	if m.historyIdx == 0 {
		m.historyEdit = m.input.Value()
	}
	if m.historyIdx >= len(m.history) {
		return
	}
	m.historyIdx++
	m.applyHistoryEntry(m.history[len(m.history)-m.historyIdx])
	m.picker.Update([]rune(firstLine(m.input.Value())))
	// feature/at-file-dropdown-newline: derive the @ file-mention
	// dropdown from the post-SetValue buffer the same way the
	// slash picker does. The parser handles newlines internally so
	// a recalled history entry containing "@src" mid-line still
	// triggers the dropdown.
	m.atFileDropdown.Update(m.input.Value(), m.cwd)
}

// historyDown navigates one slot forward.
func (m *runMode) historyDown() {
	if m.historyIdx == 0 {
		return
	}
	m.historyIdx--
	if m.historyIdx == 0 {
		m.applyHistoryEntry(m.historyEdit)
		m.picker.Update([]rune(firstLine(m.input.Value())))
		m.atFileDropdown.Update(m.input.Value(), m.cwd)
		return
	}
	m.applyHistoryEntry(m.history[len(m.history)-m.historyIdx])
	m.picker.Update([]rune(firstLine(m.input.Value())))
	m.atFileDropdown.Update(m.input.Value(), m.cwd)
}

// historyReset exits history navigation.
func (m *runMode) historyReset() {
	if m.historyIdx != 0 {
		m.historyIdx = 0
		m.historyEdit = ""
	}
}

// Cycle 182: /help list navigation.
//
// The v1.4.0 reference does NOT paginate the COMMANDS / SHORTCUTS
// tabs. It renders an 18-row *window* into the full catalog and keeps
// a cursor inside that window; Up/Down move the cursor and scroll the
// window one row at a time once the cursor reaches an edge. Verified
// against REF at 220×30:
//
//	entry          [1-18 of 99 items]   cursor at row 0
//	Down ×17       [1-18 of 99 items]   cursor at row 17 (window edge)
//	Down ×18       [2-19 of 99 items]   window scrolled one row
//	Down ×19       [3-20 of 99 items]
//	Ctrl+End       [82-99 of 99 items]  cursor at row 98
//	Down at 98     [1-18 of 99 items]   wraps to row 0
//	Ctrl+Home      [1-18 of 99 items]   cursor at row 0
//	Up at 0        [82-99 of 99 items]  wraps to row 98
//
// Right/Left/Tab cycle tabs and reset the window + cursor to 0 (the
// reference does not preserve per-tab scroll). Cycle 137 had treated
// Right/Left as page-forward/page-back on the paginated tabs, which
// diverges from REF; cycle 182 replaces that model.
//
// The helpers below take the catalog length explicitly so both the
// COMMANDS (99 entries) and SHORTCUTS (34 entries) tabs share them.

// helpListLen returns the row count of the /help list on the given
// tab. GENERAL (tab 0) has no list.
func helpListLen(tab int) int {
	switch tab {
	case 1:
		return len(tuilib.DefaultSlashCommands)
	case 2:
		return len(helpShortcuts)
	case 3:
		// Cycle 216: the VIM tab, present only when vim mode is enabled.
		return len(helpVim)
	default:
		return 0
	}
}

// helpListWindowFor returns the number of list rows the /help overlay
// renders at once for a terminal of the given height. Cycle 132 hard-
// coded 18 (correct only at 30 rows); cycle 182 recovered the real
// formula by scanning REF v1.4.0 at heights 13..60:
//
//	h :  13 14 15 16 17 18 20 22 24 26 28 30 34 40 50 60
//	n :   4  5  6  7  8  9 11 12 14 15 17 18 21 26 33 41
//
// Every sample satisfies n = min(3h/4 - 4, h - 9) with integer
// division (the `h-9` term dominates below h≈17). A height of 0 means
// the terminal size has not been reported yet — fall back to 18, the
// pre-182 behaviour the byte-exact snapshots assert.
func helpListWindowFor(height int) int {
	if height <= 0 {
		return 18
	}
	n := height*3/4 - 4
	if alt := height - 9; alt < n {
		n = alt
	}
	if n < 1 {
		n = 1
	}
	return n
}

// helpListWindowSize is the window size for this runMode's current
// terminal height.
func (m *runMode) helpListWindowSize() int {
	return helpListWindowFor(m.height)
}

// helpListVisible returns the number of rows actually drawn given the
// window start, catalog length and window size (the last window is
// short when the catalog doesn't fill it).
func helpListVisible(total, window, size int) int {
	if total == 0 || window >= total {
		return 0
	}
	if size < 1 {
		size = 1
	}
	remain := total - window
	if remain > size {
		return size
	}
	return remain
}

// helpListClampWindow keeps m.helpWindow inside the catalog after a
// tab switch or a terminal resize. The reference always resets to
// window 0 on a tab switch, so this is a safety net rather than the
// primary path.
func (m *runMode) helpListClampWindow(total int) {
	if total <= 0 {
		m.helpWindow = 0
		m.helpCursor = 0
		return
	}
	size := m.helpListWindowSize()
	maxWindow := total - size
	if maxWindow < 0 {
		maxWindow = 0
	}
	if m.helpWindow > maxWindow {
		m.helpWindow = maxWindow
	}
	if m.helpWindow < 0 {
		m.helpWindow = 0
	}
	if m.helpCursor >= total {
		m.helpCursor = total - 1
	}
	if m.helpCursor < 0 {
		m.helpCursor = 0
	}
}

// helpListDown moves the cursor down one row. When the cursor would
// leave the bottom of the window, the window scrolls one row; when the
// cursor would leave the catalog, it wraps to the first row and the
// window resets to the top.
func (m *runMode) helpListDown(total int) {
	if total <= 0 {
		return
	}
	m.helpListClampWindow(total)
	size := m.helpListWindowSize()
	m.helpCursor++
	if m.helpCursor >= total {
		m.helpCursor = 0
		m.helpWindow = 0
		return
	}
	if m.helpCursor >= m.helpWindow+size {
		m.helpWindow = m.helpCursor - size + 1
	}
}

// helpListUp moves the cursor up one row, scrolling the window up when
// the cursor leaves the top edge and wrapping to the last row when it
// would leave the catalog.
func (m *runMode) helpListUp(total int) {
	if total <= 0 {
		return
	}
	m.helpListClampWindow(total)
	size := m.helpListWindowSize()
	m.helpCursor--
	if m.helpCursor < 0 {
		m.helpCursor = total - 1
		m.helpWindow = total - size
		if m.helpWindow < 0 {
			m.helpWindow = 0
		}
		return
	}
	if m.helpCursor < m.helpWindow {
		m.helpWindow = m.helpCursor
	}
}

// helpListHome jumps to the first row and resets the window to the top
// (Ctrl+Home in REF).
func (m *runMode) helpListHome() {
	m.helpCursor = 0
	m.helpWindow = 0
}

// helpListEnd jumps to the last row and scrolls the window to the
// bottom (Ctrl+End in REF).
func (m *runMode) helpListEnd(total int) {
	if total <= 0 {
		return
	}
	m.helpCursor = total - 1
	m.helpWindow = total - m.helpListWindowSize()
	if m.helpWindow < 0 {
		m.helpWindow = 0
	}
}
