// help_host.go — the host's side of the /help overlay.
//
// # Where the panel lives
//
// The panel itself is internal/ui/views/helppanel: it owns the tab,
// the cursor, the window, the rows and the keys.
//
// The host keeps the facts the panel cannot know — the model's name,
// the tier, the working directory, the email, the conversation id,
// the terminal's size and whether vim mode is on — and hands them to
// the panel as seams.
//
// The overlay's rendering, its list navigation and its key handling
// are the panel's. The host's `/help` slash command and its `?`
// keybinding only open it.

package cliapp

import (
	"github.com/justwasm/pigasus/internal/ui/views/helppanel"
)

// newHelpPanel builds the /help overlay with the host's seams.
func (m *runMode) newHelpPanel() *helppanel.Panel {
	return helppanel.New(helppanel.Seams{
		Model:          func() string { return m.model },
		Tier:           func() string { return m.tier },
		Cwd:            func() string { return m.cwd },
		Email:          func() string { return m.email },
		ConversationID: func() string { return m.conversationID },
		Width:          func() int { return m.width },
		Height:         func() int { return m.height },
		VimEnabled:     func() bool { return m.vimOn },
		Respond: func(lines []string) {
			m.response = append(m.response, lines...)
		},
	})
}
