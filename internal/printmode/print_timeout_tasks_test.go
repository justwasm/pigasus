package printmode

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	taskregistry "github.com/justwasm/pigasus/internal/app/task"
	"github.com/justwasm/pigasus/internal/app/tools"
	"github.com/justwasm/pigasus/internal/cli"
)

func TestHandlePrintTurnTimeout_NoTasks(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	timedOut, err := handlePrintTurnTimeout(ctx, 5*time.Minute, context.DeadlineExceeded)
	if !timedOut {
		t.Fatal("expected timedOut to be true")
	}
	if err == nil {
		t.Fatal("expected non-nil error")
	}
	code, ok := cli.ExitCodeOf(err)
	if !ok || code != 3 {
		t.Fatalf("expected exit code 3, got %d (ok=%v)", code, ok)
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "print timeout after 5m0s") {
		t.Fatalf("expected stderr warning, got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "AGY_ERROR: ") {
		t.Fatalf("expected AGY_ERROR line, got: %s", errMsg)
	}

	agyJSON := errMsg[strings.Index(errMsg, "AGY_ERROR: ")+len("AGY_ERROR: "):]
	var payload map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(agyJSON)), &payload); err != nil {
		t.Fatalf("unmarshal AGY_ERROR json: %v", err)
	}

	if payload["status"] != "DEADLINE_EXCEEDED" {
		t.Errorf("status = %v, want DEADLINE_EXCEEDED", payload["status"])
	}
	if payload["reason"] != "print_timeout" {
		t.Errorf("reason = %v, want print_timeout", payload["reason"])
	}
	if payload["retryable"] != false {
		t.Errorf("retryable = %v, want false", payload["retryable"])
	}
	if !strings.Contains(payload["short_error"].(string), "DEADLINE_EXCEEDED: print timeout after 5m0s") {
		t.Errorf("short_error = %v, want substring DEADLINE_EXCEEDED: print timeout after 5m0s", payload["short_error"])
	}
}

func TestHandlePrintTurnTimeout_WithRunningTasks(t *testing.T) {
	reg := tools.SharedTaskRegistry()
	if reg == nil {
		t.Skip("no shared task registry")
	}

	taskID := reg.Register(&taskregistry.Task{
		Name:      "test-bg-task",
		Kind:      taskregistry.KindShell,
		State:     taskregistry.StateRunning,
		StartedAt: time.Now(),
	})
	cancelled := false
	reg.RegisterHandle(taskID, &taskregistry.TaskHandle{
		Cancel: func() {
			cancelled = true
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	timedOut, err := handlePrintTurnTimeout(ctx, 2*time.Second, context.DeadlineExceeded)
	if !timedOut {
		t.Fatal("expected timedOut to be true")
	}
	if !cancelled {
		t.Fatal("expected task handle to be cancelled")
	}

	tk := reg.Get(taskID)
	if tk.State != taskregistry.StateKilled {
		t.Fatalf("expected task state to be killed, got %s", tk.State)
	}

	code, ok := cli.ExitCodeOf(err)
	if !ok || code != 3 {
		t.Fatalf("expected exit code 3, got %d (ok=%v)", code, ok)
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "test-bg-task") {
		t.Fatalf("expected AGY_ERROR to name terminated task, got: %s", errMsg)
	}
}
