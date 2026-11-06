package agentserver

import (
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/app/task"
	"github.com/justwasm/pigasus/internal/app/tools"
)

func TestFormatTasksSummary(t *testing.T) {
	reg := tools.SharedTaskRegistry()
	if reg == nil {
		t.Fatal("expected SharedTaskRegistry to be non-nil")
	}

	// Case 1: Empty or with existing
	summaryEmpty := formatTasksSummary()
	if !strings.Contains(summaryEmpty, "Background Tasks") && !strings.Contains(summaryEmpty, "No background tasks") {
		t.Errorf("unexpected summary format: %s", summaryEmpty)
	}

	// Case 2: Register a mock task
	mockTask := &task.Task{
		ID:        "test-task-1",
		Name:      "test-proxy",
		Command:   "pigasus proxy serve -addr 127.0.0.1:19998",
		State:     task.StateRunning,
		StartedAt: time.Now(),
	}
	reg.Register(mockTask)
	defer func() {
		// Clean up
		reg.Update("test-task-1", func(t *task.Task) {
			t.State = task.StateKilled
		})
	}()

	summaryWithTask := formatTasksSummary()
	if !strings.Contains(summaryWithTask, "test-task-1") {
		t.Errorf("expected summary to contain 'test-task-1', got: %s", summaryWithTask)
	}
	if !strings.Contains(summaryWithTask, "pigasus proxy serve") {
		t.Errorf("expected summary to contain command, got: %s", summaryWithTask)
	}
	if !strings.Contains(summaryWithTask, "running") {
		t.Errorf("expected summary to contain running status, got: %s", summaryWithTask)
	}
}
