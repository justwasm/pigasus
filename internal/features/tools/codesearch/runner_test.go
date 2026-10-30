// runner_test.go — unit tests for codesearch runner, streaming, cancellation, and fallbacks.
package codesearch

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

func mockRgScript(t *testing.T, name, emit string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("mock rg script requires a POSIX shell")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	body := "#!/bin/sh\n" +
		"cat <<'EOF'\n" + emit + "\nEOF\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func drainMatches(s *Search) []Match {
	var out []Match
	for m := range s.Matches {
		out = append(out, m)
	}
	return out
}

func mockRgCmd(scriptPath string) func(ctx context.Context, root string, a Args) *exec.Cmd {
	return func(ctx context.Context, root string, a Args) *exec.Cmd {
		args := []string{"--json", "--no-heading", "--no-messages"}
		if a.Literal {
			args = append(args, "--fixed-strings")
		}
		for _, g := range a.Include {
			args = append(args, "-g", g)
		}
		for _, g := range a.Exclude {
			args = append(args, "-g", "!"+g)
		}
		args = append(args, "-e", a.Query)
		cmd := exec.CommandContext(ctx, scriptPath, args...)
		cmd.Dir = root
		cmd.Stderr = io.Discard
		return cmd
	}
}

func createTestFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRunRipgrepSuccess(t *testing.T) {
	emit := `{"type":"begin","data":{"path":{"text":"a.txt"}}}
{"type":"match","data":{"path":{"text":"a.txt"},"lines":{"text":"TODO wire this\n"},"line_number":2,"submatches":[{"match":{"text":"TODO"},"start":0,"end":4}]}}
{"type":"match","data":{"path":{"text":"a.txt"},"lines":{"text":"TODO refactor\n"},"line_number":5,"submatches":[{"match":{"text":"TODO"},"start":0,"end":4}]}}
{"type":"end","data":{"path":{"text":"a.txt"}}}
{"type":"begin","data":{"path":{"text":"b.go"}}}
{"type":"match","data":{"path":{"text":"b.go"},"lines":{"text":"\u4e2d\u6587 TODO\n"},"line_number":3,"submatches":[{"match":{"text":"TODO"},"start":7,"end":11}]}}
{"type":"end","data":{"path":{"text":"b.go"}}}
{"type":"summary","data":{}}`
	script := mockRgScript(t, "rg-success", emit)

	r := &Runner{
		RgPath:  script,
		Workdir: t.TempDir(),
		RgBin:   mockRgCmd(script),
	}
	s := &Search{Query: "TODO", Root: r.Workdir}
	// The deadline is generous on purpose: the mock rg script is a
	// shell script, and under -race on a loaded machine two seconds is
	// not enough headroom — a timeout here would read as a search
	// failure rather than as a slow machine.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := r.Run(ctx, s); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if s.CurrentState() != StateDone {
		t.Errorf("State = %q, want %q", s.CurrentState(), StateDone)
	}
	matches := drainMatches(s)
	if len(matches) != 3 {
		t.Fatalf("got %d matches, want 3: %+v", len(matches), matches)
	}
	if matches[2].Col != 4 {
		t.Errorf("CJK match Col = %d, want 4 (rune column, not byte)", matches[2].Col)
	}
	if !strings.Contains(matches[2].Text, "\u4e2d\u6587") {
		t.Errorf("CJK text lost: %q", matches[2].Text)
	}
}

func createBlockingRgScript(t *testing.T) string {
	t.Helper()
	blockingPath := filepath.Join(t.TempDir(), "rg-block")
	body := "#!/bin/sh\n" +
		"cat <<'EOF'\n" +
		`{"type":"begin","data":{"path":{"text":"x.txt"}}}
{"type":"match","data":{"path":{"text":"x.txt"},"lines":{"text":"x\n"},"line_number":1,"submatches":[]}}
` +
		"EOF\n" +
		"exec sleep 30\n"
	if err := os.WriteFile(blockingPath, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return blockingPath
}

func waitForAttachedChild(t *testing.T, r *Runner) int {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if pid := r.childPID(); pid != 0 {
			return pid
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("runner never attached the child cmd")
	return 0
}

func checkRgProcessDead(t *testing.T, pid int) {
	t.Helper()
	if proc, err := os.FindProcess(pid); err == nil {
		_ = syscall.Signal(0)
		if err := proc.Signal(syscall.Signal(0)); err == nil {
			t.Errorf("rg child pid=%d still alive after cancel", pid)
		}
	}
}

func TestRunRipgrepCancelled(t *testing.T) {
	blockingPath := createBlockingRgScript(t)
	r := &Runner{
		RgPath:  blockingPath,
		Workdir: t.TempDir(),
		RgBin:   mockRgCmd(blockingPath),
	}
	s := &Search{Query: "x", Root: r.Workdir}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx, s) }()

	pid := waitForAttachedChild(t, r)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run returned %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
	if s.CurrentState() != StateCancelled {
		t.Errorf("State = %q, want %q", s.CurrentState(), StateCancelled)
	}
	checkRgProcessDead(t, pid)
}

func TestRunRipgrepFallback(t *testing.T) {
	root := t.TempDir()
	createTestFiles(t, root, map[string]string{
		"a.go": "// TODO: hello\npackage x\n",
		"b.go": "// TODO again\n",
	})
	r := &Runner{
		RgPath:  "/nonexistent/rg-binary-that-does-not-exist",
		Workdir: root,
	}
	s := &Search{Query: "TODO", Root: root}
	if err := r.Run(context.Background(), s); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if s.CurrentState() != StateDone {
		t.Errorf("State = %q, want %q", s.CurrentState(), StateDone)
	}
	matches := drainMatches(s)
	if len(matches) != 2 {
		t.Fatalf("got %d fallback matches, want 2: %+v", len(matches), matches)
	}
}

func TestFallbackGlobFilter(t *testing.T) {
	root := t.TempDir()
	createTestFiles(t, root, map[string]string{
		"keep.go":  "TODO one\n",
		"skip.txt": "TODO two\n",
	})
	r := &Runner{RgPath: "", Workdir: root}
	s := &Search{
		Query:   "TODO",
		Root:    root,
		Include: []string{"*.go"},
	}
	if err := r.Run(context.Background(), s); err != nil {
		t.Fatalf("Run: %v", err)
	}
	matches := drainMatches(s)
	if len(matches) != 1 {
		t.Fatalf("Include filter: got %d matches, want 1: %+v", len(matches), matches)
	}
	if matches[0].File != "keep.go" {
		t.Errorf("matched %q, want keep.go", matches[0].File)
	}
}

func TestFallbackGlobDoubleStar(t *testing.T) {
	root := t.TempDir()
	createTestFiles(t, root, map[string]string{
		"test_qux.go":                  "TODO root\n",
		"internal/foo/bar/test_baz.go": "TODO deep\n",
		"internal/keep.go":             "TODO keep\n",
		"docs/test_skip.md":            "TODO skip\n",
	})
	r := &Runner{RgPath: "", Workdir: root}
	s := &Search{
		Query:   "TODO",
		Root:    root,
		Include: []string{"**/test_*.go"},
	}
	if err := r.Run(context.Background(), s); err != nil {
		t.Fatalf("Run: %v", err)
	}
	matches := drainMatches(s)
	if len(matches) != 2 {
		t.Fatalf("** glob: got %d matches, want 2: %+v", len(matches), matches)
	}
	got := map[string]bool{}
	for _, m := range matches {
		got[m.File] = true
	}
	if !got["test_qux.go"] {
		t.Errorf("**/test_*.go missed root-level test_qux.go")
	}
	if !got["internal/foo/bar/test_baz.go"] {
		t.Errorf("**/test_*.go missed nested internal/foo/bar/test_baz.go")
	}
	if got["internal/keep.go"] || got["docs/test_skip.md"] {
		t.Errorf("**/test_*.go matched something it shouldn't: %+v", matches)
	}
}

func TestFallbackGlobBraceExpansion(t *testing.T) {
	root := t.TempDir()
	createTestFiles(t, root, map[string]string{
		"a.go":          "TODO go-root\n",
		"sub/deep/b.go": "TODO go-deep\n",
		"c.md":          "TODO md-root\n",
		"sub/deep/d.md": "TODO md-deep\n",
		"e.txt":         "TODO txt\n",
	})
	r := &Runner{RgPath: "", Workdir: root}
	s := &Search{
		Query:   "TODO",
		Root:    root,
		Include: []string{"**/*.{go,md}"},
	}
	if err := r.Run(context.Background(), s); err != nil {
		t.Fatalf("Run: %v", err)
	}
	matches := drainMatches(s)
	if len(matches) != 4 {
		t.Fatalf("brace glob: got %d matches, want 4: %+v", len(matches), matches)
	}
	got := map[string]bool{}
	for _, m := range matches {
		got[m.File] = true
	}
	for _, want := range []string{"a.go", "sub/deep/b.go", "c.md", "sub/deep/d.md"} {
		if !got[want] {
			t.Errorf("**/*.{go,md} missed %q (got %+v)", want, got)
		}
	}
}

func TestFallbackGlobExcludeDoublestar(t *testing.T) {
	root := t.TempDir()
	createTestFiles(t, root, map[string]string{
		"keep.go":                         "TODO keep-root\n",
		"sub/keep.go":                     "TODO keep-deep\n",
		"generated/x.go":                  "TODO gen-root\n",
		"sub/generated/y.go":              "TODO gen-deep\n",
		"sub/deeper/generated/inner/z.go": "TODO gen-inner\n",
	})
	r := &Runner{RgPath: "", Workdir: root}
	s := &Search{
		Query:   "TODO",
		Root:    root,
		Exclude: []string{"**/generated/**"},
	}
	if err := r.Run(context.Background(), s); err != nil {
		t.Fatalf("Run: %v", err)
	}
	matches := drainMatches(s)
	if len(matches) != 2 {
		t.Fatalf("generated exclude: got %d matches, want 2: %+v", len(matches), matches)
	}
	got := map[string]bool{}
	for _, m := range matches {
		got[m.File] = true
	}
	for _, want := range []string{"keep.go", "sub/keep.go"} {
		if !got[want] {
			t.Errorf("exclude **/generated/** missed expected %q (got %+v)", want, got)
		}
	}
	for _, bad := range []string{"generated/x.go", "sub/generated/y.go", "sub/deeper/generated/inner/z.go"} {
		if got[bad] {
			t.Errorf("exclude **/generated/** leaked %q", bad)
		}
	}
}
