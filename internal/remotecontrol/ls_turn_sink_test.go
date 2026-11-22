package remotecontrol

import (
	"context"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/app/session/convstore"
	"github.com/justwasm/pigasus/internal/remotecontrol/cascades"
)

// TestLSTrajectorySink_ToolCallRunningToDone verifies the lifecycle of a tool call
// from in-flight (RUNNING) to completion (DONE) across the trajectory and storage.
func TestLSTrajectorySink_ToolCallRunningToDone(t *testing.T) {
	traj := newLSTrajectory("conv-tool-run")
	sink := newLSTrajectorySink(traj, nil)

	call := cascades.ToolCall{
		Name: "run_command",
		Args: []byte(`{"command":"echo hello"}`),
	}

	// 1. Tool start: emits RUNNING step to trigger the UI spinner card.
	sink.OnToolCallStart(nil, call)

	liveSteps := traj.Snapshot().Steps
	if len(liveSteps) != 1 {
		t.Fatalf("expected 1 step in trajectory after OnToolCallStart, got %d", len(liveSteps))
	}
	step0 := liveSteps[0]
	if status, ok := step0["status"].(int); !ok || status != lsStepStatusRunning {
		t.Errorf("step 0 status = %v, want %d (lsStepStatusRunning)", step0["status"], lsStepStatusRunning)
	}
	meta, ok := step0["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("step 0 missing metadata: %v", step0)
	}
	tcMeta, ok := meta["toolCall"].(map[string]any)
	if !ok || tcMeta["name"] != "run_command" {
		t.Errorf("expected toolCall metadata name 'run_command', got %+v", tcMeta)
	}

	stored := sink.StoredSteps()
	if len(stored) != 1 {
		t.Fatalf("expected 1 stored step after OnToolCallStart, got %d", len(stored))
	}
	if stored[0].Type != convstore.StepToolCall {
		t.Errorf("stored[0].Type = %v, want %v", stored[0].Type, convstore.StepToolCall)
	}
	if stored[0].Status != int64(lsStepStatusRunning) {
		t.Errorf("stored[0].Status = %v, want %d", stored[0].Status, lsStepStatusRunning)
	}
	if stored[0].ToolName != "run_command" {
		t.Errorf("stored[0].ToolName = %q, want %q", stored[0].ToolName, "run_command")
	}

	// 2. Tool completion: updates step in place with DONE status and result.
	call.Result = "hello\n"
	call.OK = true
	sink.OnToolCall(nil, call)

	liveSteps = traj.Snapshot().Steps
	if len(liveSteps) != 1 {
		t.Fatalf("expected trajectory to still have exactly 1 step (in-place update), got %d", len(liveSteps))
	}
	step0 = liveSteps[0]
	if status, ok := step0["status"].(int); !ok || status != lsStepStatusDone {
		t.Errorf("step 0 status = %v, want %d (lsStepStatusDone)", step0["status"], lsStepStatusDone)
	}
	generic, ok := step0["generic"].(map[string]any)
	if !ok {
		t.Fatalf("step 0 missing generic payload: %v", step0)
	}
	resMap, ok := generic["result"].(map[string]any)
	if !ok || resMap["result"] != "hello\n" {
		t.Errorf("step 0 generic.result = %+v, want 'hello\\n'", generic["result"])
	}

	// Verify step retains stepInfo metadata
	meta, ok = step0["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("step 0 missing metadata: %v", step0)
	}
	stepInfo, ok := meta["sourceTrajectoryStepInfo"].(map[string]any)
	if !ok {
		t.Fatalf("step 0 missing sourceTrajectoryStepInfo: %v", meta)
	}
	if stepInfo["stepIndex"] != 0 {
		t.Errorf("stepInfo.stepIndex = %v, want 0", stepInfo["stepIndex"])
	}

	// Verify stored step updated
	stored = sink.StoredSteps()
	if len(stored) != 1 {
		t.Fatalf("expected 1 stored step after OnToolCall, got %d", len(stored))
	}
	if stored[0].Status != int64(lsStepStatusDone) {
		t.Errorf("stored[0].Status = %v, want %d", stored[0].Status, lsStepStatusDone)
	}
	if stored[0].ToolResult != "hello\n" {
		t.Errorf("stored[0].ToolResult = %q, want 'hello\\n'", stored[0].ToolResult)
	}
}

// TestLSTrajectorySink_ToolCallWithoutStart tests that an un-streamed or legacy
// tool call directly emits a completed (DONE) step if no OnToolCallStart preceded it.
func TestLSTrajectorySink_ToolCallWithoutStart(t *testing.T) {
	traj := newLSTrajectory("conv-tool-nostart")
	sink := newLSTrajectorySink(traj, nil)

	call := cascades.ToolCall{
		Name:   "view_file",
		Args:   []byte(`{"path":"main.go"}`),
		Result: "package main\n",
		OK:     true,
	}

	sink.OnToolCall(nil, call)

	liveSteps := traj.Snapshot().Steps
	if len(liveSteps) != 1 {
		t.Fatalf("expected 1 step in trajectory, got %d", len(liveSteps))
	}
	if liveSteps[0]["status"] != lsStepStatusDone {
		t.Errorf("step status = %v, want %d", liveSteps[0]["status"], lsStepStatusDone)
	}

	stored := sink.StoredSteps()
	if len(stored) != 1 {
		t.Fatalf("expected 1 stored step, got %d", len(stored))
	}
	if stored[0].Status != int64(lsStepStatusDone) {
		t.Errorf("stored status = %v, want %d", stored[0].Status, lsStepStatusDone)
	}
	if stored[0].ToolResult != "package main\n" {
		t.Errorf("stored tool result = %q, want 'package main\\n'", stored[0].ToolResult)
	}
}

// TestLSTrajectorySink_MultipleSequentialTools tests running and finishing multiple tools in sequence.
func TestLSTrajectorySink_MultipleSequentialTools(t *testing.T) {
	traj := newLSTrajectory("conv-seq-tools")
	sink := newLSTrajectorySink(traj, nil)

	// Tool 1
	sink.OnToolCallStart(nil, cascades.ToolCall{Name: "tool1", Args: []byte(`{}`)})
	if len(traj.Snapshot().Steps) != 1 || traj.Snapshot().Steps[0]["status"] != lsStepStatusRunning {
		t.Fatalf("expected tool1 to be RUNNING")
	}
	sink.OnToolCall(nil, cascades.ToolCall{Name: "tool1", Args: []byte(`{}`), Result: "res1"})
	if len(traj.Snapshot().Steps) != 1 || traj.Snapshot().Steps[0]["status"] != lsStepStatusDone {
		t.Fatalf("expected tool1 to be DONE")
	}

	// Assistant text delta
	sink.OnTextDelta(nil, "Looking at res1, running tool2...")

	// Tool 2 starts: text reply should be closed as DONE, tool2 appended as RUNNING
	sink.OnToolCallStart(nil, cascades.ToolCall{Name: "tool2", Args: []byte(`{}`)})
	steps := traj.Snapshot().Steps
	if len(steps) != 3 { // tool1, text reply, tool2
		t.Fatalf("expected 3 steps, got %d", len(steps))
	}
	if steps[1]["status"] != lsStepStatusDone {
		t.Errorf("text reply should be closed with DONE status, got %v", steps[1]["status"])
	}
	if steps[2]["status"] != lsStepStatusRunning {
		t.Errorf("tool2 step should be RUNNING, got %v", steps[2]["status"])
	}

	sink.OnToolCall(nil, cascades.ToolCall{Name: "tool2", Args: []byte(`{}`), Result: "res2"})
	steps = traj.Snapshot().Steps
	if len(steps) != 3 {
		t.Fatalf("expected 3 steps after tool2 done, got %d", len(steps))
	}
	if steps[2]["status"] != lsStepStatusDone {
		t.Errorf("tool2 step should be updated to DONE, got %v", steps[2]["status"])
	}
}

// TestLSTrajectorySink_FinishWhileToolRunning_Interrupted tests that if a turn
// is cancelled while a tool is in-flight, Finish transitions the running step
// to INTERRUPTED status instead of leaving it dangling in RUNNING state.
func TestLSTrajectorySink_FinishWhileToolRunning_Interrupted(t *testing.T) {
	traj := newLSTrajectory("conv-interrupted")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	traj.BeginTurn(cancel)

	sink := newLSTrajectorySink(traj, nil)

	sink.OnToolCallStart(nil, cascades.ToolCall{
		Name: "long_running_task",
		Args: []byte(`{"wait":true}`),
	})

	// User cancels the turn
	traj.Interrupt()
	select {
	case <-ctx.Done():
	default:
		t.Fatal("context should have been cancelled by traj.Interrupt()")
	}

	// Finish is called on turn teardown
	sink.Finish(cascades.Result{}, 500*time.Millisecond)

	steps := traj.Snapshot().Steps
	if len(steps) != 1 {
		t.Fatalf("expected 1 step in trajectory, got %d", len(steps))
	}
	if status, ok := steps[0]["status"].(int); !ok || status != lsStepStatusInterrupted {
		t.Errorf("running tool step should have transitioned to %d (INTERRUPTED), got %v", lsStepStatusInterrupted, steps[0]["status"])
	}

	stored := sink.StoredSteps()
	if len(stored) != 1 {
		t.Fatalf("expected 1 stored step, got %d", len(stored))
	}
	if stored[0].Status != int64(lsStepStatusInterrupted) {
		t.Errorf("stored step status = %v, want %d (INTERRUPTED)", stored[0].Status, lsStepStatusInterrupted)
	}
}
