package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/justwasm/pigasus/internal/core/confhome"
)

// PluginControls mirrors the `pluginControls` section of managed admin_settings.json.
type PluginControls struct {
	PluginsDisabled     bool `json:"pluginsDisabled"`
	MarketplaceDisabled bool `json:"marketplaceDisabled"`
}

// AdminSettingsPath returns the managed admin_settings.json path for the current OS,
// or $ANTIGRAVITY_ADMIN_SETTINGS when set.
func AdminSettingsPath() string {
	if p := strings.TrimSpace(os.Getenv("ANTIGRAVITY_ADMIN_SETTINGS")); p != "" {
		return p
	}
	switch runtime.GOOS {
	case "darwin":
		return "/Library/Application Support/Antigravity/admin_settings.json"
	case "linux":
		return "/etc/antigravity/admin_settings.json"
	default:
		return ""
	}
}

// ReadAdminPluginControls reads pluginControls from managed admin_settings.json.
// Missing or unreadable files return zero-value PluginControls (nothing disabled).
func ReadAdminPluginControls() PluginControls {
	candidates := []string{}
	if p := AdminSettingsPath(); p != "" {
		candidates = append(candidates, p)
	}
	if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
		candidates = append(candidates, filepath.Join(home, ".antigravity", "admin_settings.json"))
	}
	for _, p := range candidates {
		raw, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		raw = confhome.StripBOM(raw)
		var doc struct {
			PluginControls PluginControls `json:"pluginControls"`
		}
		if err := json.Unmarshal(raw, &doc); err == nil {
			return doc.PluginControls
		}
	}
	return PluginControls{}
}
