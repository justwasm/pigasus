package validate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"strings"

	yaml "gopkg.in/yaml.v3"
)

// utf8BOM is the byte-order mark some Windows editors prepend.
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// pluginValidateBodyLimit is the 24 KB limit the reference applies to skill
// bodies and rule content. It is 24000 bytes, not 24576.
const pluginValidateBodyLimit = 24000

// reservedAgentNames are the built-in agent names the reference refuses.
var reservedAgentNames = map[string]bool{
	"deepagent":    true,
	"review-agent": true,
}

type skillFile struct {
	folder   string
	rel      string
	raw      []byte
	excluded bool
}

type agentFile struct {
	rel      string
	raw      []byte
	excluded bool
}

// parseFrontmatter splits a `---` delimited YAML block off a markdown file.
func parseFrontmatter(raw []byte) (fm []byte, body []byte, fmErr error, ok bool) {
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return nil, raw, nil, false
	}
	rest := text[4:]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return nil, raw, nil, false
	}
	after := rest[end+4:]
	if after != "" && !strings.HasPrefix(after, "\n") {
		return nil, raw, nil, false
	}
	fm = []byte(rest[:end])
	if strings.HasPrefix(after, "\n") {
		after = after[1:]
	}
	var node any
	if err := yaml.Unmarshal(fm, &node); err != nil {
		return fm, []byte(after), err, true
	}
	return fm, []byte(after), nil, true
}

// scanValidateSkills walks `skills/<folder>/SKILL.md` (one level).
func scanValidateSkills(tree fs.FS, rep *Report) {
	excluded := validateExclusions(tree, "skills.json", rep)
	files := collectSkillFiles(tree, excluded)
	seen := map[string]string{}
	for _, pass := range []bool{false, true} {
		for _, f := range files {
			if f.excluded != pass {
				continue
			}
			validateSkillFile(f, seen, rep)
		}
	}
}

func collectSkillFiles(tree fs.FS, excluded map[string]bool) []skillFile {
	entries, err := fs.ReadDir(tree, "skills")
	if err != nil {
		return nil
	}
	var files []skillFile
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		rel := filepath.ToSlash(filepath.Join("skills", e.Name(), "SKILL.md"))
		raw, err := fs.ReadFile(tree, rel)
		if err != nil {
			continue
		}
		files = append(files, skillFile{folder: e.Name(), rel: rel, raw: raw, excluded: excluded[e.Name()]})
	}
	return files
}

func validateSkillFile(f skillFile, seen map[string]string, rep *Report) {
	fm, body, fmErr, ok := parseFrontmatter(bytes.TrimPrefix(f.raw, utf8BOM))
	if !ok {
		rep.Findings = append(rep.Findings, Finding{
			Severity: SeverityError,
			File:     f.rel,
			Message:  "no frontmatter: the file must start with a --- block holding name and description. The loader skips this skill",
		})
		return
	}
	if fmErr != nil {
		rep.Findings = append(rep.Findings, Finding{
			Severity: SeverityError,
			File:     f.rel,
			Message:  "the frontmatter isn't valid YAML: failed to parse frontmatter: " + fmErr.Error() + ". The loader skips this skill",
		})
		return
	}
	var fields struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
	}
	if err := yaml.Unmarshal(fm, &fields); err != nil {
		rep.Findings = append(rep.Findings, Finding{
			Severity: SeverityError,
			File:     f.rel,
			Message:  "the frontmatter isn't valid YAML: failed to parse frontmatter: " + err.Error() + ". The loader skips this skill",
		})
		return
	}
	validateSkillSemantic(f, fields.Name, fields.Description, body, seen, rep)
}

func resolveSkillNameAndDesc(f skillFile, name, desc string, rep *Report) string {
	effective := name
	if effective == "" {
		effective = "SKILL"
		if !f.excluded {
			rep.Findings = append(rep.Findings, Finding{
				Severity: SeverityWarning,
				File:     f.rel,
				Message:  fmt.Sprintf("no name in the frontmatter: the skill is named %q after its file. Add name: %s", effective, f.folder),
			})
		}
	}
	if desc == "" && !f.excluded {
		rep.Findings = append(rep.Findings, Finding{
			Severity: SeverityWarning,
			File:     f.rel,
			Message:  "no description: the model uses it to decide when to load the skill",
		})
	}
	return effective
}

func validateSkillSemantic(f skillFile, name, desc string, body []byte, seen map[string]string, rep *Report) {
	effective := resolveSkillNameAndDesc(f, name, desc, rep)
	prev, dup := seen[effective]
	if dup {
		rep.Findings = append(rep.Findings, Finding{
			Severity: SeverityWarning,
			File:     f.rel,
			Message:  fmt.Sprintf("skipped: %s already defines a skill named %q", prev, effective),
		})
	}
	if name != "" && name != f.folder && (!f.excluded || dup) {
		rep.Findings = append(rep.Findings, Finding{
			Severity: SeverityWarning,
			File:     f.rel,
			Message:  fmt.Sprintf("name %q doesn't match its folder %q", name, f.folder),
		})
	}
	if dup {
		return
	}
	seen[effective] = f.rel
	if f.excluded {
		return
	}
	rep.Components.Skills++
	if n := len(strings.TrimRight(string(body), "\n")); n > pluginValidateBodyLimit {
		rep.Findings = append(rep.Findings, Finding{
			Severity: SeverityWarning,
			File:     f.rel,
			Message:  fmt.Sprintf("the body is %d bytes; only the first %d are loaded", n, pluginValidateBodyLimit),
		})
	}
}

// scanValidateAgents walks flat `agents/*.md` plus `agents/<name>/agent.json`.
func scanValidateAgents(tree fs.FS, rep *Report) {
	excluded := validateExclusions(tree, "agents.json", rep)
	files := collectAgentFiles(tree, excluded, rep)
	seen := map[string]string{}
	for _, pass := range []bool{false, true} {
		for _, f := range files {
			if f.excluded != pass {
				continue
			}
			validateAgentFile(f, seen, rep)
		}
	}
}

func collectAgentFiles(tree fs.FS, excluded map[string]bool, rep *Report) []agentFile {
	entries, err := fs.ReadDir(tree, "agents")
	if err != nil {
		return nil
	}
	var files []agentFile
	for _, e := range entries {
		full := path.Join("agents", e.Name())
		if e.IsDir() {
			validateAgentJSONFolder(tree, full, e.Name(), rep)
			continue
		}
		if !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		raw, err := fs.ReadFile(tree, full)
		if err != nil {
			continue
		}
		stem := strings.TrimSuffix(e.Name(), ".md")
		files = append(files, agentFile{
			rel:      filepath.ToSlash(filepath.Join("agents", e.Name())),
			raw:      raw,
			excluded: excluded[stem],
		})
	}
	return files
}

func validateAgentJSONFolder(tree fs.FS, full, name string, rep *Report) {
	rel := filepath.ToSlash(filepath.Join("agents", name, "agent.json"))
	raw, err := fs.ReadFile(tree, path.Join(full, "agent.json"))
	if err != nil {
		return
	}
	if bytes.HasPrefix(raw, utf8BOM) {
		rep.Findings = append(rep.Findings, Finding{
			Severity: SeverityError,
			File:     rel,
			Message:  "starts with a UTF-8 byte order mark, which the loader can't parse. Save the file without one. The loader skips this agent",
		})
		return
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		rep.Findings = append(rep.Findings, Finding{
			Severity: SeverityError,
			File:     rel,
			Message:  jsonPosMessage(raw, err) + ". The loader skips this agent",
		})
		return
	}
	rep.Findings = append(rep.Findings, Finding{
		Severity: SeverityWarning,
		File:     rel,
		Message:  "never loaded by this build: only internal builds read agent.json; write the agent as agent.md",
	})
}

func validateAgentFile(f agentFile, seen map[string]string, rep *Report) {
	fm, _, fmErr, ok := parseFrontmatter(f.raw)
	if !ok {
		rep.Findings = append(rep.Findings, Finding{
			Severity: SeverityWarning,
			File:     f.rel,
			Message:  "never loaded: an agent file starts with frontmatter that has a name and description",
		})
		return
	}
	if fmErr != nil {
		rep.Findings = append(rep.Findings, Finding{
			Severity: SeverityError,
			File:     f.rel,
			Message:  "the frontmatter isn't valid YAML: failed to unmarshal frontmatter YAML: " + fmErr.Error() + ". The loader skips this agent",
		})
		return
	}
	var fields struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
	}
	if err := yaml.Unmarshal(fm, &fields); err != nil {
		rep.Findings = append(rep.Findings, Finding{
			Severity: SeverityError,
			File:     f.rel,
			Message:  "the frontmatter isn't valid YAML: failed to unmarshal frontmatter YAML: " + err.Error() + ". The loader skips this agent",
		})
		return
	}
	checkAgentFields(f, fields.Name, fields.Description, seen, rep)
}

func checkAgentFields(f agentFile, name, desc string, seen map[string]string, rep *Report) {
	if name == "" {
		rep.Findings = append(rep.Findings, Finding{
			Severity: SeverityError,
			File:     f.rel,
			Message:  "the frontmatter has no name. The loader skips this agent",
		})
		return
	}
	if desc == "" {
		rep.Findings = append(rep.Findings, Finding{
			Severity: SeverityError,
			File:     f.rel,
			Message:  "the frontmatter has no description. The loader skips this agent",
		})
		return
	}
	if reservedAgentNames[name] {
		rep.Findings = append(rep.Findings, Finding{
			Severity: SeverityError,
			File:     f.rel,
			Message:  fmt.Sprintf("the name %q is reserved for a built-in agent, so this agent never loads", name),
		})
		return
	}
	if prev, dup := seen[name]; dup {
		rep.Findings = append(rep.Findings, Finding{
			Severity: SeverityWarning,
			File:     f.rel,
			Message:  fmt.Sprintf("skipped: %s already defines an agent named %q", prev, name),
		})
		return
	}
	seen[name] = f.rel
	if !f.excluded {
		rep.Components.Agents++
	}
}

type ruleFile struct {
	name     string
	rel      string
	raw      []byte
	excluded bool
}

// scanValidateRules walks flat `rules/*.md`.
func scanValidateRules(tree fs.FS, rep *Report) {
	excluded := validateExclusions(tree, "rules.json", rep)
	files := collectRuleFiles(tree, excluded)
	for _, pass := range []bool{false, true} {
		for _, f := range files {
			if f.excluded != pass {
				continue
			}
			validateRuleFile(f, rep)
		}
	}
}

func collectRuleFiles(tree fs.FS, excluded map[string]bool) []ruleFile {
	entries, err := fs.ReadDir(tree, "rules")
	if err != nil {
		return nil
	}
	var files []ruleFile
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		raw, err := fs.ReadFile(tree, path.Join("rules", e.Name()))
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

func validateRuleFile(f ruleFile, rep *Report) {
	fm, body, fmErr, ok := parseFrontmatter(f.raw)
	if !ok {
		if f.name == "README.md" || f.excluded {
			return
		}
		rep.Findings = append(rep.Findings, Finding{
			Severity: SeverityError,
			File:     f.rel,
			Message:  "no valid trigger in the frontmatter, so the rule never applies. Set trigger to always_on, model_decision, glob, manual",
		})
		return
	}
	if fmErr != nil {
		rep.Findings = append(rep.Findings, Finding{
			Severity: SeverityError,
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
		rep.Findings = append(rep.Findings, Finding{
			Severity: SeverityError,
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

func validateRuleFields(f ruleFile, trigger, desc, globs string, body []byte, rep *Report) {
	switch trigger {
	case "always_on", "model_decision", "glob", "manual":
	default:
		rep.Findings = append(rep.Findings, Finding{
			Severity: SeverityError,
			File:     f.rel,
			Message:  "no valid trigger in the frontmatter, so the rule never applies. Set trigger to always_on, model_decision, glob, manual",
		})
		return
	}
	if trigger == "glob" && globs == "" {
		rep.Findings = append(rep.Findings, Finding{
			Severity: SeverityWarning,
			File:     f.rel,
			Message:  "a glob rule needs globs, or it never applies",
		})
	}
	if trigger == "model_decision" && desc == "" {
		rep.Findings = append(rep.Findings, Finding{
			Severity: SeverityWarning,
			File:     f.rel,
			Message:  "a model_decision rule needs a description: the model uses it to decide when to apply the rule",
		})
	}
	if n := len(body); n > pluginValidateBodyLimit {
		rep.Findings = append(rep.Findings, Finding{
			Severity: SeverityWarning,
			File:     f.rel,
			Message:  fmt.Sprintf("the content is cut by %d bytes to fit the %d-byte limit", n, pluginValidateBodyLimit),
		})
	}
	rep.Components.Rules++
}

// checkRootMisplacedFiles warns on misplaced root rules and workflows.
func checkRootMisplacedFiles(tree fs.FS, rep *Report) {
	for _, rootFile := range []string{"AGENTS.md", "GEMINI.md"} {
		if _, err := fs.Stat(tree, rootFile); err == nil {
			rep.Findings = append(rep.Findings, Finding{
				Severity: SeverityWarning,
				File:     rootFile,
				Message:  fmt.Sprintf("never loaded: plugins load rules only from rules/. Move it to rules/%s", rootFile),
			})
		}
	}
	if st, err := fs.Stat(tree, "workflows"); err == nil && st.IsDir() {
		rep.Findings = append(rep.Findings, Finding{
			Severity: SeverityWarning,
			File:     "workflows",
			Message:  "never loaded: plugins don't load Markdown workflows. Make each one a skill in skills/<name>/SKILL.md",
		})
	}
}
