// Package settings is the store behind the reference's settings.json.
//
// The file lives at ~/.gemini/antigravity-cli/settings.json and holds the
// user's persistent choices: the colour scheme, the trusted workspaces, the
// selected model, the permission lists, the status-line toggles.
//
// It is a tier-1 package on purpose. The policies that read it — the
// permission policy (`internal/core/policy/permissions`) and the workspace
// trust policy (`internal/core/policy/trust`) — are tier-1 contracts, and
// a contract that reaches up into a capability for its state is an upward
// dependency. The store is the state; the policies are the rules over it.
//
// The store owns the file's *shape* and the reference's own writing rules:
// 2-space indent, HTML escaping of <, > and &, keys in alphabetical order,
// and a missing file is an empty object rather than an error.
package settings

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/tidwall/gjson"
	"github.com/tidwall/pretty"
	"github.com/tidwall/sjson"

	"github.com/justwasm/pigasus/internal/core/confhome"
)

// Schema is the v1.3.5 binary 1:1 schema for
// ~/.gemini/antigravity-cli/settings.json. The 2 fields are
// verified against the v1.3.5 binary's actual file content
// (cycle 42 close-gap 实证).
type Schema struct {
	ColorScheme       string   `json:"colorScheme"`       // "dark" / "light" / "solarized light" / etc
	TrustedWorkspaces []string `json:"trustedWorkspaces"` // cwd paths
}

var jsonPrettyOpts = pretty.Options{Indent: "  ", SortKeys: true}

// ValidKeys is the set of all recognized keys in settings.json.
var ValidKeys = map[string]bool{
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
	"orchestrator":            true,
	"permissions":             true,
	"persona":                 true,
	"pickerGrouping":          true,
	"productSkin":             true,
	"project4kMode":           true,
	"queuedMessages":          true,
	"runningLightSpeed":       true,
	"sandboxProxy":            true,
	"seasonalThemesOff":       true,
	"seasonalThemesOptIn":     true,
	"showFeedbackSurvey":      true,
	"showThinking":            true,
	"showTips":                true,
	"statusLine":              true,
	"title":                   true,
	"toolPermission":          true,
	"trustedWorkspaces":       true,
	"useG1Credits":            true,
	"useTerminalCursor":       true,
	"verbosity":               true,
	"vimInsertFirst":          true,
	"voice":                   true,
}

var (
	sessionSettingsMu      sync.RWMutex
	sessionSettingsOverlay map[string]any

	settingsCacheMu    sync.RWMutex
	cachedPath         string
	cachedModTime      time.Time
	cachedSize         int64
	cachedMergedBytes  []byte
	cachedOverlayGen   int64
	cachedValidChecked bool
	cachedValid        bool
	cachedFields       map[string]any
	overlayGen         int64
)

type missingFieldSentinel struct{}

var missingField = missingFieldSentinel{}

// InvalidateCache invalidates the in-memory cache for settings.json.
func InvalidateCache() {
	settingsCacheMu.Lock()
	cachedPath = ""
	cachedModTime = time.Time{}
	cachedSize = -1
	cachedMergedBytes = nil
	cachedValidChecked = false
	cachedValid = false
	cachedFields = nil
	overlayGen++
	settingsCacheMu.Unlock()
	cachedSettingsSnapshot.invalidate()
}

// Path is the settings.json file's own path.
func Path() string {
	return filepath.Join(confhome.Antigravity(), "settings.json")
}

// ParseAndValidateFlag parses and validates the --settings flag argument.
// If the argument starts with '{', it is treated as inline JSON; otherwise it is read as a file path.
// Returns the parsed settings overlay map or an error matching the reference binary.
func ParseAndValidateFlag(raw string) (map[string]any, error) {
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
	data = confhome.StripBOM(data)
	if err := json.Unmarshal(data, &overlay); err != nil {
		return nil, fmt.Errorf("--settings must be a JSON object of settings.json keys: %w", err)
	}

	keys := make([]string, 0, len(overlay))
	for k := range overlay {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if !ValidKeys[k] {
			return nil, fmt.Errorf("--settings: unknown setting %q", k)
		}
	}
	return overlay, nil
}

// SetSessionOverlay sets the transient in-memory overlay for --settings.
// These settings override settings.json for the active session and are never persisted to disk.
func SetSessionOverlay(overlay map[string]any) {
	sessionSettingsMu.Lock()
	sessionSettingsOverlay = cloneSettingsMap(overlay)
	sessionSettingsMu.Unlock()
	InvalidateCache()
}

func formatSettingsJSON(data []byte) []byte {
	return bytes.TrimSpace(pretty.PrettyOptions(data, &jsonPrettyOpts))
}

func readSettingsBytes() ([]byte, error) {
	p := Path()

	fi, statErr := os.Stat(p)

	settingsCacheMu.RLock()
	if cachedPath == p && cachedOverlayGen == overlayGen {
		if statErr == nil && fi.ModTime().Equal(cachedModTime) && fi.Size() == cachedSize && len(cachedMergedBytes) > 0 {
			b := cachedMergedBytes
			settingsCacheMu.RUnlock()
			return b, nil
		}
	}
	settingsCacheMu.RUnlock()

	settingsCacheMu.Lock()
	defer settingsCacheMu.Unlock()

	if cachedPath == p && cachedOverlayGen == overlayGen {
		if statErr == nil && fi.ModTime().Equal(cachedModTime) && fi.Size() == cachedSize && len(cachedMergedBytes) > 0 {
			return cachedMergedBytes, nil
		}
	}

	sessionSettingsMu.RLock()
	overlay := cloneSettingsMap(sessionSettingsOverlay)
	curOverlayGen := overlayGen
	sessionSettingsMu.RUnlock()

	var data []byte
	var readErr error
	if statErr != nil {
		if os.IsNotExist(statErr) {
			if len(overlay) == 0 {
				return nil, statErr
			}
			data = []byte("{}")
		} else {
			return nil, statErr
		}
	} else {
		data, readErr = os.ReadFile(p)
		if readErr != nil {
			return nil, readErr
		}
		data = confhome.StripBOM(data)
	}

	if len(overlay) == 0 {
		cachedPath = p
		cachedModTime = fi.ModTime()
		cachedSize = fi.Size()
		cachedMergedBytes = data
		cachedOverlayGen = curOverlayGen
		cachedValidChecked = false
		cachedValid = false
		cachedFields = nil
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
	merged := data
	if mBytes, mErr := json.Marshal(base); mErr == nil {
		merged = mBytes
	}

	cachedPath = p
	if statErr == nil {
		cachedModTime = fi.ModTime()
		cachedSize = fi.Size()
	} else {
		cachedModTime = time.Time{}
		cachedSize = -1
	}
	cachedMergedBytes = merged
	cachedOverlayGen = curOverlayGen
	cachedValidChecked = false
	cachedValid = false
	cachedFields = nil

	return merged, nil
}

func modifySettings(createIfMissing, resetIfCorrupt bool, mutate func(data []byte) ([]byte, error)) error {
	defer InvalidateCache()
	dir := confhome.Antigravity()
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
		data = confhome.StripBOM(data)
		if !gjson.ValidBytes(data) {
			if resetIfCorrupt {
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

// SaveColorScheme updates the colorScheme field in
// ~/.gemini/antigravity-cli/settings.json, preserving all other
// fields (trustedWorkspaces, artifactReviewPolicy, etc.). The
// v1.3.5 binary writes this when the user picks a scheme in the
// wizard.
//
// When settings.json does not exist yet, this is a no-op (the
// cycle 48 WriteCompatDefaults will write the default on first
// run).
func SaveColorScheme(name string) error {
	return modifySettings(false, false, func(data []byte) ([]byte, error) {
		return sjson.SetBytes(data, "colorScheme", name)
	})
}

// SetString writes one string value into
// ~/.gemini/antigravity-cli/settings.json, preserving every other field.
func SetString(key, value string) error {
	return modifySettings(false, false, func(data []byte) ([]byte, error) {
		return sjson.SetBytes(data, key, value)
	})
}

// SetBool writes one boolean value into settings.json, preserving
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
func SetBool(key string, value bool) error {
	return modifySettings(true, false, func(data []byte) ([]byte, error) {
		return sjson.SetBytes(data, key, value)
	})
}

// Bool returns one boolean value from settings.json. ok is false when
// the file, the key or the value is absent.
//
// A string value is accepted too: it is the pre-cycle-184 shape this package
// used to write, and treating "on"/"off" as true/false lets a settings.json
// the clone already corrupted keep working instead of silently reverting the
// toggle to its default.
func Bool(key string) (value, ok bool) {
	val, err := cachedField(key, false)
	if err != nil || val == nil {
		return false, false
	}
	switch v := val.(type) {
	case bool:
		return v, true
	case string:
		switch v {
		case "on", "true":
			return true, true
		case "off", "false":
			return false, true
		}
	}
	return false, false
}

// Has reports whether key is present in settings.json.
func Has(key string) bool {
	data, err := readSettingsBytes()
	if err != nil {
		return false
	}
	return gjson.GetBytes(data, key).Exists()
}

// String returns one string value from settings.json, or "" when the
// file, the key or the value is absent. It seeds the TUI's /statusline and
// /title toggles from disk.
func String(key string) string {
	val, err := cachedField(key, false)
	if err != nil || val == nil {
		return ""
	}
	if s, ok := val.(string); ok {
		return s
	}
	return ""
}

// Field returns one raw value from settings.json. Dot-notation
// keys walk nested objects (e.g. "statusLine.enabled" reads the boolean
// nested under the `statusLine` object). The returned `any` is the JSON
// shape: string, bool, []any, map[string]any, etc. Returns (nil, nil)
// when the file or the key is absent.
//
// Cycle 196: the /config panel reads live values through this helper so
// pressing ←/→ in the picker reflects the user's persisted state on the
// next render. Print-mode /config (print_slash.go) still goes through
// its own typed mappers — Field is the TUI-only counterpart.
func Field(key string) (any, error) {
	return cachedField(key, true)
}

func cachedField(key string, validate bool) (any, error) {
	data, err := readSettingsBytes()
	if err != nil {
		return nil, err
	}

	settingsCacheMu.RLock()
	isCurrent := len(cachedMergedBytes) > 0 && len(data) == len(cachedMergedBytes) && &data[0] == &cachedMergedBytes[0]
	if isCurrent {
		if validate && cachedValidChecked && !cachedValid {
			settingsCacheMu.RUnlock()
			var m map[string]any
			return nil, json.Unmarshal(data, &m)
		}
		if (!validate || cachedValidChecked) && cachedFields != nil {
			if v, ok := cachedFields[key]; ok {
				settingsCacheMu.RUnlock()
				if _, isMissing := v.(missingFieldSentinel); isMissing {
					return nil, nil
				}
				return v, nil
			}
		}
	}
	settingsCacheMu.RUnlock()

	if validate {
		valid := gjson.ValidBytes(data)
		if !valid {
			settingsCacheMu.Lock()
			if len(cachedMergedBytes) > 0 && len(data) == len(cachedMergedBytes) && &data[0] == &cachedMergedBytes[0] {
				cachedValidChecked = true
				cachedValid = false
			}
			settingsCacheMu.Unlock()
			var m map[string]any
			return nil, json.Unmarshal(data, &m)
		}
	}

	res := gjson.GetBytes(data, key)
	var val any
	var cacheable bool
	if !res.Exists() {
		val = missingField
		cacheable = true
	} else {
		val = res.Value()
		switch val.(type) {
		case nil, bool, string, float64, int, int64:
			cacheable = true
		}
	}

	settingsCacheMu.Lock()
	if len(cachedMergedBytes) > 0 && len(data) == len(cachedMergedBytes) && &data[0] == &cachedMergedBytes[0] {
		if validate {
			cachedValidChecked = true
			cachedValid = true
		}
		if cacheable {
			if cachedFields == nil {
				cachedFields = make(map[string]any, 16)
			}
			cachedFields[key] = val
		}
	}
	settingsCacheMu.Unlock()

	if _, isMissing := val.(missingFieldSentinel); isMissing {
		return nil, nil
	}
	return val, nil
}

// SetField writes one raw value into settings.json at the dot
// path. Nested objects are created as needed. Bool values are persisted
// as JSON `true`/`false`; strings as JSON strings. Other types pass
// through JSON marshalling.
//
// Cycle 196: companion to Field — /config ←/→ calls this after
// determining the next value in the cycle list. Pre-cycle-196 the panel
// had no live persistence; settings.json only updated via the /statusline
// and /title slash-command toggles.
func SetField(key string, value any) error {
	sessionSettingsMu.Lock()
	if sessionSettingsOverlay != nil {
		sessionSettingsOverlay[key] = cloneSettingsValue(value)
	}
	sessionSettingsMu.Unlock()

	return modifySettings(true, false, func(data []byte) ([]byte, error) {
		switch v := value.(type) {
		case bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64, string, nil:
			return sjson.SetBytes(data, key, v)
		default:
			b, err := json.Marshal(v)
			if err != nil {
				return nil, err
			}
			return sjson.SetRawBytes(data, key, b)
		}
	})
}

// DeleteField removes the value at the dot path from settings.json.
// The reference represents a setting at its default value by the *absence*
// of the key (probed: cycling /config's Agent Mode back to "default"
// deletes `agentMode`; Copy on Select's "on" is absent while "off" is
// JSON false). Cycle 197.
//
// A missing path is not an error — deleting an already-absent key is a
// no-op. Parent objects left empty are kept, matching the reference which
// never prunes `statusLine`/`title` shells.
func DeleteField(key string) error {
	sessionSettingsMu.Lock()
	if sessionSettingsOverlay != nil {
		delete(sessionSettingsOverlay, key)
	}
	sessionSettingsMu.Unlock()

	return modifySettings(true, true, func(data []byte) ([]byte, error) {
		return sjson.DeleteBytes(data, key)
	})
}

// UpdateOrCreate is the cycle-125 sibling of Update.
// When settings.json is missing, it creates the parent dir (0700) and
// writes a fresh settings.json containing only the keys the mutator
// chooses to set — instead of returning ENOENT. Used by the trust
// gate's accept handler so a first-launch user accepting the dialog
// persists the cwd before WriteCompatDefaults has had a chance to
// run. When the file does exist, the behaviour is identical to
// Update (preserves unknown fields, alphabetises keys via
// map marshalling).
func UpdateOrCreate(mutate func(map[string]any)) error {
	return modifySettings(true, true, func(data []byte) ([]byte, error) {
		var m map[string]any
		_ = json.Unmarshal(data, &m)
		if m == nil {
			m = map[string]any{}
		}
		mutate(m)
		return json.Marshal(m)
	})
}

// ResolveModel returns the model specified in settings.json,
// or fallback if settings.json does not exist or has no model.
func ResolveModel(fallback string) string {
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
