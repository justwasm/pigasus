package panels

import (
	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/ui/views/pluginsui"
)

// PluginHost defines the host operations needed by the /plugin panel.
type PluginHost interface {
	ClosePanel() tea.Cmd
}

// NewPluginPanel builds the /plugin panel with the given host seams.
func NewPluginPanel(h PluginHost) *pluginsui.Panel {
	return pluginsui.New(pluginsui.Seams{
		Close: h.ClosePanel,
	})
}
