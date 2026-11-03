// area.go — the status area's rows.
//
// The status area is drawn at the bottom of the screen:
//
//   - the built-in row is the "? for shortcuts … auto · <model> · <effort>"
//     line;
//   - a custom status line replaces the built-in row;
//   - `stack_with_default` keeps the built-in row and puts the custom one
//     below it;
//   - `padding` adds blank lines above the custom row;
//   - a failed script shows the reference's error card in the custom row's
//     place, and 30 failures in a row switch the script off for the rest of
//     the session.
//
// Evidence 【一手】: agy 1.4.7 with `padding: 2` and
// `stack_with_default: true` rendered
//
//	? for shortcuts    auto · Gemini 3.8 Flash · high
//
//	                                   ← the two padding rows
//	                                   ←
//	CUSTOM_ROW
//
// and with a failing script it rendered the error card
//
//	⚠ Statusline Error
//	  ⎿  Statusline error:
//	     command failed: exit status 3 (stderr: boom)
//	     Full logs at: <log path>
//
// The rows are the package's: the layout rules, the disabled message and
// the error card are facts this package already owns — the `Config`, the
// `State` and the `DisabledMessage` next door. The host keeps the facts
// the rows cannot know — the built-in row, whether a script is configured
// and enabled, and the log file the error card points at — and hands them
// over as an [AreaState].
package statusline

import (
	"strings"

	"github.com/justwasm/pigasus/palette"
)

// AreaState is the host's facts about the status area.
//
// The facts are the host's: the built-in row is the host's own template,
// "a script is configured and enabled" is the host's reading of the
// settings, and the log file is the CLI's own file. The rows are the
// package's.
type AreaState struct {
	// Builtin is the built-in status row.
	Builtin string
	// Active reports whether a custom status-line script is configured
	// and enabled.
	Active bool
	// Config is the effective `statusLine` block: its padding, and
	// whether the built-in row is stacked with the custom one.
	Config Config
	// State is the cached output of the script. Nil before the first
	// run.
	State *State
	// LogPath is the log file the error card points the user at.
	LogPath string
}

// AreaRows returns the rows the status area occupies, top to bottom.
//
// A script that is not configured, or that has not run yet, leaves the
// built-in row alone. A script that failed shows the reference's error
// card in the custom row's place, and a script that failed 30 times in a
// row shows the disabled message — the two are the package's own.
func AreaRows(s AreaState) []string {
	builtin := s.Builtin
	if !s.Active || s.State == nil {
		return []string{builtin}
	}
	out, err := s.State.Current()
	var custom []string
	switch {
	case s.State.Disabled():
		custom = []string{DisabledMessage}
	case err != nil:
		custom = ErrorRows(err, s.LogPath)
	case strings.TrimSpace(out) != "":
		custom = strings.Split(out, "\n")
	}
	if custom == nil {
		return []string{builtin}
	}
	rows := []string{}
	if s.Config.StackWithDefault {
		rows = append(rows, builtin)
	}
	for i := 0; i < s.Config.Padding; i++ {
		rows = append(rows, "")
	}
	return append(rows, custom...)
}

// ErrorRows renders the reference's status-line error card.
//
// The card names the failure and the log file the full output is in.
//
// Evidence 【一手】: agy 1.4.7 rendered
//
//	⚠ Statusline Error
//	  ⎿  Statusline error:
//	     command failed: exit status 3 (stderr: boom)
//	     Full logs at: /Users/gear/.gemini/antigravity-cli/log/cli-20261025_210717.log
func ErrorRows(err error, logPath string) []string {
	body := palette.WarningBodyOpen + err.Error() + palette.WarningBodyClose
	return []string{
		palette.WarningOpen + "⚠ Statusline Error" + palette.WarningClose,
		"  " + palette.WarningBlueOpen + "⎿  " + palette.WarningBlueClose +
			palette.WarningBodyOpen + "Statusline error:" + palette.WarningBodyClose,
		"     " + body,
		"     " + palette.WarningBodyOpen + "Full logs at: " + logPath + palette.WarningBodyClose,
	}
}
