package skillsui

import (
	"fmt"

	"github.com/justwasm/pigasus/internal/palette"
	"github.com/justwasm/pigasus/internal/pluginsui"
)

// DetailLine is one logical document line.
type DetailLine struct {
	Rows []string
}

// SkillsDetailDocument builds the logical-line sequence for viewing a skill's details.
func SkillsDetailDocument(name, description, path string, width int) []DetailLine {
	var doc []DetailLine
	if name != "" {
		doc = append(doc, DetailLine{Rows: []string{"\x1b[1m\x1b[39m" + name + "\x1b[0m"}})
	}
	if description != "" {
		w := width
		if w <= 0 {
			w = SkillsPanelDefaultWidth
		}
		var rows []string
		for _, r := range pluginsui.WrapWords(description, w-1) {
			rows = append(rows, PanelGray238+r+PanelGray238C)
		}
		doc = append(doc, DetailLine{Rows: rows})
	}
	if path != "" {
		doc = append(doc, DetailLine{Rows: []string{palette.DimGrayOpen + path + palette.DimGrayClose}})
	}
	doc = append(doc, DetailLine{Rows: []string{""}})
	doc = append(doc, MDSkillBody(path)...)
	return doc
}

// SkillsDetailVisible calculates how many rows are visible in the viewer for height.
func SkillsDetailVisible(height int) int {
	if height <= 0 {
		return SkillsPanelDefaultHeight*3/4 - 11
	}
	n := height*3/4 - 11
	if n < 5 {
		n = 5
	}
	return n
}

// SkillsDetailCounter returns the footer counter string for current scroll window.
func SkillsDetailCounter(window, vis, total int) string {
	if total <= vis {
		return ""
	}
	start := window + 1
	end := window + vis
	if end > total {
		end = total
	}
	return fmt.Sprintf("(%d–%d of %d lines)", start, end, total)
}

// ClampSkillsWindow bounds the window offset between 0 and maxOffset.
func ClampSkillsWindow(val, maxOffset int) int {
	if val < 0 {
		return 0
	}
	if val > maxOffset {
		return maxOffset
	}
	return val
}
