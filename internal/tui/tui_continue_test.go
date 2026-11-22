package tui

import (
	"strings"
	"testing"

	"github.com/justwasm/pigasus/actor"
	"github.com/justwasm/pigasus/internal/core/backend"
)

// TestContinueInterruptedTurn_InMemoryInterruption tests that /continue resumes
// directly from an in-memory interrupted turn without adding a new user turn.
func TestContinueInterruptedTurn_InMemoryInterruption(t *testing.T) {
	m := newRootModel(nil, "", "Gemini 3.8 Flash (High)", "", t.TempDir(), true)
	m.session.appendMessage(backend.Message{Role: "user", Content: "solve the equation 2x + 4 = 10"})
	m.conversation.turnInterrupted = true

	initialMsgCount := m.session.messageCount()

	handled, cmd := m.handleSlashCommand("/continue")
	if !handled {
		t.Fatal("/continue should be handled")
	}
	if cmd == nil {
		t.Fatal("expected non-nil tea.Cmd to drive the continued turn")
	}
	if m.conversation.turnInterrupted {
		t.Fatal("turnInterrupted should be cleared to false")
	}
	if !m.conversation.streaming {
		t.Fatal("streaming should be true while continuing")
	}

	joined := strings.Join(m.conversation.response, "\n")
	if !strings.Contains(joined, "Continuing interrupted turn") {
		t.Fatalf("expected response to contain continuation banner, got: %q", joined)
	}

	// Invariant: no new user message was created!
	if m.session.messageCount() != initialMsgCount {
		t.Fatalf("expected message count to remain %d, got %d", initialMsgCount, m.session.messageCount())
	}
}

// TestContinueInterruptedTurn_WithPartialAssistantReply tests that if an assistant
// reply was partially output before interruption, /continue injects the reference recovery steer.
func TestContinueInterruptedTurn_WithPartialAssistantReply(t *testing.T) {
	m := newRootModel(nil, "", "Gemini 3.8 Flash (High)", "", t.TempDir(), true)
	m.session.appendMessage(backend.Message{Role: "user", Content: "Write a poem"})
	m.session.appendMessage(backend.Message{Role: "assistant", Content: "Roses are red,"})
	m.conversation.turnInterrupted = true

	handled, cmd := m.handleSlashCommand("/continue")
	if !handled || cmd == nil {
		t.Fatal("expected /continue to handle and produce stream command")
	}

	msgs := m.SessionMessages()
	if len(msgs) < 3 {
		t.Fatalf("expected at least 3 messages (user, assistant, recovery steer), got %d", len(msgs))
	}
	lastMsg := msgs[len(msgs)-1]
	if lastMsg.Role != "user" || !strings.Contains(lastMsg.Content, actor.StreamInterruptedMessage) {
		t.Fatalf("expected recovery steer %q, got: %+v", actor.StreamInterruptedMessage, lastMsg)
	}
}

// TestContinueInterruptedTurn_NormalTurnCompleted tests that if the last turn completed
// normally, /continue does not execute and informs the user.
func TestContinueInterruptedTurn_NormalTurnCompleted(t *testing.T) {
	m := newRootModel(nil, "", "Gemini 3.8 Flash (High)", "", t.TempDir(), true)
	m.session.appendMessage(backend.Message{Role: "user", Content: "What is 2+2?"})
	m.session.appendMessage(backend.Message{Role: "assistant", Content: "2+2 is 4."})
	m.conversation.turnInterrupted = false

	handled, cmd := m.handleSlashCommand("/continue")
	if !handled {
		t.Fatal("/continue should be handled")
	}
	if cmd != nil {
		t.Fatal("expected nil tea.Cmd when nothing to continue")
	}
	if m.conversation.streaming {
		t.Fatal("streaming should remain false")
	}

	joined := strings.Join(m.conversation.response, "\n")
	if !strings.Contains(joined, "Last turn completed normally") {
		t.Fatalf("expected notice that last turn completed normally, got: %q", joined)
	}
}

// TestContinueInterruptedTurn_UncompletedUserPrompt tests that if a conversation
// ended in an unanswered user prompt, /continue detects it and resumes.
func TestContinueInterruptedTurn_UncompletedUserPrompt(t *testing.T) {
	m := newRootModel(nil, "", "Gemini 3.8 Flash (High)", "", t.TempDir(), true)
	m.session.appendMessage(backend.Message{Role: "user", Content: "Run tests"})
	m.conversation.turnInterrupted = false // even if memory flag was lost

	if !m.hasInterruptedTurn() {
		t.Fatal("hasInterruptedTurn() should return true for uncompleted user prompt")
	}

	handled, cmd := m.handleSlashCommand("/continue")
	if !handled || cmd == nil {
		t.Fatal("/continue should handle and start streaming for uncompleted user prompt")
	}
	if !m.conversation.streaming {
		t.Fatal("conversation should transition to streaming")
	}
}
