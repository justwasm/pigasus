// slash_helpers_test.go — tests for the per-slash-command helpers in
// slash_helpers.go. They are pure functions of the session contents
// (and any flags), so a slice is enough.
package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/backend"
)

// /fork on an empty session returns the REF text
// (`Error: no active conversation to fork`), not the legacy clone text
// (`Nothing to fork (session is empty).`).
//
// Evidence: /tmp/agycmp/panels/ppref.fork.raw.txt
//   > /fork
//     ⎿  Error: no active conversation to fork
func TestForkConversationEmptySession(t *testing.T) {
	id, msg := forkConversation(nil, "")
	if id != "" {
		t.Errorf("forkConversation empty session: id=%q, want \"\"", id)
	}
	if msg != "Error: no active conversation to fork" {
		t.Errorf("forkConversation empty session: msg=%q, want REF text", msg)
	}
	id, msg = forkConversation([]backend.Message{}, "")
	if id != "" {
		t.Errorf("forkConversation empty slice: id=%q, want \"\"", id)
	}
	if msg != "Error: no active conversation to fork" {
		t.Errorf("forkConversation empty slice: msg=%q, want REF text", msg)
	}
}

// /fork on an active session returns the reference's two-line wording:
//   Forked conversation.
//   To go back, use: /resume <parentID>
// Evidence: agy 1.4.4 google3/third_party/jetski/cli/commands/commands.(*forkCommand).Execute.func1
func TestForkConversationActiveSession(t *testing.T) {
	sess := []backend.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "world"},
	}
	parentID := "11111111-2222-3333-4444-555555555555"
	id, msg := forkConversation(sess, parentID)
	if id == "" {
		t.Fatalf("forkConversation active session: expected non-empty ID")
	}
	want := fmt.Sprintf("Forked conversation.\nTo go back, use: /resume %s", parentID)
	if msg != want {
		t.Errorf("forkConversation active session with parent: got %q, want %q", msg, want)
	}

	id2, msg2 := forkConversation(sess, "")
	if id2 == "" {
		t.Fatalf("forkConversation active session (no parent): expected non-empty ID")
	}
	want2 := "Forked conversation."
	if msg2 != want2 {
		t.Errorf("forkConversation active session without parent: got %q, want %q", msg2, want2)
	}
}

func TestForkSlashCommandResponse(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", t.TempDir(), true)
	m.conversationID = "parent-conv-1234"
	m.session = []backend.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "world"},
	}
	_, _ = m.handleSlashCommand("/fork")
	if m.conversationID == "parent-conv-1234" || m.conversationID == "" {
		t.Fatalf("expected new conversation ID, got %q", m.conversationID)
	}
	joined := strings.Join(m.response, "\n")
	if !strings.Contains(joined, "> /fork") {
		t.Errorf("expected > /fork in response: %v", m.response)
	}
	if !strings.Contains(joined, "Forked conversation.") {
		t.Errorf("expected Forked conversation. in response: %v", m.response)
	}
	if !strings.Contains(joined, "To go back, use: /resume parent-conv-1234") {
		t.Errorf("expected To go back, use: /resume parent-conv-1234 in response: %v", m.response)
	}
}

