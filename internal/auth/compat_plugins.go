package auth

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// pluginConfigPath is the file the reference writes plugin enablement to:
//
//	{"plugins": {"<name>": {"enabled": bool}}, "userSettings": {...}}
//
// Measured on the live machine (cycle 198): all four stock plugins appear here
// with "enabled": true.
func pluginConfigPath() string { return filepath.Join(GeminiConfigHome(), "config.json") }

// PluginEnabled reports config.json's plugins.<name>.enabled. A missing file
// or a missing entry reads as (false, nil): the reference only writes the row
// once a plugin is installed directory-side, and its /plugin picker renders
// everything without one as [disabled].
func PluginEnabled(name string) (bool, error) {
	data, err := os.ReadFile(pluginConfigPath())
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	data = stripBOM(data)
	var doc struct {
		Plugins map[string]struct {
			Enabled bool `json:"enabled"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return false, err
	}
	return doc.Plugins[name].Enabled, nil
}

// SetPluginEnabled writes config.json's plugins.<name>.enabled, creating the
// file, the plugins map and the entry as needed. Every other field is
// preserved — the file also carries userSettings, and the reference rewrites it
// in full rather than truncating it (cycle 198 probe: pressing space on
// /plugin's Installed tab flips exactly this flag, and it survives a restart).
//
// The error is returned so the panel's space handler can leave the row
// unchanged instead of lying about the new state.
func SetPluginEnabled(name string, enabled bool) error {
	if err := os.MkdirAll(GeminiConfigHome(), 0700); err != nil {
		return err
	}
	m := map[string]any{}
	switch data, err := os.ReadFile(pluginConfigPath()); {
	case err == nil:
		data = stripBOM(data)
		if jerr := json.Unmarshal(data, &m); jerr != nil {
			m = map[string]any{}
		}
	case !os.IsNotExist(err):
		return err
	}
	plugins, _ := m["plugins"].(map[string]any)
	if plugins == nil {
		plugins = map[string]any{}
		m["plugins"] = plugins
	}
	entry, _ := plugins[name].(map[string]any)
	if entry == nil {
		entry = map[string]any{}
		plugins[name] = entry
	}
	entry["enabled"] = enabled
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(pluginConfigPath(), out, 0600)
}

// ClearPluginEnabled deletes the plugins.<name> entry from config.json. Used
// by the /plugin ctrl+s Uninstall path so a future installedPluginRows()
// walk does not surface a plugin whose directory was just removed.
//
// A missing file or a missing entry is a no-op (returns nil); the caller
// has already shown the user a successful uninstall and a stale entry would
// only flip the badge to [disabled], which is the same state a fresh
// install yields. Every other field of config.json is preserved —
// userSettings and the other plugins.<name> entries ride through.
func ClearPluginEnabled(name string) error {
	data, err := os.ReadFile(pluginConfigPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	data = stripBOM(data)
	m := map[string]any{}
	if jerr := json.Unmarshal(data, &m); jerr != nil {
		// A malformed config.json is the caller's problem; we don't
		// invent one. Returning the parse error lets uninstall surface
		// it after the directory has already been removed.
		return jerr
	}
	plugins, _ := m["plugins"].(map[string]any)
	if plugins == nil {
		return nil
	}
	if _, ok := plugins[name]; !ok {
		return nil
	}
	delete(plugins, name)
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(pluginConfigPath(), out, 0600)
}

// disableAllPluginsField is the top-level boolean in
// ~/.gemini/config/config.json that gates every plugin regardless of its
// per-plugin enabled flag. The field was missing from the v1.3.5 schema
// (cycle 198 measured config.json had no top-level boolean) — it is the
// cycle-235 close-gap for the AGY 1.2.16 changelog line that complained
// /plugin and /mcp "showed 'enabled' or 'disabled' without indicating
// they were also blocked by disableAllPlugins". Env var wins when set so
// an operator can override the on-disk setting without rewriting it.
func disableAllPluginsField() bool {
	data, err := os.ReadFile(pluginConfigPath())
	if err != nil {
		return false
	}
	data = stripBOM(data)
	var doc struct {
		DisableAllPlugins bool `json:"disableAllPlugins"`
	}
	if json.Unmarshal(data, &doc) != nil {
		return false
	}
	return doc.DisableAllPlugins
}

// PluginsBlocked reports whether the global plugin gate is on. The env
// var AGY_CLI_DISABLE_PLUGINS wins; otherwise the on-disk
// disableAllPlugins field is consulted. Any non-empty value (after trim)
// is enough — the value is treated as a switch, not a token list.
//
// Returning true means every plugin row in /plugin's Installed tab
// should render its status as "blocked" instead of "enabled"/"disabled".
// The /mcp panel uses the matching MCPsBlocked helper.
func PluginsBlocked() bool {
	if strings.TrimSpace(os.Getenv("AGY_CLI_DISABLE_PLUGINS")) != "" {
		return true
	}
	return disableAllPluginsField()
}
