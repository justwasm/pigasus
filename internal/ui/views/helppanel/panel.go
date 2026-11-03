// Package helppanel holds the /help overlay: the tab bar, the four
// tabs' bodies, the list cursor and the window that scrolls them.
//
// # What it is
//
// /help opens a modal overlay with a tab bar — general, commands,
// shortcuts and, when vim mode is on, vim. The commands and shortcuts
// tabs are long catalogues: the reference renders an 18-row window at
// a 30-row terminal and keeps a cursor inside that window. The window
// scrolls one row at a time once the cursor reaches an edge, and the
// cursor wraps at both ends of the catalogue.
//
// 【一手】 agy v1.4.0, read at 220×30:
//
//	entry          [1-18 of 99 items]   cursor at row 0
//	Down ×17       [1-18 of 99 items]   cursor at row 17 (window edge)
//	Down ×18       [2-19 of 99 items]   window scrolled one row
//	Down ×19       [3-20 of 99 items]
//	Ctrl+End       [82-99 of 99 items]  cursor at row 98
//	Down at 98     [1-18 of 99 items]   wraps to row 0
//	Ctrl+Home      [1-18 of 99 items]   cursor at row 0
//	Up at 0        [82-99 of 99 items]  wraps to row 98
//
// Right/Left/Tab cycle the tabs and reset the window and the cursor —
// the reference does not keep a per-tab scroll position.
//
// # Where it lives
//
// The panel owns the tab, the cursor, the window, the rows and the
// keys. The host keeps the facts the panel cannot know: the model's
// name, the tier, the working directory, the email, the conversation
// id, the terminal's size and whether vim mode is on.
package helppanel

import (
	"github.com/justwasm/pigasus/internal/ui/slashcommands"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/vim"
)

// Seams are the host's answers the panel cannot answer itself.
//
// Every one of them is optional: a panel built without a seam behaves
// as if the seam were answered with the zero value, which keeps the
// package usable from a test without a running TUI.
type Seams struct {
	// Model is the model's display name. The GENERAL tab prints it.
	Model func() string
	// Tier is the account's plan tier. The GENERAL tab prints it in
	// parentheses after the email.
	Tier func() string
	// Cwd is the session's working directory. The GENERAL tab prints
	// its shortened form.
	Cwd func() string
	// Email is the signed-in account. The GENERAL tab prints it.
	Email func() string
	// ConversationID is the session's conversation id. The GENERAL tab
	// prints it.
	ConversationID func() string
	// Width is the terminal's width. The COMMANDS tab truncates its
	// descriptions against it.
	Width func() int
	// Height is the terminal's height. The list window's size is
	// derived from it.
	Height func() int
	// VimEnabled reports whether vim mode is on. The vim tab is
	// appended to the tab bar only when it is.
	VimEnabled func() bool
	// Respond echoes lines into the conversation.
	//
	// The panel writes one block here: the "/help" exit echo.
	Respond func(lines []string)
}

// Panel is the /help overlay.
//
// The zero value is usable: a panel without seams renders the tabs it
// can and answers every key with "nothing happened".
type Panel struct {
	seams Seams
	// open is true while the overlay is on screen.
	open bool
	// tab is the active tab's index.
	tab int
	// window is the first visible row of the active list.
	window int
	// cursor is the highlighted row of the active list.
	cursor int
}

// New builds the overlay.
func New(seams Seams) *Panel {
	return &Panel{seams: seams}
}

// Active is true while the overlay is on screen.
func (p *Panel) Active() bool {
	return p != nil && p.open
}

// Tab is the active tab's index.
//
// The GENERAL tab is 0, the COMMANDS tab is 1, the SHORTCUTS tab is 2
// and the vim tab is 3 — the last one only exists when vim mode is on.
func (p *Panel) Tab() int {
	if p == nil {
		return 0
	}
	return p.tab
}

// Open shows the overlay on the given tab.
//
// The window and the cursor start at the top of the list: the
// reference does not keep a scroll position across an open.
func (p *Panel) Open(tab int) {
	if p == nil {
		return
	}
	p.open = true
	p.tab = p.clampTab(tab)
	p.window = 0
	p.cursor = 0
}

// Cancel dismisses the overlay and echoes the exit line.
//
// The echo is the reference's own: the transcript carries the "/help"
// block and the "Exited /help command" line.
func (p *Panel) Cancel() {
	if p == nil {
		return
	}
	p.open = false
	p.respond(tuilib.SlashEchoLines("/help", "Exited /help command"))
}

// tabs is the tab bar's labels.
//
// The vim catalogue tab is appended only when vim mode is enabled, so
// the default three-tab bar (and every byte-verified capture of it) is
// unchanged for non-vim users.
func (p *Panel) tabs() []string {
	if p.vimEnabled() {
		return []string{"general", "commands", "shortcuts", vim.HelpTitle}
	}
	return []string{"general", "commands", "shortcuts"}
}

// clampTab keeps a tab index inside the tab bar.
func (p *Panel) clampTab(tab int) int {
	tabs := p.tabs()
	if tab < 0 || tab >= len(tabs) {
		return 0
	}
	return tab
}

// listLen is the row count of the active tab's list.
//
// The GENERAL tab has no list.
func (p *Panel) listLen() int {
	switch p.tab {
	case 1:
		return len(slashcommands.Default)
	case 2:
		return len(tuilib.HelpShortcuts)
	case 3:
		return len(vim.HelpRows)
	default:
		return 0
	}
}

// listWindowSize is the window size for the terminal's height.
func (p *Panel) listWindowSize() int {
	return tuilib.HelpListWindowFor(p.height())
}

// model is the model's display name.
func (p *Panel) model() string {
	if p == nil || p.seams.Model == nil {
		return ""
	}
	return p.seams.Model()
}

// tier is the account's plan tier.
func (p *Panel) tier() string {
	if p == nil || p.seams.Tier == nil {
		return ""
	}
	return p.seams.Tier()
}

// cwd is the session's working directory.
func (p *Panel) cwd() string {
	if p == nil || p.seams.Cwd == nil {
		return ""
	}
	return p.seams.Cwd()
}

// email is the signed-in account.
func (p *Panel) email() string {
	if p == nil || p.seams.Email == nil {
		return ""
	}
	return p.seams.Email()
}

// conversationID is the session's conversation id.
func (p *Panel) conversationID() string {
	if p == nil || p.seams.ConversationID == nil {
		return ""
	}
	return p.seams.ConversationID()
}

// width is the terminal's width.
func (p *Panel) width() int {
	if p == nil || p.seams.Width == nil {
		return 0
	}
	return p.seams.Width()
}

// height is the terminal's height.
func (p *Panel) height() int {
	if p == nil || p.seams.Height == nil {
		return 0
	}
	return p.seams.Height()
}

// vimEnabled reports whether vim mode is on.
func (p *Panel) vimEnabled() bool {
	if p == nil || p.seams.VimEnabled == nil {
		return false
	}
	return p.seams.VimEnabled()
}

// respond echoes lines into the conversation.
func (p *Panel) respond(lines []string) {
	if p == nil || p.seams.Respond == nil {
		return
	}
	p.seams.Respond(lines)
}
