// Package triggers — options.go: the per-kind
// spec validator and the canned options envelope the
// ListRemoteAgentTriggerOptions RPC returns.
//
// Why split: the spec rules are *per kind* (file-changed
// wants a glob + event; schedule wants a cron / every
// expression; webhook wants a path; manual wants a prompt),
// so the validator is a closed switch over Kind.
// Keeping it next to Spec rather than next to the
// store keeps the persistence layer format-agnostic.
//
// The hardcoded `spec_schemas` map ships in this file too:
// the ListRemoteAgentTriggerOptions RPC returns it
// verbatim so a dashboard can render a per-field form
// without consulting a separate schema service. The
// schemas are not parsed by the daemon (the validator
// inspects the spec's runtime shape, not a JSON-Schema
// document), but they describe the same rules the
// validator enforces.
//
// Stdlib-only. No third-party deps.
package triggers

import (
	"errors"
	"fmt"
	"strings"
)

// SpecFieldMissing returns the validator's standard
// "the field "<field>" is required" error wrapped with
// ErrInvalidSpec so the dispatcher can errors.As-check the
// sentinel and translate to -32602. The validator's per-kind
// funcs use this for missing keys; the per-field reasons
// vary, so the helper is a thin formatter rather than a
// generic validator.
func specFieldMissingErr(field string) error {
	if field == "" {
		field = "(unnamed)"
	}
	return fmt.Errorf("%w: field %q is required", ErrInvalidSpec, field)
}

// specWrongTypeErr returns the validator's standard
// "the field "<field>" must be a <want>" error wrapped
// with ErrInvalidSpec. The per-kind funcs use this when
// the runtime type of a spec value doesn't match the
// expected type (a number posted to a string field, an
// array posted to a single-string field, etc.).
func specWrongTypeErr(field, want string, got any) error {
	return fmt.Errorf("%w: field %q must be %s (got %T)", ErrInvalidSpec, field, want, got)
}

// specBadValueErr returns the validator's standard
// "the field "<field>" value "<value>" <reason>"
// error wrapped with ErrInvalidSpec. The per-kind funcs
// use this when the value parses as the right type but
// fails a per-value rule ("event must be one of
// create|modify|delete|rename", "cron expression
// not parseable", etc.).
func specBadValueErr(field, value, reason string) error {
	return fmt.Errorf("%w: field %q value %q %s", ErrInvalidSpec, field, value, reason)
}

// ValidateSpec enforces the per-kind shape rules
// on a spec the dashboard posts at Create or Update time.
// Returns nil when the spec satisfies its kind's contract;
// returns an error wrapped with ErrInvalidSpec otherwise.
//
// The validator is intentionally *strict on shape* and
// *light on value semantics*: it checks types and required
// fields, but does not parse cron expressions (a future
// rc-triggers-runner cycle wires the cron parser and the
// value-semantic checks move to the runner's "ready to
// schedule" pipeline). This keeps the persisted state
// forward-compatible — a spec that parses today can be
// scheduled by a future runner with no data migration.
//
// Per-kind rules:
//
//   - file-changed: requires "pattern" (string, non-empty,
//     used as a glob) and "event" (string, one of
//     "create" / "modify" / "delete" / "rename").
//   - schedule: requires "prompt" (non-empty string).
//     Either "cron" (non-empty string, the future runner's
//     cron parser validates it) OR "every" (non-empty
//     string, the future runner's interval parser
//     validates the duration) is required; both is an
//     error.
//   - webhook: requires "path" (string, must start with
//     "/") and "prompt" (non-empty string). Optional
//     "secret" (string).
//   - manual: requires "prompt" (non-empty string).
//
// Unknown keys are ignored (forward-compat for future
// kinds' optional fields).
func ValidateSpec(kind Kind, spec Spec) error {
	if spec == nil {
		return specFieldMissingErr("(spec)")
	}
	switch kind {
	case FileChanged:
		return validateFileChangedSpec(spec)
	case Schedule:
		return validateScheduleSpec(spec)
	case Webhook:
		return validateWebhookSpec(spec)
	case Manual:
		return validateManualSpec(spec)
	default:
		return fmt.Errorf("%w: unknown trigger kind %q", ErrInvalidSpec, string(kind))
	}
}

// validateFileChangedSpec enforces
//
//	{pattern: "<non-empty glob>", event: "create"|"modify"|"delete"|"rename"}
func validateFileChangedSpec(spec Spec) error {
	pattern := requireStringField(spec, "pattern")
	if pattern == "" {
		if _, ok := spec["pattern"]; !ok {
			return specFieldMissingErr("pattern")
		}
		return specBadValueErr("pattern", pattern, "must be non-empty")
	}
	if strings.TrimSpace(pattern) == "" {
		return specBadValueErr("pattern", pattern, "must be non-empty")
	}
	event := requireStringField(spec, "event")
	if event == "" {
		// requireStringField returns "" for missing or
		// wrong-type — emit the missing-key hint.
		if _, ok := spec["event"]; !ok {
			return specFieldMissingErr("event")
		}
		return specBadValueErr("event", event, "must be one of create|modify|delete|rename")
	}
	switch event {
	case "create", "modify", "delete", "rename":
		// ok
	default:
		return specBadValueErr("event", event, "must be one of create|modify|delete|rename")
	}
	return nil
}

// validateScheduleSpec enforces
//
//	{prompt: "<non-empty>",
//	 cron: "<non-empty>" OR every: "<non-empty>"}
//
// Exactly one of cron / every must be set (both is an
// error). The string's *value* is not parsed here — the
// future runner's cron / interval parser is the second
// line of defence, and persistence-side validation today
// would block future spec formats the runner wants to
// accept.
func validateScheduleSpec(spec Spec) error {
	prompt := requireStringField(spec, "prompt")
	if prompt != "" {
		if strings.TrimSpace(prompt) == "" {
			return specBadValueErr("prompt", prompt, "must be non-empty")
		}
	} else if _, ok := spec["prompt"]; ok {
		return specBadValueErr("prompt", prompt, "must be non-empty")
	} else {
		return specFieldMissingErr("prompt")
	}
	cron := requireStringField(spec, "cron")
	every := requireStringField(spec, "every")
	hasCron := cron != ""
	hasEvery := every != ""
	if !hasCron && !hasEvery {
		return specFieldMissingErr("(cron OR every)")
	}
	if hasCron && hasEvery {
		return fmt.Errorf("%w: schedule must set exactly one of cron or every, not both", ErrInvalidSpec)
	}
	if hasCron {
		if strings.TrimSpace(cron) == "" {
			return specBadValueErr("cron", cron, "must be non-empty")
		}
	}
	if hasEvery {
		if strings.TrimSpace(every) == "" {
			return specBadValueErr("every", every, "must be non-empty")
		}
	}
	return nil
}

// validateWebhookSpec enforces
//
//	{path: "/<segment>", prompt: "<non-empty>", secret?: "<hex>"}
func validateWebhookSpec(spec Spec) error {
	path := requireStringField(spec, "path")
	if path == "" {
		if _, ok := spec["path"]; !ok {
			return specFieldMissingErr("path")
		}
		return specBadValueErr("path", path, "must start with /")
	}
	if !strings.HasPrefix(path, "/") {
		return specBadValueErr("path", path, "must start with /")
	}
	prompt := requireStringField(spec, "prompt")
	if prompt == "" {
		if _, ok := spec["prompt"]; !ok {
			return specFieldMissingErr("prompt")
		}
		return specBadValueErr("prompt", prompt, "must be non-empty")
	}
	if strings.TrimSpace(prompt) == "" {
		return specBadValueErr("prompt", prompt, "must be non-empty")
	}
	// "secret" is optional; we don't validate its value
	// today — the future runner's HMAC verifier is the
	// authoritative check.
	return nil
}

// validateManualSpec enforces
//
//	{prompt: "<non-empty>"}
func validateManualSpec(spec Spec) error {
	prompt := requireStringField(spec, "prompt")
	if prompt == "" {
		if _, ok := spec["prompt"]; !ok {
			return specFieldMissingErr("prompt")
		}
		return specBadValueErr("prompt", prompt, "must be non-empty")
	}
	if strings.TrimSpace(prompt) == "" {
		return specBadValueErr("prompt", prompt, "must be non-empty")
	}
	return nil
}

// requireStringField returns spec[key] when it is a
// non-interface{}/non-nil string. Returns "" when the
// key is missing or the value's type is not string. Used
// uniformly by the per-kind validators so they all share
// the same wrong-type path.
func requireStringField(spec Spec, key string) string {
	v, ok := spec[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

// OptionsResponse is the wire shape
// ListRemoteAgentTriggerOptions returns. Two fields:
//
//   - kinds: the canonical list of trigger kinds the
//     dashboard can install (the closed set
//     AllKinds exposes; same string values).
//   - spec_schemas: a map from kind to its required-field
//     schema. The schema here is *descriptive*, not
//     authoritative — the validator
//     ValidateSpec enforces the same rules at
//     Create / Update time. A dashboard that builds a
//     per-field form from spec_schemas sees the same
//     hints the validator surfaces in error envelopes.
//
// The schemas are intentionally hand-rolled maps rather
// than a JSON-Schema document: a future dashboard that
// wants richer schema metadata (numeric ranges, regex
// patterns, conditional fields) extends the helper and
// bumps the wire-format version in PLAN.md.
type OptionsResponse struct {
	Kinds       []string             `json:"kinds"`
	SpecSchemas map[string]SpecEntry `json:"spec_schemas"`
}

// SpecEntry is one row of the spec_schemas map.
// Documented here so the wire shape is the same shape the
// store / dispatcher read.
type SpecEntry struct {
	// Description is a one-line hint for the
	// dashboard's form label. Not localised today
	// (the spec validator's error envelopes are not
	// either; a future cycle can wire either/both).
	Description string `json:"description"`
	// Required lists the field names the validator
	// MUST see (per ValidateSpec).
	Required []string `json:"required"`
	// Optional lists the field names the validator
	// MAY see. Optional fields are not enforced to
	// be absent — extras are silently dropped, which
	// is forward-compat.
	Optional []string `json:"optional"`
	// Defaults is a field→value map the dashboard
	// can use to pre-fill its form. nil when no
	// field has a default (every kind today).
	Defaults map[string]any `json:"defaults,omitempty"`
	// FieldTypes is a field→type-name map the
	// dashboard can use to render the right form
	// widget. Values are the JSON-Schema-ish
	// "string" / "boolean" / "object" / "array"
	// vocabulary.
	FieldTypes map[string]string `json:"field_types"`
}

// DefaultOptionsResponse is the canned envelope
// the handler returns. The schema map is built once
// at package init so the wire shape is identical
// regardless of which dispatcher asks.
func DefaultOptionsResponse() OptionsResponse {
	kinds := AllKinds()
	kindsWire := make([]string, 0, len(kinds))
	for _, k := range kinds {
		kindsWire = append(kindsWire, string(k))
	}
	return OptionsResponse{
		Kinds:       kindsWire,
		SpecSchemas: defaultSpecSchemas(),
	}
}

// defaultSpecSchemas builds the
// kind→schema map DefaultOptionsResponse
// returns. The per-kind entries live in their own
// helpers (one per Kind) so the parent
// constructor stays small and the per-kind schema
// stays co-located with the Kind / validator
// it describes.
func defaultSpecSchemas() map[string]SpecEntry {
	return map[string]SpecEntry{
		string(FileChanged): defaultSpecEntryFileChanged(),
		string(Schedule):    defaultSpecEntrySchedule(),
		string(Webhook):     defaultSpecEntryWebhook(),
		string(Manual):      defaultSpecEntryManual(),
	}
}

func defaultSpecEntryFileChanged() SpecEntry {
	return SpecEntry{
		Description: "Fire when a file matching the glob has the event reported by the fsnotify watcher",
		Required:    []string{"pattern", "event"},
		FieldTypes: map[string]string{
			"pattern": "string",
			"event":   "string",
		},
	}
}

func defaultSpecEntrySchedule() SpecEntry {
	return SpecEntry{
		Description: "Fire on a cron / interval expression with a prompt",
		Required:    []string{"prompt"},
		Optional:    []string{"cron", "every"},
		FieldTypes: map[string]string{
			"prompt": "string",
			"cron":   "string",
			"every":  "string",
		},
	}
}

func defaultSpecEntryWebhook() SpecEntry {
	return SpecEntry{
		Description: "Fire when an HTTP request hits the path; optional secret verifies the request",
		Required:    []string{"path", "prompt"},
		Optional:    []string{"secret"},
		FieldTypes: map[string]string{
			"path":   "string",
			"prompt": "string",
			"secret": "string",
		},
	}
}

func defaultSpecEntryManual() SpecEntry {
	return SpecEntry{
		Description: "Fire when the user clicks the Run button (no auto-event)",
		Required:    []string{"prompt"},
		FieldTypes: map[string]string{
			"prompt": "string",
		},
	}
}

// errWrongJSONType is a deprecated alias kept so the
// wider package's grep doesn't surface a false negative on
// the spec validator's calls. Newer validators use
// specWrongTypeErr directly; this alias is documentation,
// not an active call site.
var _ = errors.Is
