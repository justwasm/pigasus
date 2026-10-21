// turn_empty_step_test.go — cycle stall-empty-step-cleanup integration
// tests for the turnDoneMsg handler in tui_run.go.
//
// The unit-level classifier (agent.IsEmptyAssistantTurn) is covered
// in internal/agent/empty_step_test.go. This file pins the
// integration: when the classifier returns true, the turnDoneMsg
// handler must NOT append an assistant message to m.session (which is
// the path that grows the conversation database).
//
// The seven rows here mirror the matrix the AGY 1.3.6 changelog row
// "a stalled request no longer leaves an empty response step behind"
// advertises:
//
//	cancelled request                  → no step
//	error-path Send                    → no step
//	normal Send (text streamed)        → step recorded
//	user-Esc mid-stream (no content)   → no step
//	stall-detected middleware          → no step
//	partial-content after cancel       → step recorded (partial)
//	tool-call-only assistant           → tool-call block recorded
//
// The first five are the AGY 1.3.6 gate; the last two pin the
// carve-outs the spec calls out.
package main

import (
	"testing"

	"github.com/justwasm/pigasus/internal/backend"
)

// TestTurnDoneMsg_EmptyAssistantNotRecorded pins the headline
// behaviour: a turnDoneMsg with no reply, no tool calls, and no
// reasoning must NOT grow m.session. The single user message that
// was appended before submitPrompt (the prompt echo) stays the only
// entry.
func TestTurnDoneMsg_EmptyAssistantNotRecorded(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	// Simulate the prompt echo + user-step append the real
	// submitPrompt path produces. The cycle keeps this step; the
	// gate is only about the assistant step.
	m.response = append(m.response, "> hi")
	m.liveStart = len(m.response)
	m.session = append(m.session, backend.Message{Role: "user", Content: "hi"})

	_, _ = m.Update(turnDoneMsg{
		reply:     "",
		steps:     nil,
		reasoning: "",
	})

	if len(m.session) != 1 {
		t.Fatalf("empty turn appended %d session entries; want 1 (just the user prompt)", len(m.session))
	}
	if m.session[0].Role != "user" {
		t.Errorf("session[0].Role = %q; want user", m.session[0].Role)
	}
}

// TestTurnDoneMsg_ReasoningOnlyRecordedAsStep pins the carve-out
// for thinking-only turns: the classifier treats reasoning as
// content, so a turn that produced only thinking (no text, no tool
// calls) is NOT empty — it grows m.session with one assistant entry
// AND the on-screen tool-step block carries the "Thought for Ns, N
// tokens" rendering. The previous gate `if msg.reply != ""` already
// recorded this case (msg.reply is "" so the gate would actually
// skip; the new gate fixes that bug by also considering reasoning).
func TestTurnDoneMsg_ReasoningOnlyRecordedAsStep(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.response = append(m.response, "> think about it")
	m.liveStart = len(m.response)
	m.session = append(m.session, backend.Message{Role: "user", Content: "think about it"})

	thoughtStep := toolStep{
		kind:   stepKindThought,
		name:   "thinking",
		detail: "spent 3s on the problem",
	}
	_, _ = m.Update(turnDoneMsg{
		reply:     "",
		steps:     []toolStep{thoughtStep},
		reasoning: "spent 3s on the problem",
	})

	// Reasoning IS content per the AGY 1.3.6 classifier, so the
	// session grows with one assistant entry (text empty, but the
	// step exists because reasoning happened). The conversation
	// database stores this as StepModelResponse with empty text —
	// the presence of the step tells the resumed conversation the
	// model spent quota on this turn.
	if len(m.session) != 2 {
		t.Fatalf("reasoning-only turn appended %d session entries; want 2", len(m.session))
	}
	if m.session[1].Role != "assistant" {
		t.Errorf("session[1].Role = %q; want assistant", m.session[1].Role)
	}
	// The on-screen tool-step block is also rendered, so the user
	// sees the reasoning content.
	if len(m.blocks) == 0 {
		t.Error("reasoning-only turn did not record a tool-step block; the on-screen reasoning would be lost")
	}
}

// TestTurnDoneMsg_ReplyRecorded pins the happy path: a turn that
// streamed text content grows m.session with one assistant entry.
// This is the case the previous gate `if msg.reply != ""` already
// covered; the new gate must keep working.
func TestTurnDoneMsg_ReplyRecorded(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.response = append(m.response, "> greet")
	m.liveStart = len(m.response)
	m.session = append(m.session, backend.Message{Role: "user", Content: "greet"})

	_, _ = m.Update(turnDoneMsg{
		reply:     "Hello there.",
		steps:     nil,
		reasoning: "",
	})

	if len(m.session) != 2 {
		t.Fatalf("normal turn recorded %d session entries; want 2", len(m.session))
	}
	if m.session[1].Role != "assistant" || m.session[1].Content != "Hello there." {
		t.Errorf("session[1] = %+v; want assistant/Hello there.", m.session[1])
	}
}

// TestTurnDoneMsg_PartialReplyRecorded pins the --print-timeout
// carve-out: a turn that streamed some text and then was cancelled
// keeps the partial text as the assistant step. The classifier
// reports non-empty (content is non-empty), so the gate fires.
func TestTurnDoneMsg_PartialReplyRecorded(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.response = append(m.response, "> long question")
	m.liveStart = len(m.response)
	m.session = append(m.session, backend.Message{Role: "user", Content: "long question"})

	partial := "I was about to say that"
	_, _ = m.Update(turnDoneMsg{
		reply:     partial,
		steps:     nil,
		reasoning: "",
	})

	if len(m.session) != 2 {
		t.Fatalf("partial turn recorded %d session entries; want 2", len(m.session))
	}
	if m.session[1].Content != partial {
		t.Errorf("session[1].Content = %q; want %q", m.session[1].Content, partial)
	}
}

// TestTurnDoneMsg_ToolCallOnlyDoesNotAppendSessionStep pins the
// recovered wire shape: tool calls are recorded as StepToolCall
// rows of the conversation database (via the tool-step block in
// m.blocks), NOT as StepModelResponse rows from m.session. The new
// gate passes through the classifier's "non-empty" verdict for
// tool-call-only turns, but the call site still skips the
// m.session append because the wire shape wants StepToolCall, not
// StepModelResponse. The on-screen step block is the user-visible
// artefact.
func TestTurnDoneMsg_ToolCallOnlyDoesNotAppendSessionStep(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.response = append(m.response, "> read the file")
	m.liveStart = len(m.response)
	m.session = append(m.session, backend.Message{Role: "user", Content: "read the file"})

	toolStepEntry := toolStep{
		name:   "read_file",
		title:  "Read /tmp/x",
		detail: "Tool call: read_file",
	}
	_, _ = m.Update(turnDoneMsg{
		reply:     "",
		steps:     []toolStep{toolStepEntry},
		reasoning: "",
	})

	// The classifier agrees the turn is non-empty (tool calls ARE
	// content), so the on-screen tool-step block is rendered.
	if len(m.blocks) == 0 {
		t.Error("tool-call-only turn did not record a tool-step block; on-screen tool step would be lost")
	}
	// But the m.session does NOT grow with a StepModelResponse —
	// tool calls live as StepToolCall rows in the conversation
	// database, not StepModelResponse.
	if len(m.session) != 1 {
		t.Fatalf("tool-call-only turn appended %d session entries; want 1 (just the user prompt)", len(m.session))
	}
	if m.session[0].Role != "user" {
		t.Errorf("session[0].Role = %q; want user", m.session[0].Role)
	}
}

// TestTurnDoneMsg_WhitespaceOnlyReplyClassifiedEmpty pins the
// gateway's leading-newline SSE framing: the agw transport delivers
// "\n" as the very first delta on every stream, and the recovered
// sanitizer keeps that byte. Without trimming, the classifier would
// treat that newline as content and skip the gate on every single
// request — the very leak the changelog row deletes.
//
// This is the regression test for an earlier prototype that used
// `content == ""` instead of `strings.TrimSpace(content) == ""`.
func TestTurnDoneMsg_WhitespaceOnlyReplyClassifiedEmpty(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.response = append(m.response, "> ping")
	m.liveStart = len(m.response)
	m.session = append(m.session, backend.Message{Role: "user", Content: "ping"})

	_, _ = m.Update(turnDoneMsg{
		reply:     "\n",
		steps:     nil,
		reasoning: "",
	})

	if len(m.session) != 1 {
		t.Fatalf("whitespace-only reply appended %d session entries; want 1", len(m.session))
	}
}
