// print_format_config.go — config and help payload structures for print JSON envelope.

package printmode

import (
	"encoding/json"
	"strconv"
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

// configValues resolves every known settings key to the typed value the JSON
// payload carries.
func configValues() map[string]any {
	raw := map[string]json.RawMessage{}
	if b, err := readSettingsFile(); err == nil {
		_ = json.Unmarshal(b, &raw)
	}
	out := make(map[string]any, len(printSlashConfigKeys))
	for _, k := range printSlashConfigKeys {
		out[k] = configValue(k, raw[k])
	}
	return out
}

// configValue resolves one key. raw is nil when the key is absent.
func configValue(key string, raw json.RawMessage) any {
	if len(raw) > 0 && string(raw) != "null" {
		var v any
		if json.Unmarshal(raw, &v) == nil {
			return v
		}
	}
	switch printSlashConfigTypes[key] {
	case "null":
		return nil
	case "bool":
		return printSlashConfigDefaults[key] == "true"
	case "int":
		n, _ := strconv.Atoi(printSlashConfigDefaults[key])
		return n
	case "list":
		return []any{}
	case "object":
		return map[string]any{}
	default:
		return printSlashConfigDefaults[key]
	}
}

// configData renders the `/config` command payload.
func configData() any {
	return map[string]any{"config": configValues()}
}
