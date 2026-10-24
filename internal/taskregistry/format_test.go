package taskregistry

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestStateStyle(t *testing.T) {
	label, _ := StateStyle(StateRunning)
	if !strings.HasPrefix(label, "running") {
		t.Errorf("expected running prefix, got %q", label)
	}

	label, _ = StateStyle(StateDone)
	if !strings.HasPrefix(label, "done") {
		t.Errorf("expected done prefix, got %q", label)
	}
}

func TestElapsed(t *testing.T) {
	if got := Elapsed(nil); got != "—" {
		t.Errorf("nil task elapsed = %q, want '—'", got)
	}

	task := &Task{
		StartedAt: time.Now().Add(-15 * time.Second),
	}
	if got := Elapsed(task); !strings.HasSuffix(got, "s") {
		t.Errorf("elapsed = %q, want suffix 's'", got)
	}
}

func TestRowLine(t *testing.T) {
	task := &Task{
		ID:        "sub-12345",
		Name:      "sleep 10",
		State:     StateRunning,
		StartedAt: time.Now().Add(-2 * time.Second),
	}

	line := RowLine(task, false)
	if !strings.Contains(line, "sub-12345") || !strings.Contains(line, "sleep 10") {
		t.Errorf("RowLine missing fields: %q", line)
	}

	cursorLine := RowLine(task, true)
	if !strings.HasPrefix(cursorLine, "\033[1m") {
		t.Errorf("cursor line should start with bold: %q", cursorLine)
	}

	failedTask := &Task{
		ID:        "sub-fail",
		Name:      "broken",
		State:     StateFailed,
		Err:       errors.New("exit code 1"),
		StartedAt: time.Now().Add(-5 * time.Second),
	}
	failedLine := RowLine(failedTask, false)
	if !strings.Contains(failedLine, "Error: exit code 1") {
		t.Errorf("failed line missing error reason: %q", failedLine)
	}
}

func TestStatusSuffix(t *testing.T) {
	if got := StatusSuffix(0); got != "" {
		t.Errorf("StatusSuffix(0) = %q, want empty", got)
	}
	if got := StatusSuffix(1); got != " · 1 task" {
		t.Errorf("StatusSuffix(1) = %q, want ' · 1 task'", got)
	}
	if got := StatusSuffix(3); got != " · 3 tasks" {
		t.Errorf("StatusSuffix(3) = %q, want ' · 3 tasks'", got)
	}
}
