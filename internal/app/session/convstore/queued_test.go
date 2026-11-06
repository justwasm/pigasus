package convstore_test

import (
	"testing"

	"github.com/justwasm/pigasus/internal/app/session/convstore"
)

func TestSaveAndLoadQueuedPrompts(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", tempDir)

	convID := "test_conv_123"

	// Initially empty
	loaded := convstore.LoadQueuedPrompts(convID)
	if len(loaded) != 0 {
		t.Errorf("expected 0 queued prompts initially, got %v", loaded)
	}

	// Save prompts
	prompts := []string{"first pending task", "second pending task"}
	if err := convstore.SaveQueuedPrompts(convID, prompts); err != nil {
		t.Fatalf("SaveQueuedPrompts failed: %v", err)
	}

	// Load prompts
	loaded = convstore.LoadQueuedPrompts(convID)
	if len(loaded) != 2 {
		t.Fatalf("expected 2 loaded prompts, got %d", len(loaded))
	}
	if loaded[0] != "first pending task" || loaded[1] != "second pending task" {
		t.Errorf("unexpected loaded prompts: %v", loaded)
	}

	// Save empty slice removes the file
	if err := convstore.SaveQueuedPrompts(convID, nil); err != nil {
		t.Fatalf("SaveQueuedPrompts(nil) failed: %v", err)
	}

	loaded = convstore.LoadQueuedPrompts(convID)
	if len(loaded) != 0 {
		t.Errorf("expected 0 queued prompts after deletion, got %v", loaded)
	}
}
