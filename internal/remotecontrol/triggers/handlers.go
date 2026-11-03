// Package triggers — handlers.go: the five JSON-RPC handlers
// for the trigger methods, split from dispatcher.go to keep both
// files below the 500-line / 50-fn budget that cycle
// rc-filesize-internal-remotecontrol enforces.
//
// Why a dedicated handler file (not the dispatcher
// switch): each handler is its own contract — param
// validation rules, store mapping, broadcast side
// effect, wire envelope — and that contract is roughly
// fifty lines per handler. Inlining them all into the
// main dispatcher file would push that file past 500
// lines per the cycle's hard constraint.
//
// Cycle rc-triggers introduced the handlers;
// rc-filesize-internal-remotecontrol only relocates
// them.
//
// Stdlib-only. No third-party deps.
package triggers

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/justwasm/pigasus/internal/remotecontrol/rpc"
)

// HandleList is the JSON-RPC handler
// for "ListRemoteAgentTriggers". Cycle rc-triggers.
//
// Param validation:
//
//   - project_id: optional. When present must be a
//     string; non-string → -32602. The dispatcher
//     trims whitespace and forwards the (possibly
//     empty) string to the store, which interprets
//     empty as "all projects".
//
// Store call:
//
//   - s.Store == nil → -32603.
//   - other error → -32603.
//
// Returns the post-filter list in the
// `{ok:true, triggers:[{trigger_id, project_id, name,
// kind, spec, enabled, created_at, updated_at}, …]}`
// envelope the brief pins. The triggers slice is sorted
// by CreatedAt ascending (the store's List pins the
// order) so the dashboard walks a stable sequence.
func HandleList(s Seams, params json.RawMessage) (any, *rpc.ErrorBody) {
	if s.Store == nil {
		return nil, rpc.InternalError("no trigger store installed")
	}
	var p listRemoteAgentTriggersParams
	if len(params) > 0 && string(params) != "null" {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, rpc.InvalidParams(err.Error())
		}
	}
	if rpc.NonStringFieldInJSON(params, "project_id", p.ProjectID) {
		return nil, rpc.InvalidParams("string fields must be strings")
	}
	projectID := strings.TrimSpace(p.ProjectID)
	triggers, err := s.Store.List(nil, projectID)
	if err != nil {
		return nil, MapError(err, "list triggers")
	}
	return listEnvelope(triggers), nil
}

// HandleListOptions is the JSON-RPC
// handler for "ListRemoteAgentTriggerOptions". Cycle
// rc-triggers.
//
// Param validation:
//
//   - none required; the brief pins this as a pure
//     read.
//
// Returns the canned `{kinds: [...], spec_schemas:
// {...}}` envelope built by
// DefaultOptionsResponse. The schema map is
// descriptive — the validator
// ValidateSpec enforces the same rules at Create
// / Update time; a dashboard that builds a per-field
// form from spec_schemas sees the same hints the
// validator surfaces in error envelopes.
//
// No store call (no read from disk, no lock taken).
//
// Wire-shape note: kinds is rendered as []any
// (not []string) so the in-process return value
// matches what the JSON round-trip produces —
// encoding/json decodes a JSON array into []interface{}
// (== []any), so an in-process caller (no JSON round-
// trip) and a wire caller (post JSON) see the same
// slice header and an identical dashboard render.
func HandleListOptions(_ Seams, _ json.RawMessage) (any, *rpc.ErrorBody) {
	resp := DefaultOptionsResponse()
	kinds := make([]any, 0, len(resp.Kinds))
	for _, k := range resp.Kinds {
		kinds = append(kinds, k)
	}
	schemas := make(map[string]any, len(resp.SpecSchemas))
	for kind, schema := range resp.SpecSchemas {
		schemas[kind] = schema
	}
	return map[string]any{
		"ok":           true,
		"kinds":        kinds,
		"spec_schemas": schemas,
	}, nil
}

// HandleCreate is the JSON-RPC handler
// for "CreateRemoteAgentTrigger". Cycle rc-triggers.
//
// Param validation:
//
//   - trigger.project_id: required, non-empty after
//     trim. → -32602.
//   - trigger.name: required, non-empty after
//     normalisation. → -32602.
//   - trigger.kind: must be one of file-changed |
//     schedule | webhook | manual. → -32602.
//   - trigger.spec: must satisfy ValidateSpec
//     for the trigger's kind. → -32602.
//   - Any present-but-non-string string field →
//     -32602.
//
// Store call:
//
//   - s.Store == nil → -32603.
//   - ErrExists → -32016.
//   - ErrInvalidSpec → -32602.
//   - other error → -32603.
//
// On success emits a triggers/changed notification so
// a dashboard that subscribed to the channel sees the
// new state without re-polling. The broadcast uses
// the Seams.Broadcast callback the host installs; the
// handler here doesn't have to know which transport is
// registered, it just calls Broadcast and the registered
// broadcaster fans the frame out.
func HandleCreate(s Seams, params json.RawMessage) (any, *rpc.ErrorBody) {
	if s.Store == nil {
		return nil, rpc.InternalError("no trigger store installed")
	}
	var p createRemoteAgentTriggerParams
	if len(params) > 0 && string(params) != "null" {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, rpc.InvalidParams(err.Error())
		}
	}
	if rpc.NonStringFieldInJSON(params, "name", p.Trigger.Name) ||
		rpc.NonStringFieldInJSON(params, "project_id", p.Trigger.ProjectID) ||
		rpc.NonStringFieldInJSON(params, "kind", string(p.Trigger.Kind)) {
		return nil, rpc.InvalidParams("string fields must be strings")
	}
	t := p.Trigger
	t.Name = normaliseName(t.Name)
	t.ProjectID = strings.TrimSpace(t.ProjectID)
	if t.ProjectID == "" {
		return nil, rpc.InvalidParams("trigger.project_id is required")
	}
	if t.Name == "" {
		return nil, rpc.InvalidParams("trigger.name is required")
	}
	if !IsValidKind(t.Kind) {
		return nil, rpc.InvalidParams("trigger.kind must be one of file-changed|schedule|webhook|manual")
	}
	if err := ValidateSpec(t.Kind, t.Spec); err != nil {
		return nil, rpc.InvalidParams(err.Error())
	}
	out, err := s.Store.Create(nil, t)
	if err != nil {
		return nil, MapError(err, "create trigger")
	}
	// Broadcast the post-mutation state BEFORE returning
	// the response envelope; matches the
	// rc-permissions-grants / rc-plugins broadcast
	// pattern so a dashboard's response handler can
	// rely on the notification having already been
	// enqueued.
	broadcastChanged(s, out)
	return map[string]any{
		"ok":         true,
		"trigger_id": out.TriggerID,
		"trigger":    toWire(out),
	}, nil
}

// HandleUpdate is the JSON-RPC handler
// for "UpdateRemoteAgentTrigger". Cycle rc-triggers.
//
// Param validation:
//
//   - trigger_id: required, non-empty string. Empty /
//     missing / non-string → -32602.
//   - patch.name: optional non-empty string when
//     present. Empty / whitespace → -32602.
//   - patch.enabled: optional boolean. (We can't
//     distinguish absent from false with a *bool
//     pointer; see Patch docs.) The validator
//     accepts both missing and explicit false.
//   - patch.spec: optional. If present, must satisfy
//     ValidateSpec for the trigger's kind.
//
// Store call:
//
//   - s.Store == nil → -32603.
//   - ErrNotFound → -32017.
//   - ErrExists (the new name collides) → -32016.
//   - ErrInvalidSpec (defence-in-depth) → -32602.
//   - other error → -32603.
//
// On success emits a triggers/changed notification.
// The response envelope carries the full post-mutation
// trigger.
func HandleUpdate(s Seams, params json.RawMessage) (any, *rpc.ErrorBody) {
	if s.Store == nil {
		return nil, rpc.InternalError("no trigger store installed")
	}
	var p updateRemoteAgentTriggerParams
	if len(params) > 0 && string(params) != "null" {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, rpc.InvalidParams(err.Error())
		}
	}
	if rpc.NonStringFieldInJSON(params, "trigger_id", p.TriggerID) ||
		rpc.NonStringFieldInJSON(params, "name", "") {
		// patch.Name is a *string — the coerced
		// string is "" when absent. We only need to
		// guard against a present-but-non-string
		// "name" key; the helper handles that.
		return nil, rpc.InvalidParams("string fields must be strings")
	}
	id := normaliseName(p.TriggerID)
	if id == "" {
		return nil, rpc.InvalidParams("trigger_id is required")
	}
	if p.Patch.Name != nil {
		newName := normaliseName(*p.Patch.Name)
		if newName == "" {
			return nil, rpc.InvalidParams("patch.name must be non-empty when present")
		}
		p.Patch.Name = &newName
	}
	out, err := s.Store.Update(nil, id, p.Patch)
	if err != nil {
		return nil, MapError(err, "update trigger")
	}
	broadcastChanged(s, out)
	return map[string]any{
		"ok":      true,
		"trigger": toWire(out),
	}, nil
}

// HandleDelete is the JSON-RPC handler
// for "DeleteRemoteAgentTrigger". Cycle rc-triggers.
//
// Param validation:
//
//   - trigger_id: required, non-empty string. Empty /
//     missing / non-string → -32602.
//
// Store call:
//
//   - s.Store == nil → -32603.
//   - ErrNotFound → -32017.
//   - other error → -32603.
//
// On success emits a triggers/changed notification.
// The response envelope is the brief's lean
// `{ok:true, trigger_id, deleted_at}` shape.
func HandleDelete(s Seams, params json.RawMessage) (any, *rpc.ErrorBody) {
	if s.Store == nil {
		return nil, rpc.InternalError("no trigger store installed")
	}
	var p deleteRemoteAgentTriggerParams
	if len(params) > 0 && string(params) != "null" {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, rpc.InvalidParams(err.Error())
		}
	}
	if rpc.NonStringFieldInJSON(params, "trigger_id", p.TriggerID) {
		return nil, rpc.InvalidParams("string fields must be strings")
	}
	id := normaliseName(p.TriggerID)
	if id == "" {
		return nil, rpc.InvalidParams("trigger_id is required")
	}
	state, err := s.Store.Delete(nil, id)
	if err != nil {
		return nil, MapError(err, "delete trigger")
	}
	// Broadcast the post-mutation state BEFORE
	// returning the response envelope.
	broadcastChangedFromState(s, state)
	return map[string]any{
		"ok":         true,
		"trigger_id": id,
		"deleted_at": state.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}, nil
}

type runTriggerParams struct {
	TriggerID    string `json:"trigger_id"`
	TriggerIDAlt string `json:"triggerId"`
	Backend      int    `json:"backend"`
}

// HandleRun is the JSON-RPC handler for "RunTrigger" / "RunRemoteAgentTrigger".
func HandleRun(s Seams, params json.RawMessage) (any, *rpc.ErrorBody) {
	if s.Store == nil {
		return nil, rpc.InternalError("no trigger store installed")
	}
	var p runTriggerParams
	if len(params) > 0 && string(params) != "null" {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, rpc.InvalidParams(err.Error())
		}
	}
	id := p.TriggerID
	if id == "" {
		id = p.TriggerIDAlt
	}
	id = normaliseName(id)
	if id == "" {
		return map[string]any{}, nil
	}
	list, err := s.Store.List(nil, "")
	if err != nil {
		return nil, MapError(err, "run trigger")
	}
	for i := range list {
		if list[i].TriggerID == id {
			broadcastChanged(s, list[i])
			break
		}
	}
	return map[string]any{}, nil
}
