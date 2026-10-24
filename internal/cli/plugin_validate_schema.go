package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

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
	validateManifestHeader(raw, dir, manifestRel, res)
	if !res.ManifestOK {
		return
	}
	validateManifestUnknownFields(generic, manifestRel, rep)
}

// validateManifestHeader unmarshals manifest header fields and validates $schema.
func validateManifestHeader(raw []byte, dir, manifestRel string, res *validateResult) {
	rep := res.Report
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
			res.HeaderName = ""
			finaliseValidateResult(res)
			return
		}
		rep.AgentPluginsSpec = true
		rep.Findings = append(rep.Findings, validateFinding{
			Severity: validateInfo,
			File:     filepath.ToSlash(manifestRel),
			Message:  fmt.Sprintf("declares the Agent Plugins %s $schema: it loads as an Agent Plugins plugin while the enable-agent-plugins-spec flag is on, and as a native plugin otherwise", agentPluginsVersion),
		})
	}
	rep.Name = res.HeaderName
	validateManifestName(manifest.Name, manifestRel, dir, rep)
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

// validateManifestName performs naming format validations on manifest.Name.
func validateManifestName(manifestName, manifestRel, dir string, rep *validateReport) {
	manifestFile := filepath.ToSlash(manifestRel)
	switch {
	case manifestName == "":
		rep.Findings = append(rep.Findings, validateFinding{
			Severity: validateWarning,
			File:     manifestFile,
			Message:  fmt.Sprintf("no name: the plugin is named after its folder, %q, and `plugin install` refuses it", filepath.Base(dir)),
		})
	case rep.AgentPluginsSpec:
		if !validAgentPluginsName(manifestName) {
			rep.Findings = append(rep.Findings, validateFinding{
				Severity: validateWarning,
				File:     manifestFile,
				Message:  fmt.Sprintf("name %q isn't valid under the Agent Plugins spec: use 1-64 lowercase letters, digits, . and -, starting and ending with a letter or digit, with no \"..\" or \"--\"", manifestName),
			})
		}
	default:
		if strings.ContainsFunc(manifestName, func(r rune) bool {
			return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_')
		}) {
			rep.Findings = append(rep.Findings, validateFinding{
				Severity: validateWarning,
				File:     manifestFile,
				Message:  fmt.Sprintf("name %q has characters other than letters, digits, - and _, so `plugin install` refuses it", manifestName),
			})
		}
	}
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
