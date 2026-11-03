// defaults.go — the reference's built-in settings defaults.
//
// The table is the *compiled* default of every key the reference knows:
// the value the CLI uses for a key the user has never set. It has one
// owner — the start-up normalisation compares the file against it
// (`normalize.go`), and the print-mode `/config` dump reports it.
//
// The table used to live in `internal/printmode` (the `/config` dump's
// own copy). It moved down here because the normalisation needs it and
// `internal/printmode` is an entry surface above every tier: a tier-1
// package may not reach up into it. The print mode asks this package now.
//
// 【一手】 agy 1.4.8: `agy -p /config` with an empty settings.json prints
// exactly these values, and the `--output-format json` payload carries the
// same values with their JSON types (a string default is a string, a bool
// default is a bool, historySize is a number, and the seven nullable keys —
// customModelsConfig, gcp, permissions, sandboxProxy, statusLine, title,
// trustedWorkspaces — are null).
//
// The 1.4.8 defaults differ from the 1.3.6 capture the clone used to carry:
// artifactReviewPolicy is "asks-for-review" (was "always-proceed"),
// toolPermission is "request-review" (was "always-proceed") and colorScheme
// is "terminal" (was "light").
package settings

// defaults maps every known settings key to the reference's built-in
// default value.
//
// The key set is the reference's own 36-key catalogue — the same set
// `ValidKeys` names. A key that is not in this table has no default: the
// reference does not know it, so the file's value is kept as it is.
var defaults = map[string]any{
	"agentMode":               "",
	"allowNonWorkspaceAccess": false,
	"altScreenMode":           "default",
	"artifactReviewPolicy":    "asks-for-review",
	"clearScrollbackOnResize": true,
	"colorScheme":             "terminal",
	"contextWindow":           "",
	"copyOnSelect":            true,
	"customModelsConfig":      nil,
	"disableSlashCommands":    false,
	"editor":                  "auto",
	"editorMode":              "default",
	"enableTerminalSandbox":   false,
	"gcp":                     nil,
	"hideActiveItems":         false,
	"historySize":             2000,
	"model":                   "",
	"modelProvider":           "",
	"notifications":           false,
	"permissions":             nil,
	"pickerGrouping":          "none",
	"queuedMessages":          "queue",
	"runningLightSpeed":       "medium",
	"sandboxProxy":            nil,
	"seasonalThemesOff":       "",
	"seasonalThemesOptIn":     "",
	"showFeedbackSurvey":      true,
	"showTips":                true,
	"statusLine":              nil,
	"title":                   nil,
	"toolPermission":          "request-review",
	"trustedWorkspaces":       nil,
	"useG1Credits":            false,
	"useTerminalCursor":       false,
	"verbosity":               "medium",
	"vimInsertFirst":          false,
}

// Default returns the compiled default of one settings key.
//
// A key the reference does not know has no default, and so has a nil
// value — the same value the seven nullable keys carry. The `/config`
// dump reports an unknown key as null, and the normalisation keeps
// whatever the file holds for it.
func Default(key string) any {
	return defaults[key]
}

// DefaultString returns one key's default as a string. Keys whose default
// is not a string yield "".
func DefaultString(key string) string {
	s, _ := defaults[key].(string)
	return s
}
