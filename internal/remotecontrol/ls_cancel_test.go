// Package remotecontrol — ls_cancel_test.go: the Web UI's cancel RPCs.
//
// The contract under test is the one the IDE's own web bundle speaks:
//
//   - the chat area's Stop button calls CancelCascadeInvocation with
//     the cascade id and `killBackgroundTasks: true`,
//   - the background-tasks panel's "Cancel All Tasks" button calls
//     CancelCascadeSteps with the step indices of the running tasks,
//   - the host's variant calls ForceStopCascadeTree with the
//     conversation id.
//
// 【一手】 2026-10-26, from internal/rcfrontend/assets/main.js.
package remotecontrol

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// lsCancelBackend is a streaming backend whose turn writes the first
// word of the answer and then parks until the turn's context dies.
//
// It is how the cancel's tests hold a turn in flight: the answer is on
// the trajectory, the turn does not end, and only a cancel ends it.
type lsCancelBackend struct {
	mu       sync.Mutex
	canceled int
	entered  chan struct{}
	once     sync.Once
}

// newLSCancelBackend returns a backend whose turns park.
func newLSCancelBackend() *lsCancelBackend {
	return &lsCancelBackend{entered: make(chan struct{}, 8)}
}

// Send is the non-streaming path; the cancel's tests always stream.
func (b *lsCancelBackend) Send(ctx context.Context, prompt MessagePrompt) (MessageResult, error) {
	return MessageResult{}, errors.New("lsCancelBackend streams")
}

// SendStream writes one word of the answer and parks until the turn's
// context dies.
func (b *lsCancelBackend) SendStream(ctx context.Context, prompt MessagePrompt, sink EventSink, opts MessageSendOptions) (MessageResult, error) {
	b.once.Do(func() { close(b.entered) })
	if sink != nil {
		sink.OnTextDelta(nil, "hello")
	}
	<-ctx.Done()
	b.mu.Lock()
	b.canceled++
	b.mu.Unlock()
	return MessageResult{}, ctx.Err()
}

// cancelCount returns how many turns ended because their context was
// cancelled.
func (b *lsCancelBackend) cancelCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.canceled
}

// lsCancelCall dispatches one cancel RPC and returns its answer.
func lsCancelCall(t *testing.T, d *Dispatcher, method string, params map[string]any) (string, error) {
	t.Helper()
	payload, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	answer, err := d.DispatchDirect(context.Background(), method, string(payload))
	return answer, err
}

// TestLSCancel_CancelCascadeInvocationStopsTheTurn pins the Stop
// button's contract: the RPC answers, and the turn in flight ends.
func TestLSCancel_CancelCascadeInvocationStopsTheTurn(t *testing.T) {
	backend := newLSCancelBackend()
	d := newLSQueueDispatcher(t, backend)
	lsStartConversation(t, d, "conv-cancel")

	sent := make(chan error, 1)
	go func() { sent <- lsSendMessage(t, d, "conv-cancel", "hello", int(lsDeliveryWhenIdle)) }()
	select {
	case <-backend.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the turn never started")
	}

	answer, err := lsCancelCall(t, d, "CancelCascadeInvocation", map[string]any{
		"cascadeId":           "conv-cancel",
		"killBackgroundTasks": true,
	})
	if err != nil {
		t.Fatalf("CancelCascadeInvocation: %v", err)
	}
	if strings.TrimSpace(answer) != "{}" {
		t.Errorf("answer = %q, want the empty message the proto declares", answer)
	}

	select {
	case err := <-sent:
		if err != nil {
			t.Fatalf("the interrupted turn reported an error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the cancel did not end the turn")
	}
	if got := backend.cancelCount(); got != 1 {
		t.Errorf("cancelled turns = %d, want 1", got)
	}
}

// TestLSCancel_InterruptedTurnEndsInterrupted checks the step the
// interrupted turn leaves behind: the answer the turn had written ends
// with the interrupted status, not with "done".
func TestLSCancel_InterruptedTurnEndsInterrupted(t *testing.T) {
	backend := newLSCancelBackend()
	d := newLSQueueDispatcher(t, backend)
	lsStartConversation(t, d, "conv-status")

	sent := make(chan error, 1)
	go func() { sent <- lsSendMessage(t, d, "conv-status", "hello", int(lsDeliveryWhenIdle)) }()
	select {
	case <-backend.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the turn never started")
	}
	if _, err := lsCancelCall(t, d, "CancelCascadeInvocation", map[string]any{"cascadeId": "conv-status"}); err != nil {
		t.Fatalf("CancelCascadeInvocation: %v", err)
	}
	select {
	case <-sent:
	case <-time.After(5 * time.Second):
		t.Fatal("the cancel did not end the turn")
	}

	traj := d.trajectories.Get("conv-status")
	snap := traj.Snapshot()
	if len(snap.Steps) == 0 {
		t.Fatal("the trajectory has no steps")
	}
	last := snap.Steps[len(snap.Steps)-1]
	if got := last["status"]; got != lsStepStatusInterrupted {
		t.Errorf("the last step's status = %v, want %d (INTERRUPTED)", got, lsStepStatusInterrupted)
	}
}

// TestLSCancel_UnknownCascade checks the answer for a cascade the
// clone does not know: the clone's own error envelope.
func TestLSCancel_UnknownCascade(t *testing.T) {
	backend := newLSCancelBackend()
	d := newLSQueueDispatcher(t, backend)
	_, err := lsCancelCall(t, d, "CancelCascadeInvocation", map[string]any{"cascadeId": "conv-nope"})
	if err == nil {
		t.Fatal("the cancel of an unknown cascade answered")
	}
	if !strings.Contains(err.Error(), "-32004") {
		t.Errorf("error = %v, want the cascade-not-found code (-32004)", err)
	}
}

// TestLSCancel_CancelCascadeStepsMarksTheStepCancelled checks the
// background-tasks panel's button: the step it names is marked
// cancelled, which is what the Web UI renders as "Canceled".
func TestLSCancel_CancelCascadeStepsMarksTheStepCancelled(t *testing.T) {
	backend := newLSCancelBackend()
	d := newLSQueueDispatcher(t, backend)
	lsStartConversation(t, d, "conv-steps")

	traj := d.trajectories.Get("conv-steps")
	index := traj.Append(map[string]any{
		"type":   lsStepTypeGeneric,
		"status": lsStepStatusDone,
		"generic": map[string]any{
			"toolCall": map[string]any{"name": "run_command"},
			"result":   map[string]any{"result": "Tool is running as a background task with task id: conv-steps/task-1"},
		},
	})

	if _, err := lsCancelCall(t, d, "CancelCascadeSteps", map[string]any{
		"cascadeId":   "conv-steps",
		"stepIndices": []any{index},
	}); err != nil {
		t.Fatalf("CancelCascadeSteps: %v", err)
	}
	steps := traj.Snapshot().Steps
	if len(steps) == 0 {
		t.Fatal("the trajectory has no steps")
	}
	if got := steps[index]["status"]; got != lsStepStatusCanceled {
		t.Errorf("the step's status = %v, want %d (CANCELED)", got, lsStepStatusCanceled)
	}
}

// TestLSCancel_ForceStopCascadeTree checks the host's variant: the
// answer lists the conversation that was stopped.
func TestLSCancel_ForceStopCascadeTree(t *testing.T) {
	backend := newLSCancelBackend()
	d := newLSQueueDispatcher(t, backend)
	lsStartConversation(t, d, "conv-tree")

	sent := make(chan error, 1)
	go func() { sent <- lsSendMessage(t, d, "conv-tree", "hello", int(lsDeliveryWhenIdle)) }()
	select {
	case <-backend.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the turn never started")
	}

	answer, err := lsCancelCall(t, d, "ForceStopCascadeTree", map[string]any{"conversationId": "conv-tree"})
	if err != nil {
		t.Fatalf("ForceStopCascadeTree: %v", err)
	}
	if !strings.Contains(answer, "conv-tree") {
		t.Errorf("answer = %q, want the stopped conversation id", answer)
	}
	select {
	case <-sent:
	case <-time.After(5 * time.Second):
		t.Fatal("the cancel did not end the turn")
	}
}

// TestLSCancel_NextTurnIsNotCancelled checks the flag the cancel
// leaves behind: a turn that fails after an earlier turn was cancelled
// reports its own failure, not the cancellation.
func TestLSCancel_NextTurnIsNotCancelled(t *testing.T) {
	c, err := NewCascade(CascadeOpts{ID: "conv-flag", Model: "test-model"})
	if err != nil {
		t.Fatalf("NewCascade: %v", err)
	}
	if fired := c.Cancel("user requested"); fired {
		t.Errorf("Cancel() with no per-turn cancel returned true; want false")
	}
	if !c.IsCancelled() {
		t.Fatal("the cascade is not cancelled")
	}
	// The next turn starts: it is not a cancelled turn.
	c.StartCancel(context.Background())
	if c.IsCancelled() {
		t.Error("the cancelled flag survived the next turn; want it cleared")
	}
}
