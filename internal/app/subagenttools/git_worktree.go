package subagenttools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	// MaxNonGitWorkspaceEntries is the 100,000 entry limit for non-git workspace clones.
	MaxNonGitWorkspaceEntries = 100000
	// MaxNonGitWorkspaceBytes is the 4 GiB size limit for non-git workspace clones.
	MaxNonGitWorkspaceBytes = 4 * 1024 * 1024 * 1024
)

// UncommittedState holds uncommitted changes from the parent workspace (AGY 1.5.5).
type UncommittedState struct {
	StagedDiff     []byte            `json:"staged_diff,omitempty"`
	UnstagedDiff   []byte            `json:"unstaged_diff,omitempty"`
	UntrackedFiles map[string][]byte `json:"untracked_files,omitempty"`
}

// IsEmpty reports whether there are no uncommitted changes.
func (s *UncommittedState) IsEmpty() bool {
	if s == nil {
		return true
	}
	return len(s.StagedDiff) == 0 && len(s.UnstagedDiff) == 0 && len(s.UntrackedFiles) == 0
}

// isGitRepo reports whether dir is inside a git repository.
func isGitRepo(dir string) bool {
	cmd := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	cmd.Dir = dir
	out, err := cmd.Output()
	return err == nil && strings.TrimSpace(string(out)) == "true"
}

// CaptureUncommittedState captures staged diff, unstaged diff, and untracked files
// from the workspace. Recovered from jetski/git_utils/git_utils.CaptureUncommittedState.
func CaptureUncommittedState(ctx context.Context, repoDir string) (*UncommittedState, error) {
	if !isGitRepo(repoDir) {
		return nil, errors.New("not a git repository")
	}

	state := &UncommittedState{
		UntrackedFiles: make(map[string][]byte),
	}

	// 1. Capture staged changes
	stagedCmd := exec.CommandContext(ctx, "git", "diff", "--cached", "--binary")
	stagedCmd.Dir = repoDir
	if out, err := stagedCmd.Output(); err == nil && len(out) > 0 {
		state.StagedDiff = out
	}

	// 2. Capture unstaged changes
	unstagedCmd := exec.CommandContext(ctx, "git", "diff", "--binary")
	unstagedCmd.Dir = repoDir
	if out, err := unstagedCmd.Output(); err == nil && len(out) > 0 {
		state.UnstagedDiff = out
	}

	// 3. Capture untracked files
	untrackedCmd := exec.CommandContext(ctx, "git", "ls-files", "--others", "--exclude-standard")
	untrackedCmd.Dir = repoDir
	if out, err := untrackedCmd.Output(); err == nil && len(out) > 0 {
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			filePath := filepath.Join(repoDir, line)
			fi, err := os.Stat(filePath)
			if err != nil || fi.IsDir() || fi.Size() > 50*1024*1024 { // Skip large individual files
				continue
			}
			data, err := os.ReadFile(filePath)
			if err == nil {
				state.UntrackedFiles[line] = data
			}
		}
	}

	return state, nil
}

// ApplyUncommittedState applies captured uncommitted changes to targetDir.
// Recovered from jetski/git_utils/git_utils.ApplyUncommittedState.
func ApplyUncommittedState(ctx context.Context, targetDir string, state *UncommittedState) error {
	if state == nil || state.IsEmpty() {
		return nil
	}

	// 1. Apply staged changes
	if len(state.StagedDiff) > 0 {
		cmd := exec.CommandContext(ctx, "git", "apply", "--cached", "--binary", "--whitespace=nowarn")
		cmd.Dir = targetDir
		cmd.Stdin = bytes.NewReader(state.StagedDiff)
		_ = cmd.Run() // Best effort if clean apply fails
	}

	// 2. Apply unstaged changes
	if len(state.UnstagedDiff) > 0 {
		cmd := exec.CommandContext(ctx, "git", "apply", "--binary", "--whitespace=nowarn")
		cmd.Dir = targetDir
		cmd.Stdin = bytes.NewReader(state.UnstagedDiff)
		_ = cmd.Run()
	}

	// 3. Write untracked files
	for relPath, data := range state.UntrackedFiles {
		fullPath := filepath.Join(targetDir, relPath)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			continue
		}
		_ = os.WriteFile(fullPath, data, 0644)
	}

	return nil
}

// CloneNonGitWorkspace copies the parent workspace for agents outside version control,
// strictly bounded by 100,000 entries and 4 GiB total size (AGY 1.5.5).
func CloneNonGitWorkspace(srcDir, dstDir string) error {
	var count int64
	var totalBytes int64

	// First pass: validate bounds
	err := filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		count++
		if count > MaxNonGitWorkspaceEntries {
			return fmt.Errorf("workspace exceeds limit of %d entries", MaxNonGitWorkspaceEntries)
		}
		if !d.IsDir() {
			info, err := d.Info()
			if err == nil {
				totalBytes += info.Size()
				if totalBytes > MaxNonGitWorkspaceBytes {
					return fmt.Errorf("workspace exceeds limit of 4 GiB (%d bytes)", totalBytes)
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	// Second pass: copy files
	return filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil || rel == "." {
			return nil
		}
		target := filepath.Join(dstDir, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		return copyFile(path, target)
	})
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	s, err := os.Open(src)
	if err != nil {
		return err
	}
	defer s.Close()
	d, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer d.Close()
	_, err = io.Copy(d, s)
	return err
}

// PrepareIsolatedWorkspace sets up an isolated workspace for subagents or swarm workers:
// - If in a git repository: creates a git worktree and applies staged/unstaged/untracked changes.
// - If outside version control: clones the directory up to 100,000 entries and 4 GiB.
func PrepareIsolatedWorkspace(ctx context.Context, parentWorkspace, targetDir string) error {
	if strings.TrimSpace(parentWorkspace) == "" || strings.TrimSpace(targetDir) == "" {
		return nil
	}
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return err
	}

	if isGitRepo(parentWorkspace) {
		// Capture uncommitted state from parent workspace before creating worktree
		state, _ := CaptureUncommittedState(ctx, parentWorkspace)

		// Create git worktree
		cmd := exec.CommandContext(ctx, "git", "worktree", "add", "--detach", targetDir, "HEAD")
		cmd.Dir = parentWorkspace
		if err := cmd.Run(); err != nil {
			// Fallback: clone directory
			_ = CloneNonGitWorkspace(parentWorkspace, targetDir)
		} else if state != nil && !state.IsEmpty() {
			// Apply uncommitted changes (staged, unstaged, untracked)
			_ = ApplyUncommittedState(ctx, targetDir, state)
		}
		return nil
	}

	// Non-git workspace fallback
	return CloneNonGitWorkspace(parentWorkspace, targetDir)
}

// CleanupIsolatedWorkspace cleans up a created git worktree or cloned directory.
func CleanupIsolatedWorkspace(parentWorkspace, targetDir string) {
	if targetDir == "" {
		return
	}
	if isGitRepo(parentWorkspace) {
		cmd := exec.Command("git", "worktree", "remove", "--force", targetDir)
		cmd.Dir = parentWorkspace
		_ = cmd.Run()
	}
	_ = os.RemoveAll(targetDir)
}
