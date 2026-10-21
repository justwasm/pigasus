// Test stubs.go — verifies the cycle-30 real implementations of the 28
// v4 stubs. Five tests, one per group:
//
//   - TestStubsRunGit     — Status.Run on a fresh git repo
//   - TestStubsRunGrep    — GrepSearch + Grep on a temp directory
//   - TestStubsReadURL    — ReadURLContent via httptest.NewServer (no network)
//   - TestStubsUnzip      — Unzip on a Go-generated zip (no `zip` CLI needed)
//   - TestStubsWait       — Wait via ctx + time.After
//   - TestStubsCreateTask — create_task wires subagent.NewSubagent + returns ID/Result
//   - TestStubsSendMessage — send_message pipes content into a running Subagent via Send
//   - TestStubsSkillSearch — skill_search runs `grep -rln` through subagent and parses stdout
//
// Tests skip (rather than fail) when a system tool they need is missing,
// except Wait which only depends on time.After and is always available.
package tools

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/subagent"
)

// TestStubsRunGit — Status.Run on a freshly-initialized git repo. We don't
// require any specific output, only that the call succeeds without error
// (git prints "On branch …" / "nothing to commit" etc).
func TestStubsRunGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}
	dir := t.TempDir()
	{
		cmd := exec.Command("git", "init", "-q")
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git init: %v (%s)", err, out)
		}
		// git refuses commits without user.email / user.name when running
		// certain commands; set safe defaults for this temp repo.
		for k, v := range map[string]string{
			"user.email": "test@example.com",
			"user.name":  "test",
		} {
			c := exec.Command("git", "config", k, v)
			c.Dir = dir
			if out, err := c.CombinedOutput(); err != nil {
				t.Fatalf("git config %s: %v (%s)", k, err, out)
			}
		}
	}

	st := &Status{Workspace: dir}
	out, err := st.Run(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Status.Run: %v", err)
	}
	t.Logf("git status output: %q", out)

	// Exercise Fetch with a remote/branch, expecting a failure (no remote
	// configured) but the call path should execute without panicking.
	f := &Fetch{Workspace: dir}
	if _, err := f.Run(context.Background(), json.RawMessage(`{"remote":"origin"}`)); err == nil {
		t.Log("fetch unexpectedly succeeded (no remote configured)")
	}
}

// TestStubsRunGrep — GrepSearch + Grep on a temp directory with a known
// pattern. Verifies that the fallback to grep -rn works (rg is preferred
// but not required).
func TestStubsRunGrep(t *testing.T) {
	if _, err := exec.LookPath("grep"); err != nil {
		t.Skip("grep not in PATH")
	}
	dir := t.TempDir()
	for name, body := range map[string]string{
		"a.txt":     "hello world\nfoo bar\n",
		"b.txt":     "another file\nhello again\n",
		"skipme.md": "no match here\n",
		"sub/c.txt": "third hello\n",
	} {
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("GrepSearch", func(t *testing.T) {
		g := &GrepSearch{Workspace: dir}
		out, err := g.Run(context.Background(), json.RawMessage(`{"pattern":"hello"}`))
		if err != nil {
			t.Fatalf("GrepSearch.Run: %v", err)
		}
		s, ok := out.(string)
		if !ok {
			t.Fatalf("expected string, got %T", out)
		}
		if !strings.Contains(s, "hello") {
			t.Errorf("expected output to contain 'hello', got %q", s)
		}
		// Should match in a.txt, b.txt, sub/c.txt (3 hits).
		if c := strings.Count(s, "hello"); c < 3 {
			t.Errorf("expected ≥3 hello hits, got %d (output: %q)", c, s)
		}
	})

	t.Run("Grep", func(t *testing.T) {
		g := &Grep{Workspace: dir}
		out, err := g.Run(context.Background(), json.RawMessage(`{"pattern":"foo","case_insensitive":true}`))
		if err != nil {
			t.Fatalf("Grep.Run: %v", err)
		}
		s, ok := out.(string)
		if !ok {
			t.Fatalf("expected string, got %T", out)
		}
		if !strings.Contains(s, "foo") {
			t.Errorf("expected output to contain 'foo', got %q", s)
		}
	})

	t.Run("FileSearch", func(t *testing.T) {
		// query="txt" matches a.txt, b.txt, sub/c.txt (basename only).
		fs := &FileSearch{Workspace: dir}
		out, err := fs.Run(context.Background(), json.RawMessage(`{"query":"txt","limit":10}`))
		if err != nil {
			t.Fatalf("FileSearch.Run: %v", err)
		}
		matches, ok := out.([]string)
		if !ok {
			t.Fatalf("expected []string, got %T", out)
		}
		if len(matches) < 3 {
			t.Errorf("expected ≥3 file matches (a.txt, b.txt, sub/c.txt), got %d (%v)", len(matches), matches)
		}
	})
}

// TestStubsReadURL — ReadURLContent via httptest.NewServer. We do NOT hit
// example.com directly so this test works in sandboxed / offline CI. The
// reference saves the article and returns Title / OG Description + path.
func TestStubsReadURL(t *testing.T) {
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html><head><title>Example Domain</title></head><body><p>body content for testing.</p></body></html>"))
	}))
	defer ts.Close()

	r := &ReadURLContent{}
	out, err := r.Run(context.Background(),
		json.RawMessage(fmt.Sprintf(`{"url":%q,"max_bytes":100}`, ts.URL)))
	if err != nil {
		t.Fatalf("ReadURLContent.Run: %v", err)
	}
	s, ok := out.(string)
	if !ok {
		t.Fatalf("expected string, got %T", out)
	}
	if !strings.Contains(s, "Title: Example Domain") {
		t.Errorf("expected the reference title line, got %q", s)
	}
	if !strings.Contains(s, "has been saved to: ") {
		t.Errorf("expected the saved-content line, got %q", s)
	}
	// max_bytes caps the saved article, not the result text.
	saved := s[strings.Index(s, "has been saved to: ")+len("has been saved to: "):]
	saved = strings.TrimSpace(saved[:strings.Index(saved, "\n")])
	raw, err := os.ReadFile(saved)
	if err != nil {
		t.Fatalf("read saved article: %v", err)
	}
	if len(raw) > 100 {
		t.Errorf("article len = %d, want ≤ 100", len(raw))
	}
}

// TestStubsUnzip — Unzip on a zip created via archive/zip (no external
// `zip` CLI required). Verifies the extracted file's contents match.
func TestStubsUnzip(t *testing.T) {
	if _, err := exec.LookPath("unzip"); err != nil {
		t.Skip("unzip not in PATH")
	}
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "test.zip")
	if err := writeTestZip(zipPath, map[string][]byte{
		"src.txt": []byte("hello world"),
		"meta":    []byte("metadata"),
	}); err != nil {
		t.Fatalf("writeTestZip: %v", err)
	}
	dst := filepath.Join(dir, "extracted")
	if err := os.MkdirAll(dst, 0755); err != nil {
		t.Fatal(err)
	}

	u := &Unzip{Workspace: dir}
	out, err := u.Run(context.Background(),
		json.RawMessage(`{"archive":"test.zip","destination":"extracted"}`))
	if err != nil {
		t.Fatalf("Unzip.Run: %v (output: %q)", err, out)
	}
	t.Logf("unzip output: %q", out)

	data, err := os.ReadFile(filepath.Join(dst, "src.txt"))
	if err != nil {
		t.Fatalf("read extracted src.txt: %v", err)
	}
	if string(data) != "hello world" {
		t.Errorf("src.txt: got %q, want %q", data, "hello world")
	}
}

// TestStubsWait — Wait via duration="100ms". Verifies the call blocks for
// at least the requested duration and returns a string containing the
// duration.
func TestStubsWait(t *testing.T) {
	w := &Wait{}
	start := time.Now()
	out, err := w.Run(context.Background(), json.RawMessage(`{"duration":"100ms"}`))
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Wait.Run: %v", err)
	}
	if elapsed < 90*time.Millisecond {
		t.Errorf("expected to wait ≥100ms, only waited %v", elapsed)
	}
	s, ok := out.(string)
	if !ok {
		t.Fatalf("expected string, got %T", out)
	}
	if !strings.Contains(s, "100ms") {
		t.Errorf("expected output to mention 100ms, got %q", s)
	}
}

// writeTestZip creates a zip at <path> with the given name→content map.
func writeTestZip(path string, files map[string][]byte) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			return err
		}
		if _, err := io.Copy(w, strings.NewReader(string(body))); err != nil {
			return err
		}
	}
	return zw.Close()
}

// ============ Cycle 31: subagent-wired stubs ============

// TestStubsCreateTask — verifies create_task spawns a real Subagent via
// subagent.NewSubagent, blocks until completion, and surfaces ID + Result.
// Uses `echo hello` so the underlying process exits cleanly on its own.
func TestStubsCreateTask(t *testing.T) {
	c := &CreateTask{}
	out, err := c.Run(context.Background(), json.RawMessage(`{"agent_name":"echo","prompt":"hello","title":"say-hi"}`))
	if err != nil {
		t.Fatalf("CreateTask.Run: %v", err)
	}
	m, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", out)
	}
	id, _ := m["id"].(string)
	if id == "" {
		t.Fatalf("expected non-empty id, got %v", m["id"])
	}
	if !strings.HasPrefix(id, "sub-") {
		t.Errorf("expected id prefix 'sub-', got %q", id)
	}
	state, _ := m["state"].(string)
	if state != "done" {
		t.Errorf("expected state=done, got %q", state)
	}
	result, _ := m["result"].(string)
	if !strings.Contains(result, "hello") {
		t.Errorf("expected result to contain 'hello', got %q", result)
	}
	if name, _ := m["name"].(string); name != "echo" {
		t.Errorf("expected name=echo, got %q", name)
	}
	if title, _ := m["title"].(string); title != "say-hi" {
		t.Errorf("expected title=say-hi, got %q", title)
	}
	// Negative case: missing required field.
	if _, err := c.Run(context.Background(), json.RawMessage(`{"agent_name":"echo"}`)); err == nil {
		t.Error("expected error when prompt is missing")
	}
}

// TestStubsSendMessage — verifies send_message locates a running Subagent
// by ID (registered via create_task or directly) and pipes content into
// its stdin. We spawn `cat` directly + register with defaultSubagentManager
// (mirroring what create_task does internally) since create_task blocks
// until the subagent exits and `cat` runs forever.
func TestStubsSendMessage(t *testing.T) {
	// Re-use the package-level manager the same way create_task does.
	sa := subagent.NewSubagent("cat")
	defaultSubagentManager.Add(sa)
	if err := sa.Start(); err != nil {
		t.Fatalf("subagent Start: %v", err)
	}
	t.Cleanup(func() {
		_ = sa.Kill()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		sa.Wait(ctx)
	})

	sm := &SendMessage{}
	out, err := sm.Run(context.Background(),
		json.RawMessage(fmt.Sprintf(`{"task_id":%q,"content":"ping-from-test\n"}`, sa.ID)))
	if err != nil {
		t.Fatalf("SendMessage.Run: %v", err)
	}
	s, ok := out.(string)
	if !ok {
		t.Fatalf("expected string, got %T", out)
	}
	if !strings.Contains(s, "ok: message sent") {
		t.Errorf("expected success message, got %q", s)
	}

	// Let cat echo the message back through the captured stdout pipe.
	time.Sleep(150 * time.Millisecond)

	// Kill and drain so Result is finalised.
	if err := sa.Kill(); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	sa.Wait(ctx)

	if !strings.Contains(sa.Result, "ping-from-test") {
		t.Errorf("expected subagent Result to contain the sent message, got %q", sa.Result)
	}

	// Negative case: unknown task_id must surface an error.
	if _, err := sm.Run(context.Background(),
		json.RawMessage(`{"task_id":"sub-does-not-exist","content":"x"}`)); err == nil {
		t.Error("expected error for unknown task_id, got nil")
	}
}

// TestStubsSkillSearch — verifies skill_search runs grep -rln through
// subagent.NewSubagent and returns the matching file paths. The directory
// shape mimics the documented convention (workspace/skills/*.md).
func TestStubsSkillSearch(t *testing.T) {
	if _, err := exec.LookPath("grep"); err != nil {
		t.Skip("grep not in PATH")
	}
	dir := t.TempDir()
	skillsDir := filepath.Join(dir, "skills")
	if err := os.MkdirAll(skillsDir, 0755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"git_commit.md":       "# git commit\nhow to write a commit message\n",
		"git_push.md":         "# git push\ngit push origin main\n",
		"docker_build.md":     "# docker build\ndocker build -t foo .\n",
		"kubernetes_apply.md": "# kubernetes apply\nkubectl apply -f foo.yaml\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(skillsDir, name), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("match", func(t *testing.T) {
		s := &SkillSearch{Workspace: dir}
		out, err := s.Run(context.Background(), json.RawMessage(`{"query":"git","top_k":5}`))
		if err != nil {
			t.Fatalf("SkillSearch.Run: %v", err)
		}
		matches, ok := out.([]string)
		if !ok {
			t.Fatalf("expected []string, got %T", out)
		}
		if len(matches) < 2 {
			t.Errorf("expected ≥2 git* matches, got %d (%v)", len(matches), matches)
		}
		seen := false
		for _, m := range matches {
			if strings.HasSuffix(m, "git_commit.md") {
				seen = true
			}
		}
		if !seen {
			t.Errorf("expected git_commit.md in matches, got %v", matches)
		}
	})

	t.Run("nomatch", func(t *testing.T) {
		s := &SkillSearch{Workspace: dir}
		out, err := s.Run(context.Background(), json.RawMessage(`{"query":"totally-unrelated-string-xyz","top_k":5}`))
		if err != nil {
			t.Fatalf("SkillSearch.Run (nomatch): %v", err)
		}
		matches, ok := out.([]string)
		if !ok {
			t.Fatalf("expected []string, got %T", out)
		}
		if len(matches) != 0 {
			t.Errorf("expected zero matches, got %d (%v)", len(matches), matches)
		}
	})

	t.Run("missing_skills_dir", func(t *testing.T) {
		emptyDir := t.TempDir() // no skills/ subdirectory
		s := &SkillSearch{Workspace: emptyDir}
		out, err := s.Run(context.Background(), json.RawMessage(`{"query":"anything"}`))
		if err != nil {
			t.Fatalf("SkillSearch.Run (no skills dir): %v", err)
		}
		matches, ok := out.([]string)
		if !ok || len(matches) != 0 {
			t.Errorf("expected empty []string when skills dir is missing, got %v (%T)", out, out)
		}
	})

	t.Run("missing_query", func(t *testing.T) {
		s := &SkillSearch{Workspace: dir}
		if _, err := s.Run(context.Background(), json.RawMessage(`{}`)); err == nil {
			t.Error("expected error when query is missing")
		}
	})
}
