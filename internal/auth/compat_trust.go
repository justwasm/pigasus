package auth

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// AddTrustedWorkspace appends dir to the trustedWorkspaces array in
// ~/.gemini/antigravity-cli/settings.json, preserving every other field
// (model, permissions, colorScheme, …). It is the persistence half of the
// TUI /add-dir command (cycle 103) and the on-startup workspace-trust
// gate (cycle 125).
//
// Evidence: the key's struct tag is recovered as
// `"TrustedWorkspaces"json:"trustedWorkspaces,omitempty"`; the reference keeps
// its session working directories in this array (driving the reference over a
// pty adds each session cwd to it); and /add-dir reports `Added %s to workspace`.
// That /add-dir and the session-cwd trust path share this one array is
// 【推测】 — it is the only directory list in settings.json.
//
// A directory already present is a no-op (no duplicate entry, no rewrite).
//
// Cycle 125: when settings.json doesn't yet exist (the user accepted the
// trust prompt on a first-launch session before WriteCompatDefaults
// had a chance to materialise the file), AddTrustedWorkspace creates
// it from scratch rather than returning ENOENT. The TUI's accept
// handler would otherwise silently fail and the next launch would
// re-prompt.
func AddTrustedWorkspace(dir string) error {
	return updateOrCreateSettings(func(m map[string]any) {
		var list []string
		if raw, ok := m["trustedWorkspaces"].([]any); ok {
			for _, v := range raw {
				if s, ok := v.(string); ok {
					list = append(list, s)
				}
			}
		}
		for _, s := range list {
			if s == dir {
				return // already trusted; leave the file unchanged
			}
		}
		m["trustedWorkspaces"] = append(list, dir)
	})
}

// IsWorkspaceTrusted reports whether dir appears in settings.json's
// trustedWorkspaces list. Cycle 125: the on-startup workspace-trust gate
// calls this with `os.Getwd()` and shows the trust dialog when the
// answer is false.
//
// The comparison is exact (string equality on the canonicalised path);
// neither the v1.3.5 binary nor this clone walk the path tree, so a
// trusted parent does not implicitly trust its children — this matches
// the reference's behaviour (driving the reference over a pty adds each
// session cwd individually, never its parents). dir and each stored
// workspace are run through filepath.EvalSymlinks so /tmp/foo (the
// macOS symlink) and /private/tmp/foo (the real path) compare equal.
// When a path doesn't exist yet (the on-startup cwd before the user
// has created any files there), EvalSymlinks fails and we fall back
// to filepath.Clean.
//
// A missing or unparseable settings.json is *not* an error: a fresh
// install has no trusted list, so IsWorkspaceTrusted returns (false,
// nil) and the gate proceeds to show the dialog.
func IsWorkspaceTrusted(dir string) (bool, error) {
	target := canonicalisePath(dir)
	path := filepath.Join(AntigravityConfigHome(), "settings.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	data = stripBOM(data)
	var s Settings
	if err := json.Unmarshal(data, &s); err != nil {
		return false, nil // corrupt file → treat as untrusted, surface the dialog
	}
	for _, w := range s.TrustedWorkspaces {
		if canonicalisePath(w) == target {
			return true, nil
		}
	}
	return false, nil
}

// canonicalisePath resolves symlinks where possible (so /tmp/foo and
// /private/tmp/foo match on macOS, where /tmp is a symlink) and falls
// back to filepath.Clean when the path doesn't exist. Used by
// IsWorkspaceTrusted so the on-startup trust gate sees a single
// representation regardless of how cwd was reached.
func canonicalisePath(p string) string {
	if real, err := filepath.EvalSymlinks(p); err == nil {
		return real
	}
	return filepath.Clean(p)
}

// LoadTrustedWorkspaces returns the trustedWorkspaces list verbatim,
// or an empty slice when settings.json is missing or the field is
// absent. Cycle 125: powers the cycle-103 /add-dir test and the
// on-startup workspace-trust gate. Path canonicalisation is left to
// callers (IsWorkspaceTrusted canonicalises; AddTrustedWorkspace
// canonicalises on write so a future read by either is consistent).
func LoadTrustedWorkspaces() []string {
	path := filepath.Join(AntigravityConfigHome(), "settings.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	data = stripBOM(data)
	var s Settings
	if err := json.Unmarshal(data, &s); err != nil {
		return nil
	}
	return s.TrustedWorkspaces
}
