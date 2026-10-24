package toolrunner

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/justwasm/pigasus/internal/agent"
	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/permissions"
	"github.com/justwasm/pigasus/internal/sandbox"
	"github.com/justwasm/pigasus/internal/tools"
)

// ToolDeclsFromRegistry extracts declarations from a tools.Registry.
func ToolDeclsFromRegistry(reg *tools.Registry) []backend.ToolDecl {
	names := reg.List()
	sort.Strings(names)
	decls := make([]backend.ToolDecl, 0, len(names))
	for _, n := range names {
		t, ok := reg.Get(n)
		if !ok {
			continue
		}
		decls = append(decls, backend.ToolDecl{Name: n, Parameters: t.Schema()})
	}
	return decls
}

// AdvertisedToolDecls returns the declaration list the model sees.
func AdvertisedToolDecls(agentBody string, reg *tools.Registry) []backend.ToolDecl {
	if agentBody != "" {
		return ToolDeclsFromRegistry(reg)
	}
	model := tools.ModelDeclarations()
	decls := make([]backend.ToolDecl, 0, len(model))
	for _, d := range model {
		decls = append(decls, backend.ToolDecl{
			Name:        d.Name,
			Description: d.Description,
			Parameters:  d.Parameters,
		})
	}
	return decls
}

// RegistryRunner adapts a tools.Registry to agent.ToolRunner.
type RegistryRunner struct {
	Reg *tools.Registry
}

// Run executes the tool call against the registry.
func (r *RegistryRunner) Run(ctx context.Context, call backend.ToolCall) (string, error) {
	return r.Reg.Dispatch(ctx, call.Name, call.Arguments)
}

// PermissionRunner gates every tool call through the recovered permission policy.
type PermissionRunner struct {
	Inner   agent.ToolRunner
	Policy  *permissions.Policy
	Sandbox sandbox.Sandbox
	Denials []string
}

// Run gates tool execution through policy and sandbox checks.
func (r *PermissionRunner) Run(ctx context.Context, call backend.ToolCall) (string, error) {
	target := ToolTarget(call)
	if r.Sandbox.Enabled && (call.Name == "run_command" || call.Name == "run_shell_command") {
		// BypassSandbox is a per-call decision (【一手】 agy 1.4.6): the model
		// may ask for one call to run outside the sandbox.
		if res := r.Sandbox.CheckCommandWithBypass(target, tools.BypassSandboxRequested(call.Arguments)); res.Outside {
			r.Denials = append(r.Denials, call.Name)
			return SandboxDeniedToolResult(call.Name, target, r.Sandbox.AllowedPaths()), nil
		}
	}
	if v := r.Policy.Decide(permissions.Request{Tool: call.Name, Target: target}); v == permissions.Allow {
		res, err := r.Inner.Run(ctx, call)
		if err == nil {
			budget := tools.ApplyOutputBudget(res, "", tools.DefaultMaxResultTokens)
			res = budget.Content
		}
		return res, err
	}
	r.Denials = append(r.Denials, call.Name)
	return permissions.DeniedToolResult(call.Name, target), nil
}

// SandboxDeniedToolResult formats the rejection message when sandbox blocks a command.
func SandboxDeniedToolResult(tool, target string, allowed []string) string {
	return "Sandbox blocked '" + tool + "' on '" + target + "': the terminal sandbox only permits commands that read or write the workspace, the agent's artifact directory, or the per-conversation scratch directory. Re-run with paths inside those, or run /bypass-sandbox in the TUI to lift the restriction for this call only."
}

// ToolTarget extracts the target string from arguments for permission checks.
func ToolTarget(call backend.ToolCall) string {
	var p map[string]json.RawMessage
	if err := json.Unmarshal(call.Arguments, &p); err != nil {
		return ""
	}
	for _, key := range []string{
		"path", "command", "file", "target",
		"CommandLine", "AbsolutePath", "TargetFile", "DirectoryPath", "SearchPath",
	} {
		if raw, ok := p[key]; ok {
			var s string
			if json.Unmarshal(raw, &s) == nil {
				return s
			}
		}
	}
	return ""
}
