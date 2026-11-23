package battlemode

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func initTestGitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
		}
	}
	run("init")
	run("config", "user.email", "battle@example.com")
	run("config", "user.name", "Battle Test")

	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc greet() string { return \"hello\" }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "initial commit")
	return dir
}

func TestSession_GitWorktreeBattleLifecycle(t *testing.T) {
	repoDir := initTestGitRepo(t)
	ctx := context.Background()

	mgr := NewManager()
	var updates atomic.Int32
	mgr.OnUpdate(func(snap SessionSnapshot) {
		updates.Add(1)
	})

	arms := []ArmSpec{
		{Model: "modelA", Label: "Model A"},
		{Model: "modelB", Label: "Model B"},
		{Model: "modelC", Label: "Model C"},
	}
	if err := mgr.PrepareWorkspaces(ctx, repoDir, arms); err != nil {
		t.Fatalf("PrepareWorkspaces failed: %v", err)
	}
	defer func() { _ = mgr.Cleanup() }()

	snap := mgr.Snapshot()
	if !snap.IsGit {
		t.Fatal("expected IsGit=true")
	}
	if snap.BaseCommit == "" {
		t.Fatal("expected non-empty BaseCommit")
	}
	if len(snap.Arms) != 3 {
		t.Fatalf("len(Arms) = %d, want 3", len(snap.Arms))
	}
	seenWS := map[string]bool{}
	for i, arm := range snap.Arms {
		if arm.Status != StatusPending {
			t.Errorf("arm %d status = %q, want pending", i, arm.Status)
		}
		if arm.Workspace == "" || arm.Workspace == repoDir {
			t.Errorf("arm %d workspace = %q, want isolated worktree", i, arm.Workspace)
		}
		if seenWS[arm.Workspace] {
			t.Errorf("duplicate workspace %q", arm.Workspace)
		}
		seenWS[arm.Workspace] = true
	}

	// Verify concurrent execution using a barrier across the 3 arms.
	var active atomic.Int32
	var maxConcurrent atomic.Int32
	allStarted := make(chan struct{})

	runner := func(ctx context.Context, arm ArmSpec, workspace string, task string) (ArmOutput, error) {
		cur := active.Add(1)
		for {
			prev := maxConcurrent.Load()
			if cur <= prev || maxConcurrent.CompareAndSwap(prev, cur) {
				break
			}
		}
		if cur == 3 {
			close(allStarted)
		}
		select {
		case <-allStarted:
		case <-time.After(2 * time.Second):
			active.Add(-1)
			return ArmOutput{}, errors.New("timed out waiting for concurrent arms")
		}
		defer active.Add(-1)

		switch arm.Model {
		case "modelA":
			if err := os.WriteFile(filepath.Join(workspace, "main.go"), []byte("package main\n\nfunc greet() string { return \"hello battle\" }\n"), 0o644); err != nil {
				return ArmOutput{}, err
			}
			if err := os.WriteFile(filepath.Join(workspace, "helper.go"), []byte("package main\n\nfunc helper() int { return 42 }\n"), 0o644); err != nil {
				return ArmOutput{}, err
			}
			return ArmOutput{
				Response: "Updated greet() and added helper.go",
				Usage:    TokenUsage{InputTokens: 100, OutputTokens: 50, TotalTokens: 150},
			}, nil
		case "modelB":
			if err := os.WriteFile(filepath.Join(workspace, "README.md"), []byte("# Demo\n\nUpdated by modelB.\n"), 0o644); err != nil {
				return ArmOutput{}, err
			}
			return ArmOutput{
				Response: "Updated README.md",
				Usage:    TokenUsage{InputTokens: 80, OutputTokens: 30, TotalTokens: 110},
			}, nil
		case "modelC":
			return ArmOutput{Response: "partial"}, errors.New("modelC rate limited")
		default:
			return ArmOutput{}, nil
		}
	}

	if err := mgr.Run(ctx, "Improve greeting and add helper", runner); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if maxConcurrent.Load() < 3 {
		t.Errorf("maxConcurrent = %d, want 3 (arms did not run concurrently)", maxConcurrent.Load())
	}
	if updates.Load() == 0 {
		t.Error("expected OnUpdate callback to be invoked")
	}

	snap = mgr.Snapshot()
	if snap.Running {
		t.Error("expected Running=false after Run completes")
	}
	if snap.Task != "Improve greeting and add helper" {
		t.Errorf("Task = %q", snap.Task)
	}

	// Arm 0 checks
	arm0 := snap.Arms[0]
	if arm0.Status != StatusDone {
		t.Errorf("arm0 status = %q, want done (err=%q)", arm0.Status, arm0.Error)
	}
	if arm0.Usage.TotalTokens != 150 {
		t.Errorf("arm0 TotalTokens = %d, want 150", arm0.Usage.TotalTokens)
	}
	if !strings.Contains(arm0.Diff, "+func greet() string { return \"hello battle\" }") {
		t.Errorf("arm0 diff missing main.go change:\n%s", arm0.Diff)
	}
	if !strings.Contains(arm0.Diff, "helper.go") || !strings.Contains(arm0.Diff, "+func helper() int { return 42 }") {
		t.Errorf("arm0 diff missing new file helper.go:\n%s", arm0.Diff)
	}
	if len(arm0.ChangedFiles) != 2 {
		t.Errorf("arm0 ChangedFiles = %v, want 2 files", arm0.ChangedFiles)
	}

	// Arm 1 checks
	arm1 := snap.Arms[1]
	if arm1.Status != StatusDone {
		t.Errorf("arm1 status = %q, want done", arm1.Status)
	}
	if !strings.Contains(arm1.Diff, "+Updated by modelB.") {
		t.Errorf("arm1 diff missing README change:\n%s", arm1.Diff)
	}

	// Arm 2 checks
	arm2 := snap.Arms[2]
	if arm2.Status != StatusError {
		t.Errorf("arm2 status = %q, want error", arm2.Status)
	}
	if !strings.Contains(arm2.Error, "rate limited") {
		t.Errorf("arm2 error = %q", arm2.Error)
	}

	// Base repo must still be untouched before PickWinner.
	baseMain, err := os.ReadFile(filepath.Join(repoDir, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(baseMain), "hello battle") {
		t.Fatal("base repo was modified before PickWinner")
	}

	// Eliminate arm 1 and verify it cannot be picked as winner.
	if err := mgr.EliminateArm(1); err != nil {
		t.Fatalf("EliminateArm(1) failed: %v", err)
	}
	if got := mgr.Snapshot().Arms[1].Status; got != StatusEliminated {
		t.Errorf("arm1 status = %q, want eliminated", got)
	}
	if _, err := mgr.PickWinner(ctx, 1); err == nil {
		t.Fatal("expected PickWinner on eliminated arm to fail")
	}

	// Pick arm 0 as winner and verify changes are applied to base repo.
	res, err := mgr.PickWinner(ctx, 0)
	if err != nil {
		t.Fatalf("PickWinner(0) failed: %v", err)
	}
	if res.Method != "git-apply" {
		t.Errorf("ApplyResult.Method = %q, want git-apply", res.Method)
	}
	if res.WinnerIndex != 0 || res.WinnerModel != "modelA" {
		t.Errorf("unexpected winner result: %+v", res)
	}

	snap = mgr.Snapshot()
	if snap.WinnerIndex != 0 || !snap.Arms[0].Winner {
		t.Errorf("expected arm 0 marked as winner, got WinnerIndex=%d", snap.WinnerIndex)
	}

	updatedMain, err := os.ReadFile(filepath.Join(repoDir, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(updatedMain), "hello battle") {
		t.Errorf("base main.go not updated: %s", string(updatedMain))
	}
	helperBytes, err := os.ReadFile(filepath.Join(repoDir, "helper.go"))
	if err != nil {
		t.Fatalf("base helper.go not created: %v", err)
	}
	if !strings.Contains(string(helperBytes), "return 42") {
		t.Errorf("base helper.go unexpected content: %s", string(helperBytes))
	}

	// Cleanup removes worktrees and temp dirs.
	wtPaths := []string{snap.Arms[0].WorktreeRoot, snap.Arms[1].WorktreeRoot, snap.Arms[2].WorktreeRoot}
	if err := mgr.Cleanup(); err != nil {
		t.Fatalf("Cleanup failed: %v", err)
	}
	for _, wt := range wtPaths {
		if _, err := os.Stat(wt); !os.IsNotExist(err) {
			t.Errorf("worktree dir %q still exists after Cleanup", wt)
		}
	}
	wtList, err := runGit(ctx, repoDir, "worktree", "list", "--porcelain")
	if err != nil {
		t.Fatalf("git worktree list failed: %v", err)
	}
	if strings.Contains(wtList, "arm-0") || strings.Contains(wtList, "arm-1") {
		t.Errorf("git worktree list still contains battle worktrees:\n%s", wtList)
	}
}

func TestSession_NonGitFallbackAndFileSync(t *testing.T) {
	baseDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(baseDir, "app.txt"), []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(baseDir, "obsolete.txt"), []byte("remove me\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	sess := NewSession()
	arms := []ArmSpec{
		{Model: "alpha"},
		{Model: "beta"},
	}
	if err := sess.PrepareWorkspaces(ctx, baseDir, arms); err != nil {
		t.Fatalf("PrepareWorkspaces failed: %v", err)
	}
	defer func() { _ = sess.Cleanup() }()

	snap := sess.Snapshot()
	if snap.IsGit {
		t.Fatal("expected IsGit=false for non-git directory")
	}

	runner := func(ctx context.Context, arm ArmSpec, workspace string, task string) (ArmOutput, error) {
		if arm.Model == "alpha" {
			if err := os.WriteFile(filepath.Join(workspace, "app.txt"), []byte("v2-alpha\n"), 0o644); err != nil {
				return ArmOutput{}, err
			}
			if err := os.WriteFile(filepath.Join(workspace, "new.txt"), []byte("created\n"), 0o644); err != nil {
				return ArmOutput{}, err
			}
			_ = os.Remove(filepath.Join(workspace, "obsolete.txt"))
			return ArmOutput{Response: "alpha done"}, nil
		}
		return ArmOutput{Response: "beta noop"}, nil
	}

	if err := sess.Run(ctx, "update app", runner); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	snap = sess.Snapshot()
	if !strings.Contains(snap.Arms[0].Diff, "v2-alpha") {
		t.Errorf("expected diff to contain v2-alpha, got:\n%s", snap.Arms[0].Diff)
	}
	if len(snap.Arms[0].ChangedFiles) != 3 {
		t.Errorf("ChangedFiles = %v, want 3 files", snap.Arms[0].ChangedFiles)
	}

	res, err := sess.PickWinner(ctx, 0)
	if err != nil {
		t.Fatalf("PickWinner failed: %v", err)
	}
	if res.Method != "file-sync" {
		t.Errorf("Method = %q, want file-sync", res.Method)
	}

	gotApp, err := os.ReadFile(filepath.Join(baseDir, "app.txt"))
	if err != nil || string(gotApp) != "v2-alpha\n" {
		t.Errorf("app.txt = %q, err=%v", string(gotApp), err)
	}
	gotNew, err := os.ReadFile(filepath.Join(baseDir, "new.txt"))
	if err != nil || string(gotNew) != "created\n" {
		t.Errorf("new.txt = %q, err=%v", string(gotNew), err)
	}
	if _, err := os.Stat(filepath.Join(baseDir, "obsolete.txt")); !os.IsNotExist(err) {
		t.Errorf("expected obsolete.txt to be deleted in baseDir")
	}

	ws0 := snap.Arms[0].Workspace
	if err := sess.Cleanup(); err != nil {
		t.Fatalf("Cleanup failed: %v", err)
	}
	if _, err := os.Stat(ws0); !os.IsNotExist(err) {
		t.Errorf("expected workspace %q to be removed", ws0)
	}
}

func TestSession_EliminateWhileRunningCancelsArm(t *testing.T) {
	repoDir := initTestGitRepo(t)
	ctx := context.Background()
	sess := NewSession()
	if err := sess.PrepareWorkspaces(ctx, repoDir, []ArmSpec{{Model: "fast"}, {Model: "slow"}}); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sess.Cleanup() }()

	slowStarted := make(chan struct{})
	slowCancelled := make(chan struct{})

	runner := func(ctx context.Context, arm ArmSpec, workspace string, task string) (ArmOutput, error) {
		if arm.Model == "fast" {
			return ArmOutput{Response: "fast done"}, nil
		}
		close(slowStarted)
		<-ctx.Done()
		close(slowCancelled)
		return ArmOutput{}, ctx.Err()
	}

	runDone := make(chan error, 1)
	go func() {
		runDone <- sess.Run(ctx, "race", runner)
	}()

	<-slowStarted
	if err := sess.EliminateArm(1); err != nil {
		t.Fatalf("EliminateArm(1) failed: %v", err)
	}
	<-slowCancelled

	if err := <-runDone; err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	snap := sess.Snapshot()
	if snap.Arms[0].Status != StatusDone {
		t.Errorf("arm0 status = %q, want done", snap.Arms[0].Status)
	}
	if snap.Arms[1].Status != StatusEliminated {
		t.Errorf("arm1 status = %q, want eliminated", snap.Arms[1].Status)
	}
}
