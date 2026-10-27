// Package tools — variants.go: the reference's *second* set of tool texts.
//
// Provenance
// ----------
// The binary holds two variants of four model-facing tool
// descriptions: `run_command`, `send_message`, `manage_subagents` and
// `edit_file`. Which one a session sends is runtime/flag dependent, and the
// selector was not traced.
//
// The default — the one a live session actually sends — is the text
// internal/tools/model_tools_1.4.6.json carries (【一手】, the request bodies
// captured on 2026-10-22 with and without `--sandbox`), and the clone ships
// that one. The newer variants are recorded here so that the difference is
// evidence rather than folklore: they name a parameter
// (`run_command.NotificationTimeoutSeconds`), a recipient form
// (`send_message`'s `Recipient='@children'`), a subcommand (`manage_subagents`
// 'list_peers') and a multi-edit schema (`edit_file`'s `Edits` list) that the
// reference's own code paths know about.
//
// The texts live in testdata/tool_variants_1.4.7.json; this file only reads
// them. The fixture's *addresses* were re-derived against agy 1.4.8 in cycle
// 359 — the texts themselves are byte-identical to the capture, only their
// addresses moved. The file name keeps the capture's own version, the
// way tool_descriptions_1.4.7.json does.
package tools

import (
	_ "embed"
	"encoding/json"
	"sync"
)

//go:embed testdata/tool_variants_1.4.7.json
var referenceVariantsJSON []byte

// ReferenceVariant is one tool's alternate description: the text the
// reference's binary holds but does not send by default.
type ReferenceVariant struct {
	// Layer records which of the reference's tool surfaces the text came
	// from — the same vocabulary the descriptions fixture uses.
	Layer string `json:"layer"`

	// DefaultLength is the length in bytes of the text the reference
	// sends by default.
	DefaultLength int `json:"default_length"`

	// DefaultVMAddr lists the vmaddr/length pairs of the string
	// literals the default text is built from, in the 1.4.8 binary.
	DefaultVMAddr []string `json:"default_vmaddr"`

	// AlternateLength is the length in bytes of the alternate text.
	AlternateLength int `json:"alternate_length"`

	// AlternateVMAddr lists the vmaddr/length pairs of the literals the
	// alternate text is built from.
	AlternateVMAddr []string `json:"alternate_vmaddr"`

	// AlternateText is the reference's bytes.
	AlternateText string `json:"alternate_text"`

	// Note records what the alternate changes.
	Note string `json:"note"`
}

// referenceVariantDoc is the on-disk shape of the variants fixture.
type referenceVariantDoc struct {
	CapturedAt  string                      `json:"captured_at"`
	Source      string                      `json:"source"`
	DefaultStat string                      `json:"default_state"`
	Variants    map[string]ReferenceVariant `json:"variants"`
}

var (
	referenceVariantsOnce sync.Once
	referenceVariantsDoc  referenceVariantDoc
)

// ReferenceVariants returns the reference's alternate tool texts, keyed by
// the tool name. The map is copied so a caller cannot mutate the cached
// fixture.
func ReferenceVariants() map[string]ReferenceVariant {
	referenceVariantsOnce.Do(func() {
		if err := json.Unmarshal(referenceVariantsJSON, &referenceVariantsDoc); err != nil {
			// The fixture is embedded at build time; a parse failure is a
			// programming error, not a runtime condition.
			panic("tools: tool_variants_1.4.7.json: " + err.Error())
		}
	})
	out := make(map[string]ReferenceVariant, len(referenceVariantsDoc.Variants))
	for name, v := range referenceVariantsDoc.Variants {
		out[name] = v
	}
	return out
}

// ReferenceVariantsDoc returns the fixture's provenance text: where the
// texts came from and which of the two variants a live session sends.
func ReferenceVariantsDoc() (source, defaultState string) {
	ReferenceVariants()
	return referenceVariantsDoc.Source, referenceVariantsDoc.DefaultStat
}
