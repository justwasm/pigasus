// turn_diff.go — the proxy's GetTurnDiff implementation.
//
// The reference describes a turn's changed files as a map path →
// FileDiffData, carrying the two full file versions and the line counts.
// This server derives them from the session workspace's working copy: the
// turn ran its tools there, so the uncommitted changes are the turn's diff.
package mockremoteagent

import (
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// sessionWorkspace resolves the directory a session's tools run in.
func sessionWorkspace(session *CascadeSession) string {
	if session != nil {
		for _, raw := range session.WorkspaceURIs {
			path := raw
			if u, err := url.Parse(raw); err == nil && u.Scheme == "file" {
				path, _ = url.PathUnescape(u.Path)
			} else {
				path = strings.TrimPrefix(raw, "file://")
				path, _ = url.PathUnescape(path)
			}
			if info, err := os.Stat(path); err == nil && info.IsDir() {
				return path
			}
		}
	}
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return "."
}

// workingCopyDiff reads the workspace's uncommitted changes into the
// proxy's FileDiffData map.
func workingCopyDiff(workspace string) (map[string]FileDiffData, int32, int32) {
	files := map[string]FileDiffData{}
	var totalAdd, totalDel int32

	// Staged and unstaged tracked changes both belong to the turn.
	for _, args := range [][]string{
		{"diff", "--numstat"},
		{"diff", "--cached", "--numstat"},
	} {
		out, err := gitOutput(workspace, args...)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(out, "\n") {
			fields := strings.SplitN(line, "\t", 3)
			if len(fields) != 3 {
				continue
			}
			path := fields[2]
			added, _ := strconv.Atoi(fields[0])
			deleted, _ := strconv.Atoi(fields[1])
			original, _ := gitOutput(workspace, "show", "HEAD:"+path)
			modified, _ := os.ReadFile(filepath.Join(workspace, path))
			files[path] = FileDiffData{
				Additions:        int32(added),
				Deletions:        int32(deleted),
				OriginalContents: original,
				ModifiedContents: string(modified),
			}
			totalAdd += int32(added)
			totalDel += int32(deleted)
		}
	}
	// Untracked files are part of the turn too: the tool created them.
	if out, err := gitOutput(workspace, "ls-files", "--others", "--exclude-standard"); err == nil {
		for _, path := range strings.Split(out, "\n") {
			path = strings.TrimSpace(path)
			if path == "" {
				continue
			}
			if _, exists := files[path]; exists {
				continue
			}
			modified, err := os.ReadFile(filepath.Join(workspace, path))
			if err != nil {
				continue
			}
			added := strings.Count(string(modified), "\n")
			if added == 0 && len(modified) > 0 {
				added = 1
			}
			files[path] = FileDiffData{
				Additions:        int32(added),
				ModifiedContents: string(modified),
			}
			totalAdd += int32(added)
		}
	}
	return files, totalAdd, totalDel
}

// gitOutput runs one git command in the workspace.
func gitOutput(workspace string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", workspace}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}
