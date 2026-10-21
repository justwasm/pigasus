// Package remotecontrol — conversation_config.go: the
// per-conversation config data model + sentinel errors
// for the JSON-RPC method
// `UpsertRemoteAgentConversationConfig` introduced in
// cycle rc-conversation-config.
//
// Why split (rather than embedding in
// conversation_config_store.go): the dispatcher
// doesn't care about the on-disk format, and keeping
// the per-record types + sentinels in a single small
// file lets the storage layer
// (conversation_config_store.go) and the per-method
// handler (conversation_config_dispatcher.go) talk to
// a stable interface without re-touching each other
// when the wire shape evolves.
//
// Data model (mirror the brief):
//
//   - ConversationConfig — one row of the
//     per-conversation override the dashboard can
//     upsert. The brief's fields are model,
//     model_effort, system_prompt_patch,
//     allowed_tools, disallowed_tools, max_turns,
//     temperature, top_p, metadata, updated_at.
//
//   - ConversationConfigState — the on-disk / wire
//     envelope the store holds. Versioned
//     ({"version":1, "configs":{<id>: ...},
//     "updated_at":...}).
//
//   - ConversationConfigStore — the seam the single
//     UpsertRemoteAgentConversationConfig handler
//     calls. Upsert / Get / Snapshot. The dispatcher's
//     "created:true|false" flag is the boolean return
//     the Upsert call yields.
//
// Sentinel-error map (mirrors the rc-resources /
// rc-fastpush-toggle cycle pattern):
//
//	(other / I/O) → -32603 "Internal error"
//
// -32602 ("Invalid params") is emitted directly from
// the per-method param-validation path before calling
// the store; the helper doesn't see it.
//
// Stdlib-only. No third-party deps.
package remotecontrol

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// AllModelEfforts is the canonical closed-set
// `model_effort` enum the dashboard can post. The
// brief pins three values
// ("low" / "medium" / "high"); an unknown effort is
// rejected as -32602 (the brief's pre-flight
// validation rule). Insertion order is intentional
// (matches the rc-triggers / rc-resources
// conventions: cheapest first).
var AllModelEfforts = []string{
	ModelEffortLow,
	ModelEffortMedium,
	ModelEffortHigh,
}

// Closed-set enum constants. The brief calls out the
// "low|medium|high" set verbatim; a future cycle that
// adds (e.g.) "auto" or "thinking" only has to
// extend this slice + the AllModelEfforts /
// IsValidModelEffort helpers.
const (
	ModelEffortLow    = "low"
	ModelEffortMedium = "medium"
	ModelEffortHigh   = "high"
)

// IsValidModelEffort reports whether e is one of the
// closed-set values in AllModelEffort. A closed-set
// validator that returns false flips the dispatcher's
// error mapping to -32602 (matches the
// IsValidResourceKind / IsValidTriggerKind rules
// the previous cycles established).
func IsValidModelEffort(e string) bool {
	for _, v := range AllModelEfforts {
		if v == e {
			return true
		}
	}
	return false
}

// ConversationConfig is one row of the per-
// conversation override the dashboard can upsert.
// Fields mirror the brief:
//
//   - Model: the dashboard's preferred model
//     ("claude-3-7-sonnet" / "gemini-3.8-flash" /
//     ""). Free-form string — the brief does NOT
//     pin a closed-set; the cycle defers model-id
//     validation to the downstream `rc-conversation-
//     config-read` cycle (which adds the
//     GetRemoteAgentConversationConfig call that
//     reads it back).
//
//   - ModelEffort: closed-set
//     ("low"|"medium"|"high"). IsValidModelEffort
//     guards.
//
//   - SystemPromptPatch: a string the
//     pipeline prepends to the host's default system
//     prompt for this conversation. Today's contract
//     is opaque (the clone doesn't yet render this —
//     a future cycle wires the actual pre-pending;
//     today the value persists but doesn't change
//     behavior).
//
//   - AllowedTools: an optional allowlist of tool
//     names the pipeline will accept from the
//     conversation. nil → "no override".
//
//   - DisallowedTools: an optional blocklist of tool
//     names the pipeline will refuse. nil → "no
//     override".
//
//   - MaxTurns: bounded int [0,1000]. The brief pins
//     the upper bound at 1000; zero or negative is
//     also rejected (-32602 "Invalid params").
//
//   - Temperature: float64 ∈ [0,2]. The brief pins
//     the upper bound at 2; values outside that
//     range are rejected.
//
//   - TopP: float64 ∈ [0,1]. The brief pins the
//     upper bound at 1; values outside that range
//     are rejected.
//
//   - Metadata: an opaque map of dashboard-supplied
//     string → string. Today the cycle doesn't read
//     any specific key — the map persists, the
//     future `rc-conversation-config-read` cycle
//     surfaces it back to the dashboard.
//
//   - UpdatedAt: RFC3339Nano UTC stamp the store
//     stamped at the most-recent Upsert. The store
//     stamps this server-side; a dashboard that
//     posts a pre-populated value gets the value
//     overwritten with the server's clock.
type ConversationConfig struct {
	// Model is the dashboard's preferred model
	// label. Empty means "no override". Free-form
	// string — the cycle does not validate against
	// the host's model catalog (deferred to a
	// future read cycle).
	Model string `json:"model,omitempty"`
	// ModelEffort is the closed-set
	// "low"|"medium"|"high" enum. Empty means
	// "no override". IsValidModelEffort guards.
	ModelEffort string `json:"model_effort,omitempty"`
	// SystemPromptPatch is a string the future
	// pipeline-rendering cycle prepends to the
	// host's default system prompt. Empty means
	// "no override".
	SystemPromptPatch string `json:"system_prompt_patch,omitempty"`
	// AllowedTools is the optional tool
	// allowlist. nil → "no override".
	AllowedTools []string `json:"allowed_tools,omitempty"`
	// DisallowedTools is the optional tool
	// blocklist. nil → "no override".
	DisallowedTools []string `json:"disallowed_tools,omitempty"`
	// MaxTurns is the bounded turn cap
	// [0, 1000]. 0 → "no override"; > 1000 →
	// -32602.
	MaxTurns int `json:"max_turns,omitempty"`
	// Temperature is the bounded float ∈ [0, 2].
	// 0 → "no override"; out of range → -32602.
	Temperature float64 `json:"temperature,omitempty"`
	// TopP is the bounded float ∈ [0, 1].
	// 0 → "no override"; out of range → -32602.
	TopP float64 `json:"top_p,omitempty"`
	// Metadata is the opaque dashboard-supplied
	// key/value map. nil → "no override".
	Metadata map[string]string `json:"metadata,omitempty"`
	// UpdatedAt is when the store recorded the
	// most-recent Upsert. RFC3339Nano UTC.
	UpdatedAt time.Time `json:"updated_at"`
}

// ConversationConfigState is the on-disk / wire
// envelope the JSON file holds. One instance per
// $ANTIGRAVITY_HOME/conversation_config.json; the
// store is single-document by design — there is no
// "list of states" because there is exactly one
// registry per host.
//
// The struct is intentionally JSON-serialised with
// lowercase keys so the on-disk format matches the
// wire format the dashboard reads. The Version
// field is bumped on breaking changes (the brief
// pins v1).
type ConversationConfigState struct {
	// Version is the on-disk format version.
	// Bumped on breaking changes (renamed keys,
	// removed fields); non-breaking additions
	// (new optional fields with omitempty) leave
	// the version alone so older readers still
	// parse the file.
	Version int `json:"version"`
	// Configs is the live config map, keyed by
	// conversation_id (the dashboard's stable
	// handle). nil → empty so the wire always sees
	// "{}".
	Configs map[string]ConversationConfig `json:"configs"`
	// UpdatedAt is the wall-clock stamp the store
	// bumped at the most-recent Upsert.
	// RFC3339Nano UTC.
	UpdatedAt time.Time `json:"updated_at"`
}

// ConversationConfigStore is the seam the single
// UpsertRemoteAgentConversationConfig handler
// calls. Cycle rc-conversation-config adds Upsert
// / Get / Snapshot; future cycles
// (rc-conversation-config-read) will extend it
// with a List-by-prefix + a per-config delete so
// dashboards can manage the registry without
// upserting an empty config.
//
// Upsert writes the supplied config for convID
// and returns (created, err):
//
//   - created: true when no prior config exists
//     for convID (insert); false when an existing
//     config was replaced (update).
//   - err: ErrConversationConfigPersistenceFailed
//     on an on-disk write failure (the dispatcher
//     maps that to -32603 "Internal error").
//
// The store stamps UpdatedAt server-side so a
// dashboard that posts a pre-populated value gets
// the value overwritten with the server's clock
// (matches the rc-fastpush-toggle / rc-resources
// precedent — the brief pins server-side
// stamping).
//
// Get returns the config for convID. Returns
// (zero, false) when no row exists. Today's
// contract is non-mutating; the dispatcher
// doesn't currently call Get (the brief is
// write-only), but the helper exists so the
// future read cycle can wire it without
// re-touching the store.
//
// Snapshot returns the full state. A fresh host
// with no conversation_config.json yet returns
// the empty state (version=1, configs={}).
//
// All three methods take a context the dispatcher
// already passes through; the FS impl uses an
// in-process mutex + synchronous write, the
// signature is future-proof for a cycle that
// promotes the write path to async / batched.
type ConversationConfigStore interface {
	Upsert(ctx context.Context, convID string, c ConversationConfig) (created bool, err error)
	Get(ctx context.Context, convID string) (ConversationConfig, bool)
	Snapshot(ctx context.Context) (ConversationConfigState, error)
}

// ErrConversationConfigPersistenceFailed is the
// sentinel the FSConversationConfigStore.Upsert
// call returns when the on-disk write fails (the
// FS impl does not return errors for the in-memory
// MemConversationConfigStore — the store's write
// path is a synchronous map mutation).
//
// The dispatcher maps this to JSON-RPC -32603
// "Internal error". The brief calls out -32603
// explicitly for "store persistence failed".
//
// Cycle rc-conversation-config adds this sentinel.
var ErrConversationConfigPersistenceFailed = errors.New("rc-conversation-config: persistence failed")

// ConversationConfigStateVersion is the on-disk
// format version the current store writes. Bumped
// on breaking changes (renamed keys, removed
// fields); non-breaking additions (new optional
// fields with omitempty) leave the version alone.
//
// v1 (cycle rc-conversation-config): the initial
// layout.
const ConversationConfigStateVersion = 1

// NotifConversationConfigChangedMethod is the
// JSON-RPC method name the dispatcher uses for the
// server-push notification emitted on every
// successful Upsert. Wired via
// Dispatcher.BroadcastNotify(method, params) once
// per mutated conn.
//
// A `conversation_config/changed` notification
// carries the same shape the response envelope
// emits (see conversationConfigChangedToWire
// below) so a polling caller and a notification
// subscriber see byte-for-byte identical frames.
const NotifConversationConfigChangedMethod = "conversation_config/changed"

// MaxConversationConfigMaxTurns is the upper bound
// for ConversationConfig.MaxTurns. The brief pins
// the limit at 1000; values above that are
// rejected as -32602 "Invalid params".
const MaxConversationConfigMaxTurns = 1000

// MaxConversationConfigTemperature is the upper
// bound for ConversationConfig.Temperature. The
// brief pins the limit at 2; values above that
// are rejected as -32602. The lower bound is 0;
// the brief uses [0, 2] inclusive.
const MaxConversationConfigTemperature = 2.0

// MaxConversationConfigTopP is the upper bound for
// ConversationConfig.TopP. The brief pins the
// limit at 1; values above that are rejected as
// -32602. The lower bound is 0; the brief uses
// [0, 1] inclusive.
const MaxConversationConfigTopP = 1.0

// normaliseConversationID trims whitespace from
// the user-supplied conversation_id. Empty after
// trim → "". Used by both the dispatcher layer
// (before calling Upsert / Get) and the store
// layer (when comparing for membership). Returns
// "" when the trimmed result is empty so the
// same check trips everywhere.
//
// Today the store does not auto-normalise (an
// Upsert that passes "  conv_xxx  " and "conv_xxx"
// is considered distinct), but the helper is here
// so a future cycle that wants server-side
// canonicalisation only has to swap the lookup
// line in conversation_config_store.go for
// normaliseConversationID(...). The dispatcher
// path is the only validation site today.
func normaliseConversationID(id string) string {
	return strings.TrimSpace(id)
}

// ValidateConversationConfig checks the supplied
// config against the brief's validation rules.
// Returns nil on success; returns a non-nil error
// whose Error() is the wire-envelope `data`
// field the dispatcher surfaces as -32602.
//
// Rules:
//
//   - ModelEffort: empty → ok ("no override").
//     Non-empty → must be in AllModelEfforts;
//     unknown value → -32602.
//
//   - MaxTurns: 0 → ok ("no override"). Positive
//     values ≤ 1000 → ok. Negative or > 1000 →
//     -32602.
//
//   - Temperature: 0 → ok. ∈ [0, 2] inclusive →
//     ok. Out of range → -32602.
//
//   - TopP: 0 → ok. ∈ [0, 1] inclusive → ok. Out
//     of range → -32602.
//
//   - Metadata: nil / empty → ok. Keys and values
//     are opaque strings; the cycle does not
//     validate the values today (a future cycle
//     that adds a typed-key closed-set can extend
//     this without re-touching the dispatcher).
//
// The validator is intentionally a single
// function (rather than a per-field method on
// ConversationConfig) so a future caller — a CLI
// flag parser, a script — can re-use it without
// instantiating a ConversationConfig.
func ValidateConversationConfig(c ConversationConfig) error {
	if c.ModelEffort != "" && !IsValidModelEffort(c.ModelEffort) {
		return fmt.Errorf("model_effort must be one of low|medium|high")
	}
	if c.MaxTurns < 0 {
		return fmt.Errorf("max_turns must be >= 0")
	}
	if c.MaxTurns > MaxConversationConfigMaxTurns {
		return fmt.Errorf("max_turns must be <= %d", MaxConversationConfigMaxTurns)
	}
	if c.Temperature < 0 {
		return fmt.Errorf("temperature must be >= 0")
	}
	if c.Temperature > MaxConversationConfigTemperature {
		return fmt.Errorf("temperature must be <= %g", MaxConversationConfigTemperature)
	}
	if c.TopP < 0 {
		return fmt.Errorf("top_p must be >= 0")
	}
	if c.TopP > MaxConversationConfigTopP {
		return fmt.Errorf("top_p must be <= %g", MaxConversationConfigTopP)
	}
	return nil
}

// conversationConfigChangedToWire renders the
// `conversation_config/changed` notification
// params. The brief's wire envelope is:
//
//	{
//	  "ok":             true,
//	  "conversation_id":"<id>",
//	  "created":        <bool>,
//	  "updated_at":     "<RFC3339>"
//	  "config":         {<full ConversationConfig echo>}
//	}
//
// The same helper renders the
// UpsertRemoteAgentConversationConfig response
// envelope so a polling caller and a notification
// subscriber see byte-for-byte identical shapes.
// Mirrors broadcastFastpushSettingChanged /
// broadcastResourcesChanged /
// broadcastPluginsChanged /
// broadcastTriggersChanged (the rc-plugins /
// rc-triggers / rc-fastpush-toggle / rc-resources
// precedent).
func conversationConfigChangedToWire(convID string, created bool, c ConversationConfig) map[string]any {
	// Always emit a non-nil config map so the wire
	// shows "{}" rather than "null" when the
	// dashboard upserted an entirely-empty config
	// (all omitempty fields). The future
	// rc-conversation-config-read cycle will read
	// this verbatim.
	if c.Metadata == nil {
		c.Metadata = map[string]string{}
	}
	return map[string]any{
		"ok":              true,
		"conversation_id": convID,
		"created":         created,
		"config":          c,
		"updated_at":      c.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

// mapConversationConfigError maps the
// ConversationConfigStore sentinel errors to the
// JSON-RPC envelopes the brief calls for:
//
//	ErrConversationConfigPersistenceFailed → -32603 "Internal error"
//	(other)                                → -32603 "Internal error"
//
// op is the operation label so a dashboard log
// shows which stage failed. Mirrors
// mapFastpushError / mapResourcesError /
// mapPluginsError / mapTriggersError's shape;
// the preceding cycles established the pattern.
//
// -32602 ("Invalid params") is shared with every
// other method's "bad input" envelope and is
// emitted directly from the handler, not via this
// helper.
func mapConversationConfigError(err error, op string) *rpcErrorBody {
	if err == nil {
		return nil
	}
	return &rpcErrorBody{Code: -32603, Message: "Internal error", Data: op + ": " + err.Error()}
}
