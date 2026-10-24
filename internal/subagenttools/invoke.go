package subagenttools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	piagent "github.com/MichaelKinsy/PiG/agent"
	pi "github.com/MichaelKinsy/PiG/ai"

	"github.com/justwasm/pigasus/internal/builtinsubagent"
)

// invokeTool is the full LLM-driven delegation tool.
type invokeTool struct {
	deps Deps
	defs *Store
}

func (t *invokeTool) Name() string  { return "invoke_subagent" }
func (t *invokeTool) Label() string { return "Invoke Subagent" }
func (t *invokeTool) ExecutionMode() piagent.ToolExecutionMode {
	return piagent.ToolModeSequential
}

func (t *invokeTool) Schema() pi.ToolSchema {
	return pi.ToolSchema{
		Name:        "invoke_subagent",
		Description: invokeSubagentDescription(BuiltinSubagentCatalog()),
		Parameters: pi.JsonObject{
			"type": "object",
			"properties": pi.JsonObject{
				"prompt": pi.JsonObject{
					"type":        "string",
					"description": "The self-contained task to delegate to the subagent.",
				},
				"agent_name": pi.JsonObject{
					"type":        "string",
					"description": "Optional name of a built-in subagent to invoke (e.g. \"image-generator\"). When set, the request is routed to the named built-in handler and the parent's model is NOT contacted; the handler's structured result is returned directly. Leave empty to spawn a fresh PiG coding.Session.",
				},
				"model": pi.JsonObject{
					"type":        "string",
					"description": "Optional model id override; defaults to the parent's model.",
				},
				"system_prompt": pi.JsonObject{
					"type":        "string",
					"description": "Optional system prompt override; defaults to the parent's system prompt.",
				},
				"worktree": pi.JsonObject{
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
		if args.Worktree == "" && deps.Services != nil {
			args.Worktree = deps.Services.CWD()
		}
	}
	args.TypeName = firstNonEmpty(args.TypeName, args.TypeNameCap)
	args.Role = firstNonEmpty(args.Role, args.RoleCap)
	return args, nil
}

func (t *invokeTool) checkTypeName(ctx context.Context, args invokeArgs) (bool, piagent.AgentToolResult, error) {
	typeName := args.TypeName
	if typeName == "" || typeName == "self" {
		return false, piagent.AgentToolResult{}, nil
	}
	if t.defs != nil {
		if _, ok := t.defs.Get(typeName); ok {
			return false, piagent.AgentToolResult{}, nil
		}
	}
	if _, ok := builtinSubagentRegistry().Spec(typeName); ok {
		res, err := invokeBuiltinSubagent(ctx, typeName, args.Prompt)
		return true, res, err
	}
	text := resultStamp(time.Now()) + fmt.Sprintf("Encountered error in tool execution: subagent %q not found or not allowed to be invoked", typeName)
	return true, piagent.AgentToolResult{
		Content: []pi.ToolResultMessageContent{pi.TextContent{Text: text}},
	}, nil
}

func (t *invokeTool) resolveSystemPrompt(args invokeArgs) string {
	systemPrompt := args.SystemPrompt
	if t.defs != nil && args.TypeName != "" {
		if def, ok := t.defs.Get(args.TypeName); ok {
			if def.SystemPrompt != "" {
				systemPrompt = def.SystemPrompt
			}
		}
	}
	if args.Role != "" {
		if systemPrompt != "" {
			systemPrompt = "You are a subagent acting as: " + args.Role + ".\n\n" + systemPrompt
		} else {
			systemPrompt = "You are a subagent acting as: " + args.Role + "."
		}
	}
	return systemPrompt
}

func invokeSuccessResult(res Result) (piagent.AgentToolResult, error) {
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
	return piagent.AgentToolResult{
		Content: []pi.ToolResultMessageContent{pi.TextContent{Text: string(encoded)}},
	}, nil
}

func invokeErrorResult(res Result, err error) (piagent.AgentToolResult, error) {
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
	return piagent.AgentToolResult{
		Content: []pi.ToolResultMessageContent{pi.TextContent{Text: string(encoded)}},
		IsError: true,
	}, nil
}

func (t *invokeTool) Execute(ctx context.Context, _ string, params json.RawMessage, _ piagent.ToolUpdateCallback) (piagent.AgentToolResult, error) {
	args, err := parseInvokeArgs(params, t.deps)
	if err != nil {
		return piagent.AgentToolResult{}, err
	}
	if name := strings.TrimSpace(args.AgentName); name != "" {
		return invokeBuiltinSubagent(ctx, name, args.Prompt)
	}
	if handled, res, err := t.checkTypeName(ctx, args); handled {
		return res, err
	}
	systemPrompt := t.resolveSystemPrompt(args)
	res, err := Invoke(ctx, Invocation{
		Prompt:       args.Prompt,
		Model:        args.Model,
		SystemPrompt: systemPrompt,
		Worktree:     args.Worktree,
	}, t.deps)
	if err != nil {
		return invokeErrorResult(res, err)
	}
	return invokeSuccessResult(res)
}

func invokeBuiltinSubagent(ctx context.Context, name, prompt string) (piagent.AgentToolResult, error) {
	reg := builtinSubagentRegistry()
	res, err := reg.Invoke(ctx, name, prompt)
	if err != nil {
		body := map[string]any{
			"ok":         false,
			"agent_name": name,
			"error":      err.Error(),
		}
		encoded, _ := json.Marshal(body)
		return piagent.AgentToolResult{
			Content: []pi.ToolResultMessageContent{pi.TextContent{Text: string(encoded)}},
			IsError: true,
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
	return piagent.AgentToolResult{
		Content: []pi.ToolResultMessageContent{pi.TextContent{Text: string(encoded)}},
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
	return builtinRegistry
}

func SetBuiltinSubagentRegistry(r *builtinsubagent.Registry) {
	builtinRegistryMu.Lock()
	defer builtinRegistryMu.Unlock()
	if r == nil {
		builtinRegistry = nil
		return
	}
	builtinRegistry = r
}

func BuiltinSubagentCatalog() string {
	reg := builtinSubagentRegistry()
	if reg == nil {
		return ""
	}
	return reg.Catalog()
}
