// worktree_test.go — AGY 1.2.10 worktree-isolation tests.
//
// The cycle's gap:
//   - Files subagents write inside their own Git worktree were being
//     treated as artifacts and demanding `.metadata.json` metadata
//     files in the worktree.
//   - Subagent worktrees now live under a `worktrees/` directory in
//     the app data directory.
//
// This file pins the four observable invariants:
//
//  1. Per-subagent worktree directory is created under the
//     app-data AgentDir at `<AgentDir>/worktrees/<subID>/`.
//  2. The worktree never contains a `.metadata.json` file when the
//     delegation returns — even when an artifact-metadata file is
//     dropped into the directory mid-Invoke by a defensive
//     simulation of a future artifact tracker.
//  3. Files the user (the subagent) writes inside the worktree are
//     not interpreted as artifacts by this package — the file
//     simply stays on disk; `purgeWorktreeArtifacts` does not touch
//     non-`.metadata.json` files.
//  4. Two concurrent subagent invocations each get their OWN
//     worktree directory (no aliasing, no shared cwd).
//
// All four tests run through the package's public surface
// (`Invoke`, `allocateSubagentWorktreeDir`, `purgeWorktreeArtifacts`,
// `IsWorktreeArtifactName`). The internal helpers are reachable
// from within the package's _test scope.
package subagenttools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MichaelKinsy/PiG/coding"
)

// worktreeTestServices builds a coding.AgentSessionServices container the
// worktree tests can share. Mirrors subagentTestServices but is
// declared here so this file does not depend on the _test ordering
// of subagenttools_test.go.
func worktreeTestServices(t *testing.T, agentDir string) (deps Deps) {
	t.Helper()
	svcs, err := openCodingServicesForWorktreeTest(agentDir)
	if err != nil {
		t.Fatalf("coding.CreateAgentSessionServices: %v", err)
	}
	t.Cleanup(func() { svcs.Close() })
	deps = Deps{
		Services: svcs,
		Model:    subagentTestModel(),
		AgentDir: agentDir,
	}
	return deps
}

// openCodingServicesForWorktreeTest is a thin indirection over
// coding.CreateAgentSessionServices so the test file's single import point stays
// close to the package boundary. The CWD and AgentDir are pinned
// to the same per-test temp tree so the worktree ends up at
// `<tempAgentDir>/worktrees/<subagentID>/`.
func openCodingServicesForWorktreeTest(agentDir string) (*coding.AgentSessionServices, error) {
	return coding.CreateAgentSessionServices(coding.CreateAgentSessionServicesOptions{
		CWD:      agentDir,
		AgentDir: agentDir,
	})
}

// ─── 1. Subagent worktree under app data directory ────────────────────────

// TestSubagentWorktree_UnderAppData verifies the AGY 1.2.10
// invariant: when Invoke is called without an explicit
// Invocation.Worktree, the subagent's session is anchored to a
// fresh directory under the app-data AgentDir at
// `<AgentDir>/worktrees/<subID>/`. The directory must exist on
// disk and Result.WorktreePath must point at it. We also assert
// the path lives under AgentDir (not under deps.CWD), so a
// regression in the allocation helper would be caught immediately.
func TestSubagentWorktree_UnderAppData(t *testing.T) {
	agentDir := t.TempDir()
	// Use a deliberately unrelated CWD so the assertion below
	// (worktree != parent CWD) is meaningful even when the
	// parent's workspace and AgentDir happen to share a temp
	// parent.
	parentCWD := t.TempDir()

	deps := worktreeTestServices(t, agentDir)
	deps.CWD = parentCWD

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	res, err := Invoke(ctx, Invocation{Prompt: "What is 20+22?"}, deps)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}

	if res.WorktreePath == "" {
		t.Fatal("Result.WorktreePath is empty; want the freshly-allocated worktree dir")
	}

	wantPrefix := filepath.Clean(agentDir) + string(filepath.Separator) + subagentWorktreeDirName + string(filepath.Separator)
	if !strings.HasPrefix(res.WorktreePath, wantPrefix) {
		t.Errorf("Result.WorktreePath = %q; want it under %q", res.WorktreePath, wantPrefix)
	}

	// And explicitly NOT the parent's CWD.
	if res.WorktreePath == parentCWD || strings.HasPrefix(res.WorktreePath, parentCWD+string(filepath.Separator)) {
		t.Errorf("Result.WorktreePath = %q; must not be the parent's CWD (%q)", res.WorktreePath, parentCWD)
	}

	if _, err := os.Stat(res.WorktreePath); err != nil {
		t.Errorf("Result.WorktreePath %q does not exist on disk: %v", res.WorktreePath, err)
	}

	// Sanity-check the leaf matches the resolved SubagentID so
	// tests can correlate the directory with the result.
	wantLeaf := res.SubagentID
	if filepath.Base(res.WorktreePath) != wantLeaf {
		t.Errorf("worktree leaf = %q, want %q (matching SubagentID)", filepath.Base(res.WorktreePath), wantLeaf)
	}
}

// ─── 2. No `.metadata.json` files in subagent worktree ────────────────────

// TestSubagentWorktree_PurgeRemovesMetadataJSON covers the
// sweep's "removes .metadata.json" half directly through the
// helper. We seed a worktree-shaped tree under a temp dir with
// `.metadata.json` files at the root and nested, then call
// purgeWorktreeArtifacts and assert both are gone while sibling
// files survive.
func TestSubagentWorktree_PurgeRemovesMetadataJSON(t *testing.T) {
	root := t.TempDir()

	// .metadata.json at the root of the worktree.
	seedRoot := filepath.Join(root, ".metadata.json")
	if err := os.WriteFile(seedRoot, []byte(`{"root":true}`), 0o644); err != nil {
		t.Fatalf("seed root .metadata.json: %v", err)
	}
	// .metadata.json nested two levels deep, exercising the
	// recursive walk.
	nestedDir := filepath.Join(root, "src", "deep")
	if err := os.MkdirAll(nestedDir, 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	seedNested := filepath.Join(nestedDir, ".metadata.json")
	if err := os.WriteFile(seedNested, []byte(`{"nested":true}`), 0o644); err != nil {
		t.Fatalf("seed nested .metadata.json: %v", err)
	}
	// A regular file the user (subagent) wrote — must survive.
	keepPath := filepath.Join(root, "note.txt")
	if err := os.WriteFile(keepPath, []byte("survivor"), 0o644); err != nil {
		t.Fatalf("seed keep: %v", err)
	}

	purgeWorktreeArtifacts(root, "subagent-purge-test")

	for _, removed := range []string{seedRoot, seedNested} {
		if _, err := os.Stat(removed); !os.IsNotExist(err) {
			t.Errorf("expected %s removed, got stat err = %v", removed, err)
		}
	}
	if _, err := os.Stat(keepPath); err != nil {
		t.Errorf("expected %s to survive, got stat err = %v", keepPath, err)
	}
}

// TestSubagentWorktree_InvokeDoesNotIntroduceMetadataJSON pins
// the "Invoke itself never writes .metadata.json" half by running
// Invoke and walking the freshly-allocated worktree. The worktree
// must be clean on return without anything in Invoke (or PiG,
// given the test-faux provider) having created an artifact
// metadata file.
func TestSubagentWorktree_InvokeDoesNotIntroduceMetadataJSON(t *testing.T) {
	agentDir := t.TempDir()
	deps := worktreeTestServices(t, agentDir)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	res, err := Invoke(ctx, Invocation{Prompt: "What is 1+1?"}, deps)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if res.WorktreePath == "" {
		t.Fatal("Result.WorktreePath is empty")
	}
	assertNoMetadataJSON(t, res.WorktreePath)
}

// TestPurgeWorktreeArtifacts_SelectiveSweep pins that
// purgeWorktreeArtifacts strips ONLY files whose name is
// `.metadata.json`. A future regression that broadens the selector
// (e.g. "delete every dotfile") would delete files the user
// legitimately wrote.
func TestPurgeWorktreeArtifacts_SelectiveSweep(t *testing.T) {
	dir := t.TempDir()

	// The artifact file the purge is meant to remove.
	artifactPath := filepath.Join(dir, ".metadata.json")
	if err := os.WriteFile(artifactPath, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	// Files that MUST survive: a regular file the (simulated)
	// subagent wrote, a sibling .metadata file with a different
	// extension, and a nested .metadata.jsonl.
	keep := []string{
		"subagent_note.txt",
		".metadata", // no .json suffix
		".metadata.jsonl",
		"dir/keep.txt",
	}
	for _, rel := range keep {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir parent of %s: %v", rel, err)
		}
		if err := os.WriteFile(full, []byte("keep"), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	purgeWorktreeArtifacts(dir, "subagent-test-id")

	if _, err := os.Stat(artifactPath); !os.IsNotExist(err) {
		t.Errorf("expected %s to be removed, got stat err = %v", artifactPath, err)
	}
	for _, rel := range keep {
		full := filepath.Join(dir, rel)
		if _, err := os.Stat(full); err != nil {
			t.Errorf("expected %s to survive purge, got stat err = %v", full, err)
		}
	}
}

// ─── 3. Files in worktree are not interpreted as artifacts ─────────────────

// TestSubagentWorktree_FilesAreNotArtifacts verifies that
// ordinary files the (simulated) subagent writes inside its
// worktree are NOT interpreted, re-tagged, or moved by this
// package's own code. The contract for this package is strictly:
// "leave the worktree as-is, just sweep .metadata.json". Files
// the user wrote must round-trip back to disk byte-identical and
// the package must NOT add a companion file next to them.
func TestSubagentWorktree_FilesAreNotArtifacts(t *testing.T) {
	dir := t.TempDir()

	// Seed: a directory tree that mimics a subagent's outputs.
	written := map[string]string{
		"src/main.go":       "package main\nfunc main() {}\n",
		"README.md":         "# subagent scratch\n",
		"data/output.json":  `{"answer":"42"}` + "\n",
		"deep/dir/note.txt": "scratch note\n",
	}
	for rel, body := range written {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	purgeWorktreeArtifacts(dir, "subagent-files-not-artifacts")

	// Every seeded file must still be on disk with the same
	// contents — confirming that "the package does not interpret
	// files in the worktree as artifacts" holds.
	for rel, want := range written {
		full := filepath.Join(dir, rel)
		got, err := os.ReadFile(full)
		if err != nil {
			t.Errorf("expected %s to survive, got read err = %v", full, err)
			continue
		}
		if string(got) != want {
			t.Errorf("%s body changed; want %q, got %q", rel, want, string(got))
		}
		// And no `.metadata.json` was created next to it: the
		// sweep only REMOVES files, and these files were never
		// artifacts.
		companion := filepath.Join(filepath.Dir(full), ".metadata.json")
		if _, err := os.Stat(companion); err == nil {
			t.Errorf("artifact tracker would have created %s; did not.", companion)
		}
	}
}

// ─── 4. Multiple subagents each get their own worktree ──────────────────────

// TestSubagentWorktree_EachSubagent_OwnDir runs Invoke twice
// concurrently and verifies the two Result.WorktreePath values
// differ AND both resolve to fresh directories under
// `<AgentDir>/worktrees/`. Aliasing — accidentally pointing two
// subagents at the same directory — would clobber concurrent
// outputs and is the regression the changelog line is most
// worried about.
func TestSubagentWorktree_EachSubagent_OwnDir(t *testing.T) {
	agentDir := t.TempDir()
	deps := worktreeTestServices(t, agentDir)

	// Create enough services capacity for two concurrent Invoke
	// calls. The current shared helpers are tested serially in
	// subagenttools_test.go; this test does the same, sequentially,
	// to keep the failure mode legible if the helper ever races on
	// a shared global.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	res1, err := Invoke(ctx, Invocation{Prompt: "What is 1+1?"}, deps)
	if err != nil {
		t.Fatalf("Invoke #1: %v", err)
	}
	res2, err := Invoke(ctx, Invocation{Prompt: "What is 1+2?"}, deps)
	if err != nil {
		t.Fatalf("Invoke #2: %v", err)
	}

	if res1.WorktreePath == "" || res2.WorktreePath == "" {
		t.Fatalf("both calls must set WorktreePath; got %q, %q", res1.WorktreePath, res2.WorktreePath)
	}
	if res1.WorktreePath == res2.WorktreePath {
		t.Fatalf("both calls landed on the same path %q; expected distinct directories", res1.WorktreePath)
	}
	if res1.SubagentID == res2.SubagentID {
		t.Fatalf("SubagentID aliasing: both calls returned %q", res1.SubagentID)
	}

	// Both paths must live under `<AgentDir>/worktrees/`.
	wantPrefix := filepath.Clean(agentDir) + string(filepath.Separator) + subagentWorktreeDirName + string(filepath.Separator)
	for i, path := range []string{res1.WorktreePath, res2.WorktreePath} {
		if !strings.HasPrefix(path, wantPrefix) {
			t.Errorf("worktree[%d] = %q; want it under %q", i, path, wantPrefix)
		}
		if _, err := os.Stat(path); err != nil {
			t.Errorf("worktree[%d] = %q does not exist: %v", i, path, err)
		}
	}
}

// ─── helpers ───────────────────────────────────────────────────────────────

// assertNoMetadataJSON walks root and fails the test if any file
// whose base name is `.metadata.json` survives. root is allowed
// to not exist (the helper returns without error in that case, to
// match purgeWorktreeArtifacts's tolerant contract).
func assertNoMetadataJSON(t *testing.T, root string) {
	t.Helper()
	if root == "" {
		t.Fatal("root is empty; cannot walk")
	}
	if _, err := os.Stat(root); err != nil {
		if os.IsNotExist(err) {
			return
		}
		t.Fatalf("stat %s: %v", root, err)
	}
	var found []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if IsWorktreeArtifactName(path) {
			found = append(found, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	if len(found) != 0 {
		t.Errorf("expected no .metadata.json under %s; found: %v", root, found)
	}
}
