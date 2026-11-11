package subagenttools

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestUncommittedStateEmpty(t *testing.T) {
	var s *UncommittedState
	if !s.IsEmpty() {
		t.Errorf("nil state should be empty")
	}
	s = &UncommittedState{}
	if !s.IsEmpty() {
		t.Errorf("zero state should be empty")
	}
}

func TestCloneNonGitWorkspace(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	// Create test files
	if err := os.WriteFile(filepath.Join(src, "file1.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(src, "subdir"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "subdir", "file2.txt"), []byte("world"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := CloneNonGitWorkspace(src, dst); err != nil {
		t.Fatalf("CloneNonGitWorkspace failed: %v", err)
	}

	f1, err := os.ReadFile(filepath.Join(dst, "file1.txt"))
	if err != nil || string(f1) != "hello" {
		t.Errorf("file1.txt not cloned properly: %v, %s", err, string(f1))
	}

	f2, err := os.ReadFile(filepath.Join(dst, "subdir", "file2.txt"))
	if err != nil || string(f2) != "world" {
		t.Errorf("subdir/file2.txt not cloned properly: %v, %s", err, string(f2))
	}
}

func TestCaptureAndApplyUncommittedStateGit(t *testing.T) {
	// Initialize a real git repo in temp dir
	repo := t.TempDir()
	runGit := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}

	runGit("init")
	runGit("config", "user.name", "Test")
	runGit("config", "user.email", "test@example.com")

	// Commit initial file
	if err := os.WriteFile(filepath.Join(repo, "committed.txt"), []byte("initial\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit("add", "committed.txt")
	runGit("commit", "-m", "initial commit")

	// Make staged change
	if err := os.WriteFile(filepath.Join(repo, "staged.txt"), []byte("staged content\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit("add", "staged.txt")

	// Make unstaged change
	if err := os.WriteFile(filepath.Join(repo, "committed.txt"), []byte("modified\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Make untracked file
	if err := os.WriteFile(filepath.Join(repo, "untracked.txt"), []byte("untracked content\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Capture uncommitted state
	ctx := context.Background()
	state, err := CaptureUncommittedState(ctx, repo)
	if err != nil {
		t.Fatalf("CaptureUncommittedState failed: %v", err)
	}
	if state.IsEmpty() {
		t.Fatalf("expected non-empty state")
	}
	if len(state.StagedDiff) == 0 {
		t.Errorf("expected staged diff")
	}
	if len(state.UnstagedDiff) == 0 {
		t.Errorf("expected unstaged diff")
	}
	if len(state.UntrackedFiles) == 0 || string(state.UntrackedFiles["untracked.txt"]) != "untracked content\n" {
		t.Errorf("expected untracked files with untracked.txt: %v", state.UntrackedFiles)
	}

	// Create a new worktree
	target := filepath.Join(t.TempDir(), "worktree-1")
	runGit("worktree", "add", "--detach", target, "HEAD")

	// Apply uncommitted state
	if err := ApplyUncommittedState(ctx, target, state); err != nil {
		t.Fatalf("ApplyUncommittedState failed: %v", err)
	}

	// Verify uncommitted work is present in worktree!
	untrackedData, err := os.ReadFile(filepath.Join(target, "untracked.txt"))
	if err != nil || string(untrackedData) != "untracked content\n" {
		t.Errorf("untracked.txt missing or wrong in worktree: %v, %s", err, string(untrackedData))
	}
	committedData, err := os.ReadFile(filepath.Join(target, "committed.txt"))
	if err != nil || !strings.Contains(string(committedData), "modified") {
		t.Errorf("committed.txt unstaged changes missing in worktree: %v, %s", err, string(committedData))
	}
}
