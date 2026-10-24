package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type ruleFile struct {
	name     string
	rel      string
	raw      []byte
	excluded bool
}

// scanValidateRules walks flat `rules/*.md`.
func scanValidateRules(dir string, rep *validateReport) {
	excluded := validateExclusions(dir, "rules.json", rep)
	files := collectRuleFiles(dir, excluded)
	for _, pass := range []bool{false, true} {
		for _, f := range files {
			if f.excluded != pass {
				continue
			}
			validateRuleFile(f, rep)
		}
	}
}

func collectRuleFiles(dir string, excluded map[string]bool) []ruleFile {
	entries, err := os.ReadDir(filepath.Join(dir, "rules"))
	if err != nil {
		return nil
	}
	var files []ruleFile
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, "rules", e.Name()))
		if err != nil {
			continue
		}
		stem := strings.TrimSuffix(e.Name(), ".md")
		files = append(files, ruleFile{
			name:     e.Name(),
			rel:      filepath.ToSlash(filepath.Join("rules", e.Name())),
			raw:      raw,
			excluded: excluded[stem],
		})
	}
	return files
}

func validateRuleFile(f ruleFile, rep *validateReport) {
	fm, body, fmErr, ok := parseFrontmatter(f.raw)
	if !ok {
		if f.name == "README.md" || f.excluded {
			return
		}
		rep.Findings = append(rep.Findings, validateFinding{
			Severity: validateError,
			File:     f.rel,
			Message:  "no valid trigger in the frontmatter, so the rule never applies. Set trigger to always_on, model_decision, glob, manual",
		})
		return
	}
	if fmErr != nil {
		rep.Findings = append(rep.Findings, validateFinding{
			Severity: validateError,
			File:     f.rel,
			Message:  "the frontmatter isn't valid YAML: " + fmErr.Error() + ". The loader skips this rule",
		})
		return
	}
	var fields struct {
		Trigger     string `yaml:"trigger"`
		Description string `yaml:"description"`
		Globs       string `yaml:"globs"`
	}
	if err := yaml.Unmarshal(fm, &fields); err != nil {
		rep.Findings = append(rep.Findings, validateFinding{
			Severity: validateError,
			File:     f.rel,
			Message:  "the frontmatter isn't valid YAML: " + err.Error() + ". The loader skips this rule",
		})
		return
	}
	if f.excluded {
		return
	}
	validateRuleFields(f, fields.Trigger, fields.Description, fields.Globs, body, rep)
}

func validateRuleFields(f ruleFile, trigger, desc, globs string, body []byte, rep *validateReport) {
	switch trigger {
	case "always_on", "model_decision", "glob", "manual":
	default:
		rep.Findings = append(rep.Findings, validateFinding{
			Severity: validateError,
			File:     f.rel,
			Message:  "no valid trigger in the frontmatter, so the rule never applies. Set trigger to always_on, model_decision, glob, manual",
		})
		return
	}
	if trigger == "glob" && globs == "" {
		rep.Findings = append(rep.Findings, validateFinding{
			Severity: validateWarning,
			File:     f.rel,
			Message:  "a glob rule needs globs, or it never applies",
		})
	}
	if trigger == "model_decision" && desc == "" {
		rep.Findings = append(rep.Findings, validateFinding{
			Severity: validateWarning,
			File:     f.rel,
			Message:  "a model_decision rule needs a description: the model uses it to decide when to apply the rule",
		})
	}
	if n := len(body); n > pluginValidateBodyLimit {
		rep.Findings = append(rep.Findings, validateFinding{
			Severity: validateWarning,
			File:     f.rel,
			Message:  fmt.Sprintf("the content is cut by %d bytes to fit the %d-byte limit", n, pluginValidateBodyLimit),
		})
	}
	rep.Components.Rules++
}

// checkRootMisplacedFiles warns on misplaced root rules and workflows.
func checkRootMisplacedFiles(dir string, rep *validateReport) {
	for _, rootFile := range []string{"AGENTS.md", "GEMINI.md"} {
		if _, err := os.Stat(filepath.Join(dir, rootFile)); err == nil {
			rep.Findings = append(rep.Findings, validateFinding{
				Severity: validateWarning,
				File:     rootFile,
				Message:  fmt.Sprintf("never loaded: plugins load rules only from rules/. Move it to rules/%s", rootFile),
			})
		}
	}
	if st, err := os.Stat(filepath.Join(dir, "workflows")); err == nil && st.IsDir() {
		rep.Findings = append(rep.Findings, validateFinding{
			Severity: validateWarning,
			File:     "workflows",
			Message:  "never loaded: plugins don't load Markdown workflows. Make each one a skill in skills/<name>/SKILL.md",
		})
	}
}
