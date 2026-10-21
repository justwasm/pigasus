package main

// announcement_cards_test.go — feature/announcement-cards.
//
// These tests exercise the TUI half of the feature: the queue, the
// render row, the Esc-dismiss path, and the submitPrompt-hide path.
// The catalog and persistence helpers are covered by
// internal/announce/catalog_test.go (we keep them there so the
// package can be reused by future print-mode work).
//
// Every test calls m.loadAnnouncements() after NewRunMode so the
// queue is seeded the same way runLiveTUI seeds it in production.
// Tests that don't call loadAnnouncements would get a nil queue
// and the announcement row would never render — exactly what the
// cycle-7x cursor / separator tests rely on.

import (
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/announce"
)

// announcementModel returns a fresh runMode with the announcement
// queue seeded. Tests that need an empty catalog pass an
// ANTIGRAVITY_HOME that has dismissed every card; tests that need a
// populated catalog leave ANTIGRAVITY_HOME pointing at a fresh dir.
func announcementModel(t *testing.T) *runMode {
	t.Helper()
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	m := NewRunMode(&fakeClient{}, "u@t", "Gemini 3.8 Flash (High)", "t", "/", true)
	m.loadAnnouncements()
	return m
}

// announcementModelWithDismissals returns a fresh runMode whose
// queue has every catalog card pre-dismissed. Useful for the
// "empty queue shows no card row" test.
func announcementModelWithDismissals(t *testing.T) *runMode {
	t.Helper()
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	t.Setenv("HOME", t.TempDir())
	for _, c := range announce.Catalog() {
		if err := announce.SaveDismissed(c.ID); err != nil {
			t.Fatalf("SaveDismissed(%q): %v", c.ID, err)
		}
	}
	m := NewRunMode(&fakeClient{}, "u@t", "Gemini 3.8 Flash (High)", "t", "/", true)
	m.loadAnnouncements()
	return m
}

// TestAnnouncementQueue_NewestFirst pins the "newest first, head of
// slice = highest priority" semantic. The TUI renders the head as
// the active card; the spec requires the highest-priority card be
// the one shown first.
func TestAnnouncementQueue_NewestFirst(t *testing.T) {
	m := announcementModel(t)
	if len(m.announceVisible) == 0 {
		t.Fatal("queue empty after loadAnnouncements; expected 3 cards")
	}
	card := m.activeAnnouncement()
	if card == nil {
		t.Fatal("activeAnnouncement returned nil for non-empty queue")
	}
	wantHead := "model-claude-opus-5-5"
	if card.ID != wantHead {
		t.Errorf("head card ID = %q, want %q", card.ID, wantHead)
	}
	// Walk the rest of the queue and pin the order.
	want := []string{
		"model-claude-opus-5-5",
		"deprecate-gemini-2-0-flash",
		"service-quota-rollover",
	}
	if len(m.announceVisible) != len(want) {
		t.Fatalf("queue length = %d, want %d", len(m.announceVisible), len(want))
	}
	for i, id := range want {
		if m.announceVisible[i].ID != id {
			t.Errorf("announceVisible[%d].ID = %q, want %q", i, m.announceVisible[i].ID, id)
		}
	}
}

// TestAnnouncementRender_AppearsAbovePrompt is the layout test:
// View() output must contain the active card's title somewhere
// above the prompt ">". We find the row index of each; the card
// title must come strictly before the prompt line.
func TestAnnouncementRender_AppearsAbovePrompt(t *testing.T) {
	m := announcementModel(t)
	view := m.View()
	lines := strings.Split(view.Content, "\n")

	titleIdx := -1
	promptIdx := -1
	for i, line := range lines {
		if strings.Contains(stripSGR(line), "Claude Opus 5.5 available") {
			titleIdx = i
		}
		if strings.HasPrefix(stripSGR(line), ">") && promptIdx == -1 && strings.Contains(stripSGR(line), " ") {
			promptIdx = i
		}
	}
	if titleIdx == -1 {
		t.Fatalf("announcement title not in view:\n%s", view.Content)
	}
	if promptIdx == -1 {
		t.Fatalf("prompt not in view:\n%s", view.Content)
	}
	if titleIdx >= promptIdx {
		t.Errorf("title row %d should be above prompt row %d:\n%s", titleIdx, promptIdx, view.Content)
	}
}

// TestAnnouncementRender_EmptyQueueHidden is the spec's "empty
// queue shows no card row" rule. The view must not contain the
// announcement title when every catalog card is dismissed.
func TestAnnouncementRender_EmptyQueueHidden(t *testing.T) {
	m := announcementModelWithDismissals(t)
	view := m.View()
	for _, c := range announce.Catalog() {
		if strings.Contains(view.Content, c.Title) {
			t.Errorf("dismissed card %q title still in view:\n%s", c.Title, view.Content)
		}
	}
	// Cursor Y should match the legacy idle position (no card row).
	if view.Cursor == nil {
		t.Fatal("view has no cursor")
	}
	if view.Cursor.Y != 8 {
		t.Errorf("idle cursor Y = %d, want 8 (legacy layout)", view.Cursor.Y)
	}
}

// TestAnnouncementEsc_DismissesCard is the spec's "Esc on empty
// prompt dismisses the current card permanently and shows the next
// one". After Esc the head of the queue advances AND the dismissed
// ID is persisted.
func TestAnnouncementEsc_DismissesCard(t *testing.T) {
	m := announcementModel(t)
	if got := m.activeAnnouncement().ID; got != "model-claude-opus-5-5" {
		t.Fatalf("pre-Esc head = %q", got)
	}
	// Esc on empty prompt — fire the keypress through Update so the
	// branch in the real Update path runs.
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	if got := m.activeAnnouncement(); got == nil {
		t.Fatal("post-Esc head is nil; expected next card")
	} else if got.ID != "deprecate-gemini-2-0-flash" {
		t.Errorf("post-Esc head = %q, want deprecate-gemini-2-0-flash", got.ID)
	}
	// And the dismissed ID is persisted to settings.json. We don't
	// need to round-trip through NewRunMode — LoadDismissed is
	// already covered by internal/announce — but a sanity check on
	// the file content pins the integration.
	body, err := os.ReadFile(os.Getenv("ANTIGRAVITY_HOME") + "/settings.json")
	if err != nil {
		t.Fatalf("settings.json missing: %v", err)
	}
	if !strings.Contains(string(body), "model-claude-opus-5-5") {
		t.Errorf("dismissed id not persisted:\n%s", body)
	}
}

// TestAnnouncementEsc_NonEmptyPrompt does NOT dismiss — the spec
// requires "on an empty prompt". Pressing Esc with text in the
// buffer should keep the current card visible.
func TestAnnouncementEsc_NonEmptyPrompt(t *testing.T) {
	m := announcementModel(t)
	for _, r := range "hello" {
		m.Update(runeKey(r))
	}
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	if got := m.activeAnnouncement(); got == nil {
		t.Fatal("Esc on non-empty prompt should NOT dismiss")
	} else if got.ID != "model-claude-opus-5-5" {
		t.Errorf("head after Esc-on-text = %q, want unchanged model-claude-opus-5-5", got.ID)
	}
}

// TestAnnouncementEsc_DismissesAllThenEmpty confirms the queue
// empties after enough Esc presses, and that the third Esc on an
// empty queue is the silent-absorb branch (no panic, no error).
func TestAnnouncementEsc_DismissesAllThenEmpty(t *testing.T) {
	m := announcementModel(t)
	for i := 0; i < 3; i++ {
		m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	}
	if m.activeAnnouncement() != nil {
		t.Errorf("queue not empty after 3 Escs; head = %q", m.activeAnnouncement().ID)
	}
	// A fourth Esc on an empty queue must not panic and must not
	// change state.
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	if m.activeAnnouncement() != nil {
		t.Errorf("extra Esc surfaced a card")
	}
}

// TestAnnouncementSubmit_HidesForSession is the second half of the
// spec: "sending a message hides the card for the rest of the
// session". Submitting a prompt hides the card in-memory but does
// NOT persist.
func TestAnnouncementSubmit_HidesForSession(t *testing.T) {
	m := announcementModel(t)
	if got := m.activeAnnouncement(); got == nil || got.ID != "model-claude-opus-5-5" {
		t.Fatalf("pre-submit head = %v, want model-claude-opus-5-5", got)
	}
	for _, r := range "hi" {
		m.Update(runeKey(r))
	}
	m.streaming = false
	m.Update(enterKey())
	if got := m.activeAnnouncement(); got == nil {
		t.Fatal("submit should hide for session; got nil card with more in queue")
	}
	if got := m.activeAnnouncement().ID; got != "deprecate-gemini-2-0-flash" {
		t.Errorf("post-submit head = %q, want deprecate-gemini-2-0-flash", got)
	}
	if !m.announceSessionHiddenAfterSubmit("model-claude-opus-5-5") {
		t.Error("session-hidden ID was not recorded")
	}
	// The ID must NOT be persisted.
	if _, err := os.Stat(os.Getenv("ANTIGRAVITY_HOME") + "/settings.json"); err == nil {
		body, _ := os.ReadFile(os.Getenv("ANTIGRAVITY_HOME") + "/settings.json")
		if strings.Contains(string(body), "model-claude-opus-5-5") {
			t.Errorf("submit should not persist; settings.json contains the id:\n%s", body)
		}
	}
}

// TestAnnouncementSubmit_NoCardNoop confirms submitPrompt is a
// no-op when there's nothing to hide.
func TestAnnouncementSubmit_NoCardNoop(t *testing.T) {
	m := announcementModelWithDismissals(t)
	for _, r := range "hi" {
		m.Update(runeKey(r))
	}
	m.streaming = false
	m.Update(enterKey())
	if m.activeAnnouncement() != nil {
		t.Errorf("submit on empty queue surfaced a card")
	}
	if m.announceSessionHiddenID != "" {
		t.Errorf("submit on empty queue recorded a hidden ID: %q", m.announceSessionHiddenID)
	}
}

// TestAnnouncementRestart_RespectsPersistedDismiss is the
// integration test: a card dismissed in one session is NOT shown in
// the next. We persist via dismissCurrentAnnouncement (which calls
// announce.SaveDismissed under the hood), then construct a fresh
// runMode against the same ANTIGRAVITY_HOME so it picks up the
// persisted list.
func TestAnnouncementRestart_RespectsPersistedDismiss(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	t.Setenv("HOME", t.TempDir())

	m := NewRunMode(&fakeClient{}, "u@t", "Gemini 3.8 Flash (High)", "t", "/", true)
	m.loadAnnouncements()
	m.dismissCurrentAnnouncement()
	if got := m.activeAnnouncement(); got == nil || got.ID != "deprecate-gemini-2-0-flash" {
		t.Fatalf("first dismiss did not advance; head = %v", got)
	}
	// New session, same ANTIGRAVITY_HOME — the persisted dismissal
	// must be honoured.
	m2 := NewRunMode(&fakeClient{}, "u@t", "Gemini 3.8 Flash (High)", "t", "/", true)
	m2.loadAnnouncements()
	if m2.activeAnnouncement() == nil {
		t.Fatal("new session has no active announcement after one dismiss; expected two left")
	}
	if m2.activeAnnouncement().ID != "deprecate-gemini-2-0-flash" {
		t.Errorf("new session head = %q, want deprecate-gemini-2-0-flash (model-claude-opus-5-5 was dismissed)",
			m2.activeAnnouncement().ID)
	}
	for _, c := range m2.announceVisible {
		if c.ID == "model-claude-opus-5-5" {
			t.Errorf("persisted dismiss not honoured; model-claude-opus-5-5 still in queue")
		}
	}
}

// announceSessionHiddenAfterSubmit returns true when the user's
// last submitPrompt hid the given ID for the rest of the session.
// Test helper to keep the assertion in TestAnnouncementSubmit_*.
func (m *runMode) announceSessionHiddenAfterSubmit(id string) bool {
	if m == nil {
		return false
	}
	return m.announceSessionHiddenID == id
}

// TestAnnouncementRender_HintRow covers the card's footer hint row.
// The spec's example shows "Esc to dismiss · Send a message to hide"
// — the renderer must emit this string verbatim (or fall back to it
// when the catalog entry omits Hint).
func TestAnnouncementRender_HintRow(t *testing.T) {
	m := announcementModel(t)
	view := m.View()
	if !strings.Contains(stripSGR(view.Content), "Esc to dismiss") {
		t.Errorf("hint row missing from view:\n%s", view.Content)
	}
	if !strings.Contains(stripSGR(view.Content), "Send a message to hide") {
		t.Errorf("hint row missing from view:\n%s", view.Content)
	}
}

// TestAnnouncementRender_CardRowNotInTranscript checks the layout
// invariant: the card row sits between the top separator and the
// prompt, not inside the scrollable transcript. The transcript
// rows must precede the card's title row in the rendered output.
func TestAnnouncementRender_CardRowNotInTranscript(t *testing.T) {
	m := announcementModel(t)
	m.response = []string{"transcript line 1", "transcript line 2"}
	view := m.View()
	transcriptRow := "transcript line 2"
	transcriptIdx := -1
	titleIdx := -1
	for i, line := range strings.Split(view.Content, "\n") {
		if strings.Contains(stripSGR(line), transcriptRow) && transcriptIdx == -1 {
			transcriptIdx = i
		}
		if strings.Contains(stripSGR(line), "Claude Opus 5.5 available") && titleIdx == -1 {
			titleIdx = i
		}
	}
	if transcriptIdx == -1 || titleIdx == -1 {
		t.Fatalf("missing rows; transcriptIdx=%d titleIdx=%d", transcriptIdx, titleIdx)
	}
	if titleIdx < transcriptIdx {
		t.Errorf("card title at row %d appears before transcript row %d; layout broken:\n%s",
			titleIdx, transcriptIdx, view.Content)
	}
}
