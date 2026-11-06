// Package resumepicker is the /resume picker: the overlay that lists the
// conversations in the store, either as a flat filtered list or as the
// reference's workspace tree.
//
// The picker owns its own state — the list, the cursor, the search query,
// the tree — and asks the session that owns it for the conversations and
// for the two actions it triggers (resume, rename). The host assembles
// those seams; the picker never reaches into the host.
//
// 【一手】 agy 1.4.7, tmux capture at 200x50
// (out/resume-tree/ref/tree.txt): the reference opens the picker on the
// workspace tree — one row per workspace group and the group's
// conversations indented underneath it.
package resumepicker

import (
	"errors"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/internal/app/session/convstore"
	resumetree "github.com/justwasm/pigasus/internal/app/session/tree"
	tuiviewport "github.com/justwasm/pigasus/internal/ui/foundations/viewport"
)

// Seams are what the picker needs from the session that owns it.
//
// The picker is presentation: it draws the conversations and it asks the
// session to act on the one the user picked. It never reads the session's
// own state.
type Seams struct {
	// CurrentConversationID is the conversation the session is showing.
	// The picker keeps it out of its own list and pins its workspace
	// group to the top of the tree.
	CurrentConversationID func() string
	// ListConversations returns the store's conversations, minus the
	// session's own conversation.
	ListConversations func() ([]convstore.ConversationMeta, error)
	// ResumeConversation switches the session to the conversation.
	ResumeConversation func(meta convstore.ConversationMeta) tea.Cmd
	// RenameConversation opens the rename panel for the conversation.
	RenameConversation func(meta convstore.ConversationMeta)
}

// Picker is the /resume picker's state.
type Picker struct {
	seams Seams

	// mode is true while the picker is open.
	mode bool
	// list is the store's conversations, minus the session's own.
	list []convstore.ConversationMeta
	// cursor is the row the picker is on.
	cursor int
	// tab is the picker's tab — 0 is the CLI's own conversations, 1 the
	// other sources.
	tab int
	// query is the search text.
	query string
	// window is the index of the first drawn row.
	window int
	// cursorOn is the blink phase of the search cursor.
	cursorOn bool
	// grouped is the picker's view: the workspace tree, or the flat
	// conversation list.
	grouped bool
	// expanded holds the expanded workspace groups, by their key.
	expanded map[string]bool
	// showAll holds the workspace groups that have been expanded to show all items.
	showAll map[string]bool
	// groups is the computed tree.
	groups []resumetree.Group
	// deleteConfirm is the conversation id the delete confirmation is
	// armed for. It is empty when no confirmation is pending.
	//
	// 【一手】 official-docs/cli/commands/resume.md:55 — "Press F4. A
	// `[Delete? (y/n)]` confirmation prompt appears next to the title."
	deleteConfirm string
	// importConfirm is the conversation id the import confirmation is
	// armed for. It is empty when no confirmation is pending.
	//
	// 【一手】 official-docs/cli/commands/resume.md:69 — "Press Enter. A
	// confirmation prompt `[Import this conversation? (y/n)]` appears."
	importConfirm string
	// importError is the message of an import that failed. It is drawn
	// under the Other tab's rows and cleared when the tab is left.
	importError string
	// other is the Antigravity desktop app's conversations — the rows the
	// Other tab draws. They are read the first time the user switches to
	// that tab.
	//
	// 【一手】 official-docs/cli/commands/resume.md:67 — "The **Other** tab
	// lists the conversations that you started in the Antigravity App".
	other []convstore.ConversationMeta
	// otherPreviews holds the first prompt's text of every desktop
	// conversation, by conversation id. The Other tab's search matches it
	// the way the CLI tab's search matches its own.
	otherPreviews map[string]string
	// otherLoaded is true once the Other tab has been read.
	otherLoaded bool
	// previews holds the first prompt's text of every conversation the
	// picker knows, by conversation id.
	//
	// 【一手】 official-docs/cli/commands/resume.md:28 — the search
	// filters on "the first line of the first prompt".
	previews map[string]string
}

// New returns a picker bound to the session's seams.
func New(seams Seams) *Picker {
	return &Picker{
		seams:    seams,
		expanded: map[string]bool{},
		showAll:  map[string]bool{},
	}
}

// showAllGroup marks a group to show all of its conversations.
func (p *Picker) showAllGroup(key string) {
	if p.showAll == nil {
		p.showAll = map[string]bool{}
	}
	p.showAll[key] = true
}

// Active reports whether the picker is open.
//
// A nil picker is closed: the session may own no picker at all.
func (p *Picker) Active() bool {
	return p != nil && p.mode
}

// Open opens the picker on the store's conversations.
//
// 【一手】 agy 1.4.7: the picker opens on the workspace tree with the group
// of the current conversation expanded and the cursor on its first
// conversation.
func (p *Picker) Open() error {
	if p == nil {
		return errors.New("no resume picker")
	}
	list, err := p.seams.ListConversations()
	if err != nil {
		p.mode = false
		return err
	}
	p.list = list
	p.cursor = 0
	p.window = 0
	p.query = ""
	p.tab = 0
	p.cursorOn = false
	p.deleteConfirm = ""
	p.importConfirm = ""
	p.importError = ""
	p.other = nil
	p.otherPreviews = nil
	p.otherLoaded = false
	p.mode = true
	p.resetTreeState()
	p.buildGroups()
	p.expandFirstGroup()
	return nil
}

// Close closes the picker.
func (p *Picker) Close() {
	p.mode = false
	p.list = nil
	p.cursor, p.window, p.tab = 0, 0, 0
	p.deleteConfirm = ""
	p.importConfirm = ""
	p.importError = ""
	p.other = nil
	p.otherPreviews = nil
	p.otherLoaded = false
}

// Query returns the picker's search text.
func (p *Picker) Query() string {
	if p == nil {
		return ""
	}
	return p.query
}

// Cursor returns the row the picker is on.
func (p *Picker) Cursor() int {
	if p == nil {
		return 0
	}
	return p.cursor
}

// List returns the conversations the picker holds.
func (p *Picker) List() []convstore.ConversationMeta {
	if p == nil {
		return nil
	}
	return p.list
}

// CursorOn reports the blink phase of the search cursor.
func (p *Picker) CursorOn() bool {
	if p == nil {
		return false
	}
	return p.cursorOn
}

// SetCursorOn sets the blink phase of the search cursor.
func (p *Picker) SetCursorOn(on bool) {
	if p == nil {
		return
	}
	p.cursorOn = on
}

// TypeText inserts a character the session's own input path delivered.
//
// The session routes a typed character to the picker when the picker is
// open, so the search field grows without the character reaching the
// prompt.
func (p *Picker) TypeText(s string) {
	if p == nil {
		return
	}
	p.query += s
	p.window, p.cursor = 0, 0
}

// currentConversationID is the conversation the session is showing.
func (p *Picker) currentConversationID() string {
	if p.seams.CurrentConversationID == nil {
		return ""
	}
	return p.seams.CurrentConversationID()
}

// reload re-reads the store and rebuilds the tree.
func (p *Picker) reload() {
	if p.seams.ListConversations == nil {
		return
	}
	list, err := p.seams.ListConversations()
	if err != nil {
		p.list = nil
		return
	}
	p.list = list
	p.buildGroups()
}

// resume asks the session to switch to the conversation.
func (p *Picker) resume(meta convstore.ConversationMeta) tea.Cmd {
	if p.seams.ResumeConversation == nil {
		return nil
	}
	return p.seams.ResumeConversation(meta)
}

// switchTab moves to the next tab.
//
// 【一手】 agy 1.4.7: the tab bar reads "CLI   Other   (tab to cycle)" and
// the Tab key moves between the two.
func (p *Picker) switchTab() {
	p.tab = (p.tab + 1) % 2
	p.window, p.cursor = 0, 0
	p.importError = ""
	if p.tab != 0 {
		p.loadOther()
	}
}

// loadOther reads the Antigravity desktop app's conversations.
//
// The read happens the first time the user switches to the Other tab: the
// desktop app's store is another product's directory, and a user who never
// opens that tab never pays for the scan.
func (p *Picker) loadOther() {
	if p.otherLoaded {
		return
	}
	p.otherLoaded = true
	list, previews, err := convstore.ListDesktopConversations()
	if err != nil {
		p.importError = err.Error()
		return
	}
	p.other = list
	p.otherPreviews = previews
}

// otherMeta finds the desktop conversation an id names.
func (p *Picker) otherMeta(id string) (convstore.ConversationMeta, bool) {
	for _, c := range p.other {
		if c.ID == id {
			return c, true
		}
	}
	return convstore.ConversationMeta{}, false
}

// rowPrompt is the confirmation the row under the cursor carries.
//
// The CLI tab's row can ask to be deleted; the Other tab's row can ask to
// be imported. An unarmed row carries no prompt at all.
func (p *Picker) rowPrompt(id string) string {
	if id == "" {
		return ""
	}
	if p.deleteConfirm == id {
		return deletePrompt
	}
	if p.importConfirm == id {
		return importPrompt
	}
	return ""
}

// rename asks the session to open its rename panel for the conversation.
func (p *Picker) rename(meta convstore.ConversationMeta) {
	if p.seams.RenameConversation == nil {
		return
	}
	p.seams.RenameConversation(meta)
}

// Filtered returns the rows the flat list draws: the tab's
// conversations, narrowed by the search query.
//
// The CLI tab holds the store's own conversations; the Other tab holds the
// Antigravity desktop app's.
func (p *Picker) Filtered() []convstore.ConversationMeta {
	if p.tab != 0 {
		return filterConversations(p.other, p.otherPreviews, p.query)
	}
	return filterConversations(p.list, p.previews, p.query)
}

// filterConversations narrows a list of conversations by the search query.
func filterConversations(list []convstore.ConversationMeta, previews map[string]string, query string) []convstore.ConversationMeta {
	if query == "" {
		return list
	}
	q := strings.ToLower(query)
	out := make([]convstore.ConversationMeta, 0, len(list))
	for _, c := range list {
		if matchesQuery(c, q, previews) {
			out = append(out, c)
		}
	}
	return out
}

// matchesQuery reports whether the conversation answers the search.
//
// The search is a case-insensitive substring match over three fields: the
// title, the first line of the first prompt and the conversation id.
//
// 【一手】 official-docs/cli/commands/resume.md:28 — "Start typing to
// instantly filter conversations by their title, the first line of the
// first prompt, or conversation ID."
func matchesQuery(c convstore.ConversationMeta, q string, previews map[string]string) bool {
	if strings.Contains(strings.ToLower(c.Title), q) {
		return true
	}
	if strings.Contains(strings.ToLower(c.ID), q) {
		return true
	}
	if prompt := promptFirstLine(previews, c.ID); prompt != "" {
		return strings.Contains(strings.ToLower(prompt), q)
	}
	return false
}

// promptFirstLine is the first line of the conversation's first prompt.
//
// The reference's index holds the raw first user message in its preview
// column; the search matches its first line, with the line's own
// whitespace collapsed the way the row label collapses it.
func promptFirstLine(previews map[string]string, id string) string {
	preview := previews[id]
	if preview == "" {
		return ""
	}
	if i := strings.IndexAny(preview, "\n\r"); i >= 0 {
		preview = preview[:i]
	}
	return strings.Join(strings.Fields(preview), " ")
}

// metaFor finds the conversation a tree row points at.
func (p *Picker) metaFor(id string) convstore.ConversationMeta {
	for _, c := range p.list {
		if c.ID == id {
			return c
		}
	}
	return convstore.ConversationMeta{ID: id}
}

// listWindowFor is how many rows the picker draws.
//
// The reference's picker leaves the tab bar, the search row, the row
// counter and the keyboard hint around the list, so the list itself gets
// whatever is left — at most ten rows.
func listWindowFor(height int) int {
	if height <= 0 {
		return 10
	}
	n := height - 12
	if n > 10 {
		n = 10
	}
	if n < 1 {
		n = 1
	}
	return n
}

// listDown moves the cursor one row down.
func (p *Picker) listDown(total, height int) {
	nav := tuiviewport.ListNav{Cursor: p.cursor, Window: p.window}
	nav.MoveDown(1, total, listWindowFor(height))
	p.cursor, p.window = nav.Cursor, nav.Window
}

// listUp moves the cursor one row up.
func (p *Picker) listUp(total, height int) {
	nav := tuiviewport.ListNav{Cursor: p.cursor, Window: p.window}
	nav.MoveUp(1, total, listWindowFor(height))
	p.cursor, p.window = nav.Cursor, nav.Window
}

// listPage pages the cursor by one screen.
func (p *Picker) listPage(total, dir, height int) {
	size := listWindowFor(height)
	nav := tuiviewport.ListNav{Cursor: p.cursor, Window: p.window}
	if dir < 0 {
		nav.PageUp(total, size)
	} else {
		nav.PageDown(total, size)
	}
	p.cursor, p.window = nav.Cursor, nav.Window
}

// clampWindow keeps the cursor and the window inside the rows.
func (p *Picker) clampWindow(total, window int) {
	nav := tuiviewport.ListNav{Cursor: p.cursor, Window: p.window}
	nav.Sync(total, window)
	p.cursor, p.window = nav.Cursor, nav.Window
}

// clampCursor keeps the cursor inside the rows after the list changed.
func (p *Picker) clampCursor(total int) {
	nav := tuiviewport.ListNav{Cursor: p.cursor, Window: p.window}
	nav.Sync(total, 0)
	p.cursor, p.window = nav.Cursor, nav.Window
}

// syncWindow keeps the visible window around the cursor.
func (p *Picker) syncWindow(total, height int) {
	nav := tuiviewport.ListNav{Cursor: p.cursor, Window: p.window}
	nav.Sync(total, listWindowFor(height))
	p.cursor, p.window = nav.Cursor, nav.Window
}

// projectGroups counts the distinct workspaces the rows carry.
func projectGroups(rows []convstore.ConversationMeta) int {
	seen := make(map[string]bool, len(rows))
	for _, r := range rows {
		if r.Project != "" {
			seen[r.Project] = true
		}
	}
	return len(seen)
}
