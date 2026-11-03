// settings.go reads the permission rules out of the reference's
// settings.json.
//
// The reference stores them under `permissions` — the recovered string table
// carries `permissions.allow`, `PermissionsConfig`, `PermissionGrantsConfig`,
// `denied_tools` and `denied_command_pattern` — and the official
// documentation names the three lists:
//
//	{"permissions": {"allow": ["command(git status)", "read_url(github.com)"],
//	                 "ask":   ["command(git push)"],
//	                 "deny":  ["write_file(.env)"]}}
//
// Two rule grammars are read:
//
//   - the reference's *grant* grammar, `action(target)`. The documentation
//     says "Each rule is an action(target) string, such as command(git
//     status) or read_url(github.com), with no space before the opening
//     parenthesis", and the binary's own format regex is
//     `^(command|read_file|write_file|read_url|mcp|execute_url|unsandboxed)\s*\(.*\)$`
//     【一手】 agy 1.4.11.
//
//   - the *legacy* forms this clone shipped before the documentation was
//     read: the `tool:pattern` string and the `{"tool": …, "pattern": …}`
//     object. They keep working — a settings.json already on disk must not
//     lose its rules.
//
// A bare string with neither a colon nor a parenthesis is a
// workspace-relative pattern that applies to any tool. Unknown entries are
// skipped rather than failing the whole file, so a settings.json written for
// the real CLI stays loadable.
package permissions

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"strings"
)

// Settings is the subset of settings.json this clone understands.
type Settings struct {
	Allow []Rule
	Ask   []Rule
	Deny  []Rule
}

// LoadRules reads the permission rules from a settings.json path. A missing
// file is not an error: it yields an empty Settings.
func LoadRules(path string) (Settings, error) {
	if path == "" {
		return Settings{}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Settings{}, nil
		}
		return Settings{}, err
	}
	var doc struct {
		Permissions struct {
			Allow []json.RawMessage `json:"allow"`
			Ask   []json.RawMessage `json:"ask"`
			Deny  []json.RawMessage `json:"deny"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return Settings{}, err
	}
	return Settings{
		Allow: parseRules(doc.Permissions.Allow),
		Ask:   parseRules(doc.Permissions.Ask),
		Deny:  parseRules(doc.Permissions.Deny),
	}, nil
}

// parseRules decodes one rule list, skipping entries it cannot understand.
func parseRules(raw []json.RawMessage) []Rule {
	var out []Rule
	for _, r := range raw {
		if rule, ok := parseRule(r); ok {
			out = append(out, rule)
		}
	}
	return out
}

func parseRule(raw json.RawMessage) (Rule, bool) {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		// `null` unmarshals into a string as ""; treat that as no rule.
		return parseRuleString(s)
	}
	var obj struct {
		Tool    string `json:"tool"`
		Pattern string `json:"pattern"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil && (obj.Tool != "" || obj.Pattern != "") {
		return Rule{Tool: obj.Tool, Pattern: obj.Pattern}, true
	}
	return Rule{}, false
}

// parseRuleString parses one rule string.
//
// Three shapes are accepted:
//
//   - the grant grammar, `action(target)` — the reference's own;
//   - the legacy `tool:pattern` — the clone's own, kept for the files that
//     already carry it;
//   - a bare pattern, which applies to any tool.
//
// A leading "!" is accepted as a deny marker and skipped, so a single list
// can hold both.
func parseRuleString(s string) (Rule, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "!")
	open := strings.IndexByte(s, '(')
	colon := strings.IndexByte(s, ':')
	// The legacy form wins when its colon comes first: a legacy pattern may
	// itself hold parentheses ("run_command:echo (hi)").
	if colon > 0 && (open < 0 || colon < open) {
		return Rule{Tool: s[:colon], Pattern: s[colon+1:]}, true
	}
	if open > 0 {
		// The rule has the grant shape — or an attempt at it. An action
		// the reference does not know is not a rule at all: the reference
		// "ignores rules it can't parse", and falling back to the
		// bare-pattern form would turn "unsandboxed(git)" into a
		// tool-agnostic prefix that covers something the user never
		// wrote.
		action := strings.TrimSpace(s[:open])
		if !ValidGrantAction(action) || !strings.HasSuffix(s, ")") {
			return Rule{}, false
		}
		return Rule{Action: action, Pattern: s[open+1 : len(s)-1]}, true
	}
	if s == "" {
		return Rule{}, false
	}
	return Rule{Pattern: s}, true
}
