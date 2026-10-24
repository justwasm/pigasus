// Package tools — model_surface.go: the model-facing function declarations.
//
// Provenance
// ----------
// The reference publishes two different tool lists, and 1.3.6-era work here
// conflated them:
//
//	agy -p 'x' --output-format stream-json   init.tools   60 names
//	CloudCode streamGenerateContent          tools[]      12 declarations
//
// init.tools is the *available* surface the headless client is told about. The
// declarations the model actually receives are the twelve captured in
// model_tools_1.4.6.json — verified identical in the 1.4.3, 1.4.4, 1.4.6 and
// 1.4.7 request bodies (13,449 B of JSON, 12 functionDeclarations, no
// top-level `tools` wrapper). Until cycle 293 the clone advertised the
// 60-name catalogue as declarations, which is why reference-shaped calls
// failed with
// `Tool read_file not found` and why /context's System tools row read 4.3k
// instead of the reference's 4.6k.
//
// The capture is stored verbatim (name, description, parameters) so the wire
// bytes come from the fixture rather than from a hand-written reconstruction.
// Order matters: the reference sends this order, not the sorted init.tools.
//
// 1.4.5 vs 1.4.4 (【一手】, two live request bodies through the cycle-274
// capture wrapper): eleven declarations are byte-identical; `schedule` gained
// an `ExactTiming` boolean and one sentence in its description ("Set
// ExactTiming only when a cron must fire at the exact wall-clock time without
// jitter."). The older capture is kept next to the new one as
// model_tools_1.4.4.json.
//
// 1.4.6 vs 1.4.5 (【一手】, docs/cycles/reference-drift-146/): eleven
// declarations are byte-identical; `run_command` gained a `BypassSandbox`
// boolean ("Set to true to run outside the sandbox (requires elevated user
// permission for auth exchange, network, or port binding).") — the
// parameter the new `<terminal_sandbox>` prompt section talks about. The 1.4.5
// capture is kept next to the new one as model_tools_1.4.5.json.
//
// 1.4.7 vs 1.4.6 (【一手】, docs/cycles/reference-drift-147/): all twelve
// declarations are byte-identical. `run_command.BypassSandbox` and the
// `<terminal_sandbox>` prompt section are conditional on `--sandbox`, not on
// the version — without the flag the reference omits the parameter and the
// prompt section, which is why a capture taken without it looks like a drift
// and is not one. The fixture keeps its name and its bytes; only the drift
// registry's `captured_at` moved.
package tools

import (
	_ "embed"
	"encoding/json"
	"strconv"
	"strings"
	"sync"
)

//go:embed model_tools_1.4.6.json
var modelToolsJSON []byte

// ModelToolDecl is one function declaration exactly as the reference sends it.
// Parameters stays raw so a round trip through backend.ToolDecl cannot
// re-order keys or re-case the reference's type names (the captured schemas
// use OBJECT / STRING / INTEGER, not JSON Schema's lowercase spellings).
type ModelToolDecl struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

var (
	modelToolsOnce sync.Once
	modelTools     []ModelToolDecl
)

// ModelDeclarations returns the twelve declarations the reference sends to the
// default agent, in capture order. The slice is copied so a caller cannot
// mutate the cached fixture.
func ModelDeclarations() []ModelToolDecl {
	modelToolsOnce.Do(func() {
		if err := json.Unmarshal(modelToolsJSON, &modelTools); err != nil {
			// The fixture is embedded at build time; a parse failure is a
			// programming error, not a runtime condition.
			panic("tools: model_tools_1.4.6.json: " + err.Error())
		}
	})
	out := make([]ModelToolDecl, len(modelTools))
	copy(out, modelTools)
	return out
}

// ModelToolsJSON returns the embedded fixture. It exists so tests can pin the
// on-disk bytes against a capture without re-reading the file.
func ModelToolsJSON() []byte { return modelToolsJSON }

// SchemaParams decodes a parameter schema and lowercases its JSON Schema type
// names. The reference's wire uses protojson's uppercase spelling
// ("type":"OBJECT", "INTEGER"), which is not valid draft 2020-12 JSON Schema;
// PiG validates each tool's schema against the metaschema before dispatching a
// call, so an uppercase schema fails every call with
//
//	'INTEGER' is not valid against metaschema
//
// The transport uppercases the types again on the way out (geminiToolSchema),
// so what the model sees is unchanged.
func SchemaParams(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var schema any
	if err := json.Unmarshal(raw, &schema); err != nil {
		return nil
	}
	out, _ := lowercaseSchemaTypes(schema).(map[string]any)
	return out
}

func lowercaseSchemaTypes(v any) any {
	switch node := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(node))
		for k, value := range node {
			if k == "type" {
				if s, ok := value.(string); ok {
					out[k] = strings.ToLower(s)
					continue
				}
			}
			if protoCountSchemaKey(k) {
				if s, ok := value.(string); ok {
					if n, err := strconv.Atoi(s); err == nil {
						out[k] = n
						continue
					}
				}
			}
			out[k] = lowercaseSchemaTypes(value)
		}
		return out
	case []any:
		out := make([]any, 0, len(node))
		for _, item := range node {
			out = append(out, lowercaseSchemaTypes(item))
		}
		return out
	default:
		return v
	}
}

// protoCountSchemaKey reports whether a JSON Schema keyword is an int64 in the
// reference's Schema proto. protojson encodes those as strings
// ("minLength":"1"); draft 2020-12 requires numbers, and PiG validates every
// tool schema against the metaschema before dispatch, so they are coerced for
// PiG. The CloudCode transport re-stringifies them on the wire.
func protoCountSchemaKey(k string) bool {
	switch k {
	case "minLength", "maxLength", "minItems", "maxItems":
		return true
	}
	return false
}

// RuntimeCatalog is Catalog plus the executors the 1.4.4 wire names but the
// 1.3.6 catalogue predates: read_file, write_file and edit_file. The
// catalogue's names for the same operations are view_file / write_to_file /
// replace_file_content; those stay registered because --agent custom agents
// and older transcripts still use them.
//
// ModelDeclarations is what gets advertised. This registry only decides what
// can *execute*, so adding entries here does not change init.tools in print
// mode (that list is built from Catalog).
func RuntimeCatalog(ws string) *Registry {
	reg := Catalog(ws)
	AddReferenceTools(reg, ws)
	return reg
}

// AddReferenceTools registers the 1.4.4-only executors into an existing
// registry. Print mode builds the 60-name init list from Catalog first, then
// calls this so the runner can execute reference-shaped calls without
// changing init.tools.
func AddReferenceTools(reg *Registry, ws string) {
	if reg == nil {
		return
	}
	reg.Register(&ReadFile{Workspace: ws})
	reg.Register(&WriteFile{Workspace: ws})
	reg.Register(&EditFile{Workspace: ws})
}
