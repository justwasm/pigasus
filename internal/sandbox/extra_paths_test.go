package sandbox

import (
	"path/filepath"
	"testing"
)

// TestExtraPathsAreAllowed pins the fourth kind of allowed root.
//
// The reference's own sandbox has three roots — the workspace, the agent's
// artifact directory and the per-conversation scratch directory. A
// subagent adds a fourth: it runs in its own worktree, and the gate that
// guards its commands has to allow that directory.
func TestExtraPathsAreAllowed(t *testing.T) {
	// The clone's per-subagent worktree lives under the agent directory
	// (internal/subagenttools: <AgentDir>/worktrees/<id>), so it is
	// outside the workspace the parent's own commands are confined to.
	worktree := filepath.Join("/home", "user", ".gemini", "antigravity-cli", "worktrees", "child-1")
	s := Sandbox{
		Enabled:    true,
		Cwd:        filepath.Join("/home", "user", "workspace"),
		ExtraPaths: []string{worktree},
	}

	for _, p := range []string{worktree, worktree + "/", worktree + "/notes.md", worktree + "/deep/dir/out.bin"} {
		if !s.Allowed(p) {
			t.Errorf("Allowed(%q) = false, want true (the subagent's worktree must be in the allow-list)", p)
		}
	}
	// A sibling of the worktree is still outside.
	sibling := filepath.Join("/home", "user", ".gemini", "antigravity-cli", "worktrees", "child-10", "x")
	if s.Allowed(sibling) {
		t.Errorf("Allowed(%q) = true, want false (a sibling worktree is not in this sandbox)", sibling)
	}
}

// TestAllowedPathsCarriesTheExtraRoots pins that the extra roots are part
// of the list the denial message names.
func TestAllowedPathsCarriesTheExtraRoots(t *testing.T) {
	worktree := filepath.Join("/home", "user", "workspace", ".worktrees", "child-1")
	s := Sandbox{
		Enabled:    true,
		Cwd:        "/home/user/workspace",
		ExtraPaths: []string{worktree, ""},
	}
	paths := s.AllowedPaths()
	if len(paths) != 2 {
		t.Fatalf("AllowedPaths() = %v, want the cwd and the worktree", paths)
	}
	if paths[1] != worktree {
		t.Fatalf("AllowedPaths() = %v, want the worktree last", paths)
	}
}

// TestDisabledSandboxIgnoresTheExtraRoots pins that a disabled sandbox
// allows everything, extra roots or not.
func TestDisabledSandboxIgnoresTheExtraRoots(t *testing.T) {
	s := Sandbox{Enabled: false, Cwd: "/home/user/workspace"}
	if !s.Allowed("/etc/passwd") {
		t.Fatal("Allowed(/etc/passwd) = false, want true for a disabled sandbox")
	}
	if got := s.AllowedPaths(); got != nil {
		t.Fatalf("AllowedPaths() = %v, want nil for a disabled sandbox", got)
	}
}
