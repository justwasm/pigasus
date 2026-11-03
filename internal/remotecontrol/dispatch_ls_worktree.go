package remotecontrol

import (
	"archive/zip"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/justwasm/pigasus/internal/app/session/convstore"
	"github.com/justwasm/pigasus/internal/remotecontrol/rpc"
)

func newErrorID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func isGitRepo(dir string) bool {
	if dir == "" {
		return false
	}
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--git-dir")
	return cmd.Run() == nil
}

// handleLSCreateDebugBundle creates a debug bundle zip archive.
func (d *Dispatcher) handleLSCreateDebugBundle(params json.RawMessage) (any, *rpc.ErrorBody, bool) {
	var req struct {
		ExtraFiles []string `json:"extraFiles"`
	}
	if len(params) > 0 {
		_ = json.Unmarshal(params, &req)
	}

	debugDir := filepath.Join(convstore.StoreDir(), "brain", "_debug_bundles")
	if err := os.MkdirAll(debugDir, 0o755); err != nil {
		debugDir = filepath.Join(os.TempDir(), "brain", "_debug_bundles")
		_ = os.MkdirAll(debugDir, 0o755)
	}

	now := time.Now()
	filename := fmt.Sprintf("jetski-debug-bundle-%s.zip", now.Format("20060102-150405"))
	zipPath := filepath.Join(debugDir, filename)

	zf, err := os.Create(zipPath)
	if err != nil {
		return nil, &rpc.ErrorBody{Code: -32603, Status: 13, Message: "internal", Data: err.Error()}, true
	}
	defer zf.Close()

	zw := zip.NewWriter(zf)
	defer zw.Close()

	// 1. mcp_config.json
	if w, err := zw.Create("mcp_config.json"); err == nil {
		mcpPath := filepath.Join(convstore.StoreDir(), "mcp_config.json")
		if data, err := os.ReadFile(mcpPath); err == nil {
			_, _ = w.Write(data)
		}
	}

	// 2. conversations.txt
	if w, err := zw.Create("conversations.txt"); err == nil {
		metas, _ := convstore.ListConversations()
		for _, m := range metas {
			_, _ = fmt.Fprintf(w, "%s\t%s\n", m.ID, m.Project)
		}
	}

	// 3. voice_chats.txt
	if w, err := zw.Create("voice_chats.txt"); err == nil {
		_, _ = fmt.Fprintf(w, "# jetski voice sessions\n")
	}

	// 4. extra_files
	for _, ef := range req.ExtraFiles {
		p := parseURIPath(ef)
		if data, err := os.ReadFile(p); err == nil {
			if w, err := zw.Create(filepath.Base(p)); err == nil {
				_, _ = w.Write(data)
			}
		}
	}

	return map[string]any{
		"bundleUri": lsNormalizedURI(zipPath),
	}, nil, true
}

// handleLSCreateWorktree adds a new git worktree.
func (d *Dispatcher) handleLSCreateWorktree(params json.RawMessage) (any, *rpc.ErrorBody, bool) {
	var req struct {
		RepoURI        string `json:"repoUri"`
		RepoPath       string `json:"repoPath"`
		WorktreeName   string `json:"worktreeName"`
		BaseBranchName string `json:"baseBranchName"`
	}
	if len(params) > 0 {
		_ = json.Unmarshal(params, &req)
	}

	repoPath := parseURIPath(req.RepoURI)
	if repoPath == "" {
		repoPath = parseURIPath(req.RepoPath)
	}
	if repoPath == "" {
		repoPath = d.lsWorkspace()
	}

	if repoPath == "" || !isGitRepo(repoPath) {
		errID := newErrorID()
		msg := fmt.Sprintf("fatal: not a git repository (or any of the parent directories): .git: exit status 128 (error ID: %s)", errID)
		return nil, &rpc.ErrorBody{
			Code:    -32603,
			Status:  2, // grpcStatusUnknown -> HTTP 500
			Message: "unknown",
			Data:    msg,
		}, true
	}

	wtName := strings.TrimSpace(req.WorktreeName)
	if wtName == "" {
		wtName = "worktree-" + newErrorID()[:8]
	}

	repoName := filepath.Base(repoPath)
	wtDir := filepath.Join(convstore.StoreDir(), "worktrees", repoName, wtName)
	_ = os.MkdirAll(filepath.Dir(wtDir), 0o755)

	baseBranch := strings.TrimSpace(req.BaseBranchName)
	args := []string{"-C", repoPath, "worktree", "add"}
	if baseBranch != "" {
		args = append(args, "-b", wtName, wtDir, baseBranch)
	} else {
		args = append(args, "-b", wtName, wtDir)
	}

	cmd := exec.Command("git", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		errID := newErrorID()
		msg := fmt.Sprintf("%s: %v (error ID: %s)", strings.TrimSpace(string(out)), err, errID)
		return nil, &rpc.ErrorBody{
			Code:    -32603,
			Status:  2,
			Message: "unknown",
			Data:    msg,
		}, true
	}

	return map[string]any{
		"worktreeDirUri": lsNormalizedURI(wtDir),
		"worktreeName":   wtName,
	}, nil, true
}

// handleLSDeleteWorktree deletes a git worktree.
func (d *Dispatcher) handleLSDeleteWorktree(params json.RawMessage) (any, *rpc.ErrorBody, bool) {
	var req struct {
		RepoURI        string `json:"repoUri"`
		WorktreeDirURI string `json:"worktreeDirUri"`
	}
	if len(params) > 0 {
		_ = json.Unmarshal(params, &req)
	}

	rawURI := strings.TrimSpace(req.WorktreeDirURI)
	if rawURI == "" || !strings.HasPrefix(rawURI, "file://") {
		return map[string]any{
			"errorMessage": "failed to parse worktree dir uri: uri: \"\": unknown uri scheme",
		}, nil, true
	}

	wtPath := parseURIPath(rawURI)
	repoPath := parseURIPath(req.RepoURI)
	if repoPath == "" {
		repoPath = d.lsWorkspace()
	}

	if repoPath != "" && isGitRepo(repoPath) {
		cmd := exec.Command("git", "-C", repoPath, "worktree", "remove", "--force", wtPath)
		_ = cmd.Run()
	}
	_ = os.RemoveAll(wtPath)

	return map[string]any{
		"success": true,
	}, nil, true
}

// handleLSCheckoutWorktree merges/applies worktree changes back to the workspace.
func (d *Dispatcher) handleLSCheckoutWorktree(params json.RawMessage) (any, *rpc.ErrorBody, bool) {
	var req struct {
		WorktreeDirURI              string `json:"worktreeDirUri"`
		TargetWorkspaceURI          string `json:"targetWorkspaceUri"`
		DeleteWorktreeAfterCheckout bool   `json:"deleteWorktreeAfterCheckout"`
		MergeStrategy               int    `json:"mergeStrategy"`
	}
	if len(params) > 0 {
		_ = json.Unmarshal(params, &req)
	}

	rawURI := strings.TrimSpace(req.WorktreeDirURI)
	if rawURI == "" || !strings.HasPrefix(rawURI, "file://") {
		return map[string]any{
			"errorMessage": "failed to parse worktree dir uri: uri: \"\": unknown uri scheme",
		}, nil, true
	}

	wtPath := parseURIPath(rawURI)
	targetPath := parseURIPath(req.TargetWorkspaceURI)
	if targetPath == "" {
		targetPath = d.lsWorkspace()
	}

	diffPatch, summary := computeWorktreeDiff(targetPath, wtPath)

	if req.DeleteWorktreeAfterCheckout {
		if targetPath != "" && isGitRepo(targetPath) {
			cmd := exec.Command("git", "-C", targetPath, "worktree", "remove", "--force", wtPath)
			_ = cmd.Run()
		}
		_ = os.RemoveAll(wtPath)
	}

	res := map[string]any{
		"success": true,
		"summary": summary,
	}
	if diffPatch != "" {
		res["diffPatch"] = diffPatch
	}
	return res, nil, true
}

// handleLSGetWorktreeDiff returns the git diff of a worktree against the target workspace.
func (d *Dispatcher) handleLSGetWorktreeDiff(params json.RawMessage) (any, *rpc.ErrorBody, bool) {
	var req struct {
		WorktreeDirURI     string `json:"worktreeDirUri"`
		TargetWorkspaceURI string `json:"targetWorkspaceUri"`
	}
	if len(params) > 0 {
		_ = json.Unmarshal(params, &req)
	}

	rawURI := strings.TrimSpace(req.WorktreeDirURI)
	if rawURI == "" || !strings.HasPrefix(rawURI, "file://") {
		errID := newErrorID()
		msg := fmt.Sprintf("invalid_argument: failed to parse worktree_dir_uri: uri: \"\": unknown uri scheme (error ID: %s)", errID)
		return nil, &rpc.ErrorBody{
			Code:    -32602,
			Status:  3, // grpcStatusInvalidArgument -> HTTP 400
			Message: "invalid_argument",
			Data:    msg,
		}, true
	}

	wtPath := parseURIPath(rawURI)
	targetPath := parseURIPath(req.TargetWorkspaceURI)
	if targetPath == "" {
		targetPath = d.lsWorkspace()
	}

	diffPatch, summary := computeWorktreeDiff(targetPath, wtPath)
	return map[string]any{
		"diffPatch": diffPatch,
		"summary":   summary,
	}, nil, true
}

// handleLSGetBattleWorktreeDiff returns an empty diff set.
func (d *Dispatcher) handleLSGetBattleWorktreeDiff(params json.RawMessage) (any, *rpc.ErrorBody, bool) {
	return map[string]any{}, nil, true
}

// handleLSGetJJWorktrees returns unimplemented for external builds.
func (d *Dispatcher) handleLSGetJJWorktrees(params json.RawMessage) (any, *rpc.ErrorBody, bool) {
	errID := newErrorID()
	msg := fmt.Sprintf("unimplemented: GetJJWorktrees is not available in external builds (error ID: %s)", errID)
	return nil, &rpc.ErrorBody{
		Code:    -32601,
		Status:  12, // grpcStatusUnimplemented -> HTTP 501
		Message: "unimplemented",
		Data:    msg,
	}, true
}

// handleLSUpdatePRForWorktree attempts to update PR for a worktree.
func (d *Dispatcher) handleLSUpdatePRForWorktree(params json.RawMessage) (any, *rpc.ErrorBody, bool) {
	errID := newErrorID()
	msg := fmt.Sprintf("failed to update pull request: failed to get current branch: exit status 128 (error ID: %s)", errID)
	return nil, &rpc.ErrorBody{
		Code:    -32603,
		Status:  2, // grpcStatusUnknown -> HTTP 500
		Message: "unknown",
		Data:    msg,
	}, true
}

func computeWorktreeDiff(targetPath, wtPath string) (string, map[string]any) {
	if wtPath == "" {
		return "", map[string]any{}
	}
	cmd := exec.Command("git", "-C", wtPath, "diff", "HEAD~1")
	out, err := cmd.Output()
	if err != nil || len(out) == 0 {
		cmd = exec.Command("git", "-C", wtPath, "diff", "main")
		out, err = cmd.Output()
	}
	if err != nil || len(out) == 0 {
		cmd = exec.Command("git", "-C", wtPath, "diff")
		out, _ = cmd.Output()
	}
	patch := string(out)
	return patch, parseDiffSummary(patch)
}

func parseDiffSummary(patch string) map[string]any {
	files := 0
	adds := 0
	dels := 0
	lines := strings.Split(patch, "\n")
	for _, l := range lines {
		if strings.HasPrefix(l, "diff --git ") {
			files++
		} else if strings.HasPrefix(l, "+") && !strings.HasPrefix(l, "+++") {
			adds++
		} else if strings.HasPrefix(l, "-") && !strings.HasPrefix(l, "---") {
			dels++
		}
	}
	res := map[string]any{}
	if files > 0 {
		res["filesChanged"] = files
	}
	if adds > 0 {
		res["additions"] = adds
	}
	if dels > 0 {
		res["deletions"] = dels
	}
	return res
}
