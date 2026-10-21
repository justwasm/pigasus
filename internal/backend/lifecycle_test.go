package backend

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestLifecycle_HappyPath exercises the full Init -> Run -> Save ->
// Close cycle against the Mock client. The recovered `cascader.Run`
// is wrapped by Lifecycle.Run; this test mirrors the cycle 41
// acceptance criteria.
func TestLifecycle_HappyPath(t *testing.T) {
	mock := NewMock()
	lc, err := NewLifecycle(LifecycleConfig{
		Client:       mock,
		Sink:         NewTextSink(nil),
		DefaultModel: "claude-4.5-sonnet",
	})
	if err != nil {
		t.Fatalf("NewLifecycle: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	model, err := lc.Init(ctx, "claude-4.5-sonnet")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if model != "claude-4.5-sonnet" {
		t.Fatalf("expected model=claude-4.5-sonnet, got %s", model)
	}
	if got := lc.State(); got != LStateReady {
		t.Fatalf("expected LStateReady, got %s", got)
	}

	if err := lc.Run(ctx, "hello world"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := lc.State(); got != LStateDone {
		t.Fatalf("expected LStateDone, got %s", got)
	}
	if !strings.Contains(lc.Transcript(), "mock reply") {
		t.Fatalf("expected transcript to contain 'mock reply', got %q", lc.Transcript())
	}

	if err := lc.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := lc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// TestLifecycle_OnStateTransition asserts the recovered
// printmode-emitter FSM is observable through OnStateTransition. The
// order should be init -> ready -> execute -> stream -> save -> done.
func TestLifecycle_OnStateTransition(t *testing.T) {
	mock := NewMock()
	var mu sync.Mutex
	var got []LifecycleState
	lc, err := NewLifecycle(LifecycleConfig{
		Client:       mock,
		Sink:         NewTextSink(nil),
		DefaultModel: "claude-4.5-sonnet",
		OnStateTransition: func(_, to LifecycleState, _ string) {
			mu.Lock()
			got = append(got, to)
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatalf("NewLifecycle: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := lc.Init(ctx, "claude-4.5-sonnet"); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := lc.Run(ctx, "hi"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if err := lc.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	// Expect: ready, execute, then one or more stream, then save, done,
	// then save (from Save()), then done. The mock reply "mock reply
	// from model=claude-4.5-sonnet: hi" is 47 chars / 12 = 4 chunks,
	// so we expect 4 stream transitions.
	want := []LifecycleState{LStateReady, LStateExecute, LStateStream, LStateStream, LStateStream, LStateStream, LStateSave, LStateDone, LStateSave, LStateDone}
	if len(got) != len(want) {
		t.Fatalf("expected %d transitions, got %d (%v)", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("transition %d: expected %s, got %s", i, want[i], got[i])
		}
	}
}

// TestLifecycle_CloseIdempotent ensures Close() can be called multiple
// times without error (mirrors the decompiled (*ServerBackend).Close
// thunk at 0x87566c0).
func TestLifecycle_CloseIdempotent(t *testing.T) {
	mock := NewMock()
	lc, err := NewLifecycle(LifecycleConfig{Client: mock, Sink: NewTextSink(nil), DefaultModel: "m"})
	if err != nil {
		t.Fatalf("NewLifecycle: %v", err)
	}
	if err := lc.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := lc.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

// TestLifecycle_InitAfterClose ensures Init() refuses to run after
// Close (mirrors the decompiled `(*ServerBackend).Close` zeroing the
// 3-field struct).
func TestLifecycle_InitAfterClose(t *testing.T) {
	mock := NewMock()
	lc, err := NewLifecycle(LifecycleConfig{Client: mock, Sink: NewTextSink(nil), DefaultModel: "m"})
	if err != nil {
		t.Fatalf("NewLifecycle: %v", err)
	}
	if err := lc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := lc.Init(context.Background(), "m"); err == nil {
		t.Fatal("expected error from Init after Close")
	}
}

// TestLifecycle_Emit verifies the explicit Emit escape hatch (used by
// callers that want to push a chunk through the sink without going
// through Run).
func TestLifecycle_Emit(t *testing.T) {
	mock := NewMock()
	lc, err := NewLifecycle(LifecycleConfig{Client: mock, Sink: NewTextSink(nil), DefaultModel: "m"})
	if err != nil {
		t.Fatalf("NewLifecycle: %v", err)
	}
	if err := lc.Emit("hello"); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if got := lc.Transcript(); got != "hello" {
		t.Fatalf("expected transcript=hello, got %q", got)
	}
	if got := lc.State(); got != LStateStream {
		t.Fatalf("expected LStateStream, got %s", got)
	}
}

// TestLifecycle_RunBeforeInit ensures the lifecycle refuses Run() if
// Init() was not called first.
func TestLifecycle_RunBeforeInit(t *testing.T) {
	mock := NewMock()
	lc, err := NewLifecycle(LifecycleConfig{Client: mock, Sink: NewTextSink(nil), DefaultModel: "m"})
	if err != nil {
		t.Fatalf("NewLifecycle: %v", err)
	}
	if err := lc.Run(context.Background(), "hi"); err == nil {
		t.Fatal("expected error from Run before Init")
	}
}

// TestLifecycle_NilClient ensures NewLifecycle rejects a nil Client
// (matches the cycle 40 backend.NewRealClient's nil-handling).
func TestLifecycle_NilClient(t *testing.T) {
	if _, err := NewLifecycle(LifecycleConfig{}); err == nil {
		t.Fatal("expected error from NewLifecycle with nil Client")
	}
}
