package main

// slash_cycle221_test.go — cycle 221: close TODO §2.1 / §4 cycle-187
// follow-up by wiring the /feedback stage-2 text input box to the openagy
// submit semantics. The stage-2 form (title + body + feedback label +
// "Additional Details (optional):" + buffer + blanks + legend) is the
// cycle-208 panel render, byte-for-byte against REF 1.4.0's
// fin_fb_stage2.raw.txt; the new test pins live behaviour:
//
//	1. The stage-2 buffer is a single-line text field that fills on
//	   printable-key presses, removes a rune on backspace, and starts
//	   empty when the panel transitions stage 0 → stage 1.
//	2. Enter in stage 2 closes the panel and emits the cycle-192 local-only
//	   message. ctrl+s is a parallel submit key.
//	3. Esc returns to stage 1 with the same category (cursor index) still
//	   selected and the buffer cleared.
//	4. Stage 1 retains the cycle-187/201 bindings: 1-6 select-and-continue,
//	   ↑/↓ wrap at both ends, plain Enter advances to stage 2 with the
//	   default category (cursor 0).
//
// Reference evidence: /tmp/agy221/agy221_ref_s2_empty.txt (stage 2
// immediately after `1`), /tmp/agy221/agy221_ref_s2_typed.txt (stage 2
// after typing "hello world"), and /tmp/agy221/agy221_ref_stage1.txt
// (the stage-1 picker). REF's stage-2 legend is ` ctrl+s Submit
// Feedback   esc Back` and Enter does not submit — openagy deviates on
// the submit key alone; the panel shape matches REF.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// Stage 2 — empty: the buffer is empty and the cursor block is the
// only thing on row 8 (matches REF's empty /feedback stage-2 capture,
// where the buffer line is a single reverse-video space).
func TestCycle221_FeedbackStage2BufferEmpty(t *testing.T) {
	m := mkPanel(panelFeedback, "/feedback", 2)
	m.panel.stage = 1
	m.panel.text = ""
	lines := m.panelLines()
	if got := lines[7]; got != searchCursorBlock {
		t.Errorf("empty buffer row = %q, want %q", got, searchCursorBlock)
	}
	if m.panel.text != "" {
		t.Errorf("empty buffer text = %q, want empty", m.panel.text)
	}
}

// Stage 2 — typing: printable keys append to the buffer one rune per
// press (single-line field, no soft-wrap). Cycle 208 already shipped
// the append path; this test pins the cycle-221 contract.
func TestCycle221_FeedbackStage2BufferTyping(t *testing.T) {
	m := mkPanel(panelFeedback, "/feedback", 0)
	m.panel.stage = 1
	for _, r := range []string{"h", "e", "l", "l", "o", " ", "w", "o", "r", "l", "d"} {
		if handled, _ := m.handlePanelKey(tea.Key{Text: r}); !handled {
			t.Fatalf("printable key %q not handled", r)
		}
	}
	if m.panel.text != "hello world" {
		t.Errorf("buffer = %q, want %q", m.panel.text, "hello world")
	}
	// Backspace removes one rune.
	m.handlePanelKey(tea.Key{Code: tea.KeyBackspace})
	if m.panel.text != "hello worl" {
		t.Errorf("after backspace = %q, want %q", m.panel.text, "hello worl")
	}
	// Backspace on an empty buffer is a no-op (no underflow).
	m.panel.text = ""
	m.handlePanelKey(tea.Key{Code: tea.KeyBackspace})
	if m.panel.text != "" {
		t.Errorf("backspace on empty buffer = %q, want empty", m.panel.text)
	}
}

// Stage 2 — Enter submits (openagy-specific divergence from REF's
// ctrl+s-only textarea). The submit closes the panel and appends the
// cycle-192 local-only message to m.response.
func TestCycle221_FeedbackStage2EnterSubmits(t *testing.T) {
	m := mkPanel(panelFeedback, "/feedback", 2)
	m.panel.stage = 1
	m.panel.text = "model got stuck in a loop"
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEnter}); !handled {
		t.Fatal("enter not handled in stage 2")
	}
	if m.panel.kind != panelNone {
		t.Fatalf("enter did not close the panel: kind=%v", m.panel.kind)
	}
	got := stripSGR(strings.Join(m.response, "\n"))
	want := "Feedback recorded (openagy has no upload endpoint; saved locally only)"
	if !strings.Contains(got, want) {
		t.Errorf("submit echo missing %q:\n%s", want, got)
	}
	// The slashRespond wrapper puts the command header on the line above.
	if !strings.Contains(got, "> /feedback") {
		t.Errorf("submit echo missing the > /feedback header:\n%s", got)
	}
}

// Stage 2 — Enter with empty buffer still submits (openagy UX: pressing
// Enter is the only way out of stage 2; an empty draft is valid).
func TestCycle221_FeedbackStage2EnterSubmitsEmpty(t *testing.T) {
	m := mkPanel(panelFeedback, "/feedback", 5)
	m.panel.stage = 1
	m.panel.text = ""
	m.handlePanelKey(tea.Key{Code: tea.KeyEnter})
	if m.panel.kind != panelNone {
		t.Fatalf("enter on empty buffer did not submit: kind=%v", m.panel.kind)
	}
	got := stripSGR(strings.Join(m.response, "\n"))
	if !strings.Contains(got, "Feedback recorded") {
		t.Errorf("empty-buffer submit echo missing: %q", got)
	}
}

// Stage 2 — Esc returns to stage 1 with the same category still
// selected (the cursor index is preserved) and the draft discarded.
func TestCycle221_FeedbackStage2EscReturnsToStage1(t *testing.T) {
	m := mkPanel(panelFeedback, "/feedback", 4)
	m.panel.stage = 1
	m.panel.text = "some draft"
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEsc}); !handled {
		t.Fatal("esc not handled in stage 2")
	}
	if m.panel.stage != 0 {
		t.Errorf("esc did not return to stage 1: stage=%d", m.panel.stage)
	}
	if m.panel.text != "" {
		t.Errorf("esc did not discard the draft: text=%q", m.panel.text)
	}
	if m.panel.kind != panelFeedback {
		t.Errorf("esc closed the panel: kind=%v", m.panel.kind)
	}
	// Same category still selected (cursor preserved).
	if m.panel.cursor != 4 {
		t.Errorf("esc lost the category: cursor=%d, want 4", m.panel.cursor)
	}
	// Re-entering stage 2 from the picker leaves the buffer empty
	// (cycle-208 stage 0 → stage 1 does not carry the discarded draft).
	m.handlePanelKey(tea.Key{Code: tea.KeyEnter})
	if m.panel.stage != 1 {
		t.Fatalf("re-enter did not advance: stage=%d", m.panel.stage)
	}
	if m.panel.text != "" {
		t.Errorf("re-enter should leave the buffer empty: text=%q", m.panel.text)
	}
	if m.panel.cursor != 4 {
		t.Errorf("re-enter lost the category: cursor=%d, want 4", m.panel.cursor)
	}
}

// Stage 1 — cycle-187 wrapping + cycle-201 select-and-continue still
// work after the cycle-221 key changes: 1-6 select-and-continue
// (advances to stage 2 with that row highlighted), a plain Enter
// advances to stage 2 with the current category (cursor 0 by default),
// and Up/Down wrap at the boundaries (cycle 187 `wraps()` hook).
func TestCycle221_FeedbackStage1BindingsRetained(t *testing.T) {
	// 1-6 select-and-continue: each press advances to stage 2 with that
	// row highlighted. The cycle-201 handler routes only when the panel
	// is in stage 0 (the wrap-around branch above), so we need a fresh
	// panel for each press — a single panel ends up in stage 2 after
	// the first press and "3" / "5" would just type into the buffer.
	for _, k := range []string{"1", "3", "5"} {
		m := mkPanel(panelFeedback, "/feedback", 0)
		m.handlePanelKey(tea.Key{Text: k})
		want := int(k[0] - '1')
		if m.panel.stage != 1 || m.panel.cursor != want {
			t.Fatalf("'%s' select-and-continue: stage=%d cursor=%d, want 1, %d",
				k, m.panel.stage, m.panel.cursor, want)
		}
	}

	// Plain Enter advances with the current category.
	m := mkPanel(panelFeedback, "/feedback", 0)
	m.handlePanelKey(tea.Key{Code: tea.KeyEnter})
	if m.panel.stage != 1 || m.panel.cursor != 0 {
		t.Fatalf("plain enter: stage=%d cursor=%d, want 1, 0", m.panel.stage, m.panel.cursor)
	}
	m.handlePanelKey(tea.Key{Code: tea.KeyEsc})

	// Wrap-around: Up at cursor 0 wraps to the last row (cycle 187
	// `wraps()` hook), Down at cursor 5 wraps to 0.
	m.handlePanelKey(tea.Key{Code: tea.KeyUp})
	if m.panel.cursor != 5 {
		t.Errorf("up at cursor 0 should wrap to 5, got %d", m.panel.cursor)
	}
	m.handlePanelKey(tea.Key{Code: tea.KeyDown})
	if m.panel.cursor != 0 {
		t.Errorf("down at cursor 5 should wrap to 0, got %d", m.panel.cursor)
	}
}
