package main

// queue_stream_test.go — cycle 283 prompt queueing during a live turn.
//
// Recovered from a truecolour pty capture of agy 1.4.3 (pyte replay of
// /tmp/ref_queue.bin): with a turn in flight, typing a prompt and
// pressing Enter does not interrupt and is not dropped. The reference
//
//   - clears the input,
//   - renders the pending prompt directly above the input box as
//     "▸ <text> (alt+m to send immediately)" in dim grey
//     (#657B83) with an extra separator under it,
//   - swaps the status row's left label to
//     "  Press up to edit queued messages",
//   - and only when the in-flight turn finishes does it echo the prompt
//     into the transcript as a normal bold-blue "> ..." row and start
//     the next turn.
//
// Up on an empty buffer pulls the newest queued prompt back into the
// input (the label above advertises it). Alt+M's "send immediately"
// semantics are NOT modelled — in the clone alt+m still toggles the
// cycle-244 Queue mode. That gap is recorded in the cycle report.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// streamQueueMode returns a runMode already marked as streaming, so
// submitPrompt takes the queue branch. The backend is the test fake;
// no goroutine is started until the turnDoneMsg flush runs.
func streamQueueMode(t *testing.T) *runMode {
	t.Helper()
	m := queueTestMode(t)
	m.width, m.height = 200, 40
	m.streaming = true
	return m
}

// TestSubmitWhileStreamingQueues: Enter mid-turn stashes the prompt,
// clears the input, and leaves the transcript untouched — the echo
// belongs to the flush, so the live streamed reply is never
// interleaved with a user row.
func TestSubmitWhileStreamingQueues(t *testing.T) {
	m := streamQueueMode(t)
	m.input.SetValue("SECOND message while running")
	before := len(m.response)

	if cmd := m.submitPrompt(); cmd != nil {
		t.Fatalf("submitPrompt mid-stream returned a cmd, want nil")
	}
	if len(m.streamQueue) != 1 || m.streamQueue[0] != "SECOND message while running" {
		t.Fatalf("streamQueue = %q, want the submitted prompt", m.streamQueue)
	}
	if m.input.Value() != "" {
		t.Errorf("input not cleared after queueing: %q", m.input.Value())
	}
	if len(m.response) != before {
		t.Errorf("queueing echoed %d transcript rows, want 0", len(m.response)-before)
	}
}

// TestQueuedPromptRendersAboveInput pins the pending row, the extra
// separator and the status label. The row is the reference's
// byte shape (#657B83 + "▸ " + text + " (alt+m to send immediately)").
func TestQueuedPromptRendersAboveInput(t *testing.T) {
	m := streamQueueMode(t)
	m.streamQueue = []string{"SECOND message while running"}

	view := m.View().Content
	if !strings.Contains(view, cDimGrayOpen+"\u25b8 SECOND message while running (alt+m to send immediately)") {
		t.Errorf("pending queue row missing or mis-styled:\n%q", view)
	}
	plain := stripSGR(m.statusRow())
	if !strings.Contains(plain, "  Press up to edit queued messages") {
		t.Errorf("status left label = %q, want the up-to-edit hint", plain)
	}
	if strings.Contains(plain, "esc to cancel") {
		t.Errorf("status still shows esc-to-cancel while a prompt is queued: %q", plain)
	}
}

// TestTurnDoneFlushesQueuedPrompt: the finished turn's rows are
// finalised first, then the queued prompt is echoed in user-row style
// and dispatched as the next turn.
func TestTurnDoneFlushesQueuedPrompt(t *testing.T) {
	m := streamQueueMode(t)
	m.streamQueue = []string{"SECOND message while running"}

	_, cmd := m.Update(turnDoneMsg{reply: "DONE"})
	if len(m.streamQueue) != 0 {
		t.Fatalf("streamQueue not drained: %q", m.streamQueue)
	}
	if cmd == nil {
		t.Fatalf("turnDoneMsg returned nil cmd, want the queued streamPrompt batch")
	}
	last := m.response[len(m.response)-1]
	if last != userPromptRow("SECOND message while running") {
		t.Errorf("flushed user row = %q, want %q", last, userPromptRow("SECOND message while running"))
	}
	if len(m.session) == 0 || m.session[len(m.session)-1].Content != "SECOND message while running" {
		t.Errorf("queued prompt not appended to the session: %#v", m.session)
	}
}

// TestUpRecallsQueuedPrompt: Up on an empty buffer moves the newest
// pending prompt back into the input and drops it from the queue.
func TestUpRecallsQueuedPrompt(t *testing.T) {
	m := streamQueueMode(t)
	m.streamQueue = []string{"first", "second"}

	key := tea.KeyPressMsg(tea.Key{Code: tea.KeyUp})
	m.Update(key)

	if got := m.input.Value(); got != "second" {
		t.Errorf("input after Up = %q, want %q", got, "second")
	}
	if len(m.streamQueue) != 1 || m.streamQueue[0] != "first" {
		t.Errorf("streamQueue after Up = %q, want [first]", m.streamQueue)
	}
	// With the buffer non-empty the next Up is history recall again,
	// not a second pop.
	m.Update(key)
	if len(m.streamQueue) != 1 {
		t.Errorf("second Up popped another queued prompt: %q", m.streamQueue)
	}
}
