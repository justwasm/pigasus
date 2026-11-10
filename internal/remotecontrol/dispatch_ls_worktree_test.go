package remotecontrol

import (
	"archive/zip"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLSCreateDebugBundle(t *testing.T) {
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	d := pluginTestDispatcher(t)

	raw, err := json.Marshal(map[string]any{"extraFiles": []string{}})
	if err != nil {
		t.Fatal(err)
	}

	ans, rpcErr := d.routeMethod(context.Background(), "CreateDebugBundle", raw)
	if rpcErr != nil {
		t.Fatalf("CreateDebugBundle: %v", rpcErr)
	}
	m, ok := ans.(map[string]any)
	if !ok {
		t.Fatalf("not a map: %v", ans)
	}
	uri, ok := m["bundleUri"].(string)
	if !ok || !strings.HasPrefix(uri, "file://") {
		t.Fatalf("bundleUri = %q, want file:// prefix", uri)
	}

	p := parseURIPath(uri)
	zr, err := zip.OpenReader(p)
	if err != nil {
		t.Fatalf("failed to open created bundle zip: %v", err)
	}
	defer zr.Close()

	found := make(map[string]bool)
	for _, f := range zr.File {
		found[f.Name] = true
	}
	for _, required := range []string{"mcp_config.json", "conversations.txt", "voice_chats.txt"} {
		if !found[required] {
			t.Errorf("missing expected file in bundle zip: %s", required)
		}
	}
}

func TestLSWorktree_Lifecycle(t *testing.T) {
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	d := pluginTestDispatcher(t)

	// 1. Create a real git repo
	repoDir := t.TempDir()
	initCmd := exec.Command("git", "-C", repoDir, "init", "-b", "main")
	if out, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v (%s)", err, out)
	}
	commitCmd := exec.Command("git", "-C", repoDir, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "--allow-empty", "-m", "init")
	if out, err := commitCmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v (%s)", err, out)
	}

	// 2. Test CreateWorktree
	createReq, _ := json.Marshal(map[string]any{
		"repoUri":        lsNormalizedURI(repoDir),
		"worktreeName":   "feat-test",
		"baseBranchName": "main",
	})
	ans, rpcErr := d.routeMethod(context.Background(), "CreateWorktree", createReq)
	if rpcErr != nil {
		t.Fatalf("CreateWorktree: %v", rpcErr)
	}
	createAns := ans.(map[string]any)
	wtURI, _ := createAns["worktreeDirUri"].(string)
	if wtURI == "" || !strings.HasPrefix(wtURI, "file://") {
		t.Fatalf("worktreeDirUri = %q", wtURI)
	}
	if createAns["worktreeName"] != "feat-test" {
		t.Errorf("worktreeName = %v, want feat-test", createAns["worktreeName"])
	}

	wtPath := parseURIPath(wtURI)
	if _, err := os.Stat(wtPath); err != nil {
		t.Fatalf("worktree directory not on disk: %v", err)
	}

	// Write a file in worktree
	if err := os.WriteFile(filepath.Join(wtPath, "new.txt"), []byte("worktree file\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = exec.Command("git", "-C", wtPath, "add", "new.txt").Run()
	_ = exec.Command("git", "-C", wtPath, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "wt commit").Run()

	// 3. Test GetWorktreeDiff
	diffReq, _ := json.Marshal(map[string]any{
		"worktreeDirUri":     wtURI,
		"targetWorkspaceUri": lsNormalizedURI(repoDir),
	})
	diffAnsRaw, rpcErr := d.routeMethod(context.Background(), "GetWorktreeDiff", diffReq)
	if rpcErr != nil {
		t.Fatalf("GetWorktreeDiff: %v", rpcErr)
	}
	diffAns := diffAnsRaw.(map[string]any)
	if diffAns["diffPatch"] == "" {
		t.Errorf("expected non-empty diffPatch")
	}
	summary, _ := diffAns["summary"].(map[string]any)
	if summary["filesChanged"] != 1 {
		t.Errorf("filesChanged = %v, want 1", summary["filesChanged"])
	}

	// 4. Test CheckoutWorktree with deleteWorktreeAfterCheckout=true
	checkoutReq, _ := json.Marshal(map[string]any{
		"worktreeDirUri":              wtURI,
		"targetWorkspaceUri":          lsNormalizedURI(repoDir),
		"deleteWorktreeAfterCheckout": true,
	})
	coAnsRaw, rpcErr := d.routeMethod(context.Background(), "CheckoutWorktree", checkoutReq)
	if rpcErr != nil {
		t.Fatalf("CheckoutWorktree: %v", rpcErr)
	}
	coAns := coAnsRaw.(map[string]any)
	if coAns["success"] != true {
		t.Errorf("success = %v, want true", coAns["success"])
	}

	// Worktree directory should be deleted
	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Errorf("worktree dir still exists after deleteWorktreeAfterCheckout: %s", wtPath)
	}
}

func TestLSWorktree_ErrorsAndStubs(t *testing.T) {
	d := pluginTestDispatcher(t)

	// 1. CreateWorktree on non-git dir returns status 2 unknown error
	createReq, _ := json.Marshal(map[string]any{
		"repoUri": "file:///tmp/not-a-git-repo-at-all-xyz",
	})
	_, rpcErr := d.routeMethod(context.Background(), "CreateWorktree", createReq)
	if rpcErr == nil {
		t.Fatal("expected error on non-git repo")
	}
	if rpcErr.Status != 2 {
		t.Errorf("status = %d, want 2 (unknown)", rpcErr.Status)
	}
	if !strings.Contains(rpcErr.Data.(string), "not a git repository") {
		t.Errorf("unexpected error data: %v", rpcErr.Data)
	}

	// 2. DeleteWorktree with empty uri returns errorMessage (HTTP 200)
	delAnsRaw, rpcErr := d.routeMethod(context.Background(), "DeleteWorktree", []byte(`{}`))
	if rpcErr != nil {
		t.Fatalf("DeleteWorktree rpcErr: %v", rpcErr)
	}
	delAns := delAnsRaw.(map[string]any)
	if !strings.Contains(delAns["errorMessage"].(string), "failed to parse worktree dir uri") {
		t.Errorf("unexpected errorMessage: %v", delAns["errorMessage"])
	}

	// 3. CheckoutWorktree with empty uri returns errorMessage (HTTP 200)
	coAnsRaw, rpcErr := d.routeMethod(context.Background(), "CheckoutWorktree", []byte(`{}`))
	if rpcErr != nil {
		t.Fatalf("CheckoutWorktree rpcErr: %v", rpcErr)
	}
	coAns := coAnsRaw.(map[string]any)
	if !strings.Contains(coAns["errorMessage"].(string), "failed to parse worktree dir uri") {
		t.Errorf("unexpected errorMessage: %v", coAns["errorMessage"])
	}

	// 4. GetWorktreeDiff with empty uri returns status 3 invalid_argument error (HTTP 400)
	_, rpcErr = d.routeMethod(context.Background(), "GetWorktreeDiff", []byte(`{}`))
	if rpcErr == nil {
		t.Fatal("expected error on empty uri")
	}
	if rpcErr.Status != 3 {
		t.Errorf("status = %d, want 3 (invalid_argument)", rpcErr.Status)
	}

	// 5. GetBattleWorktreeDiff returns {}
	bAns, rpcErr := d.routeMethod(context.Background(), "GetBattleWorktreeDiff", []byte(`{}`))
	if rpcErr != nil {
		t.Fatalf("GetBattleWorktreeDiff: %v", rpcErr)
	}
	if len(bAns.(map[string]any)) != 0 {
		t.Errorf("expected empty map, got %v", bAns)
	}

	// 6. GetJJWorktrees returns status 12 unimplemented error (HTTP 501)
	_, rpcErr = d.routeMethod(context.Background(), "GetJJWorktrees", []byte(`{}`))
	if rpcErr == nil {
		t.Fatal("expected unimplemented error")
	}
	if rpcErr.Status != 12 {
		t.Errorf("status = %d, want 12 (unimplemented)", rpcErr.Status)
	}

	// 7. UpdatePRForWorktree returns status 2 unknown error
	_, rpcErr = d.routeMethod(context.Background(), "UpdatePRForWorktree", []byte(`{}`))
	if rpcErr == nil {
		t.Fatal("expected error")
	}
	if rpcErr.Status != 2 {
		t.Errorf("status = %d, want 2 (unknown)", rpcErr.Status)
	}
}
