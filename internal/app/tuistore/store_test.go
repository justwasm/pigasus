package tuistore

import (
	"testing"
)

func TestStoreDefaults(t *testing.T) {
	s := New()
	if s.BashRowIdx != -1 {
		t.Errorf("expected BashRowIdx -1, got %d", s.BashRowIdx)
	}
	if s.BashMode {
		t.Errorf("expected zero boolean flags")
	}

	called := false
	s.BashCancel = func() { called = true }
	s.BashCmd = "ls"
	s.ResetBash()
	if !called || s.BashCancel != nil || s.BashCmd != "" || s.BashRowIdx != -1 {
		t.Errorf("ResetBash failed")
	}

	s.CompactSpinnerFrame = 3
	s.CompactTokensBefore = 100
	s.ResetCompaction()
	if s.CompactSpinnerFrame != 0 || s.CompactTokensBefore != 0 {
		t.Errorf("ResetCompaction failed")
	}

	s.StreamQueue = []string{"a", "b"}
	s.QueuedPrompt = "c"
	s.ClearStreamQueue()
	if len(s.StreamQueue) != 0 || s.QueuedPrompt != "" {
		t.Errorf("ClearStreamQueue failed")
	}

	s.SessionPromptTokens = 500
	s.LastTurnPromptTokens = 200
	s.ResetSessionTokens()
	if s.SessionPromptTokens != 0 || s.LastTurnPromptTokens != 0 {
		t.Errorf("ResetSessionTokens failed")
	}
}
