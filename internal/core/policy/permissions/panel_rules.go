// panel_rules.go — cycle 205: permission panel rule reading, writing,
// validation, and tab styling.
package permissions

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/palette"
)

const (
	PermScopeProject = iota
	PermScopeShared
	PermScopeGlobal
)

// PermScopeNames are the three scope rows transcribed from the stage-1 capture.
var PermScopeNames = [3]string{"Project", "Shared with Pigasus", "Global"}

// PermListNames / PermListKeys are the three permission lists in tab order.
var PermListNames = [3]string{"allowlist", "denylist", "asklist"}
var PermListKeys = [3]string{"allow", "deny", "ask"}

// ReadSettingsPermList reads permissions.<key> from settings.json.
func ReadSettingsPermList(tab int) []string {
	v, err := auth.SettingsField("permissions." + PermListKeys[tab])
	if err != nil || v == nil {
		return nil
	}
	return AnyStringSlice(v)
}

// AnyStringSlice coerces a decoded JSON array into []string.
func AnyStringSlice(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, x := range arr {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// ProjectConfigPath is the reference's default CLI project file.
func ProjectConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".gemini", "config", "projects", "default-cli-project.json")
}

// ReadProjectPermList reads rules from the default CLI project config file.
func ReadProjectPermList(tab int) []string {
	p := ProjectConfigPath()
	if p == "" {
		return nil
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	var m map[string]any
	if json.Unmarshal(data, &m) != nil {
		return nil
	}
	pg, _ := m["permissionGrants"].(map[string]any)
	if pg == nil {
		return nil
	}
	inner, _ := pg["permissionGrants"].(map[string]any)
	if inner == nil {
		return nil
	}
	return AnyStringSlice(inner[PermListKeys[tab]])
}

// WriteProjectPermList writes rules to the default CLI project config file.
func WriteProjectPermList(tab int, list []string) {
	p := ProjectConfigPath()
	if p == "" {
		return
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return
	}
	var m map[string]any
	if json.Unmarshal(data, &m) != nil {
		return
	}
	pg, _ := m["permissionGrants"].(map[string]any)
	if pg == nil {
		pg = map[string]any{}
		m["permissionGrants"] = pg
	}
	inner, _ := pg["permissionGrants"].(map[string]any)
	if inner == nil {
		inner = map[string]any{}
		pg["permissionGrants"] = inner
	}
	vals := make([]any, len(list))
	for i, s := range list {
		vals[i] = s
	}
	inner[PermListKeys[tab]] = vals
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(p, out, 0o644)
}

// ValidateGrant reproduces the reference's two rejection messages. Only
// `command` is a verified accepted action; `read` and `foo` were both
// probed and rejected (probe_read.py).
func ValidateGrant(rule string) string {
	open := strings.IndexByte(rule, '(')
	if open <= 0 || !strings.HasSuffix(rule, ")") {
		return fmt.Sprintf("invalid grant string: %q", rule)
	}
	action := rule[:open]
	for _, r := range action {
		if !(r >= 'a' && r <= 'z') && r != '_' {
			return fmt.Sprintf("invalid grant string: %q", rule)
		}
	}
	if !ValidGrantAction(action) {
		return fmt.Sprintf("unknown action %q in grant string: %q", action, rule)
	}
	return ""
}

// ValidGrantAction reports whether an action verb in a grant string is recognized.
func ValidGrantAction(a string) bool { return a == "command" }

// PermTabRow renders the allowlist / denylist / asklist tab strip.
func PermTabRow(active int, counts [3]int) string {
	var b strings.Builder
	b.WriteString(" ")
	for i := 0; i < 3; i++ {
		if i > 0 {
			b.WriteString("    ")
		}
		name := PermListNames[i]
		cnt := fmt.Sprintf("(%d)", counts[i])
		if i == active {
			b.WriteString("\x1b[1;4m\x1b[38;5;69m" + name +
				"\x1b[0;4m\x1b[38;5;69m " + "\x1b[1m" + cnt + "\x1b[0m")
		} else {
			b.WriteString(palette.SeparatorOpen + name + " " + cnt + "\x1b[39m")
		}
	}
	b.WriteString(" " + palette.SeparatorOpen + "  (←/→ to switch)\x1b[39m")
	return b.String()
}
