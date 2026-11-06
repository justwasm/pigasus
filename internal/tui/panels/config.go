package panels

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/ui/views/configpanel"
	"github.com/justwasm/pigasus/internal/ui/views/seasonal"
)

// ConfigHost defines the host operations needed by the /config panel.
type ConfigHost interface {
	ApplyThemeLive(scheme string) tea.Cmd
	ApplyAltScreenLive(stored any)
	ApplyVerbosityLive(verbosity string)
	RefreshSeasonalTheme()
	ClosePanel() tea.Cmd
	SlashRespond(cmd, msg string, extra ...string)
	SeasonalNow() time.Time
	ConfigOverrideFor(row configpanel.Row) (string, bool)
}

// NewConfigPanel builds the /config panel with the given host seams.
func NewConfigPanel(h ConfigHost) *configpanel.Panel {
	return configpanel.New(configpanel.Seams{
		ApplyTheme:      h.ApplyThemeLive,
		ApplyAltScreen:  h.ApplyAltScreenLive,
		ApplyVerbosity:  h.ApplyVerbosityLive,
		RefreshSeasonal: h.RefreshSeasonalTheme,
		Close:           h.ClosePanel,
		Respond:         func(cmd, msg string) { h.SlashRespond(cmd, msg) },
		SeasonalEvent:   func() *seasonal.Event { return seasonal.Current(h.SeasonalNow()) },
		OverrideFor:     h.ConfigOverrideFor,
	})
}
