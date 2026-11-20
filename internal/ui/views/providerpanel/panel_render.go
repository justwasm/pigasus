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
	"github.com/justwasm/pigasus/internal/features/models"
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

	var lines []string

	// Header line 1
	header := "  " + ansiBold + "Providers" + ansiReset
	lines = append(lines, header)

	// Tab bar line 2
	lines = append(lines, p.TabBar(), "")

	// Search line 3
	var searchLine string
	if p.query == "" {
		searchLine = palette.DimGrayOpen + "  Type to search providers and models..." + palette.DimGrayClose
	} else {
		searchLine = palette.DimGrayOpen + "  Search: " + palette.DimGrayClose + p.query + searchCursorBlock
	}
	if w > 4 {
		searchLine = ansi.Truncate(searchLine, w-2, "…")
	}
	lines = append(lines, searchLine, "")

	// Top action row in Configured and All tabs (when query is empty)
	if p.tab != int(TabPresets) && p.query == "" {
		if p.cursor == -1 {
			lines = append(lines, palette.BlueOpen+"> "+ansiReset+ansiBold+"+ Add a provider..."+ansiReset)
		} else {
			lines = append(lines, "  "+ansiBold+"+ Add a provider..."+ansiReset)
		}
	}

	// Onboarding hint if no custom providers configured
	if len(p.providers) == 0 && p.query == "" && p.tab == int(TabConfigured) {
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
	if p.cursor == -1 {
		escDesc := "Close"
		if p.query != "" {
			escDesc = "Clear"
		}
		hint = panelKeyboardHint("↑/↓", "Navigate", "enter", "Add Provider", "tab", "Switch Tab", "esc", escDesc)
	} else if p.confirm >= 0 {
		hint = panelKeyboardHint("y/d", "Confirm Delete", "n/esc", "Cancel")
	} else if w > 0 && w < 90 {
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

	lead := "  "
	if cursor {
		lead = palette.BlueOpen + "> " + ansiReset
	}

	switch row.Kind {
	case RowProvider:
		cfg := p.providers[row.Provider]
		isCurrent := p.isProviderCurrent(row.Provider)

		bullet := "\x1b[38;5;238m●\x1b[39m"
		if isCurrent {
			bullet = palette.AgentFgOpen + "●" + palette.AgentFgClose
		}

		marker := ""
		if len(cfg.EffectiveModels()) > 0 {
			if row.Expanded {
				marker = "▾ "
			} else {
				marker = "▸ "
			}
		}

		nameStr := ansiBold + marker + row.Provider + ansiReset
		if cursor {
			nameStr = ansiBold + palette.BlueOpen + marker + row.Provider + ansiReset
		}

		wire := cfg.WireType()
		endpoint := Endpoint(cfg)
		modelsNote := ""
		if n := len(cfg.EffectiveModels()); n == 1 {
			modelsNote = " (1 model)"
		} else if n > 1 {
			modelsNote = fmt.Sprintf(" (%d models)", n)
		}
		meta := palette.DimGrayOpen + fmt.Sprintf("%s · %s%s", wire, endpoint, modelsNote) + palette.DimGrayClose

		currentBadge := ""
		if isCurrent {
			currentBadge = "  " + palette.AgentFgOpen + "(current)" + palette.ToolBulletClose
		}

		confirmBadge := ""
		if p.confirm >= 0 && p.cursor >= 0 && p.cursor < len(p.Rows()) && p.Rows()[p.cursor].Provider == row.Provider && p.Rows()[p.cursor].Kind == RowProvider {
			confirmBadge = " \x1b[38;5;214m[Delete provider " + row.Provider + "? (y/n)]\x1b[39m"
		}

		var line string
		if confirmBadge != "" {
			line = lead + bullet + " " + nameStr + currentBadge + confirmBadge
		} else {
			line = lead + bullet + " " + nameStr + "  " + meta + currentBadge
		}
		if maxW > 0 {
			return ansi.Truncate(line, maxW, "…")
		}
		return line

	case RowModel:
		cfg := p.providers[row.Provider]
		isCurrent := p.IsCurrentModel(row.Model)

		bullet := "\x1b[38;5;238m○\x1b[39m"
		if isCurrent {
			bullet = palette.AgentFgOpen + "○" + palette.AgentFgClose
		}

		nameStr := row.Model
		if cursor {
			nameStr = ansiBold + palette.BlueOpen + row.Model + ansiReset
		}

		val := ModelRowValue(cfg, row.Model, w)
		currentBadge := ""
		if isCurrent {
			currentBadge = "  " + palette.AgentFgOpen + "(current)" + palette.ToolBulletClose
		}

		confirmBadge := ""
		if p.confirm >= 0 && p.cursor >= 0 && p.cursor < len(p.Rows()) && p.Rows()[p.cursor].Model == row.Model && p.Rows()[p.cursor].Kind == RowModel {
			confirmBadge = " \x1b[38;5;214m[Delete model " + row.Model + "? (y/n)]\x1b[39m"
		}

		childLead := "    "
		if cursor {
			childLead = "  " + palette.BlueOpen + "> " + ansiReset
		}

		var line string
		if confirmBadge != "" {
			line = childLead + bullet + " " + nameStr + currentBadge + confirmBadge
		} else {
			line = childLead + bullet + " " + nameStr + "  " + palette.DimGrayOpen + val + palette.DimGrayClose + currentBadge
		}
		if maxW > 0 {
			return ansi.Truncate(line, maxW, "…")
		}
		return line

	case RowBuiltin:
		if row.Model == "" {
			bullet := "\x1b[38;5;238m●\x1b[39m"
			marker := ""
			if row.Expanded {
				marker = "▾ "
			} else {
				marker = "▸ "
			}
			nameStr := ansiBold + marker + row.Provider + ansiReset
			if cursor {
				nameStr = ansiBold + palette.BlueOpen + marker + row.Provider + ansiReset
			}
			line := lead + bullet + " " + nameStr
			if maxW > 0 {
				return ansi.Truncate(line, maxW, "…")
			}
			return line
		}

		isCurrent := p.IsCurrentModel(row.Model)
		bullet := "\x1b[38;5;238m○\x1b[39m"
		if isCurrent {
			bullet = palette.AgentFgOpen + "○" + palette.AgentFgClose
		}
		nameStr := row.Model
		if cursor {
			nameStr = ansiBold + palette.BlueOpen + row.Model + ansiReset
		}
		currentBadge := ""
		if isCurrent {
			currentBadge = "  " + palette.AgentFgOpen + "(current)" + palette.ToolBulletClose
		}
		childLead := "    "
		if cursor {
			childLead = "  " + palette.BlueOpen + "> " + ansiReset
		}
		line := childLead + bullet + " " + nameStr + "  " + palette.DimGrayOpen + row.Label + palette.DimGrayClose + currentBadge
		if maxW > 0 {
			return ansi.Truncate(line, maxW, "…")
		}
		return line

	case RowPreset:
		if row.Model == "" && row.Label == "" {
			bullet := "\x1b[38;5;238m●\x1b[39m"
			nameStr := ansiBold + row.Provider + ansiReset
			if cursor {
				nameStr = ansiBold + palette.BlueOpen + row.Provider + ansiReset
			}
			line := lead + bullet + " " + nameStr
			if maxW > 0 {
				return ansi.Truncate(line, maxW, "…")
			}
			return line
		}
		bullet := "\x1b[38;5;238m●\x1b[39m"
		nameStr := ansiBold + row.Provider + ansiReset
		if cursor {
			nameStr = ansiBold + palette.BlueOpen + row.Provider + ansiReset
		}
		authMethod := models.CatwalkAuthMethod(row.Provider)
		authLabel := "API Key"
		if authMethod == "browser" {
			authLabel = "Browser OAuth"
		}
		meta := palette.DimGrayOpen + fmt.Sprintf("%s · %s", row.Label, authLabel) + palette.DimGrayClose
		line := lead + bullet + " " + nameStr + "  " + meta
		if maxW > 0 {
			return ansi.Truncate(line, maxW, "…")
		}
		return line
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
		"  " + ansiBold + "Providers" + ansiReset + "\x1b[38;5;238m › \x1b[1m" + form.Title() + ansiReset,
		"",
	}
	for i, f := range form.Fields {
		fl := p.formFieldLine(f, i == form.Cursor, w)
		lines = append(lines, strings.Split(fl, "\n")...)
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
	value := f.Display()
	var res string
	if cursor {
		switch f.Kind {
		case FieldChoice:
			value = "‹ " + value + " ›  " + palette.DimGrayOpen + "(←/→ to cycle)" + palette.DimGrayClose
		case FieldText:
			value = p.input.View()
		}
		res = palette.BlueOpen + "> " + ansiReset + ansiBold + f.Label + ansiReset + "\n    \x1b[38;5;238m└─ \x1b[39m" + value
	} else {
		if f.Kind == FieldChoice {
			value = "[ " + value + " ]"
		} else if strings.TrimSpace(value) == "" && f.Placeholder != "" {
			value = palette.DimGrayOpen + f.Placeholder + palette.DimGrayClose
		}
		res = "  " + ansiBold + f.Label + ansiReset + "\n    \x1b[38;5;238m└─ \x1b[39m" + value
	}
	if w > 2 {
		parts := strings.Split(res, "\n")
		for i, part := range parts {
			parts[i] = ansi.Truncate(part, w-2, "…")
		}
		return strings.Join(parts, "\n")
	}
	return res
}
