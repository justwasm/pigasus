// Package origins — dispatcher.go: the
// per-method JSON-RPC handler for the
// single SetRemoteControlOriginPatterns
// method introduced in cycle
// rc-auth-tokens.
//
// Why a dedicated file (not another switch
// arm in the dispatcher's route table): the
// handler shares the Store seam + a small
// sentinel-error map (per-pattern
// validation → -32602 "Invalid params",
// reusing the wire's own rpc.InvalidParams
// helper; persistence failure
// → -32603 "Internal error" via
// mapError), and the mutation
// needs to emit an origin_policy/changed
// broadcast notification. Putting the
// handler here, next to the type
// definitions + store (store.go),
// keeps the per-method body focused on the
// JSON-RPC envelope shape rather than the
// dispatcher's switch table.
//
// Sentinel map:
//
//	per-pattern validation failure → -32602 "Invalid params"
//	empty / non-array patterns     → -32602 "Invalid params"
//	(other / I/O)                  → -32603 "Internal error"
//
// Cycle rc-auth-tokens also adds the
// "broadcast on change" path: every
// successful SetPatterns emits an
// `origin_policy/changed` notification to
// every open ws connection so a dashboard
// that subscribed to the channel sees the
// new state. The broadcast uses the
// Seams.Broadcast callback the host
// installs — the handler here doesn't have
// to know which transport is registered, it
// just calls Broadcast and the registered
// broadcaster fans the frame out.
// In-process callers without a transport
// fall back to a no-op broadcast (the
// mutation still lands in the store; the
// SetRemoteControlOriginPatterns handler
// returns the post-mutation state in the
// response envelope so polling is always
// possible).
//
// Stdlib-only. No third-party deps.
package origins

import (
	"context"
	"encoding/json"
	"time"

	"github.com/justwasm/pigasus/internal/remotecontrol/rpc"
	"github.com/justwasm/pigasus/originpolicy"
)

// NotifChangedMethod is the
// JSON-RPC notification method the
// SetRemoteControlOriginPatterns handler
// fans out to every open ws connection on
// a successful mutation. Dashboards can
// register a sink for this method via the
// future rc-all-sinks-fanout cycle's
// RemoteControlOnOutput handler.
//
// Cycle rc-auth-tokens.
const NotifChangedMethod = "origin_policy/changed"

// setPatternsParams is
// the JSON shape the dashboard posts for
// SetRemoteControlOriginPatterns. Patterns
// is the allowlist (required, array of
// strings). An empty array explicitly turns
// the allowlist off (the brief's "explicit
// patterns=[] ⇒ allow all" semantic).
//
// Each entry is validated by
// originpolicy.ValidateOriginPattern —
// malformed entries are rejected and the
// dispatcher echoes them in the
// `rejected` field on the response.
// Successful entries land in the store.
type setPatternsParams struct {
	Patterns []string `json:"patterns"`
}

// HandleSetPatterns is
// the JSON-RPC handler for
// "SetRemoteControlOriginPatterns". Cycle
// rc-auth-tokens.
//
// Param validation:
//
//   - patterns: required, JSON array of
//     strings. Missing / non-array /
//     non-string entries → -32602 (the
//     "dashboard bug" envelope). The
//     store's per-pattern defence-in-depth
//     validator catches whitespace-only
//     and malformed-URL entries here
//     too.
//
// Store call:
//
//   - s.Store == nil → -32603
//     (the nil-fabrication in NewDispatcher
//     means this only trips a dispatcher
//     that explicitly cleared the field
//     post-construction).
//   - Per-pattern validation failure →
//     patterns land in `rejected`, valid
//     patterns land in `patterns`.
//     Validation runs before the lock so
//     a 0-valid + N-rejected mutation
//     doesn't touch the on-disk file.
//   - errPersistenceFailed →
//     -32603 "Internal error".
//   - other → -32603.
//
// On success the handler:
//   - emits an origin_policy/changed
//     notification to every open ws
//     connection so a dashboard watching
//     the channel re-renders without
//     polling;
//   - invokes the
//     Seams.SetPolicy callback the host
//     registered via
//     (*Dispatcher).SetOriginPolicySetter
//     so the WS transport's active
//     policy mirrors the on-disk one
//     (an immediately-following Accept
//     sees the new patterns);
//   - returns the brief's lean envelope
//     `{ok:true, applied_count, rejected,
//     updated_at}` shape — subscribers
//     get the same shape via the broadcast
//     notification.
//
// The response envelope always carries
// `ok:true` on the happy path; the
// `applied_count` field is the count of
// patterns that landed in the store; the
// `rejected` slice is the verbatim list
// of patterns the validator refused, so a
// dashboard's UI can show the operator
// which entries failed; `updated_at` is
// the RFC3339Nano stamp the store bumped.
func HandleSetPatterns(s Seams, params json.RawMessage) (any, *rpc.ErrorBody) {
	if s.Store == nil {
		return nil, rpc.InternalError("no origin-policy store installed")
	}
	p, perr := decodePatternsParams(params)
	if perr != nil {
		return nil, perr
	}

	// Store mutation: validate each pattern,
	// persist the accepted ones (sorted
	// alphabetically), bump UpdatedAt. The
	// store stamps UpdatedAt so a dashboard
	// that posts a pre-populated value gets
	// it overwritten with the server's
	// clock — matches the
	// rc-fastpush-toggle / rc-resources
	// server-side stamping precedent.
	state, rejected, mutated, err := s.Store.SetPatterns(nil, p)
	if err != nil {
		return nil, mapError(err, "set origin patterns")
	}

	// Reject-all: the store did not mutate
	// (preserve-state-on-reject-all semantic)
	// — skip the setter + broadcast and
	// return the response echoing the
	// rejected list verbatim. A successful
	// Set is one where at least one pattern
	// landed in the store; the rejected-only
	// path is a dashboard-bug submission and
	// doesn't earn a transport mirror or a
	// cross-conn notification.
	if !mutated {
		return rejectAllWire(state.UpdatedAt, rejected), nil
	}
	// Push the new policy into the
	// transport's active snapshot, broadcast
	// the changed-notification, and assemble
	// the response envelope — all in one
	// helper so the handler body stays under
	// the 50-fn ceiling that cycle
	// rc-filesize-internal-remotecontrol
	// enforces.
	return applyMutation(s, state, rejected), nil
}

// broadcastChanged fans an
// origin_policy/changed notification out to
// every registered transport's open
// connection.
//
// Why a dispatcher-side helper rather than
// a transport-side one: the single origin
// handler lives on the dispatcher, and the
// broadcaster (SetBroadcaster registered
// it) is the seam a transport installs to
// deliver "frame to every open conn"
// without the dispatcher knowing the
// concrete transport type.
//
// SetBroadcaster is the seam documented in
// notify.go. The remotetransport
// package wires its own broadcaster at
// TCPServer construction. Without a
// registered broadcaster the method is a
// no-op — in-process dispatchers and the
// Unix-socket transport have no notion of
// "every open connection". The mutation
// still landed in the store and the
// SetRemoteControlOriginPatterns handler
// invoked the registered
// originPolicySetter; the response envelope
// echoes the post-state, so polling is
// always possible.
func broadcastChanged(s Seams, state State) {
	if s.Broadcast == nil {
		return
	}
	params := stateToWire(state, nil)
	s.Broadcast(context.Background(), NotifChangedMethod, params)
}

// applyMutation performs the post-store-
// mutation side effects: pushes the new policy into
// the registered transport setter (nil-safe — the
// Unix-socket transport has no setter), broadcasts a
// `origin_policy/changed` notification, and returns
// the response envelope. Extracted from
// HandleSetPatterns to keep the
// handler body under the 50-fn ceiling that cycle
// rc-filesize-internal-remotecontrol enforces.
func applyMutation(s Seams, state State, rejected []string) map[string]any {
	// Push the new policy into the WS
	// transport's active snapshot (the
	// brief's "every successful Set must
	// mirror into the transport" guarantee).
	if s.SetPolicy != nil {
		s.SetPolicy(originpolicy.OriginPolicy{
			Patterns:  append([]string(nil), state.Patterns...),
			UpdatedAt: state.UpdatedAt,
		})
	}
	// Broadcast the origin_policy/changed
	// notification BEFORE returning the
	// response envelope; matches the
	// rc-permissions-grants / rc-plugins /
	// rc-triggers / rc-fastpush-toggle /
	// rc-resources / rc-conversation-config
	// / rc-terminals pattern
	// (broadcastXxxChanged), so a
	// dashboard's response handler can rely
	// on the notification having already
	// been enqueued.
	broadcastChanged(s, state)
	// Return the brief's lean envelope.
	return stateToWire(state, rejected)
}

// stateToWire renders an
// State as the dashboard-facing
// wire shape. The envelope is the brief's
// `{ok, patterns, applied_count, rejected,
// updated_at}` shape; the broadcast
// notification's params reuses the same
// helper so a dashboard that listens for
// origin_policy/changed and one that polls
// SetRemoteControlOriginPatterns see
// byte-for-byte identical envelopes.
//
// When called from the broadcast path the
// `rejected` slice is nil and
// `applied_count` is `len(state.Patterns)`
// (every persisted pattern is a
// successful Set); when called from the
// handler the `rejected` slice is the
// verbatim list the validator refused.
//
// `patterns` is `[]string` so the wire
// always emits `[]` rather than `null`.
// `updated_at` is the RFC3339Nano stamp
// the store stamped at the most-recent
// mutation.
//
// Cycle rc-auth-tokens.
func stateToWire(state State, rejected []string) map[string]any {
	if rejected == nil {
		rejected = []string{}
	}
	patterns := state.Patterns
	if patterns == nil {
		patterns = []string{}
	}
	var updatedAt string
	if !state.UpdatedAt.IsZero() {
		updatedAt = state.UpdatedAt.UTC().Format(time.RFC3339Nano)
	}
	return map[string]any{
		"ok":            true,
		"patterns":      patterns,
		"applied_count": len(patterns),
		"rejected":      rejected,
		"updated_at":    updatedAt,
	}
}

// rejectAllWire renders the
// response envelope for the reject-all
// dispatch path (the store did not mutate;
// state was preserved). The response
// always carries applied_count=0 +
// patterns=[] (the response describes the
// dashboard's submission, not the
// preserved store state) so dashboards can
// distinguish "your submission was
// refused" from "your submission landed"
// by the applied_count alone.
//
// `updated_at` echoes the preserved
// state's stamp so a dashboard polling
// both the response envelope and the
// subscribed notification sees the same
// timestamp.
func rejectAllWire(updatedAt time.Time, rejected []string) map[string]any {
	if rejected == nil {
		rejected = []string{}
	}
	var ts string
	if !updatedAt.IsZero() {
		ts = updatedAt.UTC().Format(time.RFC3339Nano)
	}
	return map[string]any{
		"ok":            true,
		"patterns":      []string{},
		"applied_count": 0,
		"rejected":      rejected,
		"updated_at":    ts,
	}
}
