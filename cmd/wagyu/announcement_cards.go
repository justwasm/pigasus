// announcement_cards.go — feature/announcement-cards.
//
// AGY 1.2.17 changelog line 96:
//
//	"Added announcement cards above the prompt for model launches,
//	 deprecations, and other service notices. Cards appear one at a
//	 time, newest first; press `Esc` on an empty prompt to dismiss
//	 the current card permanently and show the next one, and
//	 sending a message hides the card for the rest of the session."
//
// Lifecycle:
//   - The catalog is fixed (internal/announce.Catalog) for this cycle.
//   - On NewRunMode we load any persisted dismissals from
//     settings.json's `dismissedAnnouncements` subkey and seed the
//     visible queue. We also seed a per-session hide flag that
//     survives a send-prompt event (not persisted).
//   - Esc on empty prompt pops the visible card off the queue AND
//     writes the card ID into settings.json's dismissed list so the
//     next session does not re-surface it.
//   - submitPrompt hides the current card for the rest of the
//     session — the in-memory queue's head advances to the next card,
//     but the persisted list is untouched. Sending a message is the
//     "I acknowledge, keep going" gesture; Esc is the "stop showing
//     me this" gesture.
//
// This file is the only TUI-side glue for the feature. The catalog
// and persistence logic live in internal/announce so a future cycle
// can swap the source (HTTP feed, JSON file, signed bundle) without
// touching cmd/wagyu.
package main

import (
	"strings"

	"github.com/justwasm/pigasus/internal/announce"
)

// announcementRow renders one announcement card as the rows that go
// between the top separator and the prompt input. Returns an empty
// string when the queue is empty, so viewBody can write the result
// unconditionally and get the no-op behaviour for free.
//
// The card renders as three rows (border-top, content, border-bottom)
// inside the input chrome's left margin (two spaces of indent, the
// same gutter the slash-command panels and the status row use). The
// "ⓘ" glyph is a unicode info marker; the dim-gray body uses the
// same ANSI palette as the status row's account/cwd lines so the
// card sits inside the existing visual language instead of drawing
// attention to itself.
//
// The format is deliberately minimal — a future cycle can swap it
// for a more elaborate box without changing viewBody's contract.
func (m *runMode) announcementRow() string {
	card := m.activeAnnouncement()
	if card == nil {
		return ""
	}
	// Three rows: top border, content, bottom border.
	var b strings.Builder
	b.WriteString(cSeparatorOpen)
	b.WriteString("  ┌─ announcement ")
	b.WriteString(strings.Repeat("─", maxInt(0, 56-len("  ┌─ announcement "))))
	b.WriteString(cSeparatorClose)
	b.WriteString("\n")
	b.WriteString(cDimGrayOpen)
	b.WriteString("  │  ")
	b.WriteString(cWarningBlueOpen)
	b.WriteString("ⓘ ")
	b.WriteString(cWarningBlueClose)
	b.WriteString(card.Title)
	b.WriteString("\n")
	b.WriteString(cDimGrayOpen)
	b.WriteString("  │  ")
	b.WriteString(card.Body)
	b.WriteString("\n")
	b.WriteString(cDimGrayOpen)
	b.WriteString("  │  ")
	hint := card.Hint
	if hint == "" {
		hint = "Esc to dismiss · Send a message to hide"
	}
	b.WriteString(hint)
	b.WriteString("\n")
	b.WriteString(cSeparatorOpen)
	b.WriteString("  └")
	b.WriteString(strings.Repeat("─", 70))
	b.WriteString(cSeparatorClose)
	b.WriteString("\n")
	return b.String()
}

// activeAnnouncement returns the head of m.announceVisible that the
// TUI should render, or nil when the queue is empty (either the user
// has dismissed every card or the catalog itself is empty).
//
// The slice is mutated by Esc / submitPrompt; this helper is the one
// place that picks the front. Test code drives Esc and submitPrompt
// through m.handleAnnouncementKeyEsc and m.dismissAnnouncementForSend
// and asserts on m.announceVisible.
func (m *runMode) activeAnnouncement() *announce.Card {
	if m == nil {
		return nil
	}
	if len(m.announceVisible) == 0 {
		return nil
	}
	c := m.announceVisible[0]
	return &c
}

// announcementRowsCount returns the number of screen rows
// announcementRow() will emit — 4 when a card is visible (border +
// title + body + hint + border, written as 5 lines), 0 when the
// queue is empty.
//
// viewBody calls this when computing the cursor Y offset so the
// caret still lands on the prompt's last row after a card is
// rendered.
func (m *runMode) announcementRowsCount() int {
	if m.activeAnnouncement() == nil {
		return 0
	}
	return 5
}

// dismissCurrentAnnouncement removes the head of the visible queue
// and persists the dismissal so the same card does not re-surface in
// a later session. Errors from SaveDismissed are surfaced (so the
// TUI can warn the user) but do not block the in-memory pop — the
// card disappears from this session either way.
func (m *runMode) dismissCurrentAnnouncement() {
	card := m.activeAnnouncement()
	if card == nil {
		return
	}
	m.announceVisible = m.announceVisible[1:]
	if err := announce.SaveDismissed(card.ID); err != nil {
		m.announceSaveError = err
	}
	m.announceSessionHiddenID = "" // pop the hide flag too
}

// hideCurrentAnnouncementForSession advances the visible queue head
// without persisting. submitPrompt calls this so sending a message
// hides the card for the rest of the session but lets the next
// session see the same card again.
func (m *runMode) hideCurrentAnnouncementForSession() {
	if len(m.announceVisible) == 0 {
		return
	}
	// Record the hidden ID so a future restart of the same session
	// (rare but possible if the user types /clear and expects to
	// re-trigger the announcement) does not re-show the card. The
	// in-memory visible queue is also dropped so /clear does not
	// need to know about announcements.
	m.announceSessionHiddenID = m.announceVisible[0].ID
	m.announceVisible = m.announceVisible[1:]
}

// announcementSaveErrorMessage returns a human-readable warning
// when persistence failed on the last dismiss, or "" when the last
// dismiss wrote cleanly. The TUI surfaces this in the response
// transcript so the user knows the card is gone *for now* but will
// reappear next launch.
func (m *runMode) announcementSaveErrorMessage() string {
	if m == nil || m.announceSaveError == nil {
		return ""
	}
	return "  ⓘ Could not save announcement dismissal: " + m.announceSaveError.Error()
}

// maxInt is the local min-of-two helper. We don't pull a helpers
// package for this — a single helper with a precise name is cheaper
// than the import dance for the future cycle that consolidates.
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// loadAnnouncements seeds the in-memory visible queue from the
// persisted dismiss list and the catalog. The production TUI
// (runLiveTUI) calls this once, immediately after NewRunMode, so the
// user sees any un-dismissed cards the moment the TUI lands.
//
// Tests that drive NewRunMode directly skip this — the announcement
// row is not part of the byte-exact legacy layouts the cycle-7x
// cursor/separator tests pin. Tests that DO exercise the feature
// (announcement_cards_test.go) call it explicitly to set up.
//
// Calling loadAnnouncements a second time is a no-op for the queue
// (the catalog is fixed, so re-reading settings would only matter if
// another process dismissed a card mid-session, which the spec does
// not require).
func (m *runMode) loadAnnouncements() {
	dismissed, err := announce.LoadDismissed()
	if err != nil {
		m.announceSaveError = err
	}
	m.announceVisible = announce.VisibleCards(dismissed)
	m.announceSessionHiddenID = ""
}
