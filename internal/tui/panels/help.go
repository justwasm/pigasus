package panels

import (
	"github.com/justwasm/pigasus/internal/ui/views/helppanel"
)

// HelpHost defines the host operations needed by the /help panel.
type HelpHost interface {
	ModelName() string
	TierName() string
	SessionCwd() string
	UserEmail() string
	ConversationID() string
	TerminalSize() (width int, height int)
	VimEnabled() bool
	AppendResponse(lines []string)
	SelectCommand(cmd string)
}

// NewHelpPanel builds the /help overlay with the given host seams.
func NewHelpPanel(h HelpHost) *helppanel.Panel {
	return helppanel.New(helppanel.Seams{
		Model:          h.ModelName,
		Tier:           h.TierName,
		Cwd:            h.SessionCwd,
		Email:          h.UserEmail,
		ConversationID: h.ConversationID,
		Width:          func() int { w, _ := h.TerminalSize(); return w },
		Height:         func() int { _, hgt := h.TerminalSize(); return hgt },
		VimEnabled:     h.VimEnabled,
		Respond:        h.AppendResponse,
		SelectCommand:  h.SelectCommand,
	})
}
