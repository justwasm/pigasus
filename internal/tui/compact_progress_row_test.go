package tui

// compact_progress_row_test.go — the running compaction's live row.
//
// # What these pin
//
// The reference draws a running compaction as one live line inside the
// conversation, in the place "Generating…" and "Working…" occupy:
//
//	⣾  Compacting conversation… (10s · esc to cancel)
//
// with a Braille spinner that cycles, a clock that advances and a cancel
// hint that means something. The clone used to draw a static notice row
// with no spinner, no clock and no hint, and Esc did nothing — and the
// status row could draw a second copy of the same compaction.
//
// Three facts are pinned here: the row animates in place (never appending
// a row per frame), Esc reaches the summarizer's cancel, and a compaction
// is drawn in exactly one place.

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/internal/app/session/convstore"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/core/compaction"
)

// stripSGR strips SGR spans so the assertions can read the row.
func stripSGR(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		if r == 0x1b {
			inEsc = true
			continue
		}
		if inEsc {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// hangingCompactModel arms a compaction that blocks until cancelled.
func hangingCompactModel(t *testing.T) *RootModel {
	t.Helper()
	// The cancel slot compaction.InstallCancel fills is package state, and the
	// worker that owns it clears it from a deferred call — which has not run
	// yet when a test fires Esc and reads the result channel. Without this
	// cleanup the slot survives the test, and the *next* compaction test's Esc
	// fires a cancel belonging to a compaction that is already over. That is
	// why TestCompactionEscCancels passed alone and failed in a shuffled
	// suite.
	t.Cleanup(compaction.ResetForTest)
	m := mkCompactNoProgModel(t, &fakeStreamClient{summary: "s", hang: true})
	m.session.model = "Gemini 3.8 Flash (High)"
	m.viewport.width = 120
	m.setSessionMessages([]backend.Message{{Role: "user", Content: "hello"}})
	if handled, _ := m.handleSlashCommand("/compact"); !handled {
		t.Fatal("/compact should be handled")
	}
	return m
}

// TestCompactionLiveRowShape pins the row's text: spinner, phrase, clock and
// the cancel hint the reference shows.
func TestCompactionLiveRowShape(t *testing.T) {
	m := hangingCompactModel(t)

	i := m.conversation.compactRowIdx
	if i <= 0 || i >= len(m.conversation.response) {
		t.Fatalf("compactRowIdx = %d, want a valid row inside the transcript (%d rows)",
			i, len(m.conversation.response))
	}
	row := stripSGR(m.conversation.response[i])
	for _, want := range []string{"Compacting conversation…", "(0s", "esc to cancel"} {
		if !strings.Contains(row, want) {
			t.Errorf("live row %q missing %q", row, want)
		}
	}
	// The spinner is a Braille frame, not the notice bullet.
	if !strings.ContainsAny(row, "⣾⣽⣻⢿⡿⣟⣯⣷") {
		t.Errorf("live row %q carries no spinner frame", row)
	}
	if strings.Contains(row, "⎿") {
		t.Errorf("live row %q is the static notice shape, not the live shape", row)
	}
}

// TestCompactionLiveRowAnimatesInPlace pins that a tick repaints the row
// rather than appending one: the reference shows one live line, so a frame
// per tick would scroll the conversation.
func TestCompactionLiveRowAnimatesInPlace(t *testing.T) {
	m := hangingCompactModel(t)

	before := len(m.conversation.response)
	beforeRow := m.conversation.response[m.conversation.compactRowIdx]

	// Age the clock so the repaint has something to say.
	m.session.store.CompactStart = time.Now().Add(-3 * time.Second)
	m.session.store.CompactSpinnerFrame = 0
	m.updateCompactSpinnerTickMsg()

	if got := len(m.conversation.response); got != before {
		t.Fatalf("a tick changed the transcript length %d -> %d; the live row must be repainted, not appended",
			before, got)
	}
	afterRow := m.conversation.response[m.conversation.compactRowIdx]
	if afterRow == beforeRow {
		t.Errorf("the live row did not repaint: %q", stripSGR(afterRow))
	}
	if !strings.Contains(stripSGR(afterRow), "3s") {
		t.Errorf("the live row's clock did not advance: %q", stripSGR(afterRow))
	}
}

// TestCompactionEscCancels pins the hint's promise: Esc reaches the
// summarizer's cancel channel.
func TestCompactionEscCancels(t *testing.T) {
	m := hangingCompactModel(t)

	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})

	select {
	case msg := <-m.compaction.Done():
		if !msg.Cancelled {
			t.Errorf("Esc produced a non-cancelled result: %+v", msg)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Esc did not cancel the compaction within 5s")
	}
}

// TestCompactionCancelIsArmedBeforeTheWorkerRuns pins the ordering, not just
// the outcome.
//
// TestCompactionEscCancels above is not a reliable guard on its own: it fires
// Esc right after /compact, but it reads whether the *result* arrives, and a
// cancel installed by the worker a few microseconds later still produces one.
// The test therefore passed on an idle machine and failed under a shuffled
// suite, which is how the ordering bug hid. This one reads the slot itself
// instead of waiting on the worker: if /compact returns with the slot empty,
// the "esc to cancel" hint the row draws is a lie, no matter what the
// scheduler does next.
func TestCompactionCancelIsArmedBeforeTheWorkerRuns(t *testing.T) {
	// The model itself is not read: the assertion is about the package slot
	// /compact filled on this goroutine, before any worker could run.
	hangingCompactModel(t)

	if compaction.CancelSlotIsEmpty() {
		t.Fatal("/compact drew an 'esc to cancel' row while no cancel was installed; " +
			"an Esc in that window is silently dropped")
	}
}

// TestCompactionCancelSlotIsReleasedWhenTheWorkerEnds pins the other half:
// the slot must not outlive the compaction. A slot left behind makes the next
// compaction's Esc fire a cancel belonging to a compaction that is already
// over — the leak ResetForTest exists to contain, and this asserts the
// production path does not need it.
func TestCompactionCancelSlotIsReleasedWhenTheWorkerEnds(t *testing.T) {
	m := hangingCompactModel(t)

	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})

	select {
	case <-m.compaction.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("Esc did not cancel the compaction within 5s")
	}

	// The worker clears the slot from a deferred call, so it runs just after
	// it publishes the result. Poll briefly rather than sleeping a fixed
	// amount: the assertion is "it is released", not "it is released within
	// an arbitrary constant".
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if compaction.CancelSlotIsEmpty() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("the worker finished but left its cancel installed; " +
		"the next compaction's Esc would fire a cancel that is already spent")
}

// TestCompactionDrawsInOnePlace pins the single-location rule: the live row
// replaces the old notice, and the status row stays empty while a manual
// compaction runs.
func TestCompactionDrawsInOnePlace(t *testing.T) {
	m := hangingCompactModel(t)

	if st := m.statusRowState(); st.Compact != nil {
		t.Errorf("the status row also draws the manual compaction; one compaction must be one place")
	}
	joined := strings.Join(m.conversation.response, "\n")
	if n := strings.Count(joined, "Compacting conversation"); n != 1 {
		t.Errorf("the transcript carries %d 'Compacting conversation' rows, want exactly 1", n)
	}
}

// TestCompactionLiveRowIsReplacedByItsOutcome pins that the live row does
// not survive the result: the conversation must not show a still-compacting
// line next to the row that says it finished.
func TestCompactionLiveRowIsReplacedByItsOutcome(t *testing.T) {
	m := hangingCompactModel(t)
	rowIdx := m.conversation.compactRowIdx
	if rowIdx <= 0 {
		t.Fatalf("no live row armed")
	}

	// Something can land in the transcript while the compaction runs (a
	// notice, a queued prompt echo). The live row is then no longer the last
	// row, so disposing it must remove that one row and leave the rest
	// exactly where they are.
	const marker = "row that arrived while compacting"
	m.conversation.response = append(m.conversation.response, marker)

	// Apply a cancellation outcome directly: this test owns the row's
	// disposal, not the summarizer's cancellation (TestCompactionEscCancels
	// owns that).
	m.applyGenericCompactResult(genericCompactDoneMsg{Cancelled: true}, "")

	joined := strings.Join(m.conversation.response, "\n")
	if strings.Contains(joined, "Compacting conversation") {
		t.Errorf("the live row survived the outcome:\n%s", joined)
	}
	if !strings.Contains(joined, "Compaction cancelled") {
		t.Errorf("the outcome row is missing:\n%s", joined)
	}
	if !strings.Contains(joined, marker) {
		t.Errorf("removing the live row also dropped a later row:\n%s", joined)
	}
}

// TestCompactingStateDoesNotLeakAcrossConversations pins the live row's
// lifetime: it is an index into *this* conversation's transcript.
//
// A compaction can still be running when the user switches conversations. A
// carried-over index then points at an unrelated row of the new transcript, so
// the next tick repaints somebody else's line — and the stale
// "live row is in the transcript" flag keeps the new conversation's status row
// suppressed for good.
func TestCompactingStateDoesNotLeakAcrossConversations(t *testing.T) {
	m := hangingCompactModel(t)
	if m.conversation.compactRowIdx == 0 {
		t.Fatalf("no live row armed; the test cannot proceed")
	}

	// Switch conversations while the compaction is still running.
	m.seedConversation(&convstore.Conversation{ID: "another"})

	if m.conversation.compactRowIdx != 0 {
		t.Errorf("compactRowIdx = %d after a conversation switch, want 0", m.conversation.compactRowIdx)
	}
	if m.session.store.CompactLiveInTranscript {
		t.Errorf("the new conversation's status row is still suppressed by the old compaction")
	}

	// A tick from the still-running summarizer must not touch the new
	// transcript.
	before := append([]string(nil), m.conversation.response...)
	m.updateCompactSpinnerTickMsg()
	if len(before) != len(m.conversation.response) {
		t.Fatalf("a tick changed the new transcript's length %d -> %d", len(before), len(m.conversation.response))
	}
	for i := range before {
		if before[i] != m.conversation.response[i] {
			t.Errorf("a tick rewrote row %d of the new conversation: %q -> %q",
				i, before[i], m.conversation.response[i])
		}
	}
}
