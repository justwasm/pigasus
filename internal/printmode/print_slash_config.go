package printmode

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/justwasm/pigasus/internal/auth"
)

// printSlashConfigKeys is the reference's settings key set in the order
// `agy -p /config` prints it (alphabetical). Recovered from the 1.3.6 output.
var printSlashConfigKeys = []string{
	"agentMode",
	"allowNonWorkspaceAccess",
	"altScreenMode",
	"artifactReviewPolicy",
	"clearScrollbackOnResize",
	"colorScheme",
	"contextWindow",
	"copyOnSelect",
	"customModelsConfig",
	"disableSlashCommands",
	"editor",
	"editorMode",
	"enableTerminalSandbox",
	"gcp",
	"hideActiveItems",
	"historySize",
	"model",
	"modelProvider",
	"notifications",
	"permissions",
	"pickerGrouping",
	"queuedMessages",
	"runningLightSpeed",
	"sandboxProxy",
	"showFeedbackSurvey",
	"showTips",
	"statusLine",
	"title",
	"toolPermission",
	"trustedWorkspaces",
	"useG1Credits",
	"useTerminalCursor",
	"verbosity",
	"vimInsertFirst",
}

// printSlashConfigDefaults are the reference's built-in settings defaults, as
// printed for keys the user has never set. Recovered from a default-profile
// `agy -p /config` capture.
var printSlashConfigDefaults = map[string]string{
	"allowNonWorkspaceAccess": "false",
	"altScreenMode":           "default",
	"artifactReviewPolicy":    "always-proceed",
	"clearScrollbackOnResize": "true",
	"colorScheme":             "light",
	"copyOnSelect":            "true",
	"disableSlashCommands":    "false",
	"editor":                  "auto",
	"editorMode":              "default",
	"enableTerminalSandbox":   "false",
	"hideActiveItems":         "false",
	"historySize":             "2000",
	"notifications":           "false",
	"pickerGrouping":          "none",
	"queuedMessages":          "queue",
	"runningLightSpeed":       "medium",
	"showFeedbackSurvey":      "true",
	"showTips":                "true",
	"toolPermission":          "always-proceed",
	"useG1Credits":            "false",
	"useTerminalCursor":       "false",
	"verbosity":               "medium",
	"vimInsertFirst":          "false",
}

// printSlashConfig dumps the settings as "key\tvalue" lines. String values are
// unquoted and everything else keeps its raw JSON, matching the reference.
func printSlashConfig() string {
	raw := map[string]json.RawMessage{}
	if b, err := os.ReadFile(filepath.Join(auth.AntigravityHome(), "settings.json")); err == nil {
		_ = json.Unmarshal(b, &raw)
	}
	var b strings.Builder
	for _, k := range printSlashConfigKeys {
		val := printSlashConfigDefaults[k]
		if v, ok := raw[k]; ok {
			var s string
			if enc, done := encodeOrderedConfigValue(k, v); done {
				val = enc
			} else if json.Unmarshal(v, &s) == nil {
				val = s
			} else if anyVal := any(nil); json.Unmarshal(v, &anyVal) == nil {
				// The reference re-encodes non-string values with the
				// standard library encoder, so a pretty-printed
				// settings.json still prints on one line and `<`/`>`/`&`
				// come back as \u003c/\u003e/\u0026 (Go's default HTML
				// escaping).
				if b, err := json.Marshal(anyVal); err == nil {
					val = string(b)
				}
			} else {
				val = string(v)
			}
		}
		fmt.Fprintf(&b, "%s\t%s\n", k, val)
	}
	return b.String()
}

// printSlashPermissions prints the settings.json permission rules as
// "<scope>\t<action>\t<rule>", which is the shape `agy -p /permissions`
// emits (e.g. "global\tallow\tcommand(grep)").
func printSlashPermissions() string {
	settings, err := readPrintSlashSettings()
	if err != nil {
		return ""
	}
	var b strings.Builder
	for _, r := range settings.Allow {
		fmt.Fprintf(&b, "global\tallow\t%s\n", r)
	}
	for _, r := range settings.Deny {
		fmt.Fprintf(&b, "global\tdeny\t%s\n", r)
	}
	return b.String()
}

// printSlashSettings is the subset of settings.json the print-mode commands
// read.
type printSlashSettings struct {
	Allow []string `json:"allow"`
	Deny  []string `json:"deny"`
}

func readPrintSlashSettings() (printSlashSettings, error) {
	var out printSlashSettings
	b, err := readSettingsFile()
	if err != nil {
		return out, err
	}
	var wrapper struct {
		Permissions printSlashSettings `json:"permissions"`
	}
	if err := json.Unmarshal(b, &wrapper); err != nil {
		return out, err
	}
	return wrapper.Permissions, nil
}

// readSettingsFile reads the CLI's settings.json with any leading UTF-8
// BOM stripped. AGY 1.2.16 changelog line 13 (cycle config-bom-strip):
// Notepad / PowerShell `Set-Content -Encoding UTF8` on Windows writes a
// BOM, and json.Unmarshal fails with
// "invalid character 'ï' looking for beginning of value" until the
// leading 0xEF 0xBB 0xBF is removed. Every call site that previously
// read settings.json now gets the BOM-tolerant form for free.
func readSettingsFile() ([]byte, error) {
	data, err := os.ReadFile(filepath.Join(auth.AntigravityHome(), "settings.json"))
	if err != nil {
		return nil, err
	}
	return stripUTF8BOM(data), nil
}

// stripUTF8BOM drops a leading UTF-8 byte order mark (EF BB BF) if
// present. See readSettingsFile.
func stripUTF8BOM(b []byte) []byte {
	if len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		return b[3:]
	}
	return b
}

// jsonPermissionScope is one entry of the `/permissions` payload. `scope` comes
// first and the two rule lists are omitempty, matching the reference:
//
//	{"scope":"global","allow":["command(grep)",…]}
type jsonPermissionScope struct {
	Scope string   `json:"scope"`
	Allow []string `json:"allow,omitempty"`
	Deny  []string `json:"deny,omitempty"`
}

// printSlashPermissionsData renders the `/permissions` command payload.
//
// The reference always reports the three scopes in the order project, shared,
// global. On the capture machine only the global scope holds rules — which is
// why the text answer (global lines only) and the payload agree — but the empty
// project and shared scopes are still emitted.
func printSlashPermissionsData() any {
	settings, err := readPrintSlashSettings()
	if err != nil {
		settings = printSlashSettings{}
	}
	global := jsonPermissionScope{Scope: "global", Allow: settings.Allow, Deny: settings.Deny}
	return map[string]any{"permissions": []jsonPermissionScope{
		{Scope: "project"},
		{Scope: "shared"},
		global,
	}}
}

// jsonStatusLineSetting is the value of the `statusLine` and `title` settings.
//
// The reference models these as a Go struct and re-encodes it with
// encoding/json, so the members come back in *struct* field order
// (type, command, enabled) — a map[string]any would sort them alphabetically
// (command, enabled, type) and print `{"command":"","enabled":true,"type":""}`.
// Field order is 【一手】: captured from `agy -p=/config` in both text and
// `--output-format json`.
type jsonStatusLineSetting struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Enabled bool   `json:"enabled"`
}

// orderSensitiveConfigKeys are the settings whose value is a JSON *object* with
// a recovered field order, so it must round-trip through its struct rather than
// a map (which the encoder would re-sort).
var orderSensitiveConfigKeys = map[string]bool{
	"statusLine": true,
	"title":      true,
}

// encodeOrderedConfigValue renders one settings value the way the reference
// does for the text dump. Simple values keep their raw JSON (so a string is
// printed unquoted, its surrounding quotes stripped by the caller), while an
// object with a recovered struct order is re-marshaled from that struct.
//
// Returns ("", false) when the value is not one this helper knows how to
// render — the caller falls back to its generic path.
func encodeOrderedConfigValue(key string, raw json.RawMessage) (string, bool) {
	if !orderSensitiveConfigKeys[key] {
		return "", false
	}
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return "", false
	}
	var sl jsonStatusLineSetting
	if err := json.Unmarshal(raw, &sl); err != nil {
		return "", false
	}
	b, err := json.Marshal(sl)
	if err != nil {
		return "", false
	}
	return string(b), true
}

// printSlashConfigData renders the `/config` JSON payload.
//
// It wraps configData (print_format.go) — which returns a plain
// map[string]any — and swaps the two object-valued settings for their struct
// form so the `--output-format json` payload keeps the reference's field order
// (type, command, enabled) instead of the alphabetical order a nested map
// would produce. The settings' top-level keys are unaffected: the reference
// emits those alphabetically too, which is what a map gives us.
func printSlashConfigData() any {
	data := configData()
	outer, ok := data.(map[string]any)
	if !ok {
		return data
	}
	vals, ok := outer["config"].(map[string]any)
	if !ok {
		return data
	}
	for k := range orderSensitiveConfigKeys {
		m, ok := vals[k].(map[string]any)
		if !ok {
			continue // unset (nil) or already a non-object value
		}
		b, err := json.Marshal(m)
		if err != nil {
			continue
		}
		var sl jsonStatusLineSetting
		if json.Unmarshal(b, &sl) != nil {
			continue
		}
		vals[k] = sl
	}
	return data
}

// PrintSlashConfig dumps the settings as "key\tvalue" lines.
func PrintSlashConfig() string {
	return printSlashConfig()
}

// PrintSlashConfigKeys is the reference's settings key set in canonical order.
var PrintSlashConfigKeys = printSlashConfigKeys

// PrintSlashPermissions prints the settings.json permission rules as text.
func PrintSlashPermissions() string {
	return printSlashPermissions()
}

