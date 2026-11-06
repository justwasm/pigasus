package resumepicker

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/justwasm/pigasus/internal/app/session/convstore"
	tuiviewport "github.com/justwasm/pigasus/internal/ui/foundations/viewport"
	noticerows "github.com/justwasm/pigasus/internal/ui/rows"
	"github.com/justwasm/pigasus/palette"
)

// The picker's ANSI resets.
//
// 【一手】 agy 1.4.7: the picker's rows end a coloured cell with the
// default foreground and a bold cell with the default attributes.
const (
	keyClose = "\x1b[0m"
	reset    = "\x1b[39m"
)

// The reference's /resume picker colours.
//
// 【一手】 agy 1.4.7, tmux capture (out/resume-tree/ref/tree.txt): the
// cursor and the row under it are the brand blue (38;2;66;133;244), the
// right-hand cells are the dimmed body gray (38;2;60;64;67), the inactive
// tab and the rule above the picker are (38;2;218;220;224) and the hint
// labels are (38;2;95;99;104).
// The row's colours are the selected scheme's slots — the accent, the
// muted foreground, the dim foreground and the border. The palette's
// colours follow the scheme, so they are read when the row is drawn.
func blueOpen() string     { return palette.BlueOpen }
func cellOpen() string     { return palette.SubtleOpen }
func labelOpen() string    { return palette.DimGrayOpen }
func inactiveOpen() string { return palette.SeparatorOpen }

// projectCell is the flat list's project cell.
func projectCell(project string) string {
	project = ansi.Truncate(project, 15, "…")
	return fmt.Sprintf("%*s", 15, project)
}

// deletePrompt is the reference's delete-confirmation prompt.
//
// 【一手】 official-docs/cli/commands/resume.md:55 — "Press F4. A
// `[Delete? (y/n)]` confirmation prompt appears next to the title." The
// binary's own format string is `[Delete? (%s/%s)]` — a string of the
// reference's own table, and the surviving pruned index of that table is
// out/local-macos-1.4.11/strings-index.txt (the full dump is retired).
const deletePrompt = "[Delete? (y/n)]"

// importPrompt is the reference's import-confirmation prompt.
//
// 【一手】 official-docs/cli/commands/resume.md:69 — "Press Enter. A
// confirmation prompt `[Import this conversation? (y/n)]` appears." The
// binary's own format string is `[Import this conversation? (%s/%s)]` — a
// string of the reference's own table, and the surviving pruned index of
// that table is out/local-macos-1.4.11/strings-index.txt (the full dump is
// retired).
//
// The two substitutions of the reference's own format string are 【推测】 to
// be the two key labels, the "y" and the "n" of the prompt the reference
// draws.
const importPrompt = "[Import this conversation? (y/n)]"

// writePrompt writes a confirmation prompt next to a row's title.
func writePrompt(b *strings.Builder, prompt string) {
	if prompt == "" {
		return
	}
	b.WriteString(labelOpen() + prompt + reset)
}

// titleCell is a row's title cell: the title's text and the padding that
// fills the cell.
//
// A confirmation shares the cell with the title: the prompt sits next to
// the title and the title gives up the cells the prompt needs, so the
// row's right-hand cells keep their columns.
func titleCell(title string, width int, prompt string) (string, int) {
	if title == "" {
		title = "(no title)"
	}
	promptWidth := ansi.StringWidth(prompt)
	avail := width
	if prompt != "" {
		avail = width - promptWidth - 1
		if avail < 1 {
			avail = 1
		}
	}
	text := ansi.Truncate(title, avail, "…")
	pad := width - ansi.StringWidth(text)
	if prompt != "" {
		pad -= 1 + promptWidth
	}
	if pad < 0 {
		pad = 0
	}
	return text, pad
}

// stepsCell is a row's step-count cell.
func stepsCell(steps int) string {
	return fmt.Sprintf("%*s", 12, fmt.Sprintf("%d steps", steps))
}

// ageCell is a row's age cell.
func ageCell(mod time.Time) string {
	return fmt.Sprintf("%*s", 12, ageLabel(mod))
}

// ageLoc is the zone the reference prints a calendar date in.
//
// 【一手】 agy 1.4.7: a conversation older than three days shows a date,
// and the reference's own dates are in the Pacific zone.
var ageLoc = func() *time.Location {
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		return time.UTC
	}
	return loc
}()

// ageLabel is the age cell's text: a relative age for the last three
// days and a calendar date beyond that.
//
// 【一手】 agy 1.4.7: the row reads "2h ago", "3d ago" and then "Jan 2".
func ageLabel(mod time.Time) string {
	if mod.IsZero() {
		return ""
	}
	d := time.Since(mod)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 3*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		local := mod.In(ageLoc)
		if local.Year() == time.Now().In(ageLoc).Year() {
			return local.Format("Jan 2")
		}
		return local.Format("Jan 2, 2006")
	}
}

// View draws the picker.
//
// width and height are the window's own size: the picker draws its rows
// to the last column of the window and sizes its list to what is left of
// the height.
func (p *Picker) View(width, height int) string {
	if p == nil || !p.mode {
		return ""
	}
	if width <= 0 {
		width = 220
	}
	var b strings.Builder
	p.renderTabBar(&b, p.tab)
	rows := p.Filtered()
	total := len(rows)
	window := listWindowFor(height)
	storeEmpty := total == 0 && p.query == ""
	p.renderSearchRow(&b, storeEmpty)
	if total == 0 {
		return p.renderEmpty(&b, storeEmpty)
	}
	if p.treeActive() {
		tree := p.treeRows()
		p.clampWindow(len(tree), window)
		p.renderTree(&b, tree, len(tree), window, width)
		return b.String()
	}
	p.clampWindow(total, window)
	p.renderList(&b, rows, total, window, width)
	return b.String()
}

// renderTabBar draws the picker's tab bar.
func (p *Picker) renderTabBar(b *strings.Builder, activeTab int) {
	writeTab := func(name string, active bool) {
		if active {
			b.WriteString("\x1b[1;4m" + blueOpen() + name + keyClose)
			return
		}
		b.WriteString(inactiveOpen() + name + reset)
	}
	b.WriteString("   ")
	writeTab("CLI", activeTab == 0)
	b.WriteString("    ")
	writeTab("Other", activeTab == 1)
	b.WriteString(" " + inactiveOpen() + "  (tab to cycle)" + reset + "\n\n\x1b[1m  Conversations\x1b[0m\n")
}

// renderSearchRow draws the picker's search field.
//
// The Other tab has one too: the reference's own capture of that tab shows
// the search row above the desktop conversations.
//
// 【一手】 agy 1.4.7 (out/resume-tree/ref/tree-other-tab.plain.txt):
//
//	Type to search conversations...
func (p *Picker) renderSearchRow(b *strings.Builder, storeEmpty bool) {
	if storeEmpty && p.tab == 0 {
		return
	}
	if p.query == "" {
		b.WriteString(cellOpen() + "  Type to search conversations..." + reset + "\n")
		return
	}
	b.WriteString(cellOpen() + "  Search: " + reset + p.query)
	if p.cursorOn {
		b.WriteString("\x1b[7m\x1b[37m \x1b[0m")
	} else {
		b.WriteString(" ")
	}
	b.WriteString("\n")
}

// renderEmpty draws the picker's empty state.
func (p *Picker) renderEmpty(b *strings.Builder, storeEmpty bool) string {
	b.WriteString(cellOpen())
	if p.tab == 0 && !storeEmpty {
		b.WriteString("  No matching conversations.")
	} else {
		b.WriteString("  No conversations available.")
	}
	b.WriteString(reset + "\n")
	if p.tab != 0 || storeEmpty {
		b.WriteString(cellOpen() + "  Press Enter to return." + reset + "\n")
	}
	b.WriteString("\n" + keyboardHint(p.tab == 0 && !storeEmpty, false) + "\n\n")
	return b.String()
}

// renderList draws the flat conversation list.
func (p *Picker) renderList(b *strings.Builder, rows []convstore.ConversationMeta, total, window, width int) {
	win := tuiviewport.NewWindow(total, p.window, window)
	curID := p.currentConversationID()
	for i, c := range tuiviewport.Slice(rows, win) {
		if c.ID != "" && c.ID == curID {
			c.Title = "[CURRENT] " + c.Title
		}
		renderRow(b, c, (win.Start()+i) == p.cursor, p.rowPrompt(c.ID), width)
	}
	if total > window {
		fmt.Fprintf(b, "%s  [%d-%d of %d items]%s\n",
			labelOpen(), win.Start()+1, win.End(), total, reset)
	}
	b.WriteString("\n" + p.listHint(rows) + "\n\n")
	if p.importError != "" {
		b.WriteString(noticerows.NoticeDim(p.importError) + "\n")
	}
}

// listHint is the flat list's keyboard hint.
//
// The CLI tab offers the view switch, the rename and the delete; the Other
// tab offers only what the reference's own capture shows.
func (p *Picker) listHint(rows []convstore.ConversationMeta) string {
	if p.tab != 0 {
		return otherHint()
	}
	return p.flatHint(rows)
}

// renderRow draws one conversation of the flat list.
func renderRow(b *strings.Builder, c convstore.ConversationMeta, isCursor bool, prompt string, width int) {
	hasAge := ageLabel(c.ModTime) != ""
	rowRight := 12
	if c.Project != "" {
		rowRight += 17
	}
	if hasAge {
		rowRight += 14
	}
	titleWidth := width - rowRight - 2
	if titleWidth < 1 {
		titleWidth = 1
	}
	title, pad := titleCell(c.Title, titleWidth, prompt)
	if isCursor {
		renderRowCursor(b, title, pad, prompt, c, hasAge)
	} else {
		renderRowNormal(b, title, pad, prompt, c, hasAge)
	}
}

// renderRowCursor draws the row under the cursor.
func renderRowCursor(b *strings.Builder, title string, pad int, prompt string, c convstore.ConversationMeta, hasAge bool) {
	b.WriteString(blueOpen() + "> " + title + reset)
	if prompt != "" {
		b.WriteString(" ")
		writePrompt(b, prompt)
	}
	b.WriteString(strings.Repeat(" ", pad))
	if c.Project != "" {
		b.WriteString(cellOpen() + projectCell(c.Project) + reset + "  ")
	}
	b.WriteString("\x1b[3m" + blueOpen() + stepsCell(c.StepCount))
	if hasAge {
		b.WriteString(keyClose + "  \x1b[3m" + blueOpen() + ageCell(c.ModTime))
	}
	b.WriteString(keyClose + "\n")
}

// renderRowNormal draws a row that is not under the cursor.
func renderRowNormal(b *strings.Builder, title string, pad int, prompt string, c convstore.ConversationMeta, hasAge bool) {
	b.WriteString(keyClose + "  " + title)
	if prompt != "" {
		b.WriteString(" ")
		writePrompt(b, prompt)
	}
	b.WriteString(strings.Repeat(" ", pad) + cellOpen())
	if c.Project != "" {
		b.WriteString(projectCell(c.Project) + "  ")
	}
	b.WriteString(stepsCell(c.StepCount))
	if hasAge {
		b.WriteString("  " + ageCell(c.ModTime))
	}
	b.WriteString(reset + "\n")
}

// flatHint is the flat list's keyboard hint.
//
// 【一手】 agy 1.4.7: the flat list offers "ctrl+f Grouped View", and a
// search keeps the grouped flag — so the filtered list still offers
// "ctrl+f Conversation View" and drops the ctrl+e element.
func (p *Picker) flatHint(rows []convstore.ConversationMeta) string {
	if p.grouped {
		return keyboardHintParts(hintOptions{nav: true, page: true, view: "Conversation View", rename: true, delete: true})
	}
	return keyboardHint(true, projectGroups(rows) >= 2)
}

// keyboardHint is the CLI tab's hint.
//
// withNav adds the navigation keys — the CLI tab with a visible list.
// grouped advertises the workspace tree as the view ctrl+f switches to.
//
// 【一手】 agy 1.4.2 showed "ctrl+f Grouped View" over a store whose rows
// carry project names and omitted it when no visible row has one.
func keyboardHint(withNav, grouped bool) string {
	view := ""
	if grouped {
		view = "Grouped View"
	}
	return keyboardHintParts(hintOptions{nav: withNav, page: withNav, view: view, rename: withNav, delete: withNav})
}

// otherHint is the Other tab's keyboard hint.
//
// The Other tab offers no page, no view switch, no rename and no delete:
// the desktop app's conversations are imported, and the CLI's own store
// holds nothing to rename or to delete.
//
// 【一手】 agy 1.4.7 (out/resume-tree/ref/tree-other-tab.plain.txt):
//
//	Keyboard: ↑/↓ Navigate  enter Select  tab Switch Tab  esc Go back / Clear
//	search
func otherHint() string {
	return keyboardHintParts(hintOptions{nav: true})
}

// hintOptions is the keyboard hint's shape.
//
// Every element the reference's own hint offers is named here, so the two
// tabs' hints come from one place: the CLI tab offers the page, the view
// switch, the rename and the delete, and the Other tab offers none of them.
type hintOptions struct {
	// nav offers the ↑/↓ navigation keys.
	nav bool
	// page offers the ←/→ page keys.
	page bool
	// view names the view ctrl+f switches to.
	view string
	// collapse names the ctrl+e action.
	collapse string
	// rename offers f2.
	rename bool
	// delete offers f4.
	delete bool
}

// keyboardHintParts renders the picker's keyboard hint.
//
// The hint's shape is hintOptions: view names the view ctrl+f switches to
// — "Grouped View" for the workspace tree, "Conversation View" for the
// flat list — and is empty when the picker does not offer the switch;
// collapse names the ctrl+e action.
//
// 【一手】 agy 1.4.7 (out/resume-tree/ref/tree.txt):
//
//	Keyboard: ↑/↓ Navigate  ←/→ Page  ctrl+f Conversation View  ctrl+e
//	Collapse  enter Select  f2 Rename  f4 Delete  tab Switch Tab  esc Go
//	back / Clear search
func keyboardHintParts(o hintOptions) string {
	key := func(s string) string {
		return blueOpen() + s + reset
	}
	label := func(s string) string {
		return labelOpen() + s + reset
	}
	var b strings.Builder
	b.WriteString("\x1b[1m")
	b.WriteString("Keyboard: ")
	b.WriteString("\x1b[0m")
	if o.nav {
		b.WriteString(key("↑/↓") + " " + label("Navigate") + "  ")
	}
	if o.page {
		b.WriteString(key("←/→") + " " + label("Page") + "  ")
	}
	if o.view != "" {
		b.WriteString(key("ctrl+f") + " " + label(o.view) + "  ")
	}
	if o.collapse != "" {
		b.WriteString(key("ctrl+e") + " " + label(o.collapse) + "  ")
	}
	b.WriteString(key("enter") + " " + label("Select") + "  ")
	if o.rename {
		b.WriteString(key("f2") + " " + label("Rename") + "  ")
	}
	if o.delete {
		b.WriteString(key("f4") + " " + label("Delete") + "  ")
	}
	b.WriteString(key("tab") + " " + label("Switch Tab") + "  ")
	b.WriteString(key("esc") + " " + label("Go back / Clear search"))
	return b.String()
}
