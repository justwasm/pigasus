// slash_cycle227_test.go — auto-compact wiring tests for the TUI side
// of cycle 227. The runtime side (TestRuntimeCompaction*) lives in
// internal/pigrt/cycle227_test.go; the tests here exercise the runMode
// state machine: feeding a compactStartMsg / compactEndMsg pair through
// m.Update and asserting (a) the status row carries "Compacting context…"
// while the spinner is active, (b) the same row clears once the end
// event fires, and (c) the merged record includes the upstream
// tokens-after so the post-compact row reads "Compacted 184 K → 42 K".
package main

import (
	"strings"
	"testing"
	"time"
)

// TestStatusRowCompactingSpinner is the cycle-227 TUI integration test
// the spec calls out: fire synthetic compact events, then read
// m.statusRow() at each phase. The row is a plain string the existing
// cycle-87 / 182 / 217 tests already pin down; the goal here is the
// *compact* lifecycle, not the row's full geometry (that lives in
// TestStatusRowGeometry and friends under tui_run_test.go).
//
// Unlike cycle 225, this test runs with no panel open — panelMCP and
// panelUsage both blank the left label via statusRow()'s
// `keepStatusHint()` branch, so a panel-open baseline would not carry
// "? for shortcuts" to assert on, and would mask the compact note's
// overlay (they both render to the same left cell).
func TestStatusRowCompactingSpinner(t *testing.T) {
	m := &runMode{}
	m.panel = slashPanel{kind: panelNone}
	m.width, m.height = 220, 50

	// 1. Idle row carries "?" for shortcuts — pre-compact baseline.
	idleRow := m.statusRow()
	if !strings.Contains(idleRow, "? for shortcuts") {
		t.Fatalf("idle status row missing hint: %q", idleRow)
	}
	if strings.Contains(idleRow, "Compacting context") {
		t.Errorf("idle row should not carry compact note: %q", idleRow)
	}

	// 2. Fire compactStartMsg. The status row now carries
	// "Compacting context…" with the spinner glyph (from sp.Dot).
	startAt := time.Now().UTC()
	m.Update(compactStartMsg{Reason: "threshold", StartedAt: startAt})

	active := m.statusRow()
	if !strings.Contains(active, "Compacting context") {
		t.Errorf("active row missing compact note: %q", active)
	}
	// The 8-frame sp.Dot set is ⣾⣽⣻⢿⡿⣟⣯⣷ — cycle 227 starts at
	// frame 0 (the very first tick).
	frames := []string{"⣾", "⣽", "⣻", "⢿", "⡿", "⣟", "⣯", "⣷"}
	if !strings.Contains(active, frames[0]) {
		t.Errorf("active row missing initial spinner frame %q: %q", frames[0], active)
	}

	// 3. Fire a single spin tick and verify the frame advanced.
	m.Update(compactSpinnerTickMsg{})
	ticked := m.statusRow()
	if !strings.Contains(ticked, frames[1]) {
		t.Errorf("after one tick, expected frame %q: %q", frames[1], active)
	}

	// 4. Fire compactEndMsg with token totals. After this the row
	// reflects the post-compact "Compacted 184 K → 42 K" form (no
	// spinner, since the active flag is cleared).
	endAt := startAt.Add(3 * time.Second)
	m.Update(compactEndMsg{
		Reason:               "threshold",
		EndedAt:              endAt,
		TokensBefore:         184_000,
		EstimatedTokensAfter: 42_000,
	})

	ended := m.statusRow()
	if !strings.Contains(ended, "184 K") {
		t.Errorf("post-compact row missing '184 K' (tokensBefore): %q", ended)
	}
	if !strings.Contains(ended, "42 K") {
		t.Errorf("post-compact row missing '42 K' (tokensAfter): %q", ended)
	}
	if !strings.Contains(ended, "3s") {
		t.Errorf("post-compact row missing duration '3s': %q", ended)
	}
	// Spinner frames must NOT appear post-end (compactActive cleared).
	for _, frame := range frames {
		if strings.Contains(ended, frame) {
			t.Errorf("post-compact row still carries spinner %q: %q", frame, ended)
		}
	}
}

// TestStatusRowCompactTickClearedByEnd pins the tick-stop behaviour:
// once the end event fires, the spinning tick immediately returns nil
// so the chain stops on its own — no orphaned spinner frames after
// compact finishes. Without this guard the status row would keep
// repainting the spinner label forever (and the leak would be hard to
// debug in production).
func TestStatusRowCompactTickClearedByEnd(t *testing.T) {
	m := &runMode{}
	m.panel = slashPanel{kind: panelNone}
	m.width, m.height = 220, 50

	// Start, then end.
	m.Update(compactStartMsg{Reason: "threshold", StartedAt: time.Now().UTC()})
	m.Update(compactEndMsg{
		Reason:               "threshold",
		EndedAt:              time.Now().Add(time.Second),
		TokensBefore:         100_000,
		EstimatedTokensAfter: 20_000,
	})

	// Subsequent ticks must return nil — the spinner would otherwise
	// "freeze" on a stale frame because no further events overwrite
	// it. We pin the contract by inspecting Update's second return.
	_, cmd := m.Update(compactSpinnerTickMsg{})
	if cmd != nil {
		t.Errorf("post-end tick returned a cmd (%v); should be nil to stop the chain", cmd)
	}
}

// TestStatusRowCompactNoStart asserts the row stays at the idle hint
// when a synthetic compactEndMsg arrives without a preceding start
// — the runtime guard internal/agent/loop.go:recordCompact enforces
// here. (The print path does the same; both halves of the contract
// are exercised at the same place because the bookkeeping moves the
// unset-state check into the row render.)
func TestStatusRowCompactNoStart(t *testing.T) {
	m := &runMode{}
	m.panel = slashPanel{kind: panelNone}
	m.width, m.height = 220, 50

	// End without start. m.compactStart is zero, m.compactEnd
	// becomes non-zero — the row must NOT switch to compact mode
	// (only the start branch arms it).
	m.Update(compactEndMsg{
		Reason:               "threshold",
		EndedAt:              time.Now().Add(time.Second),
		TokensBefore:         100_000,
		EstimatedTokensAfter: 20_000,
	})

	row := m.statusRow()
	// The row must NOT carry the compact note without a preceding start.
	if strings.Contains(row, "Compacting context") {
		t.Errorf("end-without-start row should not enter compact mode: %q", row)
	}
	// Idle hint preserved.
	if !strings.Contains(row, "? for shortcuts") {
		t.Errorf("end-without-start should leave the idle hint: %q", row)
	}
}
