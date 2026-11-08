package tui

// compaction_model.go — the manual-compaction sub-model.
//
// CompactionModel is the host's handle on the app-tier compaction
// runtime. The runtime (session.CompactionRuntime) owns the optional
// compactor and the no-program result channel; this model only keeps the
// pointer and creates the runtime on first use, so a bare RootModel
// literal still works.
//
// Upstream: the reference's RootModel keeps its compaction seam beside
// the engine; the clone gives that seam a named owner and the state a
// Tier-4 home.

import (
	"github.com/justwasm/pigasus/internal/app/session"
	"github.com/justwasm/pigasus/internal/core/compaction"
)

// CompactionModel owns the manual-compaction runtime.
type CompactionModel struct {
	runtime *session.CompactionRuntime
}

// Runtime returns the app-tier runtime, creating it on first use.
func (c *CompactionModel) Runtime() *session.CompactionRuntime {
	if c == nil {
		return nil
	}
	if c.runtime == nil {
		c.runtime = session.NewCompactionRuntime()
	}
	return c.runtime
}

// Compactor returns the wired compactor, or nil for the generic path.
func (c *CompactionModel) Compactor() compaction.ManualCompactor {
	return c.Runtime().Compactor()
}

// SetCompactor installs the compactor (tests) or nil (production).
func (c *CompactionModel) SetCompactor(compactor compaction.ManualCompactor) {
	c.Runtime().SetCompactor(compactor)
}

// IsCompacting reports whether the wired compactor is mid-compaction.
func (c *CompactionModel) IsCompacting() bool {
	return c.Runtime().IsCompacting()
}

// Done returns the no-program result channel.
func (c *CompactionModel) Done() chan compaction.GenericDoneMsg {
	return c.Runtime().Done()
}

// SetDone installs the no-program result channel.
func (c *CompactionModel) SetDone(ch chan compaction.GenericDoneMsg) {
	c.Runtime().SetDone(ch)
}
