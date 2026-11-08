package tui

import (
	"testing"

	"github.com/justwasm/pigasus/internal/app/session"
	"github.com/justwasm/pigasus/internal/core/compaction"
)

// TestCompactionModel_LazilyOwnsRuntime pins the ownership move: a bare
// RootModel literal still gets a runtime, and the model forwards the
// app-tier compactor and done-channel seams.
func TestCompactionModel_LazilyOwnsRuntime(t *testing.T) {
	var c CompactionModel
	if c.runtime != nil {
		t.Fatal("zero model created a runtime eagerly")
	}
	if c.Runtime() == nil {
		t.Fatal("Runtime did not create the runtime")
	}
	if c.Runtime() != c.Runtime() {
		t.Fatal("Runtime replaced the runtime")
	}
	if c.Compactor() != nil || c.IsCompacting() {
		t.Fatal("empty compaction model reports a compactor")
	}
	if c.Done() != nil {
		t.Fatal("empty compaction model has a done channel")
	}
}

// TestCompactionModel_UsesSessionRuntime pins that the model is a handle
// on session.CompactionRuntime, not a second copy of its state.
func TestCompactionModel_UsesSessionRuntime(t *testing.T) {
	var c CompactionModel
	rt := session.NewCompactionRuntime()
	c.runtime = rt
	ch := make(chan compaction.GenericDoneMsg, 1)
	c.SetDone(ch)
	if c.Runtime() != rt {
		t.Fatal("Runtime returned another runtime")
	}
	if c.Done() != ch {
		t.Fatal("the model did not forward the done channel")
	}
}
