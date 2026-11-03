package subagenttools

import (
	"errors"
	"sync"
	"time"
)

// DefaultWatchdogTimeout is the duration of zero activity before a streaming turn
// is considered stalled.
const DefaultWatchdogTimeout = 60 * time.Second

// ErrConnectionStalled indicates that no network stream tokens or tool updates
// were received within the watchdog window.
var ErrConnectionStalled = errors.New("subagent connection stalled: no streaming activity or tool events received")

// activityWatchdog monitors turn progress to detect hanging network streams or deadlocks.
type activityWatchdog struct {
	mu      sync.Mutex
	timeout time.Duration
	timer   *time.Timer
	stopped bool
	stalled bool
	onStall func()
}

// newActivityWatchdog creates and starts a watchdog with the given timeout and stall callback.
func newActivityWatchdog(timeout time.Duration, onStall func()) *activityWatchdog {
	if timeout <= 0 {
		timeout = DefaultWatchdogTimeout
	}
	w := &activityWatchdog{
		timeout: timeout,
		onStall: onStall,
	}
	w.timer = time.AfterFunc(timeout, func() {
		w.mu.Lock()
		if w.stopped || w.stalled {
			w.mu.Unlock()
			return
		}
		w.stalled = true
		cb := w.onStall
		w.mu.Unlock()

		if cb != nil {
			cb()
		}
	})
	return w
}

// Ping refreshes the watchdog timer when activity is observed.
func (w *activityWatchdog) Ping() {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stopped || w.stalled {
		return
	}
	if w.timer != nil {
		w.timer.Reset(w.timeout)
	}
}

// Stop cleanly deactivates the watchdog.
func (w *activityWatchdog) Stop() {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.stopped = true
	if w.timer != nil {
		w.timer.Stop()
	}
}

// IsStalled reports whether the watchdog triggered a stall.
func (w *activityWatchdog) IsStalled() bool {
	if w == nil {
		return false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.stalled
}
