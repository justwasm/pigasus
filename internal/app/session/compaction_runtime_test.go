package session

import (
	"context"
	"testing"

	"github.com/justwasm/pigasus/internal/core/compaction"
)

// fakeCompactor is the smallest ManualCompactor: it reports a state and
// records the compact call.
type fakeCompactor struct {
	compacting bool
	called     bool
}

func (f *fakeCompactor) IsCompacting() bool { return f.compacting }
func (f *fakeCompactor) Compact(_ context.Context, _ string) error {
	f.called = true
	return nil
}
func (f *fakeCompactor) SubscribeManual(func(any)) func() { return func() {} }

// TestCompactionRuntime_CompactorSeam pins the runtime: the compactor is
// optional, a nil one is never compacting, and the done channel is the
// no-program hand-off.
func TestCompactionRuntime_CompactorSeam(t *testing.T) {
	var c CompactionRuntime
	if c.Compactor() != nil || c.IsCompacting() {
		t.Fatal("empty runtime reports a compactor")
	}
	if c.Done() != nil {
		t.Fatal("empty runtime has a done channel")
	}

	ch := make(chan compaction.GenericDoneMsg, 1)
	c.SetDone(ch)
	if c.Done() != ch {
		t.Fatal("SetDone did not install the channel")
	}
}

// TestCompactionRuntime_IsCompacting pins the forwarded state.
func TestCompactionRuntime_IsCompacting(t *testing.T) {
	var c CompactionRuntime
	comp := &fakeCompactor{compacting: true}
	c.SetCompactor(comp)
	if !c.IsCompacting() {
		t.Fatal("IsCompacting did not read the compactor")
	}
	comp.compacting = false
	if c.IsCompacting() {
		t.Fatal("IsCompacting stayed true")
	}
	c.SetCompactor(nil)
	if c.IsCompacting() {
		t.Fatal("a nil compactor reports compacting")
	}
}
