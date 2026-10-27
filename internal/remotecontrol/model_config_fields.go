// Package remotecontrol — model_config_fields.go: the fields the
// Web UI's model picker reads off a model config.
//
// 【一手】 the picker maps every model config through one function
// (internal/rcfrontend/assets/main.js, the `Rk` mapper):
//
//	Rk = a => a.modelOrAlias ? {
//	  label: a.label,
//	  value: …a.modelOrAlias.choice…,
//	  modelAlias: …,
//	  disabled: a.disabled,
//	  supportedMimeTypes: new Map(Object.entries(a.supportedMimeTypes)),
//	  quotaInfo: a.quotaInfo,
//	  tagTitle: a.tagTitle,
//	  tagDescription: a.tagDescription,
//	  supportsThoughtCirculation: a.supportsThoughtCirculation,
//	  modelUrl: a.modelUrl,
//	  modelId: a.modelId || undefined,
//	} : null
//
// Every one of those fields has to have a source, or the picker sees
// `undefined` where the reference's own catalogue says something.
// The clone used to fill five of them; this file fills the rest.
//
// The rules are proto-JSON's: a field that holds its default is not
// emitted at all. `disabled: false` is therefore absent, exactly as
// the reference's own catalogue leaves it absent, and the picker reads
// the absence as "not disabled".
package remotecontrol

import (
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/justwasm/pigasus/internal/models"
)

// applyUserStatusModelConfig fills the model config's fields the Web
// UI's picker reads, from the catalogue row.
//
// Every field is emitted the way proto-JSON spells it: a bool that is
// false is absent, an empty string is absent, an unset message is
// absent. The reference's own catalogue answers the same way — the
// model it enables carries no `disabled`, and the model it has run
// out of quota carries a `quotaInfo` with nothing else.
func applyUserStatusModelConfig(cfg map[string]any, m ModelInfo, label string) {
	// The mime types the model accepts. The Web UI's attachment check
	// reads this map, and an absent map reads as an *empty* one — so a
	// model that takes pictures and says nothing is a model the Web
	// UI refuses to attach anything to.
	//
	// The model's own word decides: the configuration's list of media
	// types first, then its `supports_images` marker, and the
	// catalogue's family table when it says neither.
	if mimes := modelSupportedMimeTypes(label, m.ID, m.SupportsImages, m.SupportedMimeTypes...); mimes != nil {
		cfg["supportedMimeTypes"] = mimes
	}
	// A model the operator has taken out of service disappears from
	// the picker: the frontend filters the disabled entries out
	// (`filter(b => !b.disabled)`).
	if m.Disabled {
		cfg["disabled"] = true
	}
	// How much of the model's quota is left, and when it comes back.
	if quota := modelQuotaInfoJSON(m.QuotaInfo); quota != nil {
		cfg["quotaInfo"] = quota
	}
	// Whether the model circulates its thoughts — the reference's own
	// field (ClientModelConfig's field 19).
	if m.SupportsThoughtCirculation != nil && *m.SupportsThoughtCirculation {
		cfg["supportsThoughtCirculation"] = true
	}
	// A link to the model's own page — the reference's own field
	// (ClientModelConfig's field 20).
	if m.ModelURL != "" {
		cfg["modelUrl"] = m.ModelURL
	}
	// The model's own id, as the endpoint knows it — the reference's
	// own field (ClientModelConfig's field 21).
	if m.ModelID != "" {
		cfg["modelId"] = m.ModelID
	}
	// The picker's tag line under the model's name: the
	// configuration's own words first, the catalogue's guess second.
	applyUserStatusModelTag(cfg, m)
}

// modelQuotaInfoJSON renders the quota state the way proto-JSON
// spells it: the message's own field names, in the descriptor's
// order — `remainingFraction` (1) and `resetTime` (2).
//
// The timestamp is the reference's own spelling. protojson writes a
// timestamp as RFC 3339 with 0, 3, 6 or 9 fractional digits, and
// the reference's catalogue answers "2026-10-25T22:33:00.530076Z" —
// so the clone formats it the same way rather than passing the
// configuration's own string through.
func modelQuotaInfoJSON(q *models.QuotaInfo) map[string]any {
	if q == nil {
		return nil
	}
	out := map[string]any{}
	if q.RemainingFraction != nil {
		out["remainingFraction"] = *q.RemainingFraction
	}
	if raw := strings.TrimSpace(q.ResetTime); raw != "" {
		out["resetTime"] = protoTimestampString(raw)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// protoTimestampString spells an RFC 3339 timestamp the way protojson
// spells one.
//
// A string protojson cannot parse is passed through unchanged: the
// operator's own words are better than a wrong timestamp.
func protoTimestampString(raw string) string {
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return raw
	}
	encoded, err := protojson.Marshal(timestamppb.New(t.UTC()))
	if err != nil {
		return raw
	}
	return strings.Trim(string(encoded), `"`)
}
