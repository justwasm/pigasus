package permissionprompt

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/justwasm/pigasus/palette"
)

const (
	// DefaultBoxWidth is the standard width for the permission approval dialog box.
	DefaultBoxWidth = 62
	// MinBoxWidth is the minimum width required to display options on a single row.
	MinBoxWidth = 62
)

// RenderPlain produces the unstyled ASCII / Unicode box representation.
func (m *Model) RenderPlain(width int) string {
	return strings.Join(m.RenderPlainLines(width), "\n")
}

// RenderPlainLines returns the box rendered row by row without ANSI escape codes.
func (m *Model) RenderPlainLines(width int) []string {
	if width < MinBoxWidth {
		width = MinBoxWidth
	}
	innerWidth := width - 4 // inner text width between "│ " and " │"

	var lines []string

	// Top border: ┌─ <Title> ───...──┐
	title := m.req.TitleStr()
	titleLen := ansi.StringWidth(title)
	dashes := width - (titleLen + 5)
	if dashes < 1 {
		dashes = 1
	}
	top := fmt.Sprintf("┌─ %s %s┐", title, strings.Repeat("─", dashes))
	lines = append(lines, top)

	// Body content lines
	contentRows := m.plainBodyRows(innerWidth)
	for _, row := range contentRows {
		visLen := ansi.StringWidth(row)
		pad := innerWidth - visLen
		if pad < 0 {
			pad = 0
		}
		lines = append(lines, fmt.Sprintf("│ %s%s │", row, strings.Repeat(" ", pad)))
	}

	// Bottom border: └──────...─────┘
	bottom := fmt.Sprintf("└%s┘", strings.Repeat("─", width-2))
	lines = append(lines, bottom)

	return lines
}

func (m *Model) plainBodyRows(innerWidth int) []string {
	var rows []string

	if m.req.Kind == KindFileEdit || m.req.Kind == KindFileCreate {
		if path := m.req.PathStr(); path != "" {
			rows = append(rows, fmt.Sprintf("File: %s", truncateText(path, innerWidth-6)))
		}
		if m.req.OutsideWS {
			rows = append(rows, "Reason: outside workspace")
		} else if desc := m.req.Description; desc != "" {
			rows = append(rows, fmt.Sprintf("Reason: %s", truncateText(desc, innerWidth-8)))
		}
		if q := m.req.PromptQuestionStr(); q != "" {
			rows = append(rows, q)
		}
		if m.feedbackMode {
			rows = append(rows, "Feedback (Esc to cancel, Enter to submit):")
			rows = append(rows, fmt.Sprintf("> %s█", m.feedbackText))
		} else {
			rows = append(rows, plainFileOptionsBar(m.req, m.selectedOption))
		}
		return rows
	}

	// Tool name row
	rows = append(rows, fmt.Sprintf("Tool: %s", m.req.Tool))

	// Command row (if present)
	if cmd := m.req.CommandStr(); cmd != "" {
		rows = append(rows, fmt.Sprintf("Command: %s", truncateText(cmd, innerWidth-9)))
	}

	// Path row (if present)
	if path := m.req.PathStr(); path != "" {
		rows = append(rows, fmt.Sprintf("Path: %s", truncateText(path, innerWidth-6)))
	}

	// Description row (if present)
	if desc := m.req.Description; desc != "" {
		rows = append(rows, fmt.Sprintf("Reason: %s", truncateText(desc, innerWidth-8)))
	}

	// Action options or feedback input row
	if m.feedbackMode {
		rows = append(rows, "Feedback (Esc to cancel, Enter to submit):")
		rows = append(rows, fmt.Sprintf("> %s█", m.feedbackText))
	} else {
		rows = append(rows, plainOptionsBar(m.selectedOption))
	}

	return rows
}

func plainFileOptionsBar(req Request, selected Option) string {
	var parts []string
	if req.Kind == KindFileCreate {
		parts = append(parts, "[1] Yes, allow creation")
		if req.OutsideWS {
			parts = append(parts, "[a] Yes, and always allow non-workspace access")
		}
		parts = append(parts, "[2] No, deny creation")
	} else {
		parts = append(parts, "[1] Yes, accept this change")
		if req.OutsideWS {
			parts = append(parts, "[a] Yes, and always allow non-workspace access")
		}
		parts = append(parts, "[2] No, reject this change")
	}
	parts = append(parts, "[f] Full diff", "[Tab] Feedback")
	return strings.Join(parts, "  ")
}

func plainOptionsBar(selected Option) string {
	// Standard: [y] Approve once  [a] Always allow  [n] Deny  [d] Feedback
	parts := []string{
		"[y] Approve once",
		"[a] Always allow",
		"[n] Deny",
		"[d] Feedback",
	}
	return strings.Join(parts, "  ")
}

// Render produces the ANSI true-color styled box representation.
func (m *Model) Render(width int) string {
	return strings.Join(m.Lines(width), "\n")
}

// Lines returns the ANSI styled rows for terminal display.
func (m *Model) Lines(width int) []string {
	if width < MinBoxWidth {
		width = MinBoxWidth
	}
	innerWidth := width - 4

	var lines []string

	// Top border with colored title and dim border
	title := m.req.TitleStr()
	titleLen := ansi.StringWidth(title)
	dashes := width - (titleLen + 5)
	if dashes < 1 {
		dashes = 1
	}

	top := fmt.Sprintf("%s┌─ %s%s%s%s %s%s┐%s",
		palette.DimGrayOpen,
		palette.WarningOpen, title, palette.SeparatorClose,
		palette.DimGrayOpen,
		strings.Repeat("─", dashes),
		palette.DimGrayOpen,
		palette.SeparatorClose,
	)
	lines = append(lines, top)

	// Body content
	bodyRows := m.styledBodyRows(innerWidth)
	for _, row := range bodyRows {
		visLen := ansi.StringWidth(row)
		pad := innerWidth - visLen
		if pad < 0 {
			pad = 0
		}
		lines = append(lines, fmt.Sprintf("%s│%s %s%s %s│%s",
			palette.DimGrayOpen, palette.SeparatorClose,
			row, strings.Repeat(" ", pad),
			palette.DimGrayOpen, palette.SeparatorClose,
		))
	}

	// Bottom border
	bottom := fmt.Sprintf("%s└%s┘%s",
		palette.DimGrayOpen,
		strings.Repeat("─", width-2),
		palette.SeparatorClose,
	)
	lines = append(lines, bottom)

	return lines
}

func (m *Model) styledBodyRows(innerWidth int) []string {
	var rows []string

	if m.req.Kind == KindFileEdit || m.req.Kind == KindFileCreate {
		if path := m.req.PathStr(); path != "" {
			rows = append(rows, fmt.Sprintf("%sFile:%s %s",
				palette.DimGrayOpen, palette.SeparatorClose,
				truncateText(path, innerWidth-6)))
		}
		if m.req.OutsideWS {
			rows = append(rows, fmt.Sprintf("%sReason:%s %soutside workspace%s",
				palette.DimGrayOpen, palette.SeparatorClose,
				palette.WarningOpen, palette.SeparatorClose))
		} else if desc := m.req.Description; desc != "" {
			rows = append(rows, fmt.Sprintf("%sReason:%s %s",
				palette.DimGrayOpen, palette.SeparatorClose,
				truncateText(desc, innerWidth-8)))
		}
		if q := m.req.PromptQuestionStr(); q != "" {
			rows = append(rows, fmt.Sprintf("%s%s%s", palette.BlueBoldOpen, q, palette.SeparatorClose))
		}
		if m.feedbackMode {
			rows = append(rows, fmt.Sprintf("%sFeedback (Esc to cancel, Enter to submit):%s",
				palette.WarningOpen, palette.SeparatorClose))
			rows = append(rows, fmt.Sprintf("%s>%s %s%s█%s",
				palette.WarningBlueOpen, palette.SeparatorClose,
				m.feedbackText,
				palette.DimGrayOpen, palette.SeparatorClose))
		} else {
			rows = append(rows, styledFileOptionsBar(m.req, m.selectedOption))
		}
		return rows
	}

	// Tool name
	rows = append(rows, fmt.Sprintf("%sTool:%s %s%s%s",
		palette.DimGrayOpen, palette.SeparatorClose,
		palette.BlueBoldOpen, m.req.Tool, palette.SeparatorClose))

	// Command
	if cmd := m.req.CommandStr(); cmd != "" {
		rows = append(rows, fmt.Sprintf("%sCommand:%s %s",
			palette.DimGrayOpen, palette.SeparatorClose,
			truncateText(cmd, innerWidth-9)))
	}

	// Path
	if path := m.req.PathStr(); path != "" {
		rows = append(rows, fmt.Sprintf("%sPath:%s %s",
			palette.DimGrayOpen, palette.SeparatorClose,
			truncateText(path, innerWidth-6)))
	}

	// Reason / Description
	if desc := m.req.Description; desc != "" {
		rows = append(rows, fmt.Sprintf("%sReason:%s %s",
			palette.DimGrayOpen, palette.SeparatorClose,
			truncateText(desc, innerWidth-8)))
	}

	// Action row
	if m.feedbackMode {
		rows = append(rows, fmt.Sprintf("%sFeedback (Esc to cancel, Enter to submit):%s",
			palette.WarningOpen, palette.SeparatorClose))
		rows = append(rows, fmt.Sprintf("%s>%s %s%s█%s",
			palette.WarningBlueOpen, palette.SeparatorClose,
			m.feedbackText,
			palette.DimGrayOpen, palette.SeparatorClose))
	} else {
		rows = append(rows, styledOptionsBar(m.selectedOption))
	}

	return rows
}

func styledOptionsBar(selected Option) string {
	renderBtn := func(key string, label string, opt Option) string {
		if selected == opt {
			return fmt.Sprintf("%s[%s]%s %s%s%s",
				palette.WarningBlueOpen, key, palette.SeparatorClose,
				palette.BlueBoldOpen, label, palette.SeparatorClose)
		}
		return fmt.Sprintf("%s[%s]%s %s",
			palette.BlueBoldOpen, key, palette.SeparatorClose,
			label)
	}

	b1 := renderBtn("y", "Approve once", OptionApproveOnce)
	b2 := renderBtn("a", "Always allow", OptionAlwaysAllow)
	b3 := renderBtn("n", "Deny", OptionDeny)
	b4 := renderBtn("d", "Feedback", OptionFeedback)

	return fmt.Sprintf("%s  %s  %s  %s", b1, b2, b3, b4)
}

func styledFileOptionsBar(req Request, selected Option) string {
	renderBtn := func(key string, label string, opt Option) string {
		if selected == opt {
			return fmt.Sprintf("%s[%s]%s %s%s%s",
				palette.WarningBlueOpen, key, palette.SeparatorClose,
				palette.BlueBoldOpen, label, palette.SeparatorClose)
		}
		return fmt.Sprintf("%s[%s]%s %s",
			palette.BlueBoldOpen, key, palette.SeparatorClose,
			label)
	}

	var parts []string
	if req.Kind == KindFileCreate {
		parts = append(parts, renderBtn("1", "Yes, allow creation", OptionApproveOnce))
		if req.OutsideWS {
			parts = append(parts, renderBtn("a", "Yes, and always allow non-workspace access", OptionAlwaysAllow))
		}
		parts = append(parts, renderBtn("2", "No, deny creation", OptionDeny))
	} else {
		parts = append(parts, renderBtn("1", "Yes, accept this change", OptionApproveOnce))
		if req.OutsideWS {
			parts = append(parts, renderBtn("a", "Yes, and always allow non-workspace access", OptionAlwaysAllow))
		}
		parts = append(parts, renderBtn("2", "No, reject this change", OptionDeny))
	}
	parts = append(parts, renderBtn("f", "Full diff", OptionFeedback))
	return strings.Join(parts, "  ")
}

func truncateText(text string, maxLen int) string {
	if maxLen <= 0 || ansi.StringWidth(text) <= maxLen {
		return text
	}
	return ansi.Truncate(text, maxLen, "…")
}
