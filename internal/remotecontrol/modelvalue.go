// Package remotecontrol — modelvalue.go: the Model value the Web UI's
// model picker carries.
//
// The reference's wire field is the exa.codeium_common_pb.Model
// enum: the picker keeps the number in the app state's
// `lastSelectedAgentModel` and sends it back in StartCascade's
// `requestedModel`.
//
// An entry served as an *alias* instead carries no number: the picker
// then resolves it to UNSPECIFIED, the selection snaps back to the
// first model of the list, and clicking a model does nothing at all.
//
// The clone's catalogue names models the reference's enum does not
// know (the user's own configured models), so every entry takes a
// placeholder slot — MODEL_PLACEHOLDER_M<n>, the reference's own
// mechanism for a model it cannot name. The live reference answers
// exactly that way: its GetUserStatus returns
// `{"modelOrAlias":{"model":"MODEL_PLACEHOLDER_M8"}}`.
//
// The slots follow the catalogue order, so one catalogue always
// yields one assignment: the number the picker sends back always
// names the same entry.
package remotecontrol

import (
	"encoding/json"
	"strconv"
	"strings"
)

// modelWireValue returns the Model value of one catalogue entry.
//
// A host that knows the reference's enum number passes it in
// (ModelInfo.Value); every other entry takes the placeholder slot at
// its position in the catalogue.
func modelWireValue(m ModelInfo, slot int) int {
	if m.Value != 0 {
		return m.Value
	}
	return modelPlaceholderBase + slot
}

// modelCatalogueSlots assigns every catalogue entry its placeholder
// slot. The map is keyed by the model's id; entries whose id repeats
// share the first slot they were given.
func modelCatalogueSlots(models []ModelInfo) map[string]int {
	slots := make(map[string]int, len(models))
	next := 0
	for _, m := range models {
		if _, ok := slots[m.ID]; ok {
			continue
		}
		slots[m.ID] = next
		next++
	}
	return slots
}

// modelIDForWireValue resolves the number the picker sent back to a
// catalogue id. It returns "" when no entry carries that value.
func modelIDForWireValue(value int, models []ModelInfo) string {
	if value == 0 {
		return ""
	}
	slots := modelCatalogueSlots(models)
	for _, m := range models {
		if modelWireValue(m, slots[m.ID]) == value {
			return m.ID
		}
	}
	return ""
}

// modelValueFromJSON reads a Model value out of a JSON payload.
//
// proto-JSON spells an enum by its NAME, and the Web UI re-encodes
// the number it decoded into the name the enum knows it by — so the
// app state's `lastSelectedAgentModel` arrives as
// `"MODEL_PLACEHOLDER_M0"`, not 1000. A number and a numeric string
// are accepted too.
func modelValueFromJSON(raw any) (int, bool) {
	switch v := raw.(type) {
	case float64:
		return int(v), true
	case int:
		return v, true
	case json.Number:
		n, err := v.Int64()
		if err != nil {
			return 0, false
		}
		return int(n), true
	case string:
		s := strings.TrimSpace(v)
		if s == "" {
			return 0, false
		}
		if n, err := strconv.Atoi(s); err == nil {
			return n, true
		}
		return modelEnumValue(s)
	}
	return 0, false
}
