package battlepanel

import (
	"fmt"
	"strings"
	"time"

	"github.com/justwasm/pigasus/internal/app/battlemode"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/palette"
)

var spinnerFrames = []string{"⣾", "⣽", "⣻", "⢿", "⡿", "⣟", "⣯", "⣷"}

// View renders the Battle Mode overlay as a single ANSI-styled string.
// Returns "" when the overlay is inactive.
func (p *Panel) View() string {
	if p == nil || !p.active {
		return ""
	}
	return strings.Join(p.Lines(), "\n")
}

// Lines renders the Battle Mode overlay into terminal rows.
func (p *Panel) Lines() []string {
	if p == nil || !p.active {
		return nil
	}
	w, h := p.effectiveSize()

	header := p.renderHeader(w)
	tabs := p.renderArmTabs(w)
	meta := p.renderMetaBar(w)
	notice := p.renderNoticeBar(w)
	sep := palette.DimGrayOpen + strings.Repeat("─", w) + palette.DimGrayClose

	topLines := make([]string, 0, 2+len(tabs)+3)
	topLines = append(topLines, header)
	topLines = append(topLines, tabs...)
	if meta != "" {
		topLines = append(topLines, meta)
	}
	if notice != "" {
		topLines = append(topLines, notice)
	}
	topLines = append(topLines, sep)

	footer := p.renderFooter(w)
	footerLines := []string{sep, footer}

	bodyHeight := h - len(topLines) - len(footerLines)
	if bodyHeight < 4 {
		bodyHeight = 4
	}

	body := p.bodyLines(w)
	maxScroll := len(body) - bodyHeight
	if maxScroll < 0 {
		maxScroll = 0
	}
	scroll := p.scroll
	if scroll < 0 {
		scroll = 0
	}
	if scroll > maxScroll {
		scroll = maxScroll
	}

	end := scroll + bodyHeight
	if end > len(body) {
		end = len(body)
	}
	visibleBody := body[scroll:end]

	out := make([]string, 0, len(topLines)+len(visibleBody)+len(footerLines)+1)
	out = append(out, topLines...)
	out = append(out, visibleBody...)
	if len(body) > bodyHeight {
		indicator := fmt.Sprintf(" %s[lines %d-%d of %d]%s",
			palette.DimGrayOpen, scroll+1, end, len(body), palette.DimGrayClose)
		out = append(out, indicator)
	}
	out = append(out, footerLines...)
	return out
}

func (p *Panel) maxScroll() int {
	w, h := p.effectiveSize()
	tabs := p.renderArmTabs(w)
	topCount := 1 + len(tabs) + 1 // header + tabs + sep
	if p.renderMetaBar(w) != "" {
		topCount++
	}
	if p.renderNoticeBar(w) != "" {
		topCount++
	}
	bodyHeight := h - topCount - 2
	if bodyHeight < 4 {
		bodyHeight = 4
	}
	body := p.bodyLines(w)
	max := len(body) - bodyHeight
	if max < 0 {
		return 0
	}
	return max
}

func (p *Panel) renderHeader(w int) string {
	badge := palette.DimGrayOpen + "○ READY" + palette.DimGrayClose
	switch {
	case p.snapshot.WinnerIndex >= 0 && p.snapshot.WinnerIndex < len(p.snapshot.Arms):
		winName := p.snapshot.Arms[p.snapshot.WinnerIndex].Spec.DisplayName()
		badge = palette.AgentDoneOpen + "★ WINNER: " + winName + palette.BlueClose
	case p.snapshot.Running:
		frame := spinnerFrames[p.spinnerFrame%len(spinnerFrames)]
		badge = palette.AgentActiveOpen + frame + " RUNNING" + palette.BlueClose
	case len(p.snapshot.Arms) > 0:
		badge = palette.AgentDoneOpen + "✓ COMPLETE" + palette.BlueClose
	}

	task := strings.TrimSpace(p.snapshot.Task)
	if task == "" {
		task = "(no task)"
	}
	line := fmt.Sprintf(" %s⚔ Battle Mode%s  %s  %s%s%s",
		palette.BlueBoldOpen, palette.BlueBoldClose,
		badge,
		palette.SubtleOpen, task, palette.DimGrayClose)
	return tuilib.FitWidth(line, w)
}

func (p *Panel) renderArmTabs(w int) []string {
	if len(p.snapshot.Arms) == 0 {
		return []string{" " + palette.DimGrayOpen + "(no arms configured)" + palette.DimGrayClose}
	}

	var rows []string
	var cur strings.Builder
	cur.WriteString(" ")
	curLen := 1

	for i, arm := range p.snapshot.Arms {
		tabPlain, tabStyled := p.formatArmTab(i, arm, i == p.activeArm)
		sep := ""
		sepLen := 0
		if curLen > 1 {
			sep = "  "
			sepLen = 2
		}
		if curLen > 1 && curLen+sepLen+len([]rune(tabPlain)) > w {
			rows = append(rows, cur.String())
			cur.Reset()
			cur.WriteString(" ")
			curLen = 1
			sep = ""
			sepLen = 0
		}
		cur.WriteString(sep)
		cur.WriteString(tabStyled)
		curLen += sepLen + len([]rune(tabPlain))
	}
	if curLen > 1 {
		rows = append(rows, cur.String())
	}
	return rows
}

func (p *Panel) formatArmTab(index int, arm battlemode.ArmState, active bool) (plain string, styled string) {
	name := arm.Spec.DisplayName()
	glyph, glyphColor, suffix := p.armStatusBadge(arm)
	plain = fmt.Sprintf("[%d] %s %s %s", index+1, name, glyph, suffix)

	var b strings.Builder
	if active {
		b.WriteString(palette.BlueBoldOpen)
		b.WriteString(fmt.Sprintf("[%d] %s", index+1, name))
		b.WriteString(palette.BlueBoldClose)
	} else {
		b.WriteString(palette.DimGrayOpen)
		b.WriteString(fmt.Sprintf("[%d] %s", index+1, name))
		b.WriteString(palette.DimGrayClose)
	}
	b.WriteString(" ")
	b.WriteString(glyphColor)
	b.WriteString(glyph)
	b.WriteString("\x1b[39m ")
	b.WriteString(palette.DimGrayOpen)
	b.WriteString(suffix)
	b.WriteString(palette.DimGrayClose)
	return plain, b.String()
}

func (p *Panel) armStatusBadge(arm battlemode.ArmState) (glyph, color, suffix string) {
	if arm.Winner {
		return "★", palette.AgentDoneOpen, formatDuration(arm.Duration)
	}
	switch arm.Status {
	case battlemode.StatusRunning:
		frame := spinnerFrames[p.spinnerFrame%len(spinnerFrames)]
		return frame, palette.AgentActiveOpen, "..."
	case battlemode.StatusDone:
		return "✓", palette.AgentDoneOpen, formatDuration(arm.Duration)
	case battlemode.StatusError:
		return "✗", palette.AgentErrorOpen, formatDuration(arm.Duration)
	case battlemode.StatusEliminated:
		return "⊘", palette.DimGrayOpen, "eliminated"
	default:
		return "○", palette.DimGrayOpen, "pending"
	}
}

func (p *Panel) renderMetaBar(w int) string {
	if len(p.snapshot.Arms) == 0 || p.activeArm < 0 || p.activeArm >= len(p.snapshot.Arms) {
		return ""
	}
	arm := p.snapshot.Arms[p.activeArm]
	parts := []string{
		fmt.Sprintf("Model: %s", arm.Spec.DisplayName()),
		fmt.Sprintf("Status: %s", arm.Status),
	}
	if arm.Duration > 0 {
		parts = append(parts, fmt.Sprintf("Time: %s", formatDuration(arm.Duration)))
	}
	if arm.Usage.TotalTokens > 0 || arm.Usage.InputTokens > 0 || arm.Usage.OutputTokens > 0 {
		total := arm.Usage.TotalTokens
		if total == 0 {
			total = arm.Usage.InputTokens + arm.Usage.OutputTokens
		}
		parts = append(parts, fmt.Sprintf("Tokens: %d (%d in / %d out)", total, arm.Usage.InputTokens, arm.Usage.OutputTokens))
	}
	parts = append(parts, fmt.Sprintf("Files: %d", len(arm.ChangedFiles)))
	if arm.Winner {
		parts = append(parts, palette.AgentDoneOpen+"[WINNER]"+palette.DimGrayClose)
	}
	line := " " + palette.DimGrayOpen + strings.Join(parts, " · ") + palette.DimGrayClose
	return tuilib.FitWidth(line, w)
}

func (p *Panel) renderNoticeBar(w int) string {
	if strings.TrimSpace(p.statusNotice) == "" {
		return ""
	}
	line := " " + palette.BlueOpen + p.statusNotice + palette.BlueClose
	return tuilib.FitWidth(line, w)
}

func (p *Panel) bodyLines(w int) []string {
	if len(p.snapshot.Arms) == 0 || p.activeArm < 0 || p.activeArm >= len(p.snapshot.Arms) {
		return []string{" " + palette.DimGrayOpen + "No battle arms available." + palette.DimGrayClose}
	}
	arm := p.snapshot.Arms[p.activeArm]
	var lines []string

	// Response summary pane
	lines = append(lines, " "+palette.BlueBoldOpen+"Response"+palette.BlueBoldClose)
	if arm.Error != "" {
		errLines := tuilib.WrapRows("Error: "+arm.Error, maxInt(20, w-2))
		for _, el := range errLines {
			lines = append(lines, " "+palette.AgentErrorOpen+el+"\x1b[39m")
		}
	}
	resp := strings.TrimSpace(arm.Response)
	if resp == "" {
		if arm.Status == battlemode.StatusRunning {
			lines = append(lines, " "+palette.DimGrayOpen+"(running...)"+palette.DimGrayClose)
		} else if arm.Error == "" {
			lines = append(lines, " "+palette.DimGrayOpen+"(no response)"+palette.DimGrayClose)
		}
	} else {
		for _, raw := range strings.Split(resp, "\n") {
			wrapped := tuilib.WrapRows(raw, maxInt(20, w-2))
			if len(wrapped) == 0 {
				lines = append(lines, "")
				continue
			}
			for _, wl := range wrapped {
				lines = append(lines, " "+wl)
			}
		}
	}

	lines = append(lines, "")

	// Unified diff preview pane
	diffHeader := " " + palette.BlueBoldOpen + "Diff" + palette.BlueBoldClose
	if len(arm.ChangedFiles) > 0 {
		diffHeader += " " + palette.DimGrayOpen + "(" + strings.Join(arm.ChangedFiles, ", ") + ")" + palette.DimGrayClose
	}
	lines = append(lines, diffHeader)

	diffText := strings.TrimRight(arm.Diff, "\n")
	if diffText == "" {
		lines = append(lines, " "+palette.DimGrayOpen+"(no file changes)"+palette.DimGrayClose)
	} else {
		for _, raw := range strings.Split(diffText, "\n") {
			lines = append(lines, " "+styleDiffLine(raw))
		}
	}

	return lines
}

func styleDiffLine(line string) string {
	switch {
	case strings.HasPrefix(line, "diff --git "),
		strings.HasPrefix(line, "index "),
		strings.HasPrefix(line, "--- "),
		strings.HasPrefix(line, "+++ "),
		strings.HasPrefix(line, "new file mode "),
		strings.HasPrefix(line, "deleted file mode "):
		return palette.DimGrayOpen + line + palette.DimGrayClose
	case strings.HasPrefix(line, "@@"):
		return palette.BlueOpen + line + palette.BlueClose
	case strings.HasPrefix(line, "+"):
		return palette.ToolAddOpen + line + palette.ToolAddClose
	case strings.HasPrefix(line, "-"):
		return palette.ToolRemoveOpen + line + palette.ToolRemoveClose
	default:
		return line
	}
}

func (p *Panel) renderFooter(w int) string {
	hint := tuilib.PanelDotHint(" ",
		"Tab/←/→/1-9", "switch arm",
		"↑/↓/PgUp/PgDn", "scroll",
		"w/Enter", "pick winner",
		"x", "eliminate",
		"esc", "close",
	)
	return tuilib.FitWidth(hint, w)
}

func formatDuration(d time.Duration) string {
	if d <= 0 {
		return "0.0s"
	}
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
