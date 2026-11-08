package mockremoteagent

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestGetTurnDiffReadsTheWorkingCopy pins the proxy half: the session's
// workspace changes become FileDiffData entries with both versions.
func TestGetTurnDiffReadsTheWorkingCopy(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	runGit(t, git, dir, "init", "-q")
	runGit(t, git, dir, "config", "user.email", "test@example.com")
	runGit(t, git, dir, "config", "user.name", "Test")
	path := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(path, []byte("line1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, git, dir, "add", "file.txt")
	runGit(t, git, dir, "commit", "-q", "-m", "init")
	if err := os.WriteFile(path, []byte("line1\nline2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	srv, err := NewServer(WithAddr("127.0.0.1:0"))
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Close()
	srv.service.cascades.CreateSession("c1", "", "m", []string{"file://" + dir}, nil)

	rec := httptest.NewRecorder()
	srv.service.handleGetTurnDiff(rec, []byte(`{"conversationId":"c1","stepIndex":3}`))

	var got GetTurnDiffResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	fd, ok := got.FileDiffs["file.txt"]
	if !ok {
		t.Fatalf("fileDiffs = %+v", got.FileDiffs)
	}
	if fd.Additions != 1 || fd.Deletions != 0 {
		t.Errorf("counts = +%d -%d, want +1 -0", fd.Additions, fd.Deletions)
	}
	if fd.OriginalContents != "line1\n" || fd.ModifiedContents != "line1\nline2\n" {
		t.Errorf("contents = %q / %q", fd.OriginalContents, fd.ModifiedContents)
	}
	if got.TotalAdditions != 1 || got.TurnStartIndex != 3 {
		t.Errorf("response = %+v", got)
	}
}

func TestGetTurnDiffUnknownConversationIsEmpty(t *testing.T) {
	srv, err := NewServer(WithAddr("127.0.0.1:0"))
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Close()
	rec := httptest.NewRecorder()
	srv.service.handleGetTurnDiff(rec, []byte(`{"conversationId":"missing"}`))
	var got GetTurnDiffResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.FileDiffs) != 0 {
		t.Errorf("fileDiffs = %+v", got.FileDiffs)
	}
}

func runGit(t *testing.T, git, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command(git, append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// TestGetCodeActionDiffReturnsTheStepDiff pins the proxy's DiffBlock
// response for a FILE_CHANGE step.
func TestGetCodeActionDiffReturnsTheStepDiff(t *testing.T) {
	srv, err := NewServer(WithAddr("127.0.0.1:0"))
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Close()
	cs := srv.service.cascades.CreateSession("c1", "", "m", nil, nil)
	step := cs.AddFileChangeStep("file:///ws/a.txt", "write_to_file", `{"path":"a.txt"}`, []DiffLine{
		{Type: 0, Text: "@@ -1 +1 @@"},
		{Type: 2, Text: "old"},
		{Type: 1, Text: "new"},
	})

	rec := httptest.NewRecorder()
	body, _ := json.Marshal(map[string]any{"conversationId": "c1", "stepIndex": step.StepIndex})
	srv.service.handleGetCodeActionDiff(rec, body)

	var got GetCodeActionDiffResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	unified, _ := got.Diff["unifiedDiff"].(map[string]any)
	lines, _ := unified["lines"].([]any)
	if len(lines) != 3 {
		t.Fatalf("lines = %+v", got.Diff)
	}
	first := lines[0].(map[string]any)
	if first["type"] != float64(0) || first["text"] != "@@ -1 +1 @@" {
		t.Errorf("first = %+v", first)
	}
}

// TestGetTurnDiffIncludesNewFiles covers a file the turn created: it is
// untracked, so git diff --numstat alone would miss it.
func TestGetTurnDiffIncludesNewFiles(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	runGit(t, git, dir, "init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	srv, err := NewServer(WithAddr("127.0.0.1:0"))
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Close()
	srv.service.cascades.CreateSession("c1", "", "m", []string{"file://" + dir}, nil)

	rec := httptest.NewRecorder()
	srv.service.handleGetTurnDiff(rec, []byte(`{"conversationId":"c1"}`))
	var got GetTurnDiffResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	fd, ok := got.FileDiffs["new.txt"]
	if !ok || fd.Additions != 2 || fd.OriginalContents != "" || fd.ModifiedContents != "one\ntwo\n" {
		t.Errorf("fileDiffs = %+v", got.FileDiffs)
	}
}
