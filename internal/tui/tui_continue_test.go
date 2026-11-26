package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/actor"
	"github.com/justwasm/pigasus/internal/app/session/convstore"
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

// TestContinueInterruptedTurn_FailedTurn tests that when a turn terminates with
// an error (network drop, timeout, etc.), /continue treats it as an interrupted turn
// and resumes work instead of falsely claiming "Last turn completed normally".
func TestContinueInterruptedTurn_FailedTurn(t *testing.T) {
	m := newRootModel(nil, "", "Gemini 3.8 Flash (High)", "", t.TempDir(), true)
	m.session.appendMessage(backend.Message{Role: "user", Content: "Edit internal/voice/aicode_live.go"})
	m.session.appendMessage(backend.Message{Role: "assistant", Content: ""})
	m.conversation.turnInterrupted = false
	m.recordTurnOutcome(tea.ProgramStateError)

	if !m.turnFailed() {
		t.Fatal("turnFailed() should be true after recordTurnOutcome(ProgramStateError)")
	}
	if !m.hasInterruptedTurn() {
		t.Fatal("hasInterruptedTurn() should return true for a failed turn")
	}

	handled, cmd := m.handleSlashCommand("/continue")
	if !handled || cmd == nil {
		t.Fatal("/continue should handle and start streaming for a failed turn")
	}
	if !m.conversation.streaming {
		t.Fatal("conversation should transition to streaming")
	}
	if m.turnFailed() {
		t.Fatal("turnFailed() should be cleared after continueInterruptedTurn")
	}

	joined := strings.Join(m.conversation.response, "\n")
	if !strings.Contains(joined, "Continuing interrupted turn") {
		t.Fatalf("expected response to contain continuation banner, got: %q", joined)
	}
	if strings.Contains(joined, "Last turn completed normally") {
		t.Fatalf("response should NOT say last turn completed normally, got: %q", joined)
	}

	// Verify that recovery steer message was appended
	msgs := m.SessionMessages()
	if len(msgs) < 3 {
		t.Fatalf("expected at least 3 messages (user, assistant, recovery steer), got %d", len(msgs))
	}
	lastMsg := msgs[len(msgs)-1]
	if lastMsg.Role != "user" || !strings.Contains(lastMsg.Content, actor.StreamInterruptedMessage) {
		t.Fatalf("expected recovery steer %q, got: %+v", actor.StreamInterruptedMessage, lastMsg)
	}
}

// TestContinueInterruptedTurn_LeakedAssistantError tests that if an older session
// erroneously persisted a transport/auth error as an assistant message, /continue
// recognizes the leaked error as an interrupted turn, strips the error, and resumes
// the uncompleted user prompt.
func TestContinueInterruptedTurn_LeakedAssistantError(t *testing.T) {
	m := newRootModel(nil, "", "Gemini 3.8 Flash (High)", "", t.TempDir(), true)
	const userPrompt = "那么你先去反编译/提取/更新产物，然后再按照优先级推进"
	const leakedError = `gemini-api-key: connection to https://daily-cloudcode-pa.googleapis.com dropped: refresh failed: auth: auth failure (refresh): Post "https://oauth2.googleapis.com/token": context deadline exceeded (returning stale token)`

	m.session.appendMessage(backend.Message{Role: "user", Content: userPrompt})
	m.session.appendMessage(backend.Message{Role: "assistant", Content: leakedError})
	m.conversation.turnInterrupted = false

	if !m.hasInterruptedTurn() {
		t.Fatal("hasInterruptedTurn() should return true for a session ending in a leaked assistant error")
	}

	handled, cmd := m.handleSlashCommand("/continue")
	if !handled || cmd == nil {
		t.Fatal("/continue should handle and start streaming for leaked assistant error")
	}
	if !m.conversation.streaming {
		t.Fatal("conversation should transition to streaming")
	}

	// Verify that the leaked assistant error was stripped so messages end at the clean user prompt
	msgs := m.SessionMessages()
	if len(msgs) != 1 {
		t.Fatalf("expected exactly 1 message (user prompt), got %d: %+v", len(msgs), msgs)
	}
	if msgs[0].Role != "user" || msgs[0].Content != userPrompt {
		t.Fatalf("expected clean user prompt %q, got: %+v", userPrompt, msgs[0])
	}
}

// TestContinueInterruptedTurn_SeededWithLeakedError tests that when a conversation
// loaded from disk contains an old StepModelResponse with a leaked transport error,
// seedConversation does not register it as a successful assistant message, allowing
// /continue to resume the original user request.
func TestContinueInterruptedTurn_SeededWithLeakedError(t *testing.T) {
	m := newRootModel(nil, "", "Gemini 3.8 Flash (High)", "", t.TempDir(), true)
	const userPrompt = "那么你先去反编译/提取/更新产物，然后再按照优先级推进"
	const leakedError = `gemini-api-key: connection to https://daily-cloudcode-pa.googleapis.com dropped: refresh failed: auth: auth failure (refresh): Post "https://oauth2.googleapis.com/token": context deadline exceeded (returning stale token)`

	c := &convstore.Conversation{
		ID: "test-leaked-err-conv",
		Steps: []convstore.Step{
			{Idx: 0, Type: convstore.StepUserInput, Status: 3, Text: userPrompt},
			{Idx: 1, Type: convstore.StepModelResponse, Status: 3, Text: leakedError},
		},
	}
	m.seedConversation(c)

	// Since step 1 was a leaked error, seedConversation must NOT add it to session messages
	msgs := m.SessionMessages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message in session (user only), got %d: %+v", len(msgs), msgs)
	}
	if msgs[0].Role != "user" || msgs[0].Content != userPrompt {
		t.Fatalf("expected user prompt %q, got: %+v", userPrompt, msgs[0])
	}

	if !m.hasInterruptedTurn() {
		t.Fatal("hasInterruptedTurn() should return true for uncompleted user prompt")
	}

	handled, cmd := m.handleSlashCommand("/continue")
	if !handled || cmd == nil {
		t.Fatal("/continue should handle and start streaming")
	}
	if !m.conversation.streaming {
		t.Fatal("conversation should transition to streaming")
	}
}
