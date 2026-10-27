// Package tools — descriptions.go: the reference's own per-tool descriptions.
//
// Provenance
// ----------
// The catalogue in catalog.go used to carry a hand-written paraphrase for
// every advertised tool. Those paraphrases were the clone's own prose: of the
// sixty names the catalogue held at 1.4.3, exactly **one** — `view_file` —
// matched the reference's text, and the other fifty-nine were inventions that
// read like captures.
//
// agy 1.4.8 carries three tool surfaces, and only the third one describes
// anything:
//
//  1. the advertised list (`init.tools`) — sixty-three names, no prose;
//  2. the converter layer (cortex/tools/*, gemini_coder/framework/
//     toolconverters) — the prose that assembles a tool definition;
//  3. the runtime layer (cortex/core/tools/tools, oneharness/internal/
//     tools/tools) — the `Description()` methods, twelve of which are the
//     declarations the reference sends to the model.
//
// The texts in testdata/tool_descriptions_1.4.7.json are the reference's own
// bytes, read out of the binary and re-verified against agy 1.4.8 in
// cycle 359 — each text reconstructs, chunk by chunk, from 1.4.8
// literals — with the layer each one came from recorded beside it. The
// catalogue derives its descriptions from that fixture, so a paraphrase
// cannot drift away from the reference again.
//
// The twenty-three advertised names the 1.4.8 binary describes **nowhere**
// (the twenty-two browser tools and `delete_knowledge`) have no text to
// derive from: the catalogue keeps its own prose for those and says so —
// see the "invented" comment on the specs in catalog.go.
//
// `browser_subagent` is the fourth surface: its description lives in the
// reference's built-in agents package (cortex/customizations/builtin/agents),
// not in a tool layer, and it is the browser *agent's* description (cycle
// 311).
package tools

import (
	_ "embed"
	"encoding/json"
	"sync"
)

//go:embed testdata/tool_descriptions_1.4.7.json
var referenceDescriptionsJSON []byte

// ReferenceDescription is one tool's description as the reference carries it.
type ReferenceDescription struct {
	// Layer records which of the reference's three tool surfaces the text
	// came from: "converter" (the prose that assembles a tool definition),
	// "model-facing (runtime)" (the `Description()` method of a tool the
	// reference sends to the model) or "runtime (cortex/core)".
	Layer string `json:"layer"`

	// Text is the reference's bytes.
	Text string `json:"text"`

	// Complete is false when only fragments of the text were recovered —
	// `run_workflow`, whose description is assembled from a list of tool
	// names and was not recovered whole.
	Complete bool `json:"complete"`

	// ModelFacing carries the runtime text of a tool whose *converter* text
	// the catalogue mirrors: `run_command`. The two differ in the reference
	// (the converter asks the model to propose a command, the runtime text
	// states what the tool does), and only one of them can be the
	// catalogue's.
	ModelFacing string `json:"model_facing"`

	// Note records a caveat about this particular text.
	Note string `json:"note"`
}

var (
	referenceDescriptionsOnce sync.Once
	referenceDescriptions     map[string]ReferenceDescription
)

// ReferenceDescriptions returns the reference's own descriptions, keyed by the
// advertised tool name. The map is copied so a caller cannot mutate the
// cached fixture.
func ReferenceDescriptions() map[string]ReferenceDescription {
	out := make(map[string]ReferenceDescription, len(referenceDescriptionMap()))
	for name, desc := range referenceDescriptionMap() {
		out[name] = desc
	}
	return out
}

// referenceDescriptionMap returns the cached fixture map. The caller must not
// mutate it.
func referenceDescriptionMap() map[string]ReferenceDescription {
	referenceDescriptionsOnce.Do(func() {
		var doc struct {
			CapturedAt string                          `json:"captured_at"`
			Tools      map[string]ReferenceDescription `json:"tools"`
		}
		if err := json.Unmarshal(referenceDescriptionsJSON, &doc); err != nil {
			// The fixture is embedded at build time; a parse failure is a
			// programming error, not a runtime condition.
			panic("tools: tool_descriptions_1.4.7.json: " + err.Error())
		}
		referenceDescriptions = doc.Tools
	})
	return referenceDescriptions
}

// refDesc returns the reference's own description for an advertised tool.
//
// It returns "" when the reference carries no description for the name — the
// twenty-three tools the 1.4.7 binary describes nowhere — and when only
// fragments were recovered (`run_workflow`). The catalogue keeps its own
// prose for those and marks it as invented.
func refDesc(name string) string {
	desc, ok := referenceDescriptionMap()[name]
	if !ok || !desc.Complete {
		return ""
	}
	return desc.Text
}
