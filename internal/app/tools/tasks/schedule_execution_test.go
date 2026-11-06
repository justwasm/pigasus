package tasks

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/app/tools/shell"
)

func TestScheduleTool_RealTimerFiresNotification(t *testing.T) {
	home := t.TempDir()
	convID := "conv-timer-exec"

	notifications := make(chan shell.TaskNotification, 1)
	prev := shell.SetTaskNotifier(func(n shell.TaskNotification) {
		notifications <- n
	})
	defer shell.SetTaskNotifier(prev)

	tool := &ScheduleTool{ConversationID: convID, Home: home}
	res, err := tool.Run(context.Background(), json.RawMessage(`{"Prompt":"Time to review PR","DurationSeconds":1}`))
	if err != nil {
		t.Fatalf("tool.Run failed: %v", err)
	}
	text, ok := res.(string)
	if !ok || !strings.Contains(text, "Tool is running as a background task") {
		t.Fatalf("unexpected launch result: %q", text)
	}

	select {
	case n := <-notifications:
		if !strings.HasPrefix(n.TaskID, convID+"/task-") {
			t.Errorf("unexpected task ID: %q", n.TaskID)
		}
		if n.Body != "Time to review PR" {
			t.Errorf("expected body 'Time to review PR', got %q", n.Body)
		}
		if n.Priority != shell.TaskPriorityHigh {
			t.Errorf("expected high priority, got %q", n.Priority)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for scheduled timer notification to fire")
	}

	// Verify log file recorded the firing
	logPath := filepath.Join(home, "brain", convID, ".system_generated", "tasks", "task-1.log")
	b, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log file error: %v", err)
	}
	if !strings.Contains(string(b), "Timer fired at") {
		t.Errorf("expected log file to contain 'Timer fired at', got:\n%s", b)
	}
}
