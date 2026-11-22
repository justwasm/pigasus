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
// `tools.Tool read_file not found` and why /context's System tools row read 4.3k
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
// and is not one.
//
// Cycle 306 refined that: the 1.4.8 binary holds a *second* variant of four
// of these descriptions (`run_command`, `send_message`, `manage_subagents`,
// `edit_file` — see internal/tools/variants.go). The selection is runtime/flag
// dependent and was not traced, but the request bodies captured on 2026-10-22
// (with and without `--sandbox`) both carry the *older* variant, which is the
// one this fixture holds — so the clone ships the variant the reference
// actually puts on the wire. The newer ones are recorded as evidence in
// testdata/tool_variants_1.4.7.json.
//
// 1.5.14 vs 1.4.6 / 1.4.8: Re-verified against official agy 1.5.14 binary (sha256
// e8c2959bf4744580d0e5b8f938ec2dc3918a942b620a6262e53856400f14e5fb). The default
// twelve declarations remain byte-identical (13,449 B without sandbox, 13,773 B
// with --sandbox), with identical BypassSandbox parameter semantics. The fixture
// is mirrored as model_tools_1.5.14.json. Version 1.5.14 additionally introduces
// native Swarm coordination as a dynamic 13th declaration (surface.SwarmDeclaration()).
package surface

import (
	_ "embed"
	"encoding/json"
	"strconv"
	"strings"
	"sync"

	"github.com/justwasm/pigasus/internal/app/tools"
	"github.com/justwasm/pigasus/internal/app/tools/filesystem"
)

//go:embed model_tools_1.4.6.json
var modelToolsJSON []byte

//go:embed model_tools_1.5.14.json
var modelTools1514JSON []byte

// ModelTools1514JSON returns the raw embedded 1.5.14 declarations JSON.
func ModelTools1514JSON() []byte {
	return modelTools1514JSON
}

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

func loadModelTools() {
	modelToolsOnce.Do(func() {
		if err := json.Unmarshal(modelToolsJSON, &modelTools); err != nil {
			// The fixture is embedded at build time; a parse failure is a
			// programming error, not a runtime condition.
			panic("tools: model_tools_1.4.6.json: " + err.Error())
		}
	})
}

// ModelDeclarations returns the twelve declarations the reference sends to the
// default (unsandboxed) agent, in capture order.
func ModelDeclarations() []ModelToolDecl {
	return ModelDeclarationsFor(false)
}

// RawModelDeclarations returns the raw embedded 1.4.6 declarations.
func RawModelDeclarations() []ModelToolDecl {
	loadModelTools()
	out := make([]ModelToolDecl, len(modelTools))
	copy(out, modelTools)
	return out
}

// ModelDeclarationsFor returns the declarations customized for sandboxed or unsandboxed execution.
func ModelDeclarationsFor(sandboxed bool) []ModelToolDecl {
	loadModelTools()
	out := make([]ModelToolDecl, len(modelTools))
	copy(out, modelTools)
	for i, d := range out {
		switch d.Name {
		case "run_command":
			if !sandboxed {
				out[i].Parameters = stripBypassSandbox(d.Parameters)
			}
		case "invoke_subagent":
			out[i].Description = "Launch a subagent. It runs in the background with its own prompt and sends you a message when it completes or needs guidance; there is no need to poll.\n\nGuidelines:\n* The subagent is identified by the conversationId returned; message it with the send_message tool.\n* Multiple subagents may share a type name; each still gets a unique conversationId.\n* If a task continues an existing subagent's work, message that subagent instead of launching a new one."
			out[i].Parameters = updateParamProperty(d.Parameters, "Model", "description",
				"Model for the subagent. 'inherit' (default): Uses your model. Default to 'inherit' unless the user explicitly requests a different model. 'flash_lite': The lightest, fastest model for trivial or high-volume tasks. 'flash': A lighter, faster model for simple tasks. 'pro': A larger model for complex work.")
		case "manage_subagents":
			out[i].Parameters = updateParamProperty(d.Parameters, "Action", "description",
				"'list' lists active direct subagents; 'kill' terminates the subagents named in ConversationIds; 'kill_all' terminates all.")
		}
	}
	return out
}

// SwarmDeclaration returns the reference model tool declaration for the swarm coordinator tool.
func SwarmDeclaration() ModelToolDecl {
	return ModelToolDecl{
		Name: "swarm",
		Description: "Manage the swarm of agents working with you on the shared message board. ONLY call this tool when the user explicitly invoked `/swarm`, a skill you read explicitly instructs you to call it, or this conversation already has an active swarm. NEVER start or inspect a swarm on your own (do not call `add`, `title`, `status`, or `models` unprompted); use `invoke_subagent` for normal subagent delegation instead. Actions: `add` spawns a worker under a roster name (or `<name>-1`..`<name>-<k>` when `tasks` or `n` is set), pinned to a model, and posts `brief` on the board as an @mention of it, which is where the swarm sees its assignment -- by default a worker starts fresh and sees nothing of this conversation, so the brief must say what it owns and what done looks like; pass `fork:\"<member>\"` (or `\"coordinator\"`) to branch the new worker from that member's conversation history instead. `workspace:\"share\"` gives each worker its own working copy of your repository, for authors who edit or build at the same time as others; `\"branch\"` gives an isolated clone; readers and verifiers keep the default `inherit`. Briefs are public: every member can read them. `models` lists the models to choose from (`all:true` lists every model `add` accepts); with no model, a worker runs on your own model, which is what to use unless the user asks for another. `status` lists every member, the model it runs on, and its state: `waiting` is asleep on the board, costing nothing until somebody posts; `working` is somewhere in a turn; `stopped` is not loaded (e.g. after a restart) and a post naming it revives it; `killed` is gone. `model` switches a `waiting` worker (or yourself, when `name` is your own name or omitted) to another model, keeping its conversation: it is told of the switch, resumes any unfinished work on the new model, and runs on it from then on. Use it only when a worker or you run out of quota or capacity, or the user asks for another model, and say why; never `kill` and `add` instead. `kill` ends a worker PERMANENTLY and deletes its own workspace, so land its work first (uploaded for review, or its branch merged into your working copy; push to a remote only when the user asks): a killed worker cannot be revived, so use it when the work is over, never as a pause -- bringing one back means `add`ing a new worker with a new conversation. `title` names the swarm. Workers may call `status`, `models` and `add` too, to bring in helpers for their own work: the helper's brief is posted as you, it works in your workspace (or a share/branch of it), and should report back to you by name. A worker may `kill` only the workers it added, and `model` itself or the workers it added; killing a worker does not kill its helpers. The coordinator may `kill` any worker and `model` itself or any worker, and `title` is the coordinator's alone.",
		Parameters: json.RawMessage(`{
			"type": "OBJECT",
			"properties": {
				"action": {
					"type": "STRING",
					"description": "What to do: status, models, add, model, kill or title.",
					"enum": ["status", "models", "add", "model", "kill", "title"]
				},
				"all": {
					"type": "BOOLEAN",
					"description": "For models: list every model add accepts, not just the suggested ones."
				},
				"brief": {
					"type": "STRING",
					"description": "The worker's assignment, for add: what it owns and what done looks like. Posted publicly on the board."
				},
				"fork": {
					"type": "STRING",
					"description": "Optional roster name of an existing member (or \"coordinator\") whose conversation history the new worker forks from instead of starting fresh."
				},
				"model": {
					"type": "STRING",
					"description": "Model to pin this worker to, for add and model: a model name listed by the models action, or its display name. Leave it unset to use your own model, unless the user asks for another."
				},
				"n": {
					"type": "INTEGER",
					"description": "Optional number of homogeneous copies to spawn (<name>-1 through <name>-<n>), for add when the user explicitly asks for multiple agents sharing one role and brief. Omit (defaults to 1) to spawn a single named worker."
				},
				"name": {
					"type": "STRING",
					"description": "Roster name of the worker, for add, model and kill (or base name when n > 1 or tasks is set). Worker names: lowercase, starting with a letter or digit, up to 24 of a-z, 0-9, - and _ (including any -<k> suffix); not all, coordinator or coordinator-*."
				},
				"tasks": {
					"type": "ARRAY",
					"description": "Optional per-worker slice targets for add when spawning a sharded group (<name>-1 through <name>-<len(tasks)>), each combined with the shared brief. Omit for normal single-worker add calls.",
					"items": {"type": "STRING"}
				},
				"title": {
					"type": "STRING",
					"description": "A short title for the swarm, for the UI."
				},
				"workspace": {
					"type": "STRING",
					"description": "Where each worker works, for add. inherit (default) is your own workspace. share gives each worker its own working copy of your repository; branch gives each an isolated clone. kill deletes a share or branch workspace.",
					"enum": ["inherit", "share", "branch"]
				}
			},
			"required": ["action"]
		}`),
	}
}

// ModelDeclarationsForSwarm returns the model declarations including the swarm tool.
func ModelDeclarationsForSwarm(sandboxed bool) []ModelToolDecl {
	decls := ModelDeclarationsFor(sandboxed)
	return append(decls, SwarmDeclaration())
}

func stripBypassSandbox(raw json.RawMessage) json.RawMessage {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return raw
	}
	props, ok := m["properties"].(map[string]any)
	if !ok {
		return raw
	}
	delete(props, "BypassSandbox")
	out, err := json.Marshal(m)
	if err != nil {
		return raw
	}
	return out
}

func updateParamProperty(raw json.RawMessage, propName, key, val string) json.RawMessage {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return raw
	}
	props, ok := m["properties"].(map[string]any)
	if !ok {
		return raw
	}
	prop, ok := props[propName].(map[string]any)
	if !ok {
		return raw
	}
	prop[key] = val
	out, err := json.Marshal(m)
	if err != nil {
		return raw
	}
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

// RelaxedSchema returns a schema with its `required` list dropped.
//
// The reference's harness *runs* a tool call whose arguments miss a field
// the schema requires. The captured `view_file` call carries only
// `AbsolutePath` — no `toolSummary`, no `toolAction` — and the reference
// ran the tool instead of refusing the call. PiG's runtime validates the
// arguments against the schema before it dispatches, so a harness session
// has to validate against the relaxed schema or every such call fails.
//
// The *advertised* schema keeps the requirement: the model is still told
// to send the fields. Only the runtime check is relaxed.
//
// A schema that cannot be decoded is returned unchanged — the caller then
// validates exactly as before.
func RelaxedSchema(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return raw
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return raw
	}
	if _, ok := fields["required"]; !ok {
		return raw
	}
	delete(fields, "required")
	out, err := json.Marshal(fields)
	if err != nil {
		return raw
	}
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
func RuntimeCatalog(ws string) *tools.Registry {
	reg := Catalog(ws)
	AddReferenceTools(reg, ws)
	return reg
}

// AddReferenceTools registers the 1.4.4-only executors into an existing
// registry. Print mode builds the 60-name init list from Catalog first, then
// calls this so the runner can execute reference-shaped calls without
// changing init.tools.
func AddReferenceTools(reg *tools.Registry, ws string) {
	if reg == nil {
		return
	}
	reg.Register(&filesystem.ReadFile{Workspace: ws})
	reg.Register(&filesystem.WriteFile{Workspace: ws})
	reg.Register(&filesystem.EditFile{Workspace: ws})
}
