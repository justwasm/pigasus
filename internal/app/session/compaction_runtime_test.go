package session

import (
	"testing"

	"github.com/justwasm/pigasus/internal/core/compaction"
)

// TestCompactionRuntime_DoneChannel pins the runtime's remaining seam: the
// no-program result channel the generic summarizer hands its result to.
//
// The compactor seam that used to live here (Compactor / SetCompactor /
// IsCompacting) was deleted with the manual /compact path: it had no
// production implementation, and the runtime it was meant to wrap
// (pigrt.Runtime) is no longer in the tree.
func TestCompactionRuntime_DoneChannel(t *testing.T) {
	var c CompactionRuntime
	if c.Done() != nil {
		t.Fatal("empty runtime has a done channel")
	}

	ch := make(chan compaction.GenericDoneMsg, 1)
	c.SetDone(ch)
	if c.Done() != ch {
		t.Fatal("SetDone did not install the channel")
	}
}

// TestCompactionRuntime_NilReceiverIsSafe pins the zero-value contract: the
// model calls through a possibly-nil runtime, so the accessors must not
// panic on one.
func TestCompactionRuntime_NilReceiverIsSafe(t *testing.T) {
	var c *CompactionRuntime
	if c.Done() != nil {
		t.Fatal("nil runtime returned a channel")
	}
	c.SetDone(make(chan compaction.GenericDoneMsg, 1))
}
