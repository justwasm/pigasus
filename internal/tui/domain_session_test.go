package tui

import (
	"context"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/core/backend"
)

func TestRootModel_DomainSessionSync(t *testing.T) {
	m := &RootModel{
		session: sessionState{
			conversationID: "conv-101",
			model:          "gemini-3.8-flash-high",
			cwd:            "/workspace",
		},
	}

	dom := m.DomainSession()
	if dom == nil {
		t.Fatal("expected non-nil domain session")
	}
	if dom.ID() != "conv-101" {
		t.Errorf("dom.ID() = %q, want conv-101", dom.ID())
	}
	if dom.Model() != "gemini-3.8-flash-high" {
		t.Errorf("dom.Model() = %q, want gemini-3.8-flash-high", dom.Model())
	}

	// Mutate through delegators
	m.SetCurrentModel("custom-model-2")
	if m.CurrentModel() != "custom-model-2" {
		t.Errorf("m.CurrentModel() = %q, want custom-model-2", m.CurrentModel())
	}
	if dom.Model() != "custom-model-2" {
		t.Errorf("dom.Model() = %q, want custom-model-2", dom.Model())
	}

	m.SetCurrentConversationID("conv-202")
	if m.CurrentConversationID() != "conv-202" {
		t.Errorf("m.CurrentConversationID() = %q, want conv-202", m.CurrentConversationID())
	}
	if dom.ID() != "conv-202" {
		t.Errorf("dom.ID() = %q, want conv-202", dom.ID())
	}
}

func TestRootModel_CancelActiveTurnBridge(t *testing.T) {
	eng := &blockingMockEngine{}
	bridge := NewEngineBridge(BridgeOptions{
		Engine: eng,
	})

	m := &RootModel{
		turn: TurnService{
			activeBridge: bridge,
		},
		conversation: ConversationModel{
			streaming: true,
		},
	}

	done := make(chan struct{})
	go func() {
		bridge.RunTurn(context.Background(), "test-model", []backend.Message{
			{Role: "user", Content: "blocking turn"},
		})
		close(done)
	}()

	// Wait for bridge to be running
	for i := 0; i < 20; i++ {
		time.Sleep(10 * time.Millisecond)
		if bridge.IsRunning() {
			break
		}
	}
	if !bridge.IsRunning() {
		t.Fatal("bridge failed to enter running state")
	}

	// Cancel through RootModel.cancelActiveTurn
	if !m.cancelActiveTurn() {
		t.Fatal("cancelActiveTurn returned false")
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunTurn did not exit after cancelActiveTurn")
	}

	if !m.conversation.turnInterrupted {
		t.Fatal("turnInterrupted should be true after cancel")
	}
	// The cancel stops the turn's context; the presentation flag stays
	// set until the turn's terminal message lands (b74adb86). Clearing it
	// here would let a second prompt race the unwinding turn.
	if !m.conversation.streaming {
		t.Fatal("streaming should stay true until the turn's terminal message")
	}
}

func TestRootModel_SessionMessagesSync(t *testing.T) {
	m := &RootModel{
		session: sessionState{
			conversationID: "conv-msg-sync",
			model:          "gemini-3.8-flash-high",
			cwd:            "/workspace",
		},
	}

	m.AppendUserMessage("Hello agent")
	m.AppendAssistantMessage("Hello user")

	msgs := m.SessionMessages()
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Content != "Hello agent" || msgs[1].Content != "Hello user" {
		t.Errorf("unexpected message contents: %+v", msgs)
	}

	// Verify domain reflects the same
	domMsgs := m.DomainSession().Messages()
	if len(domMsgs) != 2 {
		t.Fatalf("expected 2 domain messages, got %d", len(domMsgs))
	}

	// Test Reset
	m.ResetSessionMessages()
	if len(m.SessionMessages()) != 0 {
		t.Errorf("expected 0 messages after reset, got %d", len(m.SessionMessages()))
	}
	if len(m.DomainSession().Messages()) != 0 {
		t.Errorf("expected 0 domain messages after reset, got %d", len(m.DomainSession().Messages()))
	}

	// Verify subagents belong to domain
	if m.SubagentDefs() == nil {
		t.Fatal("expected non-nil SubagentDefs on RootModel")
	}
	if m.SubagentRegistry() == nil {
		t.Fatal("expected non-nil SubagentRegistry on RootModel")
	}
}

// TestRootModel_TranscriptHasASingleOwner pins the P0 invariant: the domain
// session owns the transcript, and every TUI helper reads and writes that
// one copy. The test would fail if a second slice were reintroduced beside
// it (or if a write path bypassed the domain).
func TestRootModel_TranscriptHasASingleOwner(t *testing.T) {
	m := &RootModel{}
	m.session.conversationID = "conv-single-owner"
	dom := m.DomainSession()

	m.AppendUserMessage("first")
	m.AppendAssistantMessage("answer")
	m.AppendUserMessage("second")

	got := m.SessionMessages()
	if len(got) != 3 {
		t.Fatalf("SessionMessages() = %d messages, want 3", len(got))
	}
	// The domain is the owner: changing it is immediately visible through
	// every TUI read.
	dom.AppendUser("third")
	if n := len(m.SessionMessages()); n != 4 {
		t.Fatalf("after a domain append, SessionMessages() = %d messages, want 4", n)
	}
	// And the reverse: the TUI helper writes the domain.
	m.AppendAssistantMessage("answer two")
	if n := dom.Len(); n != 5 {
		t.Fatalf("after AppendAssistantMessage, domain length = %d, want 5", n)
	}
}

// TestRootModel_ResumeSeedKeepsHistoryOnAppend guards the divergence the
// old double-write caused: a resumed transcript used to live only in the
// engine slice, so the first append after resuming produced a domain
// transcript that had lost every adopted message.
func TestRootModel_ResumeSeedKeepsHistoryOnAppend(t *testing.T) {
	m := &RootModel{}
	m.session.conversationID = "conv-seed-owner"

	seeded := []backend.Message{
		{Role: "user", Content: "old question"},
		{Role: "assistant", Content: "old answer"},
	}
	m.setSessionMessages(seeded)

	// A resume seeds the trajectory length from the same transcript.
	if n := m.session.messageCount(); n != len(seeded) {
		t.Fatalf("messageCount() = %d, want %d", n, len(seeded))
	}
	m.AppendUserMessage("new question")

	got := m.SessionMessages()
	if len(got) != 3 {
		t.Fatalf("transcript = %d messages, want 3 (seed + append)", len(got))
	}
	if got[0].Content != "old question" || got[2].Content != "new question" {
		t.Fatalf("transcript lost the seed or the append: %+v", got)
	}
	if n := m.DomainSession().Len(); n != 3 {
		t.Fatalf("domain length = %d, want 3", n)
	}
}

// TestRootModel_StreamingReplyMutatesTheOwnedTranscript checks the streaming
// path's two writes (placeholder append, then content replacement/append)
// against the domain, which is where the persisted reply now comes from.
func TestRootModel_StreamingReplyMutatesTheOwnedTranscript(t *testing.T) {
	m := &RootModel{}
	m.AppendUserMessage("prompt")

	m.session.appendMessage(backend.Message{Role: "assistant", Content: "Hel"})
	idx := m.session.messageCount() - 1
	if idx != 1 {
		t.Fatalf("live reply index = %d, want 1", idx)
	}
	if !m.session.appendMessageContent(idx, "lo") {
		t.Fatal("appendMessageContent rejected the live index")
	}
	if !m.session.setMessageContent(idx, "Hello") {
		t.Fatal("setMessageContent rejected the live index")
	}

	got := m.SessionMessages()
	if len(got) != 2 || got[1].Content != "Hello" {
		t.Fatalf("transcript = %+v, want the finalized reply", got)
	}
}

// TestRootModel_ResetClearsTheOwnedTranscript checks that a reset leaves no
// residue in either the TUI view or the domain.
func TestRootModel_ResetClearsTheOwnedTranscript(t *testing.T) {
	m := &RootModel{}
	m.AppendUserMessage("x")
	m.ResetSessionMessages()
	if n := len(m.SessionMessages()); n != 0 {
		t.Fatalf("SessionMessages() = %d after reset, want 0", n)
	}
	if n := m.DomainSession().Len(); n != 0 {
		t.Fatalf("domain length = %d after reset, want 0", n)
	}
}

// TestCommandDispatcher_BuiltOnce pins P1: the slash table is a property
// of the run, not something rebuilt (and re-registered) per command.
func TestCommandDispatcher_BuiltOnce(t *testing.T) {
	m := &RootModel{}
	first := m.commandDispatcher()
	if first == nil {
		t.Fatal("commandDispatcher() returned nil")
	}
	if second := m.commandDispatcher(); second != first {
		t.Fatal("commandDispatcher() rebuilt the table; want one table per run")
	}
	// The session commands moved out of slashHandlers must still be
	// answered by the table, and must not be forwarded to the engine.
	for _, name := range []string{"clear", "exit", "help", "copy", "feedback", "compact", "steer"} {
		if !first.Has(name) {
			t.Errorf("dispatcher lacks %q", name)
		}
		if m.isEngineSlashCommand(name) {
			t.Errorf("isEngineSlashCommand(%q) = true; the host answers it", name)
		}
	}
	// A command that never had a host handler still goes to the engine.
	if !m.isEngineSlashCommand("totally-not-a-command") {
		t.Error("an unknown command should be offered to the engine")
	}
}

// TestSessionState_EpochLivesOnTheDomain pins the move: epoch reads and
// writes go through the domain session, not an engine-side counter.
func TestSessionState_EpochLivesOnTheDomain(t *testing.T) {
	var s sessionState
	s.conversationID = "conv-1"
	s.model = "m"
	if got := s.epoch(); got != 0 {
		t.Fatalf("fresh epoch = %d", got)
	}
	s.bumpEpoch()
	if got := s.epoch(); got == 0 {
		t.Fatal("bumpEpoch did not move the domain epoch")
	}
	if s.domain == nil {
		t.Fatal("epoch did not go through the domain session")
	}
}
