package convstore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/core/backend"
)

func TestNewUUIDv4(t *testing.T) {
	id, err := NewUUIDv4()
	if err != nil {
		t.Fatalf("NewUUIDv4 failed: %v", err)
	}
	if len(id) != 36 {
		t.Errorf("NewUUIDv4 returned %q (len %d, want 36)", id, len(id))
	}
	parts := strings.Split(id, "-")
	if len(parts) != 5 {
		t.Fatalf("expected 5 segments, got %d", len(parts))
	}
}

func TestSessionToSteps(t *testing.T) {
	sess := []backend.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: ""}, // should be elided
		{Role: "assistant", Content: "world\n\n"},
	}
	steps := SessionToSteps(sess)
	if len(steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(steps))
	}
	if steps[0].Text != "hello" || steps[0].Type != StepUserInput {
		t.Errorf("step 0 mismatch: %+v", steps[0])
	}
	if steps[1].Text != "world" || steps[1].Type != StepModelResponse {
		t.Errorf("step 1 mismatch: %+v", steps[1])
	}
}

func TestForkConversationEmpty(t *testing.T) {
	id, msg := ForkConversation(nil, "abc")
	if id != "" {
		t.Errorf("expected empty id, got %q", id)
	}
	if !strings.Contains(msg, "Error: no active conversation to fork") {
		t.Errorf("unexpected message: %q", msg)
	}
}

func TestShouldSkipDir(t *testing.T) {
	tests := []struct {
		relPath string
		name    string
		want    bool
	}{
		{".git", ".git", true},
		{"some/sub/.git", ".git", true},
		{".jj", ".jj", true},
		{"nested/.jj", ".jj", true},
		{".system_generated/worktrees", "worktrees", true},
		{".system_generated/worktrees/sub", "sub", true},
		{".system_generated/subagents", "subagents", true},
		{".system_generated/subagents/child", "child", true},
		{".system_generated/messages", "messages", true},
		{".system_generated/messages/item", "item", true},
		// Paths that should NOT be skipped
		{".system_generated", ".system_generated", false},
		{".system_generated/logs", "logs", false},
		{"scratch", "scratch", false},
		{"artifacts/subdir", "subdir", false},
		{".", ".", false},
		{"", "", false},
	}

	for _, tc := range tests {
		got := shouldSkipDir(tc.relPath, tc.name)
		if got != tc.want {
			t.Errorf("shouldSkipDir(%q, %q) = %v, want %v", tc.relPath, tc.name, got, tc.want)
		}
	}
}

func TestCopyArtifactTree(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	// Create test tree:
	// - doc.txt
	// - sub/nested.txt
	// - .git/HEAD
	// - .jj/repo
	// - .system_generated/worktrees/wt1.txt
	// - .system_generated/subagents/sub1.txt
	// - .system_generated/messages/msg1.txt
	// - .system_generated/logs/transcript.jsonl
	files := map[string]string{
		"doc.txt":                                   "hello world",
		"sub/nested.txt":                            "nested content",
		".git/HEAD":                                 "ref: refs/heads/main",
		".jj/repo":                                  "jj data",
		".system_generated/worktrees/wt1.txt":       "wt data",
		".system_generated/subagents/sub1.txt":      "subagent data",
		".system_generated/messages/msg1.txt":       "message data",
		".system_generated/logs/transcript.jsonl":   `{"step": 1}`,
	}

	for path, content := range files {
		fullPath := filepath.Join(src, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatalf("mkdir failed: %v", err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
			t.Fatalf("write file failed: %v", err)
		}
	}

	if err := CopyArtifactTree(src, dst); err != nil {
		t.Fatalf("CopyArtifactTree failed: %v", err)
	}

	// Verify allowed files exist in dst
	shouldExist := []string{
		"doc.txt",
		"sub/nested.txt",
		".system_generated/logs/transcript.jsonl",
	}
	for _, f := range shouldExist {
		target := filepath.Join(dst, f)
		data, err := os.ReadFile(target)
		if err != nil {
			t.Errorf("expected %s to be copied, but got error: %v", f, err)
		} else if string(data) != files[f] {
			t.Errorf("content mismatch for %s: got %q, want %q", f, string(data), files[f])
		}
	}

	// Verify skipped files do NOT exist in dst
	shouldNotExist := []string{
		".git",
		".jj",
		".system_generated/worktrees",
		".system_generated/subagents",
		".system_generated/messages",
	}
	for _, f := range shouldNotExist {
		target := filepath.Join(dst, f)
		if _, err := os.Stat(target); !os.IsNotExist(err) {
			t.Errorf("expected %s to be skipped, but it exists in dst", f)
		}
	}

	// Test non-existent srcDir (should not error)
	nonExistentSrc := filepath.Join(t.TempDir(), "nonexistent")
	if err := CopyArtifactTree(nonExistentSrc, dst); err != nil {
		t.Errorf("expected nil error for non-existent srcDir, got: %v", err)
	}

	// Test empty srcDir and dstDir
	if err := CopyArtifactTree("", dst); err != nil {
		t.Errorf("expected nil error for empty srcDir, got: %v", err)
	}
}

func TestForkConversationFull(t *testing.T) {
	// Set up temporary store dir
	tmpDir := t.TempDir()
	t.Setenv("OPENAGY_CONFIG_HOME", tmpDir)

	oldArtifactDir := filepath.Join(tmpDir, "old_artifacts")
	newArtifactDir := filepath.Join(tmpDir, "new_artifacts")

	if err := os.MkdirAll(oldArtifactDir, 0o755); err != nil {
		t.Fatalf("mkdir oldArtifactDir failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(oldArtifactDir, "report.md"), []byte("# Report"), 0o644); err != nil {
		t.Fatalf("write report.md failed: %v", err)
	}

	// Write an existing conversation into StoreDir
	currentID, err := NewUUIDv4()
	if err != nil {
		t.Fatalf("NewUUIDv4 failed: %v", err)
	}
	currentDB := ConversationPath(currentID)
	if err := os.MkdirAll(filepath.Dir(currentDB), 0o755); err != nil {
		t.Fatalf("mkdir conversations failed: %v", err)
	}

	initialSteps := []Step{
		{Idx: 0, Type: StepUserInput, Status: 3, Text: "Initial prompt"},
		{Idx: 1, Type: StepModelResponse, Status: 3, Text: "Initial answer"},
	}
	if err := WriteConversation(currentDB, currentID, initialSteps); err != nil {
		t.Fatalf("WriteConversation failed: %v", err)
	}

	// Fork with projectID, droppedSubagents=true, canceledTasks=true
	newID, forkNotice, feedback, err := ForkConversationFull(
		initialSteps,
		currentID,
		oldArtifactDir,
		newArtifactDir,
		"my-project",
		true,
		true,
	)
	if err != nil {
		t.Fatalf("ForkConversationFull failed: %v", err)
	}

	if newID == "" || newID == currentID {
		t.Errorf("expected distinct newID, got %q", newID)
	}

	// Verify destination DB was created and can be loaded
	newDB := ConversationPath(newID)
	conv, err := LoadConversation(newDB)
	if err != nil {
		t.Fatalf("LoadConversation on new DB failed: %v", err)
	}
	if len(conv.Steps) != 2 {
		t.Errorf("expected 2 steps in forked conv, got %d", len(conv.Steps))
	}
	if conv.TrajectoryID != newID {
		t.Errorf("expected TrajectoryID=%q, got %q", newID, conv.TrajectoryID)
	}

	// Verify artifact tree was copied
	copiedArtifact := filepath.Join(newArtifactDir, "report.md")
	if data, err := os.ReadFile(copiedArtifact); err != nil || string(data) != "# Report" {
		t.Errorf("expected copied artifact %s with content '# Report', got data=%q, err=%v", copiedArtifact, string(data), err)
	}

	// Verify forkNotice
	if !strings.Contains(forkNotice, "<fork_notice>") {
		t.Errorf("forkNotice missing <fork_notice>")
	}
	if !strings.Contains(forkNotice, currentID) {
		t.Errorf("forkNotice missing currentID: %s", forkNotice)
	}
	if !strings.Contains(forkNotice, oldArtifactDir) {
		t.Errorf("forkNotice missing oldArtifactDir: %s", forkNotice)
	}
	if !strings.Contains(forkNotice, newArtifactDir) {
		t.Errorf("forkNotice missing newArtifactDir: %s", forkNotice)
	}
	if !strings.Contains(forkNotice, "The subagents invoked in the history above are NOT yours") {
		t.Errorf("forkNotice missing dropped subagents notice: %s", forkNotice)
	}
	if !strings.Contains(forkNotice, "The background tasks in the history above are NOT yours") {
		t.Errorf("forkNotice missing canceled tasks notice: %s", forkNotice)
	}

	// Verify feedback message with projectID
	expectedFeedback := "Forked conversation into project \"my-project\".\nTo go back, use: /resume " + currentID
	if feedback != expectedFeedback {
		t.Errorf("feedback mismatch:\ngot:  %q\nwant: %q", feedback, expectedFeedback)
	}

	// Test fork without projectID, droppedSubagents=false, canceledTasks=false
	newID2, forkNotice2, feedback2, err := ForkConversationFull(
		initialSteps,
		currentID,
		oldArtifactDir,
		newArtifactDir+"2",
		"",
		false,
		false,
	)
	if err != nil {
		t.Fatalf("ForkConversationFull 2 failed: %v", err)
	}

	expectedFeedback2 := "Forked conversation.\nTo go back, use: /resume " + currentID
	if feedback2 != expectedFeedback2 {
		t.Errorf("feedback2 mismatch:\ngot:  %q\nwant: %q", feedback2, expectedFeedback2)
	}
	if strings.Contains(forkNotice2, "The subagents invoked in the history above are NOT yours") {
		t.Errorf("forkNotice2 should not contain dropped subagents notice")
	}
	if strings.Contains(forkNotice2, "The background tasks in the history above are NOT yours") {
		t.Errorf("forkNotice2 should not contain canceled tasks notice")
	}
	if newID2 == "" || newID2 == currentID || newID2 == newID {
		t.Errorf("expected distinct newID2, got %q", newID2)
	}

	// Test error when no conversation and no steps
	_, _, _, err = ForkConversationFull(nil, "", "", "", "", false, false)
	if err == nil {
		t.Errorf("expected error when forking with no conversation and no steps")
	}
}
