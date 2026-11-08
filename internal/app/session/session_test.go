package session

import (
	"testing"
)

func TestSession_Lifecycle(t *testing.T) {
	tmpDir := t.TempDir()
	sess := New(Config{
		ID:     "conv-test-123",
		Model:  "gemini-3.8-flash-high",
		Effort: "high",
		CWD:    tmpDir,
		Home:   tmpDir,
	})

	if sess.ID() != "conv-test-123" {
		t.Errorf("ID() = %q, want conv-test-123", sess.ID())
	}
	if sess.Model() != "gemini-3.8-flash-high" {
		t.Errorf("Model() = %q, want gemini-3.8-flash-high", sess.Model())
	}
	if sess.Effort() != "high" {
		t.Errorf("Effort() = %q, want high", sess.Effort())
	}
	if sess.CWD() != tmpDir {
		t.Errorf("CWD() = %q, want %q", sess.CWD(), tmpDir)
	}
	if sess.Epoch() != 0 {
		t.Errorf("Epoch() = %d, want 0", sess.Epoch())
	}

	// Append messages
	sess.AppendUser("Hello")
	sess.AppendAssistant("Hi there!")

	msgs := sess.Messages()
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Content != "Hello" || msgs[1].Content != "Hi there!" {
		t.Errorf("unexpected messages: %+v", msgs)
	}

	// Bump epoch
	ep := sess.BumpEpoch()
	if ep != 1 || sess.Epoch() != 1 {
		t.Errorf("epoch after bump = %d, want 1", ep)
	}

	// Clear session
	sess.Clear()
	if len(sess.Messages()) != 0 {
		t.Errorf("messages after clear = %d, want 0", len(sess.Messages()))
	}
	if sess.Epoch() != 2 {
		t.Errorf("epoch after clear = %d, want 2", sess.Epoch())
	}

	// Reset with new ID
	sess.Reset("conv-test-456")
	if sess.ID() != "conv-test-456" {
		t.Errorf("ID() after reset = %q, want conv-test-456", sess.ID())
	}
	if sess.Epoch() != 3 {
		t.Errorf("epoch after reset = %d, want 3", sess.Epoch())
	}
}

func TestSession_Persistence(t *testing.T) {
	tmpDir := t.TempDir()
	sess := New(Config{
		ID:   "conv-persist-1",
		CWD:  tmpDir,
		Home: tmpDir,
	})

	sess.AppendUser("Test persist")
	sess.AppendAssistant("A response")

	err := sess.Save("Auto Title")
	if err != nil {
		t.Fatalf("Save() failed: %v", err)
	}

	// Verify store can load snapshot
	snap, err := sess.Store().Load("conv-persist-1")
	if err != nil {
		t.Fatalf("Store.Load() failed: %v", err)
	}
	if snap == nil || snap.Conversation == nil {
		t.Fatal("expected non-nil snapshot and conversation")
	}
}

func TestSession_DefaultModel(t *testing.T) {
	sess := New(Config{})
	if sess.Model() != "gemini-3.8-flash-high" {
		t.Errorf("Model() = %q, want gemini-3.8-flash-high", sess.Model())
	}
}

func TestSession_SubagentStores(t *testing.T) {
	sess := New(Config{ID: "conv-parent-sub"})
	if sess.SubagentDefs() == nil {
		t.Fatal("expected non-nil SubagentDefs")
	}
	if sess.SubagentRegistry() == nil {
		t.Fatal("expected non-nil SubagentRegistry")
	}

	// Reset resets the subagent stores
	oldDefs := sess.SubagentDefs()
	sess.Reset("conv-parent-sub-2")
	if sess.SubagentDefs() == oldDefs {
		t.Fatal("Reset should allocate fresh SubagentDefs")
	}
}

// TestSession_EpochBumpsOnEpochChangingWrites pins the single counter the
// turn engines read: Replace, Clear, SetID and BumpEpoch each move it.
func TestSession_EpochBumpsOnEpochChangingWrites(t *testing.T) {
	s := New(Config{ID: "conv-1", Model: "m"})
	start := s.Epoch()
	s.BumpEpoch()
	if s.Epoch() != start+1 {
		t.Fatalf("BumpEpoch = %d, want %d", s.Epoch(), start+1)
	}
	start = s.Epoch()
	s.Replace(nil)
	if s.Epoch() <= start {
		t.Fatalf("Replace did not bump the epoch")
	}
	start = s.Epoch()
	s.Clear()
	if s.Epoch() <= start {
		t.Fatalf("Clear did not bump the epoch")
	}
	start = s.Epoch()
	s.SetID("conv-2")
	if s.Epoch() <= start {
		t.Fatalf("SetID did not bump the epoch")
	}
}
