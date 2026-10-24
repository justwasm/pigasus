// Package statusline manages external status-line scripts, pricing calculations,
// configuration, and rendering state for the Antigravity TUI.
package statusline

import (
	"strings"

	"github.com/justwasm/pigasus/internal/auth"
)

// Config is the recovered AGY 1.1.27 shape of the statusLine block in settings.json.
type Config struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Enabled bool   `json:"enabled"`
}

// LoadConfig reads the nested `statusLine` block from settings.json.
// Returns the script command and whether external status-line mode is active.
func LoadConfig() (command string, ok bool) {
	raw, err := auth.SettingsField("statusLine")
	if err != nil || raw == nil {
		return "", false
	}
	switch v := raw.(type) {
	case map[string]any:
		cmd, _ := v["command"].(string)
		en, _ := v["enabled"].(bool)
		if !en {
			return "", false
		}
		cmd = strings.TrimSpace(cmd)
		if cmd == "" {
			return "", false
		}
		return cmd, true
	}
	return "", false
}

// PersistVisibility writes the visible/hidden state into settings.json without
// clobbering the statusLine block.
func PersistVisibility(visible bool) {
	if raw, err := auth.SettingsField("statusLine"); err == nil {
		if _, ok := raw.(map[string]any); ok {
			_ = auth.SetSettingsField("statusLine.enabled", visible)
		}
	}
}
