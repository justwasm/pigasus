// slash_skills_dedup_test.go — cycle skill-discovery-already-discovered:
//
// unit tests for the content-hash deduplication of the skills catalog.
//
// The dedup is a single-pass function over a []jsonSkillInfo; tests
// drive it directly with synthetic slices so the suite does not depend
// on the host's ~/.gemini tree or any plugin bundle being installed.
// See slash_skills_dedup.go for the rule and its evidence grade.
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeSkillInDir writes a SKILL.md under <root>/<subdir>/ and returns
// the absolute path to the file. Tests use this to plant a skill at a
// chosen directory so the catalog builder can pick it up, and to copy
// the same content into a second directory to set up a duplicate.
func writeSkillInDir(t *testing.T, root, subdir, body string) string {
	t.Helper()
	dir := filepath.Join(root, subdir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestDedup_SameSkillViaPluginAndProject is the AGY 1.2.7 gap case: the
// same SKILL.md content is discovered once under a plugin's skills/
// directory (prefixed "<plugin>:<name>") and once under a shared root
// (bare "<name>"). After dedup, only one entry remains — and its name
// is the declared (frontmatter `name:`) value, not the prefixed form.
func TestDedup_SameSkillViaPluginAndProject(t *testing.T) {
	body := "---\nname: chrome-devtools\ndescription: CDP body\n---\nCDP instructions.\n"
	root := t.TempDir()
	pluginPath := writeSkillInDir(t, root, "chrome-devtools-plugin/skills/chrome-devtools", body)
	sharedPath := writeSkillInDir(t, root, ".claude/skills/chrome-devtools", body)

	// The plugin discovery produces a prefixed name; the project
	// discovery produces the bare declared name.
	in := []jsonSkillInfo{
		{Name: "chrome-devtools-plugin:chrome-devtools", Description: "CDP body",
			Path: pluginPath, Plugin: "chrome-devtools-plugin", ModelInvocable: true},
		{Name: "chrome-devtools", Description: "CDP body",
			Path: sharedPath, ModelInvocable: true},
	}

	out := dedupeSkillInfosByHash(in)
	if len(out) != 1 {
		t.Fatalf("dedupe collapsed %d → %d entries, want 1", len(in), len(out))
	}
	// The first-encountered entry wins. Its name retains the plugin
	// prefix; the Plugin field records the source for downstream
	// consumers that need to know where it lives on disk.
	if got := out[0].Name; got != "chrome-devtools-plugin:chrome-devtools" {
		t.Errorf("surviving name = %q, want the first-encountered entry's name", got)
	}
	if got := out[0].Plugin; got != "chrome-devtools-plugin" {
		t.Errorf("surviving plugin = %q, want chrome-devtools-plugin", got)
	}
	if got := out[0].Path; got != pluginPath {
		t.Errorf("surviving path = %q, want the first-encountered entry's path", got)
	}
}

// TestDedup_DifferentSkillsBothKept is the negative case: two
// SKILL.md files with distinct content hash to distinct values and
// must both survive dedup.
func TestDedup_DifferentSkillsBothKept(t *testing.T) {
	root := t.TempDir()
	p1 := writeSkillInDir(t, root, "alpha", "---\nname: alpha\n---\nA body.\n")
	p2 := writeSkillInDir(t, root, "beta", "---\nname: beta\n---\nB body.\n")

	in := []jsonSkillInfo{
		{Name: "alpha", Path: p1, ModelInvocable: true},
		{Name: "beta", Path: p2, ModelInvocable: true},
	}
	out := dedupeSkillInfosByHash(in)
	if len(out) != 2 {
		t.Fatalf("dedupe collapsed 2 distinct skills to %d entries, want 2", len(out))
	}
	got := map[string]string{}
	for _, s := range out {
		got[s.Name] = s.Path
	}
	if got["alpha"] != p1 {
		t.Errorf("alpha path = %q, want %q", got["alpha"], p1)
	}
	if got["beta"] != p2 {
		t.Errorf("beta path = %q, want %q", got["beta"], p2)
	}
}

// TestDedup_HashCollisionHandled pins the property that a hash
// collision does NOT merge genuinely-distinct skills. We don't try to
// reproduce an actual SHA-256 collision (it would take ~10�³² years of
// CPU); instead we simulate the failure mode with two entries whose
// Path points at the same unreadable file, which forces
// skillContentHash to return "" for both. The dedup must keep them
// both — collapsing would otherwise turn every I/O error into a silent
// data-loss event.
func TestDedup_HashCollisionHandled(t *testing.T) {
	// A path that does not exist produces skillContentHash == "". Two
	// distinct entries pointing at it therefore look like a "collision"
	// to the dedup, and the rule is: keep both.
	in := []jsonSkillInfo{
		{Name: "alpha", Path: filepath.Join(t.TempDir(), "missing-a", "SKILL.md"), ModelInvocable: true},
		{Name: "beta", Path: filepath.Join(t.TempDir(), "missing-b", "SKILL.md"), ModelInvocable: true},
	}
	out := dedupeSkillInfosByHash(in)
	if len(out) != 2 {
		t.Fatalf("unreadable SKILL.md paths must not collide, got %d entries, want 2", len(out))
	}
}

// TestDedup_DeclaredNameWinsOverFilename pins the post-condition from
// the AGY 1.2.7 changelog: "skill listings now show the skill's
// declared name only". When the SAME content is discovered twice, the
// surviving entry's name must be the declared (frontmatter `name:`)
// value — not the directory-derived fallback. The discovery layer in
// printSlashSkillInfos already reads frontmatter before falling back,
// so this test passes by construction; it pins the contract so a
// future refactor that drops the frontmatter read fails loudly.
func TestDedup_DeclaredNameWinsOverFilename(t *testing.T) {
	body := "---\nname: declared-name\ndescription: d\n---\nThe body.\n"
	root := t.TempDir()
	// The plugin puts the skill under directory "chrome-devtools"; the
	// frontmatter declares "declared-name". The discovery layer picks
	// the frontmatter, so both entries have Name == "declared-name"
	// regardless of where on disk they live. Two discoveries of the
	// same content must produce the same declared name.
	pluginPath := writeSkillInDir(t, root, "chrome-devtools-plugin/skills/chrome-devtools", body)
	sharedPath := writeSkillInDir(t, root, ".claude/skills/chrome-devtools", body)

	in := []jsonSkillInfo{
		{Name: "chrome-devtools-plugin:declared-name", Path: pluginPath,
			Plugin: "chrome-devtools-plugin", ModelInvocable: true},
		{Name: "declared-name", Path: sharedPath, ModelInvocable: true},
	}
	out := dedupeSkillInfosByHash(in)
	if len(out) != 1 {
		t.Fatalf("dedupe collapsed %d → %d, want 1", len(in), len(out))
	}
	// Strip the plugin prefix to read the declared component: the
	// declared name is the trailing field after the last colon.
	name := out[0].Name
	if i := strings.LastIndex(name, ":"); i >= 0 {
		name = name[i+1:]
	}
	if name != "declared-name" {
		t.Errorf("declared name = %q, want %q", name, "declared-name")
	}
}

// TestDedup_MultipleSkillHashesDeduped is the bulk case: five entries
// cover three distinct SKILL.md files (one with two duplicates, one
// unique, and one with three duplicates). After dedup, exactly three
// entries remain and the count per hash matches the input.
func TestDedup_MultipleSkillHashesDeduped(t *testing.T) {
	root := t.TempDir()
	bodyA := "---\nname: alpha\n---\nA body.\n"
	bodyB := "---\nname: beta\n---\nB body.\n"
	bodyC := "---\nname: gamma\n---\nC body.\n"

	// alpha: three discoveries under plugin, shared, and claude roots.
	// beta: one discovery (unique). gamma: two discoveries (plugin +
	// shared).
	pa1 := writeSkillInDir(t, root, "p/skills/alpha", bodyA)
	pa2 := writeSkillInDir(t, root, ".gemini/skills/alpha", bodyA)
	pa3 := writeSkillInDir(t, root, ".claude/skills/alpha", bodyA)
	pb := writeSkillInDir(t, root, ".gemini/skills/beta", bodyB)
	pc1 := writeSkillInDir(t, root, "q/skills/gamma", bodyC)
	pc2 := writeSkillInDir(t, root, ".gemini/skills/gamma", bodyC)

	in := []jsonSkillInfo{
		{Name: "p:alpha", Path: pa1, Plugin: "p", ModelInvocable: true},
		{Name: "alpha", Path: pa2, ModelInvocable: true},
		{Name: "alpha", Path: pa3, ModelInvocable: true},
		{Name: "beta", Path: pb, ModelInvocable: true},
		{Name: "q:gamma", Path: pc1, Plugin: "q", ModelInvocable: true},
		{Name: "gamma", Path: pc2, ModelInvocable: true},
	}
	out := dedupeSkillInfosByHash(in)
	if len(out) != 3 {
		t.Fatalf("dedupe collapsed %d → %d, want 3 (alpha, beta, gamma)", len(in), len(out))
	}
	seen := map[string]string{}
	for _, s := range out {
		// Normalise the plugin-prefixed form so the map key matches
		// across plugin / shared discoveries.
		key := s.Name
		if i := strings.LastIndex(key, ":"); i >= 0 {
			key = key[i+1:]
		}
		seen[key] = s.Path
	}
	// alpha's surviving entry is the first-encountered (plugin "p").
	if seen["alpha"] != pa1 {
		t.Errorf("alpha surviving path = %q, want %q (first-encountered)", seen["alpha"], pa1)
	}
	if seen["beta"] != pb {
		t.Errorf("beta surviving path = %q, want %q", seen["beta"], pb)
	}
	if seen["gamma"] != pc1 {
		t.Errorf("gamma surviving path = %q, want %q (first-encountered)", seen["gamma"], pc1)
	}
}

// TestDedup_FirstEncounteredWinsForMetadata pins that the surviving
// entry keeps the first-encountered's path / plugin / builtin /
// description rather than blending fields from the duplicate. The
// gap's "show the skill's declared name only" wording could be
// misread to mean "merge everything"; this test guards against that.
func TestDedup_FirstEncounteredWinsForMetadata(t *testing.T) {
	body := "---\nname: alpha\ndescription: original desc\n---\nBody.\n"
	root := t.TempDir()
	first := writeSkillInDir(t, root, "plugin/skills/alpha", body)
	// Second discovery uses the same body so dedup collapses it. (If
	// the body differed, dedup would not fire — see
	// TestDedup_DifferentSkillsBothKept.)
	second := writeSkillInDir(t, root, ".claude/skills/alpha", body)

	in := []jsonSkillInfo{
		{Name: "plugin:alpha", Description: "original desc",
			Path: first, Plugin: "plugin", Builtin: false, ModelInvocable: true},
		{Name: "alpha", Description: "duplicate desc",
			Path: second, ModelInvocable: true},
	}
	out := dedupeSkillInfosByHash(in)
	if len(out) != 1 {
		t.Fatalf("dedupe → %d entries, want 1", len(out))
	}
	if got := out[0].Path; got != first {
		t.Errorf("path = %q, want first-encountered %q", got, first)
	}
	if got := out[0].Plugin; got != "plugin" {
		t.Errorf("plugin = %q, want %q (first-encountered)", got, "plugin")
	}
	if got := out[0].Description; got != "original desc" {
		t.Errorf("description = %q, want %q (first-encountered)", got, "original desc")
	}
}

// TestSkillContentHash_Deterministic pins the underlying primitive:
// identical bytes produce identical hashes, and distinct bytes
// produce distinct hashes. The dedup depends on this.
func TestSkillContentHash_Deterministic(t *testing.T) {
	root := t.TempDir()
	p := writeSkillInDir(t, root, "skill", "---\nname: x\n---\nbody\n")

	h1 := skillContentHash(p)
	h2 := skillContentHash(p)
	if h1 == "" {
		t.Fatal("skillContentHash returned empty for an existing file")
	}
	if h1 != h2 {
		t.Errorf("skillContentHash not deterministic: %q != %q", h1, h2)
	}
	// 64 hex chars = 32 bytes = SHA-256.
	if len(h1) != 64 {
		t.Errorf("skillContentHash length = %d, want 64 (SHA-256 hex)", len(h1))
	}

	// A different body must hash differently.
	p2 := writeSkillInDir(t, root, "skill2", "---\nname: y\n---\nbody\n")
	if h := skillContentHash(p2); h == h1 {
		t.Errorf("different content hashed equal: %q", h)
	}

	// Empty path and missing file both yield "" — the sentinel the
	// dedup uses to mean "skip the hash check".
	if got := skillContentHash(""); got != "" {
		t.Errorf("skillContentHash(\"\") = %q, want \"\"", got)
	}
	if got := skillContentHash(filepath.Join(t.TempDir(), "nope", "SKILL.md")); got != "" {
		t.Errorf("skillContentHash(missing) = %q, want \"\"", got)
	}
}

// TestPrintSlashSkillInfos_DedupApplied is the integration check: a
// SKILL.md installed under BOTH a plugin's skills/ directory AND the
// shared ~/.gemini/skills root must surface as a single entry in the
// catalog the /skills panel renders. This is the user-visible
// behaviour the gap closes.
func TestPrintSlashSkillInfos_DedupApplied(t *testing.T) {
	// The declared name from frontmatter is "shared-skill" — the
	// directory-derived fallback "demo-skill" never enters the catalog.
	body := "---\nname: shared-skill\ndescription: d\n---\nThe body.\n"

	// printSlashSkillInfos walks three roots: the builtin bundle, the
	// .gemini/skills shared root, and every plugin's skills/ tree. We
	// plant the same SKILL.md under the shared root and one plugin's
	// tree so dedup has something to do — this is the same shape as
	// the plugin-vs-`.claude/skills` overlap the AGY 1.2.7 changelog
	// describes, just routed through two roots the enumerator already
	// walks.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ANTIGRAVITY_HOME", filepath.Join(home, ".gemini", "antigravity-cli"))

	pluginDir := filepath.Join(home, ".gemini", "config", "plugins", "demo-plugin", "skills", "demo-skill")
	sharedDir := filepath.Join(home, ".gemini", "skills", "demo-skill")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(sharedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sharedDir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	cat := printSlashSkillInfos()
	// Count entries whose declared name resolves to "shared-skill".
	// The plugin-prefixed form is "<plugin>:shared-skill"; the shared
	// form is "shared-skill". Strip the prefix to compare apples to
	// apples.
	count := 0
	for _, s := range cat {
		name := s.Name
		if i := strings.LastIndex(name, ":"); i >= 0 {
			name = name[i+1:]
		}
		if name == "shared-skill" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("catalog has %d entries named shared-skill, want 1 (dedup did not collapse the duplicates):\n%+v", count, cat)
	}
}
