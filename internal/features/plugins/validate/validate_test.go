package validate_test

import (
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/justwasm/pigasus/internal/features/plugins/validate"
)

func file(data string) *fstest.MapFile {
	return &fstest.MapFile{Data: []byte(data)}
}

func check(t *testing.T, files fstest.MapFS, manifest string) validate.Result {
	t.Helper()
	return validate.Check(validate.Source{
		Files:    files,
		Dir:      "fixture-plugin",
		Manifest: manifest,
	})
}

func finding(report validate.Report, severity validate.Severity, filename, messagePart string) bool {
	for _, got := range report.Findings {
		if got.Severity == severity && got.File == filename && strings.Contains(got.Message, messagePart) {
			return true
		}
	}
	return false
}

func requireFinding(t *testing.T, report validate.Report, severity validate.Severity, filename, messagePart string) {
	t.Helper()
	if !finding(report, severity, filename, messagePart) {
		t.Errorf("missing %q finding for %s (severity %q); findings: %+v", messagePart, filename, severity, report.Findings)
	}
}

func requireNoFinding(t *testing.T, report validate.Report, filename, messagePart string) {
	t.Helper()
	for _, got := range report.Findings {
		if got.File == filename && strings.Contains(got.Message, messagePart) {
			t.Errorf("unexpected finding for %s containing %q: %+v", filename, messagePart, got)
		}
	}
}

func TestCheck_NormalPlugin(t *testing.T) {
	files := fstest.MapFS{
		"plugin.json":            file(`{"name":"sample-plugin","description":"A plugin","category":"developer-tools"}`),
		"skills/lookup/SKILL.md": file("---\nname: lookup\ndescription: Find things\n---\nskill body\n"),
		"agents/helper.md":       file("---\nname: helper\ndescription: Help with tasks\n---\nagent body\n"),
		"rules/always.md":        file("---\ntrigger: always_on\n---\nrule body\n"),
		"hooks.json":             file(`{"my-hook":{"PreToolUse":{"command":"true"}}}`),
		"mcp_config.json":        file(`{"mcpServers":{"search":{"command":"search"}}}`),
	}

	got := check(t, files, "plugin.json")
	if !got.ManifestOK {
		t.Fatalf("ManifestOK = false; findings: %+v", got.Report.Findings)
	}
	if got.Report.Dir != "fixture-plugin" || got.Report.Manifest != "plugin.json" || got.Report.Name != "sample-plugin" {
		t.Errorf("report identity = dir %q, manifest %q, name %q", got.Report.Dir, got.Report.Manifest, got.Report.Name)
	}
	want := validate.Components{Skills: 1, Agents: 1, Rules: 1, Hooks: 1, MCPServers: 1}
	if got.Report.Components != want {
		t.Errorf("components = %+v, want %+v", got.Report.Components, want)
	}
	if got.Report.AgentPluginsSpec {
		t.Error("AgentPluginsSpec = true for a native plugin")
	}
	if errors, warnings := got.Report.Counts(); errors != 0 || warnings != 0 {
		t.Errorf("Counts() = (%d, %d), want (0, 0); findings: %+v", errors, warnings, got.Report.Findings)
	}
	if len(got.Report.Findings) != 1 || !finding(got.Report, validate.SeverityNote, "plugin.json", "no logo") {
		t.Errorf("normal manifest should only carry the no-logo note, findings: %+v", got.Report.Findings)
	}
}

func TestCheck_MalformedManifestStopsComponentScanning(t *testing.T) {
	got := check(t, fstest.MapFS{
		"plugin.json":         file(`{"name":`),
		"skills/bad/SKILL.md": file("not frontmatter\n"),
	}, "plugin.json")

	if got.ManifestOK {
		t.Error("ManifestOK = true for malformed manifest")
	}
	if errors, warnings := got.Report.Counts(); errors != 1 || warnings != 0 {
		t.Errorf("Counts() = (%d, %d), want (1, 0)", errors, warnings)
	}
	requireFinding(t, got.Report, validate.SeverityError, "plugin.json", "line 1")
	requireNoFinding(t, got.Report, "skills/bad/SKILL.md", "frontmatter")
	if got.Report.Components.Skills != 0 {
		t.Errorf("malformed manifest should stop scans; components = %+v", got.Report.Components)
	}
}

func TestCheck_MissingCapabilityPrerequisites(t *testing.T) {
	t.Run("wrapped hook group needs command", func(t *testing.T) {
		got := check(t, fstest.MapFS{
			"plugin.json": file(`{"name":"hook-plugin"}`),
			"hooks.json":  file(`{"hooks":{"PreToolUse":[{"matcher":"*.go"}]}}`),
		}, "plugin.json")
		requireFinding(t, got.Report, validate.SeverityError, "hooks.json", "command hook must specify 'command'")
		if got.Report.Components.Hooks != 0 {
			t.Errorf("invalid wrapped hook was counted: %+v", got.Report.Components)
		}
	})

	t.Run("agent needs description", func(t *testing.T) {
		got := check(t, fstest.MapFS{
			"plugin.json":      file(`{"name":"agent-plugin"}`),
			"agents/helper.md": file("---\nname: helper\n---\nbody\n"),
		}, "plugin.json")
		requireFinding(t, got.Report, validate.SeverityError, "agents/helper.md", "no description")
		if got.Report.Components.Agents != 0 {
			t.Errorf("invalid agent was counted: %+v", got.Report.Components)
		}
	})
}

func TestCheck_ComponentFindingsAndCounts(t *testing.T) {
	largeBody := strings.Repeat("x", 24001)
	files := fstest.MapFS{
		"plugin.json":              file(`{"name":"component-plugin"}`),
		"skills/nofm/SKILL.md":     file("no frontmatter\n"),
		"skills/noname/SKILL.md":   file("---\ndescription: Missing a name\n---\nbody\n"),
		"skills/nodesc/SKILL.md":   file("---\nname: nodesc\n---\nbody\n"),
		"skills/one/SKILL.md":      file("---\nname: duplicate\ndescription: first\n---\nbody\n"),
		"skills/two/SKILL.md":      file("---\nname: duplicate\ndescription: second\n---\nbody\n"),
		"skills/big/SKILL.md":      file("---\nname: big\ndescription: Large body\n---\n" + largeBody),
		"agents/noname.md":         file("---\ndescription: missing name\n---\nbody\n"),
		"agents/nodesc.md":         file("---\nname: nodesc\n---\nbody\n"),
		"agents/good.md":           file("---\nname: good\ndescription: Good agent\n---\nbody\n"),
		"agents/legacy/agent.json": file(`{"name":"legacy","description":"old format"}`),
		"rules/invalid.md":         file("---\ndescription: no trigger\n---\nrule\n"),
		"rules/glob.md":            file("---\ntrigger: glob\n---\nrule\n"),
		"rules/model.md":           file("---\ntrigger: model_decision\n---\nrule\n"),
		"rules/README.md":          file("readme without frontmatter\n"),
	}

	got := check(t, files, "plugin.json")
	requireFinding(t, got.Report, validate.SeverityError, "skills/nofm/SKILL.md", "no frontmatter")
	requireFinding(t, got.Report, validate.SeverityWarning, "skills/noname/SKILL.md", "no name in the frontmatter")
	requireFinding(t, got.Report, validate.SeverityWarning, "skills/nodesc/SKILL.md", "no description")
	requireFinding(t, got.Report, validate.SeverityWarning, "skills/two/SKILL.md", "already defines a skill named")
	requireFinding(t, got.Report, validate.SeverityWarning, "skills/big/SKILL.md", "24001 bytes")
	requireFinding(t, got.Report, validate.SeverityError, "agents/noname.md", "no name")
	requireFinding(t, got.Report, validate.SeverityError, "agents/nodesc.md", "no description")
	requireFinding(t, got.Report, validate.SeverityWarning, "agents/legacy/agent.json", "only internal builds read agent.json")
	requireFinding(t, got.Report, validate.SeverityError, "rules/invalid.md", "no valid trigger")
	requireFinding(t, got.Report, validate.SeverityWarning, "rules/glob.md", "needs globs")
	requireFinding(t, got.Report, validate.SeverityWarning, "rules/model.md", "needs a description")
	requireNoFinding(t, got.Report, "rules/README.md", "frontmatter")

	want := validate.Components{Skills: 4, Agents: 1, Rules: 2}
	if got.Report.Components != want {
		t.Errorf("components = %+v, want %+v", got.Report.Components, want)
	}
	if errors, warnings := got.Report.Counts(); errors != 4 || warnings != 9 {
		t.Errorf("Counts() = (%d, %d), want (4, 9); findings: %+v", errors, warnings, got.Report.Findings)
	}
}

func TestCheck_ExclusionsPreserveLoaderSemantics(t *testing.T) {
	t.Run("skills suppress semantic warnings but not parse errors", func(t *testing.T) {
		got := check(t, fstest.MapFS{
			"plugin.json":                    file(`{"name":"excluded-skills"}`),
			"skills.json":                    file(`{"exclude":["bad-parse","hidden-warning"]}`),
			"skills/bad-parse/SKILL.md":      file("not frontmatter\n"),
			"skills/hidden-warning/SKILL.md": file("---\ndescription: hidden\n---\nbody\n"),
			"skills/kept/SKILL.md":           file("---\nname: kept\ndescription: valid\n---\nbody\n"),
		}, "plugin.json")
		requireFinding(t, got.Report, validate.SeverityError, "skills/bad-parse/SKILL.md", "no frontmatter")
		requireNoFinding(t, got.Report, "skills/hidden-warning/SKILL.md", "no name")
		if got.Report.Components.Skills != 1 {
			t.Errorf("skills count = %d, want 1", got.Report.Components.Skills)
		}
	})

	t.Run("excluded agents still report prerequisite errors", func(t *testing.T) {
		got := check(t, fstest.MapFS{
			"plugin.json":      file(`{"name":"excluded-agents"}`),
			"agents.json":      file(`{"exclude":["broken"]}`),
			"agents/broken.md": file("---\nname: broken\n---\nbody\n"),
			"agents/kept.md":   file("---\nname: kept\ndescription: valid\n---\nbody\n"),
		}, "plugin.json")
		requireFinding(t, got.Report, validate.SeverityError, "agents/broken.md", "no description")
		if got.Report.Components.Agents != 1 {
			t.Errorf("agents count = %d, want 1", got.Report.Components.Agents)
		}
	})

	t.Run("invalid exclusion shape retains customization type name", func(t *testing.T) {
		got := check(t, fstest.MapFS{
			"plugin.json": file(`{"name":"bad-exclusions"}`),
			"skills.json": file(`[]`),
		}, "plugin.json")
		requireFinding(t, got.Report, validate.SeverityError, "skills.json", "JSONConfig")
	})
}

func TestCheck_ByteOrderMarks(t *testing.T) {
	const bom = "\ufeff"

	t.Run("manifest and skill tolerate BOM", func(t *testing.T) {
		got := check(t, fstest.MapFS{
			"plugin.json":              file(bom + `{"name":"bom-plugin"}`),
			"skills/with-bom/SKILL.md": file(bom + "---\nname: with-bom\ndescription: ok\n---\nbody\n"),
		}, "plugin.json")
		if !got.ManifestOK || got.Report.Name != "bom-plugin" || got.Report.Components.Skills != 1 {
			t.Errorf("BOM-tolerant inputs did not validate: %+v", got)
		}
	})

	t.Run("hooks and exclusion config reject BOM while MCP config tolerates it", func(t *testing.T) {
		got := check(t, fstest.MapFS{
			"plugin.json":     file(`{"name":"bom-components"}`),
			"hooks.json":      file(bom + `{"my-hook":{"PreToolUse":{"command":"true"}}}`),
			"mcp_config.json": file(bom + `{"mcpServers":{"one":{"command":"run"}}}`),
			"agents.json":     file(bom + `{"exclude":["unused"]}`),
		}, "plugin.json")
		requireFinding(t, got.Report, validate.SeverityError, "hooks.json", "byte order mark")
		requireFinding(t, got.Report, validate.SeverityError, "agents.json", "byte order mark")
		if got.Report.Components.MCPServers != 1 {
			t.Errorf("MCP server count = %d, want 1", got.Report.Components.MCPServers)
		}
	})
}

func TestCheck_SchemaManifestFindings(t *testing.T) {
	t.Run("supported Agent Plugins schema is noted", func(t *testing.T) {
		got := check(t, fstest.MapFS{
			"plugin.json": file(`{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"spec-plugin"}`),
			"mcp.json":    file(`{"mcpServers":{"one":{},"two":{}}}`),
		}, "plugin.json")
		if !got.ManifestOK || !got.Report.AgentPluginsSpec {
			t.Errorf("supported schema not recognized: %+v", got)
		}
		requireFinding(t, got.Report, validate.SeverityNote, "plugin.json", "declares the Agent Plugins 1.0.0 $schema")
		if got.Report.Components.MCPServers != 2 {
			t.Errorf("spec MCP server count = %d, want 2", got.Report.Components.MCPServers)
		}
	})

	t.Run("unsupported schema stops scans", func(t *testing.T) {
		got := check(t, fstest.MapFS{
			"plugin.json":             file(`{"$schema":"https://agent-plugins.org/schemas/9.9.9/plugin.schema.json","name":"Bad_Name"}`),
			"skills/ignored/SKILL.md": file("not frontmatter\n"),
		}, "plugin.json")
		if got.ManifestOK {
			t.Error("ManifestOK = true for unsupported schema")
		}
		requireFinding(t, got.Report, validate.SeverityError, "plugin.json", "unsupported Agent Plugins version")
		requireNoFinding(t, got.Report, "plugin.json", "strict kebab-case")
		requireNoFinding(t, got.Report, "skills/ignored/SKILL.md", "frontmatter")
		if got.Report.Components.Skills != 0 {
			t.Errorf("unsupported schema should stop scans: %+v", got.Report.Components)
		}
	})
}

func TestCheck_NameCategoryAndExtensionsFindings(t *testing.T) {
	got := check(t, fstest.MapFS{
		"plugin.json": file(`{"name":"Bad_Name","category":" ","extensions":[],"extensions2":true}`),
	}, "plugin.json")
	requireFinding(t, got.Report, validate.SeverityWarning, "plugin.json", `name "Bad_Name" isn't strict kebab-case`)
	requireFinding(t, got.Report, validate.SeverityWarning, "plugin.json", `"category" must be a non-empty string`)
	requireFinding(t, got.Report, validate.SeverityWarning, "plugin.json", `"extensions" isn't an object`)
	requireFinding(t, got.Report, validate.SeverityWarning, "plugin.json", `unknown field "extensions2" is ignored`)

	extensions := check(t, fstest.MapFS{
		"plugin.json": file(`{"name":"extension-plugin","extensions":{"com.google.antigravity":{"customField":1}}}`),
	}, "plugin.json")
	requireFinding(t, extensions.Report, validate.SeverityWarning, "plugin.json", `unknown field "customField" under extensions["com.google.antigravity"] is ignored`)

	missingName := check(t, fstest.MapFS{
		"plugin.json": file(`{"description":"No explicit name"}`),
	}, "plugin.json")
	if missingName.Report.Name != "fixture-plugin" {
		t.Errorf("name fallback = %q, want stable Dir basename", missingName.Report.Name)
	}
	requireFinding(t, missingName.Report, validate.SeverityWarning, "plugin.json", "no name: the plugin is named after its folder")
}

func TestCheck_ManifestPathMustBeValidFSPath(t *testing.T) {
	got := validate.Check(validate.Source{
		Files:    fstest.MapFS{"plugin.json": file(`{"name":"path-check"}`)},
		Dir:      "fixture-plugin",
		Manifest: "../plugin.json",
	})
	if got.ManifestOK {
		t.Error("ManifestOK = true for an escaping manifest path")
	}
	requireFinding(t, got.Report, validate.SeverityError, "../plugin.json", "outside the plugin root")
	if errors, _ := got.Report.Counts(); errors != 1 {
		t.Errorf("error count = %d, want 1", errors)
	}
}

func TestCheck_ReportFindingsAreStableAndSorted(t *testing.T) {
	files := fstest.MapFS{
		"plugin.json":            file(`{"name":"sort-plugin","unknownB":1,"unknownA":1}`),
		"skills/broken/SKILL.md": file("no frontmatter\n"),
	}
	first := check(t, files, "plugin.json")
	second := check(t, files, "plugin.json")
	if !reflect.DeepEqual(first.Report.Findings, second.Report.Findings) {
		t.Fatalf("repeated checks differ:\n%+v\n%+v", first.Report.Findings, second.Report.Findings)
	}
	if len(first.Report.Findings) < 4 || first.Report.Findings[0].Severity != validate.SeverityError {
		t.Errorf("expected stable findings with errors first: %+v", first.Report.Findings)
	}
}

func TestCheck_AgentLoaderDecisions(t *testing.T) {
	got := check(t, fstest.MapFS{
		"plugin.json":              file(`{"name":"agent-cases"}`),
		"agents/nofm.md":           file("no frontmatter\n"),
		"agents/reserved.md":       file("---\nname: review-agent\ndescription: Reserved\n---\nbody\n"),
		"agents/one.md":            file("---\nname: shared\ndescription: First\n---\nbody\n"),
		"agents/two.md":            file("---\nname: shared\ndescription: Second\n---\nbody\n"),
		"agents/broken/agent.json": file(`{bad json`),
	}, "plugin.json")
	requireFinding(t, got.Report, validate.SeverityWarning, "agents/nofm.md", "never loaded: an agent file starts with frontmatter")
	requireFinding(t, got.Report, validate.SeverityError, "agents/reserved.md", `the name "review-agent" is reserved`)
	requireFinding(t, got.Report, validate.SeverityWarning, "agents/two.md", `agents/one.md already defines an agent named "shared"`)
	requireFinding(t, got.Report, validate.SeverityError, "agents/broken/agent.json", "line 1, column 2: invalid literal: bad")
	if got.Report.Components.Agents != 1 {
		t.Errorf("agents = %d, want 1", got.Report.Components.Agents)
	}
}

func TestCheck_RuleLimitsAndExclusions(t *testing.T) {
	got := check(t, fstest.MapFS{
		"plugin.json":       file(`{"name":"rule-cases"}`),
		"rules.json":        file(`{"exclude":["excluded"]}`),
		"rules/excluded.md": file("---\ndescription: no trigger\n---\nrule\n"),
		"rules/nofm.md":     file("no frontmatter\n"),
		"rules/big.md":      file("---\ntrigger: always_on\n---\n" + strings.Repeat("x", 24001)),
	}, "plugin.json")
	requireNoFinding(t, got.Report, "rules/excluded.md", "no valid trigger")
	requireFinding(t, got.Report, validate.SeverityError, "rules/nofm.md", "no valid trigger")
	requireFinding(t, got.Report, validate.SeverityWarning, "rules/big.md", "the content is cut by 24001 bytes to fit the 24000-byte limit")
	if got.Report.Components.Rules != 1 {
		t.Errorf("rules = %d, want 1", got.Report.Components.Rules)
	}
}

func TestCheck_UnknownFieldSuggestions(t *testing.T) {
	got := check(t, fstest.MapFS{
		"plugin.json": file(`{"name":"suggestions","descripton":"d","auther":"a","nam":"x","unknown_thing":1}`),
	}, "plugin.json")
	for _, message := range []string{
		`unknown field "auther" is ignored (did you mean "author"?)`,
		`unknown field "descripton" is ignored (did you mean "description"?)`,
		`unknown field "nam" is ignored (did you mean "name"?)`,
		`unknown field "unknown_thing" is ignored`,
	} {
		requireFinding(t, got.Report, validate.SeverityWarning, "plugin.json", message)
	}
	requireNoFinding(t, got.Report, "plugin.json", `unknown_thing" is ignored (did you mean`)
}

func TestCheck_ManifestCategory(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value string
		warn  bool
	}{
		{"nonempty", `"developer-tools"`, false},
		{"numeric", `42`, true},
		{"empty", `""`, true},
		{"blank", `"   "`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := check(t, fstest.MapFS{"plugin.json": file(`{"name":"category-plugin","category":` + tc.value + `}`)}, "plugin.json")
			requireNoFinding(t, got.Report, "plugin.json", `unknown field "category"`)
			if tc.warn {
				requireFinding(t, got.Report, validate.SeverityWarning, "plugin.json", `"category" must be a non-empty string; it is ignored`)
			} else {
				requireNoFinding(t, got.Report, "plugin.json", `"category" must be`)
			}
		})
	}
}

func TestCheck_SelectedManifestKeepsComponentsAtPluginRoot(t *testing.T) {
	for _, manifest := range []string{"Plugin.json", ".antigravity-plugin/plugin.json", ".gemini-plugin/plugin.json"} {
		t.Run(manifest, func(t *testing.T) {
			files := fstest.MapFS{
				manifest:               file(`{"name":"selected-manifest"}`),
				"skills/root/SKILL.md": file("---\nname: root\ndescription: Loaded\n---\nbody\n"),
				"AGENTS.md":            file("misplaced rule\n"),
				"GEMINI.md":            file("misplaced rule\n"),
				"workflows/work.md":    file("misplaced workflow\n"),
			}
			if i := strings.LastIndex(manifest, "/"); i >= 0 {
				hidden := manifest[:i]
				files[hidden+"/skills/hidden/SKILL.md"] = file("---\nname: hidden\ndescription: Ignored\n---\nbody\n")
				files[hidden+"/hooks.json"] = file(`{"one":{"command":"true"}}`)
			}
			got := check(t, files, manifest)
			if !got.ManifestOK || got.Report.Manifest != manifest || got.Report.Components.Skills != 1 || got.Report.Components.Hooks != 0 {
				t.Errorf("selected manifest/root component behavior changed: %+v", got)
			}
			requireFinding(t, got.Report, validate.SeverityWarning, "AGENTS.md", "plugins load rules only from rules/")
			requireFinding(t, got.Report, validate.SeverityWarning, "GEMINI.md", "plugins load rules only from rules/")
			requireFinding(t, got.Report, validate.SeverityWarning, "workflows", "plugins don't load Markdown workflows")
			if i := strings.LastIndex(manifest, "/"); i >= 0 {
				hidden := manifest[:i]
				requireFinding(t, got.Report, validate.SeverityWarning, hidden+"/skills", "components are read from the plugin root")
				requireFinding(t, got.Report, validate.SeverityWarning, hidden+"/hooks.json", "components are read from the plugin root")
			}
		})
	}
}

func TestArchiveEntryPath_PreservesExtractionPolicy(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		out  string
		ok   bool
	}{
		{"simple", "assets/logo.svg", "assets/logo.svg", true},
		{"dot segments", "assets/./logo.svg", "assets/logo.svg", true},
		{"parent prefix rejected", "../outside", "", false},
		{"parent nested rejected", "assets/../../outside", "", false},
		{"absolute rejected", "/tmp/plugin.json", "", false},
		{"backslash remains a path character", `assets\\logo.svg`, `assets\\logo.svg`, true},
		{"double leading parent rejected by original prefix", ".../file", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := validate.ArchiveEntryPath(tc.in)
			if ok != tc.ok || got != tc.out {
				t.Errorf("ArchiveEntryPath(%q) = (%q, %t), want (%q, %t)", tc.in, got, ok, tc.out, tc.ok)
			}
		})
	}
}
