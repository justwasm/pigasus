// Package remotetransport — tcp_origin.go: the OriginPolicy
// read/write/mutation surface and the 403 reject response
// helper that handleWS calls before the WS upgrade.
//
// This file owns:
//
//   - OriginDecision (the read-only verdict shape).
//   - (*TCPServer).handleOriginCheck (the per-Accept origin
//     gate).
//   - writeOriginForbidden (the HTTP 403 + JSON body writer
//     used by handleWS when handleOriginCheck returns
//     Allowed:false).
//   - (*TCPServer).SetOriginPolicy (the dispatcher mutation
//     seam — SetRemoteControlOriginPatterns calls it after a
//     successful store mutation so the next Accept sees the
//     new patterns).
//   - (*TCPServer).OriginPolicySnapshot (the per-Accept snapshot
//     reader; handleOriginCheck uses it).
//
// Every helper is ≤ 50 LOC.
package remotetransport

import (
	"fmt"
	"net/http"

	"github.com/justwasm/pigasus/internal/originpolicy"
)

// OriginDecision is the read-only verdict
// (*TCPServer).handleOriginCheck returns.
// Allowed:true falls through to the
// existing websocket.Accept path;
// Allowed:false is the 403 + JSON-body
// reject handleWS writes before bailing.
//
// Origin is the verbatim value of
// r.Header.Get("Origin") at the time of
// the check. An empty Origin means
// "non-browser caller"; the Policy's
// Allows method treats that as
// "permitted" by default and the
// Decision records Origin="" so a
// debug log can confirm.
//
// Reason is the human-readable verdict
// the wire envelope echoes in `reason`.
// Allowed=true → ""; Allowed=false →
// "origin X is not in the allowlist"
// (or similar). The field is the
// diagnostic-only partner of the
// decision bool; it does NOT drive the
// HTTP status (which is always 403 on
// allow=false).
type OriginDecision struct {
	Origin  string
	Allowed bool
	Reason  string
}

// handleOriginCheck consults the
// server's active OriginPolicy and
// returns the verdict for r.
//
// The check reads the policy under
// s.setOriginPolicyMu (a short read
// lock), trims + compares against
// r.Header.Get("Origin"). The HTTP
// listener's per-conn goroutine
// invokes this from inside handleWS —
// the lock is held only for the
// snapshot read, never across the
// WS upgrade or the dispatch loop.
//
// Logging policy: every Allow + Deny
// at debug level. Production wires
// slog.Default or stdlib log; the
// current cycle doesn't ship a logger
// bridge (the rc-* cycles have not
// picked one yet — the future
// rc-logging-standardisation cycle
// is parked) so today the helper is
// silent. A future cycle that wants
// per-decision telemetry can swap the
// log.Print for slog.Debug without
// changing the helper's signature.
//
// The function is nil-safe: a nil
// receiver returns Allowed=true (the
// pre-cycle "allow all" default).
//
// Cycle rc-auth-tokens.
func (s *TCPServer) handleOriginCheck(r *http.Request) OriginDecision {
	policy := s.OriginPolicySnapshot()
	origin := ""
	if r != nil {
		// r.Header.Get is case-insensitive
		// and returns "" when the header
		// is absent (the typical case for
		// curl/wscat smoke tests).
		origin = r.Header.Get("Origin")
	}
	if policy.Allows(origin) {
		return OriginDecision{Origin: origin, Allowed: true}
	}
	reason := "origin not in allowlist"
	if origin == "" {
		// Defensive: an empty Origin
		// header with non-empty
		// Patterns is conceptually
		// mismatched (Policy.Allows
		// returns true for empty
		// origins even with patterns
		// set). If we reach here
		// something has gone wrong
		// upstream; report it as
		// such.
		reason = "empty origin rejected by allowlist"
	} else {
		reason = "origin " + origin + " not in allowlist"
	}
	return OriginDecision{Origin: origin, Allowed: false, Reason: reason}
}

// writeOriginForbidden writes the HTTP 403 + JSON body
// handleWS returns when handleOriginCheck returns
// Allowed:false. Extracted from handleWS so the body of
// handleWS (in tcp_handlers.go) stays focused on the
// upgrade + dispatch path.
//
// We use stdlib's http.Header + WriteHeader + fmt.Fprintf
// rather than encoding/json so the response stays a single
// write call (the http.ResponseWriter pattern the library
// expects — a single Write followed by no further access).
// The verbatim JSON envelope is what the
// cycle report documents:
//
//	{"ok":false,"denied":true,"origin":"<verbatim>","reason":"<human-readable>"}
//
// (followed by a trailing newline to match the
// Unix-socket transport's per-line framing on the rare
// browser-side check path).
//
// Cycle rc-auth-tokens.
func writeOriginForbidden(w http.ResponseWriter, d OriginDecision) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusForbidden)
	_, _ = fmt.Fprintf(w, `{"ok":false,"denied":true,"origin":%q,"reason":%q}`+"\n",
		d.Origin, d.Reason)
}

// SetOriginPolicy atomically swaps the active
// OriginPatterns allowlist the WS upgrade consults.
// The supplied policy's slice is copied under the
// write lock so a subsequent mutation by the caller
// does not race an in-flight Accept. A nil policy
// (or a policy with nil Patterns / empty Patterns)
// restores the "allow all" pre-cycle semantic.
//
// Cycle rc-auth-tokens: the SetRemoteControlOriginPatterns
// dispatcher handler calls this after a successful
// store mutation so an immediately-following
// Accept sees the new patterns. The lock pattern
// (RUnlock/RUnlock/RLock) is identical to the
// broadcaster seam added in cycle
// rc-permissions-grants — the dispatcher's hot
// path is one short critical section per Accept.
//
// Safe to call from any goroutine. Idempotent
// (concurrent calls serialise under the write
// lock). nil receiver is a no-op (the typical
// "host didn't wire a server" pattern; the
// Unix-socket transport doesn't have one).
func (s *TCPServer) SetOriginPolicy(policy originpolicy.OriginPolicy) {
	if s == nil {
		return
	}
	s.setOriginPolicyMu.Lock()
	defer s.setOriginPolicyMu.Unlock()
	if len(policy.Patterns) == 0 {
		s.originPolicy = nil
		return
	}
	pcopy := policy
	// Defensive copy of the patterns slice so a
	// caller that mutates its slice after the call
	// doesn't race an in-flight Allows() read.
	if policy.Patterns != nil {
		pcopy.Patterns = append([]string(nil), policy.Patterns...)
	}
	s.originPolicy = &pcopy
}

// OriginPolicySnapshot returns a defensive copy
// of the active OriginPolicy. The receiver may be
// nil; a nil receiver returns the zero value (an
// OriginPolicy with no patterns — equivalent to
// "allow all"). The dispatcher's hot-path reader
// uses this — see (*TCPServer).handleOriginCheck.
//
// Cycle rc-auth-tokens.
func (s *TCPServer) OriginPolicySnapshot() originpolicy.OriginPolicy {
	if s == nil {
		return originpolicy.OriginPolicy{}
	}
	s.setOriginPolicyMu.RLock()
	defer s.setOriginPolicyMu.RUnlock()
	if s.originPolicy == nil {
		return originpolicy.OriginPolicy{}
	}
	out := *s.originPolicy
	if s.originPolicy.Patterns != nil {
		out.Patterns = append([]string(nil), s.originPolicy.Patterns...)
	}
	return out
}
