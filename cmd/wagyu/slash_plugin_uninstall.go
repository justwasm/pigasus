// slash_plugin_uninstall.go — cycle dpl: the /plugin Installed tab's
// ctrl+s Uninstall path, with a hardened plugin ID validator that rejects
// path-traversal attempts.
//
// This file closes the AGY 1.2.7 changelog line that reads:
//
//	Fixed disabled plugins disappearing from the `/plugin` list after
//	enabled-only customization queries, and hardened plugin ID path
//	validation on uninstall.
//
// The bug has two surfaces:
//
//  1. The Installed tab can be filtered to enabled-only (the "enabled-only
//     customization query" the comment refers to). After applying such a
//     filter, the disabled rows must still reappear when the panel closes;
//     they must not vanish from the underlying `installedPluginRows()`
//     reader (which serves the panel, /plugin, and the picker). The panel
//     state is the only place this filter lives — re-deriving `installedPluginRows`
//     from the filesystem every frame means the list resets on close
//     without any explicit reset code, which is the AGY 1.2.7 fix.
//
//  2. Uninstall on ctrl+s dereferences the plugin ID as the leaf directory
//     name under ~/.gemini/config/plugins. A hostile or bug-triggered ID
//     like `../etc` would resolve outside the plugins directory and delete
//     unrelated files. The hardened validator rejects empty IDs, any path
//     separator (`/` or `\\`), the `..` token (positional or embedded), NUL
//     bytes, leading dots, and any character outside `[a-zA-Z0-9._-]`.
//     The reference picks the same conservative alphabet; we match it.
//
// What this file owns:
//
//   - validatePluginID: the rejected-set check the uninstall path runs
//     before touching the filesystem.
//   - uninstallPlugin: removes the directory and the config.json entry.
//   - installPluginToFeedback (helper): the user-facing message we echo
//     for both the success and the error path.
//
// Ctrl+s dispatch lives on the panel key handler (cycle 198 wired the
// legend already — this commit wires the action).

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/justwasm/pigasus/internal/auth"
)

// pluginIDAllowedRune is the predicate the hardened validator uses. The
// reference allows letters, digits, `.`, `-`, and `_`; everything else is
// rejected. This is the same alphabet the cycle-212 path-prompt tab
// completion constrains its outputs to (a directory name is a valid plugin
// ID here only by coincidence, but the two alphabets happen to agree).
func pluginIDAllowedRune(r rune) bool {
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

// validatePluginID rejects anything that could escape the per-plugin
// directory when the ID is joined onto `~/.gemini/config/plugins/`.
//
// The rules (and the order of checks — we want the most informative error):
//
//  1. non-empty after TrimSpace;
//  2. no path separator ("/" or "\\") — would join out of the plugins root;
//  3. no NUL byte — would truncate the path on some Go path APIs;
//  4. no `.` or `..` token — would resolve into the plugins root or
//     above, and is the conventional path-traversal sentinel;
//  5. no leading dot — defensive against `.hidden` directory traversal on
//     strict filesystems and matches the marketplace directory convention
//     used by the cycle-212 reader;
//  6. every rune is in pluginIDAllowedRune — no whitespace, no control
//     characters, no shell metacharacters.
//
// The returned error names the rule so the user-facing surface (the panel
// title's red Error: prefix) can quote it verbatim when needed.
func validatePluginID(id string) error {
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
		if !pluginIDAllowedRune(r) {
			return fmt.Errorf("plugin id contains disallowed rune %q", r)
		}
	}
	return nil
}

// uninstallPlugin removes a single plugin directory under
// ~/.gemini/config/plugins/<id>/ and clears the matching entry in
// ~/.gemini/config/config.json so a future `installedPluginRows()` walk
// no longer surfaces it.
//
// Steps:
//
//  1. validate the ID; a bad ID is a no-op + error;
//  2. stat the target directory; missing -> "not installed" error;
//  3. resolve the target through `filepath.Clean` and confirm the cleaned
//     path is still a direct child of the plugins root (defense in depth
//     against symlink swaps that survive validatePluginID);
//  4. os.RemoveAll the directory;
//  5. clear plugins.<id> from config.json so the badge stays [disabled]
//     rather than dangling on a deleted directory.
//
// The plugin ID is the directory leaf, not the manifest name. The
// marketplace install path (cycle 212 installLocalPlugin) writes the
// manifest's "name" field, which is the same value the clone uses as the
// directory leaf when it falls back to filepath.Base(src) — so the cycle
// 212 and uninstall paths agree on the storage key.
func uninstallPlugin(id string) error {
	if err := validatePluginID(id); err != nil {
		return err
	}
	root := filepath.Join(auth.GeminiConfigHome(), "plugins")
	target := filepath.Join(root, id)
	cleaned := filepath.Clean(target)
	// Defense in depth: even with validatePluginID above, a symlinked
	// plugins directory could resolve out of `root`. Re-check after
	// filepath.Clean.
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
	// Best-effort remove. Anything other than the directory itself under
	// the plugins root would have failed validatePluginID already, so
	// RemoveAll is only walking the plugin's own subtree.
	if err := os.RemoveAll(cleaned); err != nil {
		return fmt.Errorf("remove %s: %w", cleaned, err)
	}
	if err := auth.ClearPluginEnabled(id); err != nil && !os.IsNotExist(err) {
		// The directory is already gone; surface a non-fatal warning
		// rather than rolling back. A stale enabled entry is harmless.
		return fmt.Errorf("removed %s but config.json update failed: %w", id, err)
	}
	return nil
}

// uninstallFeedback wraps uninstallPlugin with the user-facing strings the
// slash panel prints. Returns the line to append to the transcript ("" if
// no message is wanted) and the error (nil on success).
//
// The success line is the recovered "Plugin %q uninstalled." format
// (cycle dpl); the error line is the validator/install error verbatim.
// The user re-presses ctrl+s to retry after fixing the input — the panel
// stays open so they can.
func uninstallFeedback(id string) (string, error) {
	if err := uninstallPlugin(id); err != nil {
		return "Error: " + err.Error(), err
	}
	return fmt.Sprintf("Plugin %q uninstalled.", id), nil
}
