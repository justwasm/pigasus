package sidequestion

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/justwasm/pigasus/internal/mdrender"
)

// MarkdownRenderer transforms raw markdown text to ANSI for terminal rendering.
type MarkdownRenderer func(string, int) string

// CardRenderOptions contains layout and styling options for rendering a Card.
type CardRenderOptions struct {
	Width     int
	Height    int
	CardCount int
	Scroll    int
	RenderMd  MarkdownRenderer
}

// RenderInputCard wraps content in a box with a thick left border (┃),
// matching google3/third_party/jetski/cli/model/model.renderInputCard.
func RenderInputCard(width int, content string) string {
	cardWidth := width - 3
	if cardWidth < 10 {
		cardWidth = 10
	}
	style := lipgloss.NewStyle().
		Border(lipgloss.ThickBorder(), false, false, false, true).
		BorderForeground(lipgloss.Color("#268BD2")).
		PaddingLeft(1).
		Width(cardWidth)
	return style.Render(content)
}

// MaxBtwAnswerLines computes maximum visible answer lines:
//   - Collapsed: 30% of windowHeight (min 5)
//   - Expanded: windowHeight - 8 (min 5)
func MaxBtwAnswerLines(height int, expanded bool) int {
	if height <= 0 {
		height = 24
	}
	if expanded {
		lines := height - 8
		if lines < 5 {
			lines = 5
		}
		return lines
	}
	lines := int(float64(height) * 0.3)
	if lines < 5 {
		lines = 5
	}
	return lines
}

// MaxAnswerLines is an alias for MaxBtwAnswerLines.
func MaxAnswerLines(height int, expanded bool) int {
	return MaxBtwAnswerLines(height, expanded)
}

// FormatBtwAnswerLines renders markdown and window-scrolls the answer text.
func FormatBtwAnswerLines(answer string, cardWidth int, expanded bool, height, scroll int, renderMd ...MarkdownRenderer) []string {
	var renderer MarkdownRenderer
	if len(renderMd) > 0 && renderMd[0] != nil {
		renderer = renderMd[0]
	} else {
		renderer = mdrender.RenderMarkdown
	}
	styled := renderer(answer, cardWidth)
	maxLines := MaxBtwAnswerLines(height, expanded)
	return FormatAnswerLines(styled, maxLines, scroll, expanded)
}

// RenderBtwBodyLines produces the body lines for a side-question Card.
func RenderBtwBodyLines(state string, answer string, err error, cardWidth int, expanded bool, height, scroll int, renderMd ...MarkdownRenderer) []string {
	if state == StatePending || (state == StateStreaming && answer == "") {
		return []string{Spinner() + " Thinking..."}
	}
	if err != nil || state == StateFailed {
		errMsg := "unknown error"
		if err != nil {
			errMsg = err.Error()
		}
		return []string{"Error: " + errMsg, "Press Enter/Esc to dismiss"}
	}
	if answer != "" {
		return FormatBtwAnswerLines(answer, cardWidth, expanded, height, scroll, renderMd...)
	}
	return []string{"Error: /btw ended unexpectedly without a response"}
}

// RenderBtwCard renders the active side-question card into an input-card box.
func RenderBtwCard(card *Card, opts CardRenderOptions) string {
	if card == nil {
		return ""
	}
	question, answer, _, state, err := card.LockedSnapshot()
	expanded := card.IsExpanded()

	w := opts.Width
	if w <= 0 {
		w = 80
	}
	cardWidth := w - 3
	if cardWidth < 10 {
		cardWidth = 10
	}

	lines := buildCardLines(question, answer, state, err, cardWidth, expanded, opts)
	return RenderInputCard(w, strings.Join(lines, "\n"))
}

// RenderCard is an alias for RenderBtwCard.
func RenderCard(card *Card, opts CardRenderOptions) string {
	return RenderBtwCard(card, opts)
}

// Render formats this Card using the provided CardRenderOptions.
func (c *Card) Render(opts CardRenderOptions) string {
	return RenderBtwCard(c, opts)
}

func buildCardLines(question, answer, state string, err error, cardWidth int, expanded bool, opts CardRenderOptions) []string {
	var lines []string
	lines = append(lines, firstLine(question), "")
	body := RenderBtwBodyLines(state, answer, err, cardWidth, expanded, opts.Height, opts.Scroll, opts.RenderMd)
	lines = append(lines, body...)
	lines = append(lines, "")

	if state == StatePending || state == StateStreaming {
		lines = append(lines, Spinner()+" Generating... (Enter/Esc to cancel)")
	} else if err == nil && state != StateFailed {
		lines = append(lines, HintText(expanded, opts.CardCount))
	}
	return lines
}

func firstLine(s string) string {
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		return s[:idx]
	}
	return s
}
