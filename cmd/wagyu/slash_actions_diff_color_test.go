package main

// slash_actions_diff_color_test.go — changelog 1.3.4 line 33 regression.
//
// When a Git / Mercurial / Jujutsu repository is configured to always emit
// colour (git: color.ui = always, hg: [ui] color = always, jj:
// ui.color = always), the diff command may still write ANSI CSI sequences
// to stdout even though vcsDiff passes --no-color / HGPLAIN=1 /
// --color=never. Without ANSI stripping, diffFeedback would treat the
// escape bytes as "real" content and report "No uncommitted changes"
// even though no diff body exists, *and* the displayed transcript would
// be polluted with raw escape codes.
//
// These tests pin the empty-case behaviour (with and without colour
// codes), confirm the body is rendered unchanged when there are no
// escapes, and confirm a coloured real diff is recognised as real
// content. vcsDiffFunc is the test seam diffFeedback already exposes.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ansiColoredDiff is a representative `git diff --no-color` output where
// the host repository has color.ui = always set. The repository is clean
// (no changed lines), but the colouring wrapper still emits a reset pair
// around the trailing newline.
const ansiColoredCleanDiff = "\x1b[m\x1b[m"

// ansiColoredDirtyDiff is the same wrapper applied to a one-line change.
// The leading "diff --git" header and the @@ hunk header carry colour
// codes; the +/- lines themselves do not, which mirrors what real Git
// does today (it colours metadata, not content).
const ansiColoredDirtyDiff = "\x1b[1mdiff --git a/foo b/foo\x1b[m\n" +
	"\x1b[1mindex 0000000..1111111 100644\x1b[m\n" +
	"\x1b[1m--- a/foo\x1b[m\n" +
	"\x1b[1m+++ b/foo\x1b[m\n" +
	"@@ -1 +1 @@\n" +
	"-old\n" +
	"+new\n" +
	"\x1b[m\x1b[m"

// withStabVCS overrides vcsDiffFunc for the duration of the test to
// return canned output as if vcs had produced it, and plants the matching
// VCS marker inside dir so detectVCS returns vcs without depending on the
// real git/hg/jj binary.
func withStabVCS(t *testing.T, dir, vcs, canned string, retErr error) {
	t.Helper()
	plantVCSMarker(t, dir, vcs)
	prev := vcsDiffFunc
	vcsDiffFunc = func(v, d string) (string, error) {
		return canned, retErr
	}
	t.Cleanup(func() { vcsDiffFunc = prev })
}

// withStabVCSAt overrides vcsDiffFunc to run the supplied function, and
// plants the matching VCS marker inside dir.
func withStabVCSAt(t *testing.T, dir, vcs string, fn func(actualVcs, d string) (string, error)) {
	t.Helper()
	plantVCSMarker(t, dir, vcs)
	prev := vcsDiffFunc
	vcsDiffFunc = fn
	t.Cleanup(func() { vcsDiffFunc = prev })
}

// plantVCSMarker creates the marker directory detectVCS looks for so the
// caller does not need a real git/hg/jj binary on PATH.
func plantVCSMarker(t *testing.T, dir, vcs string) {
	t.Helper()
	var marker string
	switch vcs {
	case "git":
		marker = ".git"
	case "hg":
		marker = ".hg"
	case "jj":
		marker = ".jj"
	default:
		t.Fatalf("unknown VCS %q", vcs)
	}
	if err := os.MkdirAll(filepath.Join(dir, marker), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestStripANSI(t *testing.T) {
	for _, tc := range []struct {
		name, in, want string
	}{
		{"empty", "", ""},
		{"plain", "abc", "abc"},
		{"sgr-only", "\x1b[m\x1b[m", ""},
		{"single-sgr-around-text", "\x1b[31mfoo\x1b[m", "foo"},
		{"multi-param-sgr", "\x1b[38;5;208;48;5;52mwarn\x1b[0m", "warn"},
		{"questionmark-param", "\x1b[?25hshown\x1b[?25l", "shown"},
		{"mixed-newlines", "\x1b[m\n\x1b[m\n", "\n\n"},
		{"non-sgr-csi", "\x1b[2Jcursor home\x1b[H", "cursor home"},
		{"non-letter-final-not-matched", "\x1b[mz", "z"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := stripANSI(tc.in); got != tc.want {
				t.Errorf("stripANSI(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestDiffFeedback_EmptyWithANSI covers the regression directly: a clean
// repository whose diff command emitted *only* colour reset codes used
// to be reported as having changes because the escape bytes are not
// whitespace. With ANSI stripped, the body is empty and the recovered
// "No uncommitted changes" line is returned.
func TestDiffFeedback_EmptyWithANSI(t *testing.T) {
	dir := t.TempDir()
	withStabVCS(t, dir, "git", ansiColoredCleanDiff, nil)
	if got := diffFeedback(dir, 0); got != "No uncommitted changes" {
		t.Errorf("diffFeedback(colored clean) = %q, want %q", got, "No uncommitted changes")
	}
}

// TestDiffFeedback_EmptyPlain pins the existing behaviour for an empty
// plain diff: no escapes, body empty, recovered empty-case line returned.
func TestDiffFeedback_EmptyPlain(t *testing.T) {
	dir := t.TempDir()
	withStabVCS(t, dir, "git", "", nil)
	if got := diffFeedback(dir, 0); got != "No uncommitted changes" {
		t.Errorf("diffFeedback(empty plain) = %q, want %q", got, "No uncommitted changes")
	}
}

// TestDiffFeedback_PlainDiff covers the unchanged path: a non-coloured
// diff with real content is rendered with the recovered footer and no
// escapes added by the clone.
func TestDiffFeedback_PlainDiff(t *testing.T) {
	plain := "diff --git a/foo b/foo\nindex 0000000..1111111 100644\n" +
		"--- a/foo\n+++ b/foo\n@@ -1 +1 @@\n-old\n+new"
	dir := t.TempDir()
	withStabVCS(t, dir, "git", plain, nil)
	got := diffFeedback(dir, 0)
	if !strings.Contains(got, "-old") || !strings.Contains(got, "+new") {
		t.Errorf("diff body missing +/- lines: %q", got)
	}
	if !strings.Contains(got, "[1-") || !strings.HasSuffix(got, "  (end of diff)") {
		t.Errorf("diff footer missing: %q", got)
	}
	if strings.Contains(got, "\x1b[") {
		t.Errorf("plain diff must not introduce ANSI escapes: %q", got)
	}
}

// TestDiffFeedback_ColoredDiff renders a coloured real diff. The escape
// bytes are stripped from the displayed body so the transcript is clean,
// but the real +/- lines remain and the recovered footer is appended.
func TestDiffFeedback_ColoredDiff(t *testing.T) {
	dir := t.TempDir()
	withStabVCS(t, dir, "git", ansiColoredDirtyDiff, nil)
	got := diffFeedback(dir, 0)
	if strings.Contains(got, "\x1b[") {
		t.Errorf("coloured diff must not leak escape bytes into the transcript: %q", got)
	}
	if !strings.Contains(got, "-old") || !strings.Contains(got, "+new") {
		t.Errorf("coloured diff lost real content after stripping: %q", got)
	}
	if !strings.Contains(got, "diff --git a/foo b/foo") {
		t.Errorf("coloured diff lost its header: %q", got)
	}
	if !strings.HasSuffix(got, "  (end of diff)") {
		t.Errorf("coloured diff footer missing: %q", got)
	}
}

// TestDiffFeedback_EmptyWithANSIAndWhitespace pins the worst-case shape:
// whitespace *and* colour codes, but no real content. The fix must
// recognise this as an empty diff (it would have falsely reported
// changes before the strip).
func TestDiffFeedback_EmptyWithANSIAndWhitespace(t *testing.T) {
	dir := t.TempDir()
	withStabVCS(t, dir, "git", "\n \x1b[m\n\t\x1b[31m\x1b[m\n", nil)
	if got := diffFeedback(dir, 0); got != "No uncommitted changes" {
		t.Errorf("diffFeedback(whitespace+ANSI) = %q, want %q", got, "No uncommitted changes")
	}
}

// TestDiffFeedback_ColoredDiffForAllVCS confirms the fix applies to all
// three VCSs the 1.3.4 changelog names (git, hg, jj). Each VCS uses a
// different diff command and therefore a different position where the
// ANSI codes could leak through; the strip is applied uniformly to the
// body regardless of source.
func TestDiffFeedback_ColoredDiffForAllVCS(t *testing.T) {
	for _, vcs := range []string{"git", "hg", "jj"} {
		vcs := vcs
		t.Run(vcs, func(t *testing.T) {
			dir := t.TempDir()
			withStabVCSAt(t, dir, vcs, func(v, d string) (string, error) {
				if v != vcs {
					t.Errorf("vcsDiff called with %q, want %q", v, vcs)
				}
				if d != dir {
					t.Errorf("vcsDiff called with dir %q, want %q", d, dir)
				}
				return ansiColoredCleanDiff, nil
			})
			if got := diffFeedback(dir, 0); got != "No uncommitted changes" {
				t.Errorf("diffFeedback(%s) = %q, want %q", vcs, got, "No uncommitted changes")
			}
		})
	}
}

// TestDiffFeedback_DiffError keeps the pre-fix error wrapper intact:
// when vcsDiff returns an error, the body is never stripped and the
// recovered "git diff: <err>" line is preserved verbatim.
func TestDiffFeedback_DiffError(t *testing.T) {
	dir := t.TempDir()
	withStabVCS(t, dir, "git", "", errBoom("boom"))
	got := diffFeedback(dir, 0)
	if !strings.HasPrefix(got, "git diff: ") || !strings.Contains(got, "boom") {
		t.Errorf("diffFeedback(error) = %q, want %q prefix", got, "git diff: ")
	}
}

// TestDiffFeedback_NoVCS confirms the AGY 1.3.3 line 52 fallback:
// when no VCS is detected (workspace not under git/hg/jj), REF runs
// `git diff HEAD` rather than emitting the misleading
// "No uncommitted changes". In a workspace that is NOT inside a git
// repo, `git diff HEAD` exits non-zero with `fatal: not a git
// repository`, so the surfaced line is "Not in a git repository".
//
// vcsDiffFunc is never called in this case; this guards against a
// future refactor that mistakenly short-circuits into the VCS path.
func TestDiffFeedback_NoVCS(t *testing.T) {
	called := false
	withStabVCSAt(t, t.TempDir(), "git", func(vcs, dir string) (string, error) {
		called = true
		return "", nil
	})
	if got := diffFeedback(t.TempDir(), 0); got != "Not in a git repository" {
		t.Errorf("diffFeedback(no VCS) = %q, want %q", got, "Not in a git repository")
	}
	if called {
		t.Errorf("vcsDiffFunc must not be called when no VCS is detected")
	}
}

// TestDiffFeedback_GitHeadFallback_RepoNoChanges pins the AGY
// 1.3.3 line-52 happy-path: when the workspace IS inside a git
// repo (detectVCS finds it), the fallback path is not exercised —
// the VCS path runs. This is the inverse of TestDiffFeedback_NoVCS,
// which covers the non-repo case. The two together pin the full
// shape of the AGY 1.3.3 line-52 contract: in-repo → VCS path;
// out-of-repo → "Not in a git repository".
func TestDiffFeedback_GitHeadFallback_RepoNoChanges(t *testing.T) {
	repo := t.TempDir()
	for _, args := range [][]string{
		{"init", "--quiet"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
		{"config", "commit.gpgsign", "false"},
		{"commit", "--allow-empty", "-m", "init", "--quiet"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v (%s)", args, err, out)
		}
	}
	// The temp dir's .git IS visible to detectVCS, so vcsDiffFunc
	// is called and returns "" (no diff). The fallback path is
	// not exercised here; this test pins the in-repo branch.
	prev := vcsDiffFunc
	vcsDiffFunc = func(vcs, dir string) (string, error) { return "", nil }
	t.Cleanup(func() { vcsDiffFunc = prev })
	if got := diffFeedback(repo, 0); got != "No uncommitted changes" {
		t.Errorf("diffFeedback(repo) = %q, want %q", got, "No uncommitted changes")
	}
}

// errBoom is a tiny error type so TestDiffFeedback_DiffError can verify
// the error wrapper without depending on the real exec path.
type errBoom string

func (e errBoom) Error() string { return string(e) }
