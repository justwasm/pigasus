package agentserver

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/infra/mockremoteagent"
)

func TestDiffBlockLines(t *testing.T) {
	lines := diffBlockLines("a.txt", "old\n", "new\n")
	var inserts, deletes int
	for _, line := range lines {
		switch line.Type {
		case 1:
			inserts++
			if line.Text != "new" {
				t.Errorf("insert text = %q", line.Text)
			}
		case 2:
			deletes++
			if line.Text != "old" {
				t.Errorf("delete text = %q", line.Text)
			}
		}
	}
	if inserts != 1 || deletes != 1 {
		t.Fatalf("lines = %+v", lines)
	}
}

func TestFileChangeStepPublishesTheDiff(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	runGit(t, git, dir, "init", "-q")
	runGit(t, git, dir, "config", "user.email", "t@example.com")
	runGit(t, git, dir, "config", "user.name", "T")
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, git, dir, "add", "a.txt")
	runGit(t, git, dir, "commit", "-q", "-m", "init")
	if err := os.WriteFile(path, []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	session := mockremoteagent.NewCascadeSession("c1", "", "m", nil, nil)
	ok := fileChangeStep(session, dir, "write_to_file", json.RawMessage(`{"path":"a.txt","content":"new"}`))
	if !ok {
		t.Fatal("fileChangeStep did not accept the tool call")
	}
	steps := session.StepsSnapshot()
	last := steps[len(steps)-1]
	if last.Type != "FILE_CHANGE" {
		t.Fatalf("step type = %q", last.Type)
	}
	if !strings.HasSuffix(last.FilePath, "/a.txt") || !strings.HasPrefix(last.FilePath, "file://") {
		t.Errorf("file path = %q", last.FilePath)
	}
	if last.ToolName != "write_to_file" || last.ToolArgs == "" {
		t.Errorf("tool = %q %q", last.ToolName, last.ToolArgs)
	}
	var inserted bool
	for _, line := range last.DiffLines {
		if line.Type == 1 && line.Text == "new" {
			inserted = true
		}
	}
	if !inserted {
		t.Errorf("diff lines = %+v", last.DiffLines)
	}
}

func TestFileChangeStepRejectsOutsidePaths(t *testing.T) {
	session := mockremoteagent.NewCascadeSession("c1", "", "m", nil, nil)
	if fileChangeStep(session, "/ws", "write_to_file", json.RawMessage(`{"path":"/etc/passwd"}`)) {
		t.Fatal("an outside path was accepted")
	}
}

func runGit(t *testing.T, git, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command(git, append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}
