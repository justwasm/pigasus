// settings.go reads the permission rules out of the reference's
// settings.json.
//
// The reference stores them under `permissions` — the recovered string table
// carries `permissions.allow`, `PermissionsConfig`, `PermissionGrantsConfig`,
// `denied_tools` and `denied_command_pattern`, and describes a rule as
// matching "the file or everything under the directory. Paths must be literal
// and absolute."
//
// The exact JSON encoding is not in the decompiled set, so this reads the two
// shapes the description implies and ignores anything else:
//
//	{"permissions": {"allow": ["run_command:go test*", "view_file"], "deny": [...]}}
//	{"permissions": {"allow": [{"tool": "view_file", "pattern": "src/"}]}}
//
// A bare string "tool:pattern" splits on the first colon. A string with no
// colon is a workspace-relative pattern that applies to any tool. Unknown
// entries are skipped rather than failing the whole file, so a settings.json
// written for the real CLI stays loadable.
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
			Deny  []json.RawMessage `json:"deny"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return Settings{}, err
	}
	return Settings{
		Allow: parseRules(doc.Permissions.Allow),
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
		if rule := parseRuleString(s); rule.Tool != "" || rule.Pattern != "" {
			return rule, true
		}
		return Rule{}, false
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

// parseRuleString splits "tool:pattern" on the first colon. A leading "!" is
// accepted as a deny marker and skipped, so a single list can hold both.
func parseRuleString(s string) Rule {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "!")
	if i := strings.IndexByte(s, ':'); i > 0 {
		return Rule{Tool: s[:i], Pattern: s[i+1:]}
	}
	return Rule{Pattern: s}
}
