package tuilib

import (
	"path/filepath"
	"strings"
)

// RelPath converts targetPath into a clean relative path anchored to workspaceRoot using filepath.Rel.
// - If targetPath is within workspaceRoot, it returns the relative path (e.g. "internal/tui/bridge.go").
// - If targetPath equals workspaceRoot, it returns ".".
// - If targetPath is outside workspaceRoot (or workspaceRoot is empty), it collapses
//   any leading user home directory to "~" (e.g. "~/other/dir") via ShortenCwd.
// - It gracefully handles "file://" URI schemes on both targetPath and workspaceRoot.
func RelPath(workspaceRoot, targetPath string) string {
	if targetPath == "" {
		return ""
	}
	cleanTarget := filepath.Clean(strings.TrimPrefix(targetPath, "file://"))

	if workspaceRoot != "" {
		cleanRoot := filepath.Clean(strings.TrimPrefix(workspaceRoot, "file://"))
		if rel, err := filepath.Rel(cleanRoot, cleanTarget); err == nil {
			if rel == "." {
				return "."
			}
			if !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel) {
				return filepath.ToSlash(rel)
			}
		}
	}

	return ShortenCwd(cleanTarget)
}

// DisplayPath formats any path for user-facing UI presentation.
// It prioritizes relative paths within workspaceRoot, then falls back to "~/" for home paths.
func DisplayPath(workspaceRoot, targetPath string) string {
	return RelPath(workspaceRoot, targetPath)
}
