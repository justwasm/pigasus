// Package triggers — triggers.go: the trigger-state types
// and the Store interface that the five JSON-RPC
// methods introduced in cycle rc-triggers call into.
//
// Why split: the five JSON-RPC methods
// (ListRemoteAgentTriggers /
// ListRemoteAgentTriggerOptions /
// CreateRemoteAgentTrigger / UpdateRemoteAgentTrigger /
// DeleteRemoteAgentTrigger) share a single CRUD-shaped seam
// they all talk to; the dispatcher does NOT know about the
// on-disk format. Putting the types in their own file keeps
// the per-handler bodies in handlers.go focused on
// the JSON-RPC envelope shape, and lets the storage layer
// (store.go / store_fs.go / store_mem.go) keep talking to a stable interface
// without re-touching the dispatcher.
//
// Storage shape:
//
//	$ANTIGRAVITY_HOME/triggers.json
//
// One file, versioned ({"version":1, "triggers":[...],
// "updated_at":...}). The file format is intentionally
// forward-compatible so future cycles can add fields without
// rewriting history (the version stamp bumps only on breaking
// changes).
//
// Cycle rc-triggers adds five JSON-RPC methods and one
// storage seam. The seam is shaped so a future cycle
// (rc-triggers-runner that actually *fires* the cascade when
// a schedule ticks / a file changes — not in this brief) can
// extend it without re-touching the dispatcher.
//
// What "trigger" means here:
//
// Triggers are *declarative rules* the dashboard installs on
// the daemon: when an event matches (a file changes, a
// schedule ticks, a webhook arrives, the user clicks Run),
// the future runner fires a cascade prompt. This cycle is
// **persistence only** — no scheduling engine, no
// file-watcher, no webhook receiver. The five methods are
// the CRUD surface; a future `rc-triggers-runner` cycle
// wires the engine that consumes the on-disk state.
//
// The four `kind`s ship today:
//
//   - "file-changed": a glob + event filter. The future
//     runner diffs against this filter on every fsnotify
//     event.
//   - "schedule": a cron / interval expression + prompt.
//   - "webhook": an HTTP path + optional secret + prompt.
//   - "manual": a user-fired trigger with no auto-event
//     (the dashboard renders a "Run" button and calls a
//     future `RunRemoteAgentTrigger` to drive it).
//
// Stdlib-only. No third-party deps.
package triggers

import (
	"context"
	"errors"
	"strings"
	"time"
)

// Kind is the closed set of trigger kinds the
// dashboard can install. The four constants are the values
// the brief pins; the closed set is what IsValidKind
// checks before the dispatcher hands the value to the store
// (an unknown kind returns -32602 "Invalid params" without
// ever touching the store seam).
//
// Wire shape is the bare string
// ("file-changed" / "schedule" / "webhook" / "manual"). A
// future cycle that wants more kinds extends the constants
// and bumps the triggers.json on-disk "version".
type Kind string

const (
	// FileChanged fires when a file matching the
	// spec's glob has the spec's event reported by the
	// fsnotify watcher the future rc-triggers-runner
	// brings up.
	FileChanged Kind = "file-changed"
	// Schedule fires on a cron / interval
	// expressed by the spec's cron / every field. The
	// future runner wires a cron parser.
	Schedule Kind = "schedule"
	// Webhook fires when an HTTP request hits the
	// spec's path (a future webhook receiver registers
	// the routes at startup).
	Webhook Kind = "webhook"
	// Manual fires when the user clicks a "Run"
	// button in the dashboard. No auto-event; the runner
	// listens for a future `RunRemoteAgentTrigger` RPC.
	Manual Kind = "manual"
)

// IsValidKind reports whether kind is one of the
// four constants above. The dispatcher uses this to map
// unknown values to -32602 before the store call.
func IsValidKind(kind Kind) bool {
	switch kind {
	case FileChanged,
		Schedule,
		Webhook,
		Manual:
		return true
	}
	return false
}

// AllKinds is the canonical list of the four kinds
// the dispatcher returns from ListRemoteAgentTriggerOptions.
// Kept as a package-level slice so the handler's
// "kinds" array is literally the same shape the spec
// validator's closed set checks against — change one, change
// the other.
func AllKinds() []Kind {
	return []Kind{
		FileChanged,
		Schedule,
		Webhook,
		Manual,
	}
}

// Spec is the per-kind payload the dashboard posts.
// The shape depends on Kind — see ValidateSpec
// (triggers.go) for the per-kind rules. The dispatcher
// stores the spec as an opaque map; only the validator and
// future-runner care about the inner keys.
//
// Why map[string]any rather than a per-kind struct: the
// future runner will validate the spec by kind before
// scheduling / wiring it; the persistence layer just hands
// the map back unchanged so a future kind can be added
// without bumping the on-disk JSON version (the spec keys
// ride on the kind field, not on the spec struct shape).
type Spec map[string]any

// Trigger is one row of the trigger registry the dashboard
// sees on ListRemoteAgentTriggers. The fields mirror what
// the decompiled agy `RemoteAgentTrigger` records carry:
//
//   - TriggerID: the stable id (e.g. "tri_3f7b5c…"),
//     unique within the registry. Wire-mapped to
//     "trigger_id". Generated at Create time by the store
//     (a hex-encoded random 16-byte id with the "tri_"
//     prefix).
//
//   - ProjectID: the project's stable id the trigger
//     belongs to. Required at Create time; used by the
//     List filter to scope per-project dashboards.
//
//   - Name: the human-facing label. Required at Create;
//     unique within (project_id, name) so two triggers in
//     different projects can share a name without
//     collision. Wire-mapped to "name".
//
//   - Kind: the trigger kind (FileChanged /
//     Schedule / Webhook / Manual).
//
//   - Spec: the per-kind payload. Validated by
//     ValidateSpec at Create and Update time.
//
//   - Enabled: whether the (future) runner should
//     consider the trigger. False pauses the trigger
//     without deleting its state.
//
//   - CreatedAt: RFC3339Nano UTC stamp the store stamped
//     at record-creation time.
//
//   - UpdatedAt: RFC3339Nano UTC stamp the store bumped at
//     the most-recent mutation.
type Trigger struct {
	// TriggerID is the stable id dashboards use to
	// address a trigger. Required and unique within a
	// registry.
	TriggerID string `json:"trigger_id"`
	// ProjectID is the project's stable id the
	// trigger belongs to. Wire-mapped to "project_id".
	// Empty — the default when the host doesn't scope
	// by project — matches a global trigger (a future
	// dashboard filter; the cycle brief keeps the field
	// always non-empty).
	ProjectID string `json:"project_id"`
	// Name is the human-facing label; unique within
	// (project_id, name).
	Name string `json:"name"`
	// Kind is the trigger kind.
	Kind Kind `json:"kind"`
	// Spec is the per-kind payload.
	Spec Spec `json:"spec"`
	// Enabled is whether the future runner considers
	// the trigger. Wire-mapped to "enabled".
	Enabled bool `json:"enabled"`
	// CreatedAt is the wall-clock stamp the store
	// stamped at record-creation time. RFC3339Nano UTC.
	CreatedAt time.Time `json:"created_at"`
	// UpdatedAt is the wall-clock stamp the store
	// stamped at the most-recent mutation. RFC3339Nano
	// UTC.
	UpdatedAt time.Time `json:"updated_at"`
}

// Patch is the optional update payload Update
// accepts. Every field is a "may or may not be present"
// pointer-to-string / map; the store applies the present
// fields and leaves the absent ones alone.
//
// The "name" field, when present, must be a non-empty
// string AND must not collide with another trigger in the
// same (project_id, name). The "enabled" field, when
// present, is a boolean (the bool zero value is a
// legitimate "disable" — we cannot distinguish "absent" from
// "false" with a bare bool, so we use *bool). The "spec"
// field, when present, replaces the spec atomically (no
// partial-merge of keys).
type Patch struct {
	// Name, when non-nil and non-empty, replaces the
	// trigger's name. The store enforces
	// (project_id, name) uniqueness.
	Name *string `json:"name,omitempty"`
	// Enabled, when non-nil, replaces the trigger's
	// enabled flag. nil means "do not touch enabled".
	Enabled *bool `json:"enabled,omitempty"`
	// Spec, when non-nil, replaces the trigger's spec.
	// nil means "do not touch spec".
	Spec *Spec `json:"spec,omitempty"`
}

// State is the on-disk / wire shape the JSON file
// holds and the dispatcher hands back. One instance per
// $ANTIGRAVITY_HOME/triggers.json; the store is
// single-document by design — there is no "list of states"
// because there is exactly one registry per host.
//
// The struct is intentionally JSON-serialised with lowercase
// keys so the on-disk format matches the wire format the
// dashboard reads. The Version field is bumped on breaking
// changes (the brief pins v1).
type State struct {
	// Version is the on-disk format version. Bumped on
	// breaking changes (renamed keys, removed fields);
	// non-breaking additions (new optional fields with
	// omitempty) leave the version alone.
	Version int `json:"version"`
	// Triggers is the live trigger slice. nil → empty
	// ([]Trigger{}) so the wire always sees "[]".
	Triggers []Trigger `json:"triggers"`
	// UpdatedAt is the wall-clock stamp the store
	// bumped at the most-recent mutation. RFC3339Nano
	// UTC.
	UpdatedAt time.Time `json:"updated_at"`
}

// Store is the seam the five JSON-RPC methods call
// into. Cycle rc-triggers adds Snapshot / List / Create /
// Update / Delete; future cycles (rc-triggers-runner) will
// extend it with a Subscribe / Run / ListByKind / etc.
//
// Snapshot returns the whole state including UpdatedAt.
// The dispatcher never checks for a "not-found" envelope
// — a fresh host with no triggers.json yet returns the
// empty state (no triggers, version=1) so the dashboard
// always has a coherent answer.
//
// List returns the triggers filtered by projectID (empty
// projectID returns all triggers). The store returns the
// slice sorted by CreatedAt ascending so dashboards see a
// stable sequence; the test for the brief's "List sorted by
// created_at" pins this contract.
//
// Create takes one Trigger (minus the TriggerID /
// CreatedAt / UpdatedAt the store stamps). The store
// validates (project_id, name) uniqueness and returns
// ErrExists on a duplicate.
//
// Update applies the non-nil fields of patch to the trigger
// identified by id; an unknown id returns
// ErrNotFound. The (project_id, name) uniqueness
// rule still applies: a Name change that collides with an
// existing row returns ErrExists.
//
// Delete removes the trigger identified by id; an unknown
// id returns ErrNotFound. Returns the post-mutation
// state.
//
// All methods take a context the dispatcher passes
// through; none block on I/O today (the FS store uses an
// in-process mutex + synchronous write).
type Store interface {
	Snapshot(ctx context.Context) (State, error)
	List(ctx context.Context, projectID string) ([]Trigger, error)
	Create(ctx context.Context, t Trigger) (Trigger, error)
	Update(ctx context.Context, id string, patch Patch) (Trigger, error)
	Delete(ctx context.Context, id string) (State, error)
}

// ErrNotFound is the sentinel Store.Update
// and Store.Delete return when the caller tries to
// mutate a trigger_id that is not in the registry. The
// dispatcher translates to JSON-RPC -32017 "Trigger not
// found".
//
// Cycle rc-triggers adds this code; prior cycles parked the
// destructive methods without defining the sentinel.
//
// The -32017 code is brand-new this cycle. -32602 ("Invalid
// params") is shared with every other method's "bad input"
// envelope (the brief explicitly says so). The "empty
// trigger_id" case maps to -32602 because it's a dashboard
// bug; the "unknown id" case maps to -32017 because it's a
// plausible retry-after-delete race that a dashboard might
// want to surface distinctly.
var ErrNotFound = errors.New("rc-triggers: trigger not found")

// ErrExists is the sentinel Store.Create and
// Store.Update return when the caller's
// (project_id, name) collides with an existing trigger. The
// dispatcher translates to JSON-RPC -32016 "Trigger exists".
//
// Cycle rc-triggers adds this code; the same
// unique-within-(project_id, name) rule governs both Create
// and Name-bearing Update. The two paths' error mapping is
// the same because a dashboard would want to surface
// "duplicate" without distinguishing "you sent two Creates
// simultaneously" from "you renamed one to collide with
// another". The race is also bounded by the store's RWMutex
// (the FS store's read-modify-write runs under s.mu), so the
// two paths are symmetric.
var ErrExists = errors.New("rc-triggers: trigger exists")

// ErrInvalidSpec is the sentinel the spec validator
// returns when a Create / Update posts a spec that does
// not satisfy the per-kind rules. The dispatcher
// translates to JSON-RPC -32602 "Invalid params" — a
// dashboard bug (the spec is malformed), not a "-3xxx"
// domain error.
//
// The error message includes the failing field name so the
// dashboard's `data` field carries a verbatim hint to the
// user. wire errors from the validator DO NOT include the
// stack-trace form (`"%+v"`) — the dashboard formatter
// renders the message directly.
var ErrInvalidSpec = errors.New("rc-triggers: invalid spec")

// StateVersion is the on-disk format version the
// current store writes. Bumped on breaking changes (renamed
// keys, removed fields); non-breaking additions (new
// optional fields with omitempty) leave the version alone
// so older readers still parse the file.
//
// v1 (cycle rc-triggers): the initial layout. See
// State for the field set.
const StateVersion = 1

// IDPrefix is the prefix the store stamps on every
// generated trigger id ("tri_"). Dashboards that want to
// distinguish trigger ids from cascade / cascade-message ids
// can match on the prefix. The store mints ids of the form
// "tri_<16hex>" via randomHexTriggerID below.
const IDPrefix = "tri_"

// normaliseName trims whitespace from the user-supplied
// trigger name. Empty after trim → "". Used by both the
// dispatcher layer (before calling the store) and the store
// layer (when comparing for duplicates). Returns "" when
// the trimmed result is empty so the same check trips
// everywhere.
//
// Today the store does not auto-normalise (Creates that
// pass "  on-pull  " and "on-pull" are considered
// distinct), but the helper is here so a future cycle
// that wants server-side canonicalisation (typical for
// id-shaped fields) only has to swap the lookup line in
// store.go for normaliseName(...). The dispatcher
// path is the only validation site today (it surfaces the
// trimmed result in error envelopes), so the helper is
// intentionally not part of the public Store
// interface.
func normaliseName(s string) string {
	return strings.TrimSpace(s)
}
