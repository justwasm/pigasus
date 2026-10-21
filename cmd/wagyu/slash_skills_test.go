// slash_skills_test.go — cycle 199: the "/<skill>" routing contract.
//
// See slash_skills.go for the captured reference evidence (agy 1.4.0
// print mode) behind the three invocable name forms and the 【推测】
// prompt choice. These tests cover the pure helpers, the lookup, and the
// handleSlashCommand wiring, and pin the catalog parity with `agy -p /skills`.
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withSyntheticCatalog installs a fake catalog for the duration of a test
// and returns a cleanup. Tests that must not depend on the host's
// ~/.gemini tree use this.
func withSyntheticCatalog(t *testing.T, skills []jsonSkillInfo) {
	t.Helper()
	orig := slashSkillCatalog
	slashSkillCatalog = func() []jsonSkillInfo { return skills }
	t.Cleanup(func() { slashSkillCatalog = orig })
}

// writeSkillMD creates a SKILL.md under a fresh dir and returns its path.
func writeSkillMD(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSkillBody_StripsFrontmatter(t *testing.T) {
	p := writeSkillMD(t, "---\nname: demo\ndescription: a demo skill\n---\n# Demo\n\nDo the thing.\n")
	got := skillBody(p)
	if got != "# Demo\n\nDo the thing." {
		t.Errorf("skillBody = %q", got)
	}
}

func TestSkillBody_NoFrontmatterReturnedWhole(t *testing.T) {
	p := writeSkillMD(t, "# Plain\n\nNo frontmatter here.\n")
	got := skillBody(p)
	if got != "# Plain\n\nNo frontmatter here." {
		t.Errorf("skillBody = %q", got)
	}
}

func TestSkillBody_UnterminatedAndMissing(t *testing.T) {
	p := writeSkillMD(t, "---\nname: demo\nno closing fence\n")
	if got := skillBody(p); got != "" {
		t.Errorf("unterminated frontmatter should yield \"\", got %q", got)
	}
	if got := skillBody(filepath.Join(t.TempDir(), "nope", "SKILL.md")); got != "" {
		t.Errorf("missing file should yield \"\", got %q", got)
	}
	if got := skillBody(""); got != "" {
		t.Errorf("empty path should yield \"\", got %q", got)
	}
}

func TestSkillBody_CRLF(t *testing.T) {
	p := writeSkillMD(t, "---\r\nname: demo\r\n---\r\nBody line.\r\n")
	if got := skillBody(p); got != "Body line." {
		t.Errorf("CRLF body = %q", got)
	}
}

// TestSkillInvocationNames pins the three accepted spellings captured from
// the reference, and — just as importantly — that nothing is normalised.
func TestSkillInvocationNames(t *testing.T) {
	plugin := jsonSkillInfo{
		Name: "chrome-devtools-plugin:chrome-devtools",
		Path: "/x/plugins/chrome-devtools-plugin/skills/chrome-devtools/SKILL.md",
	}
	got := skillInvocationNames(plugin)
	want := map[string]bool{
		"chrome-devtools-plugin:chrome-devtools": true,
		"chrome-devtools":                        true,
	}
	if len(got) != len(want) {
		t.Fatalf("plugin names = %q, want %d entries", got, len(want))
	}
	for _, n := range got {
		if !want[n] {
			t.Errorf("unexpected plugin alias %q", n)
		}
	}

	builtin := jsonSkillInfo{
		Name: "antigravity-guide",
		Path: "/x/builtin/skills/antigravity_guide/SKILL.md",
	}
	names := skillInvocationNames(builtin)
	if names[0] != "antigravity-guide" {
		t.Errorf("display name should rank first, got %q", names)
	}
	if !hasStrExact(names, "antigravity_guide") {
		t.Errorf("directory name missing from %q", names)
	}
}

func TestSlashSkillLookup_Aliases(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "plugins", "science", "skills", "pubmed_database")
	p := filepath.Join(sub, "SKILL.md")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("---\nname: pubmed-database\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	withSyntheticCatalog(t, []jsonSkillInfo{{
		Name: "science:pubmed-database", Description: "d", Path: p,
	}})

	for _, name := range []string{"science:pubmed-database", "pubmed-database", "pubmed_database"} {
		if _, ok := slashSkillLookup(name); !ok {
			t.Errorf("slashSkillLookup(%q) = false, want true", name)
		}
	}
	// The separator is NOT normalised: "pubmed_database" (dir) works but
	// "science:pubmed_database" and "PubMed-Database" do not.
	for _, name := range []string{"science:pubmed_database", "PubMed-Database", "pubmed", ""} {
		if _, ok := slashSkillLookup(name); ok {
			t.Errorf("slashSkillLookup(%q) = true, want false", name)
		}
	}
}

func TestSkillPrompt_PrefersBodyFallsBackToDescription(t *testing.T) {
	p := writeSkillMD(t, "---\nname: demo\ndescription: desc text\n---\nThe body instructions.\n")
	got := slashSkillPrompt(jsonSkillInfo{Name: "demo", Description: "desc text", Path: p}, "")
	if got != "The body instructions." {
		t.Errorf("prompt = %q, want the body", got)
	}

	// A skill whose SKILL.md has no body falls back to the description.
	empty := writeSkillMD(t, "---\nname: demo\ndescription: desc text\n---\n")
	got = slashSkillPrompt(jsonSkillInfo{Name: "demo", Description: "desc text", Path: empty}, "")
	if got != "desc text" {
		t.Errorf("prompt = %q, want the description", got)
	}

	// Args are appended as a separate block.
	got = slashSkillPrompt(jsonSkillInfo{Name: "demo", Description: "desc", Path: empty}, "all files")
	if got != "desc\n\nall files" {
		t.Errorf("prompt with args = %q", got)
	}
}

// TestRunSkill_SendsPrompt is the routing contract for the §5 P5 gap: a
// known skill must reach the model (m.streaming=true, a session message
// carrying the skill body) and must NOT render "Unknown command".
func TestRunSkill_SendsPrompt(t *testing.T) {
	p := writeSkillMD(t, "---\nname: demo-skill\n---\nSkill instructions here.\n")
	withSyntheticCatalog(t, []jsonSkillInfo{{Name: "demo-skill", Description: "d", Path: p}})

	m := newSlashModel(t)
	handled, cmd := m.handleSlashCommand("/demo-skill")
	if !handled {
		t.Fatal("/demo-skill not handled")
	}
	if cmd == nil {
		t.Fatal("/demo-skill should return a non-nil tea.Cmd (the model stream)")
	}
	if !m.streaming {
		t.Error("/demo-skill should set m.streaming=true")
	}
	if len(m.response) == 0 || m.response[0] != "> /demo-skill" {
		t.Errorf("echo = %q, want %q", firstNLines(m.response, 1), "> /demo-skill")
	}
	if got := lastUserMessage(m); got != "Skill instructions here." {
		t.Errorf("session prompt = %q", got)
	}
	for _, row := range m.response {
		if strings.Contains(row, "Unknown command") {
			t.Errorf("known skill rendered %q", row)
		}
	}
}

// TestRunSkill_AliasRouting exercises the plugin-prefix and directory
// aliases through the router, and confirms args are forwarded.
func TestRunSkill_AliasRouting(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "skills", "chrome-devtools")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(p, []byte("---\nname: chrome-devtools\n---\nCDP body.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	withSyntheticCatalog(t, []jsonSkillInfo{{
		Name: "chrome-devtools-plugin:chrome-devtools", Description: "d", Path: p,
	}})

	for _, cmdline := range []string{
		"/chrome-devtools-plugin:chrome-devtools",
		"/chrome-devtools",
	} {
		m := newSlashModel(t)
		if _, cmd := m.handleSlashCommand(cmdline); cmd == nil {
			t.Errorf("%s did not start a stream", cmdline)
		} else if !strings.Contains(lastUserMessage(m), "CDP body.") {
			t.Errorf("%s session prompt = %q", cmdline, lastUserMessage(m))
		}
	}

	// Args after the name are echoed and appended to the prompt.
	m := newSlashModel(t)
	m.handleSlashCommand("/chrome-devtools inspect example.com")
	if m.response[0] != "> /chrome-devtools inspect example.com" {
		t.Errorf("echo with args = %q", m.response[0])
	}
	if got := lastUserMessage(m); got != "CDP body.\n\ninspect example.com" {
		t.Errorf("session prompt with args = %q", got)
	}
}

// TestRunSkill_UnknownStillUnknown guards the existing default-branch
// behaviour for a name that is neither a built-in command nor a skill.
func TestRunSkill_UnknownStillUnknown(t *testing.T) {
	withSyntheticCatalog(t, nil)
	m := newSlashModel(t)
	m.handleSlashCommand("/zzznotacommand")
	rows := strings.Join(afterEcho(m), "\n")
	if !strings.Contains(rows, "Unknown command: /zzznotacommand") {
		t.Errorf("unknown command rendered %q", rows)
	}
	if m.streaming {
		t.Error("an unknown command must not start a model stream")
	}
}

// TestSkillCatalog_ReferenceSubset pins the catalog against the names
// `agy -p /skills` printed on the capture machine (57 entries). The catalog
// is disk-backed, so the test skips unless the plugin bundle is installed;
// it asserts the reference names are a subset rather than an exact match so
// a user-added skill does not break it.
func TestSkillCatalog_ReferenceSubset(t *testing.T) {
	names := map[string]bool{}
	for _, s := range printSlashSkillInfos() {
		names[s.Name] = true
	}
	if len(names) == 0 {
		t.Skip("no skills installed on this host")
	}
	if !names["science:pubmed-database"] {
		t.Skip("plugin bundle not installed; reference set incomplete")
	}
	reference := []string{
		"agents-sdk", "agy-customizations", "antigravity-guide",
		"chrome-devtools-plugin:a11y-debugging",
		"chrome-devtools-plugin:chrome-devtools",
		"chrome-devtools-plugin:debug-optimize-lcp",
		"chrome-devtools-plugin:memory-leak-debugging",
		"chrome-devtools-plugin:troubleshooting",
		"cloudflare", "cloudflare-email-service", "durable-objects",
		"google-antigravity-sdk:google-antigravity-sdk",
		"modern-web-guidance-plugin:chrome-extensions",
		"modern-web-guidance-plugin:modern-web-guidance",
		"sandbox-sdk",
		"science:alphafold-database-fetch-and-analyze",
		"science:alphagenome-single-variant-analysis",
		"science:chembl-database", "science:clinical-trials-database",
		"science:clinvar-database", "science:dbsnp-database",
		"science:embl-ebi-ols", "science:encode-ccres-database",
		"science:ensembl-database", "science:foldseek-structural-search",
		"science:gnomad-database", "science:gtex-database",
		"science:human-protein-atlas-database", "science:interpro-database",
		"science:jaspar-database", "science:literature-search-arxiv",
		"science:literature-search-biorxiv",
		"science:literature-search-europepmc",
		"science:literature-search-openalex", "science:ncbi-sequence-fetch",
		"science:openfda-database", "science:opentargets-database",
		"science:pdb-database", "science:protein-sequence-msa",
		"science:protein-sequence-similarity-search", "science:pubchem-database",
		"science:pubmed-database", "science:pymol", "science:quickgo-database",
		"science:reactome-database", "science:science-skills-common",
		"science:scienceskillscommon", "science:string-database",
		"science:ucsc-conservation-and-tfbs", "science:unibind-database",
		"science:uniprot-database", "science:uv",
		"science:workflow-skill-creator", "terminal-browser", "web-perf",
		"workers-best-practices", "wrangler",
	}
	for _, name := range reference {
		if !names[name] {
			t.Errorf("catalog is missing reference skill %q", name)
		}
		s, ok := slashSkillLookup(name)
		if !ok {
			t.Errorf("reference skill %q is not resolvable", name)
			continue
		}
		if p := slashSkillPrompt(s, ""); strings.TrimSpace(p) == "" {
			t.Errorf("reference skill %q produced an empty prompt (path %q)", name, s.Path)
		}
	}
}

func hasStrExact(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
