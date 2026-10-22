package main

import (
	"os"
	"path/filepath"
)

// skillsFromDisk enumerates ~/.gemini/skills/* (the same root that
// printSlashSkillInfos reads in print_slash.go). Cycle 193 ships the
// directory walk only — the SKILL.md frontmatter parse is in a follow-up.
//
// Returns nil when the directory does not exist so the panel can
// render an empty-state row instead of crashing.
func skillsFromDisk() []struct {
	name string
	desc string
} {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	dir := filepath.Join(home, ".gemini", "skills")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	out := make([]struct {
		name string
		desc string
	}, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		out = append(out, struct {
			name string
			desc string
		}{
			name: e.Name(),
			desc: "Skill installed under ~/.gemini/skills/" + e.Name(),
		})
	}
	return out
}

// panelSkillsLines renders the /skills picker. Cycle 203 rebuilds it on the
// real catalogue and the reference's recovered layout — the whole
// implementation lives in skillsPanelLines (slash_skills.go), which this
// delegates to. `enter View` opens the per-skill detail (cycle 201) when a
// row is selected. See the block comment at the top of the cycle-203
// section in slash_skills.go for the evidence.
func (m *runMode) panelSkillsLines() []string {
	if m.panel.detail {
		return m.panelSkillDetailLines()
	}
	return m.skillsPanelLines()
}

// panelSkillDetailLines renders the per-skill view /skills' `enter View`
// opens. Cycle 203 backs it with the *selected catalogue entry* (name,
// description and SKILL.md path) via skillsPanelDetailLines in
// slash_skills.go; REF's real screen is a full SKILL.md document viewer
// that this cycle does not reproduce (see that function's comment).
func (m *runMode) panelSkillDetailLines() []string {
	return m.skillsPanelDetailLines()
}

// openSkillDetail switches /skills into its per-skill detail view when the
// cursor is on an enumerated skill. Cycle 203: the cursor indexes the whole
// catalogue and -1 means "no selection", in which case Enter is a no-op
// (the reference's ppref behaviour — see caps/ref.enter.raw.txt).
func (m *runMode) openSkillDetail() {
	if m.panel.cursor < 0 || m.panel.cursor >= len(skillsPanelCatalog()) {
		return
	}
	m.panel.detail = true
}

