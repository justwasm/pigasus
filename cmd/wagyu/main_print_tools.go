package main

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"sort"

	"github.com/justwasm/pigasus/internal/agent"
	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/permissions"
	"github.com/justwasm/pigasus/internal/sandbox"
	"github.com/justwasm/pigasus/internal/tools"
)

func toolDeclsFromRegistry(reg *tools.Registry) []backend.ToolDecl {
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

// advertisedToolDecls is the declaration list the model sees.
func advertisedToolDecls(agentBody string, reg *tools.Registry) []backend.ToolDecl {
	if agentBody != "" {
		return toolDeclsFromRegistry(reg)
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

// registryRunner adapts a tools.Registry to agent.ToolRunner.
type registryRunner struct{ reg *tools.Registry }

func (r *registryRunner) Run(ctx context.Context, call backend.ToolCall) (string, error) {
	return r.reg.Dispatch(ctx, call.Name, call.Arguments)
}

// permissionRunner gates every tool call through the recovered permission policy.
type permissionRunner struct {
	inner   agent.ToolRunner
	policy  *permissions.Policy
	sandbox sandbox.Sandbox
	Denials []string
}

func (r *permissionRunner) Run(ctx context.Context, call backend.ToolCall) (string, error) {
	target := toolTarget(call)
	if r.sandbox.Enabled && (call.Name == "run_command" || call.Name == "run_shell_command") {
		if res := r.sandbox.CheckCommand(target); res.Outside {
			r.Denials = append(r.Denials, call.Name)
			return sandboxDeniedToolResult(call.Name, target, r.sandbox.AllowedPaths()), nil
		}
	}
	if v := r.policy.Decide(permissions.Request{Tool: call.Name, Target: target}); v == permissions.Allow {
		res, err := r.inner.Run(ctx, call)
		if err == nil {
			budget := tools.ApplyOutputBudget(res, "", tools.DefaultMaxResultTokens)
			res = budget.Content
		}
		return res, err
	}
	r.Denials = append(r.Denials, call.Name)
	return permissions.DeniedToolResult(call.Name, target), nil
}

func sandboxDeniedToolResult(tool, target string, allowed []string) string {
	return "Sandbox blocked '" + tool + "' on '" + target + "': the terminal sandbox only permits commands that read or write the workspace, the agent's artifact directory, or the per-conversation scratch directory. Re-run with paths inside those, or run /bypass-sandbox in the TUI to lift the restriction for this call only."
}

func toolTarget(call backend.ToolCall) string {
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

// printModePolicy builds the permission policy for a print-mode session.
func printModePolicy(fs *flag.FlagSet) permissions.Policy {
	mode := flagStringValue(fs, "mode")
	if mode == "" {
		if v := auth.SettingsString("agentMode"); isKnownMode(v) {
			mode = v
		}
	}
	p := permissions.Policy{
		Mode:            mode,
		SkipPermissions: flagBoolValue(fs, "dangerously-skip-permissions"),
		Strict:          permissions.StrictFromEnv(os.Getenv("OPENAGY_PERMISSION_POLICY")),
	}
	if settings, err := permissions.LoadRules(filepath.Join(auth.AntigravityHome(), "settings.json")); err == nil {
		p.Allow = settings.Allow
		p.Deny = settings.Deny
	}
	if p.SkipPermissions {
		printModeSilenceLogger.Println(permissions.SkipPermissionsNotice)
	}
	return p
}

func isKnownMode(v string) bool {
	switch v {
	case permissions.ModeAuto, permissions.ModePlan, permissions.ModeAcceptEdits:
		return true
	}
	return false
}

// printModeSandbox builds the terminal sandbox for a print-mode session.
func printModeSandbox(fs *flag.FlagSet, workspace, conversationID string) sandbox.Sandbox {
	if flagBoolValue(fs, "dangerously-skip-permissions") || os.Getenv("OPENAGY_DISABLE_SANDBOX") == "1" {
		return sandbox.Sandbox{Enabled: false, Cwd: workspace}
	}
	enabled := false
	if v, ok := auth.SettingsBool("enableTerminalSandbox"); ok {
		enabled = v
	}
	return sandbox.Resolve(enabled, workspace, auth.AntigravityHome(), conversationID)
}
