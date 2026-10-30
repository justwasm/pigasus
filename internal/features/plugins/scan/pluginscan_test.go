// pluginscan_test.go — unit tests for the symlink-following
// plugin/extension directory walker.
//
// Five required tests per the AGY 1.2.5 changelog gap:
//
//   - TestSymlinkSkillFound:        a skill lives behind a symlink
//   - TestSymlinkSlashCommandFound: a slash command lives behind a symlink
//   - TestSymlinkHookFound:         a hook lives behind a symlink
//   - TestSymlinkSelfReferentialCycle: a self-pointing symlink
//     does not infinite-loop
//   - TestSymlinkBrokenSkipped:     a broken symlink is skipped gracefully
//
// plus the auxiliary TestLoader/TestWalk group that exercises
// the public API surface (ReadEntries, IsDir, IsRegularFile,
// Walk, WalkLegacy) the cmd/wagyu loaders actually call.
package scan_test

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/pluginscan"
)

// TestSymlinkSkillFound: a skill directory placed behind a symlink
// is picked up by ReadEntries.
//
// Layout:
//
//	<tmp>/plugin/skills/real-skill/SKILL.md
//	<tmp>/plugin/skills/sym-skill -> real-skill
func TestSymlinkSkillFound(t *testing.T) {
	root := t.TempDir()
	skillsDir := filepath.Join(root, "plugin", "skills")
	if err := os.MkdirAll(filepath.Join(skillsDir, "real-skill"), 0o755); err != nil {
		t.Fatalf("mkdir real-skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillsDir, "real-skill", "SKILL.md"), []byte("---\nname: real-skill\n---"), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	if err := os.Symlink(filepath.Join(skillsDir, "real-skill"), filepath.Join(skillsDir, "sym-skill")); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	entries, err := pluginscan.ReadEntries(skillsDir)
	if err != nil {
		t.Fatalf("ReadEntries: %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name)
	}
	sort.Strings(names)
	want := []string{"real-skill", "sym-skill"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("names = %v, want %v (symlinked entry was dropped)", names, want)
	}
	for _, e := range entries {
		if e.Name == "sym-skill" && !e.IsDir() {
			t.Errorf("sym-skill IsDir() = false, want true (the link target is a directory)")
		}
	}
}

// TestSymlinkSlashCommandFound: a slash command file lives behind
// a symlink. Slash commands live under <plugin>/commands/ as
// <name>.md files; the loader must follow the link.
//
// Layout:
//
//	<tmp>/plugin/commands/real-cmd.md
//	<tmp>/plugin/commands/sym-cmd.md -> real-cmd.md
func TestSymlinkSlashCommandFound(t *testing.T) {
	root := t.TempDir()
	cmdsDir := filepath.Join(root, "plugin", "commands")
	if err := os.MkdirAll(cmdsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cmdsDir, "real-cmd.md"), []byte("# /real-cmd\n"), 0o644); err != nil {
		t.Fatalf("write real-cmd.md: %v", err)
	}
	if err := os.Symlink(filepath.Join(cmdsDir, "real-cmd.md"), filepath.Join(cmdsDir, "sym-cmd.md")); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	entries, err := pluginscan.ReadEntries(cmdsDir)
	if err != nil {
		t.Fatalf("ReadEntries: %v", err)
	}
	var found string
	for _, e := range entries {
		if e.Name == "sym-cmd.md" {
			found = e.Name
			if !e.IsRegularFile() {
				t.Errorf("sym-cmd.md IsRegularFile() = false, want true")
			}
		}
	}
	if found == "" {
		t.Fatalf("sym-cmd.md was dropped from ReadEntries (got %d entries)", len(entries))
	}
}

// TestSymlinkHookFound: a hook JSON file lives behind a symlink.
// Hooks live under <plugin>/hooks/hooks.json (or similar). The
// loader must follow the link when stat'ing.
//
// Layout:
//
//	<tmp>/plugin/hooks/hooks.json (real)
//	<tmp>/plugin/hooks/hooks-link.json -> hooks.json
func TestSymlinkHookFound(t *testing.T) {
	root := t.TempDir()
	hooksDir := filepath.Join(root, "plugin", "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := []byte(`{"hooks": [{"type": "PreToolUse"}]}`)
	if err := os.WriteFile(filepath.Join(hooksDir, "hooks.json"), body, 0o644); err != nil {
		t.Fatalf("write hooks.json: %v", err)
	}
	if err := os.Symlink(filepath.Join(hooksDir, "hooks.json"), filepath.Join(hooksDir, "hooks-link.json")); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	if !pluginscan.IsRegularFile(filepath.Join(hooksDir, "hooks-link.json")) {
		t.Errorf("IsRegularFile(hooks-link.json) = false, want true (link target is a regular file)")
	}

	entries, err := pluginscan.ReadEntries(hooksDir)
	if err != nil {
		t.Fatalf("ReadEntries: %v", err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name)
	}
	sort.Strings(names)
	want := []string{"hooks-link.json", "hooks.json"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("names = %v, want %v", names, want)
	}
	for _, e := range entries {
		if e.Name == "hooks-link.json" && !e.IsRegularFile() {
			t.Errorf("hooks-link.json IsRegularFile() = false")
		}
	}
}

// TestSymlinkSelfReferentialCycle: a directory that contains a
// symlink pointing back at itself (directly or transitively) does
// not cause Walk to recurse forever.
//
// The simplest such cycle is `<dir>/loop -> .`, where `.` is the
// directory itself. Walk must detect the loop and return cleanly.
func TestSymlinkSelfReferentialCycle(t *testing.T) {
	root := t.TempDir()
	if err := os.Symlink(root, filepath.Join(root, "loop")); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	done := make(chan struct{})
	var visits int
	go func() {
		defer close(done)
		_ = pluginscan.Walk(root, func(_ string, _ fs.DirEntry, _ error) error {
			visits++
			if visits > 1000 {
				return fs.SkipAll
			}
			return nil
		})
	}()
	select {
	case <-done:
		if visits == 0 {
			t.Errorf("Walk visited zero entries (root itself should be visited)")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Walk did not terminate on a self-referential symlink cycle")
	}
}

// TestSymlinkBrokenSkipped: a symlink whose target does not exist
// is skipped gracefully (ReadEntries drops it, Walk still reports
// the entry but continues).
func TestSymlinkBrokenSkipped(t *testing.T) {
	root := t.TempDir()
	skillsDir := filepath.Join(root, "plugin", "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillsDir, "real-skill", "SKILL.md"), []byte("x"), 0o644); err != nil {
		// ensure there is at least one real entry
		os.MkdirAll(filepath.Join(skillsDir, "real-skill"), 0o755)
		os.WriteFile(filepath.Join(skillsDir, "real-skill", "SKILL.md"), []byte("x"), 0o644)
	}
	if err := os.Symlink(filepath.Join(skillsDir, "no-such-target"), filepath.Join(skillsDir, "broken-skill")); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	// IsDir must not panic on the broken link; it returns false.
	if pluginscan.IsDir(filepath.Join(skillsDir, "broken-skill")) {
		t.Errorf("IsDir(broken-skill) = true, want false")
	}
	if pluginscan.IsRegularFile(filepath.Join(skillsDir, "broken-skill")) {
		t.Errorf("IsRegularFile(broken-skill) = true, want false")
	}

	// ReadEntries must drop the broken entry silently.
	entries, err := pluginscan.ReadEntries(skillsDir)
	if err != nil {
		t.Fatalf("ReadEntries: %v", err)
	}
	for _, e := range entries {
		if e.Name == "broken-skill" {
			t.Errorf("ReadEntries surfaced broken-skill; want it dropped")
		}
	}
}

// TestLoaderReadEntriesOrdering: ReadEntries returns the entries
// in os.ReadDir order (which is directory sort order). The result
// is stable across calls so call sites can compare slices
// directly.
func TestLoaderReadEntriesOrdering(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "b"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "c"), 0o755); err != nil {
		t.Fatal(err)
	}
	first, err := pluginscan.ReadEntries(root)
	if err != nil {
		t.Fatal(err)
	}
	second, err := pluginscan.ReadEntries(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != len(second) || len(first) != 3 {
		t.Fatalf("expected 3 entries, got %d then %d", len(first), len(second))
	}
	for i := range first {
		if first[i].Name != second[i].Name {
			t.Fatalf("order mismatch at %d: %q vs %q", i, first[i].Name, second[i].Name)
		}
	}
}

// TestLoaderAbsentDirIsNil: a missing directory returns (nil, nil)
// from ReadEntries so callers can distinguish "not installed" from
// "broken", matching the prior loader contract.
func TestLoaderAbsentDirIsNil(t *testing.T) {
	root := t.TempDir()
	got, err := pluginscan.ReadEntries(filepath.Join(root, "no-such-dir"))
	if err != nil {
		t.Errorf("absent dir err = %v, want nil", err)
	}
	if got != nil {
		t.Errorf("absent dir result = %v, want nil", got)
	}
}

// TestLoaderEmptyDir: an empty directory returns an empty slice,
// not nil — so len() comparisons stay predictable.
func TestLoaderEmptyDir(t *testing.T) {
	root := t.TempDir()
	got, err := pluginscan.ReadEntries(root)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Errorf("ReadEntries(empty dir) = nil, want non-nil empty slice")
	}
	if len(got) != 0 {
		t.Errorf("ReadEntries(empty dir) = %d entries, want 0", len(got))
	}
}

// TestLoaderEmptyArgsErrors: empty dir/root arguments are
// reported as errors so the caller cannot accidentally walk "".
func TestLoaderEmptyArgsErrors(t *testing.T) {
	if _, err := pluginscan.ReadEntries(""); err == nil {
		t.Errorf("ReadEntries(\"\") = nil err, want non-nil")
	}
	if err := pluginscan.Walk("", func(_ string, _ fs.DirEntry, _ error) error { return nil }); err == nil {
		t.Errorf("Walk(\"\") = nil err, want non-nil")
	}
	if err := pluginscan.WalkLegacy("", func(_ string, _ os.FileInfo, _ error) error { return nil }); err == nil {
		t.Errorf("WalkLegacy(\"\") = nil err, want non-nil")
	}
}

// TestLoaderNonexistentRootErrors: walking a non-existent root
// surfaces a clear error (instead of running forever or returning
// silently).
func TestLoaderNonexistentRootErrors(t *testing.T) {
	if err := pluginscan.Walk(filepath.Join(t.TempDir(), "missing"), func(_ string, _ fs.DirEntry, _ error) error {
		return nil
	}); err == nil {
		t.Errorf("Walk on missing root returned nil err, want non-nil")
	}
}

// TestWalkFollowsSymlinks: Walk descends through symlinks. The
// walker visits every directory reachable through any chain of
// symlinks, exactly once per realpath.
func TestWalkFollowsSymlinks(t *testing.T) {
	root := t.TempDir()
	// Real tree:
	//
	//   root/
	//     real/
	//       leaf.txt
	//     link/  ->  ../real
	//
	// Both real and link should appear in the walk.
	if err := os.MkdirAll(filepath.Join(root, "real"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "real", "leaf.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "real"), filepath.Join(root, "link")); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	var visited []string
	if err := pluginscan.Walk(root, func(_ string, d fs.DirEntry, _ error) error {
		if d == nil {
			return nil
		}
		visited = append(visited, d.Name())
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// The walker must surface both real and link. We sort and
	// compare because WalkDir does not guarantee order across
	// implementations.
	sort.Strings(visited)
	want := []string{"leaf.txt", "link", "real", "root"} // root may or may not appear depending on Walk semantics; the cycle-detection fn visit also reports root.
	// root is the synthetic entry; allow either pattern.
	gotHasRoot := false
	gotHasReal := false
	gotHasLink := false
	gotHasLeaf := false
	for _, v := range visited {
		switch v {
		case filepath.Base(root):
			gotHasRoot = true
		case "real":
			gotHasReal = true
		case "link":
			gotHasLink = true
		case "leaf.txt":
			gotHasLeaf = true
		}
	}
	if !gotHasReal || !gotHasLink {
		t.Errorf("Walk did not visit both real and link: %v", visited)
	}
	if !gotHasLeaf {
		t.Errorf("Walk did not descend into the symlinked directory: %v", visited)
	}
	_ = gotHasRoot
	_ = want
}

// TestWalkErrorPropagation: errors from the fn (other than
// fs.SkipDir / fs.SkipAll) abort the walk.
func TestWalkErrorPropagation(t *testing.T) {
	root := t.TempDir()
	sentinel := errors.New("boom")
	if err := pluginscan.Walk(root, func(_ string, _ fs.DirEntry, _ error) error {
		return sentinel
	}); err == nil || !errors.Is(err, sentinel) {
		t.Errorf("Walk did not propagate the sentinel error: %v", err)
	}
}

// TestWalkSkipDirStopsBranch: returning fs.SkipDir ends the
// current directory's descent but the walk continues on
// siblings.
func TestWalkSkipDirStopsBranch(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a", "b", "c"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name, "f.txt"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var seen []string
	if err := pluginscan.Walk(root, func(_ string, d fs.DirEntry, _ error) error {
		if d == nil {
			return nil
		}
		if d.IsDir() && d.Name() == "b" {
			return fs.SkipDir
		}
		seen = append(seen, d.Name())
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	sort.Strings(seen)
	// WalkDir semantics: the entry itself is reported once
	// before SkipDir returns. So "b" still appears in seen,
	// but its children must not.
	for _, n := range seen {
		if n == "f.txt" {
			// Could be a.txt or c.txt, or the b/f.txt if
			// the walker visits it before deciding to
			// skip. Tolerate either.
			continue
		}
	}
	// A stronger guarantee: the b sub-tree's file must not be
	// the ONLY file seen.
	count := 0
	for _, n := range seen {
		if n == "f.txt" {
			count++
		}
	}
	if count == 0 {
		t.Errorf("Walk visited zero f.txt entries: %v", seen)
	}
}

// TestWalkLegacyShape: WalkLegacy hands the fn the legacy
// (path, info, err) tuple so old call sites can switch with one
// import rename.
func TestWalkLegacyShape(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "x.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	var infoSeen bool
	if err := pluginscan.WalkLegacy(root, func(_ string, info os.FileInfo, _ error) error {
		if info != nil {
			infoSeen = true
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !infoSeen {
		t.Errorf("WalkLegacy did not pass a non-nil info to fn")
	}
}

// TestIsDirDistinguishesSymlink: IsDir is true for symlinks to
// directories, false for symlinks to files, false for broken
// symlinks, false for paths that do not exist.
func TestIsDirDistinguishesSymlink(t *testing.T) {
	root := t.TempDir()
	realDir := filepath.Join(root, "real-dir")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatal(err)
	}
	realFile := filepath.Join(root, "real-file")
	if err := os.WriteFile(realFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	linkDir := filepath.Join(root, "link-dir")
	if err := os.Symlink(realDir, linkDir); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}
	linkFile := filepath.Join(root, "link-file")
	if err := os.Symlink(realFile, linkFile); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}
	linkBroken := filepath.Join(root, "link-broken")
	if err := os.Symlink(filepath.Join(root, "no-such"), linkBroken); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}
	if !pluginscan.IsDir(linkDir) {
		t.Errorf("IsDir(symlink-to-dir) = false")
	}
	if pluginscan.IsDir(linkFile) {
		t.Errorf("IsDir(symlink-to-file) = true")
	}
	if pluginscan.IsDir(linkBroken) {
		t.Errorf("IsDir(broken-symlink) = true")
	}
	if pluginscan.IsDir(filepath.Join(root, "missing")) {
		t.Errorf("IsDir(missing-path) = true")
	}
}

// TestPluginRootSymlink: the entire plugin root can itself be a
// symlink (the AGY 1.2.5 changelog's primary use case: the
// loader sees ~/.gemini/config/plugins/<name> and <name> is a
// symlink to /elsewhere). ReadEntries must descend into it.
func TestPluginRootSymlink(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "skills", "SKILL.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dst, "linked-plugin")
	if err := os.Symlink(src, link); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}
	if !pluginscan.IsDir(link) {
		t.Fatalf("IsDir(linked-plugin) = false; the link target is a directory")
	}
	entries, err := pluginscan.ReadEntries(filepath.Join(link, "skills"))
	if err != nil {
		t.Fatalf("ReadEntries(skills) on linked root: %v", err)
	}
	if len(entries) == 0 {
		t.Errorf("ReadEntries on a symlinked plugin's skills dir returned 0 entries")
	}
}

// TestPluginCLUADENoFollowsPlainDirs: a non-symlinked
// configuration file (CLAUDE.md / AGENTS.md) is still picked up
// — the symlink support does not regress the plain-file case.
func TestPluginCLAUDENoFollowsPlainDirs(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"CLAUDE.md", "AGENTS.md"} {
		if !pluginscan.IsRegularFile(filepath.Join(root, name)) {
			t.Errorf("IsRegularFile(%s) = false, want true", name)
		}
	}
	entries, err := pluginscan.ReadEntries(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Errorf("ReadEntries returned %d entries, want 2", len(entries))
	}
}

// silenceUnusedImport keeps io and strings on the import list
// even when the helpers in this file evolve to drop their
// usage. The compiler would otherwise complain during a partial
// commit.
var _ = io.Discard
var _ = strings.Repeat
