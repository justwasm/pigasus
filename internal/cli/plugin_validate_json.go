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
package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

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
	"logo":         true,
	"skills":       true,
	"$schema":      true,
}

// reservedAgentNames are the built-in agent names the reference refuses.
// Probed across ~900 candidates: only these two triggered
// `the name %q is reserved for a built-in agent, so this agent never loads`.
// The full built-in set is not recoverable from strings alone, so this list is
// evidence-backed rather than exhaustive; see the cycle report's 存疑 section.
var reservedAgentNames = map[string]bool{
	"deepagent":    true,
	"review-agent": true,
}

// hookEvents are the event names the reference recognises in hooks.json.
const hookEvents = "SessionStart, PreInvocation, PostInvocation, Stop, PreToolUse, PostToolUse"

// pluginValidateBodyLimit is the 24 KB limit the reference applies to skill
// bodies and rule content. It is 24000 bytes, not 24576.
const pluginValidateBodyLimit = 24000

// agentPluginsSchemaPrefix is the `$schema` prefix that selects the Agent
// Plugins loader. Only version 1.0.0 is supported by agy 1.4.3.
const agentPluginsSchemaPrefix = "https://agent-plugins.org/schemas/"
const agentPluginsVersion = "1.0.0"

// runPluginValidateReference is the entry point for `agy plugin validate`.
//
// Reference surface (probed):
//
//	agy plugin validate [path] [--strict] [--json]
//
// Any other argument is rejected with
// `Error: unknown validate argument "<arg>": usage is validate [path] [--strict] [--json]`
// on stderr and exit 1 — including `--help`.
func runPluginValidateReference(j *JetskiCLI, args []string) error {
	var strict, asJSON bool
	var positional []string
	for _, a := range args {
		switch a {
		case "--strict":
			strict = true
			continue
		case "--json":
			asJSON = true
			continue
		}
		if strings.HasPrefix(a, "-") || len(positional) > 0 {
			return fail(j, 1, fmt.Sprintf("Error: unknown validate argument %q: usage is validate [path] [--strict] [--json]", a))
		}
		positional = append(positional, a)
	}
	path := "."
	if len(positional) == 1 {
		path = positional[0]
	}

	res := buildValidateResult(path)
	res.Strict = strict
	res.ColorOutput = isTerminalWriter(j.Out)

	if asJSON {
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

// buildValidateResult resolves the path, parses the manifest, walks the
// components, and returns the report plus the header/footer state.
func buildValidateResult(inputPath string) *validateResult {
	res := &validateResult{}
	input := inputPath
	info, statErr := os.Stat(input)
	if statErr != nil {
		// The reference echoes the path exactly as typed, relative or not.
		res.Report = &validateReport{
			Dir:      input,
			Findings: []validateFinding{{Severity: validateError, File: ".", Message: input + " is not a directory"}},
		}
		res.HeaderPath = input
		res.ErrorCount = 1
		return res
	}

	dir := input
	manifestRel := ""
	if !info.IsDir() {
		// A direct plugin.json keeps its parent path as typed, except that a
		// bare filename (parent ".") is canonicalised to the absolute CWD.
		dir = filepath.Dir(input)
		if dir == "." {
			if abs, err := filepath.Abs("."); err == nil {
				dir = abs
			}
		}
		manifestRel = filepath.Base(input)
	} else {
		// The manifest filename is case-insensitive (a plugin may ship
		// `Plugin.json` or `PLUGIN.JSON`); the two hidden directory names are
		// not.
		for _, sub := range []string{"", ".antigravity-plugin", ".gemini-plugin"} {
			if name := findManifestIn(filepath.Join(dir, sub)); name != "" {
				manifestRel = filepath.Join(sub, name)
				break
			}
		}
	}
	if manifestRel == "" {
		res.Report = &validateReport{
			Dir: dir,
			Findings: []validateFinding{{
				Severity: validateError,
				File:     "plugin.json",
				Message:  "no plugin.json found at the plugin root or in .antigravity-plugin or .gemini-plugin",
			}},
		}
		res.HeaderPath = dir
		res.ErrorCount = 1
		return res
	}

	rep := &validateReport{
		Dir:      dir,
		Manifest: filepath.ToSlash(manifestRel),
		Findings: []validateFinding{},
	}
	res.Report = rep
	res.HeaderPath = dir

	raw, readErr := os.ReadFile(filepath.Join(dir, manifestRel))
	if readErr != nil {
		rep.Findings = append(rep.Findings, validateFinding{
			Severity: validateError,
			File:     filepath.ToSlash(manifestRel),
			Message:  readErr.Error() + ". The loader skips this plugin",
		})
		finaliseValidateResult(res)
		return res
	}

	if bytes.HasPrefix(raw, utf8BOM) {
		rep.Findings = append(rep.Findings, validateFinding{
			Severity: validateError,
			File:     filepath.ToSlash(manifestRel),
			Message:  "starts with a UTF-8 byte order mark, which the loader can't parse. Save the file without one. The loader skips this plugin",
		})
		finaliseValidateResult(res)
		return res
	}

	var generic map[string]json.RawMessage
	if err := json.Unmarshal(raw, &generic); err != nil {
		rep.Findings = append(rep.Findings, validateFinding{
			Severity: validateError,
			File:     filepath.ToSlash(manifestRel),
			Message:  jsonPosMessage(raw, err) + ". The loader skips this plugin",
		})
		finaliseValidateResult(res)
		return res
	}

	// Name: manifest field, else the folder basename.
	var manifest struct {
		Name    string `json:"name"`
		Schema  string `json:"$schema"`
		Version string `json:"version"`
	}
	_ = json.Unmarshal(raw, &manifest)
	name := manifest.Name
	if name == "" {
		name = filepath.Base(filepath.Clean(dir))
	}
	res.HeaderName = name
	res.ManifestOK = true

	// Agent Plugins $schema: a supported version switches the loader and adds
	// an info finding; an unsupported one is a hard error and suppresses the
	// component summary.
	if manifest.Schema != "" && strings.HasPrefix(manifest.Schema, agentPluginsSchemaPrefix) {
		version := strings.TrimPrefix(manifest.Schema, agentPluginsSchemaPrefix)
		version = strings.TrimSuffix(version, "/plugin.schema.json")
		if version != agentPluginsVersion {
			rep.Findings = append(rep.Findings, validateFinding{
				Severity: validateError,
				File:     filepath.ToSlash(manifestRel),
				Message:  fmt.Sprintf("unsupported Agent Plugins version: plugin.json $schema is %q. The loader skips this plugin", manifest.Schema),
			})
			res.ManifestOK = false
			name = ""
			finaliseValidateResult(res)
			return res
		}
		rep.AgentPluginsSpec = true
		rep.Findings = append(rep.Findings, validateFinding{
			Severity: validateInfo,
			File:     filepath.ToSlash(manifestRel),
			Message:  fmt.Sprintf("declares the Agent Plugins %s $schema: it loads as an Agent Plugins plugin while the enable-agent-plugins-spec flag is on, and as a native plugin otherwise", agentPluginsVersion),
		})
	}

	rep.Name = name

	// Unknown manifest fields, sorted for deterministic output.
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

	// Manifest name checks. The native and Agent Plugins rules differ: the
	// native loader allows uppercase and `_`; the Agent Plugins spec allows
	// only lowercase letters/digits/`.`/`-`.
	manifestFile := filepath.ToSlash(manifestRel)
	switch {
	case manifest.Name == "":
		rep.Findings = append(rep.Findings, validateFinding{
			Severity: validateWarning,
			File:     manifestFile,
			Message:  fmt.Sprintf("no name: the plugin is named after its folder, %q, and `plugin install` refuses it", filepath.Base(dir)),
		})
	case rep.AgentPluginsSpec:
		if !validAgentPluginsName(manifest.Name) {
			rep.Findings = append(rep.Findings, validateFinding{
				Severity: validateWarning,
				File:     manifestFile,
				Message:  fmt.Sprintf("name %q isn't valid under the Agent Plugins spec: use 1-64 lowercase letters, digits, . and -, starting and ending with a letter or digit, with no \"..\" or \"--\"", manifest.Name),
			})
		}
	default:
		if strings.ContainsFunc(manifest.Name, func(r rune) bool {
			return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_')
		}) {
			rep.Findings = append(rep.Findings, validateFinding{
				Severity: validateWarning,
				File:     manifestFile,
				Message:  fmt.Sprintf("name %q has characters other than letters, digits, - and _, so `plugin install` refuses it", manifest.Name),
			})
		}
	}

	// Components that sit next to a manifest inside .antigravity-plugin/ or
	// .gemini-plugin/ are not where the loader looks (it reads them from the
	// plugin root). The reference repeats the hidden directory in the "move
	// it next to" clause; reproduce that verbatim.
	if first := strings.Split(rep.Manifest, "/")[0]; first == ".antigravity-plugin" || first == ".gemini-plugin" {
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

	// Components.
	scanValidateSkills(dir, rep)
	scanValidateAgents(dir, rep)
	scanValidateRules(dir, rep)
	scanValidateHooks(dir, rep)
	scanValidateMCPServers(dir, rep)
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

	finaliseValidateResult(res)
	return res
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

// utf8BOM is the byte-order mark some Windows editors prepend. The plugin
// manifest, hooks and agent.json reject it; skills, rules (via the generic
// frontmatter path) and mcp_config.json differ as documented at each call site.
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// validateJSONConfig is the shared shape of skills.json / agents.json /
// rules.json: a list of component names to exclude. The reference's type is
// named customizations.JSONConfig; the error-text rewrite below mirrors that.
type validateJSONConfig struct {
	Exclude []string `json:"exclude"`
}

// validateExclusions reads <component>.json and returns its exclusion set.
// A BOM or malformed file produces one finding but excludes nothing, matching
// the reference ("The loader ignores this file" leaves every component in).
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

// validateFieldSuggestion returns the known manifest field closest to name,
// case-insensitively, when it is within two edits. The reference suggests for
// typos such as `auther`, `nme`, `NAME`, and `versoin`, but stays quiet for
// distant names like `unknown_thing`.
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

// levenshtein is the plain edit distance (insert/delete/substitute) over
// runes.
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

// scanValidateSkills walks `skills/<folder>/SKILL.md` (one level, like the
// reference loader). Nested skills and loose markdown files are ignored.
// Entries excluded by skills.json still get parse/name diagnostics but are
// neither counted nor body-checked, and the loader registers the included
// skills first (which decides which side of a duplicate warning they land on).
func scanValidateSkills(dir string, rep *validateReport) {
	excluded := validateExclusions(dir, "skills.json", rep)
	entries, err := os.ReadDir(filepath.Join(dir, "skills"))
	if err != nil {
		return
	}
	type skillFile struct {
		folder   string
		rel      string
		raw      []byte
		excluded bool
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
	seen := map[string]string{}
	for _, pass := range []bool{false, true} {
		for _, f := range files {
			if f.excluded != pass {
				continue
			}
			// Skills tolerate a BOM; the frontmatter parser strips it.
			fm, body, fmErr, ok := parseFrontmatter(bytes.TrimPrefix(f.raw, utf8BOM))
			base := f.folder
			if !ok {
				rep.Findings = append(rep.Findings, validateFinding{
					Severity: validateError,
					File:     f.rel,
					Message:  "no frontmatter: the file must start with a --- block holding name and description. The loader skips this skill",
				})
				continue
			}
			var fields struct {
				Name        string `yaml:"name"`
				Description string `yaml:"description"`
			}
			if fmErr != nil {
				rep.Findings = append(rep.Findings, validateFinding{
					Severity: validateError,
					File:     f.rel,
					Message:  "the frontmatter isn't valid YAML: failed to parse frontmatter: " + fmErr.Error() + ". The loader skips this skill",
				})
				continue
			}
			if err := yaml.Unmarshal(fm, &fields); err != nil {
				rep.Findings = append(rep.Findings, validateFinding{
					Severity: validateError,
					File:     f.rel,
					Message:  "the frontmatter isn't valid YAML: failed to parse frontmatter: " + err.Error() + ". The loader skips this skill",
				})
				continue
			}
			effective := fields.Name
			if effective == "" {
				effective = "SKILL"
				if !f.excluded {
					rep.Findings = append(rep.Findings, validateFinding{
						Severity: validateWarning,
						File:     f.rel,
						Message:  fmt.Sprintf("no name in the frontmatter: the skill is named %q after its file. Add name: %s", effective, base),
					})
				}
			}
			if fields.Description == "" && !f.excluded {
				rep.Findings = append(rep.Findings, validateFinding{
					Severity: validateWarning,
					File:     f.rel,
					Message:  "no description: the model uses it to decide when to load the skill",
				})
			}
			prev, dup := seen[effective]
			if dup {
				rep.Findings = append(rep.Findings, validateFinding{
					Severity: validateWarning,
					File:     f.rel,
					Message:  fmt.Sprintf("skipped: %s already defines a skill named %q", prev, effective),
				})
			}
			// An excluded skill is normally not loaded, so its semantic
			// warnings stay quiet — but the duplicate path still reports the
			// folder mismatch.
			if fields.Name != "" && fields.Name != base && (!f.excluded || dup) {
				rep.Findings = append(rep.Findings, validateFinding{
					Severity: validateWarning,
					File:     f.rel,
					Message:  fmt.Sprintf("name %q doesn't match its folder %q", fields.Name, base),
				})
			}
			if dup {
				continue
			}
			seen[effective] = f.rel
			if f.excluded {
				continue
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
	}
}

// scanValidateAgents walks flat `agents/*.md` plus `agents/<name>/agent.json`.
// agents.json excludes match the Markdown file stem; excluded agents are still
// validated but not counted, and are registered after the included ones.
func scanValidateAgents(dir string, rep *validateReport) {
	excluded := validateExclusions(dir, "agents.json", rep)
	entries, err := os.ReadDir(filepath.Join(dir, "agents"))
	if err != nil {
		return
	}
	type agentFile struct {
		rel      string
		raw      []byte
		excluded bool
	}
	var files []agentFile
	for _, e := range entries {
		full := filepath.Join(dir, "agents", e.Name())
		if e.IsDir() {
			rel := filepath.ToSlash(filepath.Join("agents", e.Name(), "agent.json"))
			raw, err := os.ReadFile(filepath.Join(full, "agent.json"))
			if err != nil {
				continue
			}
			if bytes.HasPrefix(raw, utf8BOM) {
				rep.Findings = append(rep.Findings, validateFinding{
					Severity: validateError,
					File:     rel,
					Message:  "starts with a UTF-8 byte order mark, which the loader can't parse. Save the file without one. The loader skips this agent",
				})
				continue
			}
			var m map[string]any
			if err := json.Unmarshal(raw, &m); err != nil {
				rep.Findings = append(rep.Findings, validateFinding{
					Severity: validateError,
					File:     rel,
					Message:  jsonPosMessage(raw, err) + ". The loader skips this agent",
				})
				continue
			}
			rep.Findings = append(rep.Findings, validateFinding{
				Severity: validateWarning,
				File:     rel,
				Message:  "never loaded by this build: only internal builds read agent.json; write the agent as agent.md",
			})
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
	seen := map[string]string{}
	for _, pass := range []bool{false, true} {
		for _, f := range files {
			if f.excluded != pass {
				continue
			}
			fm, _, fmErr, ok := parseFrontmatter(f.raw)
			if !ok {
				rep.Findings = append(rep.Findings, validateFinding{
					Severity: validateWarning,
					File:     f.rel,
					Message:  "never loaded: an agent file starts with frontmatter that has a name and description",
				})
				continue
			}
			if fmErr != nil {
				rep.Findings = append(rep.Findings, validateFinding{
					Severity: validateError,
					File:     f.rel,
					Message:  "the frontmatter isn't valid YAML: failed to unmarshal frontmatter YAML: " + fmErr.Error() + ". The loader skips this agent",
				})
				continue
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
				continue
			}
			if fields.Name == "" {
				rep.Findings = append(rep.Findings, validateFinding{
					Severity: validateError,
					File:     f.rel,
					Message:  "the frontmatter has no name. The loader skips this agent",
				})
				continue
			}
			if fields.Description == "" {
				rep.Findings = append(rep.Findings, validateFinding{
					Severity: validateError,
					File:     f.rel,
					Message:  "the frontmatter has no description. The loader skips this agent",
				})
				continue
			}
			if reservedAgentNames[fields.Name] {
				rep.Findings = append(rep.Findings, validateFinding{
					Severity: validateError,
					File:     f.rel,
					Message:  fmt.Sprintf("the name %q is reserved for a built-in agent, so this agent never loads", fields.Name),
				})
				continue
			}
			if prev, dup := seen[fields.Name]; dup {
				rep.Findings = append(rep.Findings, validateFinding{
					Severity: validateWarning,
					File:     f.rel,
					Message:  fmt.Sprintf("skipped: %s already defines an agent named %q", prev, fields.Name),
				})
				continue
			}
			seen[fields.Name] = f.rel
			if f.excluded {
				continue
			}
			rep.Components.Agents++
		}
	}
}

// scanValidateRules walks flat `rules/*.md`. A README without frontmatter is
// the documented exception: it is skipped silently rather than erroring.
// rules.json excludes match the Markdown file stem; excluded rules are still
// validated but neither counted nor body-checked.
func scanValidateRules(dir string, rep *validateReport) {
	excluded := validateExclusions(dir, "rules.json", rep)
	entries, err := os.ReadDir(filepath.Join(dir, "rules"))
	if err != nil {
		return
	}
	type ruleFile struct {
		name     string
		rel      string
		raw      []byte
		excluded bool
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
	for _, pass := range []bool{false, true} {
		for _, f := range files {
			if f.excluded != pass {
				continue
			}
			fm, body, fmErr, ok := parseFrontmatter(f.raw)
			if !ok {
				if f.name == "README.md" || f.excluded {
					continue
				}
				rep.Findings = append(rep.Findings, validateFinding{
					Severity: validateError,
					File:     f.rel,
					Message:  "no valid trigger in the frontmatter, so the rule never applies. Set trigger to always_on, model_decision, glob, manual",
				})
				continue
			}
			if fmErr != nil {
				rep.Findings = append(rep.Findings, validateFinding{
					Severity: validateError,
					File:     f.rel,
					Message:  "the frontmatter isn't valid YAML: " + fmErr.Error() + ". The loader skips this rule",
				})
				continue
			}
			// Typed struct so a YAML sequence in `globs` fails exactly like the
			// loader's `cannot unmarshal !!seq into string`.
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
				continue
			}
			// Excluded rules are not loaded: only a YAML parse failure is
			// reported, never the trigger/globs/description warnings.
			if f.excluded {
				continue
			}
			switch fields.Trigger {
			case "always_on", "model_decision", "glob", "manual":
			default:
				rep.Findings = append(rep.Findings, validateFinding{
					Severity: validateError,
					File:     f.rel,
					Message:  "no valid trigger in the frontmatter, so the rule never applies. Set trigger to always_on, model_decision, glob, manual",
				})
				continue
			}
			if fields.Trigger == "glob" && fields.Globs == "" {
				rep.Findings = append(rep.Findings, validateFinding{
					Severity: validateWarning,
					File:     f.rel,
					Message:  "a glob rule needs globs, or it never applies",
				})
			}
			if fields.Trigger == "model_decision" && fields.Description == "" {
				rep.Findings = append(rep.Findings, validateFinding{
					Severity: validateWarning,
					File:     f.rel,
					Message:  "a model_decision rule needs a description: the model uses it to decide when to apply the rule",
				})
			}
			if f.excluded {
				continue
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
	}
}

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
			}
		}
		rep.Findings = append(rep.Findings, validateFinding{
			Severity: validateInfo,
			File:     "hooks.json",
			Message:  `"hooks" wraps the events, Claude Code style, so its handlers load as one hook named "hooks". Name each hook at the top level instead: {"my-hook": {"PreToolUse": [...]}}`,
		})
		// The reference counts the top-level `hooks` key as one entry.
		rep.Components.Hooks = len(top)
		return
	}
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
	// mcp_config.json tolerates a BOM (the loader strips it).
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

// parseFrontmatter splits a `---` delimited YAML block off a markdown file.
// ok is false when the file does not open with a `---` line or has no closing
// delimiter. fmErr is a YAML parse error; yaml.Unmarshal on fm can still fail
// when the block parses as YAML but not into the caller's typed struct.
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
	// The closing delimiter must be a line of its own.
	after := rest[end+4:]
	if after != "" && !strings.HasPrefix(after, "\n") {
		return nil, raw, nil, false
	}
	fm = []byte(rest[:end])
	if strings.HasPrefix(after, "\n") {
		after = after[1:]
	}
	// Surface a YAML syntax error here so callers can report it with the
	// loader-specific prefix. Unmarshal into a generic node: typed unmarshal
	// errors are reported by the caller.
	var node any
	if err := yaml.Unmarshal(fm, &node); err != nil {
		return fm, []byte(after), err, true
	}
	return fm, []byte(after), nil, true
}

// validAgentPluginsName implements the Agent Plugins name grammar: 1-64
// lowercase letters, digits, `.` and `-`, starting and ending with a letter or
// digit, with no `..` or `--`.
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

// jsonPosMessage renders a JSON parse error the way the reference does:
// `line L, column C: invalid literal: <token>`. Go's json.SyntaxError carries
// the byte offset, which is enough to reproduce the position exactly.
func jsonPosMessage(raw []byte, err error) string {
	// A truncated document gets the reference parser's context-specific
	// message ("parsing value" / "parsing object after value" / "parsing
	// array after value" / "parsing string") and a column one past the last
	// byte. encoding/json only says "unexpected end of JSON input".
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
	line, col := 1, 1
	for i := 0; i < off-1 && i < len(raw); i++ {
		if raw[i] == '\n' {
			line++
			col = 1
		} else {
			col++
		}
	}
	// The reference calls a bad bare token an "invalid literal". Extract the
	// identifier at the error offset; fall back to the decoder's message.
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

// jsonEOFContext walks the document far enough to name what the reference
// parser was reading when the input ended.
func jsonEOFContext(raw []byte) string {
	type frame struct {
		kind       byte // '{' or '['
		afterValue bool
	}
	var stack []frame
	i := 0
	for i < len(raw) {
		c := raw[i]
		switch c {
		case ' ', '\t', '\n', '\r':
			i++
		case '"':
			i++
			closed := false
			for i < len(raw) {
				if raw[i] == '\\' {
					i += 2
					continue
				}
				if raw[i] == '"' {
					i++
					closed = true
					break
				}
				i++
			}
			if !closed {
				return "parsing string"
			}
			if len(stack) > 0 {
				stack[len(stack)-1].afterValue = true
			}
		case '{':
			stack = append(stack, frame{kind: '{'})
			i++
		case '[':
			stack = append(stack, frame{kind: '['})
			i++
		case '}', ']':
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			if len(stack) > 0 {
				stack[len(stack)-1].afterValue = true
			}
			i++
		case ':', ',':
			if len(stack) > 0 {
				stack[len(stack)-1].afterValue = false
			}
			i++
		default:
			// A number, true, false or null: consume to the next delimiter.
			for i < len(raw) && !strings.ContainsRune(" \t\n\r,]}:", rune(raw[i])) {
				i++
			}
			if len(stack) > 0 {
				stack[len(stack)-1].afterValue = true
			}
		}
	}
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

func isJSONIdentByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '_' || b == '.' || b == '-'
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
