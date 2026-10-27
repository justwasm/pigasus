// Package remotecontrol — permissions_ls.go: the two permission
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
//	  exa.codeium_common_pb.ArtifactReviewMode global_artifact_review_mode = 2;
//	  exa.codeium_common_pb.PermissionGrantsConfig global_grants = 3;
//	}
//
//	message UpdatePermissionSettingsRequest {
//	  exa.codeium_common_pb.AgentPermissionPreset preset = 1;
//	  exa.codeium_common_pb.ArtifactReviewMode artifact_review_mode = 2;
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
// clone's; permission_vocabulary.go holds the translation.
//
// Stdlib-only. No third-party deps.
package remotecontrol

import (
	"context"
	"encoding/json"
	"errors"
)

// lsPermissionGrants is the proto-JSON shape of
// exa.codeium_common_pb.PermissionGrantsConfig — three lists of
// "<tool>:<pattern>" rules.
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
//	 "globalGrants": {"allow": ["run_command:go test*"]}}
//
// proto-JSON omits an empty repeated field, so a host with no grants
// answers `"globalGrants": {}`.
func (d *Dispatcher) lsHandleGetPermissionSettings(ctx context.Context) (any, *rpcErrorBody, bool) {
	if d == nil {
		return nil, internalError("dispatcher is nil"), true
	}
	if d.cfg.PermissionsStore == nil {
		return nil, internalError("no permission store installed"), true
	}
	state, err := d.cfg.PermissionsStore.Get(ctx)
	if err != nil {
		return nil, internalError(err.Error()), true
	}
	grants := map[string]any{}
	if allow := referenceGrantKeys(state.Grants); len(allow) > 0 {
		grants["allow"] = allow
	}
	return map[string]any{
		"globalPreset":             referencePresetName(state.Preset),
		"globalArtifactReviewMode": referenceReviewName(state.Review),
		"globalGrants":             grants,
	}, nil, true
}

// lsHandleUpdatePermissionSettings answers UpdatePermissionSettings.
//
// The request carries the whole setting — the preset, the review mode
// and the grants — and each part that is present is applied; a part
// that is absent is left alone.
//
// The clone's grant model is allow-only (the PermissionStore keeps one
// list of allowed rules), so a request that carries a deny or an ask
// rule is refused with -32602 rather than silently dropping the rules
// the caller asked for.
//
// The answer is the default instance of UpdatePermissionSettingsResponse,
// which carries no fields: "{}".
func (d *Dispatcher) lsHandleUpdatePermissionSettings(ctx context.Context, params json.RawMessage) (any, *rpcErrorBody, bool) {
	if d == nil {
		return nil, internalError("dispatcher is nil"), true
	}
	if d.cfg.PermissionsStore == nil {
		return nil, internalError("no permission store installed"), true
	}
	var req lsUpdatePermissionSettingsRequest
	if len(params) > 0 && string(params) != "null" {
		if err := jsonUnmarshal(params, &req); err != nil {
			return nil, invalidParams(err.Error()), true
		}
	}
	if nonStringFieldInJSON(params, "preset", req.Preset) ||
		nonStringFieldInJSON(params, "artifactReviewMode", req.Review) ||
		nonStringFieldInJSON(params, "artifact_review_mode", req.ReviewSnake) {
		return nil, invalidParams("string fields must be strings"), true
	}

	// The preset.
	preset := req.Preset
	if preset != "" {
		// The clone's own names are accepted too: the dashboard
		// methods speak them, and a client that mixes the two
		// vocabularies should not be refused for that.
		if p, ok := presetFromReferenceName(preset); ok {
			state, err := d.cfg.PermissionsStore.SetPreset(ctx, p)
			if err != nil {
				return nil, mapPermissionsError(err, "set preset"), true
			}
			d.broadcastPermissionsChanged(state)
		} else if IsValidPreset(PermissionPreset(preset)) {
			state, err := d.cfg.PermissionsStore.SetPreset(ctx, PermissionPreset(preset))
			if err != nil {
				return nil, mapPermissionsError(err, "set preset"), true
			}
			d.broadcastPermissionsChanged(state)
		} else {
			return nil, invalidParams("unknown preset: " + preset), true
		}
	}

	// The review mode.
	review := req.Review
	if review == "" {
		review = req.ReviewSnake
	}
	if review != "" {
		mode, ok := reviewFromReferenceName(review)
		if !ok {
			if IsValidReviewMode(ArtifactReviewMode(review)) {
				mode = ArtifactReviewMode(review)
			} else {
				return nil, invalidParams("unknown artifact review mode: " + review), true
			}
		}
		state, err := d.cfg.PermissionsStore.SetReview(ctx, mode)
		if err != nil {
			return nil, mapPermissionsError(err, "set review mode"), true
		}
		d.broadcastPermissionsChanged(state)
	}

	// The grants.
	if req.Grants != nil {
		if len(req.Grants.Deny) > 0 || len(req.Grants.Ask) > 0 {
			return nil, invalidParams("the clone's grant model is allow-only: deny and ask rules are not supported"), true
		}
		grants, err := grantsFromReferenceKeys(req.Grants.Allow)
		if err != nil {
			return nil, invalidParams("a grant rule must be \"<tool>:<pattern>\""), true
		}
		state, err := d.cfg.PermissionsStore.Get(ctx)
		if err != nil {
			return nil, internalError(err.Error()), true
		}
		existing := make([]string, 0, len(state.Grants))
		for _, g := range state.Grants {
			existing = append(existing, g.GrantKey())
		}
		if len(existing) > 0 {
			state, err = d.cfg.PermissionsStore.RemoveGrants(ctx, existing)
			if err != nil {
				return nil, mapPermissionsError(err, "replace grants"), true
			}
		}
		if len(grants) > 0 {
			state, err = d.cfg.PermissionsStore.AddGrants(ctx, grants)
			if err != nil {
				if errors.Is(err, ErrPermissionGrantExists) {
					// A repeated rule in one request is
					// not an error here: the whole list is
					// replaced, so the duplicate carries
					// nothing new.
					return nil, invalidParams("a grant rule is repeated"), true
				}
				return nil, mapPermissionsError(err, "replace grants"), true
			}
		}
		d.broadcastPermissionsChanged(state)
	}

	return map[string]any{}, nil, true
}
