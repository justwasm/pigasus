// Package permissions — permissions.go: the permission-state
// types and the Store interface that the four
// JSON-RPC methods introduced in cycle rc-permissions-grants
// call into.
//
// Why split: the four JSON-RPC methods
// (SetRemoteAgentPermissionPreset /
// SetRemoteAgentArtifactReviewMode /
// AddRemoteAgentPermissionGrants /
// RemoveRemoteAgentPermissionGrants) share a tiny CRUD-shaped
// seam they all talk to; the dispatcher does NOT know about
// the on-disk format. Putting the types in their own file
// keeps the per-handler bodies in dispatcher.go
// focused on the JSON-RPC envelope shape, and lets the
// storage layer (store_mem.go / store_fs.go) keep talking to a
// stable interface without re-touching the dispatcher.
//
// Storage shape:
//
//	$ANTIGRAVITY_HOME/permissions.json
//
// One file, versioned ({"version":1, ...}). The file format
// is intentionally forward-compatible so future cycles can
// add fields without rewriting history (the version stamp
// bumps only on breaking changes).
//
// Cycle rc-permissions-grants adds four JSON-RPC methods
// and one storage seam. The seam is shaped so a future
// cycle (e.g. ListRemoteAgentPermissionGrants — not in this
// brief) can extend it without re-touching the dispatcher.
//
// Stdlib-only. No third-party deps.
package permissions

import (
	"context"
	"errors"
	"strings"
	"time"
)

// Preset is the closed set of presets the dashboard
// can choose between via SetRemoteAgentPermissionPreset.
//
// The three constants are the values the brief pins; the
// closed set is what IsValidPreset checks before the
// dispatcher hands the value to the store (an unknown preset
// returns -32602 "Invalid params" without ever touching the
// store seam).
//
// Wire shape is the bare string ("default" / "strict" /
// "permissive"). Cycle rc-permissions-grants pins the
// vocabulary to these three values; a future cycle that wants
// more presets extends the constants and bumps the
// permissions.json on-disk "version".
type Preset string

const (
	// PresetDefault is the host's policy-baseline.
	// Resolved by the agent loop when a tool call would
	// normally prompt the user; the dashboard surface here
	// is just "the user wants the host's default".
	PresetDefault Preset = "default"
	// PresetStrict refuses every tool call that
	// would normally require user confirmation (the same
	// prompt-eligibility list the host's stream wraps with
	// store.waitForEligibility; see cycle 133).
	PresetStrict Preset = "strict"
	// PresetPermissive auto-allows every tool
	// call without confirmation. The brief reserves the
	// value for dashboards that want a sandbox-free
	// workflow; the agent loop still enforces the
	// sandbox flag's filesystem policy, so a permissive
	// user is not "anything goes" — only "no confirmation
	// prompts".
	PresetPermissive Preset = "permissive"
)

// IsValidPreset reports whether preset is one of the three
// constants above. The dispatcher uses this to map unknown
// values to -32602 before the store call.
func IsValidPreset(p Preset) bool {
	switch p {
	case PresetDefault,
		PresetStrict,
		PresetPermissive:
		return true
	}
	return false
}

// ReviewMode is the closed set of modes the
// dashboard can choose between via
// SetRemoteAgentArtifactReviewMode. Decides how the host
// treats generated artifacts before they reach the user —
// whether the dashboard sees an intermediate "review this
// artifact" step (always / never / auto).
//
// Wire shape is the bare string ("always" / "never" /
// "auto"). The closed set is what IsValidReviewMode
// checks; an unknown mode returns -32602 "Invalid params"
// without touching the store.
type ReviewMode string

const (
	// ReviewModeAlways forces every artifact the
	// host produces to wait for an explicit human review
	// before delivery. Matches the pre-r266 default.
	ReviewModeAlways ReviewMode = "always"
	// ReviewModeNever skips the review step
	// entirely. The dashboard is responsible for any
	// downstream filtering; the host will not gate
	// artifacts at all.
	ReviewModeNever ReviewMode = "never"
	// ReviewModeAuto lets the host decide per
	// artifact — e.g. known-safe shell echoes bypass the
	// gate but file-edits hold for review. The exact
	// heuristic is host-side; the dashboard's job is
	// just to set the policy and read whatever the
	// host surfaces.
	ReviewModeAuto ReviewMode = "auto"
)

// IsValidReviewMode reports whether mode is one of the
// three constants above. The dispatcher uses this to map
// unknown values to -32602 before the store call.
func IsValidReviewMode(m ReviewMode) bool {
	switch m {
	case ReviewModeAlways,
		ReviewModeNever,
		ReviewModeAuto:
		return true
	}
	return false
}

// Grant is one rule the user has auto-allowed (or revoked).
// Persisted in the permissions.json's `grants` array; the
// dashboard's AddRemoteAgentPermissionGrants / Remove methods
// maintain the slice.
//
// Field semantics:
//
//   - Action: the reference's grant action — the "command" of
//     "command(git status)". Empty for a legacy grant.
//
//   - ToolName: the canonical tool id the host's tool
//     catalogue recognises (the same string the model
//     emits when invoking the tool). Required for a legacy
//     grant, empty for a grant that names an action.
//
//   - Pattern: the per-tool glob / prefix the grant
//     applies to. Required. The brief's "tool_patterns"
//     wire field maps here; we use Pattern in the Go
//     shape so a dashboard reading the project files
//     doesn't have to translate JSON snake_case into
//     Go camelCase.
//
//   - Reason: optional human-readable rationale (e.g.
//     "USER approved during exec of 'npm install' at
//     2026-10-19T10:11:12Z"). Surfaced in the wire
//     envelope so a dashboard can show users why each
//     grant exists.
//
//   - AddedAt: UTC stamp the store stamped when the
//     grant was first added (constant across edits —
//     re-adding an existing grant leaves AddedAt at
//     its original value).
//
// (action, pattern) — or (tool_name, pattern) for a legacy
// grant — is the dedup key. Two grants with the same pair
// are considered identical: the store refuses to add a
// duplicate (returns ErrGrantExists), and Remove parses
// the pair off the same encoding.
type Grant struct {
	Action   string    `json:"action,omitempty"`
	ToolName string    `json:"tool_name"`
	Pattern  string    `json:"pattern"`
	Reason   string    `json:"reason,omitempty"`
	AddedAt  time.Time `json:"added_at"`
}

// GrantKey returns the dedup key for a grant. Used by the
// store's duplicate-add guard and by Remove's encoding rule.
//
// A grant that names an action is keyed the reference's way:
//
//	"command(git status)"
//
// A legacy grant — one that names a tool instead — keeps the
// clone's own encoding:
//
//	"run_command:go test*"
//
// The colon separator is the one cycle-3a's
// CancelRemoteAgentCascade envelope uses for
// "interaction_id:<id>"'s encoding rule. A future
// cycle that adds more fields to the key (e.g.
// scope) extends this function and bumps the JSON
// version.
func (g Grant) GrantKey() string {
	return grantKeyFor(g.Action, g.ToolName, g.Pattern)
}

// State is the on-disk / wire shape the JSON
// file holds and the dispatcher hands back. One
// instance per $ANTIGRAVITY_HOME/permissions.json; the
// store is single-document by design — there is no
// "list of states" because there is exactly one state
// per host.
//
// The struct is intentionally JSON-serialised with
// lowercase keys so the on-disk format matches the wire
// format the dashboard reads. The Version field is
// bumped on breaking changes (the brief pins v1).
type State struct {
	Version   int        `json:"version"`
	Preset    Preset     `json:"preset"`
	Review    ReviewMode `json:"review"`
	Grants    []Grant    `json:"grants"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// Store is the seam the four JSON-RPC
// methods call into. Cycle rc-permissions-grants adds
// Get / SetPreset / SetReview / AddGrant /
// RemoveGrants; future cycles extend it with
// ListGrants / ClearGrants as the brief is silent on
// those today.
//
// Get returns the current state. The dispatcher never
// checks for a "not-found" envelope — a fresh host with
// no permissions.json yet returns the default state
// (preset=default, review=always, grants=[]) so the
// dashboard always has a coherent answer.
//
// SetPreset / SetReview: bump UpdatedAt to the wall
// clock on success; the dispatcher uses UpdatedAt to
// render the wire envelope.
//
// AddGrant takes one Grant at a time. The
// store rejects a duplicate grant (the same
// action-or-tool and pattern) with
// ErrGrantExists; updates to an existing
// grant's Reason are allowed (the grant's AddedAt
// stays at its original value). Returns the
// post-mutation state.
//
// RemoveGrants parses the supplied strings — the
// reference's "action(target)" grant, or the legacy
// "<tool_name>:<pattern>" one — and removes every
// grant whose key matches. A string that is neither
// surfaces as ErrGrantMalformed. Unknown keys are
// silently dropped (the dashboard asked to remove
// something that wasn't there — idempotent). Returns
// the post-mutation state.
//
// Both Add and Remove take a slice so a dashboard can
// batch-update in a single round-trip — the wire
// spec's "tool_patterns": ["a:1","b:2"] shape fits
// directly. A nil or empty slice is a no-op
// (ReturnCurrentState, no error).
type Store interface {
	Get(ctx context.Context) (State, error)
	SetPreset(ctx context.Context, preset Preset) (State, error)
	SetReview(ctx context.Context, mode ReviewMode) (State, error)
	AddGrants(ctx context.Context, grants []Grant) (State, error)
	RemoveGrants(ctx context.Context, keys []string) (State, error)
}

// ErrGrantExists is the sentinel
// Store.AddGrants returns when the caller
// tries to add a grant whose (tool_name, pattern) pair
// is already in the store. The dispatcher translates to
// JSON-RPC -32014 "Permission grant exists".
//
// Cycle rc-permissions-grants adds this code; prior
// cycles parked the destructive method without
// defining the sentinel.
var ErrGrantExists = errors.New("rc-permissions: grant already exists")

// ErrGrantMalformed is the sentinel
// Store.RemoveGrants returns when one or
// more of the supplied keys did not parse as
// a grant — neither the reference's
// "action(target)" nor the legacy
// "<tool_name>:<pattern>". The dispatcher translates
// to JSON-RPC -32602 "Invalid params" — the
// distinction from -32014 (where the key is well-
// formed but already present) matters: a malformed
// key is a dashboard bug, a duplicate is a dashboard
// race.
var ErrGrantMalformed = errors.New("rc-permissions: grant key malformed")

// StateVersion is the on-disk format version
// the current store writes. Bumped on breaking
// changes (renamed keys, removed fields); non-breaking
// additions (new optional fields with omitempty) leave
// the version alone so older readers still parse the
// file.
//
// v1 (cycle rc-permissions-grants): the initial
// layout. See State for the field set.
const StateVersion = 1

// DefaultPreset / DefaultReviewMode are
// the values a fresh host starts with when no
// permissions.json exists. The dispatcher's
// permissionSnapshot helper returns a state with these
// defaults when the store reports "not found" or has no
// Grant slice in the file; the dashboard always sees
// a coherent state.
const (
	DefaultPreset     Preset     = PresetDefault
	DefaultReviewMode ReviewMode = ReviewModeAlways
)

// normaliseToolName trims whitespace from the user-
// supplied tool name. Empty after trim → "". Used by
// both the dispatcher layer (before calling
// AddGrants) and the store layer (when comparing
// for duplicates). Returns "" when the trimmed
// result is empty so the same check trips everywhere.
func normaliseToolName(name string) string {
	return strings.TrimSpace(name)
}

// normaliseAction trims whitespace from the user-supplied
// grant action ("command" in "command(git status)").
//
// The action is not lower-cased: the reference's action set
// is lower-case and a rule that spells it differently is a
// rule the reference does not know — the reference refuses
// it with "unknown action %q in grant string: %q", and
// lower-casing it here would silently accept it.
func normaliseAction(action string) string {
	return strings.TrimSpace(action)
}

// normalisePattern trims whitespace from the user-
// supplied pattern glob. Empty after trim → "".
func normalisePattern(pattern string) string {
	return strings.TrimSpace(pattern)
}

// ParseToolPattern parses the legacy "<tool_name>:<pattern>"
// encoding. Splits on the FIRST
// colon so a tool name with a colon (rare but legal)
// preserves the colon in the pattern; a leading colon
// (":<pattern>") returns ("", pattern) so the
// dispatcher can surface the malformed-shape
// envelope.
//
// Returns ok=true when both halves are non-empty
// after trim. ok=false on empty/missing colon.
//
// This is the *legacy* grammar. The reference's own is
// "action(target)" — ParseGrantString in grant.go — and
// ParseGrantKey accepts both.
func ParseToolPattern(s string) (tool, pattern string, ok bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", "", false
	}
	idx := strings.IndexByte(s, ':')
	if idx < 0 {
		return "", "", false
	}
	tool = normaliseToolName(s[:idx])
	pattern = normalisePattern(s[idx+1:])
	if tool == "" || pattern == "" {
		return "", "", false
	}
	return tool, pattern, true
}
