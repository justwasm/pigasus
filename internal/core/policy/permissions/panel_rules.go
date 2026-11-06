// panel_rules.go — cycle 205: permission panel rule reading, writing,
// validation, and tab styling.
package permissions

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	projectflags "github.com/justwasm/pigasus/internal/core/cliflags/projects"
	settings "github.com/justwasm/pigasus/internal/core/settings"
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
	v, err := settings.Field("permissions." + PermListKeys[tab])
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
//
// It is the file of the project a session runs in when no project flag
// names one — the default project — and the id and the path shape belong
// to the package that owns them.
func ProjectConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return projectflags.ConfigPath(home, projectflags.DefaultProjectID)
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

// ValidateGrant reproduces the reference's rejection messages.
//
// The reference's own grammar is `action(target)` — the binary's format
// regex names the six live actions plus the retired `unsandboxed`, and the
// panels' hint reads "Format: action(target)". `unsandboxed` is rejected
// with the reference's own message; every other unknown action with the
// "unknown action" one.
func ValidateGrant(rule string) string {
	_, _, err := ParseGrantString(rule)
	if err != nil {
		return err.Error()
	}
	return ""
}

// ValidGrantAction reports whether an action verb in a grant string is
// recognized.
//
// The six live actions are the reference's own; the answer comes from the
// grant-action mapping, so the vocabulary has one owner. `unsandboxed` is
// *not* one of them any more — the reference rejects every rule that uses
// it (see UnsandboxedRejectedMessage).
func ValidGrantAction(a string) bool {
	_, ok := grantActionTools[a]
	return ok
}

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
