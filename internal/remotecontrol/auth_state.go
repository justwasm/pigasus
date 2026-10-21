// Package remotecontrol — auth_state.go: the mutator the
// JSON-RPC surface uses to bind + read the dashboard's
// authenticated session.
//
// Cycle rc-status-tool wires the auth block the rich-status
// tool surfaces. The block answers the dashboard's "who am
// I talking to, and for how long?" question with three
// fields: which auth method produced this session
// ("google" | "shim" | "adc" | "none"), the opaque session
// id the dashboard presented, and the expiry. The method
// SetRemoteControlSession is the RPC that mutates the
// state; the rich `status` tool reads the snapshot
// verbatim.
//
// Concurrency: writes are infrequent (a dashboard calls
// SetRemoteControlSession once at connect time, maybe again
// on token refresh); reads happen on every `status` poll.
// The state is guarded by RWMutex so a heavy stream of
// readers doesn't serialise behind a single writer. An
// atomic pointer to AuthSnapshot was an alternative, but
// RWMutex is simpler and the contention surface is small
// (one writer per session refresh, not per turn).
//
// Stdlib-only. No third-party deps.
package remotecontrol

import (
	"sync"
	"time"
)

// AuthMethod is the closed set of auth methods the host
// supports. The slice is exposed so a dashboard can render
// a known vocabulary without parsing free-form strings —
// today's set is "google" / "shim" / "adc" / "none"; a
// future rc-auth-tokens cycle may add oauth/google-impersonation
// entries.
const (
	AuthMethodGoogle = "google"
	AuthMethodShim   = "shim"
	AuthMethodADC    = "adc"
	AuthMethodNone   = "none"
)

// AuthSnapshot is the read-only view a dashboard (or the
// rich-status tool) sees. The fields map 1:1 to the JSON
// block the `status` tool emits; the dispatcher marshals
// the snapshot verbatim so the wire envelope is defined by
// this struct.
//
// Method is one of the AuthMethod* constants; empty when
// the host hasn't called Set(...). SessionID is the opaque
// id the dashboard presented in its SetRemoteControlSession
// call (or the host's pre-populated value at Start time).
// UserID is the dashboard-supplied identifier (e.g. an
// email-shaped string) — empty when the dashboard omits it.
// Scopes is a slice of permission tags ("read" / "write" /
// "admin") the dashboard asserted; nil → [] on the wire so
// JSON encoding doesn't emit `null`. ExpiresAt is the
// absolute deadline after which the session id is no longer
// trusted — zero time → emit `null` on the wire.
type AuthSnapshot struct {
	// Method is "google" | "shim" | "adc" | "none" or "".
	Method string
	// SessionID is the opaque session id; "" when unset.
	SessionID string
	// UserID is the dashboard-supplied user identifier; "" when unset.
	UserID string
	// Scopes is the per-session permission tags; nil → [] on the wire.
	Scopes []string
	// ExpiresAt is the absolute RFC3339 expiry; zero time → null on the wire.
	ExpiresAt time.Time
}

// AuthState is the threadsafe state-holder the dispatcher
// reads from and SetRemoteControlSession writes to. Construct
// one with NewAuthState (or use the zero value via
// &AuthState{} — the embedded sync.RWMutex is usable from
// the zero value). Place on Dispatcher.Config.AuthState so
// the dispatcher's rich-status path can call
// a.Snapshot() under the lock.
//
// Lifecycle: callers typically wire one AuthState per
// dispatcher; a re-Start of the daemon (cycle
// rc-persistent-daemon's launchd surface) creates a fresh
// AuthState because a fresh SessionState is the bridge.
// SetRemoteControlSession's RPC handler updates the fields
// in place; Set produces the same effect for callers that
// don't want to expose the Set method via RPC (the
// foreground daemon populates the state at Start time so
// the very first `status` call already sees the host's
// resolved auth method).
type AuthState struct {
	mu sync.RWMutex

	// Method is one of the AuthMethod* constants (see
	// above); empty when the host hasn't resolved the
	// dashboard's auth yet.
	Method string

	// SessionID is the opaque id the dashboard
	// presented; populated at SetRemoteControlSession
	// time. Independent of the *Cascade id (which is a
	// per-conversation routing handle; the session id
	// spans all cascades a dashboard opens on a single
	// dispatcher).
	SessionID string

	// UserID is the dashboard-supplied identifier for
	// the signed-in user (an email-shaped string, or
	// ""); nil-safe.
	UserID string

	// Scopes is a slice of permission tags; nil when
	// the dashboard hasn't asserted any. Snapshot
	// copies the slice so a Set-after-Snapshot
	// doesn't mutate the snapshot's underlying array.
	Scopes []string

	// ExpiresAt is the absolute wall-clock deadline
	// after which the session id is no longer
	// trusted. Zero time means "no expiry" — the
	// dashboard's token was issued with no
	// time-bound, or the host doesn't track one.
	ExpiresAt time.Time
}

// NewAuthState returns a fresh AuthState initialised with
// method/scopes from a single Set call. Convenience
// constructor for tests + the cmd/wagyu wiring.
func NewAuthState(method, sessionID, userID string, scopes []string, expiresAt time.Time) *AuthState {
	a := &AuthState{}
	a.Set(method, sessionID, userID, scopes, expiresAt)
	return a
}

// Snapshot returns a value-type view of the current
// AuthState safe to marshal / share. The Scopes slice is
// copied so a concurrent Set's append doesn't grow the
// snapshot's underlying array. Nil-safe (a nil receiver
// returns the zero AuthSnapshot so the dispatcher never
// has to nil-check).
func (a *AuthState) Snapshot() AuthSnapshot {
	if a == nil {
		return AuthSnapshot{}
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	scopes := a.Scopes
	if scopes == nil {
		// Normalise to a non-nil slice so JSON marshalling
		// emits `[]` rather than `null` on the wire —
		// dashboards iterate without a nil check.
		scopes = []string{}
	} else {
		// Defensive copy: the caller's setter might
		// later do `scopes = append(scopes, …)`, and
		// append may reuse the backing array if the
		// capacity is large enough to absorb the new
		// entry. The snapshot's caller (rich-status)
		// holds it only long enough to marshal, so the
		// cost is negligible.
		cp := make([]string, len(scopes))
		copy(cp, scopes)
		scopes = cp
	}
	return AuthSnapshot{
		Method:    a.Method,
		SessionID: a.SessionID,
		UserID:    a.UserID,
		Scopes:    scopes,
		ExpiresAt: a.ExpiresAt,
	}
}

// Set replaces the entire AuthState in one write. The
// slice argument is taken verbatim (no copy) — the caller
// (host wiring or the RPC handler) is responsible for the
// backing array. Nil-safe: a nil receiver is a no-op so a
// dispatcher that didn't install an AuthState doesn't
// crash when the SetRemoteControlSession handler runs.
func (a *AuthState) Set(method, sessionID, userID string, scopes []string, expiresAt time.Time) {
	if a == nil {
		return
	}
	a.mu.Lock()
	a.Method = method
	a.SessionID = sessionID
	a.UserID = userID
	a.Scopes = scopes
	a.ExpiresAt = expiresAt
	a.mu.Unlock()
}
