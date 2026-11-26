package shell

import (
	"testing"
	"time"
)

// TestRunningTaskSnapshots_MatchTheReferenceShape pins the checkpoint's
// running-task entry: the task's own id, the tool that created it, its summary
// and command, the start time in protojson form, the spawning step and the
// log URI.
func TestRunningTaskSnapshots_MatchTheReferenceShape(t *testing.T) {
	conv := "43966145-9669-4114-ac5c-91e08c7cd05b"
	started := time.Date(2026, 11, 3, 4, 5, 44, 24_948_000, time.UTC)
	task, err := backgroundTasks.newTask(conv+"/task-23437", "/tmp/brain/task-23437.log", "serve proxy", nil)
	if err != nil {
		t.Fatalf("newTask: %v", err)
	}
	defer backgroundTasks.remove(task.id)
	task.started = started
	task.toolName = "run_command"
	task.toolSummary = "Start proxy on port 19998"
	task.isDaemon = true
	task.stepIndex = 23437

	// A second task of another conversation must not leak into the section.
	other, err := backgroundTasks.newTask("other-conv/task-1", "/tmp/other.log", "ignore", nil)
	if err != nil {
		t.Fatalf("newTask: %v", err)
	}
	defer backgroundTasks.remove(other.id)

	got := RunningTaskSnapshots(conv)
	if len(got) != 1 {
		t.Fatalf("RunningTaskSnapshots = %+v, want one task", got)
	}
	ref := got[0]
	if ref.TaskID != conv+"/task-23437" {
		t.Errorf("taskId = %q", ref.TaskID)
	}
	if ref.ToolName != "run_command" {
		t.Errorf("toolName = %q", ref.ToolName)
	}
	if ref.ToolSummary != "Start proxy on port 19998" {
		t.Errorf("toolSummary = %q", ref.ToolSummary)
	}
	if ref.Description != "serve proxy" {
		t.Errorf("description = %q", ref.Description)
	}
	if ref.StartTime != "2026-11-03T04:05:44.024948Z" {
		t.Errorf("startTime = %q", ref.StartTime)
	}
	if ref.StepIndex != 23437 {
		t.Errorf("stepIndex = %d", ref.StepIndex)
	}
	if ref.LogURI != "file:///tmp/brain/task-23437.log" {
		t.Errorf("logUri = %q", ref.LogURI)
	}
	if !ref.IsDaemon {
		t.Error("isDaemon = false")
	}
}

// TestRunningTaskSnapshots_SkipFinishedAndForeignTasks keeps a checkpoint from
// reporting a task the conversation no longer has running.
func TestRunningTaskSnapshots_SkipFinishedAndForeignTasks(t *testing.T) {
	conv := "conv-finished"
	done, err := backgroundTasks.newTask(conv+"/task-1", "/tmp/done.log", "done", nil)
	if err != nil {
		t.Fatalf("newTask: %v", err)
	}
	defer backgroundTasks.remove(done.id)
	done.mu.Lock()
	done.completed = true
	done.mu.Unlock()

	killed, err := backgroundTasks.newTask(conv+"/task-2", "/tmp/killed.log", "killed", nil)
	if err != nil {
		t.Fatalf("newTask: %v", err)
	}
	defer backgroundTasks.remove(killed.id)
	killed.mu.Lock()
	killed.killed = true
	killed.mu.Unlock()

	if got := RunningTaskSnapshots(conv); len(got) != 0 {
		t.Fatalf("RunningTaskSnapshots = %+v, want none", got)
	}
	if got := RunningTaskSnapshots(""); got != nil {
		t.Fatalf("RunningTaskSnapshots(\"\") = %+v, want nil", got)
	}
}

// TestProtoJSONTimestamp pins the protojson fractional-width rule: 0, 3, 6 or
// 9 digits, never a trailing-zero trim at an unsupported width.
func TestProtoJSONTimestamp(t *testing.T) {
	cases := []struct {
		in   time.Time
		want string
	}{
		{time.Date(2026, 11, 3, 4, 5, 44, 0, time.UTC), "2026-11-03T04:05:44Z"},
		{time.Date(2026, 11, 3, 4, 5, 44, 24_000_000, time.UTC), "2026-11-03T04:05:44.024Z"},
		{time.Date(2026, 11, 3, 4, 5, 44, 24_948_000, time.UTC), "2026-11-03T04:05:44.024948Z"},
		{time.Date(2026, 11, 3, 4, 5, 44, 24_948_123, time.UTC), "2026-11-03T04:05:44.024948123Z"},
		{time.Time{}, ""},
	}
	for _, c := range cases {
		if got := protoJSONTimestamp(c.in); got != c.want {
			t.Errorf("protoJSONTimestamp(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestAllRunningTaskSnapshots(t *testing.T) {
	convA := "conv-all-a"
	convB := "conv-all-b"
	tA, err := backgroundTasks.newTask(convA+"/task-1", "/tmp/a.log", "sleep 10", nil)
	if err != nil {
		t.Fatalf("newTask A: %v", err)
	}
	defer backgroundTasks.remove(tA.id)
	tA.toolName = "run_command"
	tA.toolSummary = "Run sleep A"

	tB, err := backgroundTasks.newTask(convB+"/task-2", "/tmp/b.log", "sleep 20", nil)
	if err != nil {
		t.Fatalf("newTask B: %v", err)
	}
	defer backgroundTasks.remove(tB.id)
	tB.toolName = "run_command"
	tB.toolSummary = "Run sleep B"
	tB.mu.Lock()
	tB.completed = true
	tB.mu.Unlock()

	got := AllRunningTaskSnapshots()
	var foundA, foundB bool
	for _, item := range got {
		if item.Task.TaskID == tA.id {
			foundA = true
			if item.ConversationID != convA {
				t.Errorf("ConversationID = %q, want %q", item.ConversationID, convA)
			}
			if item.Task.ToolSummary != "Run sleep A" {
				t.Errorf("ToolSummary = %q, want %q", item.Task.ToolSummary, "Run sleep A")
			}
		}
		if item.Task.TaskID == tB.id {
			foundB = true
		}
	}
	if !foundA {
		t.Errorf("expected running task %q in AllRunningTaskSnapshots", tA.id)
	}
	if foundB {
		t.Errorf("completed task %q should not appear in AllRunningTaskSnapshots", tB.id)
	}
}
