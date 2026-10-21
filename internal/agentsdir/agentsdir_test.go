// Package agentsdir — cycle agents-parent-dir-walk: AGY 1.2.16 changelog
// line 126 and the gap text close the parent-directory walk that surfaces
// .agents/ directories between CWD and HOME.
//
// The tests below pin the six rules called out in the gap and the
// capture-derived invariants.
//
// Each test builds a small on-disk tree rooted at a t.TempDir-shaped
// "HOME" and an explicit CWD inside that tree, so os.Stat / filepath.Walk
// operate on real directories. The HOME boundary is enforced by
// directory containment only — Walk never walks above stopDir.
package agentsdir_test

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/agentsdir"
)

// fixture creates a sandbox HOME at t.TempDir() and a sandbox CWD
// nested four levels deep. A flat list `withAgents` declares, for each
// level from CWD up to and including HOME, what directory names should
// exist there.
//
//	withAgents[i] == true   ⇒   HOME/<chain[0..i]>/<dirNames...> exist
//
// Index i counts from HOME toward CWD; that is, the LAST element of
// withAgents corresponds to the deepest directory in the CWD chain.
//
// chain is the directory-name chain below HOME; withAgents has length
// len(chain)+1 so the HOME entry sits at index 0 and the deepest CWD
// at index len(chain).
func fixture(t *testing.T, withAgents []bool, chain []string, dirNames []string) (cwd, home string) {
	t.Helper()
	home = t.TempDir()
	cur := home
	if len(withAgents) == 0 {
		return home, home
	}
	if len(withAgents) < len(chain)+1 {
		missing := len(chain) + 1 - len(withAgents)
		withAgents = append(append(make([]bool, 0, len(withAgents)+missing), withAgents...), make([]bool, missing)...)
	}
	// Index 0 → HOME itself.
	if withAgents[0] {
		for _, n := range dirNames {
			mustMkdir(t, filepath.Join(home, n))
		}
	}
	for i, seg := range chain {
		cur = filepath.Join(cur, seg)
		mustMkdir(t, cur)
		// Index i+1 → this level in the chain.
		if withAgents[i+1] {
			for _, n := range dirNames {
				mustMkdir(t, filepath.Join(cur, n))
			}
		}
	}
	return cur, home
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func paths(es []agentsdir.Entry) string {
	parts := make([]string, len(es))
	for i, e := range es {
		parts[i] = e.Path
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// fixtureCwd is a thin wrapper around fixture that returns only HOME —
// it discards the CWD so tests that do not exercise the parent walk
// (only the manifest loader) do not have to declare an unused variable.
func fixtureCwd(t *testing.T, withAgents []bool, chain []string, dirNames []string) string {
	t.Helper()
	_, home := fixture(t, withAgents, chain, dirNames)
	return home
}

// TestWalk_LocalAgentsWinsOverHomeAgents covers the gap rule "project-local
// agents/ wins over the global one": HOME has agents/, deeper level HOME/a
// has agents/, and CWD is HOME/a/b/c/d — the project-local entry must
// surface at index 0.
func TestWalk_LocalAgentsWinsOverHomeAgents(t *testing.T) {
	// Chain is a, b, c, d. withAgents indices: [0]=HOME, [1]=HOME/a,
	// [2]=HOME/a/b, [3]=HOME/a/b/c, [4]=HOME/a/b/c/d.
	cwd, home := fixture(t, []bool{true, true, false, false, false}, []string{"a", "b", "c", "d"}, []string{"agents"})

	got := agentsdir.Walk(cwd, home, agentsdir.DefaultDirNames)
	if len(got) != 2 {
		t.Fatalf("walk: want 2 entries (HOME + HOME/a); got %d: %s", len(got), paths(got))
	}
	wantFirst := filepath.Join(home, "a", "agents")
	if got[0].Path != wantFirst {
		t.Fatalf("walk[0]: want CWD-closest %s, got %s", wantFirst, got[0].Path)
	}
	if got[0].Depth != 3 {
		t.Fatalf("walk[0].Depth: want 3 (HOME→a→b→c→d, d..a is 3 hops), got %d", got[0].Depth)
	}
	wantSecond := filepath.Join(home, "agents")
	if got[1].Path != wantSecond {
		t.Fatalf("walk[1]: want HOME-level %s, got %s", wantSecond, got[1].Path)
	}
	if got[1].Depth != 4 {
		t.Fatalf("walk[1].Depth: want 4 (full chain), got %d", got[1].Depth)
	}
}

// TestWalk_OnlyHomeFallsBackWhenNoLocal covers the "walk stops at HOME,
// so when the CWD chain has no agents/ the walker surfaces the HOME
// entry" rule.
func TestWalk_OnlyHomeFallsBackWhenNoLocal(t *testing.T) {
	cwd, home := fixture(t, []bool{true, false, false, false, false}, []string{"a", "b", "c", "d"}, []string{"agents"})

	got := agentsdir.Walk(cwd, home, agentsdir.DefaultDirNames)
	if len(got) != 1 {
		t.Fatalf("walk: want 1 entry (HOME only); got %d: %s", len(got), paths(got))
	}
	if got[0].Path != filepath.Join(home, "agents") {
		t.Fatalf("walk[0]: want HOME-level, got %s", got[0].Path)
	}
	if got[0].Depth != 4 {
		t.Fatalf("walk[0].Depth: want 4, got %d", got[0].Depth)
	}
}

// TestWalk_StopsAtHome pins the upper-bound: even when an ancestor above
// HOME also has agents/, it must not show up.
func TestWalk_StopsAtHome(t *testing.T) {
	cwd, home := fixture(t, []bool{false, true, false, false, false}, []string{"a", "b", "c", "d"}, []string{"agents"})

	// Plant an agents/ directory above HOME; the walk must not surface it.
	above := filepath.Join(filepath.Dir(home), "above-home")
	mustMkdir(t, filepath.Join(above, "agents"))
	t.Cleanup(func() { _ = os.RemoveAll(above) })

	got := agentsdir.Walk(cwd, home, agentsdir.DefaultDirNames)
	for _, e := range got {
		if strings.HasPrefix(e.Path, above) {
			t.Fatalf("walk crossed HOME boundary: %s is not within %s", e.Path, home)
		}
	}
	if len(got) != 1 {
		t.Fatalf("walk: want 1 entry (HOME/a only); got %d: %s", len(got), paths(got))
	}
	if got[0].Path != filepath.Join(home, "a", "agents") {
		t.Fatalf("walk[0]: want %s, got %s", filepath.Join(home, "a", "agents"), got[0].Path)
	}
}

// TestWalk_DeeplyNestedCWDWalksAllLevels covers a 4-deep CWD where every
// intermediate directory has its own agents/. The walker must produce
// one entry per level (4 entries), in CWD-closest order.
func TestWalk_DeeplyNestedCWDWalksAllLevels(t *testing.T) {
	cwd, home := fixture(t, []bool{false /*HOME*/, true, true, true, true /*d*/},
		[]string{"a", "b", "c", "d"}, []string{"agents"})

	got := agentsdir.Walk(cwd, home, agentsdir.DefaultDirNames)
	wantN := 4
	if len(got) != wantN {
		t.Fatalf("walk: want %d entries (one per level), got %d: %s", wantN, len(got), paths(got))
	}
	want := []string{
		filepath.Join(home, "a", "b", "c", "d", "agents"),
		filepath.Join(home, "a", "b", "c", "agents"),
		filepath.Join(home, "a", "b", "agents"),
		filepath.Join(home, "a", "agents"),
	}
	for i, w := range want {
		if got[i].Path != w {
			t.Fatalf("walk[%d]: want %s, got %s", i, w, got[i].Path)
		}
		if got[i].Depth != i {
			t.Fatalf("walk[%d].Depth: want %d, got %d", i, i, got[i].Depth)
		}
	}
}

// TestWalk_DedupesByAbsolutePath pins the dedupe table: a symlinked
// ancestor that resolves to the same path as an already-seen directory
// must not produce a duplicate Entry.
func TestWalk_DedupesByAbsolutePath(t *testing.T) {
	cwd, home := fixture(t, []bool{true, false, false, false, false},
		[]string{"a", "b", "c", "d"}, []string{"agents"})

	// Replace the chain at HOME/a with a symlink to a separate dir that
	// also has an agents/ subdirectory; the symlink resolution yields
	// the same absolute path as the walked entries.
	other := t.TempDir()
	mustMkdir(t, filepath.Join(other, "agents"))

	chain := filepath.Join(home, "a")
	if err := os.RemoveAll(chain); err != nil {
		t.Fatalf("rm %s: %v", chain, err)
	}
	if err := os.Symlink(other, chain); err != nil {
		t.Skipf("symlink unsupported on %s: %v", runtime.GOOS, err)
	}

	got := agentsdir.Walk(cwd, home, agentsdir.DefaultDirNames)
	dup := map[string]int{}
	for _, e := range got {
		dup[e.Path]++
	}
	for p, n := range dup {
		if n > 1 {
			t.Fatalf("walk produced duplicate entry for %s (%d times)", p, n)
		}
	}
}

// TestWalk_EmptyResultWhenNoAgentsAnywhere pins the no-result case:
// when no ancestor has either directory, Walk returns an empty
// (non-nil) slice.
func TestWalk_EmptyResultWhenNoAgentsAnywhere(t *testing.T) {
	cwd, home := fixture(t, []bool{false, false, false, false, false},
		[]string{"a", "b", "c", "d"}, []string{"agents"})

	got := agentsdir.Walk(cwd, home, agentsdir.DefaultDirNames)
	if got == nil {
		t.Fatalf("walk: want empty non-nil slice, got nil")
	}
	if len(got) != 0 {
		t.Fatalf("walk: want 0 entries, got %d: %s", len(got), paths(got))
	}
}

// TestWalk_DefaultNamesCoverAgentsAndDotAgents exercises the
// DefaultDirNames list — only `.agents` exists at one level (HOME/a/b),
// and Walk finds it because the dotted name is part of the default
// pair.
func TestWalk_DefaultNamesCoverAgentsAndDotAgents(t *testing.T) {
	cwd, home := fixture(t, []bool{false, false, true /*b*/, false, false},
		[]string{"a", "b", "c", "d"}, []string{".agents"})

	got := agentsdir.Walk(cwd, home, agentsdir.DefaultDirNames)
	want := filepath.Join(home, "a", "b", ".agents")
	if len(got) != 1 {
		t.Fatalf("walk: want 1 entry, got %d: %s", len(got), paths(got))
	}
	if got[0].Path != want {
		t.Fatalf("walk[0]: want %s, got %s", want, got[0].Path)
	}
}

// TestDiscover_HomeIsTheGlobalRoot pins the "global ~/.agents/ is just
// HOME/.agents" half of the gap text: when no project-local agents/
// exists, the HOME-level agents/ surfaces as the only entry, with depth
// equal to the full chain length.
func TestDiscover_HomeIsTheGlobalRoot(t *testing.T) {
	cwd, home := fixture(t, []bool{false, false, false, false, false},
		[]string{"a", "b", "c", "d"}, []string{"agents"})
	// Plant the canonical "global" ~/.agents/.
	mustMkdir(t, filepath.Join(home, "agents"))

	got := agentsdir.Discover(cwd, home)
	if len(got) != 1 {
		t.Fatalf("discover: want 1 entry (HOME level), got %d: %s", len(got), paths(got))
	}
	if got[0].Path != filepath.Join(home, "agents") {
		t.Fatalf("discover[0]: want HOME-level, got %s", got[0].Path)
	}
	if got[0].Depth != 4 {
		t.Fatalf("discover[0].Depth: want 4 (full chain), got %d", got[0].Depth)
	}
}

// TestDiscover_ProjectLocalWinsOverGlobal pins the "project-local wins"
// rule explicitly: when both a project-local and HOME-level agents/
// exist, the project-local entry is at index 0.
func TestDiscover_ProjectLocalWinsOverGlobal(t *testing.T) {
	cwd, home := fixture(t, []bool{true /*HOME*/, true /*a*/, false, false, false},
		[]string{"a", "b", "c", "d"}, []string{"agents"})

	got := agentsdir.Discover(cwd, home)
	if len(got) != 2 {
		t.Fatalf("discover: want 2 entries (project-local + HOME), got %d: %s", len(got), paths(got))
	}
	if got[0].Path != filepath.Join(home, "a", "agents") {
		t.Fatalf("discover[0]: want project-local, got %s", got[0].Path)
	}
	if got[1].Path != filepath.Join(home, "agents") {
		t.Fatalf("discover[1]: want HOME-global, got %s", got[1].Path)
	}
}

// TestFirstProjectLocal_ReturnsClosestEntry pins the convenience helper.
func TestFirstProjectLocal_ReturnsClosestEntry(t *testing.T) {
	cwd, home := fixture(t, []bool{false, true, false, false, false},
		[]string{"a", "b", "c", "d"}, []string{"agents"})

	got, ok := agentsdir.FirstProjectLocal(cwd, home)
	if !ok {
		t.Fatalf("firstProjectLocal: want ok=true")
	}
	if got.Path != filepath.Join(home, "a", "agents") {
		t.Fatalf("firstProjectLocal.Path: want %s, got %s", filepath.Join(home, "a", "agents"), got.Path)
	}
}

// TestFirstProjectLocal_NoResultReturnsFalse pins the empty-result case
// of the convenience helper.
func TestFirstProjectLocal_NoResultReturnsFalse(t *testing.T) {
	cwd, home := fixture(t, []bool{false, false, false, false, false},
		[]string{"a", "b", "c", "d"}, []string{"agents"})

	got, ok := agentsdir.FirstProjectLocal(cwd, home)
	if ok {
		t.Fatalf("firstProjectLocal: want ok=false, got %+v", got)
	}
	if got.Path != "" {
		t.Fatalf("firstProjectLocal.Path: want empty, got %s", got.Path)
	}
}

// TestWalk_DotAgentsOnlyAtHome pins that the dotted `.agents` form is
// discovered even when only HOME has it (the CWD chain is bare).
func TestWalk_DotAgentsOnlyAtHome(t *testing.T) {
	cwd, home := fixture(t, []bool{false, false, false, false, false},
		[]string{"a", "b", "c", "d"}, []string{".agents"})
	mustMkdir(t, filepath.Join(home, ".agents"))

	got := agentsdir.Walk(cwd, home, agentsdir.DefaultDirNames)
	if len(got) != 1 {
		t.Fatalf("walk: want 1 entry (HOME-level .agents), got %d: %s", len(got), paths(got))
	}
	if got[0].Path != filepath.Join(home, ".agents") {
		t.Fatalf("walk[0]: want HOME-level .agents, got %s", got[0].Path)
	}
}

// TestWalk_RespectsCustomDirNames pins that callers can override the
// default names with their own list (the AGY surface exposes a fixed
// set, but the helper supports customisation for plugin-level scans).
func TestWalk_RespectsCustomDirNames(t *testing.T) {
	cwd, home := fixture(t, []bool{false, false, false, false, false},
		[]string{"a", "b", "c", "d"}, nil)
	mustMkdir(t, filepath.Join(home, "my-agents"))

	got := agentsdir.Walk(cwd, home, []string{"my-agents"})
	if len(got) != 1 {
		t.Fatalf("walk: want 1 entry (custom my-agents), got %d: %s", len(got), paths(got))
	}
	if got[0].Path != filepath.Join(home, "my-agents") {
		t.Fatalf("walk[0]: want %s, got %s", filepath.Join(home, "my-agents"), got[0].Path)
	}
}

// TestWalk_SymlinkLoopDoesNotPanic pins robustness: a symlink that
// loops back to HOME must not crash the walker.
func TestWalk_SymlinkLoopDoesNotPanic(t *testing.T) {
	cwd, home := fixture(t, []bool{false, false, false, false, false},
		[]string{"a", "b", "c", "d"}, nil)

	// Plant a symlink at HOME that points back to HOME.
	if err := os.Symlink(home, filepath.Join(home, "loop")); err != nil {
		t.Skipf("symlink unsupported on %s: %v", runtime.GOOS, err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Join(home, "loop")) })

	got := agentsdir.Walk(cwd, home, agentsdir.DefaultDirNames)
	if got == nil {
		t.Fatalf("walk: want empty non-nil slice, got nil")
	}
	if len(got) != 0 {
		t.Fatalf("walk: want 0 entries, got %d: %s", len(got), paths(got))
	}
}

// TestWalk_DirNamesSanitised pins input hygiene: empty strings or
// path-separator names are dropped silently rather than reaching
// filepath.Join with a malformed argument.
func TestWalk_DirNamesSanitised(t *testing.T) {
	cwd, home := fixture(t, []bool{false, false, false, false, false},
		[]string{"a", "b", "c", "d"}, []string{"agents"})
	mustMkdir(t, filepath.Join(home, "agents"))

	got := agentsdir.Walk(cwd, home, []string{"", string(filepath.Separator) + "etc", "agents"})
	if len(got) != 1 {
		t.Fatalf("walk: want 1 entry (HOME agents only); got %d: %s", len(got), paths(got))
	}
	if got[0].Path != filepath.Join(home, "agents") {
		t.Fatalf("walk[0]: want %s, got %s", filepath.Join(home, "agents"), got[0].Path)
	}
}

// TestDiscover_BothAgentsAndDotAtSameLevel pins that when BOTH
// `agents` and `.agents` exist at the same level (HOME here), Walk
// surfaces both — DefaultDirNames order (`agents` first, then
// `.agents`) governs precedence, and the same-depth entries are
// CWD-closest first within their depth.
//
// Both entries are returned at index 0/1 because they were found while
// the walker was inspecting `cur == home`; the walker visits
// `DefaultDirNames` in order at each step, so `agents` is yielded before
// `.agents` at the same depth.
func TestDiscover_BothAgentsAndDotAtSameLevel(t *testing.T) {
	cwd, home := fixture(t, []bool{false, false, false, false, false},
		[]string{"a", "b", "c", "d"}, nil)
	mustMkdir(t, filepath.Join(home, "agents"))
	mustMkdir(t, filepath.Join(home, ".agents"))

	got := agentsdir.Discover(cwd, home)
	if len(got) != 2 {
		t.Fatalf("discover: want 2 entries, got %d: %s", len(got), paths(got))
	}
	if got[0].Path != filepath.Join(home, "agents") {
		t.Fatalf("discover[0]: want undotted first, got %s", got[0].Path)
	}
	if got[1].Path != filepath.Join(home, ".agents") {
		t.Fatalf("discover[1]: want dotted second, got %s", got[1].Path)
	}
}

// TestWalk_DepthsAscending pins that the depth field is monotonically
// non-decreasing with the path distance from CWD.
func TestWalk_DepthsAscending(t *testing.T) {
	cwd, home := fixture(t, []bool{false, true, false, true, false},
		[]string{"a", "b", "c", "d"}, []string{"agents"})

	got := agentsdir.Walk(cwd, home, agentsdir.DefaultDirNames)
	depths := make([]int, len(got))
	for i, e := range got {
		depths[i] = e.Depth
	}
	sorted := append([]int{}, depths...)
	sort.Ints(sorted)
	for i := range depths {
		if depths[i] != sorted[i] {
			t.Fatalf("walk: depths not sorted ascending: got %v, want %v", depths, sorted)
		}
	}
}
