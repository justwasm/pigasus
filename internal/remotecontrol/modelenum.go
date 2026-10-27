// Package remotecontrol — modelenum.go: the reference's
// exa.codeium_common_pb.Model enum.
//
// The Web UI's model picker speaks that enum. The catalogue names
// every entry with a Model value, the picker keeps the number in the
// app state (`lastSelectedAgentModel`) and sends it back in
// StartCascade's `requestedModel`.
//
// The table below is the enum the IDE's own Web UI ships. It was
// extracted from the FileDescriptorProto the frontend bundle embeds
// (out/ide-language-server/extract_ls_descriptor.py) — the bytes the
// shipped UI parses, so it is first-hand evidence.
//
// Two kinds of entries:
//
//   - 40 real ones (MODEL_GOOGLE_GEMINI_2_5_PRO and friends).
//   - 1651 placeholders, MODEL_PLACEHOLDER_M0 … M1650, numbered
//     1000 … 2650.
//
// The placeholders are the reference's own slot for a model the enum
// cannot name. The reference's server uses them exactly that way: its
// mock catalogue answers
//
//	{"modelOrAlias":{"model":"MODEL_PLACEHOLDER_M8"}}
//
// for a model it has no enum name for. The clone's catalogue is the
// user's own configured models plus the reference's CLI catalogue —
// none of which the IDE's enum knows — so every entry takes a
// placeholder slot.
//
// proto-JSON spells an enum by its NAME (Go's protojson default), and
// that is what the reference's server emits. The live capture in
// out/ide-language-server/ (the reference language server answering
// GetUserStatus) reads:
//
//	"modelOrAlias":{"model":"MODEL_PLACEHOLDER_M8"}
//
// — the name, not 1008. The clone emits names too, and accepts names
// on the way back: the Web UI re-encodes the number it decoded into
// the name the enum knows it by.
package remotecontrol

import (
	"strconv"
	"strings"
)

// modelPlaceholderBase is the number of the first placeholder slot.
// The reference's enum numbers MODEL_PLACEHOLDER_M<n> as 1000+n.
const modelPlaceholderBase = 1000

// modelPlaceholderCount is how many slots the reference's enum
// reserves (M0 … M1650).
const modelPlaceholderCount = 1651

// modelEnumNames is the reference's Model enum: value → name. The 40
// real entries; the placeholder range is derived, not listed.
var modelEnumNames = map[int]string{
	0:   "MODEL_UNSPECIFIED",
	235: "MODEL_CHAT_20706",
	246: "MODEL_GOOGLE_GEMINI_2_5_PRO",
	269: "MODEL_CHAT_23310",
	281: "MODEL_CLAUDE_4_SONNET",
	282: "MODEL_CLAUDE_4_SONNET_THINKING",
	290: "MODEL_CLAUDE_4_OPUS",
	291: "MODEL_CLAUDE_4_OPUS_THINKING",
	312: "MODEL_GOOGLE_GEMINI_2_5_FLASH",
	313: "MODEL_GOOGLE_GEMINI_2_5_FLASH_THINKING",
	323: "MODEL_GOOGLE_GEMINI_TRAINING_POLICY",
	326: "MODEL_GOOGLE_GEMINI_INTERNAL_BYOM",
	327: "MODEL_GOOGLE_GEMINI_FOR_GOOGLE_2_5_PRO",
	328: "MODEL_GOOGLE_GEMINI_NEMOSREEF",
	329: "MODEL_GOOGLE_GEMINI_2_5_FLASH_THINKING_TOOLS",
	330: "MODEL_GOOGLE_GEMINI_2_5_FLASH_LITE",
	331: "MODEL_GOOGLE_GEMINI_2_5_PRO_EVAL",
	332: "MODEL_GOOGLE_GEMINI_2_5_FLASH_IMAGE_PREVIEW",
	333: "MODEL_CLAUDE_4_5_SONNET",
	334: "MODEL_CLAUDE_4_5_SONNET_THINKING",
	335: "MODEL_GOOGLE_GEMINI_COMPUTER_USE_EXPERIMENTAL",
	336: "MODEL_GOOGLE_GEMINI_HORIZONDAWN",
	337: "MODEL_GOOGLE_GEMINI_PUREPRISM",
	338: "MODEL_GOOGLE_GEMINI_GENTLEISLAND",
	339: "MODEL_GOOGLE_GEMINI_RAINSONG",
	340: "MODEL_CLAUDE_4_5_HAIKU",
	341: "MODEL_CLAUDE_4_5_HAIKU_THINKING",
	342: "MODEL_OPENAI_GPT_OSS_120B_MEDIUM",
	343: "MODEL_GOOGLE_GEMINI_ORIONFIRE",
	344: "MODEL_GOOGLE_GEMINI_INTERNAL_TAB_FLASH_LITE",
	345: "MODEL_GOOGLE_GEMINI_INTERNAL_TAB_JUMP_FLASH_LITE",
	346: "MODEL_GOOGLE_JARVIS_PROXY",
	347: "MODEL_GOOGLE_GEMINI_COSMICFORGE",
	348: "MODEL_GOOGLE_GEMINI_RIFTRUNNER",
	349: "MODEL_GOOGLE_JARVIS_V4S",
	350: "MODEL_GOOGLE_GEMINI_INFINITYJET",
	351: "MODEL_GOOGLE_GEMINI_INFINITYBLOOM",
	352: "MODEL_GOOGLE_GEMINI_RIFTRUNNER_THINKING_LOW",
	353: "MODEL_GOOGLE_GEMINI_RIFTRUNNER_THINKING_HIGH",
	354: "MODEL_GOOGLE_GEMINI_EXAMPLE_GEMAX_SAMPLER",
}

// modelEnumNumbers is the reverse table, name → value. It is built
// once from modelEnumNames; the placeholder names are added by
// modelEnumValue, which understands the derived range.
var modelEnumNumbers = func() map[string]int {
	out := make(map[string]int, len(modelEnumNames))
	for value, name := range modelEnumNames {
		out[name] = value
	}
	return out
}()

// modelEnumName returns the reference's name for a Model value.
func modelEnumName(value int) (string, bool) {
	name, ok := modelEnumNames[value]
	return name, ok
}

// modelEnumValue returns the Model value an enum name stands for.
//
// It understands the 40 real names and the derived placeholder range
// (MODEL_PLACEHOLDER_M<n> → 1000+n). An unknown name has no value:
// proto-JSON's decoder would reject it too.
func modelEnumValue(name string) (int, bool) {
	name = strings.TrimSpace(name)
	if value, ok := modelEnumNumbers[name]; ok {
		return value, true
	}
	return modelPlaceholderValue(name)
}

// modelPlaceholderName returns the enum name of placeholder slot n.
func modelPlaceholderName(slot int) string {
	return "MODEL_PLACEHOLDER_M" + strconv.Itoa(slot)
}

// modelPlaceholderValue returns the slot a placeholder name stands
// for.
func modelPlaceholderValue(name string) (int, bool) {
	const prefix = "MODEL_PLACEHOLDER_M"
	if !strings.HasPrefix(name, prefix) {
		return 0, false
	}
	slot, err := strconv.Atoi(name[len(prefix):])
	if err != nil || slot < 0 || slot >= modelPlaceholderCount {
		return 0, false
	}
	return modelPlaceholderBase + slot, true
}

// modelPlaceholderSlot returns the slot a Model value occupies in the
// placeholder range.
func modelPlaceholderSlot(value int) (int, bool) {
	slot := value - modelPlaceholderBase
	if slot < 0 || slot >= modelPlaceholderCount {
		return 0, false
	}
	return slot, true
}

// modelWireJSON spells a Model value the way proto-JSON does: the
// enum's NAME when the value is in the enum, the raw number when it
// is not. Go's protojson — the marshaller the reference's server uses
// — emits the name for a known value and the number for an unknown
// one.
func modelWireJSON(value int) any {
	if name, ok := modelEnumName(value); ok {
		return name
	}
	if slot, ok := modelPlaceholderSlot(value); ok {
		return modelPlaceholderName(slot)
	}
	return value
}
