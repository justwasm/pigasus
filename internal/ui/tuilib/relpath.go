package tuilib

import (
	"path/filepath"
	"strings"
	"sync"

	"github.com/justwasm/pigasus/internal/core/workspace"
)

var (
	wsMu                sync.RWMutex
	activeWorkspaceRoot string
)

// SetWorkspaceRoot configures the active workspace root for UI relative path formatting.
func SetWorkspaceRoot(root string) {
	wsMu.Lock()
	defer wsMu.Unlock()
	activeWorkspaceRoot = root
}

// WorkspaceRoot returns the active workspace root, falling back to workspace.Current().
func WorkspaceRoot() string {
	wsMu.RLock()
	root := activeWorkspaceRoot
	wsMu.RUnlock()
	if root != "" {
		return root
	}
	return workspace.Current()
}

// RelPath converts targetPath into a clean relative path anchored to workspaceRoot using filepath.Rel.
//   - If targetPath is within workspaceRoot, it returns the relative path (e.g. "internal/tui/bridge.go").
//   - If targetPath equals workspaceRoot, it returns ".".
//   - If targetPath is already a clean relative path, it returns it directly.
//   - If workspaceRoot is empty, it falls back to WorkspaceRoot().
//   - If targetPath is outside workspaceRoot, it collapses any leading user home directory
//     to "~" (e.g. "~/other/dir") via ShortenCwd.
//   - It gracefully handles "file://" URI schemes on both targetPath and workspaceRoot.
func RelPath(workspaceRoot, targetPath string) string {
	if targetPath == "" {
		return ""
	}
	cleanTarget := filepath.Clean(strings.TrimPrefix(targetPath, "file://"))

	// If already a relative path not escaping parent, preserve as-is.
	if !filepath.IsAbs(cleanTarget) && !strings.HasPrefix(cleanTarget, "~") {
		if !strings.HasPrefix(cleanTarget, "..") {
			return filepath.ToSlash(cleanTarget)
		}
	}

	root := workspaceRoot
	if root == "" {
		root = WorkspaceRoot()
	}

	if root != "" {
		cleanRoot := filepath.Clean(strings.TrimPrefix(root, "file://"))
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

// FormatPath formats any path using the active workspace root.
func FormatPath(targetPath string) string {
	return RelPath("", targetPath)
}
