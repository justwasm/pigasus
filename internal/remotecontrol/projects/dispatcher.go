// Package projects — dispatcher.go: the per-method JSON-RPC
// handlers for the three project methods introduced in cycle
// rc-projects:
//
//	ListRemoteAgentProjects
//	CreateRemoteAgentProject
//	UpdateRemoteAgentProject
//
// Why a dedicated file (not another switch arm in the
// dispatcher's route table): the three handlers share the
// Store seam + a small sentinel-error map
// (ErrExists → -32009, ErrNotFound →
// -32004, files.ErrPathNotAllowed → -32007), and the
// read-only / read-write split the dispatcher already
// has is more useful when the read-write (create /
// update) handlers are colocated. Future cycle
// rc-conversation-deletions adds DeleteRemoteAgentProject
// to this same file.
//
// Sentinel map:
//
//	ErrExists    → -32009 "Project exists"
//	ErrNotFound  → -32004 "Project not found"
//	files.ErrPathNotAllowed → -32007 "Path not allowed"
//	(unmapped FS / I/O)      → -32603 "Internal error"
//
// The -32009 code is brand-new this cycle; -32004 is
// shared with the cascade-not-found envelope (the
// brief explicitly says share). -32007 is shared
// with the rc-file-ops surface — both surfaces
// consume the same files.Provider.
//
// Stdlib-only. No third-party deps.
package projects

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/justwasm/pigasus/internal/remotecontrol/effort"
	"github.com/justwasm/pigasus/internal/remotecontrol/files"
	"github.com/justwasm/pigasus/internal/remotecontrol/rpc"
)

// listParams is the JSON shape the
// dashboard posts for ListRemoteAgentProjects. Both
// fields are optional. limit caps the result count;
// the dispatcher's own cap (DefaultListMax = 1000)
// keeps a misbehaving dashboard from asking for
// millions. prefix filters by workspace-path
// prefix; an empty prefix means "all projects".
type listParams struct {
	Limit  int    `json:"limit"`
	Prefix string `json:"prefix"`
}

// createParams is the JSON shape
// the dashboard posts for CreateRemoteAgentProject.
// name and workspace are required (the handler
// returns -32602 for empty); default_model and
// default_effort are optional. An invalid
// default_effort returns -32602 too.
type createParams struct {
	Name          string `json:"name"`
	Workspace     string `json:"workspace"`
	DefaultModel  string `json:"default_model"`
	DefaultEffort string `json:"default_effort"`
}

// updateParams is the JSON shape
// the dashboard posts for UpdateRemoteAgentProject.
// id is required; the rest is a patch ("" fields are
// left unchanged).
type updateParams struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	DefaultModel  string `json:"default_model"`
	DefaultEffort string `json:"default_effort"`
}

// deleteParams is the JSON shape
// the dashboard posts for DeleteRemoteAgentProject.
// Cycle rc-conversation-deletions.
//
// id: required; the dispatcher returns -32602 for
// empty.
//
// fail_if_bound: optional; defaults to true. When true,
// the store refuses the delete if any conversation
// still references the project's workspace directory
// (the brief's "fail_if_bound" semantic). When false,
// the project is deleted regardless.
//
// We use a *bool (rather than `bool`) so the wire
// "field absent" case lands naturally as nil — i.e.
// "default true" — and the dashboard's explicit
// `false` is preserved through the JSON round-trip.
type deleteParams struct {
	ID          string `json:"id"`
	FailIfBound *bool  `json:"fail_if_bound,omitempty"`
}

// HandleList is the JSON-RPC
// handler for "ListRemoteAgentProjects". Cycle
// rc-projects.
//
// Param validation:
//
//   - limit: optional, capped at MaxListLimit
//     (1000). Anything above → -32602. Zero / unset
//     → DefaultListLimit (200). Negative →
//     -32602.
//
//   - prefix: optional, pass-through to the store.
//
// Store call:
//
//   - s.Store == nil → -32603 (no project store
//     installed; the production wiring in
//     cmd/wagyu/remote_control.go always installs
//     one — nil means the dispatcher was constructed
//     in a test that didn't bother).
//   - the store's error → -32603 (the brief doesn't
//     define any sentinel for "store is broken"; the
//     dispatcher maps everything to -32603).
//
// On success the result envelope carries the project
// slice (in insertion order — oldest first) and the
// total count:
//
//	{
//	  "projects": [ {id, name, workspace,
//	                 default_model, default_effort,
//	                 created_at, updated_at}, ... ],
//	  "total":    <len>
//	}
func HandleList(s Seams, params json.RawMessage) (any, *rpc.ErrorBody) {
	if s.Store == nil {
		return nil, rpc.InternalError("no project store installed")
	}
	var p listParams
	if len(params) > 0 && string(params) != "null" {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, rpc.InvalidParams(err.Error())
		}
	}
	if p.Limit < 0 {
		return nil, rpc.InvalidParams("limit must be >= 0")
	}
	if p.Limit == 0 {
		p.Limit = DefaultListLimit
	}
	if p.Limit > MaxListLimit {
		return nil, rpc.InvalidParams("limit must be <= 1000")
	}

	rows, err := s.Store.List(nil, strings.TrimSpace(p.Prefix), p.Limit)
	if err != nil {
		return nil, rpc.InternalError(err.Error())
	}
	if rows == nil {
		rows = []Project{}
	}
	return map[string]any{
		"projects": sliceToWire(rows),
		"total":    len(rows),
	}, nil
}

// HandleCreate is the JSON-RPC
// handler for "CreateRemoteAgentProject". Cycle
// rc-projects.
//
// Param validation:
//
//   - name: required, non-empty after trim.
//     -32602 otherwise.
//   - workspace: required, non-empty after trim.
//     -32602 otherwise.
//   - default_model: optional, pass-through.
//   - default_effort: optional, must be one of
//     DefaultEffortValues when present. Empty →
//     host default.
//
// Store call:
//
//   - s.Store == nil → -32603.
//   - ErrExists → -32009.
//   - ErrPathNotAllowed (raised by FSStore
//     when a filesystem scope is wired and the workspace
//     is outside the scope) → -32007.
//   - other error → -32603.
//
// On success the result envelope carries the new
// project's full metadata. The dispatcher stamps
// created_at and updated_at from the store's
// Project record (already in UTC); the format
// matches the rc-list-readonly conversation timestamps
// (RFC3339Nano) for cross-cycle consistency.
func HandleCreate(s Seams, params json.RawMessage) (any, *rpc.ErrorBody) {
	if s.Store == nil {
		return nil, rpc.InternalError("no project store installed")
	}
	p, perr := decodeCreateParams(params)
	if perr != nil {
		return nil, perr
	}
	row, err := s.Store.Create(nil, Create{
		Name:          p.Name,
		Workspace:     p.Workspace,
		DefaultModel:  p.DefaultModel,
		DefaultEffort: p.DefaultEffort,
	})
	if err != nil {
		return nil, MapError(err, "create project")
	}
	return toWire(row), nil
}

// HandleUpdate is the JSON-RPC
// handler for "UpdateRemoteAgentProject". Cycle
// rc-projects.
//
// Param validation:
//
//   - id: required, non-empty. -32602 otherwise.
//   - name / default_model / default_effort: optional.
//     Empty fields are passed through to the store as
//     "leave unchanged"; non-empty fields are
//     validated (default_effort must be in
//     DefaultEffortValues).
//
// Store call:
//
//   - s.Store == nil → -32603.
//   - ErrNotFound → -32004.
//   - ErrExists (rename collision) → -32009.
//   - other error → -32603.
//
// On success the result envelope carries the new
// state of the project (the dispatcher does NOT
// require the dashboard to re-List to see the
// patch).
func HandleUpdate(s Seams, params json.RawMessage) (any, *rpc.ErrorBody) {
	if s.Store == nil {
		return nil, rpc.InternalError("no project store installed")
	}
	var p updateParams
	if len(params) > 0 && string(params) != "null" {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, rpc.InvalidParams(err.Error())
		}
	}
	if rpc.NonStringFieldInJSON(params, "id", p.ID) ||
		rpc.NonStringFieldInJSON(params, "name", p.Name) ||
		rpc.NonStringFieldInJSON(params, "default_model", p.DefaultModel) ||
		rpc.NonStringFieldInJSON(params, "default_effort", p.DefaultEffort) {
		return nil, rpc.InvalidParams("string fields must be strings")
	}
	if strings.TrimSpace(p.ID) == "" {
		return nil, rpc.InvalidParams("id is required")
	}
	if p.DefaultEffort != "" && !effort.IsValid(p.DefaultEffort) {
		return nil, rpc.InvalidParams("default_effort must be one of low|medium|high|xhigh|max")
	}

	row, err := s.Store.Update(nil, p.ID, Patch{
		Name:          p.Name,
		DefaultModel:  p.DefaultModel,
		DefaultEffort: p.DefaultEffort,
	})
	if err != nil {
		return nil, MapError(err, "update project")
	}

	return map[string]any{
		"id":             row.ID,
		"name":           row.Name,
		"workspace":      row.Workspace,
		"default_model":  row.DefaultModel,
		"default_effort": row.DefaultEffort,
		"created_at":     row.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updated_at":     row.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}, nil
}

// HandleDelete is the JSON-RPC
// handler for "DeleteRemoteAgentProject". Cycle
// rc-conversation-deletions.
//
// Param validation:
//
//   - id: required, non-empty after trim. -32602
//     otherwise.
//   - fail_if_bound: optional, defaults to true. The
//     pointer-form guard distinguishes "field absent"
//     from "explicit false"; an absent pointer ⇒ true.
//
// Store call:
//
//   - s.Store == nil → -32603.
//   - ErrNotFound → -32004.
//   - ErrBound → -32013.
//   - other error → -32603.
//
// On success the result envelope carries `deleted_at`
// (a UTC RFC3339Nano timestamp the store stamps
// internally; the dispatcher hands back the wall
// clock). The full project record is intentionally not
// echoed — the brief's shape is a deletion
// confirmation, not a full record replay. (Future
// cycles that want a richer envelope can add `id` /
// `name` / `workspace` keys without re-touching this
// method's structure.)
func HandleDelete(s Seams, params json.RawMessage) (any, *rpc.ErrorBody) {
	if s.Store == nil {
		return nil, rpc.InternalError("no project store installed")
	}
	var p deleteParams
	if len(params) > 0 && string(params) != "null" {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, rpc.InvalidParams(err.Error())
		}
	}
	if rpc.NonStringFieldInJSON(params, "id", p.ID) {
		return nil, rpc.InvalidParams("string fields must be strings")
	}
	if strings.TrimSpace(p.ID) == "" {
		return nil, rpc.InvalidParams("id is required")
	}
	failIfBound := true
	if p.FailIfBound != nil {
		failIfBound = *p.FailIfBound
	}

	if err := s.Store.Delete(nil, p.ID, failIfBound); err != nil {
		return nil, MapError(err, "delete project")
	}

	return map[string]any{
		"ok":         true,
		"deleted_at": time.Now().UTC().Format(time.RFC3339Nano),
	}, nil
}

// MapError maps the Store sentinel errors
// to the JSON-RPC envelopes the brief calls for:
//
//	ErrExists    → -32009 "Project exists"
//	ErrNotFound  → -32004 "Project not found"
//	ErrBound     → -32013 "Project is bound"
//	files.ErrPathNotAllowed → -32007 "Path not allowed"
//	(other)                  → -32603 "Internal error"
//
// op is the operation label ("create project",
// "update project", "delete project") so a dashboard
// log shows which stage failed.
func MapError(err error, op string) *rpc.ErrorBody {
	switch {
	case errors.Is(err, ErrExists):
		return &rpc.ErrorBody{Code: -32009, Message: "Project exists", Data: err.Error()}
	case errors.Is(err, ErrNotFound):
		return &rpc.ErrorBody{Code: -32004, Message: "Project not found", Data: err.Error()}
	case errors.Is(err, ErrBound):
		return &rpc.ErrorBody{Code: -32013, Message: "Project is bound", Data: err.Error()}
	case errors.Is(err, files.ErrPathNotAllowed):
		return &rpc.ErrorBody{Code: -32007, Message: "Path not allowed", Data: err.Error()}
	default:
		return &rpc.ErrorBody{Code: -32603, Message: "Internal error", Data: op + ": " + err.Error()}
	}
}

// DefaultListLimit is the default `limit`
// the dispatcher applies when ListRemoteAgentProjects'
// params omits the field. 200 is the same default
// the rc-list-readonly Conversations handler uses; the
// two surfaces share their caps so a dashboard can
// predict the wire envelope's max shape.
const DefaultListLimit = 200

// MaxListLimit is the absolute upper bound on
// `limit` the dispatcher accepts. Anything above this
// returns -32602 (Invalid params) before the store
// is called. 1000 is the dashboard's "absurdly large"
// threshold; a workspace with more than a thousand
// projects probably needs pagination, which the brief
// parks for a future cycle.
const MaxListLimit = 1000

// sliceToWire projects a []Project onto the
// dashboard-facing envelope shape. Each row carries
// id, name, workspace, default_model, default_effort,
// created_at, updated_at. Timestamps are formatted as
// RFC3339Nano UTC so the wire envelope matches what
// the rc-list-readonly Conversations handler returns
// (consistent time shape across cycles).
//
// Cycle rc-resources adds the `resources` field: the
// resource-id list the project is bound to. The field
// is **omitted when nil** so a pre-rc-resources
// dashboard (one that doesn't know about bindings)
// keeps parsing the same envelope; a post-rc-resources
// dashboard sees `resources` as an empty slice for a
// project with no bindings, or the bound id list when
// the project is bound. The pointer-form guard on
// Project.Resources (= Patch.Resources pointer
// form) is what distinguishes "field absent" from
// "explicit empty list", but the wire envelope collapses
// both into "empty slice" — the difference only matters
// at the storage layer (where we don't want to silently
// erase a project's bindings on a metadata-only Update).
//
// nil slice → empty slice (defensive — never null on
// the wire).
func sliceToWire(rows []Project) []map[string]any {
	out := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		entry := map[string]any{
			"id":             r.ID,
			"name":           r.Name,
			"workspace":      r.Workspace,
			"default_model":  r.DefaultModel,
			"default_effort": r.DefaultEffort,
			"created_at":     r.CreatedAt.UTC().Format(time.RFC3339Nano),
			"updated_at":     r.UpdatedAt.UTC().Format(time.RFC3339Nano),
		}
		// Cycle rc-resources: include the bound
		// resource-id list when present (non-nil).
		// nil = "field absent" — the dashboard sees no
		// `resources` key, so a pre-rc-resources client
		// parses the envelope unchanged.
		if r.Resources != nil {
			resources := make([]string, len(r.Resources))
			copy(resources, r.Resources)
			entry["resources"] = resources
		}
		out = append(out, entry)
	}
	return out
}
