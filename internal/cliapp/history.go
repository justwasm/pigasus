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
package cliapp

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

// closeComposerOverlays hides the composer overlays after a history
// recall.
//
// Cycle 295: a recall is NOT a keystroke. The reference arms its
// composer overlays from the *typed* text, not from the buffer
// content, so a recalled entry never opens the popup.
//
// 【一手】 agy 1.4.7, live (tmux 220×40, 2026-10-25):
//
//	Up            → composer holds "/agents"   (a recalled
//	               slash_command entry), popup CLOSED
//	Up            → composer holds the next-older entry — the
//	               history keeps paging across slash commands
//	Up, then "x"  → composer holds "/agentsx", popup OPEN with the
//	               "No matches" row — the *keystroke* re-derives it
//
// The same holds for the @ file-mention dropdown: recalling an entry
// that contains "@zzzprobe" leaves the dropdown closed; typing one
// character opens it.
//
// Before this the recall re-derived both overlays, so the popup
// swallowed the next Up/Down and the history stopped paging at the
// first slash command in the list.
func (m *runMode) closeComposerOverlays() {
	m.picker.Reset()
	m.atFileDropdown.Reset()
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
	// Cycle 295: the overlays are closed, not re-derived — the
	// reference re-derives them from the next typed keystroke.
	m.closeComposerOverlays()
}

// historyDown navigates one slot forward.
func (m *runMode) historyDown() {
	if m.historyIdx == 0 {
		return
	}
	m.historyIdx--
	if m.historyIdx == 0 {
		m.applyHistoryEntry(m.historyEdit)
		m.closeComposerOverlays()
		return
	}
	m.applyHistoryEntry(m.history[len(m.history)-m.historyIdx])
	m.closeComposerOverlays()
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
// renders at once for a terminal of the given height. Delegated to tuilib.
func helpListWindowFor(height int) int {
	return tuilib.HelpListWindowFor(height)
}

// helpListWindowSize is the window size for this runMode's current
// terminal height.
func (m *runMode) helpListWindowSize() int {
	return helpListWindowFor(m.height)
}

// helpListVisible returns the number of rows actually drawn given the
// window start, catalog length and window size. Delegated to tuilib.
func helpListVisible(total, window, size int) int {
	return tuilib.HelpListVisible(total, window, size)
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
