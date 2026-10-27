// Package remotecontrol — permission_vocabulary.go: the reference's
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
//	exa.codeium_common_pb.ArtifactReviewMode
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
// a "<tool>:<pattern>" rule:
//
//	{"allow": ["run_command:go test*", "view_file"]}
//
// 【一手】 the reference's own settings.json carries the same rules
// under `permissions.allow` (internal/permissions/settings.go reads
// them), so the "tool:pattern" spelling is the reference's, not the
// clone's.
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
package remotecontrol

// The reference's preset enum, spelled as proto-JSON spells it.
//
// 【一手】 agy 1.4.8, third_party/jetski/codeium_common_pb/codeium_common.proto.
const (
	referencePresetUnspecified   = "AGENT_PERMISSION_PRESET_UNSPECIFIED"
	referencePresetDefault       = "AGENT_PERMISSION_PRESET_DEFAULT"
	referencePresetRequestReview = "AGENT_PERMISSION_PRESET_REQUEST_REVIEW"
	referencePresetTurbo         = "AGENT_PERMISSION_PRESET_TURBO"
	referencePresetVetted        = "AGENT_PERMISSION_PRESET_VETTED"
	referencePresetNone          = "AGENT_PERMISSION_PRESET_NONE"
	referencePresetAuto          = "AGENT_PERMISSION_PRESET_AUTO"
)

// The reference's artifact-review enum, spelled as proto-JSON spells it.
//
// 【一手】 agy 1.4.8, the same file.
const (
	referenceReviewUnspecified = "ARTIFACT_REVIEW_MODE_UNSPECIFIED"
	referenceReviewAlways      = "ARTIFACT_REVIEW_MODE_ALWAYS"
	referenceReviewTurbo       = "ARTIFACT_REVIEW_MODE_TURBO"
	referenceReviewAuto        = "ARTIFACT_REVIEW_MODE_AUTO"
)

// referencePresetNames maps the clone's preset onto the reference's
// enum name.
//
// 【推测】 on the pairing, 【一手】 on the names — see the file comment.
var referencePresetNames = map[PermissionPreset]string{
	PermissionPresetDefault:    referencePresetDefault,
	PermissionPresetStrict:     referencePresetRequestReview,
	PermissionPresetPermissive: referencePresetNone,
}

// referenceReviewNames maps the clone's review mode onto the
// reference's enum name.
//
// 【推测】 on the pairing, 【一手】 on the names.
var referenceReviewNames = map[ArtifactReviewMode]string{
	ArtifactReviewModeAlways: referenceReviewAlways,
	ArtifactReviewModeNever:  referenceReviewTurbo,
	ArtifactReviewModeAuto:   referenceReviewAuto,
}

// clonePresetForReference maps the reference's preset enum name onto
// the clone's preset.
//
// The three values the clone models have an exact counterpart. The
// other four do not, and land on the nearest neighbour:
//
//	UNSPECIFIED  → default     (nothing was set)
//	TURBO        → permissive  (no interruption)   【推测】
//	VETTED       → strict      (every call vetted) 【推测】
//	AUTO         → default     (the host decides)   【推测】
var clonePresetForReference = map[string]PermissionPreset{
	referencePresetUnspecified:   PermissionPresetDefault,
	referencePresetDefault:       PermissionPresetDefault,
	referencePresetRequestReview: PermissionPresetStrict,
	referencePresetTurbo:         PermissionPresetPermissive,
	referencePresetVetted:        PermissionPresetStrict,
	referencePresetNone:          PermissionPresetPermissive,
	referencePresetAuto:          PermissionPresetDefault,
}

// cloneReviewForReference maps the reference's review enum name onto
// the clone's review mode.
//
//	UNSPECIFIED → always (the clone's default) 【推测】
//	ALWAYS      → always
//	TURBO       → never  (proceed without a review) 【推测】
//	AUTO        → auto   (the agent decides)
var cloneReviewForReference = map[string]ArtifactReviewMode{
	referenceReviewUnspecified: ArtifactReviewModeAlways,
	referenceReviewAlways:      ArtifactReviewModeAlways,
	referenceReviewTurbo:       ArtifactReviewModeNever,
	referenceReviewAuto:        ArtifactReviewModeAuto,
}

// referencePresetName returns the reference's enum name for the
// clone's preset. An unknown preset (a value written by a newer
// clone) falls back to DEFAULT: the answer must always be a name the
// reference's decoder accepts.
func referencePresetName(p PermissionPreset) string {
	if name, ok := referencePresetNames[p]; ok {
		return name
	}
	return referencePresetDefault
}

// referenceReviewName returns the reference's enum name for the
// clone's review mode. An unknown mode falls back to ALWAYS.
func referenceReviewName(m ArtifactReviewMode) string {
	if name, ok := referenceReviewNames[m]; ok {
		return name
	}
	return referenceReviewAlways
}

// presetFromReferenceName maps a reference preset enum name onto the
// clone's preset.
//
// The second result is false for a name the reference's enum does not
// have — the caller answers -32602 rather than guessing.
func presetFromReferenceName(name string) (PermissionPreset, bool) {
	preset, ok := clonePresetForReference[name]
	return preset, ok
}

// reviewFromReferenceName maps a reference review enum name onto the
// clone's review mode.
//
// The second result is false for a name the reference's enum does not
// have.
func reviewFromReferenceName(name string) (ArtifactReviewMode, bool) {
	mode, ok := cloneReviewForReference[name]
	return mode, ok
}

// referenceGrantKeys renders the clone's grants as the reference's
// rule strings — "<tool>:<pattern>", the spelling the reference's own
// settings.json uses.
//
// 【一手】 the spelling; see internal/permissions/settings.go.
func referenceGrantKeys(grants []PermissionGrant) []string {
	if len(grants) == 0 {
		return nil
	}
	out := make([]string, 0, len(grants))
	for _, g := range grants {
		out = append(out, g.GrantKey())
	}
	return out
}

// grantsFromReferenceKeys parses the reference's rule strings into
// the clone's grants.
//
// A rule the clone cannot parse is an error: the reference's rules
// are "tool:pattern" pairs, and a rule without a colon is a rule the
// clone does not know how to store.
func grantsFromReferenceKeys(keys []string) ([]PermissionGrant, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	out := make([]PermissionGrant, 0, len(keys))
	for _, key := range keys {
		tool, pattern, ok := ParseToolPattern(key)
		if !ok {
			return nil, ErrPermissionGrantMalformed
		}
		out = append(out, PermissionGrant{ToolName: tool, Pattern: pattern})
	}
	return out, nil
}
