// skills_host.go — the host's side of the /skills panel.
//
// # Where the panel lives
//
// The panel itself is internal/ui/views/skillsui: it owns the grouped
// list, the cursor, the per-skill detail view and its scroll window, the
// keys and the rows. The host supplies the four things the panel cannot
// answer itself — the catalogue the panel reads, whether the terminal can
// carry OSC 8 hyperlinks, the terminal's size and the way out of the
// panel — and nothing else.
//
// The panel is rebuilt every time it opens: the host's slashPanel struct
// is reset when a panel closes, so a stale copy of the cursor, the detail
// view or the scroll window is never shown.

package cliapp

import (
	"github.com/justwasm/pigasus/internal/ui/views/skillsui"
)

// newSkillsPanel builds the /skills panel with the host's seams.
func (m *runMode) newSkillsPanel() *skillsui.Panel {
	return skillsui.New(skillsui.Seams{
		// The catalogue is the host's: slashSkillCatalog is a var so the
		// tests can substitute a synthetic catalogue. The panel reads it
		// fresh on every draw, so a skill installed while the panel is
		// open shows up without the panel being rebuilt.
		Catalog: func() []skillsui.SkillInfo {
			return slashSkillCatalog()
		},
		// The marketplace link under a skill row is only rendered when
		// the terminal can carry an OSC 8 hyperlink.
		OSC8: func() bool {
			return m.hyperlinkCaps.OSC8
		},
		// The panel's rows are laid out for the terminal's size; before
		// the first size message the panel falls back to the capture's
		// 220×30.
		Size: func() (int, int) {
			return m.width, m.height
		},
		// Esc — and the 'q' the list navigation answers with — leave the
		// panel.
		Close: m.closePanel,
	})
}

// skillsPanel returns the /skills panel, building it if the host has
// not opened one yet.
func (m *runMode) skillsPanel() *skillsui.Panel {
	if m.panel.sk == nil {
		m.panel.sk = m.newSkillsPanel()
	}
	return m.panel.sk
}
