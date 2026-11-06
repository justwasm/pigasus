// history.go — the prompt history's wiring in the run mode.
//
// The list itself — the remembered prompts, the navigation over them and
// the blank-prompt rule — lives in internal/ui/foundations/history. The
// host owns the composer, the persistence to history.jsonl and the
// composer's own overlays.
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
// entry to the history list — confusing Up/Down navigation (the user
// landed on a row with no text) and shifting scroll offsets by exactly
// one row per accidental submit.
package tui

import (
	"github.com/justwasm/pigasus/internal/app/session/convstore"
)

// pushHistory adds a non-empty, non-whitespace prompt to the prompt
// history and persists it to history.jsonl.
//
// The list owns the trimming, the dedupe and the cap; the host owns the
// persistence, because the persisted entry carries the workspace and the
// conversation the prompt belongs to.
//
// The entry is persisted so the user can navigate history from either
// binary.
func (m *runMode) pushHistory(prompt string) {
	trimmed, ok := m.promptHistory.Push(prompt)
	if !ok {
		return
	}
	// Persist to history.jsonl in agy-compatible format so the user
	// can navigate history from either binary.
	_ = convstore.AppendHistoryEntry(convstore.HistoryEntry{
		Display:        trimmed,
		Workspace:      m.cwd,
		ConversationID: m.conversationID,
	})
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
	m.atFilePicker.Reset()
}

// historyUp navigates one slot back.
//
// The list owns the navigation; the host owns the composer's write and
// the overlays the recall closes.
func (m *runMode) historyUp() {
	entry, ok := m.promptHistory.Up(m.input.Value())
	if !ok {
		return
	}
	m.applyHistoryEntry(entry)
	// Cycle 295: the overlays are closed, not re-derived — the
	// reference re-derives them from the next typed keystroke.
	m.closeComposerOverlays()
}

// historyDown navigates one slot forward.
func (m *runMode) historyDown() {
	entry, ok := m.promptHistory.Down()
	if !ok {
		return
	}
	m.applyHistoryEntry(entry)
	m.closeComposerOverlays()
}

// Cycle 381: the /help overlay's list navigation moved to
// internal/ui/views/helppanel, which owns the cursor and the window.
//
// Cycle 381: the history's own reset moved with the list. The host
// calls m.promptHistory.Reset() directly — a one-line forward into
// another package is a shell, and the host has no navigation state of
// its own to leave.
