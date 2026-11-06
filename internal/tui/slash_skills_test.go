// slash_skills_test.go — cycle 199: the "/<skill>" routing contract.
//
// See slash_skills.go for the captured reference evidence (agy 1.4.0
// print mode) behind the three invocable name forms and the 【推测】
// prompt choice. These tests cover the pure helpers, the lookup, and the
// handleSlashCommand wiring, and pin the catalog parity with `agy -p /skills`.
//
// Cycle 381 moved the *picker* out of the host: the grouped list, the
// cursor, the per-skill detail view and its scroll window, the keys and
// the rows live in internal/ui/views/skillsui, and so do their tests
// (skillsui/panel_test.go). What is left here is the host's own half —
// the catalogue the panel reads, the panel's wiring into the host's key
// router and the live-catalogue smoke test.
package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/printmode"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	termcolor "github.com/justwasm/pigasus/internal/ui/render/color"
	"github.com/justwasm/pigasus/internal/ui/views/skillsui"
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
	got := printmode.SkillBody(p)
	if got != "# Demo\n\nDo the thing." {
		t.Errorf("skillBody = %q", got)
	}
}

func TestSkillBody_NoFrontmatterReturnedWhole(t *testing.T) {
	p := writeSkillMD(t, "# Plain\n\nNo frontmatter here.\n")
	got := printmode.SkillBody(p)
	if got != "# Plain\n\nNo frontmatter here." {
		t.Errorf("skillBody = %q", got)
	}
}

func TestSkillBody_UnterminatedAndMissing(t *testing.T) {
	p := writeSkillMD(t, "---\nname: demo\nno closing fence\n")
	if got := printmode.SkillBody(p); got != "" {
		t.Errorf("unterminated frontmatter should yield \"\", got %q", got)
	}
	if got := printmode.SkillBody(filepath.Join(t.TempDir(), "nope", "SKILL.md")); got != "" {
		t.Errorf("missing file should yield \"\", got %q", got)
	}
	if got := printmode.SkillBody(""); got != "" {
		t.Errorf("empty path should yield \"\", got %q", got)
	}
}

func TestSkillBody_CRLF(t *testing.T) {
	p := writeSkillMD(t, "---\r\nname: demo\r\n---\r\nBody line.\r\n")
	if got := printmode.SkillBody(p); got != "Body line." {
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
	got := printmode.SkillInvocationNames(plugin)
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
	names := printmode.SkillInvocationNames(builtin)
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
	got := printmode.SlashSkillPrompt(jsonSkillInfo{Name: "demo", Description: "desc text", Path: p}, "")
	if got != "The body instructions." {
		t.Errorf("prompt = %q, want the body", got)
	}

	// A skill whose SKILL.md has no body falls back to the description.
	empty := writeSkillMD(t, "---\nname: demo\ndescription: desc text\n---\n")
	got = printmode.SlashSkillPrompt(jsonSkillInfo{Name: "demo", Description: "desc text", Path: empty}, "")
	if got != "desc text" {
		t.Errorf("prompt = %q, want the description", got)
	}

	// Args are appended as a separate block.
	got = printmode.SlashSkillPrompt(jsonSkillInfo{Name: "demo", Description: "desc", Path: empty}, "all files")
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
		if p := printmode.SlashSkillPrompt(s, ""); strings.TrimSpace(p) == "" {
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

func TestVerbatimSkillArgs_AtPath(t *testing.T) {
	in := "@path/to/file"
	got := skillsui.VerbatimSkillArgs(in)
	if got != in {
		t.Fatalf("skillsui.VerbatimSkillArgs(%q) = %q, want identity pass-through", in, got)
	}
}

func TestSlashSkillPrompt_AtArgVerbatim(t *testing.T) {
	p := writeSkillMD(t, "---\nname: demo\n---\nbody.\n")
	got := printmode.SlashSkillPrompt(jsonSkillInfo{Name: "demo", Path: p}, "@path/to/file")
	want := "body.\n\n@path/to/file"
	if got != want {
		t.Fatalf("slashSkillPrompt with @arg = %q, want %q", got, want)
	}
	if via := printmode.SlashSkillPrompt(jsonSkillInfo{Name: "demo", Path: p}, skillsui.VerbatimSkillArgs("@path/to/file")); via != want {
		t.Fatalf("printmode.SlashSkillPrompt(skillsui.VerbatimSkillArgs(@arg)) = %q, want %q", via, want)
	}
}

func TestSlashSkillPrompt_NormalArgsWorks(t *testing.T) {
	p := writeSkillMD(t, "---\nname: demo\n---\nbody.\n")
	got := printmode.SlashSkillPrompt(jsonSkillInfo{Name: "demo", Path: p}, "inspect example.com")
	want := "body.\n\ninspect example.com"
	if got != want {
		t.Fatalf("slashSkillPrompt with normal args = %q, want %q", got, want)
	}
}

func TestSlashSkillPrompt_NoAtArgWorks(t *testing.T) {
	p := writeSkillMD(t, "---\nname: demo\n---\nbody only.\n")
	if got := printmode.SlashSkillPrompt(jsonSkillInfo{Name: "demo", Path: p}, ""); got != "body only." {
		t.Fatalf("slashSkillPrompt with no args = %q, want %q", got, "body only.")
	}
	if got := printmode.SlashSkillPrompt(jsonSkillInfo{Name: "demo", Path: p}, skillsui.VerbatimSkillArgs("")); got != "body only." {
		t.Fatalf("printmode.SlashSkillPrompt(skillsui.VerbatimSkillArgs(\"\")) = %q, want %q", got, "body only.")
	}
}

func TestSlashSkillPrompt_MultipleAtArgsPreserved(t *testing.T) {
	p := writeSkillMD(t, "---\nname: demo\n---\nbody.\n")
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"two", "@a @b", "body.\n\n@a @b"},
		{"three", "@a @b @c", "body.\n\n@a @b @c"},
		{"mixed", "@path/to/file and then @another/place", "body.\n\n@path/to/file and then @another/place"},
		{"dup", "@a @a", "body.\n\n@a @a"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := printmode.SlashSkillPrompt(jsonSkillInfo{Name: "demo", Path: p}, tc.in)
			if got != tc.want {
				t.Errorf("printmode.SlashSkillPrompt(%q) = %q, want %q", tc.in, got, tc.want)
			}
			if via := printmode.SlashSkillPrompt(jsonSkillInfo{Name: "demo", Path: p}, skillsui.VerbatimSkillArgs(tc.in)); via != tc.want {
				t.Errorf("printmode.SlashSkillPrompt(skillsui.VerbatimSkillArgs(%q)) = %q, want %q", tc.in, via, tc.want)
			}
		})
	}
}

func TestSlashSkillPrompt_AtInMiddleNotExpanded(t *testing.T) {
	p := writeSkillMD(t, "---\nname: demo\n---\nbody.\n")
	in := "please consider @path/to/file between two regular words"
	want := "body.\n\nplease consider @path/to/file between two regular words"
	got := printmode.SlashSkillPrompt(jsonSkillInfo{Name: "demo", Path: p}, in)
	if got != want {
		t.Errorf("printmode.SlashSkillPrompt(%q) = %q, want %q", in, got, want)
	}
}

func TestRunSkill_AtPathVerbatimThroughRouter(t *testing.T) {
	p := writeSkillMD(t, "---\nname: demo-skill\n---\nSkill instructions here.\n")
	withSyntheticCatalog(t, []jsonSkillInfo{{Name: "demo-skill", Description: "d", Path: p}})

	m := newSlashModel(t)
	handled, _ := m.handleSlashCommand("/demo-skill @path/to/file")
	if !handled {
		t.Fatal("/demo-skill @path/to/file was not handled")
	}
	if m.response == nil || m.response[0] != "> /demo-skill @path/to/file" {
		t.Fatalf("transcript echo = %v, want \"> /demo-skill @path/to/file\"", m.response)
	}
	got := lastUserMessage(m)
	want := "Skill instructions here.\n\n@path/to/file"
	if got != want {
		t.Fatalf("session prompt = %q, want %q", got, want)
	}
	for _, row := range m.response {
		if strings.Contains(row, "<file name=") {
			t.Errorf("transcript contains a file-mention wrapper: %q", row)
		}
	}
}

func TestRunSkill_PluginSkillAtArgVerbatim(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "plugins", "demo-plugin", "skills", "demo-skill")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(p, []byte("---\nname: demo-plugin:demo-skill\n---\nCDP-like body.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	withSyntheticCatalog(t, []jsonSkillInfo{{
		Name: "demo-plugin:demo-skill", Description: "d", Path: p,
	}})

	m := newSlashModel(t)
	handled, _ := m.handleSlashCommand("/demo-plugin:demo-skill @path/to/file")
	if !handled {
		t.Fatal("/demo-plugin:demo-skill @path/to/file was not handled")
	}
	got := lastUserMessage(m)
	want := "CDP-like body.\n\n@path/to/file"
	if got != want {
		t.Fatalf("plugin skill session prompt = %q, want %q", got, want)
	}
}

// syntheticSharedSkills builds a sorted catalogue of n shared skills.
//
// The host's catalogue seam is the panel's only source of skills, so a
// host-side test that needs a known catalogue builds one here.
func syntheticSharedSkills(n int) []jsonSkillInfo {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	out := make([]jsonSkillInfo, 0, n)
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("s%02d", i)
		out = append(out, jsonSkillInfo{
			Name: name, Description: "d",
			Path:           filepath.Join(home, ".gemini", "skills", name, "SKILL.md"),
			ModelInvocable: true,
		})
	}
	return out
}

// TestSkillsPanelOpensUnselected pins that opening /skills leaves the cursor at
// -1 (the reference shows no "› " until the first ↓).
func TestSkillsPanelOpensUnselected(t *testing.T) {
	withSyntheticCatalog(t, syntheticSharedSkills(3))
	m := &runMode{}
	if !m.openSlashPanel("skills") {
		t.Fatal("openSlashPanel(skills) = false")
	}
	if got := m.skillsPanel().Cursor(); got != -1 {
		t.Errorf("cursor = %d, want -1", got)
	}
}

// assertPanelSkillsLayout backs the delegation stub TestPanelSkillsLayout in
// slash_panels_test.go. It is host-tolerant: it uses the real catalogue and
// asserts the shape rather than the exact rows.
func assertPanelSkillsLayout(t *testing.T) {
	t.Helper()
	cat := skillsui.PanelCatalog(slashSkillCatalog())
	m := mkPanel(panelregistry.Skills, "/skills", -1) // height 0 → the 220x30 default
	plain := plainLines(m)
	if len(plain) < 5 {
		t.Fatalf("panel too short:\n%s", strings.Join(plain, "\n"))
	}
	if plain[0] != "Skills" {
		t.Errorf("title = %q", plain[0])
	}
	if want := fmt.Sprintf("%d skills", len(cat)); plain[1] != want {
		t.Errorf("subtitle = %q, want %q", plain[1], want)
	}
	joined := strings.Join(plain, "\n")
	if !strings.Contains(joined, "Create new skills") {
		t.Errorf("missing the create block:\n%s", joined)
	}
	if len(cat) == 0 {
		return
	}
	if !strings.Contains(joined, "Shared skills · ") {
		t.Errorf("missing the shared group header:\n%s", joined)
	}
	if n := len(skillsui.NewPanelModel(slashSkillCatalog()).Items); n > 11 {
		if want := fmt.Sprintf("  [1-11 of %d items]", n); !strings.Contains(joined, want) {
			t.Errorf("missing counter %q:\n%s", want, joined)
		}
	}
	last := termcolor.StripSGR(plain[len(plain)-1])
	if !strings.Contains(last, "Navigate") || !strings.Contains(last, "View") {
		t.Errorf("legend = %q", last)
	}
}

// TestSkillsPanelLayout_HostCatalogue is the live-catalogue smoke test: on a
// host with the plugin bundle it asserts the reference arithmetic (60 skills,
// 66 items) and that no rendered line exceeds the window width.
func TestSkillsPanelLayout_HostCatalogue(t *testing.T) {
	cat := skillsui.PanelCatalog(slashSkillCatalog())
	if len(cat) == 0 {
		t.Skip("no skills installed on this host")
	}
	plugins := map[string]bool{}
	for _, s := range cat {
		if s.Builtin {
			plugins["<builtin>"] = true
		} else if s.Plugin != "" {
			plugins[s.Plugin] = true
		} else {
			plugins["<shared>"] = true
		}
	}
	if !plugins["science"] {
		t.Skip("plugin bundle not installed; reference set incomplete")
	}
	if got := len(cat); got != 60 && got != 66 {
		t.Errorf("skills = %d, want 60 or 66", got)
	}
	model := skillsui.NewPanelModel(slashSkillCatalog())
	expectedItems := len(cat) + len(model.Groups)
	if got := len(model.Items); got != expectedItems {
		t.Errorf("items = %d, want %d (%d skills + %d headers)", got, expectedItems, len(cat), len(model.Groups))
	}
	if got := len(model.Groups); got != 6 {
		t.Errorf("groups = %d, want 6", got)
	}

	m := mkPanel(panelregistry.Skills, "/skills", -1)
	m.height = 50
	m.width = 220
	for _, l := range m.panelLines() {
		if n := len([]rune(termcolor.StripSGR(l))); n > m.width-1 && strings.Contains(termcolor.StripSGR(l), ": ") {
			t.Errorf("row exceeds %d columns (%d): %q", m.width-1, n, termcolor.StripSGR(l)[:40])
		}
	}
	joined := strings.Join(plainLines(m), "\n")
	expectedCounter := fmt.Sprintf("[1-26 of %d items]", expectedItems)
	if !strings.Contains(joined, expectedCounter) {
		t.Errorf("missing 220x50 counter:\n%s", joined)
	}
}

// TestCycle211_SkillsDetail is the host's half of the cycle-211 entry
// point. The detail viewer's own family (the document, the counter and
// the scroll keys) moved to internal/ui/views/skillsui with the code it
// exercises; what is left here is the host's wiring: the panel opens the
// detail view and Esc returns to the list.
func TestCycle211_SkillsDetail(t *testing.T) {
	t.Run("HostCatalogOpensDetail", TestSkillsDetail_OpensViaHandlePanelKey)
}

// TestSkillsDetail_OpensViaHandlePanelKey — Enter on a /skills panel row
// (with the host's catalogue) opens the detail view, and Esc returns to
// the list. The panel-level router handles both transitions.
func TestSkillsDetail_OpensViaHandlePanelKey(t *testing.T) {
	p := writeSkillMD(t, "# T\n\nbody.\n")
	withSyntheticCatalog(t, []jsonSkillInfo{{
		Name: "demo", Description: "d", Path: p,
	}})
	m := &runMode{}
	m.panel = slashPanel{kind: panelregistry.Skills, cmd: "/skills"}
	m.height = 50
	m.width = 80
	m.skillsPanel().SetCursor(0)

	if h, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEnter}); !h {
		t.Fatal("enter not handled")
	}
	if !m.skillsPanel().Detail() {
		t.Fatal("enter should open the detail view")
	}
	if h, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEsc}); !h {
		t.Fatal("esc not handled")
	}
	if m.skillsPanel().Detail() || m.panel.kind != panelregistry.Skills {
		t.Fatalf("esc should return to the list: detail=%v kind=%v",
			m.skillsPanel().Detail(), m.panel.kind)
	}
}
