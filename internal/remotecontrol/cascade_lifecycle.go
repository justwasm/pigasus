// Package remotecontrol — cascade_lifecycle.go: the cascade's
// per-turn lifecycle hooks (Cancel / StartCancel / StopCancel /
// IsCancelled / Focus / IsFocused / SetFastpush /
// IsFastpushEnabled). Pure file split; every method here was
// previously inline in cascade.go.
package remotecontrol

import (
	"context"
	"time"
)

// StartCancel derives a child ctx from parent with a per-turn
// cancel func, stores the func on the cascade, and returns the
// derived ctx. (*CascadeRegistry).SendStream calls this before
// driving the backend; the returned ctx is what the backend's
// SendStream receives so that (*Cascade).Cancel can interrupt
// the turn in flight.
//
// Behaviour:
//
//   - Always returns a non-nil ctx; the caller always has a
//     cancelable ctx to thread through.
//   - If a previous per-turn cancel func is still on the
//     cascade (caller forgot to StopCancel), the previous one
//     is fired so the cascade is at most one-active-cancel at
//     any time.
//   - The cascade's cancelled flag is cleared: a new turn is not
//     a cancelled turn. The flag decides how a *failed* turn is
//     reported (see mapStreamError), and a turn that failed
//     after an earlier turn was cancelled has to report its own
//     failure.
//
// Nil-safe: a nil *Cascade returns parent verbatim.
func (c *Cascade) StartCancel(parent context.Context) context.Context {
	if c == nil {
		return parent
	}
	if parent == nil {
		parent = context.Background()
	}
	c.cancelMu.Lock()
	defer c.cancelMu.Unlock()
	if c.cancel != nil {
		// Defensive cleanup of a leaked prior cancel
		// func — fire it so the cancel call doesn't get
		// lost. Production callers always defer
		// StopCancel so this is unreachable; tests can
		// land here if they forget.
		c.cancel()
		c.cancel = nil
	}
	ctx, cancel := context.WithCancel(parent)
	c.cancel = cancel
	c.cancelReason = ""
	c.cancelled.Store(false)
	return ctx
}

// StopCancel clears the per-turn cancel func (the cascade is
// no longer driving a turn). Idempotent — a second call is a
// no-op. registry.SendStream calls this in a defer after the
// backend returns so the next StartCancel can install a fresh
// cancel func without firing the previous one.
//
// StopCancel always fires the stored cancel func so a caller
// that exits SendStream early (e.g. via a panic recovery)
// still tears down the cancel. The cascade's cancelled flag
// is left untouched — StopCancel is the turn-end signal, not
// the cancel-firing signal.
//
// Nil-safe.
func (c *Cascade) StopCancel() {
	if c == nil {
		return
	}
	c.cancelMu.Lock()
	defer c.cancelMu.Unlock()
	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}
}

// Cancel fires the per-turn cancel func (if one is in flight)
// and flips the cascade's atomic cancelled flag. Returns true
// iff a cancel was actually fired — i.e. there was a non-nil
// cancel func to call. Idempotent: a second call after the
// first returns false even though the cancelled flag is
// already set.
//
// The flag is per turn: the next turn's StartCancel clears it
// (see StartCancel).
//
// The optional `reason` argument is the human-readable text
// the cancel was requested with. It is captured on the
// cascade so the cancel notification and the error-ring entry
// can surface it verbatim.
//
// Nil-safe: a nil *Cascade returns false.
func (c *Cascade) Cancel(reason string) bool {
	if c == nil {
		return false
	}
	c.cancelMu.Lock()
	cancel := c.cancel
	c.cancel = nil
	if reason != "" {
		c.cancelReason = reason
	}
	c.cancelMu.Unlock()
	c.cancelled.Store(true)
	if cancel == nil {
		return false
	}
	cancel()
	return true
}

// IsCancelled reports whether Cancel has been called on this
// cascade for the turn in flight. The accessor is lock-free
// (atomic.Bool.Load) so the dispatcher's status-tool poll path
// stays cheap.
//
// The flag is cleared when the next turn starts (see StartCancel).
//
// Nil-safe: a nil *Cascade returns false.
func (c *Cascade) IsCancelled() bool {
	if c == nil {
		return false
	}
	return c.cancelled.Load()
}

// CancelReason returns the human-readable reason Cancel was
// called with. Defaults to "user requested" when the cascade
// was cancelled without a reason (or never explicitly
// cancelled but IsCancelled is true — which shouldn't happen,
// but the lazy default keeps the notification envelope
// well-formed).
//
// Nil-safe: a nil *Cascade returns "".
func (c *Cascade) CancelReason() string {
	if c == nil {
		return ""
	}
	c.cancelMu.Lock()
	defer c.cancelMu.Unlock()
	if c.cancelReason == "" {
		return "user requested"
	}
	return c.cancelReason
}

// Focus marks the cascade as the dashboard's currently-focused
// conversation. The dispatcher's remoteControlDeepLink handler
// calls this on a focus-cascade verb so subsequent status
// polls reflect the new focus. The cascade's focused flag is
// monotonic (Focus only flips false → true); a future cycle
// that needs to clear it can add an Unfocus method or flip
// the field directly. The cycle-rc-deep-link brief pins the
// read+write surface as Focus / IsFocused.
//
// Nil-safe: a nil *Cascade is a no-op.
func (c *Cascade) Focus() {
	if c == nil {
		return
	}
	c.focused.Store(true)
}

// IsFocused reports whether Focus has been called on this
// cascade. The accessor is lock-free (atomic.Bool.Load) so a
// dashboard's status poll stays cheap. Returns false for a
// never-focused cascade (the default) and for a nil
// *Cascade.
//
// Cycle rc-deep-link adds this.
func (c *Cascade) IsFocused() bool {
	if c == nil {
		return false
	}
	return c.focused.Load()
}

// SetFastpush flips the cascade's per-cascade fast-push flag
// to the supplied value. The dispatcher's
// SetRemoteAgentFastpushEnabled handler calls this AFTER the
// FastpushStore.SetEnabled mutation lands, so the in-memory
// flag and the on-disk row stay in lockstep.
//
// atomic.Bool is the same lock-free shape the focused flag
// uses — the future rc-fastpush-channel cycle's delivery
// path reads it via IsFastpushEnabled() on every mid-turn
// notification, and a lock-free read is what the brief's
// "high-frequency poll" rule calls for.
//
// Nil-safe: a nil *Cascade is a no-op.
func (c *Cascade) SetFastpush(enabled bool) {
	if c == nil {
		return
	}
	c.fastpush.Store(enabled)
}

// IsFastpushEnabled reports whether SetFastpush has flipped
// the cascade's fast-push flag to true. The accessor is
// lock-free (atomic.Bool.Load) so the future
// rc-fastpush-channel cycle's mid-turn delivery hot-path stays
// cheap. Returns false for a never-toggled cascade (the
// default) and for a nil *Cascade.
//
// Cycle rc-fastpush-toggle adds this. The flag is
// intentionally orthogonal to Focused / Cancelled — the three
// booleans each cover one axis of the cascade lifecycle.
func (c *Cascade) IsFastpushEnabled() bool {
	if c == nil {
		return false
	}
	return c.fastpush.Load()
}

// terminalOutputCapacity is the buffered-chann capacity the
// cycle briefs pin on (*Cascade).TerminalOutput(). A slow
// terminal subscriber can't pin the streaming goroutine while
// the buffer has room; 32 is a reasonable upper bound on a
// single-turn chunk count.
const terminalOutputCapacity = 32

// TerminalOutput returns the cascade's text-delta chan,
// lazy-allocating it on first call. The chan is buffered
// with capacity terminalOutputCapacity (32). Future
// rc-pty-bridge cycle writes to this chan from a real pty
// driver; today's StubBackend / PiG streaming path doesn't
// actually write here, but the seam exists so future cycles
// don't have to mutate the Cascade struct again.
//
// Nil-safe: a nil *Cascade returns nil. A non-nil cascade
// that hasn't yet had a chunk stream allocates on first
// call.
func (c *Cascade) TerminalOutput() <-chan []byte {
	if c == nil {
		return nil
	}
	c.terminalOutputOnce.Do(func() {
		c.terminalOutput = make(chan []byte, terminalOutputCapacity)
	})
	return c.terminalOutput
}

// AppendTerminalOutput writes a chunk to the cascade's
// TerminalOutput chan. Future rc-pty-bridge cycle calls this
// from a real pty driver; today's per-cycle test wires a
// custom streaming backend. The drop-when-full rule is
// identical to (*TerminalsRegistry).Send's: a full chan (no
// reader waiting) drops the chunk and silently returns; the
// future backpressure cycle surfaces drop metrics.
//
// Nil-safe: a nil *Cascade is a no-op. Calling this before
// TerminalOutput has been called by some reader allocates
// the chan first.
func (c *Cascade) AppendTerminalOutput(chunk []byte) {
	if c == nil {
		return
	}
	c.terminalOutputOnce.Do(func() {
		c.terminalOutput = make(chan []byte, terminalOutputCapacity)
	})
	select {
	case c.terminalOutput <- append([]byte(nil), chunk...):
	default:
		// Drop on full.
	}
}

// recordReply is the per-cascade state-mutation hook
// (*CascadeRegistry).Send calls on a successful turn.
// Updates ConversationID only when newConvID is non-empty
// (an empty result.NewConversationID means "the backend
// reused the existing conversation; don't change anything").
// LastReplyAt is always stamped, so a dashboard can probe
// idle / liveness by reading the registry.
//
// T is taken from a passed-in timestamp instead of
// time.Now() so tests can pin the recorded value without
// sleeping. Nil-safe: a nil *Cascade is a no-op.
func (c *Cascade) recordReply(newConvID string, t time.Time) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if newConvID != "" {
		c.ConversationID = newConvID
	}
	c.LastReplyAt = t
}
