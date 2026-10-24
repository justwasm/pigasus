package agyprompt

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// ruleHome builds a fake ~/.gemini tree and returns (geminiDir, appDataDir).
func ruleHome(t *testing.T) (string, string) {
	t.Helper()
	gemini := filepath.Join(t.TempDir(), ".gemini")
	appData := filepath.Join(gemini, "antigravity-cli")
	if err := os.MkdirAll(appData, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	return gemini, appData
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// labels returns the <RULE[...]> labels of the rendered section, in order.
func labels(rules []UserRule) []string {
	out := make([]string, 0, len(rules))
	for _, r := range rules {
		l := r.Label
		if l == "" {
			l = r.Path
		}
		out = append(out, l)
	}
	return out
}

func TestDiscoverUserRules_OrderAndLabels(t *testing.T) {
	gemini, appData := ruleHome(t)
	ws := filepath.Join(t.TempDir(), "ws")

	writeFile(t, filepath.Join(gemini, "AGENTS.md"), "home agents")
	writeFile(t, filepath.Join(gemini, "GEMINI.md"), "home gemini")
	writeFile(t, filepath.Join(gemini, "config", "AGENTS.md"), "config agents")
	writeFile(t, filepath.Join(gemini, "config", "GEMINI.md"), "config gemini")
	writeFile(t, filepath.Join(gemini, "config", "rules", "AGENTS.md"), "global rules")
	writeFile(t, filepath.Join(gemini, "config", "rules", "GEMINI.md"), "global rules gemini")
	writeFile(t, filepath.Join(ws, "AGENTS.md"), "ws agents")
	writeFile(t, filepath.Join(ws, "GEMINI.md"), "ws gemini")
	writeFile(t, filepath.Join(ws, ".agents", "rules", "AGENTS.md"), "ws rules agents")
	writeFile(t, filepath.Join(ws, ".agents", "rules", "GEMINI.md"), "ws rules gemini")

	got := labels(DiscoverUserRules(ws, appData))

	want := []string{
		"user_global", "user_global", "user_global", "user_global",
		filepath.Join(gemini, "config", "rules", "AGENTS.md"),
		filepath.Join(gemini, "config", "rules", "GEMINI.md"),
		filepath.Join(ws, "AGENTS.md"),
		filepath.Join(ws, "GEMINI.md"),
		filepath.Join(ws, ".agents", "rules", "AGENTS.md"),
		filepath.Join(ws, ".agents", "rules", "GEMINI.md"),
	}
	if len(got) != len(want) {
		t.Fatalf("discovered %d rules, want %d:\n got %q\nwant %q", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("rule %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

// The home-level files are the only ones the reference labels "user_global";
// every other scope prints its own path.
func TestDiscoverUserRules_HomeFilesAreLabelledUserGlobal(t *testing.T) {
	gemini, appData := ruleHome(t)
	ws := t.TempDir()
	writeFile(t, filepath.Join(gemini, "AGENTS.md"), "home")
	writeFile(t, filepath.Join(gemini, "config", "AGENTS.md"), "config")
	writeFile(t, filepath.Join(ws, "AGENTS.md"), "ws")

	rules := DiscoverUserRules(ws, appData)
	byPath := map[string]UserRule{}
	for _, r := range rules {
		byPath[r.Path] = r
	}
	for _, p := range []string{
		filepath.Join(gemini, "AGENTS.md"),
		filepath.Join(gemini, "config", "AGENTS.md"),
	} {
		r, ok := byPath[p]
		if !ok {
			t.Fatalf("%s not discovered", p)
		}
		if r.Label != userGlobalLabel {
			t.Errorf("%s labelled %q, want %q", p, r.Label, userGlobalLabel)
		}
	}
	wsRule, ok := byPath[filepath.Join(ws, "AGENTS.md")]
	if !ok {
		t.Fatalf("workspace AGENTS.md not discovered")
	}
	if wsRule.Label != wsRule.Path {
		t.Errorf("workspace rule labelled %q, want its path", wsRule.Label)
	}
}

// A plugin contributes its rules only when it is a plugin: a parseable
// plugin.json and not disabled in the user config.json.
func TestDiscoverUserRules_PluginRules(t *testing.T) {
	gemini, appData := ruleHome(t)
	ws := t.TempDir()
	pluginsRoot := filepath.Join(gemini, "config", "plugins")

	// loaded: parseable plugin.json, no config.json entry
	writeFile(t, filepath.Join(pluginsRoot, "alpha", "plugin.json"), `{"name":"alpha"}`)
	writeFile(t, filepath.Join(pluginsRoot, "alpha", "rules", "AGENTS.md"), "alpha rules")
	writeFile(t, filepath.Join(pluginsRoot, "alpha", "rules", "GEMINI.md"), "alpha rules gemini")

	// no plugin.json: not a plugin
	writeFile(t, filepath.Join(pluginsRoot, "beta", "rules", "AGENTS.md"), "beta rules")

	// disabled
	writeFile(t, filepath.Join(pluginsRoot, "gamma", "plugin.json"), `{"name":"gamma"}`)
	writeFile(t, filepath.Join(pluginsRoot, "gamma", "rules", "AGENTS.md"), "gamma rules")
	writeFile(t, filepath.Join(gemini, "config", "config.json"),
		`{"plugins":{"gamma":{"enabled":false}}}`)

	got := labels(DiscoverUserRules(ws, appData))
	want := []string{
		filepath.Join(pluginsRoot, "alpha", "rules", "AGENTS.md"),
		filepath.Join(pluginsRoot, "alpha", "rules", "GEMINI.md"),
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

// Only AGENTS.md and GEMINI.md are taken from a rules directory; any other
// file name in the same directory is ignored.
func TestDiscoverUserRules_IgnoresOtherFileNames(t *testing.T) {
	gemini, appData := ruleHome(t)
	ws := t.TempDir()
	writeFile(t, filepath.Join(gemini, "config", "rules", "PROBE.md"), "probe")
	writeFile(t, filepath.Join(ws, ".agents", "rules", "PROBE.md"), "probe")
	writeFile(t, filepath.Join(ws, ".agents", "rules", "AGENTS.md"), "keep")

	got := labels(DiscoverUserRules(ws, appData))
	for _, l := range got {
		if strings.Contains(l, "PROBE.md") {
			t.Errorf("PROBE.md was injected: %q", l)
		}
	}
	if len(got) != 1 || !strings.HasSuffix(got[0], filepath.Join(".agents", "rules", "AGENTS.md")) {
		t.Errorf("got %q, want only .agents/rules/AGENTS.md", got)
	}
}

func TestRenderUserRules_PreambleAndDelimiters(t *testing.T) {
	body := renderUserRules([]UserRule{
		{Path: "/p/one.md", Content: "one"},
		{Path: "/p/two.md", Label: userGlobalLabel, Content: "two"},
	})
	if !strings.Contains(body, "These rules take precedence over any other instructions.") {
		t.Errorf("preamble is not the reference's 1.4.5 wording:\n%s", body)
	}
	if strings.Contains(body, "any following instructions") {
		t.Errorf("preamble still carries the 1.3.x–1.4.3 wording")
	}
	if !strings.Contains(body, "<RULE[/p/one.md]>\none\n</RULE[/p/one.md]>") {
		t.Errorf("path delimiter missing:\n%s", body)
	}
	if !strings.Contains(body, "<RULE[user_global]>\ntwo\n</RULE[user_global]>") {
		t.Errorf("user_global delimiter missing:\n%s", body)
	}
}

func TestRenderUserRules_EmptyIsEmpty(t *testing.T) {
	if got := renderUserRules(nil); got != "" {
		t.Errorf("empty rules rendered %q", got)
	}
}

// A custom agent's prompt carries the same <user_rules> section as the
// default prompt, between <user_information> and <skills>.
func TestBuildAgent_CarriesUserRules(t *testing.T) {
	o := DefaultOptions("/w", "/a", "id")
	o.UserRules = []UserRule{{Path: "/w/AGENTS.md", Content: "be careful"}}

	got := BuildAgent("body", o)
	at := strings.Index(got, "<user_rules>")
	if at < 0 {
		t.Fatalf("custom-agent prompt has no <user_rules> section:\n%s", got)
	}
	if ui := strings.Index(got, "<user_information>"); ui < 0 || ui > at {
		t.Errorf("<user_rules> must follow <user_information>")
	}
	if sk := strings.Index(got, "<skills>"); sk < 0 || sk < at {
		t.Errorf("<user_rules> must precede <skills>")
	}
	if !strings.Contains(got, "<RULE[/w/AGENTS.md]>") {
		t.Errorf("rule delimiter missing:\n%s", got)
	}
}

func TestBuildAgent_OmitsUserRulesWhenEmpty(t *testing.T) {
	got := BuildAgent("body", DefaultOptions("/w", "/a", "id"))
	if strings.Contains(got, "<user_rules>") {
		t.Errorf("custom-agent prompt must not carry an empty <user_rules> section")
	}
}

// The live 1.4.5 <user_rules> block, captured from a real request body
// (testdata/user_rules_1.4.5.txt). The rule's own path is machine-specific,
// so the test renders the captured rule at a sentinel path and compares the
// two byte for byte.
func TestRenderUserRules_MatchesLiveCapture(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "user_rules_1.4.5.txt"))
	if err != nil {
		t.Fatalf("read capture: %v", err)
	}
	fixture := string(raw)

	re := regexp.MustCompile(`(?s)<RULE\[(.*?)\]>\n(.*?)\n</RULE\[.*?\]>`)
	m := re.FindStringSubmatch(fixture)
	if m == nil {
		t.Fatalf("capture has no <RULE[...]> block:\n%s", fixture)
	}
	rulePath, content := m[1], m[2]

	const sentinel = "/SENTINEL/RULES/AGENTS.md"
	want := strings.ReplaceAll(fixture, rulePath, sentinel)

	got := "<user_rules>" + renderUserRules([]UserRule{{Path: sentinel, Content: content}}) + "</user_rules>"
	if got != want {
		t.Errorf("rendered block differs from the 1.4.5 capture:\n got %q\nwant %q", got, want)
	}
}
