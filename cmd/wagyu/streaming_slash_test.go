package main

// cycle289_streaming_slash_test.go — slash commands during a live turn.
//
// agy 1.4.3 dispatches local/panel slash commands while the model is
// still streaming (verified against a live 1.4.3 session driven by a
// slow mock CloudCode stream: /context, /usage and /model all opened
// with the spinner still running). Cycle 292 finished the split: every
// local command runs immediately (/clear, /new, /fork, /compact, /rewind,
// /resume included), and only commands that start another model turn
// queue.

import (
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/backend"
)

func TestSubmitPrompt_RunsContextPanelWhileStreaming(t *testing.T) {
	m := streamQueueMode(t)
	m.input.SetValue("/context")

	if cmd := m.submitPrompt(); cmd != nil {
		t.Fatalf("submitPrompt(/context) mid-stream returned a cmd, want nil")
	}
	if len(m.streamQueue) != 0 {
		t.Fatalf("streamQueue = %q, want slash command not queued", m.streamQueue)
	}
	if m.panel.kind != panelContext {
		t.Fatalf("panel.kind = %v, want panelContext", m.panel.kind)
	}
	if !m.streaming {
		t.Error("panel dispatch cleared streaming; the in-flight turn must survive")
	}
}

func TestSubmitPrompt_QueuesModelTurnSlashWhileStreaming(t *testing.T) {
	m := streamQueueMode(t)
	m.input.SetValue("/plan next steps")

	if cmd := m.submitPrompt(); cmd != nil {
		t.Fatalf("submitPrompt(/plan) mid-stream returned a cmd, want nil")
	}
	if len(m.streamQueue) != 1 || m.streamQueue[0] != "/plan next steps" {
		t.Fatalf("streamQueue = %q, want /plan queued", m.streamQueue)
	}
	if m.panel.kind != panelNone {
		t.Errorf("panel.kind = %v, want panelNone for a queued /plan", m.panel.kind)
	}
}

// Cycle 292: the reference runs transcript-mutating commands mid-stream
// too. /clear and /new cancel the turn and throw its output away; /compact
// answers without touching the turn; /rewind opens its panel and leaves the
// stream running.
func TestSubmitPrompt_RunsTranscriptMutatingSlashWhileStreaming(t *testing.T) {
	for _, buf := range []string{"/clear", "/new"} {
		m := streamQueueMode(t)
		m.session = []backend.Message{{Role: "user", Content: "live turn"}}
		m.response = []string{"> live turn"}
		m.input.SetValue(buf)
		m.submitPrompt()

		if len(m.streamQueue) != 0 {
			t.Errorf("%s: streamQueue = %q, want immediate dispatch", buf, m.streamQueue)
		}
		if m.streaming {
			t.Errorf("%s: streaming = true, want the turn aborted", buf)
		}
		if !m.discardTurn {
			t.Errorf("%s: discardTurn = false, want the cancelled turn suppressed", buf)
		}
		if len(m.session) != 0 || len(m.response) != 0 {
			t.Errorf("%s: session/response not cleared (session %d, response %d)", buf, len(m.session), len(m.response))
		}
	}

	m := streamQueueMode(t)
	m.input.SetValue("/compact")
	m.submitPrompt()
	if !m.streaming {
		t.Error("/compact mid-stream stopped the turn; the reference keeps streaming")
	}
	// 1.4.4 renders the answer in the input row (gray placeholder after the
	// blue ">"), not as a transcript row.
	if m.inputNotice != "Nothing to compact yet." {
		t.Errorf("/compact mid-stream inputNotice = %q, want %q", m.inputNotice, "Nothing to compact yet.")
	}
	if len(m.response) != 0 {
		t.Errorf("/compact mid-stream wrote a transcript row: %q", m.response)
	}
	if m.input.Value() == "Nothing to compact yet." {
		t.Error("/compact mid-stream put the notice in the editable buffer")
	}
	if m.discardTurn {
		t.Error("/compact mid-stream discarded the turn")
	}

	m = streamQueueMode(t)
	m.input.SetValue("/rewind")
	m.submitPrompt()
	if m.panel.kind != panelRewind {
		t.Errorf("/rewind mid-stream panel = %v, want panelRewind", m.panel.kind)
	}
	if !m.streaming {
		t.Error("/rewind mid-stream stopped the turn; the reference leaves it running")
	}
}

func TestQueuedSlashDispatchesOnTurnDone(t *testing.T) {
	m := streamQueueMode(t)
	m.streamQueue = []string{"/context"}
	prevSession := len(m.session)

	if _, cmd := m.Update(turnDoneMsg{reply: "", steps: nil, reasoning: ""}); cmd == nil {
		t.Fatal("turnDoneMsg with a queued slash returned a nil cmd (ClearScreen should remain)")
	}
	if m.panel.kind != panelContext {
		t.Fatalf("queued /context did not open the panel on flush; panel.kind = %v", m.panel.kind)
	}
	if len(m.session) != prevSession {
		t.Errorf("queued slash was appended to the transcript as a user message (len %d, want %d)",
			len(m.session), prevSession)
	}
	if m.streaming {
		t.Error("queued slash started a model turn; streaming = true")
	}
}

func TestDispatchSlashLine(t *testing.T) {
	m := streamQueueMode(t)
	if handled, _ := m.dispatchSlashLine("plain text"); handled {
		t.Error("dispatchSlashLine(plain text) = handled, want false")
	}
	if handled, _ := m.dispatchSlashLine("/context"); !handled || m.panel.kind != panelContext {
		t.Errorf("/context: handled=%v panel=%v, want handled=true panelContext", handled, m.panel.kind)
	}
}

func TestContextPanel_StreamingLayout(t *testing.T) {
	m := streamQueueMode(t)
	m.width = 120
	m.session = append(m.session, backend.Message{Role: "user", Content: "slow sea story please"})

	lines := m.panelContextLines()
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "Estimated usage (awaiting generation)") {
		t.Errorf("streaming /context missing the awaiting-generation row:\n%s", joined)
	}
	if strings.Contains(joined, "Token usage by category") {
		t.Errorf("streaming /context rendered the completed-session layout:\n%s", joined)
	}
	// Cycle 292: in flight, the step has no stored token count yet, so the
	// reference falls back to max(len(raw)/4, 1) for the user row and
	// len(partial reply)/4 for the agent row.
	wantUser := contextTokenLabel(len("slow sea story please") / 4)
	if !strings.Contains(joined, "User messages: "+wantUser+" tokens") {
		t.Errorf("streaming /context user tokens = want %s:\n%s", wantUser, joined)
	}
	m.liveReplyBuf = "The lighthouse stood at the edge of the harbor, its white tower weathered by salt and wind.  The lighthouse stood"
	joined = strings.Join(m.panelContextLines(), "\n")
	wantAgent := contextTokenLabel(len(m.liveReplyBuf) / 4)
	if !strings.Contains(joined, "Agent responses: "+wantAgent+" tokens") {
		t.Errorf("streaming /context agent tokens = want %s:\n%s", wantAgent, joined)
	}
	if !strings.Contains(joined, "Free space: 1.0M (100.0%)") {
		t.Errorf("streaming /context free row = %q, want 1.0M (100.0%%)", joined)
	}
}

func TestModelPicker_KeepsMiniMaxM3(t *testing.T) {
	var found bool
	for _, r := range modelRowsView() {
		if r.slug == "minimax-m3" {
			found = true
			if r.display != "MiniMax M3" {
				t.Errorf("display = %q, want MiniMax M3", r.display)
			}
			if r.note != "MiniMax M3 via agw (openagy wrapper)" {
				t.Errorf("note = %q, want the agw registry description", r.note)
			}
		}
	}
	if !found {
		t.Fatal("minimax-m3 missing from modelRowsView(); the /model picker must keep it")
	}
}
