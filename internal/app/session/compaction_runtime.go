package session

// compaction_runtime.go — the session's compaction runtime.
//
// The TUI's manual /compact path needs two things that are not the
// turn's transport: an optional compactor (the tests wire one; production
// falls back to the generic summarizer) and the channel a no-program run
// uses to hand its result back to the UI goroutine. They live in the app
// tier so the host does not carry compaction state of its own.

import (
	"sync"

	"github.com/justwasm/pigasus/internal/core/compaction"
)

// CompactionRuntime owns the manual-compaction seam.
//
// The zero value is usable. Methods are safe for concurrent use: the
// compaction worker posts its result while the UI goroutine reads it.
type CompactionRuntime struct {
	mu        sync.Mutex
	compactor compaction.ManualCompactor
	done      chan compaction.GenericDoneMsg
}

// NewCompactionRuntime returns an empty compaction runtime.
func NewCompactionRuntime() *CompactionRuntime {
	return &CompactionRuntime{}
}

// Compactor returns the wired compactor, or nil when the generic
// summarizer path should run.
func (c *CompactionRuntime) Compactor() compaction.ManualCompactor {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.compactor
}

// SetCompactor installs the compactor. A nil compactor selects the
// generic summarizer.
func (c *CompactionRuntime) SetCompactor(compactor compaction.ManualCompactor) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.compactor = compactor
	c.mu.Unlock()
}

// IsCompacting reports whether the wired compactor is mid-compaction.
// A nil compactor is never compacting.
func (c *CompactionRuntime) IsCompacting() bool {
	compactor := c.Compactor()
	return compactor != nil && compactor.IsCompacting()
}

// Done returns the channel a no-program generic compaction posts its
// result to. It is nil until SetDone installs one.
func (c *CompactionRuntime) Done() chan compaction.GenericDoneMsg {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.done
}

// SetDone installs the no-program result channel.
func (c *CompactionRuntime) SetDone(ch chan compaction.GenericDoneMsg) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.done = ch
	c.mu.Unlock()
}
