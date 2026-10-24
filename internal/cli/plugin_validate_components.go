package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
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
func scanValidateSkills(dir string, rep *validateReport) {
	excluded := validateExclusions(dir, "skills.json", rep)
	files := collectSkillFiles(dir, excluded)
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

func collectSkillFiles(dir string, excluded map[string]bool) []skillFile {
	entries, err := os.ReadDir(filepath.Join(dir, "skills"))
	if err != nil {
		return nil
	}
	var files []skillFile
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		rel := filepath.ToSlash(filepath.Join("skills", e.Name(), "SKILL.md"))
		raw, err := os.ReadFile(filepath.Join(dir, rel))
		if err != nil {
			continue
		}
		files = append(files, skillFile{folder: e.Name(), rel: rel, raw: raw, excluded: excluded[e.Name()]})
	}
	return files
}

func validateSkillFile(f skillFile, seen map[string]string, rep *validateReport) {
	fm, body, fmErr, ok := parseFrontmatter(bytes.TrimPrefix(f.raw, utf8BOM))
	if !ok {
		rep.Findings = append(rep.Findings, validateFinding{
			Severity: validateError,
			File:     f.rel,
			Message:  "no frontmatter: the file must start with a --- block holding name and description. The loader skips this skill",
		})
		return
	}
	if fmErr != nil {
		rep.Findings = append(rep.Findings, validateFinding{
			Severity: validateError,
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
		rep.Findings = append(rep.Findings, validateFinding{
			Severity: validateError,
			File:     f.rel,
			Message:  "the frontmatter isn't valid YAML: failed to parse frontmatter: " + err.Error() + ". The loader skips this skill",
		})
		return
	}
	validateSkillSemantic(f, fields.Name, fields.Description, body, seen, rep)
}

func resolveSkillNameAndDesc(f skillFile, name, desc string, rep *validateReport) string {
	effective := name
	if effective == "" {
		effective = "SKILL"
		if !f.excluded {
			rep.Findings = append(rep.Findings, validateFinding{
				Severity: validateWarning,
				File:     f.rel,
				Message:  fmt.Sprintf("no name in the frontmatter: the skill is named %q after its file. Add name: %s", effective, f.folder),
			})
		}
	}
	if desc == "" && !f.excluded {
		rep.Findings = append(rep.Findings, validateFinding{
			Severity: validateWarning,
			File:     f.rel,
			Message:  "no description: the model uses it to decide when to load the skill",
		})
	}
	return effective
}

func validateSkillSemantic(f skillFile, name, desc string, body []byte, seen map[string]string, rep *validateReport) {
	effective := resolveSkillNameAndDesc(f, name, desc, rep)
	prev, dup := seen[effective]
	if dup {
		rep.Findings = append(rep.Findings, validateFinding{
			Severity: validateWarning,
			File:     f.rel,
			Message:  fmt.Sprintf("skipped: %s already defines a skill named %q", prev, effective),
		})
	}
	if name != "" && name != f.folder && (!f.excluded || dup) {
		rep.Findings = append(rep.Findings, validateFinding{
			Severity: validateWarning,
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
		rep.Findings = append(rep.Findings, validateFinding{
			Severity: validateWarning,
			File:     f.rel,
			Message:  fmt.Sprintf("the body is %d bytes; only the first %d are loaded", n, pluginValidateBodyLimit),
		})
	}
}

// scanValidateAgents walks flat `agents/*.md` plus `agents/<name>/agent.json`.
func scanValidateAgents(dir string, rep *validateReport) {
	excluded := validateExclusions(dir, "agents.json", rep)
	files := collectAgentFiles(dir, excluded, rep)
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

func collectAgentFiles(dir string, excluded map[string]bool, rep *validateReport) []agentFile {
	entries, err := os.ReadDir(filepath.Join(dir, "agents"))
	if err != nil {
		return nil
	}
	var files []agentFile
	for _, e := range entries {
		full := filepath.Join(dir, "agents", e.Name())
		if e.IsDir() {
			validateAgentJSONFolder(full, e.Name(), rep)
			continue
		}
		if !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		raw, err := os.ReadFile(full)
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

func validateAgentJSONFolder(full, name string, rep *validateReport) {
	rel := filepath.ToSlash(filepath.Join("agents", name, "agent.json"))
	raw, err := os.ReadFile(filepath.Join(full, "agent.json"))
	if err != nil {
		return
	}
	if bytes.HasPrefix(raw, utf8BOM) {
		rep.Findings = append(rep.Findings, validateFinding{
			Severity: validateError,
			File:     rel,
			Message:  "starts with a UTF-8 byte order mark, which the loader can't parse. Save the file without one. The loader skips this agent",
		})
		return
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		rep.Findings = append(rep.Findings, validateFinding{
			Severity: validateError,
			File:     rel,
			Message:  jsonPosMessage(raw, err) + ". The loader skips this agent",
		})
		return
	}
	rep.Findings = append(rep.Findings, validateFinding{
		Severity: validateWarning,
		File:     rel,
		Message:  "never loaded by this build: only internal builds read agent.json; write the agent as agent.md",
	})
}

func validateAgentFile(f agentFile, seen map[string]string, rep *validateReport) {
	fm, _, fmErr, ok := parseFrontmatter(f.raw)
	if !ok {
		rep.Findings = append(rep.Findings, validateFinding{
			Severity: validateWarning,
			File:     f.rel,
			Message:  "never loaded: an agent file starts with frontmatter that has a name and description",
		})
		return
	}
	if fmErr != nil {
		rep.Findings = append(rep.Findings, validateFinding{
			Severity: validateError,
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
		rep.Findings = append(rep.Findings, validateFinding{
			Severity: validateError,
			File:     f.rel,
			Message:  "the frontmatter isn't valid YAML: failed to unmarshal frontmatter YAML: " + err.Error() + ". The loader skips this agent",
		})
		return
	}
	checkAgentFields(f, fields.Name, fields.Description, seen, rep)
}

func checkAgentFields(f agentFile, name, desc string, seen map[string]string, rep *validateReport) {
	if name == "" {
		rep.Findings = append(rep.Findings, validateFinding{
			Severity: validateError,
			File:     f.rel,
			Message:  "the frontmatter has no name. The loader skips this agent",
		})
		return
	}
	if desc == "" {
		rep.Findings = append(rep.Findings, validateFinding{
			Severity: validateError,
			File:     f.rel,
			Message:  "the frontmatter has no description. The loader skips this agent",
		})
		return
	}
	if reservedAgentNames[name] {
		rep.Findings = append(rep.Findings, validateFinding{
			Severity: validateError,
			File:     f.rel,
			Message:  fmt.Sprintf("the name %q is reserved for a built-in agent, so this agent never loads", name),
		})
		return
	}
	if prev, dup := seen[name]; dup {
		rep.Findings = append(rep.Findings, validateFinding{
			Severity: validateWarning,
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
