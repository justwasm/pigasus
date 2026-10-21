package agent

import (
	"strings"
	"sync"
	"testing"
)

func TestCascade_HappyPath(t *testing.T) {
	var trans []CascadeTransition
	var mu sync.Mutex
	c := NewCascade(func(t CascadeTransition) {
		mu.Lock()
		defer mu.Unlock()
		trans = append(trans, t)
	})

	if c.State() != StateInit {
		t.Fatalf("expected initial state init, got %s", c.State())
	}

	// Walk the happy path explicitly
	want := []struct {
		from CascadeState
		to   CascadeState
	}{
		{StateInit, StateRead},
		{StateRead, StatePlan},
		{StatePlan, StateExecute},
		{StateExecute, StateStream},
		{StateStream, StateSave},
		{StateSave, StateDone},
	}
	for _, w := range want {
		if err := c.Transition(w.to, "test"); err != nil {
			t.Fatalf("transition %s -> %s: %v", w.from, w.to, err)
		}
		if c.State() != w.to {
			t.Fatalf("state after %s -> %s: got %s", w.from, w.to, c.State())
		}
	}

	chain := c.RenderChain()
	if !strings.Contains(chain, "init -> read -> plan -> execute -> stream -> save -> done") {
		t.Errorf("RenderChain missing happy path: %s", chain)
	}
}

func TestCascade_RejectsInvalidEdge(t *testing.T) {
	c := NewCascade(nil)
	if err := c.Transition(StateExecute, "jump"); err == nil {
		t.Errorf("expected error for init -> execute (skipping read/plan)")
	}
}

func TestCascade_ErrorEscapeHatch(t *testing.T) {
	c := NewCascade(nil)
	// Walk to stream first (must follow the happy path), then escape to error.
	for _, s := range []CascadeState{StateRead, StatePlan, StateExecute, StateStream} {
		if err := c.Transition(s, ""); err != nil {
			t.Fatalf("transition -> %s: %v", s, err)
		}
	}
	if err := c.Transition(StateError, "oops"); err != nil {
		t.Fatalf("stream -> error: %v", err)
	}
	if c.State() != StateError {
		t.Errorf("expected error state, got %s", c.State())
	}
}

func TestCascade_Reset(t *testing.T) {
	c := NewCascade(nil)
	_ = c.Transition(StateRead, "")
	_ = c.Transition(StatePlan, "")
	c.Reset()
	if c.State() != StateInit {
		t.Errorf("after Reset, state = %s", c.State())
	}
	if len(c.History()) == 0 {
		t.Errorf("after Reset, history should be non-empty (synthetic init entry)")
	}
}

func TestCascade_IdempotentSameState(t *testing.T) {
	c := NewCascade(nil)
	// Transition to the same state is a no-op (returns nil).
	if err := c.Transition(StateInit, "redundant"); err != nil {
		t.Errorf("init -> init should be idempotent, got %v", err)
	}
}
