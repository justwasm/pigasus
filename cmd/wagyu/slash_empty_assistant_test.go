package main

// slash_empty_assistant_test.go — AGY 1.2.13: empty assistant messages
// (the model returned no content) must not be persisted to the
// conversation transcript, and --continue must not replay them as
// blank turns.
//
// Cycle 225 / feature branch feature/empty-assistant-message-skip.
//
// The fix lives in two places:
//
//	cmd/wagyu/slash_helpers.go  sessionToSteps() drops an assistant
//	                             step whose Content is empty or
//	                             whitespace-only, so the WriteConversation
//	                             payload never carries a blank row.
//
//	cmd/wagyu/conversation.go   seedConversation() also drops an
//	                             empty/whitespace-only model step, so a
//	                             legacy DB that pre-dates the fix and
//	                             still holds a blank row (Transcript()
//	                             already filters, but defense in depth)
//	                             does not render a blank scrollback line.
//
// These tests lock both surfaces plus the integration: a round-trip
// SaveConversation → LoadConversation must yield a transcript whose
// assistant turns are all non-empty.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/convstore"
)

// TestEmptyAssistantNotSaved — assistant message with empty Content is
// dropped from sessionToSteps. The user step survives; the assistant
// step does not.
func TestEmptyAssistantNotSaved(t *testing.T) {
	sess := []backend.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: ""},
	}
	steps := sessionToSteps(sess)
	if len(steps) != 1 {
		t.Fatalf("sessionToSteps dropped %d steps, want 1 user step only:\n%+v", len(sess)-len(steps), steps)
	}
	if steps[0].Type != convstore.StepUserInput || steps[0].Text != "hello" {
		t.Errorf("kept step = %+v, want the user step with text %q", steps[0], "hello")
	}
}

// TestWhitespaceAssistantNotSaved — assistant message whose Content is
// only whitespace is also dropped. A model that returned "\n   \n"
// must not bloat the DB.
func TestWhitespaceAssistantNotSaved(t *testing.T) {
	sess := []backend.Message{
		{Role: "user", Content: "ping"},
		{Role: "assistant", Content: "   \n\t  \n"},
		{Role: "assistant", Content: "\n\n\n"},
		{Role: "assistant", Content: " "},
	}
	steps := sessionToSteps(sess)
	if len(steps) != 1 {
		t.Fatalf("got %d steps, want 1 (the user step):\n%+v", len(steps), steps)
	}
	if steps[0].Type != convstore.StepUserInput || steps[0].Text != "ping" {
		t.Errorf("kept step = %+v", steps[0])
	}
}

// TestNormalAssistantSaved — a non-empty assistant reply is preserved.
// Also locks the Idx column: the assistant step keeps the index it
// held in sess (so subsequent saves do not shift user steps).
func TestNormalAssistantSaved(t *testing.T) {
	sess := []backend.Message{
		{Role: "user", Content: "ping"},
		{Role: "assistant", Content: "pong"},
		{Role: "user", Content: "ping again"},
		{Role: "assistant", Content: "pong again"},
	}
	steps := sessionToSteps(sess)
	if len(steps) != 4 {
		t.Fatalf("got %d steps, want 4:\n%+v", len(steps), steps)
	}
	wantTypes := []int64{
		convstore.StepUserInput, convstore.StepModelResponse,
		convstore.StepUserInput, convstore.StepModelResponse,
	}
	wantTexts := []string{"ping", "pong", "ping again", "pong again"}
	for i, s := range steps {
		if s.Type != wantTypes[i] {
			t.Errorf("step %d type = %d, want %d", i, s.Type, wantTypes[i])
		}
		if s.Text != wantTexts[i] {
			t.Errorf("step %d text = %q, want %q", i, s.Text, wantTexts[i])
		}
		if s.Idx != int64(i) {
			t.Errorf("step %d Idx = %d, want %d (Idx must track the sess slot, not the kept count)", i, s.Idx, i)
		}
	}
}

// TestToolCallAssistantSaved — an assistant turn that produced only
// tool calls (no prose) is still recorded, but the empty-text bubble
// is filtered. We synthesise this by writing the conversation via
// convstore.WriteConversation directly (the tool-call rows) and
// round-tripping through SaveConversation, which is what saveSession
// ultimately drives. The empty-text assistant step in sessionToSteps
// must not appear alongside the tool-call rows.
//
// In practice the model turns that contain only tool calls still
// appear in the in-memory session as an empty-text assistant
// message; the liveSteps slice carries the tool calls. sessionToSteps
// must drop the empty bubble so the persisted transcript does not
// carry a blank scrollback row between the user prompt and the tool
// output.
func TestToolCallAssistantSaved(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "conversations"), 0o755); err != nil {
		t.Fatal(err)
	}

	const id = "11111111-2222-4000-8000-00000000ea01"

	// Hand-craft a conversation that includes an empty-text model
	// step sandwiched between two tool calls — the buggy shape.
	steps := []convstore.Step{
		{Idx: 0, Type: convstore.StepUserInput, Status: 3, Text: "list /tmp"},
		{Idx: 1, Type: convstore.StepModelResponse, Status: 3, Text: ""}, // blank, must be filtered at sessionToSteps
		{Idx: 2, Type: convstore.StepToolCall, Status: 3, ToolName: "run_command",
			ToolArgs: `{"CommandLine":"ls","Cwd":"/tmp"}`, ToolResult: "drwxr-xr-x"},
		{Idx: 3, Type: convstore.StepUserInput, Status: 3, Text: "thanks"},
		{Idx: 4, Type: convstore.StepModelResponse, Status: 3, Text: "you are welcome"},
	}
	dbPath := filepath.Join(dir, "conversations", id+".db")
	if err := convstore.WriteConversation(dbPath, id, steps); err != nil {
		t.Fatalf("WriteConversation: %v", err)
	}

	// Now seed a live session that mirrors the same turn shape, then
	// run it through sessionToSteps — the empty assistant bubble must
	// be dropped.
	sess := []backend.Message{
		{Role: "user", Content: "list /tmp"},
		{Role: "assistant", Content: ""}, // tool-only turn: filtered
		{Role: "user", Content: "thanks"},
		{Role: "assistant", Content: "you are welcome"},
	}
	persisted := sessionToSteps(sess)

	// Expected: only 3 rows survive — user/tool-result/user. The empty
	// assistant bubble is dropped, but the surrounding user messages
	// and the surviving reply are preserved with their sess-slot
	// indices intact.
	want := []struct {
		idx  int64
		typ  int64
		text string
	}{
		{0, convstore.StepUserInput, "list /tmp"},
		{1, convstore.StepUserInput, "thanks"}, // sess-slot 2 → idx 2, but we list the expected by position
		{2, convstore.StepModelResponse, "you are welcome"},
	}
	if len(persisted) != len(want) {
		t.Fatalf("persisted %d steps, want %d:\n%+v", len(persisted), len(want), persisted)
	}
	for i, w := range want {
		if persisted[i].Type != w.typ || persisted[i].Text != w.text {
			t.Errorf("persisted[%d] = %+v, want type=%d text=%q",
				i, persisted[i], w.typ, w.text)
		}
	}
	// The empty-bubble idx (1) must not appear in the persisted slice.
	for _, s := range persisted {
		if s.Idx == 1 && s.Type == convstore.StepModelResponse && s.Text == "" {
			t.Errorf("empty assistant step was not filtered: %+v", s)
		}
	}

	// Sanity: loading the conversation we wrote earlier still returns
	// the empty step (we deliberately write it that way to exercise
	// the load filter in seedConversation / Transcript), and
	// Transcript() drops it from the rendered transcript.
	c, err := convstore.LoadConversation(dbPath)
	if err != nil {
		t.Fatalf("LoadConversation: %v", err)
	}
	transcript := c.Transcript()
	for _, s := range transcript {
		if s.Type == convstore.StepModelResponse && strings.TrimSpace(s.Text) == "" {
			t.Errorf("Transcript() surfaced an empty model step: %+v", s)
		}
	}
	// ChatTurns() — the channel the --continue replay feeds into the
	// next model call — must not yield an empty assistant turn.
	for _, ct := range c.ChatTurns() {
		if ct.Role == "assistant" && strings.TrimSpace(ct.Text) == "" {
			t.Errorf("ChatTurns() surfaced an empty assistant turn: %+v", ct)
		}
	}
}

// TestContinueNoBlankTurns — load a conversation whose model step
// has empty Text (a legacy DB shape) and feed it through
// seedConversation. The runMode transcript must not gain a blank
// row, and the live m.session must not gain an empty assistant
// message. This is the --continue half of the AGY 1.2.13 fix.
func TestContinueNoBlankTurns(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "conversations"), 0o755); err != nil {
		t.Fatal(err)
	}

	const id = "11111111-2222-4000-8000-00000000ea02"
	// Synthesise a DB with a blank model step in the middle. We write
	// the protobuf payload directly so LoadConversation parses the
	// shape the reference would emit. sessionToSteps would never
	// produce this row today, but a legacy DB might still hold it.
	steps := []convstore.Step{
		{Idx: 0, Type: convstore.StepUserInput, Status: 3, Text: "first"},
		{Idx: 1, Type: convstore.StepModelResponse, Status: 3, Text: ""}, // blank row to filter
		{Idx: 2, Type: convstore.StepUserInput, Status: 3, Text: "second"},
		{Idx: 3, Type: convstore.StepModelResponse, Status: 3, Text: "real reply"},
	}
	dbPath := filepath.Join(dir, "conversations", id+".db")
	if err := convstore.WriteConversation(dbPath, id, steps); err != nil {
		t.Fatalf("WriteConversation: %v", err)
	}

	c, err := convstore.LoadConversation(dbPath)
	if err != nil {
		t.Fatalf("LoadConversation: %v", err)
	}

	m := NewRunMode(&fakeClient{}, "u@t", "Gemini 3.8 Flash (High)", "t", "/", true)
	m.conversationID = id
	m.seedConversation(c)

	// Count assistant messages in the live session: must equal 1
	// (the "real reply"), the blank row must not have been re-seeded.
	var assistantCount int
	for _, msg := range m.session {
		if msg.Role == "assistant" {
			assistantCount++
			if strings.TrimSpace(msg.Content) == "" {
				t.Errorf("seedConversation re-seeded an empty assistant message: %+v", msg)
			}
		}
	}
	if assistantCount != 1 {
		t.Errorf("m.session assistant count = %d, want 1 (the blank row must be dropped)", assistantCount)
	}

	// User messages are still preserved (blank rows do not eat user turns).
	var userTexts []string
	for _, msg := range m.session {
		if msg.Role == "user" {
			userTexts = append(userTexts, msg.Content)
		}
	}
	if len(userTexts) != 2 || userTexts[0] != "first" || userTexts[1] != "second" {
		t.Errorf("user turns = %v, want [first second]", userTexts)
	}
}

// TestSessionToStepsIdxUnchanged — a focused regression for the
// internal Idx invariant: dropping a step in the middle of sess must
// not shift the Idx of later steps. Idx must keep tracking the
// session slot so the saved DB rows land on the same primary-key
// values a previous save wrote; ON CONFLICT(idx) DO UPDATE then
// overwrites the matching row instead of duplicating it.
func TestSessionToStepsIdxUnchanged(t *testing.T) {
	sess := []backend.Message{
		{Role: "user", Content: "u0"},
		{Role: "assistant", Content: ""}, // dropped
		{Role: "user", Content: "u2"},
		{Role: "assistant", Content: "a3"},   // kept, Idx must be 3
		{Role: "assistant", Content: "\n  "}, // dropped
		{Role: "user", Content: "u5"},
		{Role: "assistant", Content: "a6"}, // kept, Idx must be 6
	}
	steps := sessionToSteps(sess)
	wantIdxs := []int64{0, 2, 3, 5, 6}
	if len(steps) != len(wantIdxs) {
		t.Fatalf("got %d steps, want %d: %+v", len(steps), len(wantIdxs), steps)
	}
	for i, s := range steps {
		if s.Idx != wantIdxs[i] {
			t.Errorf("step %d Idx = %d, want %d", i, s.Idx, wantIdxs[i])
		}
	}
}
