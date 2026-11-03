// Package permissions — vocabulary.go: the reference's
// own permission vocabulary, and the one place where it meets the
// clone's.
//
// The clone's four dashboard methods (SetRemoteAgentPermissionPreset
// and friends) speak a three-value vocabulary the clone invented in
// cycle rc-permissions-grants: "default|strict|permissive" for the
// preset and "always|never|auto" for the artifact-review mode. The
// reference speaks a different one, and it is the one the IDE's
// workbench and the reference's own web UI speak:
//
//	exa.codeium_common_pb.AgentPermissionPreset
//	  AGENT_PERMISSION_PRESET_UNSPECIFIED  = 0
//	  AGENT_PERMISSION_PRESET_DEFAULT      = 1
//	  AGENT_PERMISSION_PRESET_REQUEST_REVIEW = 2
//	  AGENT_PERMISSION_PRESET_TURBO        = 3
//	  AGENT_PERMISSION_PRESET_VETTED       = 4
//	  AGENT_PERMISSION_PRESET_NONE         = 5
//	  AGENT_PERMISSION_PRESET_AUTO         = 6
//
//	exa.codeium_common_pb.ReviewMode
//	  ARTIFACT_REVIEW_MODE_UNSPECIFIED = 0
//	  ARTIFACT_REVIEW_MODE_ALWAYS      = 1
//	  ARTIFACT_REVIEW_MODE_TURBO       = 2
//	  ARTIFACT_REVIEW_MODE_AUTO        = 3
//
// 【一手】 2026-10-27: the FileDescriptorProto of
// third_party/jetski/codeium_common_pb/codeium_common.proto, read out
// of the agy 1.4.8 binary (file offset 0x3cd5130, 185 828 B). The
// numbers are the descriptor's own; the names are the strings the
// reference puts on the wire (proto-JSON spells an enum by its name).
//
// The grants travel as exa.codeium_common_pb.PermissionGrantsConfig —
// three lists of strings, `allow`, `deny` and `ask` — and a string is
// the reference's own grant string, "action(target)":
//
//	{"allow": ["command(git status)", "read_url(github.com)"]}
//
// 【一手】 the shape is the reference's: its own parser
// (shared.ParseGrantString) splits the action from the target and
// validates the action against a closed set, and its panels carry the
// format hint "Format: action(target)". See grant.go, which owns the
// grammar.
//
// The clone shipped a "tool:pattern" spelling of its own
// ("run_command:go test*"). It is kept as the *legacy* form: a
// permissions.json written before the grammar was corrected must not lose
// its rules, and the four dashboard methods speak it.
//
// The translation between the two vocabularies is a judgement, and it
// is marked as one. The names are first-hand; the pairing is not:
//
//   - default    ↔ AGENT_PERMISSION_PRESET_DEFAULT      【推测】
//   - strict     ↔ AGENT_PERMISSION_PRESET_REQUEST_REVIEW 【推测】
//   - permissive ↔ AGENT_PERMISSION_PRESET_NONE         【推测】
//   - always     ↔ ARTIFACT_REVIEW_MODE_ALWAYS          【推测】
//   - never      ↔ ARTIFACT_REVIEW_MODE_TURBO           【推测】
//   - auto       ↔ ARTIFACT_REVIEW_MODE_AUTO            【推测】
//
// The three review modes pair one to one with the reference's own
// three *display* names — "always-proceed", "agent-decides" and
// "asks-for-review" (【一手】 the /config panel's prose in the 1.4.8
// binary) — which is what fixes the direction: ALWAYS asks for the
// review, AUTO lets the agent decide, and TURBO proceeds without one.
//
// The presets have no such one-to-one anchor: the reference's display
// names for the Tool Execution Policy are four ("always-proceed",
// "request-review", "strict", "proceed-in-sandbox") against seven enum
// values, and which enum value each name selects was not traced. The
// four values the clone cannot express (TURBO, VETTED, AUTO and
// UNSPECIFIED) therefore land on the clone's nearest neighbour, and
// the pairing is 【推测】.
//
// Stdlib-only. No third-party deps.
package permissions

// The reference's preset enum, spelled as proto-JSON spells it.
//
// 【一手】 agy 1.4.8, third_party/jetski/codeium_common_pb/codeium_common.proto.
const (
	ReferencePresetUnspecified   = "AGENT_PERMISSION_PRESET_UNSPECIFIED"
	ReferencePresetDefault       = "AGENT_PERMISSION_PRESET_DEFAULT"
	ReferencePresetRequestReview = "AGENT_PERMISSION_PRESET_REQUEST_REVIEW"
	ReferencePresetTurbo         = "AGENT_PERMISSION_PRESET_TURBO"
	ReferencePresetVetted        = "AGENT_PERMISSION_PRESET_VETTED"
	ReferencePresetNone          = "AGENT_PERMISSION_PRESET_NONE"
	ReferencePresetAuto          = "AGENT_PERMISSION_PRESET_AUTO"
)

// The reference's artifact-review enum, spelled as proto-JSON spells it.
//
// 【一手】 agy 1.4.8, the same file.
const (
	ReferenceReviewUnspecified = "ARTIFACT_REVIEW_MODE_UNSPECIFIED"
	ReferenceReviewAlways      = "ARTIFACT_REVIEW_MODE_ALWAYS"
	ReferenceReviewTurbo       = "ARTIFACT_REVIEW_MODE_TURBO"
	ReferenceReviewAuto        = "ARTIFACT_REVIEW_MODE_AUTO"
)

// ReferencePresetNames maps the clone's preset onto the reference's
// enum name.
//
// 【推测】 on the pairing, 【一手】 on the names — see the file comment.
var ReferencePresetNames = map[Preset]string{
	PresetDefault:    ReferencePresetDefault,
	PresetStrict:     ReferencePresetRequestReview,
	PresetPermissive: ReferencePresetNone,
}

// ReferenceReviewNames maps the clone's review mode onto the
// reference's enum name.
//
// 【推测】 on the pairing, 【一手】 on the names.
var ReferenceReviewNames = map[ReviewMode]string{
	ReviewModeAlways: ReferenceReviewAlways,
	ReviewModeNever:  ReferenceReviewTurbo,
	ReviewModeAuto:   ReferenceReviewAuto,
}

// ClonePresetForReference maps the reference's preset enum name onto
// the clone's preset.
//
// The three values the clone models have an exact counterpart. The
// other four do not, and land on the nearest neighbour:
//
//	UNSPECIFIED  → default     (nothing was set)
//	TURBO        → permissive  (no interruption)   【推测】
//	VETTED       → strict      (every call vetted) 【推测】
//	AUTO         → default     (the host decides)   【推测】
var ClonePresetForReference = map[string]Preset{
	ReferencePresetUnspecified:   PresetDefault,
	ReferencePresetDefault:       PresetDefault,
	ReferencePresetRequestReview: PresetStrict,
	ReferencePresetTurbo:         PresetPermissive,
	ReferencePresetVetted:        PresetStrict,
	ReferencePresetNone:          PresetPermissive,
	ReferencePresetAuto:          PresetDefault,
}

// CloneReviewForReference maps the reference's review enum name onto
// the clone's review mode.
//
//	UNSPECIFIED → always (the clone's default) 【推测】
//	ALWAYS      → always
//	TURBO       → never  (proceed without a review) 【推测】
//	AUTO        → auto   (the agent decides)
var CloneReviewForReference = map[string]ReviewMode{
	ReferenceReviewUnspecified: ReviewModeAlways,
	ReferenceReviewAlways:      ReviewModeAlways,
	ReferenceReviewTurbo:       ReviewModeNever,
	ReferenceReviewAuto:        ReviewModeAuto,
}

// ReferencePresetName returns the reference's enum name for the
// clone's preset. An unknown preset (a value written by a newer
// clone) falls back to DEFAULT: the answer must always be a name the
// reference's decoder accepts.
func ReferencePresetName(p Preset) string {
	if name, ok := ReferencePresetNames[p]; ok {
		return name
	}
	return ReferencePresetDefault
}

// ReferenceReviewName returns the reference's enum name for the
// clone's review mode. An unknown mode falls back to ALWAYS.
func ReferenceReviewName(m ReviewMode) string {
	if name, ok := ReferenceReviewNames[m]; ok {
		return name
	}
	return ReferenceReviewAlways
}

// PresetFromReferenceName maps a reference preset enum name onto the
// clone's preset.
//
// The second result is false for a name the reference's enum does not
// have — the caller answers -32602 rather than guessing.
func PresetFromReferenceName(name string) (Preset, bool) {
	preset, ok := ClonePresetForReference[name]
	return preset, ok
}

// ReviewFromReferenceName maps a reference review enum name onto the
// clone's review mode.
//
// The second result is false for a name the reference's enum does not
// have.
func ReviewFromReferenceName(name string) (ReviewMode, bool) {
	mode, ok := CloneReviewForReference[name]
	return mode, ok
}

// ReferenceGrantKeys renders the clone's grants as the reference's
// grant strings — "action(target)".
//
// A grant that names an action is rendered the reference's way; a legacy
// grant — one that names a tool — keeps the clone's own "tool:pattern"
// spelling, because the reference has no form for it and inventing one
// would be a guess.
//
// 【一手】 the grammar; see grant.go.
func ReferenceGrantKeys(grants []Grant) []string {
	if len(grants) == 0 {
		return nil
	}
	out := make([]string, 0, len(grants))
	for _, g := range grants {
		out = append(out, g.GrantKey())
	}
	return out
}

// GrantsFromReferenceKeys parses the reference's grant strings into
// the clone's grants.
//
// Two forms are accepted: the reference's own "action(target)" and the
// clone's legacy "tool:pattern". A string that is neither is an error,
// and the error is the reference's own ("invalid grant string: %q",
// "unknown action %q in grant string: %q", or the sentence that refuses
// the retired `unsandboxed` action).
func GrantsFromReferenceKeys(keys []string) ([]Grant, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	out := make([]Grant, 0, len(keys))
	for _, key := range keys {
		g, err := ParseGrantKey(key)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, nil
}
