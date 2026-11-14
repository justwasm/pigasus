package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	yaml "gopkg.in/yaml.v3"
)

// ---- plugin_validate_components.go ----

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

// ---- plugin_validate_format.go ----

func outputValidateHuman(j *JetskiCLI, res *validateResult, strict bool) error {
	code := validateExitCode(res.Report, strict)
	writeValidateHuman(j.Out, res)
	footer := validateFooter(res, strict)
	if code != 0 {
		// The reference sends a failing footer to stderr; a passing one
		// (including "passed with N warnings") stays on stdout.
		fmt.Fprintln(j.ErrOut, footer)
		return errExitCode(code)
	}
	if res.ColorOutput {
		footer = "\x1b[32m✔\x1b[0m" + strings.TrimPrefix(footer, "✔")
	}
	fmt.Fprintln(j.Out, footer)
	return nil
}

// writeValidateHuman renders the reference's human block:
//
//	Validating <name> (<manifest>)
//	<blank>
//	  <symbol> <file>: <message>
//	<blank>
//	  skills N · agents N · rules N · hooks N · mcpServers N · sidecars N
//	<blank>
//	<footer>
//
// The summary line is omitted when the manifest could not be resolved or
// parsed.
func writeValidateHuman(w io.Writer, res *validateResult) {
	if res.ManifestOK {
		fmt.Fprintf(w, "Validating %s (%s)\n", res.HeaderName, filepath.Join(res.HeaderPath, res.Report.Manifest))
	} else {
		fmt.Fprintf(w, "Validating %s\n", res.HeaderPath)
	}
	fmt.Fprintln(w)

	for _, f := range res.Report.Findings {
		symbol := "·"
		if res.ColorOutput {
			switch f.Severity {
			case validateError:
				symbol = "\x1b[31m✗\x1b[0m"
			case validateWarning:
				symbol = "\x1b[33m⚠\x1b[0m"
			default:
				symbol = "\x1b[36m·\x1b[0m"
			}
		} else {
			switch f.Severity {
			case validateError:
				symbol = "✗"
			case validateWarning:
				symbol = "⚠"
			}
		}
		fmt.Fprintf(w, "  %s %s: %s\n", symbol, f.File, f.Message)
	}
	if len(res.Report.Findings) > 0 {
		fmt.Fprintln(w)
	}

	if res.ManifestOK {
		c := res.Report.Components
		fmt.Fprintf(w, "  skills %d · agents %d · rules %d · hooks %d · mcpServers %d · sidecars %d\n",
			c.Skills, c.Agents, c.Rules, c.Hooks, c.MCPServers, c.Sidecars)
		fmt.Fprintln(w)
	}
}

// validateFooter builds the reference's footer text.
func validateFooter(res *validateResult, strict bool) string {
	e, w := res.ErrorCount, res.WarnCount
	switch {
	case e > 0:
		return fmt.Sprintf("Error: validation failed: %d %s, %d %s", e, plural(e, "error", "errors"), w, plural(w, "warning", "warnings"))
	case w > 0 && strict:
		return fmt.Sprintf("Error: validation failed in strict mode: %d %s", w, plural(w, "warning", "warnings"))
	case w > 0:
		return fmt.Sprintf("✔ Validation passed with %d %s", w, plural(w, "warning", "warnings"))
	default:
		return "✔ Validation passed"
	}
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// isTerminalWriter reports whether w is a character device, which is when the
// reference enables ANSI colour.
func isTerminalWriter(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// ---- plugin_validate_hooks.go ----

// hookEvents are the event names the reference recognises in hooks.json.
const hookEvents = "SessionStart, PreInvocation, PostInvocation, Stop, PreToolUse, PostToolUse"

// scanValidateHooks counts hooks.json events and warns about unknown event
// keys in the legacy flat shape.
func scanValidateHooks(dir string, rep *validateReport) {
	raw, err := os.ReadFile(filepath.Join(dir, "hooks.json"))
	if err != nil {
		return
	}
	if bytes.HasPrefix(raw, utf8BOM) {
		rep.Findings = append(rep.Findings, validateFinding{
			Severity: validateError,
			File:     "hooks.json",
			Message:  "starts with a UTF-8 byte order mark, which the loader can't parse. Save the file without one. The loader skips this whole file",
		})
		return
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		rep.Findings = append(rep.Findings, validateFinding{
			Severity: validateError,
			File:     "hooks.json",
			Message:  "failed to parse hooks.json: " + err.Error() + ". The loader skips this whole file",
		})
		return
	}
	if hooksRaw, ok := top["hooks"]; ok {
		validateWrappedHooks(hooksRaw, len(top), rep)
		return
	}
	validateFlatHooks(top, rep)
}

// validateWrappedHooks validates Claude Code style wrapped hooks ("hooks": {...}).
func validateWrappedHooks(hooksRaw json.RawMessage, topCount int, rep *validateReport) {
	var events map[string]json.RawMessage
	if err := json.Unmarshal(hooksRaw, &events); err != nil {
		rep.Findings = append(rep.Findings, validateFinding{
			Severity: validateError,
			File:     "hooks.json",
			Message:  "failed to parse hooks.json: " + hookErrorText(err.Error()) + ". The loader skips this whole file",
		})
		return
	}
	names := make([]string, 0, len(events))
	for name := range events {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		var groups []json.RawMessage
		if err := json.Unmarshal(events[name], &groups); err != nil {
			rep.Findings = append(rep.Findings, validateFinding{
				Severity: validateError,
				File:     "hooks.json",
				Message:  fmt.Sprintf("failed to parse hooks.json: json: cannot unmarshal object into Go struct field .hooks.%s of type []jsonhook.ToolHookGroup. The loader skips this whole file", name),
			})
			return
		}
		for _, g := range groups {
			var spec map[string]json.RawMessage
			if err := json.Unmarshal(g, &spec); err != nil {
				continue
			}
			if _, ok := spec["command"]; !ok {
				rep.Findings = append(rep.Findings, validateFinding{
					Severity: validateError,
					File:     "hooks.json",
					Message:  `invalid hook "hooks": command hook must specify 'command'. The loader skips this whole file`,
				})
				return
			}
			checkHookCommands(g, rep)
		}
	}
	rep.Findings = append(rep.Findings, validateFinding{
		Severity: validateInfo,
		File:     "hooks.json",
		Message:  `"hooks" wraps the events, Claude Code style, so its handlers load as one hook named "hooks". Name each hook at the top level instead: {"my-hook": {"PreToolUse": [...]}}`,
	})
	rep.Components.Hooks = topCount
}

// validateFlatHooks validates legacy flat style hooks ({"my-hook": {"PreToolUse": ...}}).
func validateFlatHooks(top map[string]json.RawMessage, rep *validateReport) {
	rep.Components.Hooks = len(top)
	known := map[string]bool{}
	for _, e := range strings.Split(hookEvents, ", ") {
		known[e] = true
	}
	names := make([]string, 0, len(top))
	for name := range top {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		var spec map[string]json.RawMessage
		if err := json.Unmarshal(top[name], &spec); err != nil {
			rep.Findings = append(rep.Findings, validateFinding{
				Severity: validateError,
				File:     "hooks.json",
				Message:  "failed to parse hooks.json: " + hookErrorText(err.Error()) + ". The loader skips this whole file",
			})
			continue
		}
		keys := make([]string, 0, len(spec))
		for key := range spec {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if !known[key] {
				rep.Findings = append(rep.Findings, validateFinding{
					Severity: validateWarning,
					File:     "hooks.json",
					Message:  fmt.Sprintf("hook %q: unknown event %q is ignored (events: %s)", name, key, hookEvents),
				})
			} else {
				checkHookCommands(spec[key], rep)
			}
		}
	}
}

func checkHookCommands(raw json.RawMessage, rep *validateReport) {
	if len(raw) == 0 {
		return
	}
	var single map[string]any
	if err := json.Unmarshal(raw, &single); err == nil {
		if cmd, ok := single["command"].(string); ok {
			if strings.Contains(cmd, "${PLUGIN_ROOT}") || strings.Contains(cmd, "$PLUGIN_ROOT") {
				rep.Findings = append(rep.Findings, validateFinding{
					Severity: validateWarning,
					File:     "hooks.json",
					Message:  `hook command uses ${PLUGIN_ROOT} or $PLUGIN_ROOT; hooks already run inside the plugin folder, suggest using a relative path instead`,
				})
			}
		}
		return
	}
	var list []map[string]any
	if err := json.Unmarshal(raw, &list); err == nil {
		for _, item := range list {
			if cmd, ok := item["command"].(string); ok {
				if strings.Contains(cmd, "${PLUGIN_ROOT}") || strings.Contains(cmd, "$PLUGIN_ROOT") {
					rep.Findings = append(rep.Findings, validateFinding{
						Severity: validateWarning,
						File:     "hooks.json",
						Message:  `hook command uses ${PLUGIN_ROOT} or $PLUGIN_ROOT; hooks already run inside the plugin folder, suggest using a relative path instead`,
					})
				}
			}
		}
	}
}

// hookErrorText rewrites encoding/json's anonymous type names to the
// reference's `jsonhook` package names.
func hookErrorText(msg string) string {
	msg = strings.ReplaceAll(msg, "map[string]jsontext.Value", "map[string]jsonhook.JSONHookSpec")
	msg = strings.ReplaceAll(msg, "[]jsontext.Value", "[]jsonhook.ToolHookGroup")
	msg = strings.ReplaceAll(msg, "jsontext.Value", "jsonhook.JSONHookSpec")
	return msg
}

// scanValidateMCPServers counts `mcp_config.json` servers.
func scanValidateMCPServers(dir string, rep *validateReport) {
	raw, err := os.ReadFile(filepath.Join(dir, "mcp_config.json"))
	if err != nil {
		return
	}
	raw = bytes.TrimPrefix(raw, utf8BOM)
	var config struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		rep.Findings = append(rep.Findings, validateFinding{
			Severity: validateError,
			File:     "mcp_config.json",
			Message:  "failed to parse mcp_config.json: " + err.Error() + ". The loader skips this whole file",
		})
		return
	}
	rep.Components.MCPServers = len(config.MCPServers)
}

// ---- plugin_validate_json.go ----

// plugin_validate_json.go — `agy plugin validate` parity with AGY 1.4.3.
//
// The 1.4.1 changelog bullet (internal/cli/testdata/changelog_reference.txt:16)
// says the validator now also checks a plugin's skills, subagents and rules,
// reports each finding with its file (and line where one applies), accepts a
// plugin directory or its `plugin.json`, and adds `--strict` / `--json`.
//
// Cycle 238 implemented that bullet from the changelog prose alone, and the
// implementation has since drifted from the binary: the human block was the
// pre-1.4.1 `[ok] … processed` form, and `--json` used a made-up schema
// (`version` / `plugin_path` / `errors` / `warnings`). A live probe of
// `agy 1.4.3` shows a different surface entirely:
//
//	Validating <name> (<manifest>)
//
//	  ✗ skills/x/SKILL.md: no frontmatter: … The loader skips this skill
//	  ⚠ skills/y/SKILL.md: no name in the frontmatter: …
//	  · plugin.json: declares the Agent Plugins 1.0.0 $schema: …
//
//	  skills 0 · agents 0 · rules 0 · hooks 0 · mcpServers 0 · sidecars 0
//
//	Error: validation failed: 1 error, 1 warning
//
// and the JSON document is
//
//	{"dir","manifest","name","agentPluginsSpec",
//	 "components":{"agents","hooks","mcpServers","rules","sidecars","skills"},
//	 "findings":[{"severity","file","message"}]}
//
// This file reproduces that surface. Evidence is the live binary probed with
// crafted fixtures (see docs/cycles/plugin-validate-components/deliverable.md);
// messages quoted in the constants below are copied from its stdout.

// validateSeverity is the wire value of a finding. The reference emits
// "error", "warning" and "info"; info findings render with a middle dot.
type validateSeverity string

const (
	validateError   validateSeverity = "error"
	validateWarning validateSeverity = "warning"
	validateInfo    validateSeverity = "note"
)

// validateFinding is one entry in the reference's `findings` array. There is
// deliberately no `line` field: the reference folds line information into
// `message` for YAML/JSON parse failures (e.g. `line 1, column 3: …`).
type validateFinding struct {
	Severity validateSeverity `json:"severity"`
	File     string           `json:"file"`
	Message  string           `json:"message"`
}

// validateComponents is the summary line / `components` object. YAML key order
// matches the reference's `snake`-free JSON (agents, hooks, mcpServers, rules,
// sidecars, skills).
type validateComponents struct {
	Agents     int `json:"agents"`
	Hooks      int `json:"hooks"`
	MCPServers int `json:"mcpServers"`
	Rules      int `json:"rules"`
	Sidecars   int `json:"sidecars"`
	Skills     int `json:"skills"`
}

// validateReport is the structured `--json` document. Field order is the
// reference's marshal order, so `--json` output diffs cleanly against a live
// capture.
type validateReport struct {
	Dir              string             `json:"dir"`
	Manifest         string             `json:"manifest,omitempty"`
	Name             string             `json:"name,omitempty"`
	AgentPluginsSpec bool               `json:"agentPluginsSpec"`
	Components       validateComponents `json:"components"`
	Findings         []validateFinding  `json:"findings"`
}

// validateResult bundles the report with the two pieces of state the human
// writer needs but the JSON schema does not carry.
type validateResult struct {
	Report      *validateReport
	ManifestOK  bool // manifest parsed: header names it, summary line prints
	HeaderName  string
	HeaderPath  string
	Strict      bool
	ErrorCount  int
	WarnCount   int
	ColorOutput bool
}

type validateFlags struct {
	strict bool
	asJSON bool
	path   string
	// help is set by -h / --help: the reference prints the usage
	// block on stderr and exits 0.
	help bool
}

// usagePluginValidate is the reference's `plugin validate` usage block,
// byte for byte (the program name is the only variable part).
//
// The reference prints it on stderr and exits 0 for `--help` / `-h`.
func usagePluginValidate(n string) string {
	return "Usage: " + n + " plugin validate [path] [--strict] [--json]\n" +
		"\n" +
		"Checks a plugin the way the plugin loader reads it, and reports the files and\n" +
		"fields that the loader skips or ignores.\n" +
		"\n" +
		"Arguments:\n" +
		"  path        A plugin directory, or its plugin.json (default: the current\n" +
		"              directory)\n" +
		"\n" +
		"Flags:\n" +
		"  --strict    Also fail on warnings\n" +
		"  --json      Print the report as JSON\n" +
		"  -h, --help  Show this help\n" +
		"\n" +
		"Each finding is an error (the loader skips a file or the plugin, or an item\n" +
		"can never work), a warning (a field or file is ignored or falls back to a\n" +
		"default) or a note (how the plugin loads). Exits non-zero if there are errors,\n" +
		"or warnings with --strict.\n"
}

// runPluginValidateReference is the entry point for `agy plugin validate`.
//
// Reference surface (probed against the 1.4.6 binary):
//
//	agy plugin validate [path] [--strict] [--json]
//
// `--help` / `-h` print the usage block on stderr and exit 0. Any other
// argument is rejected with
// `Error: unknown validate argument "<arg>": usage is validate [path] [--strict] [--json]. See validate --help`
// on stderr and exit 1.
func runPluginValidateReference(j *JetskiCLI, args []string) error {
	flags, err := parseValidateArgs(args)
	if err != nil {
		return fail(j, 1, err.Error())
	}
	if flags.help {
		fmt.Fprint(j.ErrOut, usagePluginValidate(j.Name))
		return nil
	}

	res := buildValidateResult(flags.path)
	res.Strict = flags.strict
	res.ColorOutput = isTerminalWriter(j.Out)

	if flags.asJSON {
		return outputValidateJSON(j, res, flags.strict)
	}
	return outputValidateHuman(j, res, flags.strict)
}

func parseValidateArgs(args []string) (validateFlags, error) {
	var flags validateFlags
	var positional []string
	for _, a := range args {
		switch a {
		case "--strict":
			flags.strict = true
			continue
		case "--json":
			flags.asJSON = true
			continue
		case "-h", "--help":
			flags.help = true
			continue
		}
		if strings.HasPrefix(a, "-") || len(positional) > 0 {
			return flags, fmt.Errorf("Error: unknown validate argument %q: usage is validate [path] [--strict] [--json]. See validate --help", a)
		}
		positional = append(positional, a)
	}
	flags.path = "."
	if len(positional) == 1 {
		flags.path = positional[0]
	}
	return flags, nil
}

func outputValidateJSON(j *JetskiCLI, res *validateResult, strict bool) error {
	enc := json.NewEncoder(j.Out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(res.Report); err != nil {
		return err
	}
	code := validateExitCode(res.Report, strict)
	if code != 0 {
		fmt.Fprintln(j.ErrOut, validateFooter(res, strict))
	}
	return errExitCode(code)
}

// buildValidateResult resolves the path, parses the manifest, walks the
// components, and returns the report plus the header/footer state.
func buildValidateResult(inputPath string) *validateResult {
	dir, manifestRel, errRes := resolveValidateDirAndManifest(inputPath)
	if errRes != nil {
		return errRes
	}
	res := &validateResult{
		Report: &validateReport{
			Dir:      dir,
			Manifest: filepath.ToSlash(manifestRel),
			Findings: []validateFinding{},
		},
		HeaderPath: dir,
	}
	validateManifest(dir, manifestRel, res)
	if !res.ManifestOK {
		return res
	}
	checkMisplacedHiddenComponents(dir, res.Report.Manifest, res.Report)
	scanAllComponents(dir, res.Report)
	finaliseValidateResult(res)
	return res
}

func resolveValidateDirAndManifest(input string) (string, string, *validateResult) {
	info, statErr := os.Stat(input)
	if statErr != nil {
		return "", "", validateDirStatError(input)
	}

	dir := input
	manifestRel := ""
	if !info.IsDir() {
		dir = filepath.Dir(input)
		if dir == "." {
			if abs, err := filepath.Abs("."); err == nil {
				dir = abs
			}
		}
		manifestRel = filepath.Base(input)
	} else {
		manifestRel = locateManifestInSubdirs(dir)
	}
	if manifestRel == "" {
		return "", "", validateManifestNotFoundError(dir)
	}
	return dir, manifestRel, nil
}

func locateManifestInSubdirs(dir string) string {
	for _, sub := range []string{"", ".antigravity-plugin", ".gemini-plugin"} {
		if name := findManifestIn(filepath.Join(dir, sub)); name != "" {
			return filepath.Join(sub, name)
		}
	}
	return ""
}

func validateDirStatError(input string) *validateResult {
	return &validateResult{
		Report: &validateReport{
			Dir:      input,
			Findings: []validateFinding{{Severity: validateError, File: ".", Message: input + " is not a directory"}},
		},
		HeaderPath: input,
		ErrorCount: 1,
	}
}

func validateManifestNotFoundError(dir string) *validateResult {
	return &validateResult{
		Report: &validateReport{
			Dir: dir,
			Findings: []validateFinding{{
				Severity: validateError,
				File:     "plugin.json",
				Message:  "no plugin.json found at the plugin root or in .antigravity-plugin or .gemini-plugin",
			}},
		},
		HeaderPath: dir,
		ErrorCount: 1,
	}
}

func scanAllComponents(dir string, rep *validateReport) {
	scanValidateSkills(dir, rep)
	scanValidateAgents(dir, rep)
	scanValidateRules(dir, rep)
	scanValidateHooks(dir, rep)
	scanValidateMCPServers(dir, rep)
	checkRootMisplacedFiles(dir, rep)
}

// finaliseValidateResult sorts findings (errors, then warnings, then infos,
// each by file and message), counts them, and sets the report's slice.
func finaliseValidateResult(res *validateResult) {
	rep := res.Report
	rank := func(s validateSeverity) int {
		switch s {
		case validateError:
			return 0
		case validateWarning:
			return 1
		default:
			return 2
		}
	}
	sort.SliceStable(rep.Findings, func(i, k int) bool {
		a, b := rep.Findings[i], rep.Findings[k]
		if rank(a.Severity) != rank(b.Severity) {
			return rank(a.Severity) < rank(b.Severity)
		}
		return a.File < b.File
	})
	for _, f := range rep.Findings {
		switch f.Severity {
		case validateError:
			res.ErrorCount++
		case validateWarning:
			res.WarnCount++
		}
	}
	if rep.Findings == nil {
		rep.Findings = []validateFinding{}
	}
}

// validateExitCode mirrors the reference: errors are fatal, warnings only
// fatal under --strict.
func validateExitCode(rep *validateReport, strict bool) int {
	var e, w int
	for _, f := range rep.Findings {
		switch f.Severity {
		case validateError:
			e++
		case validateWarning:
			w++
		}
	}
	if e > 0 || (strict && w > 0) {
		return 1
	}
	return 0
}

// ---- plugin_validate_logo.go ----

// plugin_validate_logo.go — the AGY 1.4.9 validator's image checks.
//
// 【一手】 agy 1.4.9, the vendor's own release notes:
//
//	Improved `agy plugin validate` to warn when a plugin's `name` isn't
//	lowercase kebab-case, such as `Neo4j` or `foo_bar`, which the
//	marketplace can't publish, and to suggest a valid name, failing under
//	`--strict`; it now also checks the plugin's `logo`, noting when
//	plugin.json has none and warning when the logo path is blank, uses
//	backslashes, is absolute or points outside the plugin, sits in a folder
//	the marketplace bundle leaves out such as .github/, or is a URL that
//	won't be shown, with the same checks for context provider `iconUri`
//	values.
//
// Every message below was read off the 1.4.9 binary's own stdout, one
// crafted fixture per branch (docs/cycles/cycle-362-agy-1.4.9-alignment/
// deliverable.md lists the fixtures). The reference's own message strings
// live in the binary as format templates:
//
//	%s %q isn't shown: the client can't parse it as a URL%s. Fix the URL, or
//	    put the image in the plugin and use a relative path, like "assets/logo.svg"
//	%s %q is outside the plugin root, so the Marketplace bundle can't include
//	    it and the Marketplace build rejects it. Move the image into the plugin
//	    and use a path like "assets/logo.svg"
//	%s %q won't be packaged in the Marketplace bundle%s: the bundle leaves out
//	    paths with a part starting with "." (like .github/ or .logo.png) or named
//	    __pycache__. Move it, for example to "assets/"
//	%s %q isn't shown: the client only shows https images on google.com,
//	    gstatic.com, googleusercontent.com or googleplex.com%s. Put the image
//	    in the plugin and use a relative path, like "assets/logo.svg"
//
// The leading `%s` is the field's own name ("logo", "iconUri"), which is why
// the same checks serve both the manifest's logo and a context provider's
// iconUri.

// pluginImageExtensions are the extensions the Marketplace build accepts.
var pluginImageExtensions = map[string]bool{
	".png":  true,
	".svg":  true,
	".jpg":  true,
	".jpeg": true,
	".webp": true,
}

// googleImageDomains are the hosts the client is willing to show an image
// from. A host matches when it is the domain itself or a subdomain of it.
var googleImageDomains = []string{
	"google.com",
	"gstatic.com",
	"googleusercontent.com",
	"googleplex.com",
}

// validatePluginImageRef checks one image reference the way the reference's
// validator does, and returns the finding it produces.
//
// field is the JSON field's name ("logo", "iconUri"); it is what the messages
// quote. dir is the plugin root the reference is resolved against.
//
// The returned bool is false when the reference produces no finding — a
// relative path to an existing image, or an https URL on a Google domain.
func validatePluginImageRef(dir, field, ref string) (validateSeverity, string, bool) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return validateWarning, fmt.Sprintf(
			"%q is blank: the Marketplace build rejects it. Remove the field or set it to an image inside the plugin, like %q",
			field, "assets/logo.svg"), true
	}
	if sev, msg, handled := validatePluginImageURL(field, ref); handled {
		if sev == "" {
			return "", "", false
		}
		return sev, msg, true
	}
	return validatePluginImagePath(dir, field, ref)
}

// validatePluginImageURL handles the URL half of the check.
//
// The reference enters this branch for anything that parses as a URL with a
// scheme ("https://…", but also "mailto:…" and "C:\logo.png"), and for
// anything that looks like a URL but does not parse ("https://exa mple.com/…"
// — a space in the host).
//
// The third return value says whether the reference took the URL branch at
// all; an empty severity then means the reference is happy with the URL.
func validatePluginImageURL(field, ref string) (validateSeverity, string, bool) {
	u, err := url.Parse(ref)
	if err == nil && u.Scheme != "" {
		if !strings.EqualFold(u.Scheme, "https") || u.Host == "" {
			return validateWarning, fmt.Sprintf(
				"%s %q is ignored and the Marketplace build rejects it: use an https URL on a Google domain, or a relative path to an image inside the plugin",
				field, ref), true
		}
		if !isGoogleImageHost(u.Hostname()) {
			return validateWarning, fmt.Sprintf(
				"%s %q isn't shown: the client only shows https images on google.com, gstatic.com, googleusercontent.com or googleplex.com. The Marketplace build accepts it, but the plugin gets a generic icon. Put the image in the plugin and use a relative path, like %q",
				field, ref, "assets/logo.svg"), true
		}
		return "", "", true
	}
	if err != nil && looksLikeURL(ref) {
		return validateWarning, fmt.Sprintf(
			"%s %q isn't shown: the client can't parse it as a URL. The Marketplace build accepts it, but the plugin gets a generic icon. Fix the URL, or put the image in the plugin and use a relative path, like %q",
			field, ref, "assets/logo.svg"), true
	}
	return "", "", false
}

// looksLikeURL reports whether ref starts with a scheme and "://" — the shape
// that is meant to be a URL even when it does not parse.
func looksLikeURL(ref string) bool {
	scheme := ref
	if i := strings.Index(scheme, "://"); i >= 0 {
		scheme = scheme[:i]
	} else {
		return false
	}
	if scheme == "" {
		return false
	}
	for i, r := range scheme {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9', r == '+', r == '-', r == '.':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// isGoogleImageHost reports whether the client would show an image from host.
func isGoogleImageHost(host string) bool {
	host = strings.ToLower(host)
	for _, d := range googleImageDomains {
		if host == d || strings.HasSuffix(host, "."+d) {
			return true
		}
	}
	return false
}

// validatePluginImagePath handles the relative-path half of the check.
//
// The order of the branches is the reference's own: a Windows path, then an
// absolute one, then one that escapes the plugin root, then a path whose
// last element has no image extension, then one the Marketplace bundle
// leaves out, then one that is not a plain image path, then one whose file
// is missing.
func validatePluginImagePath(dir, field, ref string) (validateSeverity, string, bool) {
	if strings.Contains(ref, "\\") {
		return validateWarning, fmt.Sprintf(
			"%s %q uses backslashes, which only work on Windows, and the Marketplace build rejects it: use forward slashes, like %q",
			field, ref, "assets/logo.svg"), true
	}
	if strings.HasPrefix(ref, "/") {
		return validateWarning, fmt.Sprintf(
			"%s %q is an absolute path, which the client ignores and the Marketplace build rejects: use a path relative to the plugin root, like %q",
			field, ref, "assets/logo.svg"), true
	}
	cleaned := path.Clean(ref)
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return validateWarning, fmt.Sprintf(
			"%s %q is outside the plugin root, so the Marketplace bundle can't include it and the Marketplace build rejects it. Move the image into the plugin and use a path like %q",
			field, ref, "assets/logo.svg"), true
	}
	if !pluginImageExtensions[strings.ToLower(path.Ext(path.Base(cleaned)))] {
		return validateWarning, fmt.Sprintf(
			"%s %q is ignored and the Marketplace build rejects it: use a .png, .svg, .jpg, .jpeg or .webp file inside the plugin, or an https URL on a Google domain",
			field, ref), true
	}
	for _, part := range strings.Split(cleaned, "/") {
		if strings.HasPrefix(part, ".") || part == "__pycache__" {
			return validateWarning, fmt.Sprintf(
				"%s %q won't be packaged in the Marketplace bundle, so the Marketplace build fails: the bundle leaves out paths with a part starting with %q (like .github/ or .logo.png) or named __pycache__. Move it, for example to %q",
				field, ref, ".", "assets/"), true
		}
	}
	if strings.HasSuffix(ref, "/") {
		return validateWarning, fmt.Sprintf(
			"%s %q isn't shown: the client can't read it as an image path. The Marketplace build accepts it, but the plugin gets a generic icon. Use a plain path, like %q",
			field, ref, "assets/logo.svg"), true
	}
	info, statErr := os.Stat(filepath.Join(dir, filepath.FromSlash(ref)))
	if statErr != nil || info.IsDir() {
		return validateWarning, fmt.Sprintf(
			"%s file %q doesn't exist, so the plugin shows a generic icon and the Marketplace build fails",
			field, ref), true
	}
	return "", "", false
}

// validatePluginLogo checks the manifest's "logo" field.
//
// A manifest with no logo at all gets the reference's note; every other
// shape is handed to the shared image checks.
func validatePluginLogo(dir, manifestRel, logo string, present bool, rep *validateReport) {
	file := filepath.ToSlash(manifestRel)
	if !present {
		rep.Findings = append(rep.Findings, validateFinding{
			Severity: validateInfo,
			File:     file,
			Message:  `no logo: the plugin shows a generic icon. Add "logo": "assets/logo.svg" (a .png, .svg, .jpg, .jpeg or .webp file inside the plugin)`,
		})
		return
	}
	if sev, msg, ok := validatePluginImageRef(dir, "logo", logo); ok {
		rep.Findings = append(rep.Findings, validateFinding{
			Severity: sev,
			File:     file,
			Message:  msg,
		})
	}
}

// ---- plugin_validate_permissions.go ----

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

// ---- plugin_validate_schema.go ----

// knownPluginManifestFields is the reference's recognised key set. Probed
// field-by-field: every key here produced no warning, every other candidate
// (`agents`, `commands`, `rules`, `hooks`, `mcpServers`, `sidecars`, `tags`,
// `categories`, `publisher`, `private`, `files`, `dependencies`, `scripts`,
// `os`, `cpu`, `type`, `$id`, `iconUrl`, `website`, `bugs`, `readme`, `icon`)
// produced `unknown field "…" is ignored`.
var knownPluginManifestFields = map[string]bool{
	"name":         true,
	"version":      true,
	"description":  true,
	"author":       true,
	"homepage":     true,
	"license":      true,
	"repository":   true,
	"keywords":     true,
	"displayName":  true,
	"display_name": true,
	"category":     true,
	"logo":         true,
	"skills":       true,
	"$schema":      true,
	// 【一手】 agy 1.4.9 accepts "extensions" (it carries the plugin's
	// context providers). Probed: every other candidate key still produces
	// `unknown field "…" is ignored`.
	"extensions": true,
}

// agentPluginsSchemaPrefix is the `$schema` prefix that selects the Agent
// Plugins loader. Only version 1.0.0 is supported by agy 1.4.3.
const agentPluginsSchemaPrefix = "https://agent-plugins.org/schemas/"
const agentPluginsVersion = "1.0.0"

// validateJSONConfig is the shared shape of skills.json / agents.json /
// rules.json: a list of component names to exclude. The reference's type is
// named customizations.JSONConfig; the error-text rewrite below mirrors that.
type validateJSONConfig struct {
	Exclude []string `json:"exclude"`
}

// findManifestIn returns the actual filename of a case-insensitive
// `plugin.json` in dir, or "".
func findManifestIn(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if !e.IsDir() && strings.EqualFold(e.Name(), "plugin.json") {
			return e.Name()
		}
	}
	return ""
}

// validateManifest reads and validates the plugin manifest file.
//
// The findings come out in the reference's own order: the unknown fields
// first, then the name, then the $schema note, then the logo.
//
// 【一手】 agy 1.4.9, probed against the binary:
//
//	⚠ plugin.json: unknown field "bogus" is ignored
//	⚠ plugin.json: name "Neo4j" isn't strict kebab-case (lowercase letters, digits and single hyphens), so it can't be published to the Antigravity Marketplace; try "neo4j"
//	· plugin.json: declares the Agent Plugins 1.0.0 $schema: it loads as an Agent Plugins plugin while the enable-agent-plugins-spec flag is on, and as a native plugin otherwise
//	· plugin.json: no logo: the plugin shows a generic icon. Add "logo": "assets/logo.svg" (a .png, .svg, .jpg, .jpeg or .webp file inside the plugin)
func validateManifest(dir, manifestRel string, res *validateResult) {
	rep := res.Report
	raw, readErr := os.ReadFile(filepath.Join(dir, manifestRel))
	if readErr != nil {
		rep.Findings = append(rep.Findings, validateFinding{
			Severity: validateError,
			File:     filepath.ToSlash(manifestRel),
			Message:  readErr.Error() + ". The loader skips this plugin",
		})
		finaliseValidateResult(res)
		return
	}
	if bytes.HasPrefix(raw, utf8BOM) {
		rep.Findings = append(rep.Findings, validateFinding{
			Severity: validateError,
			File:     filepath.ToSlash(manifestRel),
			Message:  "starts with a UTF-8 byte order mark, which the loader can't parse. Save the file without one. The loader skips this plugin",
		})
		finaliseValidateResult(res)
		return
	}
	var generic map[string]json.RawMessage
	if err := json.Unmarshal(raw, &generic); err != nil {
		rep.Findings = append(rep.Findings, validateFinding{
			Severity: validateError,
			File:     filepath.ToSlash(manifestRel),
			Message:  jsonPosMessage(raw, err) + ". The loader skips this plugin",
		})
		finaliseValidateResult(res)
		return
	}
	header, headerErr := parseValidateManifestHeader(raw, generic)
	if headerErr != nil {
		rep.Findings = append(rep.Findings, validateFinding{
			Severity: validateError,
			File:     filepath.ToSlash(manifestRel),
			Message:  headerErr.Error() + ". The loader skips this plugin",
		})
		finaliseValidateResult(res)
		return
	}
	if header.name == "" {
		// A manifest with no name is named after its folder — but the
		// name is still missing, and the reference says so.
		res.HeaderName = filepath.Base(filepath.Clean(dir))
	} else {
		res.HeaderName = header.name
	}
	res.ManifestOK = true

	if header.schema != "" && strings.HasPrefix(header.schema, agentPluginsSchemaPrefix) {
		version := strings.TrimPrefix(header.schema, agentPluginsSchemaPrefix)
		version = strings.TrimSuffix(version, "/plugin.schema.json")
		if version != agentPluginsVersion {
			rep.Findings = append(rep.Findings, validateFinding{
				Severity: validateError,
				File:     filepath.ToSlash(manifestRel),
				Message:  fmt.Sprintf("unsupported Agent Plugins version: plugin.json $schema is %q. The loader skips this plugin", header.schema),
			})
			res.ManifestOK = false
			res.HeaderName = ""
			finaliseValidateResult(res)
			return
		}
		rep.AgentPluginsSpec = true
	}
	rep.Name = res.HeaderName

	validateManifestUnknownFields(generic, manifestRel, rep)
	validateManifestCategory(generic, manifestRel, rep)
	validateManifestName(header.name, manifestRel, dir, rep, rep.AgentPluginsSpec)
	validateManifestExtensions(generic, manifestRel, rep)
	if rep.AgentPluginsSpec {
		rep.Findings = append(rep.Findings, validateFinding{
			Severity: validateInfo,
			File:     filepath.ToSlash(manifestRel),
			Message:  fmt.Sprintf("declares the Agent Plugins %s $schema: it loads as an Agent Plugins plugin while the enable-agent-plugins-spec flag is on, and as a native plugin otherwise", agentPluginsVersion),
		})
	}
	validatePluginLogo(dir, manifestRel, header.logo, header.logoPresent, rep)
}

// validateManifestHeaderFields is the manifest's header: the fields the
// loader reads before anything else.
type validateManifestHeaderFields struct {
	name        string
	schema      string
	version     string
	logo        string
	logoPresent bool
}

// parseValidateManifestHeader reads the header fields out of the raw
// manifest.
//
// The logo is read as a raw value so that a non-string one can be reported
// the way the reference does:
//
//	✗ plugin.json: (line 1:51): invalid value for string field logo: 5. The loader skips this plugin
//
// 【一手】 agy 1.4.9, probed against the binary.
func parseValidateManifestHeader(raw []byte, generic map[string]json.RawMessage) (validateManifestHeaderFields, error) {
	var out validateManifestHeaderFields
	var manifest struct {
		Name    string `json:"name"`
		Schema  string `json:"$schema"`
		Version string `json:"version"`
	}
	_ = json.Unmarshal(raw, &manifest)
	out.name = manifest.Name
	out.schema = manifest.Schema
	out.version = manifest.Version

	logoRaw, ok := generic["logo"]
	if !ok {
		return out, nil
	}
	trimmed := strings.TrimSpace(string(logoRaw))
	if trimmed == "null" {
		// A null logo is a missing logo: the reference notes the
		// generic icon rather than rejecting the plugin.
		return out, nil
	}
	var logo string
	if err := json.Unmarshal(logoRaw, &logo); err != nil {
		off := validateJSONFieldOffset(raw, "logo")
		line, col := jsonPosition(raw, off)
		return out, fmt.Errorf("(line %d:%d): invalid value for string field logo: %s",
			line, col, validateJSONValueText(logoRaw))
	}
	out.logo = logo
	out.logoPresent = true
	return out, nil
}

// validateJSONFieldOffset returns the 1-based byte offset of a top-level
// field's value in raw, or 0 when the field is not there.
func validateJSONFieldOffset(raw []byte, field string) int {
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil || tok != json.Delim('{') {
		return 0
	}
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return 0
		}
		key, _ := keyTok.(string)
		after := int(dec.InputOffset())
		var val json.RawMessage
		if err := dec.Decode(&val); err != nil {
			return 0
		}
		if key != field {
			continue
		}
		i := after
		for i < len(raw) && isJSONSpace(raw[i]) {
			i++
		}
		if i < len(raw) && raw[i] == ':' {
			i++
		}
		for i < len(raw) && isJSONSpace(raw[i]) {
			i++
		}
		return i + 1
	}
	return 0
}

// isJSONSpace reports whether b is JSON whitespace.
func isJSONSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

// validateJSONValueText renders a JSON value the way the reference's own
// error does: the raw text for a scalar, and the opening bracket for an
// array or an object.
func validateJSONValueText(raw []byte) string {
	s := strings.TrimSpace(string(raw))
	if s == "" {
		return s
	}
	if s[0] == '{' || s[0] == '[' {
		return s[:1]
	}
	return s
}

// validateManifestUnknownFields warns on unknown plugin.json fields.
func validateManifestUnknownFields(generic map[string]json.RawMessage, manifestRel string, rep *validateReport) {
	unknown := make([]string, 0, len(generic))
	for k := range generic {
		if !knownPluginManifestFields[k] {
			unknown = append(unknown, k)
		}
	}
	sort.Strings(unknown)
	for _, k := range unknown {
		msg := fmt.Sprintf("unknown field %q is ignored", k)
		if suggestion := validateFieldSuggestion(k); suggestion != "" {
			msg += fmt.Sprintf(" (did you mean %q?)", suggestion)
		}
		rep.Findings = append(rep.Findings, validateFinding{
			Severity: validateWarning,
			File:     filepath.ToSlash(manifestRel),
			Message:  msg,
		})
	}
}

// validateManifestCategory checks plugin.json's "category" field.
//
// AGY 1.5.3: "Improved `agy plugin validate` so the `category` field in
// `plugin.json` is accepted instead of reported as an unknown field, with
// a warning only when it is not a non-empty string."
func validateManifestCategory(generic map[string]json.RawMessage, manifestRel string, rep *validateReport) {
	raw, ok := generic["category"]
	if !ok {
		return
	}
	var category string
	if err := json.Unmarshal(raw, &category); err == nil && strings.TrimSpace(category) != "" {
		return
	}
	rep.Findings = append(rep.Findings, validateFinding{
		Severity: validateWarning,
		File:     filepath.ToSlash(manifestRel),
		Message:  `"category" must be a non-empty string; it is ignored`,
	})
}

// validateManifestExtensions checks the manifest's "extensions" object.
//
// The extensions carry the plugin's context providers, so a manifest whose
// "extensions" is not an object loads none of them.
//
// 【一手】 agy 1.4.9, probed against the binary:
//
//	⚠ plugin.json: "extensions" isn't an object, so no context providers load from it
//
// A null "extensions" is the same as an absent one.
func validateManifestExtensions(generic map[string]json.RawMessage, manifestRel string, rep *validateReport) {
	raw, ok := generic["extensions"]
	if !ok {
		return
	}
	if strings.TrimSpace(string(raw)) == "null" {
		return
	}
	var extensions map[string]json.RawMessage
	if err := json.Unmarshal(raw, &extensions); err != nil {
		rep.Findings = append(rep.Findings, validateFinding{
			Severity: validateWarning,
			File:     filepath.ToSlash(manifestRel),
			Message:  `"extensions" isn't an object, so no context providers load from it`,
		})
		return
	}

	// AGY 1.5.2: In extensions["com.google.antigravity"], unknown fields are reported as
	// warnings that the field was ignored, rather than causing validation failure, and
	// validation proceeds with the rest of the extension.
	if agyRaw, hasAgy := extensions["com.google.antigravity"]; hasAgy {
		var agyFields map[string]json.RawMessage
		if err := json.Unmarshal(agyRaw, &agyFields); err == nil && agyFields != nil {
			knownAgyFields := map[string]bool{
				"skills":           true,
				"subagents":        true,
				"tools":            true,
				"commands":         true,
				"mcp_servers":      true,
				"mcpServers":       true,
				"contextProviders": true,
				"hooks":            true,
			}
			for k := range agyFields {
				if !knownAgyFields[k] {
					rep.Findings = append(rep.Findings, validateFinding{
						Severity: validateWarning,
						File:     filepath.ToSlash(manifestRel),
						Message:  fmt.Sprintf(`unknown field %q under extensions["com.google.antigravity"] is ignored`, k),
					})
				}
			}
		}
	}
}

// validateManifestName performs naming format validations on manifest.Name.
//
// 【一手】 agy 1.4.9, probed against the binary. A name that is not
// lowercase kebab-case gets one warning that also says what the name
// cannot do and what to use instead:
//
//	⚠ plugin.json: name "Neo4j" isn't strict kebab-case (lowercase letters, digits and single hyphens), so it can't be published to the Antigravity Marketplace; try "neo4j"
//	⚠ plugin.json: name "foo.bar" isn't strict kebab-case (lowercase letters, digits and single hyphens), so `plugin install` refuses it and it can't be published to the Antigravity Marketplace; try "foo-bar"
//	⚠ plugin.json: name "foo_bar" isn't strict kebab-case (lowercase letters, digits and single hyphens), so it can't be published to the Antigravity Marketplace; try "foo-bar"
//
// Under the Agent Plugins spec the same warning names the spec's own
// grammar and adds the spec clause:
//
//	⚠ plugin.json: name "foo bar" isn't strict kebab-case (at most 64 lowercase letters, digits and single hyphens), so `plugin install` refuses it, it isn't valid under the Agent Plugins spec and it can't be published to the Antigravity Marketplace; try "foo-bar"
//
// A name that *is* kebab-case but breaks the spec's grammar keeps the
// spec's own older message:
//
//	⚠ plugin.json: name "aaaa…" isn't valid under the Agent Plugins spec: use 1-64 lowercase letters, digits, . and -, starting and ending with a letter or digit, with no ".." or "--"
func validateManifestName(manifestName, manifestRel, dir string, rep *validateReport, spec bool) {
	manifestFile := filepath.ToSlash(manifestRel)
	if manifestName == "" {
		rep.Findings = append(rep.Findings, validateFinding{
			Severity: validateWarning,
			File:     manifestFile,
			Message:  fmt.Sprintf("no name: the plugin is named after its folder, %q, and `plugin install` refuses it", filepath.Base(dir)),
		})
		return
	}
	if strictKebabCase(manifestName) {
		// The name is publishable; only the spec's own grammar can still
		// reject it.
		if spec && !validAgentPluginsName(manifestName) {
			rep.Findings = append(rep.Findings, validateFinding{
				Severity: validateWarning,
				File:     manifestFile,
				Message:  fmt.Sprintf("name %q isn't valid under the Agent Plugins spec: use 1-64 lowercase letters, digits, . and -, starting and ending with a letter or digit, with no \"..\" or \"--\"", manifestName),
			})
		}
		return
	}
	clauses := make([]string, 0, 3)
	if pluginInstallRefusesName(manifestName) {
		clauses = append(clauses, "`plugin install` refuses it")
	}
	if spec && !validAgentPluginsName(manifestName) {
		clauses = append(clauses, "it isn't valid under the Agent Plugins spec")
	}
	clauses = append(clauses, "it can't be published to the Antigravity Marketplace")
	grammar := "lowercase letters, digits and single hyphens"
	if spec {
		grammar = "at most 64 lowercase letters, digits and single hyphens"
	}
	msg := fmt.Sprintf("name %q isn't strict kebab-case (%s), so %s",
		manifestName, grammar, joinValidateClauses(clauses))
	if suggestion := kebabCaseSuggestion(manifestName); suggestion != "" {
		msg += fmt.Sprintf("; try %q", suggestion)
	}
	rep.Findings = append(rep.Findings, validateFinding{
		Severity: validateWarning,
		File:     manifestFile,
		Message:  msg,
	})
}

// joinValidateClauses joins the clauses of the kebab-case warning: the last
// one is joined with " and ", the others with ", ".
func joinValidateClauses(clauses []string) string {
	if len(clauses) == 0 {
		return ""
	}
	if len(clauses) == 1 {
		return clauses[0]
	}
	return strings.Join(clauses[:len(clauses)-1], ", ") + " and " + clauses[len(clauses)-1]
}

// strictKebabCase reports whether name is lowercase kebab-case: lowercase
// letters, digits and single hyphens.
func strictKebabCase(name string) bool {
	if name == "" {
		return false
	}
	for _, part := range strings.Split(name, "-") {
		if part == "" {
			return false
		}
		for _, r := range part {
			if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9') {
				return false
			}
		}
	}
	return true
}

// pluginInstallRefusesName reports whether `plugin install` refuses the
// name: the loader's own name grammar allows letters, digits, "-" and "_".
func pluginInstallRefusesName(name string) bool {
	return strings.ContainsFunc(name, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_')
	})
}

// kebabCaseSuggestion turns a name into the lowercase kebab-case name the
// Marketplace can publish: uppercase letters are lowercased, every other
// character becomes a hyphen, runs of hyphens collapse into one, and the
// leading and trailing hyphens are dropped.
//
//	Neo4j    → neo4j
//	foo_bar  → foo-bar
//	foo.bar  → foo-bar
//	a--b--c  → a-b-c
//	Ünïcode  → n-code
func kebabCaseSuggestion(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + ('a' - 'A'))
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := b.String()
	for strings.Contains(out, "--") {
		out = strings.ReplaceAll(out, "--", "-")
	}
	return strings.Trim(out, "-")
}

// checkMisplacedHiddenComponents warns if components sit inside hidden dirs.
func checkMisplacedHiddenComponents(dir, manifestRel string, rep *validateReport) {
	first := strings.Split(manifestRel, "/")[0]
	if first != ".antigravity-plugin" && first != ".gemini-plugin" {
		return
	}
	for _, comp := range []string{"skills", "agents", "rules", "workflows", "hooks.json", "mcp_config.json"} {
		if _, err := os.Stat(filepath.Join(dir, first, comp)); err == nil {
			rep.Findings = append(rep.Findings, validateFinding{
				Severity: validateWarning,
				File:     first + "/" + comp,
				Message:  fmt.Sprintf("never loaded: components are read from the plugin root, not %s. Move it next to %s", first, first),
			})
		}
	}
}

// validateExclusions reads <component>.json and returns its exclusion set.
func validateExclusions(dir, filename string, rep *validateReport) map[string]bool {
	raw, err := os.ReadFile(filepath.Join(dir, filename))
	if err != nil {
		return nil
	}
	if bytes.HasPrefix(raw, utf8BOM) {
		rep.Findings = append(rep.Findings, validateFinding{
			Severity: validateError,
			File:     filename,
			Message:  "starts with a UTF-8 byte order mark, which the loader can't parse. Save the file without one. The loader ignores this file",
		})
		return nil
	}
	var cfg validateJSONConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		msg := jsonPosMessage(raw, err)
		if _, syntax := err.(*json.SyntaxError); !syntax {
			msg = strings.ReplaceAll(err.Error(), "cli.validateJSONConfig", "customizations.JSONConfig")
			msg = strings.ReplaceAll(msg, "validateJSONConfig", "JSONConfig")
		}
		rep.Findings = append(rep.Findings, validateFinding{
			Severity: validateError,
			File:     filename,
			Message:  msg + ". The loader ignores this file",
		})
		return nil
	}
	if len(cfg.Exclude) == 0 {
		return nil
	}
	set := make(map[string]bool, len(cfg.Exclude))
	for _, name := range cfg.Exclude {
		set[name] = true
	}
	return set
}

// validateFieldSuggestion returns the known manifest field closest to name.
func validateFieldSuggestion(name string) string {
	lower := strings.ToLower(name)
	fields := make([]string, 0, len(knownPluginManifestFields))
	for k := range knownPluginManifestFields {
		fields = append(fields, k)
	}
	sort.Strings(fields)
	best, bestDist := "", 3
	for _, k := range fields {
		d := levenshtein(lower, strings.ToLower(k))
		if d < bestDist {
			best, bestDist = k, d
		}
	}
	return best
}

// levenshtein is the plain edit distance (insert/delete/substitute) over runes.
func levenshtein(a, b string) int {
	ar, br := []rune(a), []rune(b)
	prev := make([]int, len(br)+1)
	cur := make([]int, len(br)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ar); i++ {
		cur[0] = i
		for j := 1; j <= len(br); j++ {
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(br)]
}

// validAgentPluginsName implements the Agent Plugins name grammar.
func validAgentPluginsName(name string) bool {
	if len(name) == 0 || len(name) > 64 || strings.Contains(name, "..") || strings.Contains(name, "--") {
		return false
	}
	for i, r := range name {
		ok := r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.' || r == '-'
		if !ok {
			return false
		}
		if i == 0 || i == len(name)-1 {
			if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9') {
				return false
			}
		}
	}
	return true
}

// jsonPosMessage renders a JSON parse error the way the reference does.
func jsonPosMessage(raw []byte, err error) string {
	if err.Error() == "unexpected end of JSON input" {
		line, col := jsonPosition(raw, len(raw)+1)
		return fmt.Sprintf("line %d, column %d: %s: unexpected EOF", line, col, jsonEOFContext(raw))
	}
	se, ok := err.(*json.SyntaxError)
	if !ok {
		return err.Error()
	}
	off := int(se.Offset)
	if off > len(raw) {
		off = len(raw)
	}
	line, col := jsonPosition(raw, off)
	start := off
	if start > 0 {
		start--
	}
	for start < len(raw) && !isJSONIdentByte(raw[start]) {
		start++
	}
	end := start
	for end < len(raw) && isJSONIdentByte(raw[end]) {
		end++
	}
	if start < end {
		return fmt.Sprintf("line %d, column %d: invalid literal: %s", line, col, string(raw[start:end]))
	}
	return fmt.Sprintf("line %d, column %d: %s", line, col, err.Error())
}

// jsonPosition converts a 1-based byte offset to line/column.
func jsonPosition(raw []byte, off int) (line, col int) {
	line, col = 1, 1
	for i := 0; i < off-1 && i < len(raw); i++ {
		if raw[i] == '\n' {
			line++
			col = 1
		} else {
			col++
		}
	}
	return line, col
}

type jsonEOFFrame struct {
	kind       byte
	afterValue bool
}

// jsonEOFContext walks the document far enough to name what the reference
// parser was reading when the input ended.
func jsonEOFContext(raw []byte) string {
	var stack []jsonEOFFrame
	i := 0
	for i < len(raw) {
		switch c := raw[i]; c {
		case ' ', '\t', '\n', '\r':
			i++
		case '"':
			var closed bool
			i, closed = scanJSONString(raw, i+1)
			if !closed {
				return "parsing string"
			}
			setTopAfterValue(stack, true)
		case '{', '[':
			stack = append(stack, jsonEOFFrame{kind: c})
			i++
		case '}', ']':
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			setTopAfterValue(stack, true)
			i++
		case ':', ',':
			setTopAfterValue(stack, false)
			i++
		default:
			i = scanJSONToken(raw, i)
			setTopAfterValue(stack, true)
		}
	}
	return topEOFContext(stack)
}

func setTopAfterValue(stack []jsonEOFFrame, val bool) {
	if len(stack) > 0 {
		stack[len(stack)-1].afterValue = val
	}
}

func topEOFContext(stack []jsonEOFFrame) string {
	if len(stack) == 0 {
		return "parsing value"
	}
	top := stack[len(stack)-1]
	if top.afterValue {
		if top.kind == '{' {
			return "parsing object after value"
		}
		return "parsing array after value"
	}
	return "parsing value"
}

func scanJSONString(raw []byte, i int) (int, bool) {
	for i < len(raw) {
		if raw[i] == '\\' {
			i += 2
			continue
		}
		if raw[i] == '"' {
			return i + 1, true
		}
		i++
	}
	return i, false
}

func scanJSONToken(raw []byte, i int) int {
	for i < len(raw) && !strings.ContainsRune(" \t\n\r,]}:", rune(raw[i])) {
		i++
	}
	return i
}

func isJSONIdentByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '_' || b == '.' || b == '-'
}
