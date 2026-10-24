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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
