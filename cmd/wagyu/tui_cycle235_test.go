// tui_cycle235_test.go — cycle 235 / AGY 1.2.10:
//
//	"Fixed pressing Esc while the suggestions dropdown is open
//	 interrupting the agent's turn; it now closes the dropdown,
//	 and a second Esc interrupts."
//
// The picker (slash-command dropdown) must close on the first Esc
// press without disturbing an in-flight turn, and the second Esc
// press — with the dropdown gone but the turn still streaming —
// must cancel the model's stream. The tests below drive the Esc
// handler through m.Update(tea.KeyPressMsg{Esc}) on a fakeClient
// runMode, so no real network is involved.
package main

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// escKey returns a tea.KeyPressMsg for Esc.
func escKey() tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc})
}

// TestEscPickerOpenClosesDropdownOnly pins the first half of the
// spec: pressing Esc while the suggestions dropdown is open must
// close the dropdown and MUST NOT cancel the streaming turn. The
// test arms both states (picker visible + turnCancel installed),
// presses Esc, and verifies that:
//   - the picker is closed (Visible() == false)
//   - the cancel function was NOT invoked
//   - m.streaming stays true (turn is still in flight)
func TestEscPickerOpenClosesDropdownOnly(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.Update(runeKey('/'))
	if !m.picker.Visible() {
		t.Fatalf("precondition: picker should be visible after '/'")
	}
	m.streaming = true
	cancelCount := 0
	m.turnCancel = func() { cancelCount++ }

	m.Update(escKey())

	if m.picker.Visible() {
		t.Errorf("picker should be closed after Esc, still visible")
	}
	if cancelCount != 0 {
		t.Errorf("first Esc (picker open) must NOT cancel the turn, got cancelCount=%d", cancelCount)
	}
	if !m.streaming {
		t.Errorf("first Esc must leave streaming=true (turn untouched), got false")
	}
}

// TestEscNoPickerInterruptsStreamingTurn pins the second half of
// the spec: with the picker already closed and a turn in flight,
// pressing Esc must cancel the in-flight stream via the stored
// turnCancel. After the cancel fires the handler clears turnCancel
// so a third Esc press (no-op in normal use) cannot re-cancel.
func TestEscNoPickerInterruptsStreamingTurn(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.streaming = true
	cancelCount := 0
	m.turnCancel = func() { cancelCount++ }

	m.Update(escKey())

	if cancelCount != 1 {
		t.Errorf("Esc with streaming + no picker must call turnCancel once, got %d", cancelCount)
	}
	if m.turnCancel != nil {
		t.Errorf("turnCancel must be cleared after cancel, got non-nil")
	}
	if !m.streaming {
		t.Errorf("streaming stays true until turnDoneMsg fires; Esc alone only cancels the ctx, got false")
	}
}

// TestEscTwoPressesCloseThenInterrupt reproduces the user flow:
//  1. type '/'  → picker opens
//  2. press Esc → picker closes (turn untouched)
//  3. press Esc → turn cancels (picker already gone)
//
// The two presses are sequential m.Update calls; the assertions
// check both states (picker visibility and cancel invocation)
// after each press.
func TestEscTwoPressesCloseThenInterrupt(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.Update(runeKey('/'))
	if !m.picker.Visible() {
		t.Fatalf("precondition: picker should be visible after '/'")
	}
	m.streaming = true
	cancelCount := 0
	m.turnCancel = func() { cancelCount++ }

	// First Esc — close the dropdown.
	m.Update(escKey())
	if m.picker.Visible() {
		t.Errorf("after first Esc: picker should be closed, still visible")
	}
	if cancelCount != 0 {
		t.Errorf("after first Esc: cancelCount=%d, want 0 (turn untouched)", cancelCount)
	}
	if m.turnCancel == nil {
		t.Errorf("after first Esc: turnCancel should still be installed")
	}

	// Second Esc — interrupt the turn.
	m.Update(escKey())
	if cancelCount != 1 {
		t.Errorf("after second Esc: cancelCount=%d, want 1 (turn cancelled)", cancelCount)
	}
	if m.turnCancel != nil {
		t.Errorf("after second Esc: turnCancel should be cleared")
	}
}

// TestEscNoDropdownNoStreamAbsorbsSilently is the regression guard
// for the existing "absorb Esc silently when nothing else applies"
// branch. Pressing Esc with the picker closed, no turn in flight,
// and an empty input must do nothing — including NOT setting any
// new state and NOT invoking any cancel.
func TestEscNoDropdownNoStreamAbsorbsSilently(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	if m.picker.Visible() {
		t.Fatalf("precondition: picker must start hidden")
	}
	if m.streaming {
		t.Fatalf("precondition: must not be streaming")
	}
	cancelCount := 0
	m.turnCancel = func() { cancelCount++ }

	m.Update(escKey())

	if m.picker.Visible() {
		t.Errorf("picker must remain hidden on no-op Esc")
	}
	if m.streaming {
		t.Errorf("streaming must remain false on no-op Esc")
	}
	if cancelCount != 0 {
		t.Errorf("cancelCount=%d, want 0 (no turn to cancel)", cancelCount)
	}
	if m.input.Value() != "" {
		t.Errorf("input must remain empty on no-op Esc, got %q", m.input.Value())
	}
	if len(m.response) != 0 {
		t.Errorf("no-op Esc must not append to response, got %d rows", len(m.response))
	}
}

// TestEscStreamingWithoutCancelDoesNotCrash covers the stale-flag
// guard: streaming=true but turnCancel==nil (the test fakeClient
// returns nil from Stream, so the runMode is left in streaming=true
// with no live cancel — the same shape a backend error leaves the
// session in). Esc must NOT panic and must NOT block; it should
// fall through to the silent-absorb branch.
func TestEscStreamingWithoutCancelDoesNotCrash(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.streaming = true
	if m.turnCancel != nil {
		t.Fatalf("precondition: turnCancel should start nil")
	}

	// Should not panic, should not append, should not crash.
	m.Update(escKey())

	if m.turnCancel != nil {
		t.Errorf("stale streaming without cancel must leave turnCancel nil")
	}
	if !m.streaming {
		t.Errorf("stale streaming flag must not be cleared by Esc, got false")
	}
	if len(m.response) != 0 {
		t.Errorf("no response rows should be appended on stale-streaming Esc, got %d", len(m.response))
	}
}

// TestEscInputClearStillWorks pins that the long-standing "Esc
// clears the input when there's text" branch is not regressed by
// the new streaming-interrupt branch. Priority: picker-close >
// streaming-interrupt > input-clear > silent-absorb. So Esc with
// input text + a turn in flight + no picker must clear the input
// (the input-clear branch fires BEFORE the silent-absorb branch,
// but the streaming-interrupt branch fires BEFORE input-clear and
// takes precedence when turnCancel != nil).
//
// The spec is "second Esc interrupts" — once the picker is closed
// and the turn is still streaming, the *next* Esc interrupts the
// turn (input is left alone). This matches the reference binary's
// behaviour captured in /tmp/agycmp/tui/esc_interrupts.esc.txt.
func TestEscInputClearOrInterruptPriority(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.Update(runeKey('h'))
	m.Update(runeKey('i'))
	if got := m.input.Value(); got != "hi" {
		t.Fatalf("precondition: input should be 'hi', got %q", got)
	}
	m.streaming = true
	cancelCount := 0
	m.turnCancel = func() { cancelCount++ }

	m.Update(escKey())

	// Streaming-interrupt takes priority over input-clear: the
	// turn is interrupted, the input text "hi" stays.
	if cancelCount != 1 {
		t.Errorf("Esc with streaming+no picker+input text must cancel turn, got cancelCount=%d", cancelCount)
	}
	if got := m.input.Value(); got != "hi" {
		t.Errorf("Esc with streaming+no picker must leave input text intact (interrupts turn, not input), got %q", got)
	}
}
