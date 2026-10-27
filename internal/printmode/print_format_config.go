// print_format_config.go — config and help payload structures for print JSON envelope.

package printmode

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// jsonHelpCommand is one row of the `commands` list in the `/help` JSON payload.
type jsonHelpCommand struct {
	Name        string   `json:"name"`
	Aliases     []string `json:"aliases,omitempty"`
	Description string   `json:"description"`
}

// printSlashHelpCommands is the command table `/help` reports, in the order the
// reference emits it. It is the structured form of printSlashHelpText and is
// checked against it by TestHelpTableMatchesCapturedText.
var printSlashHelpCommands = []jsonHelpCommand{
	{Name: "agents", Description: "List available custom agents"},
	{Name: "changelog", Description: "Show release notes and changes"},
	{Name: "config", Aliases: []string{"settings"}, Description: "Open settings panel"},
	{Name: "credits", Description: "Show remaining G1 credits and purchase link"},
	{Name: "effort", Description: "Set the reasoning effort"},
	{Name: "help", Description: "Show available commands and keybindings"},
	{Name: "hooks", Description: "Manage hook configurations for tool events"},
	{Name: "model", Description: "Set a model, or run a single prompt on another model"},
	{Name: "permissions", Description: "Manage tool permissions"},
	{Name: "plugin", Description: "Manage, discover, install, uninstall, enable, and disable plugins"},
	{Name: "skills", Description: "List available skills"},
	{Name: "usage", Aliases: []string{"quota"}, Description: "View model quota usage"},
}

// helpCommandsData renders the `/help` command payload.
func helpCommandsData() any {
	return map[string]any{"commands": printSlashHelpCommands}
}

// printSlashConfigTypes records each settings key's JSON type, recovered from
// the `command.data.config` payload of `agy -p=/config --output-format json`.
//
// The type decides what an explicit `null` in settings.json decodes to: the
// zero value of the key's type ("" for a string key, false for a bool key, 0
// for a number key, null for the nullable ones).
var printSlashConfigTypes = map[string]string{
	"agentMode":               "string",
	"allowNonWorkspaceAccess": "bool",
	"altScreenMode":           "string",
	"artifactReviewPolicy":    "string",
	"clearScrollbackOnResize": "bool",
	"colorScheme":             "string",
	"contextWindow":           "string",
	"copyOnSelect":            "bool",
	"customModelsConfig":      "null",
	"disableSlashCommands":    "bool",
	"editor":                  "string",
	"editorMode":              "string",
	"enableTerminalSandbox":   "bool",
	"gcp":                     "null",
	"hideActiveItems":         "bool",
	"historySize":             "int",
	"model":                   "string",
	"modelProvider":           "string",
	"notifications":           "bool",
	"permissions":             "object",
	"pickerGrouping":          "string",
	"queuedMessages":          "string",
	"runningLightSpeed":       "string",
	"sandboxProxy":            "null",
	"seasonalThemesOff":       "string",
	"seasonalThemesOptIn":     "string",
	"showFeedbackSurvey":      "bool",
	"showTips":                "bool",
	"statusLine":              "null",
	"title":                   "null",
	"toolPermission":          "string",
	"trustedWorkspaces":       "list",
	"useG1Credits":            "bool",
	"useTerminalCursor":       "bool",
	"verbosity":               "string",
	"vimInsertFirst":          "bool",
}

// configValues resolves every settings key the reference reports to the typed
// value its JSON payload carries.
//
// The key set is the union of the reference's known keys and every key
// present in settings.json — see printSlashConfigKeySet.
func configValues() map[string]any {
	raw := readRawConfigSettings()
	out := make(map[string]any, len(raw))
	for _, k := range printSlashConfigKeySet(raw) {
		out[k] = configValue(k, raw[k])
	}
	return out
}

// configValue resolves one key. raw is nil when the key is absent.
//
//   - absent → the compiled default, with the key's JSON type
//   - present and null → the zero value of the key's type
//   - present → the file's value
//
// 【一手】 agy 1.4.8: with `{"editor":null}` in settings.json the text dump
// prints an empty cell for `editor` and the JSON payload carries the empty
// string; a key the reference does not know keeps its null.
func configValue(key string, raw json.RawMessage) any {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return printSlashConfigDefaults[key]
	}
	if bytes.Equal(trimmed, []byte("null")) {
		return configZeroValue(key)
	}
	return configRawValue(key, trimmed)
}

// configZeroValue is the value an explicit null decodes to for one key.
func configZeroValue(key string) any {
	switch printSlashConfigTypes[key] {
	case "string":
		return ""
	case "bool":
		return false
	case "int":
		return 0
	}
	return nil
}

// configRawValue decodes a settings value that is present in the file.
//
// The two object-valued settings with a recovered field order decode into
// their struct, so the encoder keeps the reference's member order (type,
// command, enabled) instead of re-sorting it alphabetically.
func configRawValue(key string, raw json.RawMessage) any {
	if orderSensitiveConfigKeys[key] {
		var sl jsonStatusLineSetting
		if err := json.Unmarshal(raw, &sl); err == nil {
			return sl
		}
	}
	var v any
	if err := json.Unmarshal(raw, &v); err == nil {
		return v
	}
	return nil
}

// configValueText renders a resolved settings value the way the reference's
// text dump does: a string bare, everything else through the standard JSON
// encoder — so a null renders as an empty cell, a number as its literal, and
// an object on one line with Go's HTML escaping (`<` comes back as \u003c).
func configValueText(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

// configData renders the `/config` command payload.
func configData() any {
	return map[string]any{"config": configValues()}
}
