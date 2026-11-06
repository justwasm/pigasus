package panels

import (
	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/ui/views/skillsui"
)

// SkillsHost defines the host operations needed by the /skills panel.
type SkillsHost interface {
	SkillsCatalog() []skillsui.SkillInfo
	TerminalOSC8() bool
	TerminalSize() (width int, height int)
	ClosePanel() tea.Cmd
}

// NewSkillsPanel builds the /skills panel with the given host seams.
func NewSkillsPanel(h SkillsHost) *skillsui.Panel {
	return skillsui.New(skillsui.Seams{
		Catalog: h.SkillsCatalog,
		OSC8:    h.TerminalOSC8,
		Size:    h.TerminalSize,
		Close:   h.ClosePanel,
	})
}
