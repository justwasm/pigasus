// Package remotecontrol — cascade.go: the *Cascade struct's
// immediate getters + simple mutators (Pending, AddInteraction,
// NewCascade, Backend, SetBackend, IsClosed, MarkClosed,
// ErrorsSnapshot, recordError, recordCancel) and the
// mintCascadeID helper. See cascade_doc.go for the full
// per-cycle package history; cascade_types.go owns the *Cascade
// struct + CascadeClientInfo + CascadeOpts + CascadeError +
// MaxCascadeErrors, cascade_lifecycle.go owns the per-turn
// cancel / focus / fastpush hooks + the TerminalOutput chan,
// cascade_io.go owns the Send / SendStream pipeline +
// reply-state helpers, and cascade_registry.go owns the
// *CascadeRegistry type and its accessors.
//
// Stdlib-only. No third-party deps.
package remotecontrol

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"
)

// Pending returns the cascade's pending-interaction registry,
// allocating one on first access. nil-safe: a nil *Cascade
// returns nil (matching the accessor's "no panic" discipline).
//
// Tests and the rc-handle-user-interaction cycle's
// dispatcher's handler call this to look up an interaction by
// id.
func (c *Cascade) Pending() *PendingInteractions {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pending == nil {
		c.pending = &PendingInteractions{}
	}
	return c.pending
}

// AddInteraction mints a fresh PendingInteraction with the given
// prompt metadata, registers it on the cascade's pending
// registry, and returns the interaction. The returned
// *PendingInteraction's ID is "int_<12 hex chars>" — opaque to
// dashboards except as a routing handle.
//
// `now` is the wall-clock stamp; production callers leave it
// zero and AddInteraction uses time.Now().UTC(). Tests pin a
// custom stamp for deterministic CreatedAt / ExpiresAt
// comparisons.
//
// `expiresAt` is the absolute deadline after which the
// interaction is auto-expired by the dispatcher's expiry tick.
// Zero means "never expires".
//
// AddInteraction is nil-safe: a nil *Cascade returns (nil,
// errors.New(...)) so the caller's happy-path branching stays
// simple.
func (c *Cascade) AddInteraction(kind, title, prompt string, opts []InteractionOption, dangerous bool, expiresAt time.Time, now time.Time) (*PendingInteraction, error) {
	if c == nil {
		return nil, errors.New("remotecontrol: AddInteraction on nil cascade")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	id, err := mintInteractionID()
	if err != nil {
		return nil, err
	}
	intr := &PendingInteraction{
		ID:        id,
		Kind:      kind,
		Title:     title,
		Prompt:    prompt,
		Options:   opts,
		Dangerous: dangerous,
		CreatedAt: now,
		ExpiresAt: expiresAt,
		ResultCh:  make(chan InteractionResult, 1),
		cascade:   c,
	}
	c.Pending().Add(intr)
	return intr, nil
}

// NewCascade mints a *Cascade with a fresh opaquely-random ID
// and CreatedAt pinned to opts.Now (or time.Now().UTC()). The
// returned *Cascade is NOT registered — call Registry.Register
// to make it discoverable. Splitting construction from
// registration lets tests build a cascade and assert it before
// inserting; it also lets the dispatcher stamp a registry error
// (e.g. ErrRegistryFull) without leaking the partially-built
// value into the registry.
//
// Cycle rc-cascade-message: NewCascade also seeds the backend
// field with a fresh *StubBackend.
//
// Cycle rc-piagent-bind: NewCascade additionally accepts a
// dispatcher-supplied default (CascadeOpts.DispatchDefault).
func NewCascade(opts CascadeOpts) (*Cascade, error) {
	id, err := mintCascadeID()
	if err != nil {
		return nil, err
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return &Cascade{
		ID:              id,
		CreatedAt:       now,
		Model:           opts.Model,
		BackendLabel:    opts.BackendLabel,
		Agent:           opts.Agent,
		ClientInfo:      opts.ClientInfo,
		Workspace:       opts.Workspace,
		Effort:          opts.Effort,
		Sandbox:         opts.Sandbox,
		Scaffold:        opts.Scaffold,
		SessionMode:     opts.SessionMode,
		ProjectID:       opts.ProjectID,
		backend:         &StubBackend{},
		dispatchDefault: opts.DispatchDefault,
	}, nil
}

// Backend returns the MessageBackend this cascade's turns route
// through. The accessor is nil-safe: a nil *Cascade returns nil;
// a *Cascade whose backend has not been explicitly installed
// returns the constructor-seeded *StubBackend.
//
// Cycle rc-piagent-bind: when dispatchDefault is non-nil AND
// c.backend is still the *StubBackend seeded by NewCascade, the
// accessor swaps in dispatchDefault on first call and returns
// it.
func (c *Cascade) Backend() MessageBackend {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.backend == nil {
		// Defensive: NewCascade seeds this; only a
		// nil-backend cascade (or one whose SetBackend
		// was called with nil) lands here.
		c.backend = &StubBackend{}
		return c.backend
	}
	// Lazy init: when the cascade is on the
	// constructor-seeded *StubBackend AND a dispatcher
	// default is in scope AND no SetBackend call has
	// run yet, swap once.
	if !c.backendExchanged && c.dispatchDefault != nil {
		if _, isStub := c.backend.(*StubBackend); isStub {
			c.backend = c.dispatchDefault
		}
	}
	return c.backend
}

// SetBackend installs the MessageBackend this cascade will route
// to.
//
// Cycle rc-piagent-bind: SetBackend now records the exchange in
// `backendExchanged`, so the lazy Backend() accessor stops
// trying to install dispatchDefault once a caller has installed
// its own.
//
// SetBackend is nil-safe: a nil *Cascade is a no-op.
func (c *Cascade) SetBackend(b MessageBackend) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.backend = b
	c.backendExchanged = true
	c.mu.Unlock()
}

// IsClosed reports whether MarkClosed has been called on this
// cascade. The accessor takes the per-cascade mu so a
// concurrent MarkClosed / SetClosed call is race-clean under
// -race. Nil-safe: a nil *Cascade returns false.
func (c *Cascade) IsClosed() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.Closed
}

// MarkClosed sets the closed sentinel. Once marked closed, a
// subsequent registry.Send returns ErrCascadeClosed.
//
// Nil-safe.
func (c *Cascade) MarkClosed() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.Closed = true
	c.mu.Unlock()
}

// ErrorsSnapshot returns a defensive copy of the cascade's
// error ring. Nil-safe (a nil *Cascade or a cascade that hasn't
// recorded any errors returns an empty slice). The slice is
// re-sliced to the live length so JSON marshalling emits `[]`
// rather than `null` on the wire.
func (c *Cascade) ErrorsSnapshot() []CascadeError {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.errors) == 0 {
		return nil
	}
	out := make([]CascadeError, len(c.errors))
	copy(out, c.errors)
	return out
}

// recordError appends one entry to the cascade's error ring,
// dropping the oldest when the cap is reached. At is the
// wall-clock stamp the caller supplies; Method is the failing
// RPC name; err is the wrapped error whose text becomes the
// entry's Message; code is the JSON-RPC envelope code the
// dispatcher mapped the error to.
//
// Nil-safe: a nil *Cascade or nil err is a no-op.
func (c *Cascade) recordError(method string, err error, code int, at time.Time) {
	if c == nil || err == nil {
		return
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	entry := CascadeError{
		At:      at,
		Method:  method,
		Message: err.Error(),
		Code:    code,
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.errors) >= MaxCascadeErrors {
		// Drop the oldest. Allocation-free: the
		// replacement doesn't grow capacity because
		// the cap is fixed.
		copy(c.errors, c.errors[1:])
		c.errors = c.errors[:len(c.errors)-1]
	}
	c.errors = append(c.errors, entry)
}

// recordCancel appends a Cancel-sourced entry to the cascade's
// error ring so RemoteAgentErrorStatus surfaces the
// cancellation as the most-recent error. reason is the
// human-readable text the cancel was requested with; at is
// the wall-clock stamp (zero defaults to time.Now().UTC()).
//
// The Method is hard-coded to "CancelRemoteAgentCascade" so a
// future dashboard filter can route cancel errors to a
// distinct UI surface. The Code is -32603 (Internal error)
// which matches the cancel handler's envelope.
//
// Nil-safe: a nil *Cascade is a no-op. An empty reason falls
// back to "user requested".
func (c *Cascade) recordCancel(reason string, at time.Time) {
	if c == nil {
		return
	}
	if reason == "" {
		reason = "user requested"
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	c.recordError("CancelRemoteAgentCascade", errors.New(reason), -32603, at)
}

// mintCascadeID allocates one "cas_<16 hex chars>" id using
// crypto/rand. The id body is 8 random bytes (64 bits of
// entropy), hex-encoded into 16 chars.
func mintCascadeID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "cas_" + hex.EncodeToString(b[:]), nil
}
