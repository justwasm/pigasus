package cli

// plugin_validate_reference_test.go — pins `agy plugin validate` to the
// behaviour observed from the live 1.4.3 binary (see
// docs/cycles/plugin-validate-components/deliverable.md). The old cycle-238
// tests pinned a fabricated schema; these replace them.

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runValidateRef drives runPluginValidate with captured buffers and an exit
// code extracted from the sentinel error.
func runValidateRef(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var out, errb bytes.Buffer
	j := NewJetski("agy", "1.4.3")
	j.Out = &out
	j.ErrOut = &errb
	err := runPluginValidate(j, args)
	if err != nil {
		ec, ok := err.(*exitCodeError)
		if !ok {
			t.Fatalf("unexpected error type %T: %v", err, err)
		}
		code = ec.code
	}
	return out.String(), errb.String(), code
}

func writeValidateFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func newValidatePlugin(t *testing.T, manifest string) string {
	t.Helper()
	root := t.TempDir()
	writeValidateFile(t, filepath.Join(root, "plugin.json"), manifest)
	return root
}

func TestPluginValidate_SkillFindings(t *testing.T) {
	root := newValidatePlugin(t, `{"name":"plug","description":"d"}`)
	writeValidateFile(t, filepath.Join(root, "skills", "ok", "SKILL.md"),
		"---\nname: ok\ndescription: fine\n---\nbody\n")
	writeValidateFile(t, filepath.Join(root, "skills", "nofm", "SKILL.md"), "no frontmatter\n")
	writeValidateFile(t, filepath.Join(root, "skills", "noname", "SKILL.md"),
		"---\ndescription: d\n---\nbody\n")
	writeValidateFile(t, filepath.Join(root, "skills", "nodesc", "SKILL.md"),
		"---\nname: nodesc\n---\nbody\n")
	writeValidateFile(t, filepath.Join(root, "skills", "folder", "SKILL.md"),
		"---\nname: other\ndescription: d\n---\nbody\n")
	writeValidateFile(t, filepath.Join(root, "skills", "one", "SKILL.md"),
		"---\nname: same\ndescription: d\n---\nbody\n")
	writeValidateFile(t, filepath.Join(root, "skills", "two", "SKILL.md"),
		"---\nname: same\ndescription: d\n---\nbody\n")
	writeValidateFile(t, filepath.Join(root, "skills", "big", "SKILL.md"),
		"---\nname: big\ndescription: d\n---\n"+strings.Repeat("a", 24001))

	out, _, code := runValidateRef(t, root)
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	for _, want := range []string{
		"✗ skills/nofm/SKILL.md: no frontmatter: the file must start with a --- block holding name and description. The loader skips this skill",
		`⚠ skills/noname/SKILL.md: no name in the frontmatter: the skill is named "SKILL" after its file. Add name: noname`,
		"⚠ skills/nodesc/SKILL.md: no description: the model uses it to decide when to load the skill",
		`⚠ skills/folder/SKILL.md: name "other" doesn't match its folder "folder"`,
		`⚠ skills/two/SKILL.md: skipped: skills/one/SKILL.md already defines a skill named "same"`,
		"⚠ skills/big/SKILL.md: the body is 24001 bytes; only the first 24000 are loaded",
		"skills 6 · agents 0 · rules 0 · hooks 0 · mcpServers 0 · sidecars 0",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestPluginValidate_AgentFindings(t *testing.T) {
	root := newValidatePlugin(t, `{"name":"plug","description":"d"}`)
	writeValidateFile(t, filepath.Join(root, "agents", "good.md"),
		"---\nname: good\ndescription: d\n---\nbody\n")
	writeValidateFile(t, filepath.Join(root, "agents", "noname.md"),
		"---\ndescription: d\n---\nbody\n")
	writeValidateFile(t, filepath.Join(root, "agents", "nodesc.md"),
		"---\nname: nodesc\n---\nbody\n")
	writeValidateFile(t, filepath.Join(root, "agents", "nofm.md"), "body\n")
	writeValidateFile(t, filepath.Join(root, "agents", "reserved.md"),
		"---\nname: review-agent\ndescription: d\n---\nbody\n")
	writeValidateFile(t, filepath.Join(root, "agents", "helper", "agent.json"),
		`{"name":"helper","description":"d"}`)
	writeValidateFile(t, filepath.Join(root, "agents", "broken", "agent.json"), `{bad json`)

	out, _, code := runValidateRef(t, root)
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	for _, want := range []string{
		"✗ agents/noname.md: the frontmatter has no name. The loader skips this agent",
		"✗ agents/nodesc.md: the frontmatter has no description. The loader skips this agent",
		"⚠ agents/nofm.md: never loaded: an agent file starts with frontmatter that has a name and description",
		`✗ agents/reserved.md: the name "review-agent" is reserved for a built-in agent, so this agent never loads`,
		"⚠ agents/helper/agent.json: never loaded by this build: only internal builds read agent.json; write the agent as agent.md",
		"✗ agents/broken/agent.json: line 1, column 2: invalid literal: bad. The loader skips this agent",
		"agents 1 · rules 0",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestPluginValidate_RuleFindings(t *testing.T) {
	root := newValidatePlugin(t, `{"name":"plug","description":"d"}`)
	writeValidateFile(t, filepath.Join(root, "rules", "always.md"),
		"---\ntrigger: always_on\ndescription: d\n---\nrule\n")
	writeValidateFile(t, filepath.Join(root, "rules", "notrigger.md"),
		"---\ndescription: d\n---\nrule\n")
	writeValidateFile(t, filepath.Join(root, "rules", "nofm.md"), "rule\n")
	writeValidateFile(t, filepath.Join(root, "rules", "noglobs.md"),
		"---\ntrigger: glob\n---\nrule\n")
	writeValidateFile(t, filepath.Join(root, "rules", "nodesc.md"),
		"---\ntrigger: model_decision\n---\nrule\n")
	writeValidateFile(t, filepath.Join(root, "rules", "README.md"), "readme\n")
	writeValidateFile(t, filepath.Join(root, "rules", "big.md"),
		"---\ntrigger: always_on\n---\n"+strings.Repeat("a", 24001))

	out, _, code := runValidateRef(t, root)
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	for _, want := range []string{
		"✗ rules/notrigger.md: no valid trigger in the frontmatter, so the rule never applies. Set trigger to always_on, model_decision, glob, manual",
		"✗ rules/nofm.md: no valid trigger in the frontmatter",
		"⚠ rules/noglobs.md: a glob rule needs globs, or it never applies",
		"⚠ rules/nodesc.md: a model_decision rule needs a description: the model uses it to decide when to apply the rule",
		"⚠ rules/big.md: the content is cut by 24001 bytes to fit the 24000-byte limit",
		"rules 4 · hooks 0",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "rules/README.md") {
		t.Errorf("README.md without frontmatter must be skipped:\n%s", out)
	}
}

func TestPluginValidate_ManifestFindings(t *testing.T) {
	root := newValidatePlugin(t, `{"name":"bad name","bogus":1}`)
	out, _, _ := runValidateRef(t, root)
	for _, want := range []string{
		`⚠ plugin.json: unknown field "bogus" is ignored`,
		`⚠ plugin.json: name "bad name" has characters other than letters, digits, - and _, so ` + "`plugin install` refuses it",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}

	root = newValidatePlugin(t, `{"description":"d"}`)
	out, _, _ = runValidateRef(t, root)
	if !strings.Contains(out, "no name: the plugin is named after its folder") {
		t.Errorf("missing no-name warning in:\n%s", out)
	}
}

func TestPluginValidate_SummaryAndRootFiles(t *testing.T) {
	root := newValidatePlugin(t, `{"name":"plug","description":"d"}`)
	writeValidateFile(t, filepath.Join(root, "mcp_config.json"),
		`{"mcpServers":{"a":{"command":"echo"},"b":{"command":"echo"}}}`)
	writeValidateFile(t, filepath.Join(root, "hooks.json"),
		`{"PreToolUse":{"command":"true"},"Stop":{"command":"true"}}`)
	writeValidateFile(t, filepath.Join(root, "AGENTS.md"), "x\n")
	writeValidateFile(t, filepath.Join(root, "GEMINI.md"), "x\n")
	writeValidateFile(t, filepath.Join(root, "workflows", "w.md"), "x\n")

	out, _, code := runValidateRef(t, root)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (warnings are non-fatal)", code)
	}
	for _, want := range []string{
		"⚠ AGENTS.md: never loaded: plugins load rules only from rules/. Move it to rules/AGENTS.md",
		"⚠ GEMINI.md: never loaded: plugins load rules only from rules/. Move it to rules/GEMINI.md",
		"⚠ workflows: never loaded: plugins don't load Markdown workflows. Make each one a skill in skills/<name>/SKILL.md",
		"hooks 2 · mcpServers 2",
		"✔ Validation passed with 5 warnings",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestPluginValidate_JSONShape(t *testing.T) {
	root := newValidatePlugin(t, `{"name":"plug","description":"d"}`)
	writeValidateFile(t, filepath.Join(root, "skills", "s", "SKILL.md"),
		"---\nname: s\ndescription: d\n---\nbody\n")
	out, _, code := runValidateRef(t, root, "--json")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr=%q)", code, out)
	}
	var doc struct {
		Dir        string `json:"dir"`
		Manifest   string `json:"manifest"`
		Name       string `json:"name"`
		AgentSpec  bool   `json:"agentPluginsSpec"`
		Components struct {
			Agents, Hooks, MCPServers, Rules, Sidecars, Skills int
		} `json:"components"`
		Findings []struct {
			Severity, File, Message string
		} `json:"findings"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if doc.Manifest != "plugin.json" || doc.Name != "plug" || doc.Components.Skills != 1 {
		t.Errorf("unexpected report: %+v", doc)
	}
	if doc.Findings == nil {
		t.Errorf("findings must be [] not null: %s", out)
	}
}

func TestPluginValidate_AgentPluginsSchemaNote(t *testing.T) {
	root := newValidatePlugin(t, `{"name":"bad_name","$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json"}`)
	out, _, code := runValidateRef(t, root, "--json")
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	var doc struct {
		AgentSpec bool `json:"agentPluginsSpec"`
		Findings  []struct {
			Severity string
			Message  string
		} `json:"findings"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if !doc.AgentSpec {
		t.Error("agentPluginsSpec = false, want true")
	}
	sawNote := false
	for _, f := range doc.Findings {
		if f.Severity == "note" && strings.Contains(f.Message, "declares the Agent Plugins 1.0.0 $schema") {
			sawNote = true
		}
	}
	if !sawNote {
		t.Errorf("missing note finding: %+v", doc.Findings)
	}

	root = newValidatePlugin(t, `{"name":"plug","$schema":"https://agent-plugins.org/schemas/9.9.9/plugin.schema.json"}`)
	out, stderr, code := runValidateRef(t, root)
	if code != 1 {
		t.Fatalf("unsupported schema exit = %d, want 1", code)
	}
	if !strings.Contains(out, "unsupported Agent Plugins version") {
		t.Errorf("missing unsupported-version finding:\n%s", out)
	}
	if !strings.Contains(stderr, "validation failed: 1 error") {
		t.Errorf("stderr footer = %q", stderr)
	}
}

func TestPluginValidate_FooterRoutingAndStrict(t *testing.T) {
	pass := newValidatePlugin(t, `{"name":"pass"}`)
	out, errb, code := runValidateRef(t, pass)
	if code != 0 || !strings.Contains(out, "✔ Validation passed") || errb != "" {
		t.Errorf("pass: code=%d out=%q err=%q", code, out, errb)
	}

	warn := newValidatePlugin(t, `{"name":"warn","bogus":1}`)
	out, errb, code = runValidateRef(t, warn)
	if code != 0 || !strings.Contains(out, "✔ Validation passed with 1 warning") || errb != "" {
		t.Errorf("warn: code=%d out=%q err=%q", code, out, errb)
	}
	out, errb, code = runValidateRef(t, warn, "--strict")
	if code != 1 || !strings.Contains(errb, "Error: validation failed in strict mode: 1 warning") {
		t.Errorf("strict: code=%d out=%q err=%q", code, out, errb)
	}

	bad := newValidatePlugin(t, `{"name":"bad","$schema":"https://agent-plugins.org/schemas/9.9.9/plugin.schema.json"}`)
	out, errb, code = runValidateRef(t, bad)
	if code != 1 || !strings.Contains(errb, "Error: validation failed: 1 error, 0 warnings") {
		t.Errorf("error: code=%d out=%q err=%q", code, out, errb)
	}
	if strings.Contains(out, "Error: validation failed") {
		t.Errorf("failing footer must not be on stdout: %q", out)
	}
}

// TestPluginValidate_FlagSurface pins the reference's 1.4.6 behaviour:
// `--help` / `-h` print the usage block on stderr and exit 0, every
// other argument is an error.
//
// The probe that produced the first implementation concluded that
// `--help` was rejected like any other unknown argument. The live
// binary says otherwise:
//
//	$ agy plugin validate --help
//	Usage: agy plugin validate [path] [--strict] [--json]
//	… (exit 0)
func TestPluginValidate_FlagSurface(t *testing.T) {
	for _, arg := range []string{"--help", "-h"} {
		out, errb, code := runValidateRef(t, arg)
		if code != 0 {
			t.Errorf("%q: exit = %d, want 0", arg, code)
		}
		if !strings.HasPrefix(errb, "Usage: ") || !strings.Contains(errb, "plugin validate [path] [--strict] [--json]") {
			t.Errorf("%q: stderr = %q, want the usage block", arg, errb)
		}
		if out != "" {
			t.Errorf("%q: stdout should be empty, got %q", arg, out)
		}
	}
	for _, arg := range []string{"--wat", "extra"} {
		args := []string{arg}
		if arg == "extra" {
			args = []string{".", arg}
		}
		out, errb, code := runValidateRef(t, args...)
		if code != 1 {
			t.Errorf("%q: exit = %d, want 1", arg, code)
		}
		if !strings.Contains(errb, `unknown validate argument "`+arg+`": usage is validate [path] [--strict] [--json]. See validate --help`) {
			t.Errorf("%q: stderr = %q", arg, errb)
		}
		if out != "" {
			t.Errorf("%q: stdout should be empty, got %q", arg, out)
		}
	}
}

func TestPluginValidate_CapitalisedManifest(t *testing.T) {
	root := t.TempDir()
	writeValidateFile(t, filepath.Join(root, "Plugin.json"), `{"name":"caps"}`)
	out, _, code := runValidateRef(t, root)
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if !strings.Contains(out, "Plugin.json)") {
		t.Errorf("header should echo the actual manifest filename:\n%s", out)
	}
}

func TestPluginValidate_UnknownFieldSuggestions(t *testing.T) {
	root := newValidatePlugin(t, `{"name":"dym","descripton":"d","auther":"a","nam":"x","unknown_thing":1}`)
	out, _, _ := runValidateRef(t, root)
	for _, want := range []string{
		`unknown field "auther" is ignored (did you mean "author"?)`,
		`unknown field "descripton" is ignored (did you mean "description"?)`,
		`unknown field "nam" is ignored (did you mean "name"?)`,
		`unknown field "unknown_thing" is ignored`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, `unknown_thing" is ignored (did you mean`) {
		t.Errorf("distant field should get no suggestion:\n%s", out)
	}
}

func TestPluginValidate_ByteOrderMarks(t *testing.T) {
	const bom = "\ufeff"

	// Manifest BOM: a specific error, no name, no summary.
	root := newValidatePlugin(t, bom+`{"name":"bom"}`)
	out, stderr, code := runValidateRef(t, root)
	if code != 1 || !strings.Contains(out, "starts with a UTF-8 byte order mark") ||
		!strings.Contains(out, ". The loader skips this plugin") {
		t.Errorf("manifest BOM: code=%d out=%q err=%q", code, out, stderr)
	}
	if strings.Contains(out, "Validating bom (") {
		t.Errorf("BOM manifest should not name the plugin:\n%s", out)
	}

	// hooks.json BOM is a component error; mcp_config.json tolerates one.
	root = newValidatePlugin(t, `{"name":"bomh"}`)
	writeValidateFile(t, filepath.Join(root, "hooks.json"), bom+`{"PreToolUse":{"command":"true"}}`)
	writeValidateFile(t, filepath.Join(root, "mcp_config.json"), bom+`{"mcpServers":{"a":{"command":"echo"}}}`)
	out, _, code = runValidateRef(t, root)
	if code != 1 || !strings.Contains(out, "✗ hooks.json: starts with a UTF-8 byte order mark") {
		t.Errorf("hooks BOM: code=%d out=%q", code, out)
	}
	if !strings.Contains(out, "mcpServers 1") {
		t.Errorf("mcp_config.json BOM should be tolerated:\n%s", out)
	}

	// A skill tolerates a BOM; agents.json does not.
	root = newValidatePlugin(t, `{"name":"boms"}`)
	writeValidateFile(t, filepath.Join(root, "skills", "s", "SKILL.md"),
		bom+"---\nname: s\ndescription: d\n---\nbody\n")
	writeValidateFile(t, filepath.Join(root, "agents.json"), bom+`{"exclude":["x"]}`)
	out, _, _ = runValidateRef(t, root)
	if !strings.Contains(out, "skills 1") {
		t.Errorf("skill BOM should be tolerated:\n%s", out)
	}
	if !strings.Contains(out, "✗ agents.json: starts with a UTF-8 byte order mark") ||
		!strings.Contains(out, ". The loader ignores this file") {
		t.Errorf("agents.json BOM should error:\n%s", out)
	}
}

func TestPluginValidate_Exclusions(t *testing.T) {
	// skills.json excludes by folder: an excluded skill is not counted, its
	// semantic warnings are suppressed, but a parse error still shows.
	root := newValidatePlugin(t, `{"name":"ex"}`)
	writeValidateFile(t, filepath.Join(root, "skills", "a", "SKILL.md"), "no fm\n")
	writeValidateFile(t, filepath.Join(root, "skills", "b", "SKILL.md"),
		"---\nname: b\ndescription: d\n---\nbody\n")
	writeValidateFile(t, filepath.Join(root, "skills.json"), `{"exclude":["a"]}`)
	out, _, code := runValidateRef(t, root)
	if code != 1 || !strings.Contains(out, "✗ skills/a/SKILL.md: no frontmatter") || !strings.Contains(out, "skills 1") {
		t.Errorf("excluded skill parse error: code=%d out=%q", code, out)
	}

	root = newValidatePlugin(t, `{"name":"ex2"}`)
	writeValidateFile(t, filepath.Join(root, "skills", "a", "SKILL.md"),
		"---\nname: other\ndescription: d\n---\nbody\n")
	writeValidateFile(t, filepath.Join(root, "skills", "b", "SKILL.md"),
		"---\nname: b\ndescription: d\n---\nbody\n")
	writeValidateFile(t, filepath.Join(root, "skills.json"), `{"exclude":["a"]}`)
	out, _, _ = runValidateRef(t, root)
	if strings.Contains(out, "doesn't match its folder") {
		t.Errorf("excluded skill mismatch should be suppressed:\n%s", out)
	}
	if !strings.Contains(out, "skills 1") || !strings.Contains(out, "✔ Validation passed\n") {
		t.Errorf("excluded skill should pass cleanly:\n%s", out)
	}

	// rules.json excludes by file stem and suppresses trigger diagnostics.
	root = newValidatePlugin(t, `{"name":"exr"}`)
	writeValidateFile(t, filepath.Join(root, "rules", "a.md"), "---\ndescription: d\n---\nR\n")
	writeValidateFile(t, filepath.Join(root, "rules", "b.md"), "---\ntrigger: always_on\n---\nR\n")
	writeValidateFile(t, filepath.Join(root, "rules.json"), `{"exclude":["a"]}`)
	out, _, code = runValidateRef(t, root)
	if code != 0 || !strings.Contains(out, "rules 1") || strings.Contains(out, "no valid trigger") {
		t.Errorf("excluded rule: code=%d out=%q", code, out)
	}

	// agents.json excludes by file stem but keeps every finding.
	root = newValidatePlugin(t, `{"name":"exa"}`)
	writeValidateFile(t, filepath.Join(root, "agents", "a.md"), "---\nname: a\n---\nB\n")
	writeValidateFile(t, filepath.Join(root, "agents", "b.md"), "---\nname: b\ndescription: d\n---\nB\n")
	writeValidateFile(t, filepath.Join(root, "agents.json"), `{"exclude":["a"]}`)
	out, _, code = runValidateRef(t, root)
	if code != 1 || !strings.Contains(out, "✗ agents/a.md: the frontmatter has no description") ||
		!strings.Contains(out, "agents 1") {
		t.Errorf("excluded agent: code=%d out=%q", code, out)
	}
}

func TestPluginValidate_HiddenDirComponents(t *testing.T) {
	root := t.TempDir()
	writeValidateFile(t, filepath.Join(root, ".antigravity-plugin", "plugin.json"), `{"name":"hid"}`)
	writeValidateFile(t, filepath.Join(root, ".antigravity-plugin", "skills", "s", "SKILL.md"),
		"---\nname: s\ndescription: d\n---\nbody\n")
	writeValidateFile(t, filepath.Join(root, ".antigravity-plugin", "hooks.json"), `{"PreToolUse":{"command":"true"}}`)
	writeValidateFile(t, filepath.Join(root, ".antigravity-plugin", "mcp_config.json"), `{"mcpServers":{"a":{"command":"echo"}}}`)
	out, _, code := runValidateRef(t, root)
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	for _, want := range []string{
		"⚠ .antigravity-plugin/skills: never loaded: components are read from the plugin root, not .antigravity-plugin. Move it next to .antigravity-plugin",
		"⚠ .antigravity-plugin/hooks.json: never loaded: components are read from the plugin root",
		"⚠ .antigravity-plugin/mcp_config.json: never loaded: components are read from the plugin root",
		"skills 0 · agents 0 · rules 0 · hooks 0 · mcpServers 0",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}
