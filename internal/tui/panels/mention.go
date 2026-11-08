package panels

import (
	atfiles "github.com/justwasm/pigasus/internal/app/prompt/mentions/files"
	"github.com/justwasm/pigasus/internal/ui/views/mentionpicker"
)

// MentionHost defines host seams for the @mention picker.
type MentionHost interface {
	InputValue() string
	SetInputValue(string)
	SessionCwd() string
	TerminalWidth() int
	// MentionList is non-nil in a remote-agent session: the workspace
	// lives on the proxy, so the picker must ask the host instead of
	// walking the local disk.
	MentionList() func(root string) ([]atfiles.Entry, error)
}

// NewMentionPicker builds the @mention picker with the host's seams.
func NewMentionPicker(h MentionHost) *mentionpicker.Panel {
	return mentionpicker.New(mentionpicker.Seams{
		Buffer:    h.InputValue,
		SetBuffer: h.SetInputValue,
		Cwd:       h.SessionCwd,
		Width:     h.TerminalWidth,
		List:      h.MentionList(),
	})
}
