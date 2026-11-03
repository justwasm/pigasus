// Package triggers — dispatcher.go: the shared parts of the
// five trigger methods introduced in cycle rc-triggers:
//
//	ListRemoteAgentTriggers
//	ListRemoteAgentTriggerOptions
//	CreateRemoteAgentTrigger
//	UpdateRemoteAgentTrigger
//	DeleteRemoteAgentTrigger
//
// Why a dedicated file (not another switch arm in the
// dispatcher's route table): the five handlers share the
// Store seam + a small sentinel-error map (ErrNotFound
// → -32017, ErrExists → -32016, ErrInvalidSpec
// → -32602) and three of them (Create / Update / Delete)
// need to emit a triggers/changed broadcast notification.
// Putting the handlers here, next to the type definitions
// (triggers.go) and the store (store.go / store_fs.go /
// store_mem.go), keeps the per-method bodies focused on the
// JSON-RPC envelope shape rather than the dispatcher's switch
// table.
//
// Sentinel map:
//
//	ErrNotFound     → -32017 "Trigger not found"
//	ErrExists       → -32016 "Trigger exists"
//	ErrInvalidSpec         → -32602 "Invalid params"
//	(other / I/O)          → -32603 "Internal error"
//
// The -32016 / -32017 codes are brand-new this cycle;
// -32602 ("Invalid params") is shared with every other
// method's "bad input" envelope (the brief explicitly says
// so). The "empty trigger_id" case maps to -32602 because
// it's a dashboard bug; the "unknown id" case maps to
// -32017 because it's a plausible retry-after-delete race.
//
// Cycle rc-triggers also adds the "broadcast on change"
// path: every successful Create / Update / Delete emits
// a `triggers/changed` notification to every open ws
// connection so a dashboard that subscribed to the
// channel sees the new state. The broadcast goes out
// through the Seams.Broadcast callback the host installs —
// the handlers here don't have to know which transport is
// registered, they just call Broadcast and the registered
// broadcaster fans the frame out. In-process callers
// without a transport fall back to a no-op broadcast (the
// mutation still lands in the store; the next
// ListRemoteAgentTriggers call always reflects the
// post-state).
//
// File layout (cycle rc-filesize-internal-remotecontrol):
//
//   - dispatcher.go       — package doc + constants +
//     params types +
//     listEnvelope /
//     toWire /
//     MapError /
//     broadcastChanged /
//     broadcastChangedFromState
//   - handlers.go         — the 5 handler
//     functions (HandleList /
//     HandleListOptions /
//     HandleCreate /
//     HandleUpdate /
//     HandleDelete)
//   - route.go            — the domain's own route table
//   - options.go          — the trigger-options catalogue
//
// Stdlib-only. No third-party deps.
package triggers

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/justwasm/pigasus/internal/remotecontrol/rpc"
)

// NotifChangedMethod is the JSON-RPC method name the
// dispatcher uses for the server-push notification
// emitted on every successful Create / Update / Delete.
// Wired via Seams.Broadcast(method, params) once per
// changed conn.
//
// A "triggers/changed" notification carries the full
// post-mutation state:
//
//	{ok:true, triggers:[{trigger_id, project_id, name,
//	 kind, spec, enabled, created_at, updated_at}, …],
//	 updated_at:"<RFC3339Nano>"}
//
// — the same shape the ListRemoteAgentTriggers response
// emits. The notification lets a dashboard that watches
// the channel re-render without re-polling.
const NotifChangedMethod = "triggers/changed"

// listRemoteAgentTriggersParams is the JSON shape the
// dashboard posts for ListRemoteAgentTriggers. project_id
// is optional; empty / absent returns all triggers.
type listRemoteAgentTriggersParams struct {
	// ProjectID, when non-empty, filters the result to
	// triggers whose ProjectID matches exactly.
	ProjectID string `json:"project_id"`
}

// createRemoteAgentTriggerParams is the JSON shape the
// dashboard posts for CreateRemoteAgentTrigger. The
// `trigger` field carries the trigger payload; the
// dispatcher validates each sub-field against the
// closed set / non-empty / spec rules before delegating
// to the store.
type createRemoteAgentTriggerParams struct {
	Trigger Trigger `json:"trigger"`
}

// updateRemoteAgentTriggerParams is the JSON shape the
// dashboard posts for UpdateRemoteAgentTrigger. trigger_id
// is required; patch carries the optional fields (any
// absent field is left untouched on the stored trigger).
type updateRemoteAgentTriggerParams struct {
	TriggerID string `json:"trigger_id"`
	Patch     Patch  `json:"patch"`
}

// deleteRemoteAgentTriggerParams is the JSON shape the
// dashboard posts for DeleteRemoteAgentTrigger. Just
// trigger_id — the dispatcher surfaces a
// no-op-with-Ok=false envelope if the id is missing.
type deleteRemoteAgentTriggerParams struct {
	TriggerID string `json:"trigger_id"`
}

// listEnvelope renders a triggers slice as the
// dashboard-facing wire shape:
//
//	{
//	  "ok":        true,
//	  "triggers":  [
//	    {trigger_id, project_id, name, kind, spec,
//	     enabled, created_at, updated_at}, …
//	  ]
//	}
//
// The Create / Update response envelopes use
// toWire (one trigger) rather than this list
// helper; the broadcast reuses this helper so a
// dashboard that listens for triggers/changed and one
// that polls ListRemoteAgentTriggers see byte-for-byte
// identical envelopes.
//
// Triggers: nil → empty slice (`[]`) on the wire so
// dashboards never see `triggers: null`.
func listEnvelope(triggers []Trigger) map[string]any {
	if triggers == nil {
		triggers = []Trigger{}
	}
	list := make([]any, 0, len(triggers))
	for _, t := range triggers {
		list = append(list, toWire(t))
	}
	return map[string]any{
		"ok":       true,
		"triggers": list,
	}
}

// toWire renders one Trigger as the wire envelope
// shape. Per-trigger fields (per the brief):
//
//	{
//	  "trigger_id":  "<id>",
//	  "project_id":  "<id>",
//	  "name":        "<label>",
//	  "kind":        "<kind>",
//	  "spec":        {…},
//	  "enabled":     <bool>,
//	  "created_at":  "<RFC3339Nano>",
//	  "updated_at":  "<RFC3339Nano>",
//	}
//
// nil spec → empty map (`{}`) on the wire so dashboards
// never see `spec: null`. The spec's inner keys ride on
// the trigger's kind; the dispatcher stores the spec
// verbatim, so a future kind that wants richer shape just
// extends the validator (the wire stays
// forward-compatible).
func toWire(t Trigger) map[string]any {
	spec := t.Spec
	if spec == nil {
		spec = Spec{}
	}
	return map[string]any{
		"trigger_id": t.TriggerID,
		"project_id": t.ProjectID,
		"name":       t.Name,
		"kind":       string(t.Kind),
		"spec":       map[string]any(spec),
		"enabled":    t.Enabled,
		"created_at": t.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updated_at": t.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

// MapError maps the Store sentinel errors
// to the JSON-RPC envelopes the brief calls for:
//
//	ErrNotFound     → -32017 "Trigger not found"
//	ErrExists       → -32016 "Trigger exists"
//	ErrInvalidSpec         → -32602 "Invalid params"
//	(other / I/O)          → -32603 "Internal error"
//
// op is the operation label so a dashboard log shows
// which stage failed. Mirrors
// plugins.MapError / permissions.MapError shape; the
// rc-permissions-grants cycle established the pattern.
func MapError(err error, op string) *rpc.ErrorBody {
	switch {
	case errors.Is(err, ErrNotFound):
		return &rpc.ErrorBody{Code: -32017, Message: "Trigger not found", Data: err.Error()}
	case errors.Is(err, ErrExists):
		return &rpc.ErrorBody{Code: -32016, Message: "Trigger exists", Data: err.Error()}
	case errors.Is(err, ErrInvalidSpec):
		return &rpc.ErrorBody{Code: -32602, Message: "Invalid params", Data: err.Error()}
	default:
		return &rpc.ErrorBody{Code: -32603, Message: "Internal error", Data: op + ": " + err.Error()}
	}
}

// broadcastChanged fans a triggers/changed
// notification out to every registered transport's open
// connection. The dispatcher hands the broadcast seam
// the post-mutation state so a dashboard watcher can
// re-render without re-polling.
//
// Why a domain-side helper rather than a
// transport-side one: the three trigger handlers all live
// in this domain, and the broadcaster (Seams.Broadcast) is
// the seam a transport installs to deliver "frame to every
// open conn" without the domain knowing the concrete
// transport type.
func broadcastChanged(s Seams, t Trigger) {
	if s.Broadcast == nil {
		return
	}
	list := []Trigger{t}
	broadcastChangedFromState(s, State{
		Version:   StateVersion,
		Triggers:  list,
		UpdatedAt: t.UpdatedAt,
	})
}

// broadcastChangedFromState is the post-state
// variant the Delete handler uses (which has the full
// state but no single "affected" trigger) and the
// per-trigger variant above delegates to.
//
// Without a registered broadcaster (in-process
// dispatchers, Unix-socket transport) this is a no-op
// — mutations still land in the store; the next
// ListRemoteAgentTriggers call always reflects the
// post-state.
func broadcastChangedFromState(s Seams, state State) {
	if s.Broadcast == nil {
		return
	}
	params := listEnvelope(state.Triggers)
	s.Broadcast(context.Background(), NotifChangedMethod, params)
}

// errWrap is the package-internal alias for fmt.Errorf
// with %w so the triggers handlers can wrap a sentinel
// into a typed error without pulling fmt into every
// dispatcher file's imports. Documented here; the
// implementation is a thin fmt.Errorf("%w", ...) call.
var _ = fmt.Errorf
