package subagenttools

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// subagentWorktreeDirName is the directory under <AgentDir> each
// per-subagent worktree lives in. AGY 1.2.10 changed this from
// "scattered wherever the parent happened to be CWD-ing" to a
// dedicated `worktrees/` directory so files written by the subagent
// are isolated from the user's workspace and the artifact tracker
// never has a reason to index them.
const subagentWorktreeDirName = "worktrees"

// allocateSubagentWorktreeDir creates (or reuses) the per-subagent
// directory `<AgentDir>/worktrees/<subID>/` and returns its absolute
// path. A successful return guarantees:
//
//   - the directory exists (os.MkdirAll with 0o755, parents
//     included);
//   - the path is a clean absolute path — Invoke passes it straight
//     to PiG's CWDOverride, which does not tolerate ".." segments.
//
// The directory is created with mode 0o755 so the subagent's session
// can write inside; the parent `<AgentDir>` is left alone (it is
// owned by pigrt.NewRuntime / the auth store and we have no business
// changing it). Cycle 229 will upgrade the body to a `git worktree
// add` invocation in the user's repository; the path layout does
// not change.
//
// An empty AgentDir is an error: subagents have no business running
// without a place to keep their files, and silently falling back to
// the parent's CWD would reintroduce the very bug the AGY 1.2.10
// changelog line describes.
func allocateSubagentWorktreeDir(agentDir, subID string) (string, error) {
	if strings.TrimSpace(agentDir) == "" {
		return "", errors.New("subagenttools: cannot allocate worktree: AgentDir is empty")
	}
	if strings.TrimSpace(subID) == "" {
		return "", errors.New("subagenttools: cannot allocate worktree: subID is empty")
	}
	worktreesRoot := filepath.Join(agentDir, subagentWorktreeDirName)
	target := filepath.Join(worktreesRoot, subID)
	if err := os.MkdirAll(target, 0o755); err != nil {
		return "", fmt.Errorf("subagenttools: mkdir worktree dir %s: %w", target, err)
	}
	// Resolve to a clean absolute path so the value can be fed
	// into PiG and round-tripped back through Result.WorktreePath
	// without surprises (a non-cleansed path with a ".." inside
	// could otherwise escape the worktrees/ tree on macOS where
	// filepath.Join does not error on "..").
	abs, err := filepath.Abs(target)
	if err != nil {
		return "", fmt.Errorf("subagenttools: abs worktree dir %s: %w", target, err)
	}
	return filepath.Clean(abs), nil
}

// purgeWorktreeArtifacts walks worktree (every file named
// `.metadata.json` under it) and removes it. The AGY 1.2.10 fix
// promises that subagent worktrees are free of `.metadata.json`
// files when the delegation returns, so callers (a future /agents
// panel, the artifacts tab, a conversation-replay UI) never see
// internal bookkeeping inside what is conceptually a scratch
// directory.
//
// Best-effort: a permission error on a single file does not abort
// the sweep or surface to the caller. The function is mounted as a
// defer in Invoke so the worktree is always cleaned even when the
// session fails or ctx is cancelled.
//
// subID is the stable subagent handle; today the function does not
// use it (the walk is bounded by `worktree`), but pinning it on the
// signature makes it cheap for cycle 229 to extend with a per-
// subagent index without another API break.
func purgeWorktreeArtifacts(worktree, subID string) {
	if strings.TrimSpace(worktree) == "" {
		return
	}
	_ = filepath.WalkDir(worktree, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		name := d.Name()
		if !strings.HasPrefix(name, ".") {
			return nil
		}
		base := strings.TrimPrefix(name, ".")
		if base == "metadata.json" {
			_ = os.Remove(path)
		}
		return nil
	})
}

// IsWorktreeArtifactName reports whether a leaf filename is one
// AGY's artifact subsystem writes into a tracked directory
// (currently `.metadata.json`). The helper exists so callers (and
// tests) can mirror purgeWorktreeArtifacts's selector without
// re-implementing the rules.
func IsWorktreeArtifactName(name string) bool {
	return filepath.Base(name) == ".metadata.json"
}
