package pluginimport

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// stripBOM strips the UTF-8 BOM if present.
func stripBOM(data []byte) []byte {
	if bytes.HasPrefix(data, []byte("\xef\xbb\xbf")) {
		return data[3:]
	}
	return data
}

// firstLine extracts the first non-empty, non-header line as fallback description.
func firstLine(body []byte) string {
	lines := strings.Split(string(body), "\n")
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		l = strings.TrimLeft(l, "#* \t")
		l = strings.TrimSpace(l)
		if l != "" {
			return l
		}
	}
	return ""
}

// splitFrontmatter splits YAML frontmatter from markdown body.
func splitFrontmatter(data []byte) ([]byte, []byte) {
	data = stripBOM(data)
	trimmed := bytes.TrimLeft(data, " \t\r\n")
	if !bytes.HasPrefix(trimmed, []byte("---")) {
		return nil, data
	}
	rest := trimmed[3:]
	idx := bytes.Index(rest, []byte("\n---"))
	if idx < 0 {
		idx = bytes.Index(rest, []byte("\r\n---"))
		if idx < 0 {
			return nil, data
		}
		end := idx + 5
		return rest[:idx], bytes.TrimLeft(rest[end:], " \t\r\n")
	}
	end := idx + 4
	return rest[:idx], bytes.TrimLeft(rest[end:], " \t\r\n")
}

type commandFrontmatter struct {
	Name                   string `yaml:"name,omitempty"`
	Description            string `yaml:"description,omitempty"`
	DisableModelInvocation bool   `yaml:"disable-model-invocation,omitempty"`
	ArgumentHint           string `yaml:"argument-hint,omitempty"`
}

// parseCommandFrontmatter extracts metadata from frontmatter or falls back to body.
func parseCommandFrontmatter(fm, body []byte, defaultName string) (description string, disableModel bool, argHint string) {
	var meta commandFrontmatter
	if len(fm) > 0 {
		err := yaml.Unmarshal(fm, &meta)
		if err != nil {
			// Claude Code frontmatter often contains non-standard YAML such as:
			// argument-hint: [base branch] [focus]
			// The reference parses line-by-line when YAML unmarshal fails.
			lines := strings.Split(string(fm), "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "description:") {
					meta.Description = strings.TrimSpace(strings.TrimPrefix(line, "description:"))
				} else if strings.HasPrefix(line, "argument-hint:") {
					meta.ArgumentHint = strings.TrimSpace(strings.TrimPrefix(line, "argument-hint:"))
				} else if strings.HasPrefix(line, "disable-model-invocation:") {
					val := strings.TrimSpace(strings.TrimPrefix(line, "disable-model-invocation:"))
					meta.DisableModelInvocation = strings.EqualFold(val, "true") || val == "1"
				}
			}
		}
	}
	description = strings.TrimSpace(meta.Description)
	if description == "" {
		description = firstLine(body)
	}
	if description == "" {
		description = defaultName
	}
	return description, meta.DisableModelInvocation, strings.TrimSpace(meta.ArgumentHint)
}

// commandSkill converts a Claude command file into an Antigravity SKILL.md.
func commandSkill(name string, raw []byte) []byte {
	fm, body := splitFrontmatter(raw)
	desc, disableModel, argHint := parseCommandFrontmatter(fm, body, name)

	outMeta := map[string]any{
		"name":        name,
		"description": desc,
	}
	if disableModel {
		outMeta["disable-model-invocation"] = true
	}
	if argHint != "" {
		outMeta["argument-hint"] = argHint
	}

	metaBytes, err := yaml.Marshal(outMeta)
	if err != nil {
		metaBytes = []byte(fmt.Sprintf("name: %s\ndescription: %s\n", name, desc))
	}

	var buf bytes.Buffer
	buf.WriteString("---\n")
	buf.Write(metaBytes)
	buf.WriteString("---\n\n")
	buf.Write(bytes.TrimSpace(body))
	buf.WriteString("\n")
	return buf.Bytes()
}

// convertCommands turns <plugin>/commands/<name>.md into the skill
// <plugin>/skills/<name>/SKILL.md and removes the leftover commands folder.
func convertCommands(dest, plugin string) ([]string, error) {
	dir := filepath.Join(dest, "commands")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".md")
		body, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		skillData := commandSkill(name, body)
		skillDir := filepath.Join(dest, "skills", name)
		if err := os.MkdirAll(skillDir, 0700); err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), skillData, 0600); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	// agy 1.5.5 removes the leftover commands/ folder so agy plugin validate doesn't warn.
	_ = os.RemoveAll(dir)
	sort.Strings(names)
	return names, nil
}

// ClaudeToAntigravityTools maps Claude Code tool names to Antigravity tools.
var ClaudeToAntigravityTools = map[string]string{
	"Bash":         "run_command",
	"Read":         "view_file",
	"Write":        "write_to_file",
	"Edit":         "replace_file_content",
	"MultiEdit":    "multi_replace_file_content",
	"Grep":         "grep_search",
	"Glob":         "find_by_name",
	"LS":           "list_dir",
	"WebFetch":     "read_url_content",
	"WebSearch":    "search_web",
	"Task":         "invoke_subagent",
	"NotebookEdit": "notebook_edit",
}

// TranslateToolMatcher translates a Claude tool matcher into Antigravity tool names.
func TranslateToolMatcher(matcher string) (string, []string) {
	matcher = strings.TrimSpace(matcher)
	if matcher == "" || matcher == "*" {
		return "*", nil
	}
	parts := strings.FieldsFunc(matcher, func(r rune) bool {
		return r == '|' || r == ','
	})
	var translated []string
	var warns []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if p == "*" {
			translated = append(translated, "*")
			continue
		}
		if mapped, ok := ClaudeToAntigravityTools[p]; ok {
			translated = append(translated, mapped)
		} else {
			warns = append(warns, fmt.Sprintf("tool %q has no Antigravity equivalent and was skipped", p))
		}
	}
	if len(translated) == 0 {
		return "", warns
	}
	return strings.Join(translated, "|"), warns
}

// ClaudeHookEntry represents one hook handler in Claude hooks.json.
type ClaudeHookEntry struct {
	Matcher string `json:"matcher,omitempty"`
	Command string `json:"command,omitempty"`
	Timeout int    `json:"timeout,omitempty"`
	Type    string `json:"type,omitempty"`
	Hooks   []struct {
		Type    string `json:"type,omitempty"`
		Command string `json:"command,omitempty"`
		Timeout int    `json:"timeout,omitempty"`
	} `json:"hooks,omitempty"`
}

// stageClaudeHooks translates Claude hooks in hooks/hooks.json to Antigravity format.
func stageClaudeHooks(dest string) ([]string, error) {
	hooksPath := filepath.Join(dest, "hooks", "hooks.json")
	data, err := os.ReadFile(hooksPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var rawDoc map[string]json.RawMessage
	if err := json.Unmarshal(data, &rawDoc); err != nil {
		return []string{fmt.Sprintf("parse hooks.json: %v", err)}, nil
	}

	// Claude hooks can be top-level event maps or nested under "hooks".
	eventsMap := rawDoc
	if hooksVal, ok := rawDoc["hooks"]; ok {
		var nested map[string]json.RawMessage
		if err := json.Unmarshal(hooksVal, &nested); err == nil && len(nested) > 0 {
			eventsMap = nested
		}
	}

	converted := make(map[string][]map[string]any)
	var warns []string

	supportedEvents := map[string]bool{
		"PreToolUse":   true,
		"PostToolUse":  true,
		"SessionStart": true,
		"Stop":         true,
	}

	for evName, evRaw := range eventsMap {
		if !supportedEvents[evName] {
			warns = append(warns, fmt.Sprintf("hook event %q has no Antigravity equivalent and was skipped", evName))
			continue
		}

		var entries []ClaudeHookEntry
		if err := json.Unmarshal(evRaw, &entries); err != nil {
			// Try single entry
			var single ClaudeHookEntry
			if err := json.Unmarshal(evRaw, &single); err == nil {
				entries = []ClaudeHookEntry{single}
			} else {
				warns = append(warns, fmt.Sprintf("hook event %q failed to parse: %v", evName, err))
				continue
			}
		}

		var agyEntries []map[string]any
		for _, e := range entries {
			var matcher string
			if evName == "PreToolUse" || evName == "PostToolUse" {
				var trWarns []string
				matcher, trWarns = TranslateToolMatcher(e.Matcher)
				warns = append(warns, trWarns...)
				if matcher == "" && e.Matcher != "" {
					continue
				}
			}

			// Gather handlers
			var handlers []map[string]any
			if e.Command != "" {
				to := e.Timeout
				if to <= 0 {
					to = 60
				}
				typ := e.Type
				if typ == "" {
					typ = "command"
				}
				handlers = append(handlers, map[string]any{
					"type":    typ,
					"command": e.Command,
					"timeout": to,
				})
			}
			for _, h := range e.Hooks {
				if h.Command == "" {
					continue
				}
				to := h.Timeout
				if to <= 0 {
					to = 60
				}
				typ := h.Type
				if typ == "" {
					typ = "command"
				}
				handlers = append(handlers, map[string]any{
					"type":    typ,
					"command": h.Command,
					"timeout": to,
				})
			}

			if len(handlers) == 0 {
				continue
			}

			item := map[string]any{
				"hooks": handlers,
			}
			if matcher != "" {
				item["matcher"] = matcher
			}
			agyEntries = append(agyEntries, item)
		}

		if len(agyEntries) > 0 {
			converted[evName] = agyEntries
		}
	}

	if len(converted) == 0 {
		_ = os.Remove(hooksPath)
		return warns, nil
	}

	finalDoc := map[string]any{
		"hooks": converted,
	}
	outBytes, err := json.MarshalIndent(finalDoc, "", "  ")
	if err != nil {
		return warns, err
	}
	if err := os.WriteFile(hooksPath, append(outBytes, '\n'), 0600); err != nil {
		return warns, err
	}

	return warns, nil
}
