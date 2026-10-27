// statusline_render.go — the status area's layout.
//
// The reference renders the status area at the bottom of the screen:
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
package cliapp

import (
	"path/filepath"
	"strings"

	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/statusline"
	"github.com/justwasm/pigasus/internal/tuilib"
	"github.com/justwasm/pigasus/palette"
)

// statusAreaRows returns the rows the status area occupies, top to bottom.
func (m *runMode) statusAreaRows() []string {
	builtin := m.builtinStatusRow()
	if !m.statusLineScriptActive() {
		return []string{builtin}
	}
	state := m.externalStateFor()
	if state == nil {
		return []string{builtin}
	}
	out, err := state.Current()
	var custom []string
	switch {
	case state.Disabled():
		custom = []string{statusline.DisabledMessage}
	case err != nil:
		custom = m.statusLineErrorRows(err)
	case strings.TrimSpace(out) != "":
		custom = strings.Split(out, "\n")
	}
	if custom == nil {
		return []string{builtin}
	}
	cfg := m.statusLineConfig()
	rows := []string{}
	if cfg.StackWithDefault {
		rows = append(rows, builtin)
	}
	for i := 0; i < cfg.Padding; i++ {
		rows = append(rows, "")
	}
	return append(rows, custom...)
}

// statusLineErrorRows renders the reference's status-line error card.
//
// Evidence 【一手】: agy 1.4.7 rendered
//
//	⚠ Statusline Error
//	  ⎿  Statusline error:
//	     command failed: exit status 3 (stderr: boom)
//	     Full logs at: /Users/gear/.gemini/antigravity-cli/log/cli-20261025_210717.log
func (m *runMode) statusLineErrorRows(err error) []string {
	body := palette.WarningBodyOpen + err.Error() + palette.WarningBodyClose
	return []string{
		palette.WarningOpen + "⚠ Statusline Error" + palette.WarningClose,
		"  " + palette.WarningBlueOpen + "⎿  " + palette.WarningBlueClose +
			palette.WarningBodyOpen + "Statusline error:" + palette.WarningBodyClose,
		"     " + body,
		"     " + palette.WarningBodyOpen + "Full logs at: " + m.statusLineLogPath() + palette.WarningBodyClose,
	}
}

// statusLineLogPath is the log file the reference points the user at when a
// status-line script fails.
func (m *runMode) statusLineLogPath() string {
	return filepath.Join(auth.AntigravityConfigHome(), "log", "cli-"+tuilib.FormatLogTS()+".log")
}

// windowTitleText is the terminal window title: the title script's output
// when a title script is configured, the conversation title otherwise.
func (m *runMode) windowTitleText() string {
	if m.titleScriptActive() {
		if title := m.windowTitleValue(); title != "" {
			return title
		}
	}
	if m.title == "on" {
		return m.conversationTitle()
	}
	return ""
}
