// Package remotecontrol — pending.go: the per-cascade
// registry of in-flight user interactions the JSON-RPC
// server surfaces to the dashboard.
//
// Why a separate file:
//
//   - The PendingInteractions type is independent of the
//     cascade lifecycle (cascade.go) and the streaming
//     surface (streaming.go): it owns a small map of
//     interaction_id → PendingInteraction, plus the
//     Resolve / Expire / Add methods. Adding it next to
//     cascade.go would have buried the type-specific
//     comments under hundreds of lines of cascade-lifecycle
//     prose; splitting it out keeps the doc-comment close
//     to the code it describes.
//
//   - Future cycles (rc-piagent-bind) will hook PiG's
//     tool-permission flow into this seam. Splitting
//     pending.go out now means the PiG-side patch is
//     localised to a single file (the lifecycle stays in
//     cascade.go; the streaming hook stays in streaming.go;
//     the registry stays here).
//
// Lifecycle of a pending interaction:
//
//  1. A streaming backend (StubBackend today;
//     PiG-backed in rc-piagent-bind) calls
//     sink.OnUserInteraction(cascade, intr) when a tool
//     needs the user's input.
//  2. The ConnNotifierSink emits a
//     "cascade/user_interaction_required" notification
//     carrying the prompt + a fresh interaction_id.
//  3. The dashboard calls
//     HandleRemoteAgentCascadeUserInteraction with the
//     interaction_id and a response; the handler resolves
//     the pending entry.
//  4. Resolve closes the ResultCh; the backend (whatever
//     has been blocked on it) wakes up.
//
// The registry is per-cascade (not per-dispatcher) because
// "an interaction belongs to the cascade that produced it"
// is the natural model: a cascade is the dashboard's
// session-scoped view of "my turn in progress", and a
// permission prompt is part of that turn. The cascade's
// *Pending pointer is allocated lazily on the first Add so
// freshly-started cascades carry no allocation overhead.
//
// Concurrency: PendingInteractions is guarded by a sync.Mutex
// (not RWMutex — every operation is short-lived and the map
// is small, so an RWMutex's read-fast / write-slow
// distinction doesn't pay back). nil-safe (every method is
// a no-op on a nil receiver so callers don't have to
// nil-check at the cascade level).
//
// Stdlib-only. No third-party deps.
package remotecontrol

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"
	"time"
)

// InteractionKind is the closed set of "what kind of
// permission / question the dashboard is being asked".
// Surfaced verbatim in the cascade/user_interaction_required
// notification's `kind` field; the dispatcher's handler does
// NOT validate the value (a future real backend may grow the
// set; today the only consumers are StubBackend + the
// dashboard). Empty string is treated as "permission" by
// dashboards that want a default.
const (
	InteractionKindPermission   = "permission"
	InteractionKindQuestion     = "question"
	InteractionKindConfirmation = "confirmation"
	InteractionKindChoice       = "choice"
)

// InteractionOption is one choice in a permission / question
// prompt. The dispatcher forwards these verbatim in the
// notification's `options` array (omitted when empty).
//
// Description is optional; dashboards that render a button
// row use it for tooltip text. Value is what the backend
// expects back as the interaction's Response when this
// option is chosen (Label is what the user sees).
type InteractionOption struct {
	Label       string `json:"label"`
	Value       string `json:"value"`
	Description string `json:"description,omitempty"`
}

// PendingInteraction is one in-flight user-interaction
// prompt. Constructed by AddInteraction; mutated by Resolve;
// garbage-collected by Expire.
//
// Fields are read by:
//
//   - ConnNotifierSink.OnUserInteraction — emits the
//     cascade/user_interaction_required notification.
//   - The dispatcher's
//     HandleRemoteAgentCascadeUserInteraction handler —
//     reads Intr.ID + Casc.ID to look up the entry, then
//     pushes a result onto ResultCh.
//   - The dispatcher / dispatcher's expiry ticker (parked
//     for rc-piagent-bind) — calls Expire to garbage-
//     collect stale entries.
//
// ResultCh is buffered (len 1) so the dashboard's RPC
// handler can complete without blocking on the backend's
// read; the backend's read may still block waiting for the
// dashboard, but that's the design intent (the backend is
// parked until the user answers). The 200ms StubBackend
// timeout pins the test-side failure path.
type PendingInteraction struct {
	ID        string              // "int_<12 hex chars>"; unique within a cascade
	Kind      string              // InteractionKind* constant
	Title     string              // human-readable summary (dashboards render in header)
	Prompt    string              // the question text
	Options   []InteractionOption `json:"options,omitempty"`
	Dangerous bool                // surfaced for permission kinds
	CreatedAt time.Time           // stamped at AddInteraction time
	ExpiresAt time.Time           // zero = never expires; non-zero = wall-clock deadline

	// ResultCh is the channel Resolve sends the InteractionResult on.
	// Buffered with len 1 so a slow / unresponsive caller doesn't wedge the
	// dispatcher's RPC handler — but a buggy caller that reads the channel
	// without sending still blocks the backend. Length 1 is what the brief
	// pins.
	ResultCh chan InteractionResult

	// cascade is the back-reference so Expire / Notify paths can
	// reconstruct the JSON-RPC envelope's cascade_id without
	// re-looking-up. Set by AddInteraction; never mutated.
	cascade *Cascade
}

// InteractionResult is what the dashboard's
// HandleRemoteAgentCascadeUserInteraction call pushes onto
// the PendingInteraction.ResultCh. The Response field is
// the raw JSON the dashboard sent (so the backend can
// decide what shape it expects: a string for "yes"/"no",
// an object for {choice_id, ...} or {approve, remember}).
// DecidedAt is the wall-clock the dispatcher stamped at
// Resolve time.
type InteractionResult struct {
	// Response is the raw JSON payload the dashboard sent
	// for the response. We keep it as json.RawMessage so
	// the backend can decode it into whatever shape its
	// tool-permission flow expects without going through
	// a Go-side string round-trip.
	Response json.RawMessage
	// DecidedAt is when the dispatcher accepted the
	// response. The notification envelope surfaces it
	// back to the dashboard.
	DecidedAt time.Time
}

// PendingInteractions is the per-cascade registry of
// pending user-interaction prompts. Maps interaction_id →
// *PendingInteraction; Add/Get/Resolve/Expire are the four
// methods the dispatcher and the sink call.
//
// The zero value is a usable empty registry; the cascade
// lazy-allocates one on the first Add (see
// (*Cascade).AddInteraction) so a freshly-started cascade
// has nil *PendingInteractions — callers must always go
// through the cascade-level accessor rather than reaching
// for a registry on a nil receiver.
//
// All methods are nil-safe; a nil receiver is a no-op so
// cascade-side accessors don't have to nil-check.
type PendingInteractions struct {
	mu sync.Mutex
	m  map[string]*PendingInteraction
}

// ErrInteractionNotFound is returned by Resolve when the
// interaction_id is unknown. The dispatcher's handler
// surfaces it as JSON-RPC -32006 "Interaction not found".
// Returned by the *PendingInteractions.Resolve method;
// ErrCascadeNotFound (-32004) is returned by the
// cascade-lookup step before this error is reachable.
var ErrInteractionNotFound = errors.New("remotecontrol: interaction not found")

// Add inserts intr into the registry, returning intr
// unchanged on success. intr.ID must be unique within this
// registry — Add does NOT detect a collision; a colliding
// ID silently overwrites the previous entry (mirroring
// (*CascadeRegistry).Register's contract). The ID is
// minted by (*Cascade).AddInteraction via crypto/rand so
// collisions within a cascade's lifetime are vanishingly
// unlikely.
//
// intr's ResultCh is left intact — Add does not allocate
// or replace it. intr's cascade back-reference is left
// intact — Add does not overwrite it.
//
// Add is nil-safe on a nil receiver (no-op).
func (p *PendingInteractions) Add(intr *PendingInteraction) {
	if p == nil || intr == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.m == nil {
		p.m = map[string]*PendingInteraction{}
	}
	p.m[intr.ID] = intr
}

// Get returns the PendingInteraction registered under id,
// or (nil, false) when no such interaction exists. nil-safe
// (nil receiver returns (nil, false)).
//
// Get is read-only; callers MUST NOT mutate the returned
// *PendingInteraction's fields. The only mutation a caller
// should do is receive on intr.ResultCh (which is a channel
// read, not a struct mutation).
func (p *PendingInteractions) Get(id string) (*PendingInteraction, bool) {
	if p == nil {
		return nil, false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	intr, ok := p.m[id]
	return intr, ok
}

// Resolve delivers result to intr.ResultCh (closing the
// question the dashboard's HandleRemoteAgentCascadeUserInteraction
// RPC just answered) and removes intr from the registry
// (one-shot semantics — a second Resolve returns
// ErrInteractionNotFound).
//
// Returns ErrInteractionNotFound when no interaction is
// registered under id; the caller maps this to JSON-RPC
// -32006. Returns nil on success.
//
// Resolve is nil-safe on a nil receiver (returns
// ErrInteractionNotFound — no entry to resolve on a nil
// map).
//
// Resolution is NOT cancellable from the dashboard side: a
// successful Resolve drops the entry and sends on
// ResultCh; a subsequent Resolve sees ErrInteractionNotFound.
// Concurrent resolves for the same id race — exactly one
// wins (the others see ErrInteractionNotFound) because the
// Remove runs under the same lock as the Get; the brief
// pins this with TestUserInteraction_ConcurrentResolves_NoDoubleAck.
func (p *PendingInteractions) Resolve(id string, result InteractionResult) error {
	if p == nil {
		return ErrInteractionNotFound
	}
	p.mu.Lock()
	intr, ok := p.m[id]
	if ok {
		delete(p.m, id)
	}
	p.mu.Unlock()
	if !ok {
		return ErrInteractionNotFound
	}
	// Send on the channel outside the lock so a slow
	// consumer (which is normal — the backend may be
	// parked on the channel) doesn't serialise other
	// resolutions. ResultCh is buffered (len 1) so the
	// send does not block.
	intr.ResultCh <- result
	return nil
}

// Expire removes every interaction whose ExpiresAt is in
// the past (or equal to now). Returns the list of expired
// IDs so a caller can log / audit / close the underlying
// ResultCh. The returned slice's leftover
// channel receive does NOT happen here — Expire leaves the
// channel open so the backend's parked goroutine sees the
// channel close (channel close is the "interaction
// expired" signal; ResultCh is closed when Expire drops
// the entry).
//
// Expire is nil-safe (no-op on a nil receiver; returns nil).
//
// Concurrency: Expire takes the per-registry mu; a Resolve
// racing Expire sees either "entry still there" (Resolve
// succeeds; Expire on next tick drops nothing for that id)
// or "entry already gone" (Resolve returns
// ErrInteractionNotFound — the same error path a stale
// caller would see). The brief pins this with
// TestUserInteraction_Expires.
func (p *PendingInteractions) Expire(now time.Time) []string {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.m == nil {
		return nil
	}
	var expired []string
	for id, intr := range p.m {
		if intr.ExpiresAt.IsZero() {
			continue
		}
		if !intr.ExpiresAt.After(now) {
			// close the channel so any blocked
			// receiver wakes up with the zero
			// value (an expired-decision-style
			// backend can distinguish "expired"
			// from "user answered no" because
			// the value is the zero
			// InteractionResult).
			close(intr.ResultCh)
			delete(p.m, id)
			expired = append(expired, id)
		}
	}
	return expired
}

// Len returns the number of pending interactions currently
// in this registry. nil-safe (0 on a nil receiver). Cheap;
// intended for tests and future-cycle's status tool.
func (p *PendingInteractions) Len() int {
	if p == nil {
		return 0
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.m)
}

// mintInteractionID allocates one "int_<12 hex chars>" id
// using crypto/rand. 12 hex chars is 6 bytes (48 bits) of
// entropy — well above what a per-cascade registry needs
// to stay collision-free in practice (the brief's
// permission prompts happen at human cadence, so a single
// cascade's lifetime sees at most a few dozen entries).
// The "int_" prefix matches the cascade id's "cas_" prefix
// so log lines can grep both without ambiguity.
func mintInteractionID() (string, error) {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "int_" + hex.EncodeToString(b[:]), nil
}
