package statusline

import (
	"sync"
	"sync/atomic"
)

// State manages thread-safe cached rendered output, tick tracking and the
// consecutive-failure counter for an external status line.
//
// The reference stops running a status-line script after 30 failed runs in
// a row and shows `⚠ Statusline disabled after 30 consecutive failures.`
// for the rest of the session. Evidence: the reference's own status line
// documentation; the format string `Statusline disabled after %d
// consecutive failures` is in agy 1.4.7's string table.
type State struct {
	renderMu        sync.Mutex
	rendered        string
	lastErr         error
	lastErrReported bool
	tickCount       int64
	failures        int
	disabled        bool
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

// RecordFailure counts one failed script run and returns the new
// consecutive-failure count together with whether the status line has just
// been disabled.
func (s *State) RecordFailure() (count int, disabled bool) {
	if s == nil {
		return 0, false
	}
	s.renderMu.Lock()
	defer s.renderMu.Unlock()
	s.failures++
	if s.failures >= MaxConsecutiveFailures {
		s.disabled = true
	}
	return s.failures, s.disabled
}

// RecordSuccess clears the consecutive-failure counter.
func (s *State) RecordSuccess() {
	if s == nil {
		return
	}
	s.renderMu.Lock()
	defer s.renderMu.Unlock()
	s.failures = 0
}

// Failures returns the number of consecutive failures.
func (s *State) Failures() int {
	if s == nil {
		return 0
	}
	s.renderMu.Lock()
	defer s.renderMu.Unlock()
	return s.failures
}

// Disabled reports whether too many consecutive failures have switched the
// status line off for the rest of the session.
func (s *State) Disabled() bool {
	if s == nil {
		return false
	}
	s.renderMu.Lock()
	defer s.renderMu.Unlock()
	return s.disabled
}

// ResetDisabled clears the disabled flag. It is used when the user points
// the status line at a different command.
func (s *State) ResetDisabled() {
	if s == nil {
		return
	}
	s.renderMu.Lock()
	defer s.renderMu.Unlock()
	s.failures = 0
	s.disabled = false
}
