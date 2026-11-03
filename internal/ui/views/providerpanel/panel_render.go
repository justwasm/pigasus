// panel_render.go — the /provider panel's three views.
//
// The panel has three faces: the list of providers and models, the edit
// form, and the OAuth dialog. Only one of them is on screen at a time,
// and the panel picks by its own state.

package providerpanel

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/justwasm/pigasus/palette"
)

// Lines renders the panel for a terminal of the given height and optional width.
//
// The height decides how many rows the list draws; the width decides truncation
// and responsive compact layouts (following Bubbletea Golden Rules).
func (p *Panel) Lines(height int, width ...int) []string {
	if p == nil {
		return nil
	}
	w := 0
	if len(width) > 0 {
		w = width[0]
	}
	if p.oauth != nil {
		return p.oauthLines(w)
	}
	if p.form != nil {
		return p.formLines(w)
	}
	lines := []string{
		panelTitleOpen + "Providers" + panelTitleClose,
		p.TabBar(),
		"",
	}

	searchLine := "  Search: " + p.searchContent()
	if w > 4 {
		searchLine = ansi.Truncate(searchLine, w-2, "…")
	}
	lines = append(lines, searchLine, "          "+searchUnderline, "")

	// Onboarding hint if no custom providers configured
	if len(p.providers) == 0 && p.query == "" {
		onboarding := "  " + palette.SubtleOpen + "No custom providers configured. Press enter on a preset below or a to add." + panelGray238C
		if w > 4 {
			onboarding = ansi.Truncate(onboarding, w-2, "…")
		}
		lines = append(lines, onboarding, "")
	}

	rows := p.Rows()
	total := len(rows)
	visible := visibleRowsFor(height, total)

	if total == 0 {
		if p.query != "" {
			l1 := "  " + palette.SubtleOpen + "No matching providers or models in this tab." + panelGray238C
			l2 := "  " + palette.SubtleOpen + "Press Tab to search other tabs, or esc to clear." + panelGray238C
			if w > 4 {
				l1 = ansi.Truncate(l1, w-2, "…")
				l2 = ansi.Truncate(l2, w-2, "…")
			}
			lines = append(lines, l1, l2)
		} else {
			l1 := "  " + palette.SubtleOpen + "No providers in this tab." + panelGray238C
			if w > 4 {
				l1 = ansi.Truncate(l1, w-2, "…")
			}
			lines = append(lines, l1)
		}
	} else {
		if p.cursor >= total {
			p.cursor = total - 1
		}
		if p.cursor < 0 {
			p.cursor = 0
		}
		p.adjustWindow(p.cursor, visible, total)
		end := p.window + visible
		if end > total {
			end = total
		}
		for i := p.window; i < end; i++ {
			lines = append(lines, p.rowLine(rows[i], i == p.cursor, w))
		}
		if total > visible {
			lines = append(lines, palette.DimGrayOpen+fmt.Sprintf("  [%d-%d of %d items]", p.window+1, end, total)+palette.DimGrayClose)
		} else {
			lines = append(lines, "")
		}
	}

	if p.notice != "" {
		noticeLine := p.styledNotice(p.notice)
		if w > 4 {
			noticeLine = ansi.Truncate(noticeLine, w-2, "…")
		}
		lines = append(lines, noticeLine)
	}
	if line := p.VerifyLine(); line != "" {
		verifyLine := "  " + line
		if w > 4 {
			verifyLine = ansi.Truncate(verifyLine, w-2, "…")
		}
		lines = append(lines, verifyLine)
	}
	lines = append(lines, "")

	var hint string
	if w > 0 && w < 90 {
		hint = panelKeyboardHint("↑/↓", "Move", "enter", "Edit",
			"tab", "Tab", "ctrl+a", "Add", "ctrl+d", "Del", "esc", "Exit")
	} else if w >= 90 && w < 115 {
		hint = panelKeyboardHint("↑/↓", "Nav", "enter", "Edit",
			"tab", "Tab", "ctrl+a", "Add", "ctrl+d", "Del", "f5", "Test", "esc", "Exit")
	} else {
		hint = panelKeyboardHint("↑/↓", "Navigate", "enter", "Expand/Edit",
			"space/ctrl+e", "Toggle", "tab", "Switch Tab", "ctrl+a", "Add", "ctrl+m", "Add Model", "ctrl+d", "Delete", "f5", "Verify", "esc", "Exit")
	}
	if w > 0 {
		hint = ansi.Truncate(hint, w, "")
	}
	lines = append(lines, hint)
	return lines
}

// styledNotice renders the notice line: a warning for a failure, a tick
// for a save.
func (p *Panel) styledNotice(notice string) string {
	styled := "  " + palette.SubtleOpen + notice + panelGray238C
	if strings.Contains(notice, "again") || strings.HasPrefix(strings.ToLower(notice), "error") || strings.HasPrefix(strings.ToLower(notice), "failed") {
		styled = "  " + palette.WarningOpen + "⚠ " + palette.WarningClose + palette.SubtleOpen + notice + panelGray238C
	} else if strings.HasPrefix(notice, "saved ") || strings.HasPrefix(notice, "removed ") {
		styled = "  " + palette.AgentFgOpen + "✓ " + palette.ToolBulletClose + palette.SubtleOpen + notice + panelGray238C
	}
	return styled
}

// searchContent is the search box's content: the typed text in front of
// the reverse-video cursor cell, matching /config.
func (p *Panel) searchContent() string {
	if p == nil || p.query == "" {
		return searchCursorBlock
	}
	return p.query + searchCursorBlock
}

// rowLine renders one row of the list with optional width constraints.
func (p *Panel) rowLine(row Row, cursor bool, width ...int) string {
	w := 0
	if len(width) > 0 {
		w = width[0]
	}
	maxW := w - 2
	if maxW <= 0 {
		maxW = 0
	}
	switch row.Kind {
	case RowProvider:
		cfg := p.providers[row.Provider]
		marker := ""
		if len(cfg.EffectiveModels()) > 0 {
			if row.Expanded {
				marker = "▾ "
			} else {
				marker = "▸ "
			}
		}
		name := marker + row.Provider
		return RowText(name, ProviderRowValue(cfg), cursor, false, palette.BlueOpen, palette.BlueClose, maxW)
	case RowModel:
		cfg := p.providers[row.Provider]
		val := ModelRowValue(cfg, row.Model, w)
		if p.IsCurrentModel(row.Model) {
			val += "  " + palette.AgentFgOpen + "(current)" + palette.ToolBulletClose
		}
		return RowText(row.Model, val, cursor, true, palette.BlueOpen, palette.BlueClose, maxW)
	case RowBuiltin:
		if row.Model == "" {
			// The group's header.
			marker := ""
			if row.Expanded {
				marker = "▾ "
			} else {
				marker = "▸ "
			}
			line := "  " + palette.SubtleOpen + marker + "── " + row.Provider + " ──" + panelGray238C
			if maxW > 0 {
				return ansi.Truncate(line, maxW, "…")
			}
			return line
		}
		val := row.Label
		if p.IsCurrentModel(row.Model) {
			val += "  " + palette.AgentFgOpen + "(current)" + palette.ToolBulletClose
		}
		return RowText(row.Model, val, cursor, true, palette.BlueOpen, palette.BlueClose, maxW)
	case RowPreset:
		if row.Model == "" && row.Label == "" {
			// The group's header.
			line := "  " + palette.SubtleOpen + "── " + row.Provider + " ──" + panelGray238C
			if maxW > 0 {
				return ansi.Truncate(line, maxW, "…")
			}
			return line
		}
		return RowText(row.Provider, row.Label, cursor, true, palette.BlueOpen, palette.BlueClose, maxW)
	}
	return ""
}

// formLines renders the edit form.
func (p *Panel) formLines(width ...int) []string {
	form := p.form
	if form == nil {
		return nil
	}
	w := 0
	if len(width) > 0 {
		w = width[0]
	}
	lines := []string{
		panelTitleOpen + form.Title() + panelTitleClose,
		"  " + searchUnderline,
		"",
	}
	for i, f := range form.Fields {
		lines = append(lines, p.formFieldLine(f, i == form.Cursor, w))
	}
	if p.notice != "" {
		notice := p.notice
		styledNotice := "  " + palette.SubtleOpen + notice + panelGray238C
		if strings.HasPrefix(strings.ToLower(notice), "error") || strings.HasPrefix(strings.ToLower(notice), "failed") {
			styledNotice = "  " + palette.WarningOpen + "⚠ " + palette.WarningClose + palette.SubtleOpen + notice + panelGray238C
		}
		lines = append(lines, "", styledNotice)
	}
	if line := p.VerifyLine(); line != "" {
		lines = append(lines, "  "+line)
	}
	var hint string
	if form.Kind == FormProvider {
		if form.HasChoice() {
			if w > 0 && w < 85 {
				hint = panelKeyboardHint("↑/↓", "Field", "←/→", "Change", "enter", "Save", "esc", "Cancel")
			} else {
				hint = panelKeyboardHint("↑/↓", "Field", "←/→", "Change", "enter", "Save", "v", "Verify", "esc", "Cancel")
			}
		} else {
			hint = panelKeyboardHint("↑/↓", "Field", "enter", "Save", "v", "Verify", "esc", "Cancel")
		}
	} else {
		if form.HasChoice() {
			hint = panelKeyboardHint("↑/↓", "Field", "←/→", "Change", "enter", "Save", "esc", "Cancel")
		} else {
			hint = panelKeyboardHint("↑/↓", "Field", "enter", "Save", "esc", "Cancel")
		}
	}
	lines = append(lines, "", hint)
	return lines
}

// formFieldLine renders one field of the form.
func (p *Panel) formFieldLine(f FormField, cursor bool, width ...int) string {
	w := 0
	if len(width) > 0 {
		w = width[0]
	}
	pad := 20 - len(f.Label)
	if pad < 1 {
		pad = 1
	}
	value := f.Display()
	var res string
	if cursor {
		switch f.Kind {
		case FieldChoice:
			value = "‹ " + value + " ›  " + palette.DimGrayOpen + "(←/→ to cycle)" + palette.DimGrayClose
		case FieldText:
			value = p.input.View()
		}
		res = palette.BlueOpen + "> " + f.Label + palette.BlueClose + strings.Repeat(" ", pad) + value
	} else {
		if f.Kind == FieldChoice {
			value = "[ " + value + " ]"
		} else if strings.TrimSpace(value) == "" && f.Placeholder != "" {
			value = palette.DimGrayOpen + f.Placeholder + palette.DimGrayClose
		}
		res = "  " + f.Label + strings.Repeat(" ", pad) + value
	}
	if w > 2 {
		return ansi.Truncate(res, w-2, "…")
	}
	return res
}
