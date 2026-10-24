// inline_test.go — unit tests for inline diff execution and styling.
package diffaction

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/palette"
	"github.com/justwasm/pigasus/internal/tuilib"
)

func TestFirstChangedPath(t *testing.T) {
	for _, tc := range []struct {
		name       string
		tracked    []string
		untracked  []string
		want       string
	}{
		{"tracked first", []string{"a.go", "b.go"}, []string{"c.go"}, "a.go"},
		{"untracked fallback", nil, []string{"c.go"}, "c.go"},
		{"untracked fallback empty tracked", []string{}, []string{"c.go"}, "c.go"},
		{"both empty", nil, nil, ""},
		{"both empty slices", []string{}, []string{}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := FirstChangedPath(tc.tracked, tc.untracked); got != tc.want {
				t.Errorf("FirstChangedPath(%v, %v) = %q, want %q", tc.tracked, tc.untracked, got, tc.want)
			}
		})
	}
}

func TestInlineStyle(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"added", "+added line", palette.ToolAddOpen + "+added line\x1b[39m"},
		{"removed", "-removed line", palette.ToolBulletErrOpen + "-removed line\x1b[39m"},
		{"context", " context line", " context line"},
		{"hunk", "@@ -1,3 +1,4 @@", "@@ -1,3 +1,4 @@"},
		{"diff header", "diff --git a/file b/file", "diff --git a/file b/file"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := InlineStyle(tc.in); got != tc.want {
				t.Errorf("InlineStyle(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestInlineLegend(t *testing.T) {
	got := InlineLegend()
	want := "   " + tuilib.PanelDotHint("", "f", "fold hunk", "esc", "back")
	if got != want {
		t.Errorf("InlineLegend() = %q, want %q", got, want)
	}
	for _, expected := range []string{"f", "fold hunk", "esc", "back"} {
		if !strings.Contains(got, expected) {
			t.Errorf("InlineLegend() missing %q in %q", expected, got)
		}
	}
}

func initTestRepo(t *testing.T) (string, string) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	dir := t.TempDir()
	cmds := [][]string{
		{"init"},
		{"config", "user.name", "Pigasus Test"},
		{"config", "user.email", "test@pigasus.local"},
	}
	for _, args := range cmds {
		if _, err := RunGit(dir, args...); err != nil {
			t.Fatalf("git %s failed: %v", strings.Join(args, " "), err)
		}
	}
	filePath := filepath.Join(dir, "sample.txt")
	if err := os.WriteFile(filePath, []byte("line1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := RunGit(dir, "add", "sample.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := RunGit(dir, "commit", "-m", "init"); err != nil {
		t.Fatal(err)
	}
	hash, err := RunGit(dir, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	return dir, strings.TrimSpace(hash)
}

func TestGitDiffBody_File(t *testing.T) {
	dir, _ := initTestRepo(t)
	filePath := filepath.Join(dir, "sample.txt")
	if err := os.WriteFile(filePath, []byte("line1\nline2   \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lines := GitDiffBody(dir, "sample.txt", "file")
	if len(lines) == 0 {
		t.Fatalf("GitDiffBody(file) returned empty slice, want diff lines")
	}
	var foundAdded bool
	for _, l := range lines {
		if strings.HasPrefix(l, "+line2") {
			foundAdded = true
			if strings.HasSuffix(l, " ") {
				t.Errorf("line %q still has trailing whitespace", l)
			}
		}
	}
	if !foundAdded {
		t.Errorf("GitDiffBody output missing '+line2': %v", lines)
	}
}

func TestGitDiffBody_Commit(t *testing.T) {
	dir, hash := initTestRepo(t)
	lines := GitDiffBody(dir, hash, "commit")
	if len(lines) == 0 {
		t.Fatalf("GitDiffBody(commit) returned empty slice, want commit lines")
	}
	var foundCommit bool
	for _, l := range lines {
		if strings.Contains(l, hash) || strings.Contains(l, "init") {
			foundCommit = true
			break
		}
	}
	if !foundCommit {
		t.Errorf("GitDiffBody(commit) output missing commit info: %v", lines)
	}
}

func TestGitDiffBody_Invalid(t *testing.T) {
	if lines := GitDiffBody(t.TempDir(), "target", "invalid_kind"); lines != nil {
		t.Errorf("GitDiffBody(invalid_kind) = %v, want nil", lines)
	}
	if lines := GitDiffBody("/nonexistent/dir/path", "target", "file"); lines != nil {
		t.Errorf("GitDiffBody(missing dir) = %v, want nil", lines)
	}
}
