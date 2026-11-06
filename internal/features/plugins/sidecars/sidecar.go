package sidecars

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"

	"github.com/justwasm/pigasus/internal/core/confhome"
	"github.com/justwasm/pigasus/internal/features/plugins/core"
)

var varRegex = regexp.MustCompile(`\$\{([a-zA-Z_0-9]+)\}`)

// ResolveSidecarTemplates expands variable placeholders (such as ${HOME} or ${ARG})
// in a sidecar config's workspaceUris, accessGrants, args, and env.
//
// Changelog 1.5.0: "Added a built-in ${HOME} variable to plugin sidecar.json templates,
// so a sidecar can name paths under the user's home directory in workspaceUris and accessGrants,
// such as file://${HOME}/worktrees; a sidecar argument named HOME still takes precedence."
func ResolveSidecarTemplates(config map[string]any, argumentValues map[string]string) map[string]any {
	if config == nil {
		return map[string]any{}
	}

	homeDir, _ := os.UserHomeDir()
	resolveVar := func(varName string) string {
		if varName == "HOME" {
			if val, ok := argumentValues["HOME"]; ok && val != "" {
				return val
			}
			return homeDir
		}
		if val, ok := argumentValues[varName]; ok {
			return val
		}
		return "${" + varName + "}"
	}

	expandText := func(s string) string {
		return varRegex.ReplaceAllStringFunc(s, func(m string) string {
			sub := varRegex.FindStringSubmatch(m)
			if len(sub) == 2 {
				return resolveVar(sub[1])
			}
			return m
		})
	}

	out := make(map[string]any, len(config))
	for k, v := range config {
		out[k] = v
	}

	expandSlice := func(items []any) []any {
		res := make([]any, len(items))
		for i, item := range items {
			if s, ok := item.(string); ok {
				res[i] = expandText(s)
			} else {
				res[i] = item
			}
		}
		return res
	}

	// Handle agentPermissions / agent_permissions
	for _, permKey := range []string{"agentPermissions", "agent_permissions"} {
		if rawPerm, ok := out[permKey].(map[string]any); ok {
			permCopy := make(map[string]any, len(rawPerm))
			for pk, pv := range rawPerm {
				permCopy[pk] = pv
			}
			// workspaceUris / workspace_uris
			for _, wsKey := range []string{"workspaceUris", "workspace_uris"} {
				if wsList, ok := permCopy[wsKey].([]any); ok {
					permCopy[wsKey] = expandSlice(wsList)
				} else if strList, ok := permCopy[wsKey].([]string); ok {
					expanded := make([]any, len(strList))
					for i, s := range strList {
						expanded[i] = expandText(s)
					}
					permCopy[wsKey] = expanded
				}
			}
			// accessGrants / access_grants
			for _, agKey := range []string{"accessGrants", "access_grants"} {
				if agList, ok := permCopy[agKey].([]any); ok {
					permCopy[agKey] = expandSlice(agList)
				} else if strList, ok := permCopy[agKey].([]string); ok {
					expanded := make([]any, len(strList))
					for i, s := range strList {
						expanded[i] = expandText(s)
					}
					permCopy[agKey] = expanded
				}
			}
			out[permKey] = permCopy
		}
	}

	// Handle args
	if rawArgs, ok := out["args"].([]any); ok {
		out["args"] = expandSlice(rawArgs)
	}

	return out
}

// FindSidecarConfig looks up sidecar.json for the given sidecarId across
// installed plugins and global config sidecars directory.
func FindSidecarConfig(sidecarID string) (map[string]any, string, error) {
	// 1. Check user/global sidecars directory
	home := confhome.Antigravity()
	userSidecarPath := filepath.Join(home, "sidecars", sidecarID, "sidecar.json")
	if data, err := os.ReadFile(userSidecarPath); err == nil {
		var cfg map[string]any
		if err := json.Unmarshal(data, &cfg); err == nil {
			return cfg, userSidecarPath, nil
		}
	}

	// Also check ~/.gemini/jetski/sidecars/<id>/sidecar.json
	userHome, _ := os.UserHomeDir()
	if userHome != "" {
		legacyPath := filepath.Join(userHome, ".gemini", "jetski", "sidecars", sidecarID, "sidecar.json")
		if data, err := os.ReadFile(legacyPath); err == nil {
			var cfg map[string]any
			if err := json.Unmarshal(data, &cfg); err == nil {
				return cfg, legacyPath, nil
			}
		}
	}

	// 2. Check installed plugins
	installed := core.InstalledPlugins()
	for _, p := range installed {
		pluginSidecarPath := filepath.Join(p.Path, "sidecars", sidecarID, "sidecar.json")
		if data, err := os.ReadFile(pluginSidecarPath); err == nil {
			var cfg map[string]any
			if err := json.Unmarshal(data, &cfg); err == nil {
				return cfg, pluginSidecarPath, nil
			}
		}
	}

	return nil, "", os.ErrNotExist
}
