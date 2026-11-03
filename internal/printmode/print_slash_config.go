package printmode

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/justwasm/pigasus/internal/features/auth"
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
	"seasonalThemesOff",
	"seasonalThemesOptIn",
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

// printSlashConfigKeySet returns the union of the reference's known settings
// keys and every key present in settings.json, sorted alphabetically.
//
// 【一手】 agy 1.4.8 reports the union: a key the reference does not know
// ("dismissedAnnouncements" on the capture machine) is printed next to the
// known keys, at its alphabetical place — between "disableSlashCommands" and
// "editor".
func printSlashConfigKeySet(raw map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(printSlashConfigKeys)+len(raw))
	seen := make(map[string]bool, len(printSlashConfigKeys))
	for _, k := range printSlashConfigKeys {
		keys = append(keys, k)
		seen[k] = true
	}
	for k := range raw {
		if !seen[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

// readRawConfigSettings reads settings.json into raw JSON messages.
func readRawConfigSettings() map[string]json.RawMessage {
	raw := map[string]json.RawMessage{}
	if b, err := readSettingsFile(); err == nil {
		_ = json.Unmarshal(b, &raw)
	}
	return raw
}

// printSlashConfig dumps the settings as "key\tvalue" lines. String values are
// unquoted and everything else keeps its raw JSON, matching the reference.
func printSlashConfig() string {
	raw := readRawConfigSettings()
	var b strings.Builder
	for _, k := range printSlashConfigKeySet(raw) {
		fmt.Fprintf(&b, "%s\t%s\n", k, configValueText(configValue(k, raw[k])))
	}
	return b.String()
}

// printSlashPermissions prints the settings.json permission rules as
// "<scope>\t<action>\t<rule>", which is the shape `agy -p /permissions`
// emits (e.g. "global\tallow\tcommand(grep)").
//
// The three lists are printed in the reference's own order — allow, deny,
// ask — because the reference has three lists (PermissionGrantsConfig
// carries GetAllow, GetDeny and GetAsk) and the ask list now steers the
// agent's behaviour: a rule the dump does not show is a rule the user
// cannot account for.
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
	for _, r := range settings.Ask {
		fmt.Fprintf(&b, "global\task\t%s\n", r)
	}
	return b.String()
}

// printSlashSettings is the subset of settings.json the print-mode commands
// read.
type printSlashSettings struct {
	Allow []string `json:"allow"`
	Ask   []string `json:"ask"`
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
// first and the three rule lists are omitempty, matching the reference:
//
//	{"scope":"global","allow":["command(grep)",…]}
type jsonPermissionScope struct {
	Scope string   `json:"scope"`
	Allow []string `json:"allow,omitempty"`
	Deny  []string `json:"deny,omitempty"`
	Ask   []string `json:"ask,omitempty"`
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
	global := jsonPermissionScope{
		Scope: "global",
		Allow: settings.Allow,
		Deny:  settings.Deny,
		Ask:   settings.Ask,
	}
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

// printSlashConfigData renders the `/config` JSON payload.
//
// The two object-valued settings keep the reference's field order (type,
// command, enabled) because configValue resolves them into their struct form
// rather than a map — the encoder would otherwise re-sort them
// alphabetically. The settings' top-level keys are unaffected: the reference
// emits those alphabetically too, which is what a map gives us.
func printSlashConfigData() any {
	return configData()
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

