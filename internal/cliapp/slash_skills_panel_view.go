// slash_skills_panel_view.go — UI panel rendering and navigation for the /skills picker.

package cliapp

import (
	"fmt"
	"os"
	"path/filepath"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/skillsui"
	"github.com/justwasm/pigasus/internal/tuilib"
)

func verbatimSkillArgs(args string) string {
	return skillsui.VerbatimSkillArgs(args)
}

func skillsPanelRow(s jsonSkillInfo, cursor bool, width int) string {
	return skillsui.PanelRow(s, cursor, width)
}

func skillsPanelMarketplaceLink(s jsonSkillInfo, osc8Supported bool) string {
	return skillsui.MarketplaceLink(s, osc8Supported)
}

func (m *runMode) renderSkillsPanelItems(model skillsPanelModel, start, end, cursor, width int) []string {
	var lines []string
	first := true
	for i := start; i < end; i++ {
		it := model.Items[i]
		if it.Header {
			if !first {
				lines = append(lines, "")
			}
			g := model.Groups[it.Group]
			lines = append(lines, "\x1b[1m"+g.Left+" · "+panelGray238+g.Label+"\x1b[0m")
		} else {
			s := model.Skills[it.Skill]
			lines = append(lines, skillsPanelRow(s, it.Skill == cursor, width))
			if link := skillsPanelMarketplaceLink(s, m.hyperlinkCaps.OSC8); link != "" {
				lines = append(lines, link)
			}
		}
		first = false
	}
	return lines
}

func (m *runMode) skillsPanelLines() []string {
	model := newSkillsPanelModel()
	width := m.width
	if width <= 0 {
		width = skillsPanelDefaultWidth
	}
	lines := skillsui.HeaderLines(len(model.Skills))
	if len(model.Items) == 0 {
		lines = append(lines, "", panelKeyboardHint("↑/↓", "Navigate", "enter", "View", "esc", "Cancel"))
		return lines
	}

	cursor := m.panel.cursor
	if cursor < -1 || cursor >= len(model.Skills) {
		cursor = -1
	}
	n := skillsui.VisibleItemsFor(m.height)
	if n > len(model.Items) {
		n = len(model.Items)
	}
	start := 0
	if cursor >= 0 {
		start = model.SkillItem[cursor] - (n - 1)
		if start < 0 {
			start = 0
		}
	}
	end := start + n
	if end > len(model.Items) {
		end = len(model.Items)
	}

	lines = append(lines, m.renderSkillsPanelItems(model, start, end, cursor, width)...)
	if n < len(model.Items) {
		lines = append(lines, cDimGrayOpen+
			fmt.Sprintf("  [%d-%d of %d items]", start+1, end, len(model.Items))+
			cDimGrayClose)
	}
	lines = append(lines, "", panelKeyboardHint("↑/↓", "Navigate", "enter", "View", "esc", "Cancel"))
	return lines
}

func (m *runMode) moveSkillsCursor(down bool) {
	n := len(skillsPanelCatalog())
	if n == 0 {
		m.panel.cursor = -1
		return
	}
	if down {
		if m.panel.cursor < n-1 {
			m.panel.cursor++
		}
		return
	}
	if m.panel.cursor > 0 {
		m.panel.cursor--
	}
}

func (m *runMode) skillsPanelDetailLines() []string {
	if m.skillsDetailSelected().Path == "" {
		return m.skillsDetailLegacyLines()
	}
	return m.skillsDetailViewLines()
}

func (m *runMode) skillsDetailLegacyLines() []string {
	s := m.skillsDetailSelected()
	return []string{
		panelTitleOpen + "Skills" + panelTitleClose,
		"",
		"  " + "\x1b[1m" + cBlueOpen + skillsui.DisplayName(s) + "\x1b[0m",
		"",
		"  " + panelGray238 + s.Description + panelGray238C,
		"",
		"  " + panelGray238 + "Path: " + s.Path + panelGray238C,
		"",
		panelKeyboardHint("esc", "Back"),
	}
}

// skillsFromDisk enumerates ~/.gemini/skills/*
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

// panelSkillsLines renders the /skills picker.
func (m *runMode) panelSkillsLines() []string {
	if m.panel.detail {
		return m.panelSkillDetailLines()
	}
	return m.skillsPanelLines()
}

// panelSkillDetailLines renders the per-skill view /skills' `enter View` opens.
func (m *runMode) panelSkillDetailLines() []string {
	return m.skillsPanelDetailLines()
}

// openSkillDetail switches /skills into its per-skill detail view.
func (m *runMode) openSkillDetail() {
	if m.panel.cursor < 0 || m.panel.cursor >= len(skillsPanelCatalog()) {
		return
	}
	m.panel.detail = true
}

// Palette. Every constant below was read off a REF capture at 220×50.
// The values live in internal/skillsui — the markdown renderer that
// emits them is there — and are aliased here so the panel's own
// vocabulary (and its tests) keep working.
const (
	mdText         = skillsui.MDText
	mdTextClose    = skillsui.MDTextClose
	mdCodeOpen     = skillsui.MDCodeOpen
	mdBold         = skillsui.MDBold
	mdBoldRestore  = skillsui.MDBoldRestore
	mdCodeClose    = skillsui.MDCodeClose
	mdCodeCloseEnd = skillsui.MDCodeCloseEnd
	mdReset        = skillsui.MDReset
	mdH1Open       = skillsui.MDH1Open
	mdH2Open       = skillsui.MDH2Open
	mdLinkOpen     = skillsui.MDLinkOpen
	mdHROpen       = skillsui.MDHROpen
	mdHRText       = skillsui.MDHRText
	mdTableBorder  = skillsui.MDTableBorder
)

// skillsDetailLine is one logical document line.
//
// The shape lives in internal/skillsui; this alias keeps the panel's
// own vocabulary.
type skillsDetailLine = skillsui.DetailLine

// skillsDetailDocument is the viewer's logical-line sequence.
func (m *runMode) skillsDetailDocument(s jsonSkillInfo) []skillsDetailLine {
	return skillsui.SkillsDetailDocument(skillsui.DisplayName(s), s.Description, s.Path, m.width)
}

// skillsDetailVisible is the window height in visual rows the REF /skills-detail viewer fits.
func skillsDetailVisible(height int) int {
	return skillsui.SkillsDetailVisible(height)
}

// skillsDetailViewLines renders the open detail sub-view.
func (m *runMode) skillsDetailViewLines() []string {
	cat := skillsPanelCatalog()
	var s jsonSkillInfo
	if m.panel.cursor >= 0 && m.panel.cursor < len(cat) {
		s = cat[m.panel.cursor]
	}
	doc := m.skillsDetailDocument(s)
	vis := skillsDetailVisible(m.height)
	total := len(doc) + 1
	maxOffset := total - vis
	if maxOffset < 0 {
		maxOffset = 0
	}
	if m.panel.window < 0 {
		m.panel.window = 0
	}
	if m.panel.window > maxOffset {
		m.panel.window = maxOffset
	}
	hi := m.panel.window + vis
	if hi > len(doc) {
		hi = len(doc)
	}
	var out []string
	for _, l := range doc[m.panel.window:hi] {
		out = append(out, l.Rows...)
	}
	if total > vis {
		out = append(out, viewerCounter(m.panel.window+1, m.panel.window+vis, total))
	} else {
		out = append(out, "")
	}
	out = append(out, "",
		panelDotHint("  ", "↑/↓", "scroll", "pgup/pgdown", "page",
			"ctrl+end", "bottom", "ctrl+home", "top", "esc", "back"))
	return out
}

// skillsDetailSelected is the catalogue entry the detail view is bound to.
func (m *runMode) skillsDetailSelected() jsonSkillInfo {
	cat := skillsPanelCatalog()
	if m.panel.cursor < 0 || m.panel.cursor >= len(cat) {
		return jsonSkillInfo{}
	}
	return cat[m.panel.cursor]
}

// handleSkillsDetailKey drives the open /skills detail viewer.
func (m *runMode) handleSkillsDetailKey(key tea.Key) (bool, tea.Cmd) {
	vis := skillsDetailVisible(m.height)
	page := max(1, vis-1)
	total := len(m.skillsDetailDocument(m.skillsDetailSelected())) + 1
	maxOffset := max(0, total-vis)

	action := tuilib.ClassifyViewerNavKey(key)
	if action == tuilib.NavCancel {
		m.panel.detail = false
		return true, nil
	}
	if m.applySkillsDetailNav(action, page, maxOffset) {
		return true, nil
	}
	return key.Mod == 0, nil
}

func (m *runMode) applySkillsDetailNav(action tuilib.NavAction, page, maxOffset int) bool {
	half := max(1, page/2)
	switch action {
	case tuilib.NavUp:
		m.panel.window = clampSkillsWindow(m.panel.window-1, maxOffset)
	case tuilib.NavDown:
		m.panel.window = clampSkillsWindow(m.panel.window+1, maxOffset)
	case tuilib.NavHalfPageUp:
		m.panel.window = clampSkillsWindow(m.panel.window-half, maxOffset)
	case tuilib.NavHalfPageDown:
		m.panel.window = clampSkillsWindow(m.panel.window+half, maxOffset)
	case tuilib.NavPageUp:
		m.panel.window = clampSkillsWindow(m.panel.window-page, maxOffset)
	case tuilib.NavPageDown:
		m.panel.window = clampSkillsWindow(m.panel.window+page, maxOffset)
	case tuilib.NavTop:
		m.panel.window = 0
	case tuilib.NavBottom:
		m.panel.window = maxOffset
	default:
		return false
	}
	return true
}

func clampSkillsWindow(val, maxOffset int) int {
	return skillsui.ClampSkillsWindow(val, maxOffset)
}
