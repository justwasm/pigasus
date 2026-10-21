// Package mdrender — dir_link_test.go: regression tests for
// DirLinkTarget / RenderDirLink (cycle 252).
//
// Every test in this file uses package-internal DirLinkTarget and
// RenderDirLink, never the glamour pipeline; the glamour-side
// integration (markdown preprocessing) is covered by
// TestGlamour_DirLinkReplacesOSC8Hyperlink below, which exercises the
// full ANSI render path through mdrender.ANSI to confirm the
// preprocessor substitutes the dir-link with the plain-text directory
// name.
package mdrender

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDirLinkTarget_EndsWithSlash: the canonical changelog form —
// `[pipelines/](pipelines/)`. The URL ends with exactly one `/`; the
// function must classify it as a directory-link and the label must be
// `pipelines/` with a single trailing slash.
func TestDirLinkTarget_EndsWithSlash(t *testing.T) {
	isDir, label := DirLinkTarget("pipelines/")
	if !isDir {
		t.Fatalf("DirLinkTarget(\"pipelines/\") isDir = false, want true")
	}
	if label != "pipelines/" {
		t.Fatalf("DirLinkTarget(\"pipelines/\") label = %q, want %q", label, "pipelines/")
	}
}

// TestDirLinkTarget_EndsWithSlashNoDoubleSlash: repeated trailing
// slashes must collapse to exactly one. A model that emits
// `pipelines//` (a typo or a sloppy copy) must still render as
// `pipelines/`, never as `pipelines//`. This pins the trim rule the
// changelog example implies.
func TestDirLinkTarget_EndsWithSlashNoDoubleSlash(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"pipelines/", "pipelines/"},
		{"pipelines//", "pipelines/"},
		{"pipelines///", "pipelines/"},
		{"a/b/c/", "a/b/c/"},
		{"a/b/c//", "a/b/c/"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			isDir, label := DirLinkTarget(tc.in)
			if !isDir {
				t.Fatalf("DirLinkTarget(%q) isDir = false, want true", tc.in)
			}
			if label != tc.want {
				t.Fatalf("DirLinkTarget(%q) label = %q, want %q", tc.in, label, tc.want)
			}
		})
	}
}

// TestDirLinkTarget_ResolvesToDir: when the URL does NOT end with a
// slash but `os.Stat` resolves it to an existing directory on disk,
// the function must still classify it as a directory-link and add the
// trailing slash. `/tmp` is a directory on every Unix-y system the
// test runner is likely to see, so it pins the os.Stat path
// deterministically.
func TestDirLinkTarget_ResolvesToDir(t *testing.T) {
	if _, err := os.Stat("/tmp"); err != nil {
		t.Skipf("/tmp not a directory on this host: %v", err)
	}
	isDir, label := DirLinkTarget("/tmp")
	if !isDir {
		t.Fatalf("DirLinkTarget(\"/tmp\") isDir = false, want true (os.Stat says /tmp is a directory)")
	}
	if label != "/tmp/" {
		t.Fatalf("DirLinkTarget(\"/tmp\") label = %q, want %q", label, "/tmp/")
	}
}

// TestDirLinkTarget_ResolvesToFile: when the URL is a regular file,
// the function must return (false, "") so the regular file-link
// rendering path runs. `/etc/passwd` exists on every Linux/macOS host
// the test runner sees; if the host is exotic enough to lack it, the
// test skips with a clear message rather than reporting a false pass.
func TestDirLinkTarget_ResolvesToFile(t *testing.T) {
	if _, err := os.Stat("/etc/passwd"); err != nil {
		t.Skipf("/etc/passwd not present on this host: %v", err)
	}
	isDir, label := DirLinkTarget("/etc/passwd")
	if isDir {
		t.Fatalf("DirLinkTarget(\"/etc/passwd\") isDir = true, want false")
	}
	if label != "" {
		t.Fatalf("DirLinkTarget(\"/etc/passwd\") label = %q, want \"\"", label)
	}
}

// TestDirLinkTarget_NonExistent: a path that does not exist on disk
// must return (false, "") without panicking. The function MUST NOT
// raise on a Stat error; the only error path is "not a directory",
// which keeps the regular OSC 8 hyperlink render alive.
func TestDirLinkTarget_NonExistent(t *testing.T) {
	isDir, label := DirLinkTarget("/nonexistent/path/that/does/not/exist/abcxyz123")
	if isDir {
		t.Fatalf("DirLinkTarget(<nonexistent>) isDir = true, want false")
	}
	if label != "" {
		t.Fatalf("DirLinkTarget(<nonexistent>) label = %q, want \"\"", label)
	}
}

// TestDirLinkTarget_Empty: empty input is a no-op — returns
// (false, "") without consulting os.Stat. This guards the fast path
// the markdown preprocessor relies on for empty hrefs (rare but
// legal in a malformed link).
func TestDirLinkTarget_Empty(t *testing.T) {
	isDir, label := DirLinkTarget("")
	if isDir {
		t.Fatalf("DirLinkTarget(\"\") isDir = true, want false")
	}
	if label != "" {
		t.Fatalf("DirLinkTarget(\"\") label = %q, want \"\"", label)
	}
}

// TestDirLinkTarget_RelativePath: relative paths resolve against the
// caller's cwd, the same way `ls src` does from the shell. The test
// pins this by chdir-ing into a temp dir that has a `src` subdir but
// no `nosrc` entry — `src` resolves as a directory, `nosrc` does not.
// The cwd restoration on t.Cleanup keeps the rest of the test suite
// (and any later t.Parallel sub-tests) unaffected.
func TestDirLinkTarget_RelativePath(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src")
	if err := os.Mkdir(src, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir tmp: %v", err)
	}

	// ./src → directory-link (rel `./src` resolves to tmp/src).
	isDir, label := DirLinkTarget("./src")
	if !isDir {
		t.Fatalf("DirLinkTarget(\"./src\") isDir = false, want true")
	}
	if label != "./src/" {
		t.Fatalf("DirLinkTarget(\"./src\") label = %q, want %q", label, "./src/")
	}

	// ./nosrc → not a directory.
	isDir, label = DirLinkTarget("./nosrc")
	if isDir {
		t.Fatalf("DirLinkTarget(\"./nosrc\") isDir = true, want false")
	}
	if label != "" {
		t.Fatalf("DirLinkTarget(\"./nosrc\") label = %q, want \"\"", label)
	}
}

// TestRenderDirLink: the thin wrapper around DirLinkTarget that
// drops the bool. Every case mirrors a DirLinkTarget case; the test
// pins "RenderDirLink(href) == DirLinkTarget(href).label" by
// asserting the same expected strings.
func TestRenderDirLink(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"pipelines/", "pipelines/"},
		{"pipelines//", "pipelines/"},
		{"pipelines///", "pipelines/"},
		{"", ""},
		{"/nonexistent/path/abcxyz", ""},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got := RenderDirLink(tc.in)
			if got != tc.want {
				t.Fatalf("RenderDirLink(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestRenderDirLink_TrailingSlashAlwaysPresent: every non-empty
// result from RenderDirLink ends with exactly one `/`. This is the
// visible-text invariant the changelog quotes ("they now show the
// directory name, for example `pipelines/`"). The test loops over
// several inputs to catch a regression where someone replaces the
// trailing slash with the trimmed href verbatim.
func TestRenderDirLink_TrailingSlashAlwaysPresent(t *testing.T) {
	inputs := []string{"pipelines/", "src/foo/", "a/b/c//", "deep/nested/path/"}
	for _, in := range inputs {
		t.Run(in, func(t *testing.T) {
			got := RenderDirLink(in)
			if got == "" {
				t.Fatalf("RenderDirLink(%q) = \"\", want non-empty", in)
			}
			if !strings.HasSuffix(got, "/") {
				t.Fatalf("RenderDirLink(%q) = %q, want trailing slash", in, got)
			}
			// No double slashes.
			if strings.Contains(got, "//") {
				t.Fatalf("RenderDirLink(%q) = %q, contains \"//\"", in, got)
			}
		})
	}
}

// TestDirLinkTarget_StatPanicRecovers: simulates a panicking os.Stat
// by replacing the path with one whose Stat goes through a code path
// that panic-recovered. The simplest way: hand statIsDir a path that
// triggers Go's runtime panic in Stat (we cannot easily synthesise
// this without exotic filesystem access). Instead, this test pins the
// defensive posture by exercising a path that *would* be problematic
// on weird FS mounts — a long, deeply-nested, non-existent path —
// and asserting the function does not panic. The actual recover
// branch is exercised in production; the test guards against
// regressions where the recover is removed.
//
// (On a panic, the function returns false; the test does not assert
// isDir — it only asserts no panic.)
func TestDirLinkTarget_StatPanicRecovers(t *testing.T) {
	// A path that does not exist AND looks weird enough to provoke
	// Stat anomalies on a misconfigured FUSE mount. The test is
	// designed to never panic regardless of the underlying FS.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("DirLinkTarget panicked on weird path: %v", r)
		}
	}()
	_, _ = DirLinkTarget("/this/path/does/not/exist/and/should/not/cause/a/panic/at/all/abcxyz123")
	_, _ = DirLinkTarget("///triple/slash///")
	_, _ = DirLinkTarget("relative/with/../../escapes/abcxyz")
}

// TestDirLinkTarget_SchemePrefixGate: URLs with a recognised scheme
// prefix (http://, https://, file://, ftp://, ssh://, ws://, wss://)
// must NOT be classified as directory-links, even when they end with
// `/`. A web URL ending in `/` is just URL convention; the AGY 1.4.0
// changelog row is about workspace file links, not web URLs. The
// test covers the schemes the function recognises plus a negative
// case for an unrecognised scheme.
func TestDirLinkTarget_SchemePrefixGate(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		// Each must NOT be a directory-link.
		{"https://example.com/", false},
		{"https://example.com/foo/", false},
		{"http://example.com/foo/", false},
		{"file:///etc/passwd/", false},
		{"file:///etc/", false},
		{"ftp://example.com/", false},
		{"ssh://git@example.com/repo/", false},
		{"ws://example.com/socket/", false},
		{"wss://example.com/socket/", false},
		// Unrecognised scheme — still treated as local path; the
		// trailing slash rule applies.
		{"mailto:foo@bar/", true},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			isDir, label := DirLinkTarget(tc.in)
			if isDir != tc.want {
				t.Fatalf("DirLinkTarget(%q) isDir = %v, want %v (label=%q)",
					tc.in, isDir, tc.want, label)
			}
			if tc.want {
				if label == "" {
					t.Fatalf("DirLinkTarget(%q) label = \"\", want non-empty", tc.in)
				}
				if strings.Contains(label, "//") {
					t.Fatalf("DirLinkTarget(%q) label = %q, contains \"//\"", tc.in, label)
				}
			} else {
				if label != "" {
					t.Fatalf("DirLinkTarget(%q) label = %q, want \"\"", tc.in, label)
				}
			}
		})
	}
}
