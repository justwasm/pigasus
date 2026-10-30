package rehydration

import "encoding/json"

// legacyOriginalStepKey is the key the reference stores the original
// step under.
//
// 【反编译】 the reference's `SetOriginalStep` (LS 0x100f41690),
// `MigrateLegacyPayload` (LS 0x100f41770) and `originalStepAny`
// (LS 0x100f41cf0) all use the 22-byte key `__original_cortex_step`.
const legacyOriginalStepKey = "__original_cortex_step"

// MigrateLegacyPayload normalises the stored arguments of a tool call
// into the shape the rehydrators read.
//
// The reference's legacy records keep the *original* step in the
// payload's metadata map under `__original_cortex_step`; the
// rehydrated payload and the original step then disagree, and the
// reference resolves the conflict in favour of the original — it
// promotes the map entry into the payload's dedicated field and drops
// the map entry (【反编译】 `MigrateLegacyPayload`, LS 0x100f41770).
//
// The clone's convstore parser does not read the payload's metadata
// map: it reads the arguments field, which is where the arguments
// themselves live (【一手】 the stored shape is
// `5 -> 4 -> {2: tool name, 3: JSON arguments}`). The migration
// therefore works on what the clone can see — an arguments blob that
// still carries the legacy wrapper is unwrapped, so the original
// arguments win over the derived ones.
//
// 【一手】 none of the 14,735 tool-call steps in the 500 reference
// conversations on this machine carry the key, so this is a guard
// rather than a hot path.
func MigrateLegacyPayload(args json.RawMessage) json.RawMessage {
	if len(args) == 0 {
		return args
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(args, &probe); err != nil {
		return args
	}
	inner, ok := probe[legacyOriginalStepKey]
	if !ok {
		return args
	}
	// The wrapper may hold the original step as a nested object (the
	// shape the reference's own migration produces) or as an encoded
	// string. Only the object form can be spliced back in.
	var nested map[string]json.RawMessage
	if err := json.Unmarshal(inner, &nested); err != nil {
		return args
	}
	// Keep the keys the wrapper does not shadow: the wrapper is a
	// carrier for the original step, not a replacement for the whole
	// arguments object.
	merged := make(map[string]json.RawMessage, len(nested)+len(probe))
	for k, v := range probe {
		if k == legacyOriginalStepKey {
			continue
		}
		merged[k] = v
	}
	for k, v := range nested {
		merged[k] = v
	}
	out, err := json.Marshal(merged)
	if err != nil {
		return args
	}
	return out
}
