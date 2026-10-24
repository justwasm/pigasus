// Package pluginsui provides UI rendering, search, filtering, and local install/uninstall
// logic for the /plugin slash command panel.
package pluginsui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/justwasm/pigasus/internal/auth"
)

// PluginIDAllowedRune is the predicate the hardened validator uses. The
// reference allows letters, digits, '.', '-', and '_'; everything else is
// rejected.
func PluginIDAllowedRune(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z':
		return true
	case r >= 'A' && r <= 'Z':
		return true
	case r >= '0' && r <= '9':
		return true
	case r == '-' || r == '_' || r == '.':
		return true
	}
	return false
}

// ValidatePluginID rejects anything that could escape the per-plugin
// directory when the ID is joined onto ~/.gemini/config/plugins/.
func ValidatePluginID(id string) error {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return errors.New("plugin id is empty")
	}
	if strings.ContainsRune(trimmed, 0) {
		return errors.New("plugin id contains NUL")
	}
	if strings.ContainsRune(trimmed, '/') || strings.ContainsRune(trimmed, '\\') {
		return errors.New("plugin id contains a path separator")
	}
	if trimmed == "." || trimmed == ".." || strings.HasPrefix(trimmed, "../") ||
		strings.HasPrefix(trimmed, "..\\") {
		return errors.New("plugin id is a path-traversal token")
	}
	if strings.HasPrefix(trimmed, ".") {
		return errors.New("plugin id must not start with '.'")
	}
	for _, r := range trimmed {
		if !PluginIDAllowedRune(r) {
			return fmt.Errorf("plugin id contains disallowed rune %q", r)
		}
	}
	return nil
}

// UninstallPlugin removes a single plugin directory under
// ~/.gemini/config/plugins/<id>/ and clears the matching entry in
// ~/.gemini/config/config.json so a future walk no longer surfaces it.
func UninstallPlugin(id string) error {
	if err := ValidatePluginID(id); err != nil {
		return err
	}
	root := filepath.Join(auth.GeminiConfigHome(), "plugins")
	target := filepath.Join(root, id)
	cleaned := filepath.Clean(target)
	if cleaned != filepath.Join(root, id) {
		return errors.New("plugin id resolves outside the plugins directory")
	}
	info, err := os.Stat(cleaned)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("plugin %q is not installed", id)
		}
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("plugin %q is not a directory", id)
	}
	if err := os.RemoveAll(cleaned); err != nil {
		return fmt.Errorf("remove %s: %w", cleaned, err)
	}
	if err := auth.ClearPluginEnabled(id); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removed %s but config.json update failed: %w", id, err)
	}
	return nil
}

// UninstallFeedback wraps UninstallPlugin with the user-facing strings the
// slash panel prints.
func UninstallFeedback(id string) (string, error) {
	if err := UninstallPlugin(id); err != nil {
		return "Error: " + err.Error(), err
	}
	return fmt.Sprintf("Plugin %q uninstalled.", id), nil
}
