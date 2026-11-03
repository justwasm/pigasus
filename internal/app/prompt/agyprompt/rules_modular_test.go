package agyprompt

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A rules/ directory holds modular rules: every immediate *.md child that
// declares a trigger in its frontmatter is a rule (official-docs/rules.md:21,
// :32, :39). A file nested in a subdirectory is ignored unless a rules.json
// registers it (official-docs/rules.md:32).
func TestDiscoverUserRules_ModularRulesDirectory(t *testing.T) {
	_, appData := ruleHome(t)
	ws := t.TempDir()
	rulesDir := filepath.Join(ws, ".agents", "rules")

	writeFile(t, filepath.Join(rulesDir, "typescript.md"),
		"---\ntrigger: always_on\ndescription: the typescript rules\n---\n\nuse strict\n")
	writeFile(t, filepath.Join(rulesDir, "frontend", "react.md"),
		"---\ntrigger: always_on\n---\n\nnested rule\n")

	got := labels(DiscoverUserRules(ws, appData))
	want := filepath.Join(rulesDir, "typescript.md")
	found := false
	for _, l := range got {
		if l == want {
			found = true
		}
		if strings.Contains(l, "react.md") {
			t.Errorf("a nested rule was injected: %q", l)
		}
	}
	if !found {
		t.Errorf("the modular rule was not discovered: %q", got)
	}
}

// The two fixed names are never modular rules: the reference treats their
// whole content as plain Markdown and keeps them continuously active
// (official-docs/rules.md:38) — even inside a rules/ directory, and even when
// they carry a frontmatter.
func TestDiscoverUserRules_FixedNamesIgnoreFrontmatter(t *testing.T) {
	_, appData := ruleHome(t)
	ws := t.TempDir()
	rulesDir := filepath.Join(ws, ".agents", "rules")

	writeFile(t, filepath.Join(rulesDir, "AGENTS.md"),
		"---\ntrigger: manual\n---\n\nthe agents body\n")

	rules := DiscoverUserRules(ws, appData)
	if len(rules) != 1 {
		t.Fatalf("discovered %q", labels(rules))
	}
	if rules[0].Trigger != "" {
		t.Errorf("trigger %q, want none", rules[0].Trigger)
	}
	if !strings.Contains(rules[0].Content, "the agents body") {
		t.Errorf("content %q", rules[0].Content)
	}
}

// A file in a rules/ directory that omits the frontmatter, or that names a
// trigger the reference does not know, is silently discarded
// (official-docs/rules.md:43).
func TestDiscoverUserRules_DiscardsInvalidModularRules(t *testing.T) {
	_, appData := ruleHome(t)
	ws := t.TempDir()
	rulesDir := filepath.Join(ws, ".agents", "rules")

	writeFile(t, filepath.Join(rulesDir, "no-frontmatter.md"), "just markdown\n")
	writeFile(t, filepath.Join(rulesDir, "camel.md"),
		"---\ntrigger: alwaysOn\n---\n\ncamel case trigger\n")
	writeFile(t, filepath.Join(rulesDir, "camel-decision.md"),
		"---\ntrigger: modelDecision\n---\n\ncamel case trigger\n")
	writeFile(t, filepath.Join(rulesDir, "no-trigger.md"),
		"---\ndescription: no trigger at all\n---\n\nno trigger\n")
	writeFile(t, filepath.Join(rulesDir, "broken.md"),
		"---\ntrigger always_on\n---\n\nmalformed frontmatter\n")

	got := labels(DiscoverUserRules(ws, appData))
	for _, l := range got {
		for _, name := range []string{"no-frontmatter.md", "camel.md", "camel-decision.md", "no-trigger.md", "broken.md"} {
			if strings.HasSuffix(l, name) {
				t.Errorf("%s was injected: %q", name, l)
			}
		}
	}
}

// The four triggers the reference knows, and the parse of each of them.
func TestParseRuleFrontmatter_Triggers(t *testing.T) {
	cases := []struct {
		trigger string
		want    RuleTrigger
	}{
		{"always_on", RuleTriggerAlwaysOn},
		{"model_decision", RuleTriggerModelDecision},
		{"glob", RuleTriggerGlob},
		{"manual", RuleTriggerManual},
	}
	for _, tc := range cases {
		body := "---\ntrigger: " + tc.trigger + "\ndescription: a description\n---\n\nthe body\n"
		fm, rest, err := parseRuleFrontmatter([]byte(body))
		if err != nil {
			t.Errorf("%s: %v", tc.trigger, err)
			continue
		}
		if fm.Trigger != tc.want {
			t.Errorf("%s: trigger %q, want %q", tc.trigger, fm.Trigger, tc.want)
		}
		if fm.Description != "a description" {
			t.Errorf("%s: description %q", tc.trigger, fm.Description)
		}
		if rest != "\nthe body\n" {
			t.Errorf("%s: body %q", tc.trigger, rest)
		}
	}
}

// A file without a frontmatter block is not a modular rule; a file whose
// frontmatter does not parse is malformed, not absent.
func TestParseRuleFrontmatter_NoFrontmatter(t *testing.T) {
	_, _, err := parseRuleFrontmatter([]byte("just markdown\n"))
	if !errors.Is(err, errRuleNoFrontmatter) {
		t.Errorf("a plain file: got %v, want errRuleNoFrontmatter", err)
	}
	_, _, err = parseRuleFrontmatter([]byte("---\ntrigger: always_on\n"))
	if !errors.Is(err, errRuleNoFrontmatter) {
		t.Errorf("an unclosed frontmatter: got %v, want errRuleNoFrontmatter", err)
	}
	_, _, err = parseRuleFrontmatter([]byte("---\ntrigger always_on\n---\nbody\n"))
	if err == nil || errors.Is(err, errRuleNoFrontmatter) {
		t.Errorf("a malformed frontmatter: got %v, want a parse error", err)
	}
}

// The globs are the rule's own comma-separated list, and the singular `glob`
// key is accepted beside `globs` (official-docs/rules.md:53, :61).
func TestParseRuleFrontmatter_Globs(t *testing.T) {
	fm, _, err := parseRuleFrontmatter([]byte("---\ntrigger: glob\nglobs: \"*.ts, *.tsx\"\n---\nbody\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(fm.Globs) != 2 || fm.Globs[0] != "*.ts" || fm.Globs[1] != "*.tsx" {
		t.Errorf("globs %q", fm.Globs)
	}

	fm, _, err = parseRuleFrontmatter([]byte("---\ntrigger: glob\nglob: \"*.py\"\n---\nbody\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(fm.Globs) != 1 || fm.Globs[0] != "*.py" {
		t.Errorf("globs %q", fm.Globs)
	}
}

// An @[label](path) include is inlined before the size limits are applied,
// and the frontmatter of the included file is stripped
// (official-docs/rules.md:106, :108).
func TestRuleIncludes_AreExpanded(t *testing.T) {
	_, appData := ruleHome(t)
	ws := t.TempDir()
	rulesDir := filepath.Join(ws, ".agents", "rules")

	writeFile(t, filepath.Join(rulesDir, "shared.md"),
		"---\ntrigger: always_on\n---\n\nshared body\n")
	writeFile(t, filepath.Join(ws, "docs", "standards.md"), "api standards\n")
	writeFile(t, filepath.Join(rulesDir, "main.md"),
		"---\ntrigger: always_on\n---\n\nFollow @[Shared](shared.md) and @[API Design Standards](../../docs/standards.md).\n")

	rules := DiscoverUserRules(ws, appData)
	var main *UserRule
	for i := range rules {
		if rules[i].Path == filepath.Join(rulesDir, "main.md") {
			main = &rules[i]
		}
	}
	if main == nil {
		t.Fatalf("the rule was not discovered: %q", labels(rules))
	}
	if !strings.Contains(main.Content, "shared body") {
		t.Errorf("the include was not expanded:\n%s", main.Content)
	}
	if strings.Contains(main.Content, "trigger:") {
		t.Errorf("the frontmatter of the included file was not stripped:\n%s", main.Content)
	}
	if !strings.Contains(main.Content, "api standards") {
		t.Errorf("the include outside the rules directory was not expanded:\n%s", main.Content)
	}
	if strings.Contains(main.Content, "@[") {
		t.Errorf("an include survived the expansion:\n%s", main.Content)
	}
}

// A reference to a file that cannot be read stays in the rule: the rule keeps
// its text and the turn is not broken.
func TestRuleIncludes_MissingFileStaysLiteral(t *testing.T) {
	_, appData := ruleHome(t)
	ws := t.TempDir()
	rulesDir := filepath.Join(ws, ".agents", "rules")

	writeFile(t, filepath.Join(rulesDir, "main.md"),
		"---\ntrigger: always_on\n---\n\nsee @[Gone](missing.md)\n")

	rules := DiscoverUserRules(ws, appData)
	if len(rules) != 1 {
		t.Fatalf("discovered %q", labels(rules))
	}
	if !strings.Contains(rules[0].Content, "@[Gone](missing.md)") {
		t.Errorf("the missing include was not kept:\n%s", rules[0].Content)
	}
}

// The rules.json registers the rules that live outside a rules/ directory and
// the nested ones inside it (official-docs/rules.md:120-131).
func TestRuleManifest_Entries(t *testing.T) {
	_, appData := ruleHome(t)
	ws := t.TempDir()

	shared := filepath.Join(ws, "shared-rules")
	writeFile(t, filepath.Join(shared, "one.md"), "---\ntrigger: always_on\n---\n\none\n")
	writeFile(t, filepath.Join(shared, "two.md"), "---\ntrigger: always_on\n---\n\ntwo\n")
	writeFile(t, filepath.Join(shared, "legacy", "old.md"), "---\ntrigger: always_on\n---\n\nold\n")

	nested := filepath.Join(ws, "nested-rules")
	writeFile(t, filepath.Join(nested, "other.md"), "---\ntrigger: always_on\n---\n\nother\n")
	writeFile(t, filepath.Join(nested, "frontend", "react.md"), "---\ntrigger: always_on\n---\n\nreact\n")

	writeFile(t, filepath.Join(ws, ".agents", "rules.json"), `{
	  "entries": [
	    { "path": "shared-rules", "exclude": ["two.md", "legacy/"] },
	    { "path": "nested-rules", "include_only": ["frontend/react.md"] }
	  ]
	}`)

	got := labels(DiscoverUserRules(ws, appData))
	want := []string{
		filepath.Join(shared, "one.md"),
		filepath.Join(nested, "frontend", "react.md"),
	}
	for _, w := range want {
		found := false
		for _, l := range got {
			if l == w {
				found = true
			}
		}
		if !found {
			t.Errorf("%s was not registered: %q", w, got)
		}
	}
	for _, l := range got {
		for _, name := range []string{"two.md", "old.md", "other.md"} {
			if strings.HasSuffix(l, name) {
				t.Errorf("%s was registered but excluded: %q", name, l)
			}
		}
	}
}

// An inherited manifest contributes its own entries, and the inheriting
// entry's filters apply to them (official-docs/rules.md:124-126).
func TestRuleManifest_Inherits(t *testing.T) {
	_, appData := ruleHome(t)
	ws := t.TempDir()

	base := filepath.Join(ws, "base")
	writeFile(t, filepath.Join(base, "inherited.md"), "---\ntrigger: always_on\n---\n\ninherited\n")
	writeFile(t, filepath.Join(base, "dropped.md"), "---\ntrigger: always_on\n---\n\ndropped\n")
	writeFile(t, filepath.Join(base, "rules.json"), `{
	  "entries": [ { "path": "." } ]
	}`)

	writeFile(t, filepath.Join(ws, ".agents", "rules.json"), `{
	  "inherits": [ { "path": "../base/rules.json", "exclude": ["dropped.md"] } ]
	}`)

	got := labels(DiscoverUserRules(ws, appData))
	found := false
	for _, l := range got {
		if l == filepath.Join(base, "inherited.md") {
			found = true
		}
		if strings.HasSuffix(l, "dropped.md") {
			t.Errorf("an excluded inherited rule was registered: %q", l)
		}
	}
	if !found {
		t.Errorf("the inherited rule was not registered: %q", got)
	}
}

// The aggregate rules budget is 20,000 tokens, and a rule body is charged
// len(content)/4 tokens (official-docs/rules.md:139).
func TestRulesBudget_IsTokens(t *testing.T) {
	exact := UserRule{Path: "/exact.md", Content: strings.Repeat("x", userRulesTotalBudget*4)}
	over := UserRule{Path: "/over.md", Content: strings.Repeat("x", userRulesTotalBudget*4+4)}

	inline, demoted := splitRuleBudget([]UserRule{exact, over})
	if len(inline) != 1 || inline[0].Path != "/exact.md" {
		t.Errorf("inline %q, want the rule that fits exactly", labels(inline))
	}
	if len(demoted) != 1 || demoted[0].Path != "/over.md" {
		t.Errorf("demoted %q, want the rule that does not fit", labels(demoted))
	}

	// Two rules of half the budget each still fit.
	half := UserRule{Path: "/half.md", Content: strings.Repeat("x", userRulesTotalBudget*2)}
	inline, demoted = splitRuleBudget([]UserRule{half, half})
	if len(inline) != 2 || len(demoted) != 0 {
		t.Errorf("inline %q, demoted %q, want both inlined", labels(inline), labels(demoted))
	}
}

// A model_decision rule contributes its path and its description to the
// prompt and nothing else (official-docs/rules.md:60, :67).
func TestRenderUserRules_ModelDecisionIsAPointer(t *testing.T) {
	body := renderUserRules([]UserRule{{
		Path:        "/w/.agents/rules/db.md",
		Trigger:     RuleTriggerModelDecision,
		Description: "apply these guidelines when writing migrations",
		Content:     "the full body must not be inlined",
	}})
	if strings.Contains(body, "the full body must not be inlined") {
		t.Errorf("the body of a model_decision rule was inlined:\n%s", body)
	}
	if !strings.Contains(body, "/w/.agents/rules/db.md: apply these guidelines when writing migrations") {
		t.Errorf("the pointer to the rule is missing:\n%s", body)
	}
}

// The model_decision rule is discovered with its trigger, and its body stays
// out of the inline set.
func TestDiscoverUserRules_ModelDecision(t *testing.T) {
	_, appData := ruleHome(t)
	ws := t.TempDir()
	path := filepath.Join(ws, ".agents", "rules", "db.md")

	writeFile(t, path, "---\ntrigger: model_decision\ndescription: db rules\n---\n\nthe body\n")

	rules := DiscoverUserRules(ws, appData)
	if len(rules) != 1 {
		t.Fatalf("discovered %q", labels(rules))
	}
	if rules[0].Trigger != RuleTriggerModelDecision {
		t.Errorf("trigger %q", rules[0].Trigger)
	}
	if rules[0].Description != "db rules" {
		t.Errorf("description %q", rules[0].Description)
	}
	if rules[0].Content != "" {
		t.Errorf("the body was inlined: %q", rules[0].Content)
	}
}

// A manual rule is never loaded automatically (official-docs/rules.md:97).
func TestDiscoverUserRules_ManualIsNeverLoaded(t *testing.T) {
	_, appData := ruleHome(t)
	ws := t.TempDir()

	writeFile(t, filepath.Join(ws, ".agents", "rules", "release.md"),
		"---\ntrigger: manual\ndescription: the release checklist\n---\n\nthe checklist\n")

	if got := labels(DiscoverUserRules(ws, appData)); len(got) != 0 {
		t.Errorf("a manual rule was injected: %q", got)
	}
}

// A glob rule activates when the agent touches a file the rule's patterns
// match (official-docs/rules.md:83-95).
func TestGetTriggeredMemories_GlobRule(t *testing.T) {
	_, appData := ruleHome(t)
	ws := t.TempDir()
	writeFile(t, filepath.Join(ws, ".agents", "rules", "proto.md"),
		"---\ntrigger: glob\nglobs: \"*.proto, **/*.pb.go\"\ndescription: protobuf rules\n---\n\nnever reuse a field number\n")
	writeFile(t, filepath.Join(ws, "api", "schema.proto"), "syntax = \"proto3\";\n")
	writeFile(t, filepath.Join(ws, "api", "schema.pb.go"), "package api\n")
	writeFile(t, filepath.Join(ws, "api", "main.go"), "package api\n")

	// The glob rule is not part of the prompt.
	if got := labels(DiscoverUserRules(ws, appData)); len(got) != 0 {
		t.Errorf("a glob rule was injected: %q", got)
	}

	mem := GetTriggeredMemories(ws, filepath.Join(ws, "api", "schema.proto"))
	if !strings.Contains(mem, "never reuse a field number") {
		t.Errorf("the glob rule did not fire:\n%s", mem)
	}

	mem = GetTriggeredMemories(ws, filepath.Join(ws, "api", "schema.pb.go"))
	if !strings.Contains(mem, "never reuse a field number") {
		t.Errorf("the ** pattern did not fire:\n%s", mem)
	}

	if got := GetTriggeredMemories(ws, filepath.Join(ws, "api", "main.go")); got != "" {
		t.Errorf("a file that matches no pattern triggered the rule:\n%s", got)
	}
}

// The global and the plugin rule roots of the CLI
// (official-docs/rules.md:169-170).
func TestDiscoverUserRules_GlobalAndPluginRoots(t *testing.T) {
	gemini, appData := ruleHome(t)
	ws := t.TempDir()

	// The modular global rule roots.
	writeFile(t, filepath.Join(gemini, "config", "rules", "global.md"),
		"---\ntrigger: always_on\n---\n\nglobal modular rule\n")
	writeFile(t, filepath.Join(appData, "rules", "cli.md"),
		"---\ntrigger: always_on\n---\n\ncli modular rule\n")

	// The plugin rule roots: a plugin is a directory with a plugin.json.
	writeFile(t, filepath.Join(gemini, "config", "plugins", "beta", "plugin.json"), `{"name":"beta"}`)
	writeFile(t, filepath.Join(gemini, "config", "plugins", "beta", "rules", "plugin.md"),
		"---\ntrigger: always_on\n---\n\nbeta plugin rule\n")
	writeFile(t, filepath.Join(appData, "plugins", "alpha", "plugin.json"), `{"name":"alpha"}`)
	writeFile(t, filepath.Join(appData, "plugins", "alpha", "rules", "plugin.md"),
		"---\ntrigger: always_on\n---\n\nalpha plugin rule\n")

	got := labels(DiscoverUserRules(ws, appData))
	want := []string{
		filepath.Join(gemini, "config", "plugins", "beta", "rules", "plugin.md"),
		filepath.Join(appData, "plugins", "alpha", "rules", "plugin.md"),
		filepath.Join(gemini, "config", "rules", "global.md"),
		filepath.Join(appData, "rules", "cli.md"),
	}
	if len(got) != len(want) {
		t.Fatalf("discovered %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("rule %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

// A workspace without a rules/ directory and without an AGENTS.md carries no
// rule at all: the prompt's bytes must not change.
func TestDiscoverUserRules_EmptyWorkspace(t *testing.T) {
	_, appData := ruleHome(t)
	ws := t.TempDir()
	if got := DiscoverUserRules(ws, appData); len(got) != 0 {
		t.Errorf("discovered %q", labels(got))
	}
}

// The rules of a plugin that is disabled in the user config are not loaded.
func TestDiscoverUserRules_DisabledPluginRule(t *testing.T) {
	gemini, appData := ruleHome(t)
	ws := t.TempDir()

	writeFile(t, filepath.Join(gemini, "config", "plugins", "gamma", "plugin.json"), `{"name":"gamma"}`)
	writeFile(t, filepath.Join(gemini, "config", "plugins", "gamma", "rules", "plugin.md"),
		"---\ntrigger: always_on\n---\n\ngamma plugin rule\n")
	writeFile(t, filepath.Join(gemini, "config", "config.json"),
		`{"plugins":{"gamma":{"enabled":false}}}`)

	if got := labels(DiscoverUserRules(ws, appData)); len(got) != 0 {
		t.Errorf("a disabled plugin contributed a rule: %q", got)
	}
}

// A rule file that cannot be read is skipped, not fatal.
func TestDiscoverUserRules_UnreadableFile(t *testing.T) {
	_, appData := ruleHome(t)
	ws := t.TempDir()

	path := filepath.Join(ws, ".agents", "rules", "locked.md")
	writeFile(t, path, "---\ntrigger: always_on\n---\n\nbody\n")
	if err := os.Chmod(path, 0o000); err != nil {
		t.Skipf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })

	// The file is unreadable: it is skipped. The test only asserts that the
	// walk does not fail.
	_ = DiscoverUserRules(ws, appData)
}
