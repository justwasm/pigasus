package subagenttools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/justwasm/pigasus/actor"
	builtinsubagent "github.com/justwasm/pigasus/internal/app/subagent/builtin"
)

// invokeTool is the full LLM-driven delegation tool.
type invokeTool struct {
	deps Deps
	defs *Store
}

func (t *invokeTool) Name() string        { return "invoke_subagent" }
func (t *invokeTool) Description() string { return invokeSubagentDescription(BuiltinSubagentCatalog()) }

func (t *invokeTool) RequiresApproval(_ string) bool { return false }

func (t *invokeTool) Definition() actor.ToolDefinition {
	return actor.ToolDefinition{
		Name:        "invoke_subagent",
		Description: invokeSubagentDescription(BuiltinSubagentCatalog()),
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"prompt": map[string]any{
					"type":        "string",
					"description": "The self-contained task to delegate to the subagent.",
				},
				"agent_name": map[string]any{
					"type":        "string",
					"description": "Optional name of a built-in subagent to invoke (e.g. \"image-generator\"). When set, the request is routed to the named built-in handler and the parent's model is NOT contacted; the handler's structured result is returned directly. Leave empty to spawn a fresh Actor.",
				},
				"model": map[string]any{
					"type":        "string",
					"description": "Optional model id override; defaults to the parent's model.",
				},
				"system_prompt": map[string]any{
					"type":        "string",
					"description": "Optional system prompt override; defaults to the parent's system prompt.",
				},
				"worktree": map[string]any{
					"type":        "string",
					"description": "Optional absolute path the subagent runs in; defaults to the parent's cwd.",
				},
			},
			"required": []string{"prompt"},
		},
	}
}

func invokeSubagentDescription(catalog string) string {
	const base = "Delegate a self-contained sub-task to a fresh agent with its own transcript. " +
		"Use when the task is self-contained and can run independently. " +
		"The subagent's final answer is returned as the tool result; intermediate tool calls " +
		"inside the subagent are NOT visible to the parent."
	if strings.TrimSpace(catalog) == "" {
		return base
	}
	return base + "\n\nBuilt-in subagents (set `agent_name` to route here):\n" + catalog
}

type invokeArgs struct {
	Prompt       string `json:"prompt"`
	PromptCap    string `json:"Prompt,omitempty"`
	AgentName    string `json:"agent_name,omitempty"`
	TypeName     string `json:"type_name,omitempty"`
	TypeNameCap  string `json:"TypeName,omitempty"`
	Role         string `json:"role,omitempty"`
	RoleCap      string `json:"Role,omitempty"`
	Model        string `json:"model,omitempty"`
	ModelCap     string `json:"Model,omitempty"`
	SystemPrompt string `json:"system_prompt,omitempty"`
	Worktree     string `json:"worktree,omitempty"`
	Workspace    string `json:"workspace,omitempty"`
	WorkspaceCap string `json:"Workspace,omitempty"`
}

func parseInvokeArgs(params json.RawMessage, deps Deps) (invokeArgs, error) {
	var args invokeArgs
	if err := json.Unmarshal(params, &args); err != nil {
		return invokeArgs{}, fmt.Errorf("invoke_subagent: parse args: %w", err)
	}
	args.Prompt = firstNonEmpty(args.Prompt, args.PromptCap)
	if args.Prompt == "" {
		return invokeArgs{}, errors.New("invoke_subagent: prompt is required")
	}
	args.Model = firstNonEmpty(args.Model, args.ModelCap)
	if args.Model == "inherit" {
		args.Model = ""
	}
	args.Workspace = firstNonEmpty(args.Workspace, args.WorkspaceCap)
	if strings.TrimSpace(args.Worktree) == "" && args.Workspace == "inherit" {
		args.Worktree = deps.CWD
	}
	args.TypeName = firstNonEmpty(args.TypeName, args.TypeNameCap, args.AgentName)
	args.Role = firstNonEmpty(args.Role, args.RoleCap)
	return args, nil
}

// researchSystemPrompt is the reference's research type's own
// instruction.
//
// 【一手】 the 1.4.6 capture describes the type as a "Research subagent
// with read-only tools for exploring the codebase, searching the web,
// and reading files".
const researchSystemPrompt = "You are a research subagent. You have read-only tools for exploring the codebase, searching the web, and reading files."

// resolve turns the request into the delegation to run.
//
// handled is true when the request was answered without a delegation:
// a built-in type runs its own handler, and a type the conversation
// cannot reach is refused with the reference's own sentence.
//
// The type decides the tool set, and that is the reference's
// authorization model:
//
//   - a type the conversation defined runs under the grant its
//     definition recorded;
//   - `self` inherits the parent's full configuration — its tools, its
//     system prompt and its model;
//   - `research` runs on the read-only research set;
//   - an unnamed request runs on the base set every subagent gets.
func (t *invokeTool) resolve(ctx context.Context, args invokeArgs) (Invocation, bool, actor.ToolResult, error) {
	typeName := strings.TrimSpace(args.TypeName)
	switch typeName {
	case "":
		// The reference's own declaration lists TypeName as required,
		// so an unnamed request is the clone's leniency. It runs on the
		// reference's base set: the research tools and the messaging
		// tool.
		inv := t.invocation(args, Grant{}, grantedToolSet(t.deps, t.defs, Grant{}))
		inv.SystemPrompt = t.resolveSystemPrompt(args, t.deps.parentPrompt())
		return inv, false, actor.ToolResult{}, nil
	case "self":
		// The reference's self type: "inherits the parent agent's full
		// configuration including tools, system prompt, and model". The
		// parent's prompt is used as it is — the role prefix does not
		// apply, because self *is* the parent.
		inv := t.invocation(args, Grant{}.All(), selfToolSet(t.deps, t.defs))
		inv.SystemPrompt = t.deps.parentPrompt()
		inv.InheritPrompt = true
		return inv, false, actor.ToolResult{}, nil
	case "research":
		inv := t.invocation(args, Grant{}, grantedToolSet(t.deps, t.defs, Grant{}))
		inv.SystemPrompt = t.resolveSystemPrompt(args, researchSystemPrompt)
		return inv, false, actor.ToolResult{}, nil
	case string(builtinsubagent.KindDeepCoder):
		inv := t.invocation(args, Grant{}.All(), selfToolSet(t.deps, t.defs))
		inv.SystemPrompt = t.resolveSystemPrompt(args, builtinsubagent.DeepCoderCoordinatorPrompt(2))
		if inv.Role == "" {
			inv.Role = builtinsubagent.RoleDeepCoder
		}
		return inv, false, actor.ToolResult{}, nil
	case string(builtinsubagent.KindDeepCoderWorkerL0):
		inv := t.invocation(args, Grant{Write: true, MCP: true}, grantedToolSet(t.deps, t.defs, Grant{Write: true, MCP: true}))
		inv.SystemPrompt = t.resolveSystemPrompt(args, builtinsubagent.DeepCoderWorkerL0Prompt)
		if inv.Role == "" {
			inv.Role = builtinsubagent.RoleDeepCoderWorkerL0
		}
		return inv, false, actor.ToolResult{}, nil
	case string(builtinsubagent.KindDeepCoderImprovementWorker):
		inv := t.invocation(args, Grant{Write: true, MCP: true}, grantedToolSet(t.deps, t.defs, Grant{Write: true, MCP: true}))
		inv.SystemPrompt = t.resolveSystemPrompt(args, builtinsubagent.DeepCoderImprovementWorkerPrompt)
		if inv.Role == "" {
			inv.Role = builtinsubagent.RoleDeepCoderImprovementWorker
		}
		return inv, false, actor.ToolResult{}, nil
	case string(builtinsubagent.KindDeepInvestigator):
		inv := t.invocation(args, Grant{MCP: true, Subagent: true}, grantedToolSet(t.deps, t.defs, Grant{MCP: true, Subagent: true}))
		inv.SystemPrompt = t.resolveSystemPrompt(args, builtinsubagent.DeepInvestigatorCoordinatorPrompt(2))
		if inv.Role == "" {
			inv.Role = builtinsubagent.RoleDeepInvestigator
		}
		return inv, false, actor.ToolResult{}, nil
	case string(builtinsubagent.KindDeepInvestigatorWorkerL0):
		inv := t.invocation(args, Grant{MCP: true}, grantedToolSet(t.deps, t.defs, Grant{MCP: true}))
		inv.SystemPrompt = t.resolveSystemPrompt(args, builtinsubagent.DeepInvestigatorWorkerL0Prompt)
		if inv.Role == "" {
			inv.Role = builtinsubagent.RoleDeepInvestigatorWorkerL0
		}
		return inv, false, actor.ToolResult{}, nil
	case string(builtinsubagent.KindDeepInvestigatorImprovementWorker):
		inv := t.invocation(args, Grant{MCP: true}, grantedToolSet(t.deps, t.defs, Grant{MCP: true}))
		inv.SystemPrompt = t.resolveSystemPrompt(args, builtinsubagent.DeepInvestigatorImprovementWorkerPrompt)
		if inv.Role == "" {
			inv.Role = builtinsubagent.RoleDeepInvestigatorImprovementWorker
		}
		return inv, false, actor.ToolResult{}, nil
	}
	if twRole, ok := builtinsubagent.LookupTeamworkRole(typeName); ok {
		grant := Grant{
			Write:    twRole.CanWrite,
			MCP:      twRole.CanMCP,
			Subagent: twRole.CanSubagent,
		}
		var tools []actor.Tool
		if twRole.CanSubagent {
			tools = selfToolSet(t.deps, t.defs)
		} else {
			tools = grantedToolSet(t.deps, t.defs, grant)
		}
		inv := t.invocation(args, grant, tools)
		inv.SystemPrompt = t.resolveSystemPrompt(args, twRole.SystemPrompt)
		if inv.Role == "" {
			inv.Role = twRole.Role
		}
		return inv, false, actor.ToolResult{}, nil
	}
	if def, ok := t.definition(typeName); ok {
		inv := t.invocation(args, def.Grant, grantedToolSet(t.deps, t.defs, def.Grant))
		inv.SystemPrompt = t.resolveSystemPrompt(args, t.deps.parentPrompt())
		return inv, false, actor.ToolResult{}, nil
	}
	if _, ok := builtinSubagentRegistry().Spec(typeName); ok {
		res, err := invokeBuiltinSubagent(ctx, typeName, args.Prompt)
		return Invocation{}, true, res, err
	}
	text := resultStamp(time.Now()) + fmt.Sprintf("Encountered error in tool execution: subagent %q not found or not allowed to be invoked", typeName)
	return Invocation{}, true, actor.ToolResult{
		Output: text,
	}, nil
}

// invocation assembles the delegation request: the prompt, the model
// and the tool set the type's grant allows.
func (t *invokeTool) invocation(args invokeArgs, grant Grant, toolSet []actor.Tool) Invocation {
	return Invocation{
		Prompt:   args.Prompt,
		Model:    args.Model,
		Worktree: args.Worktree,
		Grant:    grant,
		Tools:    toolSet,
		TypeName: strings.TrimSpace(args.TypeName),
		Role:     args.Role,
	}
}

// resolveSystemPrompt computes the subagent's system prompt.
//
// The order is the call's own override, then the definition's prompt,
// then the type's base (the parent's prompt, or the research type's own
// instruction). The role prefix goes on top of whichever of those the
// call resolved to.
func (t *invokeTool) resolveSystemPrompt(args invokeArgs, base string) string {
	systemPrompt := args.SystemPrompt
	if def, ok := t.definition(args.TypeName); ok && def.SystemPrompt != "" {
		systemPrompt = def.SystemPrompt
	}
	if systemPrompt == "" {
		systemPrompt = base
	}
	return rolePrompt(args, systemPrompt)
}

// definition looks a type up in the conversation's definition store.
//
// The reference's own built-in types are never in it, and a definition
// of the same name does not shadow them: `self` and `research` are the
// reference's, and the switch in resolve has already answered for them.
func (t *invokeTool) definition(typeName string) (Invocation, bool) {
	switch typeName {
	case "", "self", "research",
		"DeepCoder", "DeepCoderWorkerL0", "DeepCoderImprovementWorker",
		"DeepInvestigator", "DeepInvestigatorWorkerL0", "DeepInvestigatorImprovementWorker":
		return Invocation{}, false
	}
	if _, ok := builtinsubagent.LookupTeamworkRole(typeName); ok {
		return Invocation{}, false
	}
	if t.defs == nil {
		return Invocation{}, false
	}
	def, ok := t.defs.Get(typeName)
	return def, ok
}

// rolePrompt puts the role prefix on top of a prompt.
//
// The prefix is the clone's own reconstruction of what the reference
// does with the call's `Role` ("A 2-5 word description of the
// subagent's role"), and it does not apply to `self` — that type *is*
// the parent.
func rolePrompt(args invokeArgs, prompt string) string {
	if args.Role == "" {
		return prompt
	}
	if prompt != "" {
		return "You are a subagent acting as: " + args.Role + ".\n\n" + prompt
	}
	return "You are a subagent acting as: " + args.Role + "."
}

func invokeSuccessResult(res Result) (actor.ToolResult, error) {
	body := map[string]any{
		"ok":          true,
		"subagent_id": res.SubagentID,
		"text":        res.Text,
		"duration_ms": res.Duration.Milliseconds(),
	}
	if res.SessionID != "" {
		body["session_id"] = res.SessionID
	}
	encoded, _ := json.Marshal(body)
	return actor.ToolResult{
		Output: string(encoded),
	}, nil
}

func invokeErrorResult(res Result, err error) (actor.ToolResult, error) {
	body := map[string]any{
		"ok":          false,
		"error":       err.Error(),
		"subagent_id": res.SubagentID,
	}
	if res.SessionID != "" {
		body["session_id"] = res.SessionID
	}
	body["duration_ms"] = res.Duration.Milliseconds()
	encoded, _ := json.Marshal(body)
	return actor.ToolResult{
		Output: string(encoded),
		Error:  err.Error(),
	}, nil
}

// Execute launches the delegation and returns the reference's
// acknowledgement.
func (t *invokeTool) Execute(ctx context.Context, input string) (actor.ToolResult, error) {
	args, err := parseInvokeArgs([]byte(input), t.deps)
	if err != nil {
		return actor.ToolResult{Error: err.Error()}, err
	}
	if name := strings.TrimSpace(args.AgentName); name != "" {
		return invokeBuiltinSubagent(ctx, name, args.Prompt)
	}
	inv, handled, res, err := t.resolve(ctx, args)
	if handled {
		return res, err
	}
	launched, err := Launch(ctx, inv, t.deps)
	if err != nil {
		return invokeErrorResult(Result{}, err)
	}
	return actor.ToolResult{
		Output: launched.Acknowledgement.Text(),
	}, nil
}

func invokeBuiltinSubagent(ctx context.Context, name, prompt string) (actor.ToolResult, error) {
	reg := builtinSubagentRegistry()
	res, err := reg.Invoke(ctx, name, prompt)
	if err != nil {
		body := map[string]any{
			"ok":         false,
			"agent_name": name,
			"error":      err.Error(),
		}
		encoded, _ := json.Marshal(body)
		return actor.ToolResult{
			Output: string(encoded),
			Error:  err.Error(),
		}, nil
	}
	body := map[string]any{
		"ok":            true,
		"agent_name":    name,
		"text":          res.Text,
		"path":          res.Path,
		"mime_type":     res.MIMEType,
		"bytes":         res.Bytes,
		"deterministic": res.Deterministic,
	}
	encoded, _ := json.Marshal(body)
	return actor.ToolResult{
		Output: string(encoded),
	}, nil
}

var (
	builtinRegistryOnce sync.Once
	builtinRegistry     *builtinsubagent.Registry
	builtinRegistryMu   sync.Mutex
)

func builtinSubagentRegistry() *builtinsubagent.Registry {
	builtinRegistryOnce.Do(func() {
		builtinRegistry = builtinsubagent.Default()
	})
	// The lock matters: a session installs its own registry (the browser
	// agent's handler carries that turn's model and services), and a
	// tool call reads the registry while the next turn installs one.
	builtinRegistryMu.Lock()
	defer builtinRegistryMu.Unlock()
	return builtinRegistry
}

func SetBuiltinSubagentRegistry(r *builtinsubagent.Registry) {
	builtinRegistryMu.Lock()
	defer builtinRegistryMu.Unlock()
	// The lazy default must not overwrite a registry a session has
	// already installed. Marking the Once as done *before* the
	// assignment is what makes that true: without it, the first read
	// after this call would seed the default and silently drop the
	// session's registry — a browser agent installed at session setup
	// would simply never be routed to.
	builtinRegistryOnce.Do(func() {})
	builtinRegistry = r
}

func BuiltinSubagentCatalog() string {
	reg := builtinSubagentRegistry()
	if reg == nil {
		return ""
	}
	return reg.Catalog()
}
