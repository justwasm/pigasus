// statusline_render.go — the host's side of the status area.
//
// The rows are internal/ui/views/statusline's: the package owns the
// layout rules, the disabled message and the error card, and it renders
// them from the facts the host hands over.
//
// The host keeps the facts the rows cannot know — the built-in row,
// whether a custom script is configured and enabled, and the log file the
// error card points the user at — and collects them into a
// statusline.AreaState.
//
// The window title lives here as well: the title script's output when a
// title script is configured, the conversation title otherwise.
package cliapp

import (
	confhome "github.com/justwasm/pigasus/internal/core/confhome"
	"github.com/justwasm/pigasus/internal/core/logts"
	"github.com/justwasm/pigasus/internal/ui/views/statusline"
	"github.com/justwasm/pigasus/internal/ui/views/statusrow"
)

// statusAreaState collects the facts the status area's rows are drawn
// from.
//
// The rows themselves are the package's — the layout rules, the disabled
// message and the error card. This is the collector: it gathers what the
// host alone knows and hands it over.
func (m *runMode) statusAreaState() statusline.AreaState {
	return statusline.AreaState{
		Builtin: statusrow.Render(m.statusRowState()),
		Active:  m.statusLineScriptActive(),
		Config:  m.statusLineCfg,
		State:   m.externalStateFor(),
		LogPath: confhome.CLILogPath(logts.FormatLogTS()),
	}
}

// windowTitleText is the terminal window title: the title script's output
// when a title script is configured, the conversation title otherwise.
func (m *runMode) windowTitleText() string {
	if m.titleCfg.Active() {
		// The title the title script produced.
		if title := m.titleStoreFor().Get(); title != "" {
			return title
		}
	}
	if m.title == "on" {
		return m.conversationTitle()
	}
	return ""
}
