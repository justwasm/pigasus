// codesearch_test.go — cycle 228: the /codesearch panel's streaming
// runtime.
//
// The six tests cover the contract recovered from the AGY 1.4.0
// changelog: ripgrep success + cancel + fallback, the f:/file: glob
// flags, the -F/--literal flag, the CJK-truncation fix, and the empty-
// query short-circuit. They all run against a mock rg binary (a shell
// script that emits the rg --json protocol verbatim) so the test never
// depends on a real ripgrep install — important because the CJK test
// in particular wants deterministic byte-exact output.

package codesearch

import (
	"context"
	"errors"
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

// TestParseArgs locks the cycle-228 argument contract: -F and --literal
// are recognised flags, -- terminates the flag list, f:<glob> and
// file:<glob> are repeatable include / exclude flags, and an empty
// pattern tail yields ErrEmptyQuery.
func TestParseArgs(t *testing.T) {
	cases := []struct {
		name      string
		args      string
		wantQuery string
		wantLit   bool
		wantInc   []string
		wantExc   []string
		wantErr   error
	}{
		{
			name:      "regex-default",
			args:      "TODO",
			wantQuery: "TODO",
		},
		{
			name:      "literal-short",
			args:      "-F needle",
			wantQuery: "needle",
			wantLit:   true,
		},
		{
			name:      "literal-long",
			args:      "--literal n.edle",
			wantQuery: "n.edle",
			wantLit:   true,
		},
		{
			name:      "include-glob",
			args:      "TODO f:*.go",
			wantQuery: "TODO",
			wantInc:   []string{"*.go"},
		},
		{
			name:      "exclude-glob",
			args:      "TODO file:*_test.go",
			wantQuery: "TODO",
			wantExc:   []string{"*_test.go"},
		},
		{
			name:      "include-and-exclude",
			args:      "TODO f:*.go file:*_test.go file:*.pb.go",
			wantQuery: "TODO",
			wantInc:   []string{"*.go"},
			wantExc:   []string{"*_test.go", "*.pb.go"},
		},
		{
			name:    "empty",
			args:    "",
			wantErr: ErrEmptyQuery,
		},
		{
			name:    "flags-only",
			args:    "-F",
			wantErr: ErrEmptyQuery,
		},
		{
			name:      "unknown-flag-becomes-pattern",
			args:      "--zzz TODO",
			wantQuery: "--zzz TODO",
		},
		{
			name:      "double-dash-makes-flag-is-pattern",
			args:      "-- -F",
			wantQuery: "-F",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, err := ParseArgs(tc.args)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("ParseArgs(%q) err = %v, want %v", tc.args, err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseArgs(%q) err = %v", tc.args, err)
			}
			if a.Query != tc.wantQuery {
				t.Errorf("Query = %q, want %q", a.Query, tc.wantQuery)
			}
			if a.Literal != tc.wantLit {
				t.Errorf("Literal = %v, want %v", a.Literal, tc.wantLit)
			}
			if !stringSliceEqual(a.Include, tc.wantInc) {
				t.Errorf("Include = %v, want %v", a.Include, tc.wantInc)
			}
			if !stringSliceEqual(a.Exclude, tc.wantExc) {
				t.Errorf("Exclude = %v, want %v", a.Exclude, tc.wantExc)
			}
		})
	}
}

// TestParseArgsMalformed is the dedicated short-circuit check.
func TestParseArgsMalformed(t *testing.T) {
	if _, err := ParseArgs(""); !errors.Is(err, ErrEmptyQuery) {
		t.Errorf("empty args: err = %v, want ErrEmptyQuery", err)
	}
	if _, err := ParseArgs("-F"); !errors.Is(err, ErrEmptyQuery) {
		t.Errorf("flags-only args: err = %v, want ErrEmptyQuery", err)
	}
	if _, err := ParseArgs("f:*.go"); !errors.Is(err, ErrEmptyQuery) {
		t.Errorf("glob-only args: err = %v, want ErrEmptyQuery", err)
	}
}

// mockRgScript builds a one-shot shell script that emits the rg --json
// protocol verbatim. name is the basename (so multiple mocks can run
// concurrently); emit is one JSON object per line.
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

// TestRunRipgrepSuccess runs the runner against a mock rg that emits
// three matches across two files. The Search must end up StateDone
// with all three matches streamed.
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
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := r.Run(ctx, s); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if s.State != StateDone {
		t.Errorf("State = %q, want %q", s.State, StateDone)
	}
	matches := drainMatches(s)
	if len(matches) != 3 {
		t.Fatalf("got %d matches, want 3: %+v", len(matches), matches)
	}
	// The byte offset "TODO" at byte 7 of "\u4e2d\u6587 TODO" must
	// become rune column 5 (rune index 4 + 1) — not byte column 8.
	if matches[2].Col != 4 {
		t.Errorf("CJK match Col = %d, want 4 (rune column, not byte)", matches[2].Col)
	}
	if !strings.Contains(matches[2].Text, "\u4e2d\u6587") {
		t.Errorf("CJK text lost: %q", matches[2].Text)
	}
}

// TestRunRipgrepCancelled cancels the search mid-stream and verifies
// the State flips to StateCancelled and the rg child is no longer
// running. The mock rg blocks on stdin so we can hold the process
// alive until cancel fires.
func TestRunRipgrepCancelled(t *testing.T) {
	// Wrap so the mock rg blocks on stdin — the test cancels before
	// EOF, and Cancel() must kill it.
	blockingPath := filepath.Join(t.TempDir(), "rg-block")
	body := "#!/bin/sh\n" +
		"cat <<'EOF'\n" +
		`{"type":"begin","data":{"path":{"text":"x.txt"}}}
{"type":"match","data":{"path":{"text":"x.txt"},"lines":{"text":"x\n"},"line_number":1,"submatches":[]}}
` +
		"EOF\n" +
		// Now block so cmd.Wait() would hang forever if Kill didn't work.
		"exec sleep 30\n"
	if err := os.WriteFile(blockingPath, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	r := &Runner{
		RgPath:  blockingPath,
		Workdir: t.TempDir(),
		RgBin:   mockRgCmd(blockingPath),
	}
	s := &Search{Query: "x", Root: r.Workdir}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx, s) }()

	// Wait until the runner has the child attached.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if r.cancelCmd != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if r.cancelCmd == nil {
		t.Fatal("runner never attached the child cmd")
	}
	pid := r.cancelCmd.Process.Pid

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run returned %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
	if s.State != StateCancelled {
		t.Errorf("State = %q, want %q", s.State, StateCancelled)
	}

	// Verify the rg child has actually exited (Kill cleared it).
	if proc, err := os.FindProcess(pid); err == nil {
		// On Unix, Signal(0) is the existence probe. ESRCH means the
		// process is gone; any other error means it isn't reachable
		// from us, but FindProcess on Unix always succeeds. The
		// robust check is to wait briefly for it to disappear.
		_ = syscall.Signal(0) // existence-probe signal
		if err := proc.Signal(syscall.Signal(0)); err == nil {
			t.Errorf("rg child pid=%d still alive after cancel", pid)
		}
	}
}

// TestRunRipgrepFallback feeds the runner a nonexistent rg binary;
// it must fall through to the filepath.Walk path and still report
// the matches.
func TestRunRipgrepFallback(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"),
		[]byte("// TODO: hello\npackage x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "b.go"),
		[]byte("// TODO again\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := &Runner{
		RgPath:  "/nonexistent/rg-binary-that-does-not-exist",
		Workdir: root,
	}
	s := &Search{Query: "TODO", Root: root}
	if err := r.Run(context.Background(), s); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if s.State != StateDone {
		t.Errorf("State = %q, want %q", s.State, StateDone)
	}
	matches := drainMatches(s)
	if len(matches) != 2 {
		t.Fatalf("got %d fallback matches, want 2: %+v", len(matches), matches)
	}
}

// TestCJKTruncation exercises the AGY 1.4.0 fix: long lines and CJK
// paths are no longer rendered as replacement glyphs. The Truncate
// helper must end the result with an ellipsis and never emit a U+FFFD.
func TestCJKTruncation(t *testing.T) {
	cases := []struct {
		name  string
		text  string
		width int
		// contains the bytes "�" → forbidden
	}{
		{
			name:  "ascii-fits",
			text:  "hello world",
			width: 20,
		},
		{
			name:  "ascii-clips",
			text:  strings.Repeat("a", 80),
			width: 10,
		},
		{
			name:  "cjk-clips",
			text:  strings.Repeat("\u4e2d", 50), // 中 × 50
			width: 6,
		},
		{
			name:  "mixed",
			text:  "TODO \u4e2d\u6587 " + strings.Repeat("z", 100),
			width: 12,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Truncate(tc.text, tc.width)
			if strings.Contains(got, "\ufffd") {
				t.Errorf("Truncate emitted replacement glyph: %q", got)
			}
			if !strings.HasSuffix(got, "\u2026") && len(tc.text) > tc.width && tc.width > 0 {
				t.Errorf("Truncate should end with ellipsis when over budget: %q", got)
			}
		})
	}

	// Width = 1 always returns the ellipsis alone — never the first
	// rune, never empty.
	if got := Truncate("\u4e2d\u6587", 1); got != "\u2026" {
		t.Errorf("Truncate(width=1) = %q, want \u2026", got)
	}
}

// TestFallbackGlobFilter verifies the f: / file: flags actually
// prune the result set when running through the fallback path.
func TestFallbackGlobFilter(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "keep.go"),
		[]byte("TODO one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "skip.txt"),
		[]byte("TODO two\n"), 0o644); err != nil {
		t.Fatal(err)
	}

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

// TestFallbackGlobDoubleStar pins doublestar's multi-segment glob
// semantics. The pattern `**/test_*.go` must match test files at any
// depth, including at the root. filepath.Match would silently fail
// the depth > 1 case (its `*` cannot cross `/`).
func TestFallbackGlobDoubleStar(t *testing.T) {
	root := t.TempDir()
	// tree:
	//   test_qux.go                  (root-level match)
	//   internal/foo/bar/test_baz.go (depth-3 match)
	//   internal/keep.go             (negative — same name no test_)
	//   docs/test_skip.md            (negative — wrong extension)
	files := map[string]string{
		"test_qux.go":                 "TODO root\n",
		"internal/foo/bar/test_baz.go": "TODO deep\n",
		"internal/keep.go":            "TODO keep\n",
		"docs/test_skip.md":           "TODO skip\n",
	}
	for rel, content := range files {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

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

// TestFallbackGlobBraceExpansion pins doublestar's `{a,b}` brace
// expansion: a pattern like `**/*.{go,md}` matches either extension
// at any depth. filepath.Match treats the braces as literals.
func TestFallbackGlobBraceExpansion(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"a.go":               "TODO go-root\n",
		"sub/deep/b.go":      "TODO go-deep\n",
		"c.md":               "TODO md-root\n",
		"sub/deep/d.md":      "TODO md-deep\n",
		"e.txt":              "TODO txt\n",
	}
	for rel, content := range files {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

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

// TestFallbackGlobExcludeDoublestar pins doublestar-only multi-segment
// glob negation. The pattern `**/generated/**` removes every file under
// any `generated/` directory at any depth. (We avoid `vendor/`
// here because it's already in codesearch.SkipDirs — the walker
// never reaches its files, so the exclude pattern would be a no-op.)
func TestFallbackGlobExcludeDoublestar(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"keep.go":                              "TODO keep-root\n",
		"sub/keep.go":                          "TODO keep-deep\n",
		"generated/x.go":                       "TODO gen-root\n",
		"sub/generated/y.go":                   "TODO gen-deep\n",
		"sub/deeper/generated/inner/z.go":      "TODO gen-inner\n",
	}
	for rel, content := range files {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

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

// mockRgCmd is the closure the tests pass as Runner.RgBin. It
// substitutes the script path for the rg binary so the runner's
// exec.Cmd spawns the mock, not the real rg.
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

// stringSliceEqual is a tiny helper for test assertions.
func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
