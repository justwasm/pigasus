// Package remotecontrol — dispatch_userstatus.go: the Web UI's
// GetUserStatus handler and the model-catalogue helpers it needs.
//
// Why a sibling file (not dispatch_languageserver.go): the
// LanguageServerService route table in that file is a long switch
// that already sits above the 50-LOC function budget the
// rc-filesize-internal-remotecontrol cycle enforces. Keeping the
// user-status block — the one the Web UI's account chip reads — out
// of that file leaves the route table as the only oversized
// function there.
//
// The response shape is exa.language_server_pb.GetUserStatusResponse:
// a `user_status` block (exa.codeium_common_pb.UserStatus) plus an
// optional `plan_info`. The clone answers the first and leaves the
// second unset.
package remotecontrol

import (
	"os"
	"strings"
)

// The plan tier the clone reports to the Web UI.
//
// The clone serves the user's own account from the user's own machine;
// there is no cloud project to hold a licence, so the tier is the one
// the local account is entitled to. The two surfaces that report a
// tier — the account chip's user_status and the licence read — have to
// agree on it, so both use these two constants.
const (
	// lsPlanTierID is the tier's id (the message's `id` member).
	lsPlanTierID = "pro-tier"
	// lsPlanTierName is the tier's display name.
	lsPlanTierName = "Pro"
)

// handleGetUserStatus answers the Web UI's GetUserStatus call. The
// Web UI's account chip shows the `email` field verbatim, so the
// address has to be the signed-in account's real one — the host
// supplies it through Config.AccountEmail. The
// "<user>@pigasus.local" form is only the placeholder for a host
// that installed no account.
func (d *Dispatcher) handleGetUserStatus() (any, *rpcErrorBody, bool) {
	user := os.Getenv("USER")
	if user == "" {
		user = "antigravity"
	}
	email := strings.TrimSpace(d.cfg.AccountEmail)
	if email == "" {
		email = user + "@pigasus.local"
	}
	models := d.userStatusModels()
	clientModelConfigs, modelLabels := userStatusClientModelConfigs(models)
	return map[string]any{
		"userStatus": map[string]any{
			"email": email,
			"name":  user,
			// google.internal.cloud.code.v1internal.UserTier has
			// id(1) name(2) description(3) … — the clone used to
			// invent "userTier"/"tierDisplayName" keys, which no
			// field of the message can carry.
			"userTier": map[string]any{
				"id":          lsPlanTierID,
				"name":        lsPlanTierName,
				"description": lsPlanTierName,
			},
			"disableTelemetry": true,
			"cascadeModelConfigData": map[string]any{
				"clientModelConfigs": clientModelConfigs,
				"clientModelSorts":   userStatusClientModelSorts(modelLabels),
			},
		},
	}, nil, true
}

// userStatusModels returns the model catalogue the Web UI's model
// picker shows. The host's ListBackend wins; an empty catalogue
// falls back to the built-in list so the picker is never blank.
func (d *Dispatcher) userStatusModels() []ModelInfo {
	var models []ModelInfo
	if d.cfg.ListBackend.Models != nil {
		if list, err := d.cfg.ListBackend.Models("", ""); err == nil && len(list) > 0 {
			models = list
		}
	}
	if len(models) > 0 {
		return models
	}
	return []ModelInfo{
		{ID: "gemini-3.8-flash-high", Label: "Gemini 3.8 Flash (High)", EffortSupported: true, Default: true},
		{ID: "gemini-3.8-flash-medium", Label: "Gemini 3.8 Flash (Medium)", EffortSupported: true},
		{ID: "gemini-3.8-flash-low", Label: "Gemini 3.8 Flash (Low)", EffortSupported: true},
		{ID: "claude-sonnet-4-6", Label: "Claude Sonnet 4.6 (Thinking)", EffortSupported: true},
		{ID: "claude-opus-4-6-thinking", Label: "Claude Opus 4.6 (Thinking)", EffortSupported: true},
		{ID: "gpt-oss-120b-medium", Label: "GPT-OSS 120B (Medium)", EffortSupported: true},
	}
}

// userStatusClientModelConfigs renders the catalogue as the
// exa.codeium_common_pb.ClientModelConfig entries the Web UI's model
// picker reads, and returns the labels it sorts them by.
func userStatusClientModelConfigs(models []ModelInfo) ([]map[string]any, []string) {
	var configs []map[string]any
	var labels []string
	slots := modelCatalogueSlots(models)
	for _, m := range models {
		label := m.Label
		if label == "" {
			label = m.ID
		}
		labels = append(labels, label)
		cfg := map[string]any{
			"label": label,
			// The picker reads the number out of the model choice and
			// sends it back in StartCascade's `requestedModel`. An
			// alias entry carries no number, so the picker resolved it
			// to UNSPECIFIED and every selection snapped back to the
			// first model — the "clicking a model does nothing"
			// symptom.
			//
			// The choice is a protobuf `oneof choice { Model model = 1;
			// ModelAlias alias = 2; }`. Its JSON form on the wire is
			// the *member* field, not a `{case, value}` envelope: the
			// Web UI decodes the response with protobuf-es v2's
			// fromJson, which matches every JSON key against the
			// message's field names and drops anything else (the
			// response is decoded with ignoreUnknownFields). A
			// `choice` key is not a field name, so the whole envelope
			// was dropped and the picker saw an unset choice — `value`
			// stayed UNSPECIFIED for every entry and the selection
			// snapped back to the first model.
			//
			// The value itself is spelled the way proto-JSON spells
			// an enum: by its NAME. The reference's own server does
			// that — its GetUserStatus answers
			// `{"modelOrAlias":{"model":"MODEL_PLACEHOLDER_M8"}}` —
			// and the Web UI's decoder accepts the name as readily as
			// the number.
			"modelOrAlias": map[string]any{
				"model": modelWireJSON(modelWireValue(m, slots[m.ID])),
			},
			// proto-JSON omits a field that holds its default, so the
			// reference's own catalogue carries neither `disabled`
			// nor an empty `supportedMimeTypes` map — only the
			// entries it actually disables say so.
			"provider": userStatusModelProvider(label, m.ID),
		}
		// The rest of the fields the Web UI's picker reads: the mime
		// types, the disabled flag, the quota, the tag line and the
		// model's own links (model_config_fields.go).
		applyUserStatusModelConfig(cfg, m, label)
		configs = append(configs, cfg)
	}
	return configs, labels
}

// userStatusModelProvider returns the ModelProvider enum the
// reference's catalogue names a model by.
//
// The reference's own catalogue (its live GetUserStatus capture) maps
// gemini-3.1-pro and gemini-3.1-ultra to MODEL_PROVIDER_GOOGLE and
// claude-4.5-opus to MODEL_PROVIDER_ANTHROPIC — the vendor, taken from
// the model's own name. The enum's real entries agree: the reference's
// Model enum calls a GPT-OSS model MODEL_OPENAI_GPT_OSS_120B_MEDIUM.
//
// A model of the user's own — MiniMax, say, served through their own
// endpoint — belongs to no vendor in the enum, and the product's own
// provider is the honest answer: MODEL_PROVIDER_ANTIGRAVITY.
func userStatusModelProvider(label, modelID string) string {
	name := strings.ToLower(label + " " + modelID)
	switch {
	case strings.Contains(name, "gemini"):
		return "MODEL_PROVIDER_GOOGLE"
	case strings.Contains(name, "claude"):
		return "MODEL_PROVIDER_ANTHROPIC"
	case strings.Contains(name, "gpt"), strings.Contains(name, "openai"):
		return "MODEL_PROVIDER_OPENAI"
	case strings.Contains(name, "grok"):
		return "MODEL_PROVIDER_XAI"
	case strings.Contains(name, "deepseek"):
		return "MODEL_PROVIDER_DEEPSEEK"
	case strings.Contains(name, "glm"):
		return "MODEL_PROVIDER_GLM"
	case strings.Contains(name, "kimi"):
		return "MODEL_PROVIDER_KIMI"
	}
	return "MODEL_PROVIDER_ANTIGRAVITY"
}

// applyUserStatusModelTag adds the picker's tag line ("Fast" /
// "Thinking") to a model entry.
//
// The configuration's own words win: a model that says it is "Fast"
// says it. The catalogue's guess from the model's id is the
// fallback, and it is a guess — a model called "flash" is not
// necessarily fast.
func applyUserStatusModelTag(cfg map[string]any, m ModelInfo) {
	if m.TagTitle != "" {
		cfg["tagTitle"] = m.TagTitle
	}
	if m.TagDescription != "" {
		cfg["tagDescription"] = m.TagDescription
	}
	if m.TagTitle != "" || m.TagDescription != "" {
		return
	}
	idLower := strings.ToLower(m.ID)
	switch {
	case strings.Contains(idLower, "flash"):
		cfg["tagTitle"] = "Fast"
		cfg["tagDescription"] = "Recommended for general coding and fast turns"
	case strings.Contains(idLower, "thinking"), strings.Contains(idLower, "sonnet"), strings.Contains(idLower, "opus"):
		cfg["tagTitle"] = "Thinking"
		cfg["tagDescription"] = "Deep reasoning and architectural planning"
	}
}

// userStatusClientModelSorts renders the picker's sort order — one
// group holding every catalogue label.
//
// The group carries no `groupName`: proto-JSON omits a field that
// holds its default, and the reference's own catalogue omits it too —
// its groups carry `modelLabels` and nothing else.
func userStatusClientModelSorts(labels []string) []map[string]any {
	return []map[string]any{
		{
			"name": "Recommended",
			"groups": []map[string]any{
				{
					"modelLabels": labels,
				},
			},
		},
	}
}
