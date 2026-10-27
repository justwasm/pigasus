package harnessprompt

// The built-in subagents the reference harness ships.
//
// The harness's prompt lists them under "Available subagents:", and the
// model may invoke them by name. The descriptions are the reference's
// own words — they are what the model reads to decide between them.
//
// The two are complementary:
//
//   - `research` runs with read-only tools: it explores the codebase,
//     searches the web and reads files.
//   - `self` inherits the parent agent's whole configuration — tools,
//     system prompt and model.
//
// The names and descriptions are recovered from the reference binary; the
// captured prompt is the ground truth (see `out/localharness/`).
const (
	// SubagentResearch is the read-only exploration subagent.
	SubagentResearch = "research"
	// SubagentSelf is the subagent that inherits the parent's
	// configuration.
	SubagentSelf = "self"
)

// researchDescription is the reference's description of `research`.
const researchDescription = "Research subagent with read-only tools for exploring the codebase, searching the web, " +
	"and reading files. Delegate to this agent when you need to run a task in a separate conversation context but with " +
	"the same capabilities as the current agent, when a research task requires many search and file-reading steps that " +
	"would clutter your context, or when you need a broad survey of the codebase or documentation. Prefer doing research " +
	"yourself for quick, targeted lookups."

// selfDescription is the reference's description of `self`.
const selfDescription = "Subagent that inherits the parent agent's full configuration including tools, system prompt, " +
	"and model. Use this when you need to run a task in a separate conversation context but with the same capabilities " +
	"as the current agent."

// Subagent is one subagent the prompt lists.
type Subagent struct {
	// Name is the name the model invokes it by.
	Name string
	// Description is the one-line summary the prompt carries.
	Description string
}

// BuiltinSubagents returns the reference's own subagents, in the order
// the prompt lists them.
func BuiltinSubagents() []Subagent {
	return []Subagent{
		{Name: SubagentResearch, Description: researchDescription},
		{Name: SubagentSelf, Description: selfDescription},
	}
}
