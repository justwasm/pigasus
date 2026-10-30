// Package rehydration rebuilds an agent's live state from a stored
// trajectory.
//
// # The rule
//
// The reference (Antigravity CLI 1.4.6, `agy`) keeps exactly one durable
// record of a conversation: the trajectory. Everything the agent "knows"
// about its world — the subagents it defined, the tasks it scheduled,
// the subagents it killed — is derived from the stored steps when the
// conversation is loaded, not kept in a second place.
//
// 【反编译】 the reference's `cortex/rehydration` package: `rehydrateToolCall`
// (LS 0x100f3f6d0) routes a tool call by name, `rehydrateNativePayload`
// (LS 0x100f3c890) routes it by the type of the stored payload, and
// `RehydrateStep` (LS 0x100f41860) picks between the two and migrates the
// legacy records. The cascade manager runs the pass when it loads a
// trajectory (`processLoadedTrajectory`, CLI 0x101f26440).
// See out/ide-language-server/README.md §10.9.
//
// # The shape
//
// A rehydrator turns one tool call's stored arguments back into the
// state delta that call produced. The five tools that carry state are
// the reference's own:
//
//	define_subagent  → name, description, system prompt, tool grants
//	manage_task      → action, input, task id
//	manage_subagents → action, conversation ids, killed ids
//	send_message     → recipient, recipient name
//	schedule         → mode, outcome, iterations, cancelled by
//
// 【反编译】 the reference's per-tool rehydrators: `rehydrateDefineSubagent`
// (LS 0x100f3e870), `rehydrateManageTask` (LS 0x100f3e1a0),
// `rehydrateManageSubagents` (LS 0x100f3e430), `rehydrateSendMessage`
// (LS 0x100f3e6f0), `rehydrateSchedule` (LS 0x100f3f1b0). The delta is a
// `map[string]string`; the reference attaches it to the step it rebuilt
// (`rehydratedGenericWithMetadata`, LS 0x100f3dfd0) and the consumers
// read it back (`shared.ManageSubagentsResultMetadataFromMap`,
// LS 0x100e6fcf0).
//
// # The difference
//
// The reference stores a tool call's arguments twice — as a typed
// "native payload" protobuf and as the JSON the model produced — and
// rehydrates from the typed one. The clone stores only the JSON, so the
// clone's rehydrators parse the arguments with the *same* parser the
// live handler uses. That keeps the property that matters: a rehydrated
// state and a live state agree by construction, because there is only
// one parser.
//
// The clone's rehydrators accept both spellings of an argument: the
// reference's stored ("native payload") capitalisation — `Name`,
// `Action`, `TaskId`, `Recipient`, `DurationSeconds` — and the
// model-facing snake_case. The capitalised form is what the reference
// actually writes: 【一手】 13,724 tool calls across 505 conversations in
// ~/.gemini/antigravity-cli/conversations/*.db store `define_subagent`
// as {"Name","Description","SystemPrompt","EnableWriteTools",…},
// `manage_task` as {"Action","TaskId"} and `send_message` as
// {"Recipient","Message"}.
package rehydration

import (
	"encoding/json"
	"os"
	"strings"
)

// Metadata is the state delta one rehydrated tool call carries.
//
// The reference's delta is a `map[string]string` (【反编译】
// `rehydratedGenericWithMetadata`); the clone keeps the same shape. A
// key that the tool does not need is simply absent.
type Metadata map[string]string

// Get returns the value for key, or "" when the key is absent.
func (m Metadata) Get(key string) string { return m[key] }

// Has reports whether the delta carries key.
func (m Metadata) Has(key string) bool {
	_, ok := m[key]
	return ok
}

// Step is one stored step of a trajectory, reduced to what a
// rehydrator needs: which tool ran, with which arguments, and how it
// ended.
//
// The clone's convstore keeps a richer step (a text, a result, a
// trajectory id); the reduction happens at the call site so this
// package does not depend on the store.
type Step struct {
	// Idx is the step's position in the trajectory.
	Idx int64
	// Type is the step type. 132 is the reference's tool-call type
	// (【一手】 the stored shape is `step_type = 132` for every tool
	// call in the reference's conversations). Zero means "not set",
	// which the dispatcher reads as a tool call when a tool name is
	// present.
	Type int64
	// ToolName is the tool the model called. Empty for a step that is
	// not a tool call.
	ToolName string
	// ToolArgs is the raw JSON the model produced, e.g.
	// {"CommandLine":"ls","Cwd":"/tmp"}.
	ToolArgs string
	// Status is the step's outcome. See Succeeded.
	Status int64
}

// The reference's step status values. The clone writes 3 for a step
// that ran to completion (`convstore.SessionToSteps`) and reads 4, 6
// and 7 back as failures (`cliapp.loadedStepErr`).
//
// 【一手】 every step in the 505 reference conversations sampled on this
// machine carries status 3 — the reference writes the status of a
// finished step.
const (
	// StatusCompleted is the status of a step that ran to completion.
	StatusCompleted = 3
	// StatusError is the status of a step that failed.
	StatusError = 4
	// StatusFailed is the status of a step that failed to execute.
	StatusFailed = 6
	// StatusCancelled is the status of a step that was cancelled.
	StatusCancelled = 7
)

// StepToolCall is the reference's step type for a tool call.
//
// 【一手】 every tool call in the 500 reference conversations on this
// machine is stored with `step_type = 132`.
const StepToolCall = 132

// Succeeded reports whether a step with this status ran to completion.
//
// A tool call that failed or was cancelled changed nothing, so its
// state delta is not applied. This is the clone's reading of the
// reference's `StepIndicesExcludingStatuses` (【反编译】 CLI
// 0x1016bba40): the trajectory can be asked for the steps that are
// *not* in a given set of statuses, and the rehydration pass walks the
// complement.
func Succeeded(status int64) bool {
	switch status {
	case StatusError, StatusFailed, StatusCancelled:
		return false
	}
	return true
}

// Delta is one rehydrated tool call: the step it came from and the
// state delta it carried.
type Delta struct {
	// Idx is the position of the step in the trajectory.
	Idx int64
	// ToolName is the tool that produced the delta.
	ToolName string
	// Metadata is the state delta.
	Metadata Metadata
}

// Rehydrator rebuilds the state delta of one tool call from the
// arguments the model produced.
//
// It reports false when the arguments do not describe a call the tool
// could have made — an empty argument list, malformed JSON, or a call
// that is missing the field the delta is built from.
type Rehydrator func(args json.RawMessage) (map[string]string, bool)

// Applier writes one state delta back into the live state. It reports
// whether the delta changed anything.
type Applier func(meta map[string]string) bool

// Tool is one state-carrying tool: the rehydrator that rebuilds its
// delta and the applier that writes the delta back.
type Tool struct {
	// Name is the tool name the model calls it by.
	Name string
	// Rehydrate rebuilds the delta from the stored arguments.
	Rehydrate Rehydrator
	// Apply writes the delta into the live state.
	Apply Applier
}

// Dispatcher routes a tool call to the tool that owns it.
//
// The reference's dispatch is by tool name (`rehydrateToolCall`) with a
// second, type-driven dispatch for the stored native payload
// (`rehydrateNativePayload`). The clone has no native payload, so the
// name dispatch is the whole story.
type Dispatcher struct {
	tools []Tool
}

// New returns a dispatcher with the given tools registered.
func New(tools ...Tool) *Dispatcher {
	d := &Dispatcher{}
	for _, t := range tools {
		d.Register(t)
	}
	return d
}

// Register adds a tool to the dispatcher. A tool registered twice
// replaces the earlier one — the last registration wins, the same way
// the clone's tool surface resolves a name collision ("caller-supplied
// take precedence on name collision").
func (d *Dispatcher) Register(t Tool) {
	for i, existing := range d.tools {
		if existing.Name == t.Name {
			d.tools[i] = t
			return
		}
	}
	d.tools = append(d.tools, t)
}

// Names returns the tool names the dispatcher knows, in registration
// order.
func (d *Dispatcher) Names() []string {
	out := make([]string, 0, len(d.tools))
	for _, t := range d.tools {
		out = append(out, t.Name)
	}
	return out
}

// lookup returns the tool the model called by name.
func (d *Dispatcher) lookup(name string) (Tool, bool) {
	for _, t := range d.tools {
		if t.Name == name {
			return t, true
		}
	}
	return Tool{}, false
}

// Rehydrate walks the trajectory and returns the state deltas in step
// order.
//
// A step that is not a tool call, or that did not run to completion,
// contributes nothing. A tool the dispatcher does not know contributes
// nothing either — the reference's dispatch has the same property: an
// unknown tool name falls through to the generic step.
func (d *Dispatcher) Rehydrate(steps []Step) []Delta {
	if d == nil {
		return nil
	}
	var out []Delta
	for _, s := range steps {
		if s.ToolName == "" || !Succeeded(s.Status) {
			continue
		}
		if s.Type != 0 && s.Type != StepToolCall {
			continue
		}
		tool, ok := d.lookup(s.ToolName)
		if !ok || tool.Rehydrate == nil {
			continue
		}
		meta, ok := tool.Rehydrate(json.RawMessage(s.ToolArgs))
		if !ok {
			continue
		}
		out = append(out, Delta{Idx: s.Idx, ToolName: s.ToolName, Metadata: Metadata(meta)})
	}
	return out
}

// Apply writes the deltas into the live state and returns the number
// of deltas that changed something.
func (d *Dispatcher) Apply(deltas []Delta) int {
	if d == nil {
		return 0
	}
	applied := 0
	for _, delta := range deltas {
		tool, ok := d.lookup(delta.ToolName)
		if !ok || tool.Apply == nil {
			continue
		}
		if tool.Apply(delta.Metadata) {
			applied++
		}
	}
	return applied
}

// DisableEnvVar is the environment variable that turns the rehydration
// pass off.
//
// The reference gates the same pass behind the boolean flag
// `cascade-disable-rehydration` (【反编译】 CLI
// `(*CascadeManager).shouldDisableRehydration` 0x101f29860, LS
// 0x101f1fef0); the flag is read from the reference's internal flag
// service. The clone has no flag service, so the switch is an
// environment variable, named after the clone's own `OPENAGY_*`
// convention.
const DisableEnvVar = "OPENAGY_DISABLE_REHYDRATION"

// Enabled reports whether the rehydration pass should run.
//
// It is off when OPENAGY_DISABLE_REHYDRATION is set to a non-empty
// value other than "0" or "false".
func Enabled() bool {
	v := strings.TrimSpace(os.Getenv(DisableEnvVar))
	if v == "" {
		return true
	}
	switch strings.ToLower(v) {
	case "0", "false", "off", "no":
		return false
	}
	return true
}
