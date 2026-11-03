// tiers.go — the per-model reasoning tiers a configured model offers.
//
// # What a tier is
//
// A tier is the reasoning level a model runs at: "low", "medium", "high",
// "xhigh" or "max". The reference spells the level into the model's own
// catalogue id — "gemini-3.8-flash-high" is the high tier of
// "gemini-3.8-flash" — and the Web UI's dispatcher accepts the same five
// words. A model the user configured has no such id, so the tier list
// comes from the provider configuration:
//
//	{"id": "minimax-m3.1-flash", "model": "MiniMax-M3.1-Flash",
//	 "reasoning_efforts": ["low", "medium", "high", "xhigh", "max"],
//	 "reasoning_effort": "high"}
//
// `reasoning_efforts` are the tiers the model offers; `reasoning_effort` is
// the one it runs at when nothing else says.
//
// # The rows
//
// A model that declares tiers is listed once per tier — the reference's own
// shape ("Gemini 3.8 Flash (High)"). The Web UI's picker and the TUI's
// /model panel both list whatever rows they are handed, so the tiers reach
// both without a frontend change: the picker shows the label, and the tier
// rides the catalogue id the picker sends back.
package models

import (
	"fmt"
	"strings"
)

// EffortLevels is the closed set of reasoning levels — the "tiers" — a
// model may be selected at.
//
// It is the vocabulary the Web UI's dispatcher accepts (its `effort`
// cascade parameter validates against low|medium|high|xhigh|max) and the
// one the reference's own --effort flag knows. A configuration that names
// a level outside this set states a tier nobody can select.
var EffortLevels = []string{"low", "medium", "high", "xhigh", "max"}

// IsEffortLevel reports whether s names one of the levels.
func IsEffortLevel(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	for _, e := range EffortLevels {
		if s == e {
			return true
		}
	}
	return false
}

// InvalidEffortMessage renders the reference's answer to a level that is
// not one of the effort words.
//
// The reference's own format string is `Invalid effort %q (valid: %s)`
// (【一手】 the reference's string table, agy 1.4.12, offset 0x3680e49) and
// its documented instance is
//
//	Invalid effort "turbo" (valid: low, medium, high, xhigh, max)
//
// The valid list is the vocabulary above, so the message and the accepted
// set cannot drift apart: one owner, one list.
func InvalidEffortMessage(arg string) string {
	return fmt.Sprintf("Invalid effort %q (valid: %s)", arg, strings.Join(EffortLevels, ", "))
}

// CurrentModelName is the phrase the reference's own answer uses when no
// model is named.
//
// It is the reference's own string — 【strings】 agy 1.4.12, file offset
// 0x35c7811, and the binary stores the length 17 beside it — and the
// /effort command loads it as the %s of "Effort isn't adjustable for %s."
// when the model carries no name of its own
// (【反编译】 commands.(*effortCommand).Execute.func1, 0x102b0926c).
const CurrentModelName = "the current model"

// NotAdjustableMessage renders the reference's answer to an effort change
// on a model whose effort cannot be adjusted.
//
// The reference's own format string is `Effort isn't adjustable for %s.`
// — 【strings】 agy 1.4.12, file offset 0x36945ec, and the binary stores
// the length 31 beside it — and the command fills the %s with the
// model's name
// (【反编译】 the fmt.Sprintf call at 0x102b092c0 in
// commands.(*effortCommand).Execute.func1).
//
// A model with no name of its own is answered with CurrentModelName, so
// the answer reads
//
//	Effort isn't adjustable for the current model.
//
// which is the sentence the official documentation quotes
// (official-docs/cli/commands/effort.md).
func NotAdjustableMessage(model string) string {
	name := strings.TrimSpace(model)
	if name == "" {
		name = CurrentModelName
	}
	return fmt.Sprintf("Effort isn't adjustable for %s.", name)
}

// EffortAdjustable reports whether a model that publishes the given
// reasoning tiers lets the user choose one.
//
// The reference's own rule is this one: the effort is adjustable when the
// model publishes more than one level. The selection the reference builds
// for the running model carries an "adjustable" flag, and that flag is
// computed as `len(levels) > 1`
// (【反编译】 agy 1.4.12, backend.CurrentEffortSelection at 0x10274e920:
// `cmp x1, 1; cset x7, gt` at 0x10274e9b0). The /effort command refuses
// the change when the flag is false
// (【反编译】 0x102b09248, before the command even looks at its
// argument).
//
// The list is normalised first, so a configuration that repeats a level
// or writes an empty one is counted the way the model publishes it.
func EffortAdjustable(tiers []string) bool {
	return len(NormalizeEfforts(tiers)) > 1
}

// NormalizeEfforts trims, lowercases and de-duplicates a list of tiers,
// dropping the empty ones.
//
// The order the tiers were written in is kept: it is the order the
// catalogue lists the rows in.
func NormalizeEfforts(in []string) []string {
	var out []string
	seen := make(map[string]bool, len(in))
	for _, raw := range in {
		v := strings.ToLower(strings.TrimSpace(raw))
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

// ValidateEfforts checks a list of tiers against the closed set.
//
// The error names the offending tier and the whole set, so a typo is
// fixable from the message alone.
func ValidateEfforts(in []string) error {
	for _, raw := range in {
		v := strings.ToLower(strings.TrimSpace(raw))
		if v == "" {
			continue
		}
		if !IsEffortLevel(v) {
			return fmt.Errorf("unknown reasoning level %q (valid: %s)",
				strings.TrimSpace(raw), strings.Join(EffortLevels, ", "))
		}
	}
	return nil
}

// EffortTierRows expands one model into the rows a catalogue lists.
//
// A model that declares no tiers is its own row. A model that declares
// tiers gets one row per tier: the id carries the tier (the reference's
// own spelling — "gemini-3.8-flash-high"), the label carries it in
// brackets ("Gemini 3.8 Flash (High)"), and the row's reasoning effort is
// the tier — so the tier rides the row all the way to the request.
func EffortTierRows(m Model) []Model {
	tiers := NormalizeEfforts(m.ReasoningEfforts)
	if len(tiers) == 0 {
		return []Model{m}
	}
	base := strings.TrimSpace(m.Name)
	label := strings.TrimSpace(m.DisplayName)
	if label == "" {
		label = base
	}
	out := make([]Model, 0, len(tiers))
	for _, tier := range tiers {
		row := m
		row.Name = base + "-" + tier
		row.DisplayName = label + " (" + TierLabel(tier) + ")"
		row.ReasoningEffort = tier
		out = append(out, row)
	}
	return out
}

// TierRows expands a list of models into the catalogue's rows.
//
// The order is the list's order, and inside one model the order the tiers
// were declared in.
func TierRows(list []Model) []Model {
	var out []Model
	for _, m := range list {
		out = append(out, EffortTierRows(m)...)
	}
	return out
}

// TierLabel spells a tier the way a catalogue label spells it: the word
// with its first letter in upper case ("High", "Xhigh").
func TierLabel(tier string) string {
	t := strings.TrimSpace(tier)
	if t == "" {
		return ""
	}
	return strings.ToUpper(t[:1]) + t[1:]
}

// DefaultEffort returns the tier a model's bare id stands for.
//
// The model's own `reasoning_effort` wins when it names one of the tiers —
// that is the level the operator wrote down. Otherwise the reference's own
// rule applies: medium when the model offers it, else the first tier.
func DefaultEffort(m Model) string {
	tiers := NormalizeEfforts(m.ReasoningEfforts)
	if len(tiers) == 0 {
		return ""
	}
	hint := strings.ToLower(strings.TrimSpace(m.ReasoningEffort))
	for _, t := range tiers {
		if t == hint {
			return t
		}
	}
	for _, t := range tiers {
		if t == "medium" {
			return t
		}
	}
	return tiers[0]
}

// BaseModelID strips a trailing tier suffix from a catalogue id:
// "gemini-3.8-flash-high" -> "gemini-3.8-flash". An id that carries no
// tier suffix (claude-sonnet-4-6) is returned unchanged.
func BaseModelID(id string) string {
	for _, e := range EffortLevels {
		if strings.HasSuffix(id, "-"+e) {
			return strings.TrimSuffix(id, "-"+e)
		}
	}
	return id
}

// TierFromID returns the tier a catalogue id encodes, or "".
func TierFromID(id string) string {
	base := BaseModelID(id)
	if base == id {
		return ""
	}
	return strings.TrimPrefix(id, base+"-")
}

// TierRowID is the catalogue id a tier row of a model carries.
func TierRowID(base, tier string) string {
	base = strings.TrimSpace(base)
	tier = strings.ToLower(strings.TrimSpace(tier))
	if base == "" || tier == "" {
		return base
	}
	return base + "-" + tier
}

// TierNameAt returns the tier word at an index in a model's own list of
// tiers.
//
// The list is the model's own: a model that publishes "max" but not
// "xhigh" has "max" at index 3, so the word cannot come from the
// vocabulary alone. An index outside the list returns "".
func TierNameAt(tiers []string, index int) string {
	if index < 0 || index >= len(tiers) {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(tiers[index]))
}

// TierIndexOf returns the index of a tier word in a model's own list of
// tiers, or -1 when the list does not carry it.
//
// The comparison is the vocabulary's own: the words are trimmed and
// lowercased on both sides, so a configuration that writes "High" is
// found by the word "high".
func TierIndexOf(tiers []string, name string) int {
	word := strings.ToLower(strings.TrimSpace(name))
	if word == "" {
		return -1
	}
	for i, t := range tiers {
		if strings.ToLower(strings.TrimSpace(t)) == word {
			return i
		}
	}
	return -1
}

// BaseLabel strips a trailing tier from a display label: "Gemini 3.8
// Flash (High)" -> "Gemini 3.8 Flash".
//
// A label with no tier in brackets ("MiniMax M3") is returned unchanged,
// and so is one whose brackets hold something else ("Claude Sonnet 4.6
// (Thinking)") — the tier is the model's, not the label's.
//
// The name a message names is the model's name, not its row: the
// reference's own answer to an effort change it refuses
// ("Effort isn't adjustable for %s.") names the model, and the tier is
// the model's setting rather than part of its name.
func BaseLabel(label string) string {
	t := strings.TrimSpace(label)
	if TierFromLabel(t) == "" {
		return t
	}
	i := strings.LastIndex(t, " (")
	if i < 0 {
		return t
	}
	return strings.TrimSpace(t[:i])
}

// TierFromLabel returns the tier a display label carries in brackets:
// "Gemini 3.8 Flash (High)" -> "high".
//
// A label with no tier in brackets ("MiniMax M3") carries none, and so
// does one whose brackets hold something else ("Claude Sonnet 4.6
// (Thinking)").
func TierFromLabel(label string) string {
	label = strings.TrimSpace(label)
	if !strings.HasSuffix(label, ")") {
		return ""
	}
	i := strings.LastIndex(label, " (")
	if i < 0 {
		return ""
	}
	word := strings.ToLower(strings.TrimSpace(label[i+2 : len(label)-1]))
	if !IsEffortLevel(word) {
		return ""
	}
	return word
}
