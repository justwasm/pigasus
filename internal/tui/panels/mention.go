package panels

import (
	"github.com/justwasm/pigasus/internal/ui/views/mentionpicker"
)

// MentionHost defines host seams for the @mention picker.
type MentionHost interface {
	InputValue() string
	SetInputValue(string)
	SessionCwd() string
	TerminalWidth() int
}

// NewMentionPicker builds the @mention picker with the host's seams.
func NewMentionPicker(h MentionHost) *mentionpicker.Panel {
	return mentionpicker.New(mentionpicker.Seams{
		Buffer:    h.InputValue,
		SetBuffer: h.SetInputValue,
		Cwd:       h.SessionCwd,
		Width:     h.TerminalWidth,
	})
}
