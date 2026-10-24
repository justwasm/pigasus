package statusline

import (
	"sync"
	"sync/atomic"
)

// Payload is the JSON object the CLI writes to the status-line script stdin.
type Payload struct {
	Model             string  `json:"model"`
	Cost              float64 `json:"cost"`
	ConversationTitle string  `json:"conversation_title"`
	CurrentDir        string  `json:"current_dir"`
}

// State manages thread-safe cached rendered output and tick tracking for an
// external status line.
type State struct {
	renderMu        sync.Mutex
	rendered        string
	lastErr         error
	lastErrReported bool
	tickCount       int64
}

// MarkTick records a tick occurrence.
func (s *State) MarkTick() int64 {
	if s == nil {
		return 0
	}
	return atomic.AddInt64(&s.tickCount, 1)
}

// SnapshotTick returns current accumulated ticks.
func (s *State) SnapshotTick() int64 {
	if s == nil {
		return 0
	}
	return atomic.LoadInt64(&s.tickCount)
}

// ResetTick resets tick counter to 0.
func (s *State) ResetTick() {
	if s != nil {
		atomic.StoreInt64(&s.tickCount, 0)
	}
}

// SetRendered sets the latest rendered output or error.
func (s *State) SetRendered(out string, err error) {
	if s == nil {
		return
	}
	s.renderMu.Lock()
	defer s.renderMu.Unlock()
	s.rendered = out
	if err != nil && !s.lastErrReported {
		s.lastErr = err
		s.lastErrReported = true
	} else if err == nil {
		s.lastErr = nil
		s.lastErrReported = false
	}
}

// Current returns the current rendered output and any unreported error.
func (s *State) Current() (out string, err error) {
	if s == nil {
		return "", nil
	}
	s.renderMu.Lock()
	defer s.renderMu.Unlock()
	return s.rendered, s.lastErr
}
