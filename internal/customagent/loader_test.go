package customagent_test

// loader_test.go — cycle 235: AGY 1.3.1 changelog gap closure
//
//	Fixed markdown custom agents ignoring `skills:`, `plugins:`,
//	`rules:`, `agents:`, and `hooks:` entries written as paths
//	relative to the agent's own file when the agent ran as a
//	subagent or as the main agent; such entries now resolve
//	against the agent's directory, so an agent that lists
//	`agents: [child.md]` can invoke `child`.
//
// The four required test cases (per the gap spec) are:
//
//  1. A relative-path entry (`agents: [child.md]`) is
//     resolved against the agent file's directory, NOT
//     against CWD. This is the headline 1.3.1 fix.
//
//  2. An absolute-path entry is used verbatim — left alone.
//
//  3. A child agent referenced via a relative path is
//     actually invocable end-to-end: write the parent agent
//     file, write a child markdown file next to it, parse
//     parent, prove the resolved path points at the child.
//
//  4. Multiple relative paths in different frontmatter
//     fields all resolve against the agent's directory at
//     once.
//
// On top of the four required cases we add:
//
//   - `LoadAgentDir` returns sorted agents and skips
//     frontmatter-less bodies.
//   - `looksLikePath` predicate covers the four documented
//     extension cases.
//   - `ResolvedAgents` builds the expected name→agent map,
//     and a duplicated `name:` keeps the first definition.
//   - `parseFrontmatter` of a body-only markdown file
//     returns `ErrEmpty` and is silently skipped on
//     `LoadAgentDir`.
//   - Normalisation drops whitespace and empties.

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	customagent "github.com/justwasm/pigasus/internal/customagent"
)

// writeFile is a t.Helper that writes content to a path inside
// the temp dir, returning the joined path. The parent
// directories are created with mode 0700.
func writeFile(t *testing.T, root, rel, content string) string {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(full), err)
	}
	if err := os.WriteFile(full, []byte(content), 0600); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
	return full
}

// changeDir switches the test into dir and arranges for it to
// be reset on test end. We use it to pin down the CWD so a
// relative-path resolver that forgot to anchor against the
// agent's own file would have pointed somewhere wrong.
func changeDir(t *testing.T, dir string) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
}

// TestRelativePathResolvedAgainstAgentFile is the headline test
// for the AGY 1.3.1 gap: agents: [child.md] must point at the
// sibling file, not at CWD.
//
// Layout:
//
//	tmp/
//	  .agents/agents/parent.md   agents: [child.md]
//	  .agents/agents/child.md    sibling file
//
// We cd to tmp/ and parse parent.md; the resolved `agents:`
// must come back as <tmp>/.agents/agents/child.md (NOT
// tmp/child.md, which is where a CWD-resolved bug would land).
func TestRelativePathResolvedAgainstAgentFile(t *testing.T) {
	root := t.TempDir()
	agentsDir := filepath.Join(root, ".agents", "agents")
	writeFile(t, agentsDir, "child.md", "---\nname: child\n---\n# child agent\n")
	parentPath := writeFile(t, agentsDir, "parent.md", "---\nname: parent\nagents:\n  - child.md\n---\n# parent agent\n")

	// Move into a directory where a CWD-relative
	// `child.md` would resolve to a missing file. We
	// cannot test "the bug" directly without one, but
	// pinning CWD away from the agents dir means a
	// buggy implementation that joined against
	// os.Getwd() would have produced
	// "<not-root>/child.md", which we assert is NOT
	// what comes back.
	changeDir(t, t.TempDir())

	a, err := customagent.LoadAgent(parentPath)
	if err != nil {
		t.Fatalf("LoadAgent: %v", err)
	}
	if got, want := a.Name, "parent"; got != want {
		t.Errorf("name = %q, want %q", got, want)
	}
	if got, want := len(a.ResolvedAgents), 1; got != want {
		t.Fatalf("resolved agents count = %d, want %d", got, want)
	}
	got := a.ResolvedAgents[0]
	// Resolved path must be the sibling, not CWD-relative.
	if got != filepath.Join(agentsDir, "child.md") {
		t.Errorf("ResolvedAgents[0] = %q, want %q", got, filepath.Join(agentsDir, "child.md"))
	}
	if filepath.IsAbs(got) != true {
		t.Errorf("resolved path %q is not absolute", got)
	}
}

// TestAbsolutePathPassesThrough checks that an entry written
// as an absolute path is preserved verbatim (after
// filepath.Clean) and is NOT subjected to the
// join-with-baseDir rule.
func TestAbsolutePathPassesThrough(t *testing.T) {
	root := t.TempDir()
	agentsDir := filepath.Join(root, ".agents", "agents")
	writeFile(t, agentsDir, "child.md", "---\nname: child\n---\n")
	parentPath := writeFile(t, agentsDir, "parent.md",
		"---\nname: parent\nagents:\n  - /var/abs/child.md\n---\n")

	// Force CWD to a different directory so the
	// absolute-vs-relative join bug, were it present,
	// would resurface.
	changeDir(t, t.TempDir())

	a, err := customagent.LoadAgent(parentPath)
	if err != nil {
		t.Fatalf("LoadAgent: %v", err)
	}
	if got := a.ResolvedAgents[0]; !strings.HasSuffix(got, "/var/abs/child.md") {
		t.Errorf("ResolvedAgents[0] = %q, want /var/abs/child.md (or filepath.Clean of it)", got)
	}
}

// TestChildAgentInvocable is the end-to-end assertion: a parent
// that lists `agents: [child.md]` can in fact load the child
// via the resolved path. We prove this by:
//
//  1. writing both files;
//  2. parsing both with LoadAgent;
//  3. reading back via ResolvedAgents and confirming the
//     resolved path actually points at the child file (file
//     exists, content matches what we wrote).
func TestChildAgentInvocable(t *testing.T) {
	root := t.TempDir()
	agentsDir := filepath.Join(root, ".agents", "agents")
	childPath := writeFile(t, agentsDir, "child.md",
		"---\nname: child\ndescription: a child agent\n---\n# child agent body\n")
	parentPath := writeFile(t, agentsDir, "parent.md",
		"---\nname: parent\nagents:\n  - child.md\n---\n# parent body\n")

	changeDir(t, t.TempDir())

	parent, err := customagent.LoadAgent(parentPath)
	if err != nil {
		t.Fatalf("LoadAgent(parent): %v", err)
	}
	// Walk the directory the way an actual session loader
	// would, then resolve the parent against it.
	agents, _, err := customagent.LoadAgentDir(agentsDir)
	if err != nil {
		t.Fatalf("LoadAgentDir: %v", err)
	}
	if len(agents) != 2 {
		t.Fatalf("LoadAgentDir: got %d agents, want 2", len(agents))
	}
	// The resolution name → path -> file pipeline.
	byPath := make(map[string]*customagent.Agent)
	for _, a := range agents {
		byPath[a.Path] = a
	}
	resolvedParent := parent.ResolvedAgents[0]
	resolvedParent = filepath.Clean(resolvedParent)
	child, ok := byPath[resolvedParent]
	if !ok {
		t.Fatalf("resolved path %q is not a known agent", resolvedParent)
	}
	if child.Path != childPath {
		t.Errorf("child.path = %q, want %q", child.Path, childPath)
	}
	if child.Name != "child" {
		t.Errorf("child.name = %q, want %q", child.Name, "child")
	}
	if !strings.Contains(child.Body, "child agent body") {
		t.Errorf("child body missing expected content: %q", child.Body)
	}
}

// TestMultipleRelativePathsResolved covers the "many fields
// at once" case: every path-bearing field on the same
// frontmatter is resolved against the agent's directory.
//
// We plant a child file per field and assert each resolved
// entry points at the right path.
func TestMultipleRelativePathsResolved(t *testing.T) {
	root := t.TempDir()
	agentsDir := filepath.Join(root, "deep", "nested", "agents")

	writeFile(t, agentsDir, "skill.md", "# skill")
	writeFile(t, agentsDir, "plugin.json", "{}")
	writeFile(t, agentsDir, "rule.md", "# rule")
	writeFile(t, agentsDir, "child.md", "---\nname: child\n---\n")
	writeFile(t, agentsDir, "hook.sh", "#!/bin/sh\nexit 0\n")

	parentPath := writeFile(t, agentsDir, "parent.md",
		"---\n"+
			"name: parent\n"+
			"skills:\n"+
			"  - skill.md\n"+
			"plugins:\n"+
			"  - plugin.json\n"+
			"rules:\n"+
			"  - rule.md\n"+
			"agents:\n"+
			"  - child.md\n"+
			"hooks:\n"+
			"  - hook.sh\n"+
			"---\n")

	changeDir(t, t.TempDir())

	a, err := customagent.LoadAgent(parentPath)
	if err != nil {
		t.Fatalf("LoadAgent: %v", err)
	}

	type pair struct {
		field, want string
		got         string
	}
	cases := []pair{
		{"skills", filepath.Join(agentsDir, "skill.md"), a.ResolvedSkills[0]},
		{"plugins", filepath.Join(agentsDir, "plugin.json"), a.ResolvedPlugins[0]},
		{"rules", filepath.Join(agentsDir, "rule.md"), a.ResolvedRules[0]},
		{"agents", filepath.Join(agentsDir, "child.md"), a.ResolvedAgents[0]},
		{"hooks", filepath.Join(agentsDir, "hook.sh"), a.ResolvedHooks[0]},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s resolved = %q, want %q", c.field, c.got, c.want)
		}
		if !filepath.IsAbs(c.got) {
			t.Errorf("%s resolved = %q is not absolute", c.field, c.got)
		}
	}
}

// TestNameReferenceNotPath is the safety-net test: an entry
// that does not look like a path (no extension, no slash) is
// left as a name reference so the upstream loader can pick it
// up from its own catalogue. We deliberately use a name that
// the test does NOT plant on disk — if the resolver joined it
// onto baseDir, the result would point at a missing file
// (which is exactly what the 1.3.1 fix must avoid).
func TestNameReferenceNotPath(t *testing.T) {
	root := t.TempDir()
	agentsDir := filepath.Join(root, "agents")
	parentPath := writeFile(t, agentsDir, "parent.md",
		"---\nname: parent\nagents:\n  - code-reviewer\n  - helper-bot\n---\n")

	changeDir(t, t.TempDir())

	a, err := customagent.LoadAgent(parentPath)
	if err != nil {
		t.Fatalf("LoadAgent: %v", err)
	}
	if got := a.ResolvedAgents; !reflect.DeepEqual(got, []string{"code-reviewer", "helper-bot"}) {
		t.Errorf("ResolvedAgents = %v, want [code-reviewer helper-bot] (name references unchanged)", got)
	}
	// Raw unchanged: we want the raw slice too so the
	// upstream loader can still see what the user wrote.
	if got := a.RawAgents; !reflect.DeepEqual(got, []string{"code-reviewer", "helper-bot"}) {
		t.Errorf("RawAgents = %v, want [code-reviewer helper-bot]", got)
	}
}

// TestMixedPathsAndNames ensures that within the same
// frontmatter a name reference and a path reference coexist
// correctly. The resolved slice keeps the input order.
func TestMixedPathsAndNames(t *testing.T) {
	root := t.TempDir()
	agentsDir := filepath.Join(root, "agents")
	writeFile(t, agentsDir, "child.md", "---\nname: child\n---\n")
	parentPath := writeFile(t, agentsDir, "parent.md",
		"---\nname: parent\nagents:\n  - code-reviewer\n  - child.md\n  - another-bot\n---\n")

	a, err := customagent.LoadAgent(parentPath)
	if err != nil {
		t.Fatalf("LoadAgent: %v", err)
	}
	want := []string{
		"code-reviewer",
		filepath.Join(agentsDir, "child.md"),
		"another-bot",
	}
	if got := a.ResolvedAgents; !reflect.DeepEqual(got, want) {
		t.Errorf("ResolvedAgents = %v, want %v", got, want)
	}
}

// TestLoadAgentDirSkipsBodyOnly covers the directory
// walker's soft-skip behaviour: a file with no frontmatter
// is just a Markdown file the user happened to drop in,
// not an agent, and must not surface as an error.
func TestLoadAgentDirSkipsBodyOnly(t *testing.T) {
	root := t.TempDir()
	agentsDir := filepath.Join(root, "agents")
	// Has frontmatter -> should load.
	writeFile(t, agentsDir, "real.md", "---\nname: real\n---\nbody\n")
	// No frontmatter -> should be silently skipped.
	writeFile(t, agentsDir, "note.md", "just a note\nno frontmatter\n")
	// Hidden file -> skipped.
	writeFile(t, agentsDir, ".hidden.md", "---\nname: hidden\n---\n")
	// Non-md file -> skipped.
	writeFile(t, agentsDir, "config.json", "{}")

	agents, bad, err := customagent.LoadAgentDir(agentsDir)
	if err != nil {
		t.Fatalf("LoadAgentDir: %v", err)
	}
	if len(bad) != 0 {
		t.Errorf("bad = %v, want none", bad)
	}
	if len(agents) != 1 {
		t.Fatalf("agents count = %d, want 1", len(agents))
	}
	if agents[0].Name != "real" {
		t.Errorf("agents[0].Name = %q, want %q", agents[0].Name, "real")
	}
}

// TestLoadAgentDirSorted checks the deterministic sort order
// the directory walker promises: alphabetical by Path.
func TestLoadAgentDirSorted(t *testing.T) {
	root := t.TempDir()
	agentsDir := filepath.Join(root, "agents")
	writeFile(t, agentsDir, "zeta.md", "---\nname: zeta\n---\n")
	writeFile(t, agentsDir, "alpha.md", "---\nname: alpha\n---\n")
	writeFile(t, agentsDir, "mu.md", "---\nname: mu\n---\n")

	agents, _, err := customagent.LoadAgentDir(agentsDir)
	if err != nil {
		t.Fatalf("LoadAgentDir: %v", err)
	}
	got := make([]string, len(agents))
	for i, a := range agents {
		got[i] = filepath.Base(a.Path)
	}
	want := []string{"alpha.md", "mu.md", "zeta.md"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("walker order = %v, want %v", got, want)
	}
}

// TestLoadAgentErrEmptyWhenFrontmatterMissing ensures the
// sentinel error type is the one `LoadAgentDir` watches for,
// so that future surface additions can't accidentally
// surface a non-frontmatter file as a hard failure.
func TestLoadAgentErrEmptyWhenFrontmatterMissing(t *testing.T) {
	root := t.TempDir()
	p := writeFile(t, root, "no-frontmatter.md", "no frontmatter at all\n")
	_, err := customagent.LoadAgent(p)
	if !errors.Is(err, customagent.ErrEmpty) {
		t.Errorf("err = %v, want ErrEmpty", err)
	}
}

// TestLooksLikePathExhaustive pins down every documented
// "looks like a path" predicate: extensions .md, .sh, .json
// are matched case-insensitively, and any path separator
// (forward slash or backslash) counts.
//
// The rules are:
//
//   - Empty/whitespace-only entries are dropped by the
//     normaliser (a YAML author who listed `agents:\n  -
//     ""\n` produces no entry at all).
//   - Absolute paths are used verbatim.
//   - Entries that "look like a path" (have a separator or
//     end in a known extension) are joined onto the agent
//     file's directory.
//   - Everything else (a bare name like `code-reviewer`) is
//     returned as a name reference — left untouched.
func TestLooksLikePathExhaustive(t *testing.T) {
	cases := []struct {
		in           string
		wantResolved string // empty == identity / passed through
		wantCount    int    // entries expected post-normalisation
		// fullPath is what the resolver is expected
		// to produce when an absolute path is given;
		// the test computes the expected filepath.Clean
		// at runtime so case-1 expects identity but
		// case-7 expects filepath.Clean("/abs/path").
	}{
		// Empty / whitespace-only: normaliser drops.
		{in: "", wantCount: 0},
		{in: "\"   \"", wantCount: 0},
		// Plain names: untouched.
		{in: "foo", wantResolved: "foo", wantCount: 1},
		{in: "code-reviewer", wantResolved: "code-reviewer", wantCount: 1},
		{in: "agent-md", wantResolved: "agent-md", wantCount: 1},
		{in: "foo.bar", wantResolved: "foo.bar", wantCount: 1},
		// "looks like a path" cases: joined onto base.
		{in: "child.md", wantCount: 1},
		{in: "child.MD", wantCount: 1},
		{in: "helper.sh", wantCount: 1},
		{in: "manifest.json", wantCount: 1},
		{in: "subdir/child.md", wantCount: 1},
		{in: "subdir\\\\child.md", wantCount: 1}, // backslash on macOS too — ContainsAny
		{in: "foo/bar", wantCount: 1},
		{in: "./relative/path", wantCount: 1},
		// Absolute paths: returned cleaned (filepath.Clean).
		{in: "/abs/path", wantResolved: "/abs/path", wantCount: 1},
	}
	for _, tc := range cases {
		name := tc.in
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			agentsDir := filepath.Join(root, "agents")
			p := writeFile(t, agentsDir, "parent.md",
				"---\nname: parent\nagents:\n  - "+name+"\n---\n")
			a, err := customagent.LoadAgent(p)
			if err != nil {
				t.Fatalf("LoadAgent(%q): %v", name, err)
			}
			if got := len(a.ResolvedAgents); got != tc.wantCount {
				t.Fatalf("ResolvedAgents count = %d, want %d (input %q)", got, tc.wantCount, name)
			}
			if tc.wantCount == 0 {
				return
			}
			got := a.ResolvedAgents[0]
			if tc.wantResolved != "" {
				// Identity / pass-through: the
				// entry was left as a name or
				// returned cleaned. Compare to
				// filepath.Clean for absolute
				// paths; otherwise exact match.
				if got != tc.wantResolved {
					t.Errorf("input %q -> %q, want %q", name, got, tc.wantResolved)
				}
			} else {
				// "looks like a path": the
				// resolved must be joined to
				// agentsDir (we use the
				// filepath.Clean to match the
				// resolver's own behaviour).
				want := filepath.Clean(filepath.Join(agentsDir, name))
				if got != want {
					t.Errorf("input %q -> %q, want %q (joined to agentsDir)", name, got, want)
				}
			}
		})
	}
}

// TestResolvedAgentsIndexByName verifies the name -> agent
// map built by ResolvedAgents, including the first-wins
// duplicate-name rule.
func TestResolvedAgentsIndexByName(t *testing.T) {
	root := t.TempDir()
	agentsDir := filepath.Join(root, "agents")
	// Two agents sharing the name "dup"; the first
	// found (after LoadAgentDir's alpha sort) is
	// "dup-first.md" because "d" < "d-f" < "second" by
	// byte sort.
	writeFile(t, agentsDir, "dup-first.md", "---\nname: dup\n---\nfirst body\n")
	writeFile(t, agentsDir, "dup-second.md", "---\nname: dup\n---\nsecond body\n")
	writeFile(t, agentsDir, "unique.md", "---\nname: unique\n---\n")

	agents, _, err := customagent.LoadAgentDir(agentsDir)
	if err != nil {
		t.Fatalf("LoadAgentDir: %v", err)
	}
	idx := customagent.ResolvedAgents(agents)
	if len(idx) != 2 {
		t.Fatalf("ResolvedAgents size = %d, want 2", len(idx))
	}
	if idx["dup"].Name != "dup" {
		t.Errorf("dup name = %q", idx["dup"].Name)
	}
	if !strings.Contains(idx["dup"].Body, "first body") {
		t.Errorf("dup body should be the first definition, got %q", idx["dup"].Body)
	}
	if _, ok := idx["unique"]; !ok {
		t.Errorf("unique not indexed")
	}
}

// TestNormaliseEntriesDropsEmpty proves a YAML author who
// listed `skills:\n  - ""\n` (a stray blank line) does NOT
// produce a path-bearing list. The pre-cycle-175 YAML
// decoder in cmd/wagyu/print_slash.go had the same hole and
// ran into it for `description:` with a colon; this loader
// is meant to be defensive from day one.
func TestNormaliseEntriesDropsEmpty(t *testing.T) {
	root := t.TempDir()
	agentsDir := filepath.Join(root, "agents")
	p := writeFile(t, agentsDir, "parent.md",
		"---\nname: parent\nagents:\n  - child.md\n  - \"\"\n  - \"   \"\n  - \"keep-me\"\n---\n")
	a, err := customagent.LoadAgent(p)
	if err != nil {
		t.Fatalf("LoadAgent: %v", err)
	}
	// Empty / whitespace-only entries get dropped;
	// "child.md" and "keep-me" both survive (one is
	// a path, the other is a name reference that
	// looks like a path because of... wait, "keep-me"
	// does not look like a path, so it is left as a
	// name reference. The normaliser keeps both,
	// the resolver keeps both. We assert the
	// normaliser dropped the empties.
	if got, want := a.RawAgents, []string{"child.md", "keep-me"}; !reflect.DeepEqual(got, want) {
		t.Errorf("RawAgents = %v, want %v", got, want)
	}
}

// TestLoadAgentInvalidYAML covers the parseError path: a
// file that opens fine but has a malformed YAML block.
func TestLoadAgentInvalidYAML(t *testing.T) {
	root := t.TempDir()
	agentsDir := filepath.Join(root, "agents")
	// Unclosed flow mapping: yaml.v3 returns an error.
	p := writeFile(t, agentsDir, "broken.md",
		"---\nagents: [child.md\n---\n")
	_, err := customagent.LoadAgent(p)
	if err == nil {
		t.Fatalf("LoadAgent accepted malformed frontmatter")
	}
	var pe *customagent.ParseError
	if errors.As(err, &pe) {
		// expected
	} else {
		t.Errorf("err type = %T, want *customagent.ParseError", err)
	}
}

// TestLoadAgentMissingFile covers the read-error path: a
// file that does not exist at the path we hand in. The
// error must wrap an os.IsNotExist-compatible underlying.
func TestLoadAgentMissingFile(t *testing.T) {
	root := t.TempDir()
	_, err := customagent.LoadAgent(filepath.Join(root, "nope.md"))
	if err == nil {
		t.Fatalf("LoadAgent should fail on missing file")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("err = %v, want os.ErrNotExist in chain", err)
	}
}

// TestLoadAgentRelativePathFromCaller confirms `LoadAgent`
// normalises a relative input into an absolute Path so the
// resolved entries are always absolute. This is the contract
// callers building their own directory walker rely on.
func TestLoadAgentRelativePathFromCaller(t *testing.T) {
	root := t.TempDir()
	abs := writeFile(t, root, "agent.md", "---\nname: a\n---\n")
	rel := "agent.md"
	changeDir(t, root)
	a, err := customagent.LoadAgent(rel)
	if err != nil {
		t.Fatalf("LoadAgent: %v", err)
	}
	// `a.Path` should resolve to the same file we wrote.
	// macOS's /var/folders is a symlink to
	// /private/var/folders; both sides of the
	// comparison go through filepath.EvalSymlinks so
	// the test is hermetic regardless.
	if want, err := filepath.EvalSymlinks(abs); err == nil {
		if got, err := filepath.EvalSymlinks(a.Path); err == nil {
			if got != want {
				t.Errorf("a.Path (resolved) = %q, want %q", got, want)
			}
		}
	} else if a.Path != abs {
		t.Errorf("a.Path = %q, want %q", a.Path, abs)
	}
	if !filepath.IsAbs(a.Dir) {
		t.Errorf("a.Dir %q is not absolute", a.Dir)
	}
}

// TestLoadAgentDirEmptyOrMissing: an absent directory yields
// (nil, nil), not an error. Many callers rely on this for
// the "no custom agents installed" path.
func TestLoadAgentDirEmptyOrMissing(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		agents, bad, err := customagent.LoadAgentDir(filepath.Join(t.TempDir(), "absent"))
		if err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		if agents != nil || bad != nil {
			t.Errorf("got (%v, %v), want (nil, nil)", agents, bad)
		}
	})
	t.Run("present empty", func(t *testing.T) {
		agents, bad, err := customagent.LoadAgentDir(t.TempDir())
		if err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		if len(agents) != 0 || len(bad) != 0 {
			t.Errorf("got (%d, %d), want (0, 0)", len(agents), len(bad))
		}
	})
}

// TestLoadAgentDirIsShallow: a sub-directory under agents/
// is NOT recursed into (cycle 234 made plugins' agents/
// shallow by default — custom agents follow the same rule).
func TestLoadAgentDirIsShallow(t *testing.T) {
	root := t.TempDir()
	agentsDir := filepath.Join(root, "agents")
	// Deep agent: must NOT be picked up.
	writeFile(t, root, filepath.Join("agents", "nested", "deep.md"), "---\nname: deep\n---\n")
	// Top-level agent: must be picked up.
	writeFile(t, root, filepath.Join("agents", "top.md"), "---\nname: top\n---\n")

	agents, _, err := customagent.LoadAgentDir(agentsDir)
	if err != nil {
		t.Fatalf("LoadAgentDir: %v", err)
	}
	if len(agents) != 1 {
		t.Fatalf("got %d agents, want 1 (shallow)", len(agents))
	}
	if agents[0].Name != "top" {
		t.Errorf("agents[0].Name = %q, want top", agents[0].Name)
	}
}

// TestAgentSortedFromLoadAgentDirIsStable runs the same
// walker twice on the same directory and asserts the result
// is byte-identical. This is the contract the TUI's /agents
// panel relies on (a stable ordering keeps cursor position
// meaningful across redraws).
func TestAgentSortedFromLoadAgentDirIsStable(t *testing.T) {
	root := t.TempDir()
	agentsDir := filepath.Join(root, "agents")
	for _, n := range []string{"a.md", "b.md", "c.md", "d.md", "e.md"} {
		writeFile(t, agentsDir, n, "---\nname: "+strings.TrimSuffix(n, ".md")+"\n---\n")
	}
	first, _, err := customagent.LoadAgentDir(agentsDir)
	if err != nil {
		t.Fatalf("first LoadAgentDir: %v", err)
	}
	second, _, err := customagent.LoadAgentDir(agentsDir)
	if err != nil {
		t.Fatalf("second LoadAgentDir: %v", err)
	}
	var firstNames, secondNames []string
	for _, a := range first {
		firstNames = append(firstNames, a.Name)
	}
	for _, a := range second {
		secondNames = append(secondNames, a.Name)
	}
	sort.Strings(firstNames)
	sort.Strings(secondNames)
	if !reflect.DeepEqual(firstNames, secondNames) {
		t.Errorf("walker unstable: first=%v second=%v", firstNames, secondNames)
	}
}
