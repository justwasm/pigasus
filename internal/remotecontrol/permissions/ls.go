// Package permissions — ls.go: the two permission
// methods the LanguageServerService gained at agy 1.4.8.
//
//	exa.language_server_pb.GetPermissionSettings
//	exa.language_server_pb.UpdatePermissionSettings
//
// 【一手】 2026-10-27: the FileDescriptorProto of
// third_party/jetski/language_server_pb/language_server.proto, read out
// of the agy 1.4.8 binary (file offset 0x3c81c00, 164 508 B). The
// service carried 358 RPCs at 1.4.8; the two above are new — they
// were not in the 1.4.7 descriptor.
//
//	message GetPermissionSettingsResponse {
//	  exa.codeium_common_pb.AgentPermissionPreset global_preset = 1;
//	  exa.codeium_common_pb.ReviewMode global_artifact_review_mode = 2;
//	  exa.codeium_common_pb.PermissionGrantsConfig global_grants = 3;
//	}
//
//	message UpdatePermissionSettingsRequest {
//	  exa.codeium_common_pb.AgentPermissionPreset preset = 1;
//	  exa.codeium_common_pb.ReviewMode artifact_review_mode = 2;
//	  exa.codeium_common_pb.PermissionGrantsConfig grants = 3;
//	}
//
//	message UpdatePermissionSettingsResponse {}   // no fields
//
// The wire is canonical proto-JSON: the members are the camelCase
// names ("globalPreset", "globalArtifactReviewMode",
// "globalGrants"), the enums are spelled by name, and the update's
// answer is the default instance of an empty message — "{}".
//
// A name collision worth knowing about: the *remote-agent proxy*
// service (google/gca/aicode/v1main/remote_agent_proxy.proto) has a
// GetPermissionSettings of its own, whose response carries two
// `PermissionSettings` messages (global and project) instead of the
// three flat fields above. The clone serves the language server, not
// the cloud's proxy, and the language-server table is consulted first
// (dispatch.go) — so the flat shape is the one the clone answers.
//
// The vocabulary the two methods speak is the reference's, not the
// clone's; vocabulary.go holds the translation.
//
// Stdlib-only. No third-party deps.
package permissions

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/justwasm/pigasus/internal/remotecontrol/rpc"
)

// lsPermissionGrants is the proto-JSON shape of
// exa.codeium_common_pb.PermissionGrantsConfig — three lists of
// grant strings.
//
// A grant string is the reference's own "action(target)" form (and the
// clone's legacy "tool:pattern" one); see grant.go.
//
// 【一手】 the three field names; see the file comment.
type lsPermissionGrants struct {
	Allow []string `json:"allow,omitempty"`
	Deny  []string `json:"deny,omitempty"`
	Ask   []string `json:"ask,omitempty"`
}

// lsUpdatePermissionSettingsRequest is the proto-JSON shape of
// exa.language_server_pb.UpdatePermissionSettingsRequest.
//
// Both spellings of the review mode's member are accepted: the
// reference's own client sends the camelCase one ("artifactReviewMode",
// the JSON name of `artifact_review_mode`), and a hand-written client
// may send the proto field name.
type lsUpdatePermissionSettingsRequest struct {
	Preset      string              `json:"preset,omitempty"`
	Review      string              `json:"artifactReviewMode,omitempty"`
	ReviewSnake string              `json:"artifact_review_mode,omitempty"`
	Grants      *lsPermissionGrants `json:"grants,omitempty"`
}

// lsHandleGetPermissionSettings answers GetPermissionSettings.
//
// The answer is the clone's own permission state, spoken in the
// reference's vocabulary:
//
//	{"globalPreset": "AGENT_PERMISSION_PRESET_DEFAULT",
//	 "globalArtifactReviewMode": "ARTIFACT_REVIEW_MODE_ALWAYS",
//	 "globalGrants": {"allow": ["command(git status)"]}}
//
// proto-JSON omits an empty repeated field, so a host with no grants
// answers `"globalGrants": {}`.
func LSGetSettings(s Seams, ctx context.Context) (any, *rpc.ErrorBody, bool) {
	if s.Store == nil {
		return nil, rpc.InternalError("no permission store installed"), true
	}
	state, err := s.Store.Get(ctx)
	if err != nil {
		return nil, rpc.InternalError(err.Error()), true
	}
	grants := map[string]any{}
	if allow := ReferenceGrantKeys(state.Grants); len(allow) > 0 {
		grants["allow"] = allow
	}
	return map[string]any{
		"globalPreset":             ReferencePresetName(state.Preset),
		"globalArtifactReviewMode": ReferenceReviewName(state.Review),
		"globalGrants":             grants,
	}, nil, true
}

// lsHandleUpdatePermissionSettings answers UpdatePermissionSettings.
//
// The request carries the whole setting — the preset, the review mode
// and the grants — and each part that is present is applied; a part
// that is absent is left alone.
//
// The clone's grant model is allow-only (the Store keeps one
// list of allowed rules), so a request that carries a deny or an ask
// rule is refused with -32602 rather than silently dropping the rules
// the caller asked for.
//
// The answer is the default instance of UpdatePermissionSettingsResponse,
// which carries no fields: "{}".
func LSUpdateSettings(s Seams, ctx context.Context, params json.RawMessage) (any, *rpc.ErrorBody, bool) {
	if s.Store == nil {
		return nil, rpc.InternalError("no permission store installed"), true
	}
	var req lsUpdatePermissionSettingsRequest
	if len(params) > 0 && string(params) != "null" {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, rpc.InvalidParams(err.Error()), true
		}
	}
	if rpc.NonStringFieldInJSON(params, "preset", req.Preset) ||
		rpc.NonStringFieldInJSON(params, "artifactReviewMode", req.Review) ||
		rpc.NonStringFieldInJSON(params, "artifact_review_mode", req.ReviewSnake) {
		return nil, rpc.InvalidParams("string fields must be strings"), true
	}

	// The preset.
	preset := req.Preset
	if preset != "" {
		// The clone's own names are accepted too: the dashboard
		// methods speak them, and a client that mixes the two
		// vocabularies should not be refused for that.
		if p, ok := PresetFromReferenceName(preset); ok {
			state, err := s.Store.SetPreset(ctx, p)
			if err != nil {
				return nil, MapError(err, "set preset"), true
			}
			broadcastChanged(s, state)
		} else if IsValidPreset(Preset(preset)) {
			state, err := s.Store.SetPreset(ctx, Preset(preset))
			if err != nil {
				return nil, MapError(err, "set preset"), true
			}
			broadcastChanged(s, state)
		} else {
			return nil, rpc.InvalidParams("unknown preset: " + preset), true
		}
	}

	// The review mode.
	review := req.Review
	if review == "" {
		review = req.ReviewSnake
	}
	if review != "" {
		mode, ok := ReviewFromReferenceName(review)
		if !ok {
			if IsValidReviewMode(ReviewMode(review)) {
				mode = ReviewMode(review)
			} else {
				return nil, rpc.InvalidParams("unknown artifact review mode: " + review), true
			}
		}
		state, err := s.Store.SetReview(ctx, mode)
		if err != nil {
			return nil, MapError(err, "set review mode"), true
		}
		broadcastChanged(s, state)
	}

	// The grants.
	if req.Grants != nil {
		if len(req.Grants.Deny) > 0 || len(req.Grants.Ask) > 0 {
			return nil, rpc.InvalidParams("the clone's grant model is allow-only: deny and ask rules are not supported"), true
		}
		grants, err := GrantsFromReferenceKeys(req.Grants.Allow)
		if err != nil {
			// The reference's own words: the `data` field carries
			// "invalid grant string: %q", "unknown action %q in grant
			// string: %q" or the sentence that refuses the retired
			// `unsandboxed` action.
			return nil, rpc.InvalidParams(grantParseError(err)), true
		}
		state, err := s.Store.Get(ctx)
		if err != nil {
			return nil, rpc.InternalError(err.Error()), true
		}
		existing := make([]string, 0, len(state.Grants))
		for _, g := range state.Grants {
			existing = append(existing, g.GrantKey())
		}
		if len(existing) > 0 {
			state, err = s.Store.RemoveGrants(ctx, existing)
			if err != nil {
				return nil, MapError(err, "replace grants"), true
			}
		}
		if len(grants) > 0 {
			state, err = s.Store.AddGrants(ctx, grants)
			if err != nil {
				if errors.Is(err, ErrGrantExists) {
					// A repeated rule in one request is
					// not an error here: the whole list is
					// replaced, so the duplicate carries
					// nothing new.
					return nil, rpc.InvalidParams("a grant rule is repeated"), true
				}
				return nil, MapError(err, "replace grants"), true
			}
		}
		broadcastChanged(s, state)
	}

	return map[string]any{}, nil, true
}
