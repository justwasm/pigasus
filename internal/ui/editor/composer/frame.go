package composer

import (
	"strings"

	tas "charm.land/bubbles/v2/textarea"
	"github.com/justwasm/pigasus/palette"
)

// StandardOptions configures standard composer textarea behavior.
type StandardOptions struct {
	Prompt      string // Line 0 prompt (e.g. "> " or styled prompt). Defaults to "> "
	ContPrompt  string // Subsequent lines continuation prompt. Defaults to "  " (2 spaces)
	Placeholder string
	MinHeight   int // Minimum visual rows. Defaults to 1.
	MaxHeight   int // Maximum visual rows. Defaults to 8.
	Width       int // Initial width.
}

// ConfigureStandardTextArea sets up a bubbles textarea.Model with standard
// Pigasus composer behaviors: dynamic height, multi-line continuation prompt,
// virtual cursor disabled, line numbers hidden, and canonical min/max visual bounds.
func ConfigureStandardTextArea(ta *tas.Model, opts StandardOptions) {
	if ta == nil {
		return
	}
	prompt := opts.Prompt
	if prompt == "" {
		prompt = "> "
	}
	contPrompt := opts.ContPrompt
	if contPrompt == "" {
		contPrompt = "  "
	}
	minH := opts.MinHeight
	if minH <= 0 {
		minH = 1
	}
	maxH := opts.MaxHeight
	if maxH <= 0 {
		maxH = 8
	}

	ta.SetVirtualCursor(false)
	ta.ShowLineNumbers = false
	ta.DynamicHeight = true
	ta.MinHeight = minH
	ta.MaxHeight = maxH
	ta.Prompt = prompt
	ta.SetPromptFunc(2, func(info tas.PromptInfo) string {
		if info.LineNumber == 0 {
			return prompt
		}
		return contPrompt
	})
	ta.Placeholder = opts.Placeholder
	ta.CharLimit = 0
	ta.EndOfBufferCharacter = ' '
	if opts.Width > 0 {
		ta.SetWidth(opts.Width)
	}
	ta.SetHeight(minH)
}

// DualRulesFrame wraps an inputView between two canonical Pigasus horizontal
// rules from the palette package:
//
//	<palette.Rule(width)>
//	<inputView>
//	<palette.Rule(width)>
//
// Trailing newlines in inputView are trimmed so the frame preserves exact visual height.
func DualRulesFrame(width int, inputView string) string {
	if width <= 0 {
		width = 80
	}
	rule := palette.Rule(width)
	trimmed := strings.TrimRight(inputView, "\r\n")
	if trimmed == "" {
		return rule + "\n" + rule
	}
	return rule + "\n" + trimmed + "\n" + rule
}

// CalculateViewportHeight computes the height for the scrollable viewport above
// the composer box, accounting for the dynamic input height, status bar rows,
// and the two divider rules (top and bottom).
func CalculateViewportHeight(totalHeight, inputHeight, statusHeight int) int {
	const rulesCount = 2 // Top rule + bottom rule
	const minVpHeight = 4

	vpHeight := totalHeight - inputHeight - statusHeight - rulesCount
	if vpHeight < minVpHeight {
		return minVpHeight
	}
	return vpHeight
}
