// Package tools — harness_surface.go: the model-facing function
// declarations the Antigravity *harness* sends.
//
// # Two catalogues, two binaries
//
// The reference ships two agents with two tool surfaces:
//
//	agy CLI          →  init.tools lists 60 names; the CloudCode request
//	                   carries 12 declarations (model_tools_1.4.6.json)
//	localharness     →  the Gemini request carries 18 declarations
//	                   (harness_tools_1.4.7.json)
//
// The harness's surface is not a subset of the CLI's: the two agents name
// the same operations differently. The CLI reads a file with `read_file`
// and the harness reads it with `view_file`; the CLI writes with
// `write_file` and the harness writes with `write_to_file` and edits with
// `replace_file_content`. A harness session that advertised the CLI's
// declarations would offer the model a tool the harness never implements
// and hide the ones it does.
//
// # Provenance
//
// The fixture was captured verbatim from the reference `localharness`
// binary: the binary was driven through the SDK's own protocol against a
// local fake Gemini endpoint, and the request body it sent was recorded.
// The capture lives in `out/localharness/captures/`; the cycle report
// records the procedure.
//
// Unlike the CLI's declarations, the harness's schemas are plain JSON
// Schema (draft 2020-12): `$schema`, `additionalProperties` and lowercase
// type names. They are stored as sent.
package tools

import (
	_ "embed"
	"encoding/json"
	"sync"
)

//go:embed harness_tools_1.4.7.json
var harnessToolsJSON []byte

var (
	harnessToolsOnce sync.Once
	harnessTools     []ModelToolDecl
)

// HarnessDeclarations returns the eighteen declarations the reference
// harness sends, in capture order. The slice is copied so a caller cannot
// mutate the cached fixture.
func HarnessDeclarations() []ModelToolDecl {
	harnessToolsOnce.Do(func() {
		if err := json.Unmarshal(harnessToolsJSON, &harnessTools); err != nil {
			// The fixture is embedded at build time; a parse failure is
			// a programming error, not a runtime condition.
			panic("tools: harness_tools_1.4.7.json: " + err.Error())
		}
	})
	out := make([]ModelToolDecl, len(harnessTools))
	copy(out, harnessTools)
	return out
}

// HarnessDeclarationsJSON returns the embedded fixture. It exists so tests
// can pin the on-disk bytes against the capture.
func HarnessDeclarationsJSON() []byte { return harnessToolsJSON }
