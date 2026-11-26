package permissionprompt

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/justwasm/pigasus/palette"
)

const (
	// DefaultBoxWidth is the standard width for the permission confirmation area.
	DefaultBoxWidth = 80
	// MinBoxWidth is the minimum width required to render the confirmation prompt cleanly.
	MinBoxWidth = 50
)

// RenderPlain produces the unstyled ASCII / Unicode text representation.
func (m *Model) RenderPlain(width int) string {
	return strings.Join(m.RenderPlainLines(width), "\n")
}

// RenderPlainLines returns the prompt rendered row by row without ANSI escape codes.
func (m *Model) RenderPlainLines(width int) []string {
	return m.renderLines(width, false)
}

// Render produces the ANSI true-color styled representation.
func (m *Model) Render(width int) string {
	return strings.Join(m.Lines(width), "\n")
}

// Lines returns the ANSI styled rows for terminal display.
func (m *Model) Lines(width int) []string {
	return m.renderLines(width, true)
}

func (m *Model) renderLines(width int, styled bool) []string {
	if width < MinBoxWidth {
		width = MinBoxWidth
	}

	var lines []string

	// 1. Title line
	title := m.req.TitleStr()
	if styled {
		lines = append(lines, palette.BoldOpen+title+palette.SeparatorClose)
	} else {
		lines = append(lines, title)
	}

	// 2. Divider line: full width rule
	rule := strings.Repeat("─", width)
	if styled {
		lines = append(lines, palette.DimGrayOpen+rule+palette.SeparatorClose)
	} else {
		lines = append(lines, rule)
	}

	// 3. Blank line
	lines = append(lines, "")

	// 4. Details / Diff / Target
	if m.req.Kind == KindFileEdit || m.req.Kind == KindFileCreate {
		diff := extractDiffPreview(m.req, width, m.expandedView)
		pathText := diff.Path
		if pathText == "" {
			pathText = m.req.PathStr()
		}

		var statParts []string
		if diff.Additions > 0 {
			if styled {
				statParts = append(statParts, fmt.Sprintf("%s+%d%s", palette.ToolAddOpen, diff.Additions, palette.SeparatorClose))
			} else {
				statParts = append(statParts, fmt.Sprintf("+%d", diff.Additions))
			}
		}
		if diff.Deletions > 0 {
			if styled {
				statParts = append(statParts, fmt.Sprintf("%s-%d%s", palette.ToolRemoveOpen, diff.Deletions, palette.SeparatorClose))
			} else {
				statParts = append(statParts, fmt.Sprintf("-%d", diff.Deletions))
			}
		}

		pathLine := pathText
		if len(statParts) > 0 {
			pathLine += "  " + strings.Join(statParts, " ")
		}
		if styled {
			lines = append(lines, palette.BoldOpen+pathText+palette.SeparatorClose+"  "+strings.Join(statParts, " "))
		} else {
			lines = append(lines, pathLine)
		}

		// Diff lines
		for _, dl := range diff.Lines {
			linePrefix := fmt.Sprintf("  %d  %c  ", dl.Num, dl.Type)
			fullText := linePrefix + dl.Text
			if styled {
				if dl.Type == '-' {
					lines = append(lines, palette.ToolRemoveOpen+fullText+palette.SeparatorClose)
				} else if dl.Type == '+' {
					lines = append(lines, palette.ToolAddOpen+fullText+palette.SeparatorClose)
				} else {
					lines = append(lines, fullText)
				}
			} else {
				lines = append(lines, fullText)
			}
		}

		if m.req.OutsideWS {
			if styled {
				lines = append(lines, fmt.Sprintf("  %sReason:%s %soutside workspace%s",
					palette.DimGrayOpen, palette.SeparatorClose,
					palette.WarningOpen, palette.SeparatorClose))
			} else {
				lines = append(lines, "  Reason: outside workspace")
			}
		} else if desc := m.req.Description; desc != "" {
			if styled {
				lines = append(lines, fmt.Sprintf("  %sReason:%s %s",
					palette.DimGrayOpen, palette.SeparatorClose, desc))
			} else {
				lines = append(lines, "  Reason: "+desc)
			}
		}

		if m.req.Kind == KindFileEdit {
			if styled {
				lines = append(lines, fmt.Sprintf("  %sshift+tab to auto-approve file edits%s",
					palette.DimGrayOpen, palette.SeparatorClose))
			} else {
				lines = append(lines, "  shift+tab to auto-approve file edits")
			}
		}
	} else {
		// Command / Tool
		if styled {
			lines = append(lines, fmt.Sprintf("%sRequesting permission for:%s", palette.DimGrayOpen, palette.SeparatorClose))
		} else {
			lines = append(lines, "Requesting permission for:")
		}

		cmd := m.req.CommandStr()
		if cmd != "" {
			if styled {
				lines = append(lines, fmt.Sprintf("   %s%s%s", palette.BoldOpen, cmd, palette.SeparatorClose))
			} else {
				lines = append(lines, "   "+cmd)
			}
		} else if m.req.Tool != "" {
			if styled {
				lines = append(lines, fmt.Sprintf("   %sTool:%s %s", palette.DimGrayOpen, palette.SeparatorClose, m.req.Tool))
			} else {
				lines = append(lines, "   Tool: "+m.req.Tool)
			}
		}

		if m.req.PathStr() != "" {
			if styled {
				lines = append(lines, fmt.Sprintf("  %sPath:%s %s",
					palette.DimGrayOpen, palette.SeparatorClose, m.req.PathStr()))
			} else {
				lines = append(lines, "  Path: "+m.req.PathStr())
			}
		}

		if m.req.OutsideWS {
			if styled {
				lines = append(lines, fmt.Sprintf("  %sReason:%s %soutside workspace%s",
					palette.DimGrayOpen, palette.SeparatorClose,
					palette.WarningOpen, palette.SeparatorClose))
			} else {
				lines = append(lines, "  Reason: outside workspace")
			}
		} else if desc := m.req.Description; desc != "" {
			if styled {
				lines = append(lines, fmt.Sprintf("  %sReason:%s %s",
					palette.DimGrayOpen, palette.SeparatorClose, desc))
			} else {
				lines = append(lines, "  Reason: "+desc)
			}
		}
	}

	// 5. Blank line
	lines = append(lines, "")

	// 6. Question line
	q := m.req.PromptQuestionStr()
	if q != "" {
		if styled {
			lines = append(lines, palette.BoldOpen+q+palette.SeparatorClose)
		} else {
			lines = append(lines, q)
		}
	}

	// 7. Choices / Feedback / Amend
	if m.amendMode {
		promptHint := "Amend command (Esc to cancel, Enter to submit):"
		if styled {
			lines = append(lines, palette.WarningOpen+promptHint+palette.SeparatorClose)
			lines = append(lines, fmt.Sprintf("%s>%s %s%s█%s",
				palette.BlueOpen, palette.SeparatorClose,
				m.amendText,
				palette.DimGrayOpen, palette.SeparatorClose))
		} else {
			lines = append(lines, promptHint)
			lines = append(lines, fmt.Sprintf("> %s█", m.amendText))
		}
	} else if m.feedbackMode {
		promptHint := "Feedback (Esc to cancel, Enter to submit):"
		if styled {
			lines = append(lines, palette.WarningOpen+promptHint+palette.SeparatorClose)
			lines = append(lines, fmt.Sprintf("%s>%s %s%s█%s",
				palette.BlueOpen, palette.SeparatorClose,
				m.feedbackText,
				palette.DimGrayOpen, palette.SeparatorClose))
		} else {
			lines = append(lines, promptHint)
			lines = append(lines, fmt.Sprintf("> %s█", m.feedbackText))
		}
	} else {
		choices := m.Choices()
		for i, c := range choices {
			if i == m.selectedIndex {
				if styled {
					lines = append(lines, fmt.Sprintf("%s> %d. %s%s",
						palette.BlueBoldOpen, c.Number, c.Title, palette.SeparatorClose))
				} else {
					lines = append(lines, fmt.Sprintf("> %d. %s", c.Number, c.Title))
				}
			} else {
				if styled {
					lines = append(lines, fmt.Sprintf("  %s%d. %s%s",
						palette.DimGrayOpen, c.Number, c.Title, palette.SeparatorClose))
				} else {
					lines = append(lines, fmt.Sprintf("  %d. %s", c.Number, c.Title))
				}
			}
		}
	}

	// 8. Blank line
	lines = append(lines, "")

	// 9. Hints line
	hint := "  ↑/↓ Navigate"
	if m.req.Kind == KindFileEdit || m.req.Kind == KindFileCreate {
		hint = "  ↑/↓ Navigate · f full diff"
	} else if m.req.CommandStr() != "" || m.req.Tool == "run_command" || m.req.Tool == "run_shell_command" {
		hint = "  ↑/↓ Navigate · tab Amend · ctrl+g edit/expand command"
	}

	if styled {
		lines = append(lines, palette.DimGrayOpen+hint+palette.SeparatorClose)
	} else {
		lines = append(lines, hint)
	}

	return lines
}

type diffPreview struct {
	Path      string
	Additions int
	Deletions int
	Lines     []diffLine
}

type diffLine struct {
	Num  int
	Type rune // '-' or '+'
	Text string
}

func extractDiffPreview(req Request, maxW int, expanded bool) diffPreview {
	dp := diffPreview{
		Path: req.PathStr(),
	}

	limit := 6
	if expanded {
		limit = 100
	}

	// If explicit diff string is provided
	if req.Diff != "" {
		parseUnifiedDiff(req.Diff, &dp, limit)
		return dp
	}

	// If args contain Target and Replacement (from edit_file)
	if req.Args != nil {
		target, _ := req.Args["Target"].(string)
		if target == "" {
			target, _ = req.Args["target"].(string)
		}
		replacement, _ := req.Args["Replacement"].(string)
		if replacement == "" {
			replacement, _ = req.Args["replacement"].(string)
		}

		if target != "" || replacement != "" {
			targetLines := splitCleanLines(target)
			replacementLines := splitCleanLines(replacement)
			dp.Deletions = len(targetLines)
			dp.Additions = len(replacementLines)

			lineNum := 1
			for _, l := range targetLines {
				if len(dp.Lines) >= limit {
					break
				}
				dp.Lines = append(dp.Lines, diffLine{Num: lineNum, Type: '-', Text: l})
			}
			for _, l := range replacementLines {
				if len(dp.Lines) >= limit {
					break
				}
				dp.Lines = append(dp.Lines, diffLine{Num: lineNum, Type: '+', Text: l})
			}
			return dp
		}

		// If args contain Content (from write_file)
		content, _ := req.Args["Content"].(string)
		if content == "" {
			content, _ = req.Args["content"].(string)
		}
		if content != "" {
			contentLines := splitCleanLines(content)
			dp.Additions = len(contentLines)
			for i, l := range contentLines {
				if len(dp.Lines) >= limit {
					break
				}
				dp.Lines = append(dp.Lines, diffLine{Num: i + 1, Type: '+', Text: l})
			}
			return dp
		}
	}

	return dp
}

func splitCleanLines(s string) []string {
	if s == "" {
		return nil
	}
	s = strings.TrimSuffix(s, "\n")
	return strings.Split(s, "\n")
}

func parseUnifiedDiff(diff string, dp *diffPreview, limit int) {
	lines := strings.Split(diff, "\n")
	lineNum := 1
	for _, l := range lines {
		if strings.HasPrefix(l, "---") || strings.HasPrefix(l, "+++") || strings.HasPrefix(l, "@@") {
			continue
		}
		if strings.HasPrefix(l, "-") {
			dp.Deletions++
			if len(dp.Lines) < limit {
				dp.Lines = append(dp.Lines, diffLine{Num: lineNum, Type: '-', Text: l[1:]})
			}
		} else if strings.HasPrefix(l, "+") {
			dp.Additions++
			if len(dp.Lines) < limit {
				dp.Lines = append(dp.Lines, diffLine{Num: lineNum, Type: '+', Text: l[1:]})
			}
			lineNum++
		} else if strings.HasPrefix(l, " ") {
			lineNum++
		}
	}
}

func truncateText(s string, maxLen int) string {
	if maxLen <= 3 || ansi.StringWidth(s) <= maxLen {
		return s
	}
	return ansi.Truncate(s, maxLen-3, "...")
}
