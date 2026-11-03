package convstore

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// Tests for the destructive helpers added by cycle
// rc-conversation-deletions. The read-only constraint on
// sqlite.go is preserved: this suite uses SaveConversation
// (the cycle-167 writer) and operates on its output, not on
// the read-only connection sqlite.go opens. A deleted .db
// is gone — the tests don't try to read it after they
// remove it.

func TestDeleteConversation_RemovesFile(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", tempHome)

	convID := "deadbeef-0000-4000-8000-000000000001"
	if err := SaveConversation(convID, "/workspace/foo", []Step{
		{Idx: 0, Type: StepUserInput, Status: 1, Text: "hello"},
		{Idx: 1, Type: StepModelResponse, Status: 1, Text: "world"},
	}); err != nil {
		t.Fatalf("SaveConversation: %v", err)
	}
	dbPath := ConversationPath(convID)
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("db missing after save: %v", err)
	}

	if err := DeleteConversation(convID); err != nil {
		t.Fatalf("DeleteConversation: %v", err)
	}

	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		t.Errorf("db still present: err = %v", err)
	}
}

func TestDeleteConversation_NotFound(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", tempHome)

	err := DeleteConversation("ghost-conv")
	if !errors.Is(err, ErrConversationNotFound) {
		t.Errorf("err = %v, want ErrConversationNotFound", err)
	}
}

func TestDeleteConversation_EmptyID(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", tempHome)

	err := DeleteConversation("")
	if !errors.Is(err, ErrConversationNotFound) {
		t.Errorf("err = %v, want ErrConversationNotFound", err)
	}
}

func TestDeleteMessage_DeletesSingleRow(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", tempHome)

	convID := "feedface-0000-4000-8000-000000000010"
	steps := []Step{
		{Idx: 0, Type: StepUserInput, Status: 1, Text: "first"},
		{Idx: 1, Type: StepModelResponse, Status: 1, Text: "second"},
		{Idx: 2, Type: StepUserInput, Status: 1, Text: "third"},
		{Idx: 3, Type: StepModelResponse, Status: 1, Text: "fourth"},
		{Idx: 4, Type: StepUserInput, Status: 1, Text: "fifth"},
	}
	if err := SaveConversation(convID, "/workspace/msg", steps); err != nil {
		t.Fatalf("SaveConversation: %v", err)
	}

	if err := DeleteMessage(convID, "2"); err != nil {
		t.Fatalf("DeleteMessage: %v", err)
	}

	// Reload and verify.
	c, err := LoadConversation(ConversationPath(convID))
	if err != nil {
		t.Fatalf("LoadConversation: %v", err)
	}
	if len(c.Steps) != 4 {
		t.Errorf("len(steps) = %d, want 4", len(c.Steps))
	}
	for _, s := range c.Steps {
		if s.Idx == 2 {
			t.Errorf("idx 2 still present")
		}
	}
}

func TestDeleteMessage_NotFound(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", tempHome)

	convID := "feedface-0000-4000-8000-000000000020"
	steps := []Step{
		{Idx: 0, Type: StepUserInput, Status: 1, Text: "first"},
	}
	if err := SaveConversation(convID, "/workspace/x", steps); err != nil {
		t.Fatalf("SaveConversation: %v", err)
	}

	err := DeleteMessage(convID, "999")
	if !errors.Is(err, ErrMessageNotFound) {
		t.Errorf("err = %v, want ErrMessageNotFound", err)
	}
}

func TestDeleteMessage_EmptyMessageID(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", tempHome)

	convID := "feedface-0000-4000-8000-000000000030"
	steps := []Step{
		{Idx: 0, Type: StepUserInput, Status: 1, Text: "first"},
	}
	if err := SaveConversation(convID, "/workspace/y", steps); err != nil {
		t.Fatalf("SaveConversation: %v", err)
	}

	err := DeleteMessage(convID, "")
	if !errors.Is(err, ErrMessageNotFound) {
		t.Errorf("err = %v, want ErrMessageNotFound", err)
	}
}

func TestDeleteMessage_NonNumericMessageID(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", tempHome)

	convID := "feedface-0000-4000-8000-000000000040"
	steps := []Step{
		{Idx: 0, Type: StepUserInput, Status: 1, Text: "first"},
	}
	if err := SaveConversation(convID, "/workspace/z", steps); err != nil {
		t.Fatalf("SaveConversation: %v", err)
	}

	err := DeleteMessage(convID, "abc-not-a-number")
	if !errors.Is(err, ErrMessageNotFound) {
		t.Errorf("err = %v, want ErrMessageNotFound", err)
	}
}

func TestDeleteMessage_LastRemainingFallsBackToEmpty(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", tempHome)

	convID := "feedface-0000-4000-8000-000000000050"
	steps := []Step{
		{Idx: 0, Type: StepUserInput, Status: 1, Text: "first"},
		{Idx: 1, Type: StepModelResponse, Status: 1, Text: "second"},
	}
	if err := SaveConversation(convID, "/workspace/last", steps); err != nil {
		t.Fatalf("SaveConversation: %v", err)
	}

	if err := DeleteMessage(convID, "0"); err != nil {
		t.Fatalf("DeleteMessage step 0: %v", err)
	}
	if err := DeleteMessage(convID, "1"); err != nil {
		t.Fatalf("DeleteMessage step 1: %v", err)
	}

	n, err := CountMessages(convID)
	if err != nil {
		t.Fatalf("CountMessages: %v", err)
	}
	if n != 0 {
		t.Errorf("CountMessages = %d, want 0", n)
	}
}

func TestMessageByID(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", tempHome)

	convID := "feedface-0000-4000-8000-000000000060"
	steps := []Step{
		{Idx: 0, Type: StepUserInput, Status: 1, Text: "first"},
		{Idx: 7, Type: StepUserInput, Status: 1, Text: "weird index"},
	}
	if err := SaveConversation(convID, "/workspace/probe", steps); err != nil {
		t.Fatalf("SaveConversation: %v", err)
	}

	ok, err := MessageByID(convID, "7")
	if err != nil {
		t.Fatalf("MessageByID: %v", err)
	}
	if !ok {
		t.Errorf("MessageByID(7) = false, want true")
	}
	ok, err = MessageByID(convID, "42")
	if err != nil {
		t.Fatalf("MessageByID: %v", err)
	}
	if ok {
		t.Errorf("MessageByID(42) = true, want false")
	}
}

func TestCountMessages(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", tempHome)

	convID := "feedface-0000-4000-8000-000000000070"
	steps := []Step{
		{Idx: 0, Type: StepUserInput, Status: 1, Text: "first"},
		{Idx: 1, Type: StepModelResponse, Status: 1, Text: "second"},
		{Idx: 2, Type: StepUserInput, Status: 1, Text: "third"},
	}
	if err := SaveConversation(convID, "/workspace/count", steps); err != nil {
		t.Fatalf("SaveConversation: %v", err)
	}

	n, err := CountMessages(convID)
	if err != nil {
		t.Fatalf("CountMessages: %v", err)
	}
	if n != 3 {
		t.Errorf("CountMessages = %d, want 3", n)
	}
}

func TestConversationBoundToProject_Empty(t *testing.T) {
	count, err := ConversationBoundToProject("")
	if err != nil {
		t.Fatalf("ConversationBoundToProject: %v", err)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0", count)
	}
}

func TestConversationBoundToProject_NoConversations(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", tempHome)

	count, err := ConversationBoundToProject("/tmp/empty")
	if err != nil {
		t.Fatalf("ConversationBoundToProject: %v", err)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0", count)
	}
}

func TestDeleteConversation_RemovesCacheEntry(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", tempHome)

	convID := "feedface-0000-4000-8000-000000000080"
	steps := []Step{
		{Idx: 0, Type: StepUserInput, Status: 1, Text: "hi"},
		{Idx: 1, Type: StepModelResponse, Status: 1, Text: "hello"},
	}
	if err := SaveConversation(convID, "/workspace/cache-test", steps); err != nil {
		t.Fatalf("SaveConversation: %v", err)
	}

	cachePath := filepath.Join(tempHome, "cache", "conversation_metadata.json")
	if _, err := os.Stat(cachePath); err != nil {
		t.Fatalf("cache missing after save: %v", err)
	}

	if err := DeleteConversation(convID); err != nil {
		t.Fatalf("DeleteConversation: %v", err)
	}

	// Read cache back: the entry for convID should be gone.
	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("ReadFile cache: %v", err)
	}
	if containsBytes(data, []byte(convID)) {
		t.Errorf("cache still references %s: %s", convID, string(data))
	}
}

// containsBytes is a tiny stdlib-only helper for the
// delete-cache-entry check above.
func containsBytes(haystack, needle []byte) bool {
	if len(needle) == 0 {
		return true
	}
	if len(needle) > len(haystack) {
		return false
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		match := true
		for j := range needle {
			if haystack[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
