package auth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/tidwall/gjson"
	"github.com/tidwall/pretty"
	"github.com/tidwall/sjson"
)

// Settings is the v1.3.5 binary 1:1 schema for
// ~/.gemini/antigravity-cli/settings.json. The 2 fields are
// verified against the v1.3.5 binary's actual file content
// (cycle 42 close-gap 实证).
type Settings struct {
	ColorScheme       string   `json:"colorScheme"`       // "dark" / "light" / "solarized light" / etc
	TrustedWorkspaces []string `json:"trustedWorkspaces"` // cwd paths
}

var jsonPrettyOpts = pretty.Options{Indent: "  ", SortKeys: true}

// ValidSettingsKeys is the set of all recognized keys in settings.json.
var ValidSettingsKeys = map[string]bool{
	"agentMode":               true,
	"allowNonWorkspaceAccess": true,
	"altScreenMode":           true,
	"artifactReviewPolicy":    true,
	"clearScrollbackOnResize": true,
	"colorScheme":             true,
	"contextWindow":           true,
	"copyOnSelect":            true,
	"customModelsConfig":      true,
	"disableSlashCommands":    true,
	"editor":                  true,
	"editorMode":              true,
	"enableTerminalSandbox":   true,
	"gcp":                     true,
	"hideActiveItems":         true,
	"historySize":             true,
	"model":                   true,
	"modelProvider":           true,
	"notifications":           true,
	"permissions":             true,
	"pickerGrouping":          true,
	"queuedMessages":          true,
	"runningLightSpeed":       true,
	"sandboxProxy":            true,
	"seasonalThemesOff":       true,
	"seasonalThemesOptIn":     true,
	"showFeedbackSurvey":      true,
	"showTips":                true,
	"statusLine":              true,
	"title":                   true,
	"toolPermission":          true,
	"trustedWorkspaces":       true,
	"useG1Credits":            true,
	"useTerminalCursor":       true,
	"verbosity":               true,
	"vimInsertFirst":          true,
}

var (
	sessionSettingsMu      sync.RWMutex
	sessionSettingsOverlay map[string]any
)

// ParseAndValidateSettingsFlag parses and validates the --settings flag argument.
// If the argument starts with '{', it is treated as inline JSON; otherwise it is read as a file path.
// Returns the parsed settings overlay map or an error matching the reference binary.
func ParseAndValidateSettingsFlag(raw string) (map[string]any, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var data []byte
	if strings.HasPrefix(raw, "{") {
		data = []byte(raw)
	} else {
		var err error
		data, err = os.ReadFile(raw)
		if err != nil {
			return nil, fmt.Errorf("reading --settings file: %w", err)
		}
	}
	var overlay map[string]any
	if err := json.Unmarshal(data, &overlay); err != nil {
		return nil, fmt.Errorf("--settings must be a JSON object of settings.json keys: %w", err)
	}

	keys := make([]string, 0, len(overlay))
	for k := range overlay {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if !ValidSettingsKeys[k] {
			return nil, fmt.Errorf("--settings: unknown setting %q", k)
		}
	}
	return overlay, nil
}

// SetSessionSettingsOverlay sets the transient in-memory overlay for --settings.
// These settings override settings.json for the active session and are never persisted to disk.
func SetSessionSettingsOverlay(overlay map[string]any) {
	sessionSettingsMu.Lock()
	defer sessionSettingsMu.Unlock()
	sessionSettingsOverlay = overlay
}

// ClearSessionSettingsOverlay clears the transient in-memory overlay.
func ClearSessionSettingsOverlay() {
	sessionSettingsMu.Lock()
	defer sessionSettingsMu.Unlock()
	sessionSettingsOverlay = nil
}

func formatSettingsJSON(data []byte) []byte {
	return bytes.TrimSpace(pretty.PrettyOptions(data, &jsonPrettyOpts))
}

func readSettingsBytes() ([]byte, error) {
	path := filepath.Join(AntigravityConfigHome(), "settings.json")
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if err != nil && os.IsNotExist(err) {
		data = []byte("{}")
	} else {
		data = stripBOM(data)
	}

	sessionSettingsMu.RLock()
	overlay := sessionSettingsOverlay
	sessionSettingsMu.RUnlock()

	if len(overlay) == 0 {
		if err != nil {
			return nil, err
		}
		return data, nil
	}

	var base map[string]any
	if len(data) > 0 && gjson.ValidBytes(data) {
		_ = json.Unmarshal(data, &base)
	}
	if base == nil {
		base = make(map[string]any)
	}
	for k, v := range overlay {
		base[k] = v
	}
	merged, mErr := json.Marshal(base)
	if mErr != nil {
		return data, nil
	}
	return merged, nil
}

func modifySettings(createIfMissing bool, mutate func(data []byte) ([]byte, error)) error {
	dir := AntigravityConfigHome()
	if createIfMissing {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
	}
	path := filepath.Join(dir, "settings.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if createIfMissing && os.IsNotExist(err) {
			data = []byte("{}")
		} else {
			return err
		}
	} else {
		data = stripBOM(data)
		if !gjson.ValidBytes(data) {
			if createIfMissing {
				data = []byte("{}")
			} else {
				var m map[string]any
				return json.Unmarshal(data, &m)
			}
		}
	}
	data, err = mutate(data)
	if err != nil {
		return err
	}
	out := formatSettingsJSON(data)
	return os.WriteFile(path, out, 0600)
}

// SaveSettingsColorScheme updates the colorScheme field in
// ~/.gemini/antigravity-cli/settings.json, preserving all other
// fields (trustedWorkspaces, artifactReviewPolicy, etc.). The
// v1.3.5 binary writes this when the user picks a scheme in the
// wizard.
//
// When settings.json does not exist yet, this is a no-op (the
// cycle 48 WriteCompatDefaults will write the default on first
// run).
func SaveSettingsColorScheme(name string) error {
	return modifySettings(false, func(data []byte) ([]byte, error) {
		return sjson.SetBytes(data, "colorScheme", name)
	})
}

// SetSettingsString writes one string value into
// ~/.gemini/antigravity-cli/settings.json, preserving every other field.
func SetSettingsString(key, value string) error {
	return modifySettings(false, func(data []byte) ([]byte, error) {
		return sjson.SetBytes(data, key, value)
	})
}

// SetSettingsBool writes one boolean value into settings.json, preserving
// every other field. It is the persistence half of the TUI /statusline and
// /title toggles.
//
// Cycle 184: the toggles used to persist the strings "on"/"off". That was a
// guess (【推测】 in cycle 105) and it is wrong: agy 1.4.0 types both keys as
// booleans and refuses to start at all on a string —
//
//	⚠ Settings Error
//	  └ invalid settings: statusLine: invalid value "off"; title: invalid value "on"
//
// so a single `/statusline off` in the clone bricked the reference binary
// until the key was deleted by hand. Verified by writing each candidate into
// a live settings.json and starting agy: true/false are accepted, every
// string form ("on"/"off"/"auto"/"always"/"never") is rejected.
func SetSettingsBool(key string, value bool) error {
	return modifySettings(false, func(data []byte) ([]byte, error) {
		return sjson.SetBytes(data, key, value)
	})
}

// SettingsBool returns one boolean value from settings.json. ok is false when
// the file, the key or the value is absent.
//
// A string value is accepted too: it is the pre-cycle-184 shape this package
// used to write, and treating "on"/"off" as true/false lets a settings.json
// the clone already corrupted keep working instead of silently reverting the
// toggle to its default.
func SettingsBool(key string) (value, ok bool) {
	data, err := readSettingsBytes()
	if err != nil {
		return false, false
	}
	res := gjson.GetBytes(data, key)
	if !res.Exists() {
		return false, false
	}
	switch res.Type {
	case gjson.True:
		return true, true
	case gjson.False:
		return false, true
	case gjson.String:
		switch res.Str {
		case "on", "true":
			return true, true
		case "off", "false":
			return false, true
		}
	}
	return false, false
}

// SettingsString returns one string value from settings.json, or "" when the
// file, the key or the value is absent. It seeds the TUI's /statusline and
// /title toggles from disk.
func SettingsString(key string) string {
	data, err := readSettingsBytes()
	if err != nil {
		return ""
	}
	res := gjson.GetBytes(data, key)
	if res.Type == gjson.String {
		return res.Str
	}
	return ""
}

// SettingsField returns one raw value from settings.json. Dot-notation
// keys walk nested objects (e.g. "statusLine.enabled" reads the boolean
// nested under the `statusLine` object). The returned `any` is the JSON
// shape: string, bool, []any, map[string]any, etc. Returns (nil, nil)
// when the file or the key is absent.
//
// Cycle 196: the /config panel reads live values through this helper so
// pressing ←/→ in the picker reflects the user's persisted state on the
// next render. Print-mode /config (print_slash.go) still goes through
// its own typed mappers — SettingsField is the TUI-only counterpart.
func SettingsField(key string) (any, error) {
	data, err := readSettingsBytes()
	if err != nil {
		return nil, err
	}
	if !gjson.ValidBytes(data) {
		var m map[string]any
		return nil, json.Unmarshal(data, &m)
	}
	res := gjson.GetBytes(data, key)
	if !res.Exists() {
		return nil, nil
	}
	return res.Value(), nil
}

// SetSettingsField writes one raw value into settings.json at the dot
// path. Nested objects are created as needed. Bool values are persisted
// as JSON `true`/`false`; strings as JSON strings. Other types pass
// through JSON marshalling.
//
// Cycle 196: companion to SettingsField — /config ←/→ calls this after
// determining the next value in the cycle list. Pre-cycle-196 the panel
// had no live persistence; settings.json only updated via the /statusline
// and /title slash-command toggles.
func SetSettingsField(key string, value any) error {
	return modifySettings(false, func(data []byte) ([]byte, error) {
		return sjson.SetBytes(data, key, value)
	})
}

// DeleteSettingsField removes the value at the dot path from settings.json.
// The reference represents a setting at its default value by the *absence*
// of the key (probed: cycling /config's Agent Mode back to "default"
// deletes `agentMode`; Copy on Select's "on" is absent while "off" is
// JSON false). Cycle 197.
//
// A missing path is not an error — deleting an already-absent key is a
// no-op. Parent objects left empty are kept, matching the reference which
// never prunes `statusLine`/`title` shells.
func DeleteSettingsField(key string) error {
	return modifySettings(true, func(data []byte) ([]byte, error) {
		return sjson.DeleteBytes(data, key)
	})
}

// updateSettings reads settings.json, lets mutate change it, and rewrites it.
//
// The rewrite reproduces the reference's formatting: 2-space indent, HTML
// escaping of <, > and & (Go's json.Marshal default), and keys in alphabetical
// order — which is what marshalling a map produces, and what the live file
// shows.
func updateSettings(mutate func(map[string]any)) error {
	return modifySettings(false, func(data []byte) ([]byte, error) {
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, err
		}
		mutate(m)
		return json.Marshal(m)
	})
}

// updateOrCreateSettings is the cycle-125 sibling of updateSettings.
// When settings.json is missing, it creates the parent dir (0700) and
// writes a fresh settings.json containing only the keys the mutator
// chooses to set — instead of returning ENOENT. Used by the trust
// gate's accept handler so a first-launch user accepting the dialog
// persists the cwd before WriteCompatDefaults has had a chance to
// run. When the file does exist, the behaviour is identical to
// updateSettings (preserves unknown fields, alphabetises keys via
// map marshalling).
func updateOrCreateSettings(mutate func(map[string]any)) error {
	return modifySettings(true, func(data []byte) ([]byte, error) {
		var m map[string]any
		_ = json.Unmarshal(data, &m)
		if m == nil {
			m = map[string]any{}
		}
		mutate(m)
		return json.Marshal(m)
	})
}

// ResolveSettingsModel returns the model specified in settings.json,
// or fallback if settings.json does not exist or has no model.
func ResolveSettingsModel(fallback string) string {
	data, err := readSettingsBytes()
	if err != nil {
		return fallback
	}
	res := gjson.GetBytes(data, "model")
	if res.Type == gjson.String && strings.TrimSpace(res.Str) != "" {
		return strings.TrimSpace(res.Str)
	}
	return fallback
}
