package session

import (
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/app/session/convstore"
	"github.com/justwasm/pigasus/internal/app/tools/shell"
)

func TestSessionStore_LoadSnapshot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("JETSKI_HOME", home)
	t.Setenv("ANTIGRAVITY_HOME", home)

	convID := "test-session-snapshot-1"

	// 1. Seed a conversation step via convstore
	err := convstore.AppendConversationSteps(convID, "/tmp", []convstore.Step{
		{
			Idx:  0,
			Type: convstore.StepUserInput,
			Text: "hello from test",
		},
	})
	if err != nil {
		t.Fatalf("AppendConversationSteps: %v", err)
	}

	// 2. Seed queued prompts
	err = convstore.SaveQueuedPrompts(convID, []string{"queued prompt 1", "queued prompt 2"})
	if err != nil {
		t.Fatalf("SaveQueuedPrompts: %v", err)
	}

	// 3. Seed an active schedule
	err = shell.AddActiveSchedule(home, convID, shell.PersistedSchedule{
		TaskID:      "task-timer-1",
		Prompt:      "Probe status",
		DurationSec: 300,
		CreatedAt:   time.Now(),
		TargetTime:  time.Now().Add(5 * time.Minute),
	})
	if err != nil {
		t.Fatalf("AddActiveSchedule: %v", err)
	}

	// 4. Use unified SessionStore to load snapshot
	store := NewStore(home)
	snap, err := store.Load(convID)
	if err != nil {
		t.Fatalf("store.Load: %v", err)
	}

	if snap.ID != convID {
		t.Errorf("snap.ID = %q, want %q", snap.ID, convID)
	}
	if snap.Conversation == nil {
		t.Fatal("expected non-nil snap.Conversation")
	}
	if len(snap.QueuedPrompts) != 2 || snap.QueuedPrompts[0] != "queued prompt 1" {
		t.Errorf("unexpected QueuedPrompts: %v", snap.QueuedPrompts)
	}
	if len(snap.ActiveSchedules) != 1 || snap.ActiveSchedules[0].TaskID != "task-timer-1" {
		t.Errorf("unexpected ActiveSchedules: %v", snap.ActiveSchedules)
	}

	// 5. Test RemoveSchedule through store facade
	err = store.RemoveSchedule(convID, "task-timer-1")
	if err != nil {
		t.Fatalf("store.RemoveSchedule: %v", err)
	}
	remaining := store.LoadSchedules(convID)
	if len(remaining) != 0 {
		t.Errorf("expected 0 remaining schedules, got %d", len(remaining))
	}
}
