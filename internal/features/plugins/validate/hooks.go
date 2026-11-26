package validate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

// hookEvents are the event names the reference recognises in hooks.json.
const hookEvents = "SessionStart, PreInvocation, PostInvocation, Stop, PreToolUse, PostToolUse"

// scanValidateHooks counts hooks.json events and warns about unknown event
// keys in the legacy flat shape.
func scanValidateHooks(tree fs.FS, rep *Report) {
	raw, err := fs.ReadFile(tree, "hooks.json")
	if err != nil {
		return
	}
	if bytes.HasPrefix(raw, utf8BOM) {
		rep.Findings = append(rep.Findings, Finding{
			Severity: SeverityError,
			File:     "hooks.json",
			Message:  "starts with a UTF-8 byte order mark, which the loader can't parse. Save the file without one. The loader skips this whole file",
		})
		return
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		rep.Findings = append(rep.Findings, Finding{
			Severity: SeverityError,
			File:     "hooks.json",
			Message:  "failed to parse hooks.json: " + err.Error() + ". The loader skips this whole file",
		})
		return
	}
	delete(top, "$schema")
	if hooksRaw, ok := top["hooks"]; ok {
		validateWrappedHooks(hooksRaw, len(top), rep)
		return
	}
	validateFlatHooks(top, rep)
}

// validateWrappedHooks validates Claude Code style wrapped hooks ("hooks": {...}).
func validateWrappedHooks(hooksRaw json.RawMessage, topCount int, rep *Report) {
	var events map[string]json.RawMessage
	if err := json.Unmarshal(hooksRaw, &events); err != nil {
		rep.Findings = append(rep.Findings, Finding{
			Severity: SeverityError,
			File:     "hooks.json",
			Message:  "failed to parse hooks.json: " + hookErrorText(err.Error()) + ". The loader skips this whole file",
		})
		return
	}
	names := make([]string, 0, len(events))
	for name := range events {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		var groups []json.RawMessage
		if err := json.Unmarshal(events[name], &groups); err != nil {
			rep.Findings = append(rep.Findings, Finding{
				Severity: SeverityError,
				File:     "hooks.json",
				Message:  fmt.Sprintf("failed to parse hooks.json: json: cannot unmarshal object into Go struct field .hooks.%s of type []jsonhook.ToolHookGroup. The loader skips this whole file", name),
			})
			return
		}
		for _, g := range groups {
			var spec map[string]json.RawMessage
			if err := json.Unmarshal(g, &spec); err != nil {
				continue
			}
			if _, ok := spec["command"]; !ok {
				rep.Findings = append(rep.Findings, Finding{
					Severity: SeverityError,
					File:     "hooks.json",
					Message:  `invalid hook "hooks": command hook must specify 'command'. The loader skips this whole file`,
				})
				return
			}
			checkHookCommands(g, rep)
		}
	}
	rep.Findings = append(rep.Findings, Finding{
		Severity: SeverityNote,
		File:     "hooks.json",
		Message:  `"hooks" wraps the events, Claude Code style, so its handlers load as one hook named "hooks". Name each hook at the top level instead: {"my-hook": {"PreToolUse": [...]}}`,
	})
	rep.Components.Hooks = topCount
}

// validateFlatHooks validates legacy flat style hooks ({"my-hook": {"PreToolUse": ...}}).
func validateFlatHooks(top map[string]json.RawMessage, rep *Report) {
	rep.Components.Hooks = len(top)
	known := map[string]bool{}
	for _, e := range strings.Split(hookEvents, ", ") {
		known[e] = true
	}
	names := make([]string, 0, len(top))
	for name := range top {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		var spec map[string]json.RawMessage
		if err := json.Unmarshal(top[name], &spec); err != nil {
			rep.Findings = append(rep.Findings, Finding{
				Severity: SeverityError,
				File:     "hooks.json",
				Message:  "failed to parse hooks.json: " + hookErrorText(err.Error()) + ". The loader skips this whole file",
			})
			continue
		}
		keys := make([]string, 0, len(spec))
		for key := range spec {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if !known[key] {
				rep.Findings = append(rep.Findings, Finding{
					Severity: SeverityWarning,
					File:     "hooks.json",
					Message:  fmt.Sprintf("hook %q: unknown event %q is ignored (events: %s)", name, key, hookEvents),
				})
			} else {
				checkHookCommands(spec[key], rep)
			}
		}
	}
}

func checkHookCommands(raw json.RawMessage, rep *Report) {
	if len(raw) == 0 {
		return
	}
	var single map[string]any
	if err := json.Unmarshal(raw, &single); err == nil {
		if cmd, ok := single["command"].(string); ok {
			if strings.Contains(cmd, "${PLUGIN_ROOT}") || strings.Contains(cmd, "$PLUGIN_ROOT") {
				rep.Findings = append(rep.Findings, Finding{
					Severity: SeverityWarning,
					File:     "hooks.json",
					Message:  `hook command uses ${PLUGIN_ROOT} or $PLUGIN_ROOT; hooks already run inside the plugin folder, suggest using a relative path instead`,
				})
			}
		}
		return
	}
	var list []map[string]any
	if err := json.Unmarshal(raw, &list); err == nil {
		for _, item := range list {
			if cmd, ok := item["command"].(string); ok {
				if strings.Contains(cmd, "${PLUGIN_ROOT}") || strings.Contains(cmd, "$PLUGIN_ROOT") {
					rep.Findings = append(rep.Findings, Finding{
						Severity: SeverityWarning,
						File:     "hooks.json",
						Message:  `hook command uses ${PLUGIN_ROOT} or $PLUGIN_ROOT; hooks already run inside the plugin folder, suggest using a relative path instead`,
					})
				}
			}
		}
	}
}

// hookErrorText rewrites encoding/json's anonymous type names to the
// reference's `jsonhook` package names.
func hookErrorText(msg string) string {
	msg = strings.ReplaceAll(msg, "map[string]jsontext.Value", "map[string]jsonhook.JSONHookSpec")
	msg = strings.ReplaceAll(msg, "[]jsontext.Value", "[]jsonhook.ToolHookGroup")
	msg = strings.ReplaceAll(msg, "jsontext.Value", "jsonhook.JSONHookSpec")
	return msg
}

// scanValidateMCPServers counts MCP servers from mcp.json (if Agent Plugins spec) or mcp_config.json.
func scanValidateMCPServers(tree fs.FS, rep *Report) {
	if rep.AgentPluginsSpec {
		mcpRaw, mcpErr := fs.ReadFile(tree, "mcp.json")
		if mcpErr == nil {
			mcpRaw = bytes.TrimPrefix(mcpRaw, utf8BOM)
			var config struct {
				MCPServers map[string]json.RawMessage `json:"mcpServers"`
			}
			if err := json.Unmarshal(mcpRaw, &config); err == nil && len(config.MCPServers) > 0 {
				rep.Components.MCPServers = len(config.MCPServers)
			} else {
				var flat map[string]json.RawMessage
				if err2 := json.Unmarshal(mcpRaw, &flat); err2 == nil {
					delete(flat, "$schema")
					rep.Components.MCPServers = len(flat)
				}
			}
		}
		if _, err := fs.Stat(tree, "mcp_config.json"); err == nil {
			rep.Findings = append(rep.Findings, Finding{
				Severity: SeverityWarning,
				File:     "mcp_config.json",
				Message:  "ignored while enable-agent-plugins-spec is on: declare the servers in mcp.json too, and the fields it can't express in mcp_config.json",
			})
		}
		if rep.Components.MCPServers > 0 {
			return
		}
	} else {
		if _, err := fs.Stat(tree, "mcp.json"); err == nil {
			schemaVal := agentPluginsSchemaPrefix + agentPluginsVersion + "/plugin.schema.json"
			rep.Findings = append(rep.Findings, Finding{
				Severity: SeverityWarning,
				File:     "mcp.json",
				Message:  fmt.Sprintf("never loaded: mcp.json is read only for Agent Plugins plugins (plugin.json \"$schema\": %q). Move the servers to mcp_config.json", schemaVal),
			})
		}
	}

	raw, err := fs.ReadFile(tree, "mcp_config.json")
	if err != nil {
		return
	}
	raw = bytes.TrimPrefix(raw, utf8BOM)
	var config struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		rep.Findings = append(rep.Findings, Finding{
			Severity: SeverityError,
			File:     "mcp_config.json",
			Message:  "failed to parse mcp_config.json: " + err.Error() + ". The loader skips this whole file",
		})
		return
	}
	rep.Components.MCPServers = len(config.MCPServers)
}
