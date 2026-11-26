package validate_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/justwasm/pigasus/internal/features/plugins/validate"
)

func TestCheck_NameCompatibility(t *testing.T) {
	for _, tc := range []struct {
		name       string
		pluginName string
		suggestion string
		refuses    bool
	}{
		{"uppercase", "Neo4j", "neo4j", false},
		{"underscore", "foo_bar", "foo-bar", false},
		{"dot", "foo.bar", "foo-bar", true},
		{"double hyphen", "a--b--c", "a-b-c", false},
		{"trailing hyphen", "foo-", "foo", false},
		{"no suggestion left", "--", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := check(t, fstest.MapFS{
				"plugin.json": file(`{"name":"` + tc.pluginName + `"}`),
			}, "plugin.json")
			requireFinding(t, got.Report, validate.SeverityWarning, "plugin.json", "isn't strict kebab-case (lowercase letters, digits and single hyphens)")
			requireFinding(t, got.Report, validate.SeverityWarning, "plugin.json", "can't be published to the Antigravity Marketplace")
			if tc.suggestion != "" {
				requireFinding(t, got.Report, validate.SeverityWarning, "plugin.json", `try "`+tc.suggestion+`"`)
			} else {
				requireNoFinding(t, got.Report, "plugin.json", "; try ")
			}
			if tc.refuses {
				requireFinding(t, got.Report, validate.SeverityWarning, "plugin.json", "`plugin install` refuses it")
			} else {
				requireNoFinding(t, got.Report, "plugin.json", "`plugin install` refuses it")
			}
		})
	}

	for _, name := range []string{"ok-plugin", "a", "123", "a1-b2"} {
		t.Run("accepted "+name, func(t *testing.T) {
			got := check(t, fstest.MapFS{"plugin.json": file(`{"name":"` + name + `"}`)}, "plugin.json")
			requireNoFinding(t, got.Report, "plugin.json", "kebab-case")
			if errors, warnings := got.Report.Counts(); errors != 0 || warnings != 0 {
				t.Errorf("accepted name produced (%d, %d) errors/warnings: %+v", errors, warnings, got.Report.Findings)
			}
		})
	}
}

func TestCheck_AgentPluginsNameGrammar(t *testing.T) {
	t.Run("invalid native name includes spec and install clauses", func(t *testing.T) {
		got := check(t, fstest.MapFS{
			"plugin.json": file(`{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"foo bar"}`),
		}, "plugin.json")
		requireFinding(t, got.Report, validate.SeverityWarning, "plugin.json", "at most 64 lowercase letters, digits and single hyphens")
		requireFinding(t, got.Report, validate.SeverityWarning, "plugin.json", "`plugin install` refuses it, it isn't valid under the Agent Plugins spec")
		requireFinding(t, got.Report, validate.SeverityWarning, "plugin.json", `try "foo-bar"`)
	})

	t.Run("kebab case over spec length keeps spec wording", func(t *testing.T) {
		got := check(t, fstest.MapFS{
			"plugin.json": file(`{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"` + strings.Repeat("a", 70) + `"}`),
		}, "plugin.json")
		requireFinding(t, got.Report, validate.SeverityWarning, "plugin.json", "isn't valid under the Agent Plugins spec: use 1-64 lowercase letters")
		requireNoFinding(t, got.Report, "plugin.json", "kebab-case")
	})
}

func TestCheck_LogoTypeErrorStopsScanning(t *testing.T) {
	got := check(t, fstest.MapFS{
		"plugin.json": file(`{"name":"ok-plugin","logo":5}`),
		"skills/ignored/SKILL.md": file("not frontmatter\n"),
	}, "plugin.json")
	if got.ManifestOK {
		t.Error("ManifestOK = true for a non-string logo")
	}
	requireFinding(t, got.Report, validate.SeverityError, "plugin.json", "(line 1:28): invalid value for string field logo: 5. The loader skips this plugin")
	requireNoFinding(t, got.Report, "skills/ignored/SKILL.md", "frontmatter")
}
