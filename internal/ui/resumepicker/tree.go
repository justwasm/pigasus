package resumepicker

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	resumetree "github.com/justwasm/pigasus/internal/app/session/tree"
	"github.com/justwasm/pigasus/internal/ui/editor/keybind"
	tuiviewport "github.com/justwasm/pigasus/internal/ui/foundations/viewport"
)

// The reference's /resume picker (agy 1.4.7) draws the conversations as a
// workspace tree: one row per workspace group, and the group's
// conversations indented underneath it.
//
// 【一手】 agy 1.4.7, tmux capture at 200x50
// (out/resume-tree/ref/tree.txt):
//
//	  ▾ gear                                    [1 conv]        2h ago
//	>   南昌天气查询                            17 steps        2h ago
//	  ▸ dir                                     [3 convs]        1h ago
//
// The geometry, measured from that capture:
//
//	group row  2 cells of cursor + 2 cells of marker + the label + a
//	           12-cell count + a 14-cell age. The row reaches the last
//	           column of the window.
//	child row  2 cells of cursor + 2 cells of indent + the title + a
//	           12-cell step count + 2 cells of gap + a 12-cell age. The
//	           right-hand block is as wide as the group row's, so the row
//	           reaches the last column too.
//
// 【一手】 The tree keeps at most five conversations per group. Expanding
// the 486-conversation "[No Workspace]" group grows the model from 12 to 15
// rows — five of its conversations, not all 486.
const (
	// treeMaxChildren is how many conversations the reference keeps
	// per group in the tree model before collapsing the remainder.
	treeMaxChildren = 4
	// treeCountCell is the width of a group row's "[N convs]" cell.
	treeCountCell = 12
	// treeGroupAgeCell is the width of a group row's age cell.
	treeGroupAgeCell = 14
	// treeStepsCell is the width of a child row's step count cell.
	treeStepsCell = 12
	// treeChildAgeCell is the width of a child row's age cell.
	treeChildAgeCell = 12
)

// treeRow is one row of the tree: a group row, one of the group's
// conversations, or an expand-all "more" row.
type treeRow struct {
	group     *resumetree.Group
	item      *resumetree.Item
	isMore    bool
	moreCount int
}

// treeActive reports whether the picker draws the workspace tree.
//
// The tree is the reference's default view (agy 1.4.7). A search switches
// the picker to the flat filtered list and keeps the grouped flag, so the
// tree only shows when there is no query.
func (p *Picker) treeActive() bool {
	return p.mode && p.tab == 0 && p.grouped && p.query == ""
}

// treeRows flattens the groups into the rows the picker draws: one
// row per group, followed by the group's conversations when the group is
// expanded.
func (p *Picker) treeRows() []treeRow {
	rows := make([]treeRow, 0, len(p.groups))
	for i := range p.groups {
		g := &p.groups[i]
		rows = append(rows, treeRow{group: g})
		if !p.expanded[g.Key] {
			continue
		}
		if p.showAll != nil && p.showAll[g.Key] {
			for j := range g.Items {
				rows = append(rows, treeRow{group: g, item: &g.Items[j]})
			}
			continue
		}
		limit := len(g.Items)
		if limit > treeMaxChildren {
			limit = treeMaxChildren
		}
		for j := 0; j < limit; j++ {
			rows = append(rows, treeRow{group: g, item: &g.Items[j]})
		}
		if len(g.Items) > treeMaxChildren {
			rows = append(rows, treeRow{
				group:     g,
				isMore:    true,
				moreCount: len(g.Items) - treeMaxChildren,
			})
		}
	}
	return rows
}

// cursorGroup is the group the cursor sits in — the group row itself
// or the parent of a conversation row.
func cursorGroup(rows []treeRow, cursor int) *resumetree.Group {
	if cursor < 0 || cursor >= len(rows) {
		return nil
	}
	return rows[cursor].group
}

// treeCountText is the "[N conv]" / "[N convs]" cell of a group row.
func treeCountText(n int) string {
	if n == 1 {
		return "[1 conv]"
	}
	return fmt.Sprintf("[%d convs]", n)
}

// treeCountWidth is the width of the group rows' count cell: the
// reference's fixed eleven cells, or the widest count when a group outgrows
// it.
func treeCountWidth(groups []resumetree.Group) int {
	width := treeCountCell
	for _, g := range groups {
		if n := ansi.StringWidth(treeCountText(len(g.Items))); n > width {
			width = n
		}
	}
	return width
}

// renderTreeGroupRow draws one group row.
//
// 【一手】 The reference bolds the label of a group that is not under the
// cursor and paints the whole row blue when it is.
func (p *Picker) renderTreeGroupRow(b *strings.Builder, g *resumetree.Group, isCursor bool, width, countWidth int) {
	marker := "▸ "
	if p.expanded[g.Key] {
		marker = "▾ "
	}
	labelWidth := width - 4 - countWidth - treeGroupAgeCell
	if labelWidth < 1 {
		labelWidth = 1
	}
	label := ansi.Truncate(g.Label, labelWidth, "…")
	pad := labelWidth - ansi.StringWidth(label)
	if pad < 0 {
		pad = 0
	}
	count := fmt.Sprintf("%*s", countWidth, treeCountText(len(g.Items)))
	age := fmt.Sprintf("%*s", treeGroupAgeCell, ageLabel(g.LastActive))
	if isCursor {
		b.WriteString(blueOpen() + "> " + marker + label + reset)
	} else {
		b.WriteString("  \x1b[1m" + marker + label + keyClose)
	}
	b.WriteString(strings.Repeat(" ", pad))
	b.WriteString(cellOpen() + count + age + reset + "\n")
}

// renderTreeChildRow draws one conversation of a group.
//
// 【一手】 The reference paints the title blue and the two right-hand cells
// italic when the row is under the cursor, and leaves the whole row in the
// default colours otherwise.
func (p *Picker) renderTreeChildRow(b *strings.Builder, item *resumetree.Item, isCursor bool, prompt string, width int) {
	// The child row's right-hand block is the same width as the group
	// row's — the step count, a gap and the age — so both rows end at the
	// last column of the window and both titles are the same width.
	//
	// 【一手】 agy 1.4.7 at 200 columns: the title starts at column 4, the
	// step count ends at column 185 and the age at column 199.
	titleWidth := width - 4 - treeStepsCell - 2 - treeChildAgeCell
	if titleWidth < 1 {
		titleWidth = 1
	}
	title := item.Title
	if item.ID != "" && item.ID == p.currentConversationID() {
		title = "[CURRENT] " + title
	}
	title, pad := titleCell(title, titleWidth, prompt)
	steps := fmt.Sprintf("%*s", treeStepsCell, fmt.Sprintf("%d steps", item.Steps))
	age := fmt.Sprintf("%*s", treeChildAgeCell, ageLabel(item.Time))
	if isCursor {
		b.WriteString(blueOpen() + "> " + reset + "  " + blueOpen() + title)
		if prompt != "" {
			b.WriteString(" ")
			writePrompt(b, prompt)
			// The prompt's own colour closed the row's blue; the
			// two right-hand cells carry it.
			b.WriteString(blueOpen())
		}
		b.WriteString(strings.Repeat(" ", pad))
		b.WriteString("\x1b[3m" + steps + keyClose + "  \x1b[3m" + blueOpen() + age + keyClose + "\n")
		return
	}
	b.WriteString("    " + title)
	if prompt != "" {
		b.WriteString(" ")
		writePrompt(b, prompt)
	}
	b.WriteString(strings.Repeat(" ", pad))
	b.WriteString(cellOpen() + steps + "  " + age + reset + "\n")
}

// renderTreeMoreRow draws the "⋯ N more in <workspace> — enter to show" row.
func (p *Picker) renderTreeMoreRow(b *strings.Builder, g *resumetree.Group, count int, isCursor bool, width int) {
	text := fmt.Sprintf("⋯ %d more in %s", count, g.Label)
	if width >= 70 {
		text += " — enter to show"
	}
	if isCursor {
		b.WriteString(blueOpen() + "> " + reset + "  " + blueOpen() + text + reset + "\n")
		return
	}
	b.WriteString("    " + labelOpen() + text + reset + "\n")
}

// renderTree draws the tree and its keyboard hint.
func (p *Picker) renderTree(b *strings.Builder, rows []treeRow, total, window, width int) {
	win := tuiviewport.NewWindow(total, p.window, window)
	countWidth := treeCountWidth(p.groups)
	for i, row := range tuiviewport.Slice(rows, win) {
		idx := win.Start() + i
		if row.isMore {
			p.renderTreeMoreRow(b, row.group, row.moreCount, idx == p.cursor, width)
			continue
		}
		if row.item == nil {
			p.renderTreeGroupRow(b, row.group, idx == p.cursor, width, countWidth)
			continue
		}
		p.renderTreeChildRow(b, row.item, idx == p.cursor, p.rowPrompt(row.item.ID), width)
	}
	if total > window {
		fmt.Fprintf(b, "%s  [%d-%d of %d items]%s\n",
			labelOpen(), win.Start()+1, win.End(), total, reset)
	}
	b.WriteString("\n" + p.treeHint(rows) + "\n\n")
}

// treeHint is the tree's keyboard hint.
//
// 【一手】 agy 1.4.7: the tree offers "ctrl+f Conversation View" and
// "ctrl+e Collapse" — the latter turns into "Expand" when the group under
// the cursor is collapsed.
func (p *Picker) treeHint(rows []treeRow) string {
	collapse := "Expand"
	if g := cursorGroup(rows, p.cursor); g != nil && p.expanded[g.Key] {
		collapse = "Collapse"
	}
	return keyboardHintParts(hintOptions{nav: true, page: true, view: "Conversation View", collapse: collapse, rename: true, delete: true})
}

// buildGroups rebuilds the workspace tree from the store.
//
// The workspace of a conversation comes from the reference's conversation
// index; the conversations themselves come from the conversation list the
// picker already holds.
func (p *Picker) buildGroups() {
	if len(p.list) == 0 {
		p.groups = nil
		p.previews = nil
		return
	}
	ids := make([]string, 0, len(p.list)+1)
	for _, c := range p.list {
		ids = append(ids, c.ID)
	}
	if cur := p.currentConversationID(); cur != "" {
		ids = append(ids, cur)
	}
	summaries := resumetree.LookupSummaries(ids)
	cache := resumetree.LookupCacheSummaries(ids)
	items := make([]resumetree.Item, 0, len(p.list))
	p.previews = make(map[string]string, len(p.list))
	for _, c := range p.list {
		item := resumetree.Item{
			ID:    c.ID,
			Title: c.Title,
			Steps: c.StepCount,
			Time:  c.ModTime,
		}
		summary, ok := summaries[c.ID]
		if !ok {
			summary, ok = cache[c.ID]
		}
		if ok {
			item.WorkspaceURIs = summary.WorkspaceURIs
			if !summary.LastModifiedTime.IsZero() {
				item.Time = summary.LastModifiedTime
			}
			// The preview is the raw first user message; the
			// search matches its first line.
			p.previews[c.ID] = summary.Preview
		} else if c.ID == p.currentConversationID() && p.seams.CurrentWorkspace != nil {
			if ws := p.seams.CurrentWorkspace(); ws != "" {
				item.WorkspaceURIs = []string{"file://" + ws}
			}
		}
		items = append(items, item)
	}
	pinned := p.pinnedKey(summaries, cache)
	p.groups = resumetree.Build(items, pinned)
	if p.expanded == nil {
		p.expanded = map[string]bool{}
	}
}

// pinnedKey is the workspace group the reference keeps at the top of
// the tree: the group that holds the conversation the session is showing.
//
// 【反编译】 model.preferredGroup (0x102af8a20) picks that group and
// model.defaultExpandedGroup (0x102af8b90) expands it.
//
// 【一手】 The reference's live picker keeps the "/Users/gear" group first
// whatever its timestamps say — that group holds the conversation the
// reference's session was showing.
func (p *Picker) pinnedKey(summaries, cache map[string]resumetree.Summary) string {
	cur := p.currentConversationID()
	if cur != "" {
		summary, ok := summaries[cur]
		if !ok && cache != nil {
			summary, ok = cache[cur]
		}
		if ok && len(summary.WorkspaceURIs) > 0 {
			return resumetree.ItemWorkspaceURI(summary.WorkspaceURIs)
		}
	}
	if p.seams.CurrentWorkspace != nil {
		if ws := p.seams.CurrentWorkspace(); ws != "" {
			return resumetree.NormalizeWorkspaceURI(ws)
		}
	}
	return ""
}

// resetTreeState prepares the tree of a freshly opened picker.
//
// 【一手】 The reference opens the tree with the group of the current
// conversation expanded and the cursor on its first conversation.
func (p *Picker) resetTreeState() {
	p.grouped = true
	p.expanded = map[string]bool{}
	p.showAll = map[string]bool{}
	p.groups = nil
	p.cursor = 0
	p.window = 0
}

// expandFirstGroup expands the group the reference opens the tree
// with and puts the cursor on its first conversation.
//
// 【一手】 agy 1.4.7: the tree opens with the group of the current
// conversation expanded and the cursor on that conversation.
func (p *Picker) expandFirstGroup() {
	if len(p.groups) == 0 {
		return
	}
	first := p.groups[0]
	p.expandGroup(first.Key, true)
	if len(first.Items) > 0 {
		p.cursor = 1
		p.window = 0
	}
}

// expandGroup expands or collapses a group.
func (p *Picker) expandGroup(key string, expanded bool) {
	if p.expanded == nil {
		p.expanded = map[string]bool{}
	}
	p.expanded[key] = expanded
	if !expanded && p.showAll != nil {
		delete(p.showAll, key)
	}
}

// treeTarget is what the cursor points at: a group row, or a
// conversation inside a group.
type treeTarget struct {
	valid  bool
	group  string
	item   string
	isMore bool
}

// treeCursorTarget remembers what the cursor points at, so the cursor
// can follow it when the tree changes shape — a group that expands adds rows
// under itself, a group that collapses takes them away.
func treeCursorTarget(rows []treeRow, cursor int) treeTarget {
	if cursor < 0 || cursor >= len(rows) {
		return treeTarget{}
	}
	row := rows[cursor]
	if row.isMore {
		return treeTarget{valid: true, group: row.group.Key, isMore: true}
	}
	if row.item == nil {
		return treeTarget{valid: true, group: row.group.Key}
	}
	return treeTarget{valid: true, group: row.group.Key, item: row.item.ID}
}

// treeFocusTarget moves the cursor back onto a remembered target. A
// conversation whose group collapsed is gone, so the cursor falls back to
// the group row.
func (p *Picker) treeFocusTarget(rows []treeRow, target treeTarget) {
	if !target.valid {
		return
	}
	groupRow := -1
	for i, row := range rows {
		if row.group.Key != target.group {
			continue
		}
		if groupRow < 0 {
			groupRow = i
		}
		if target.isMore && row.isMore {
			p.cursor = i
			return
		}
		if target.item != "" && row.item != nil && row.item.ID == target.item {
			p.cursor = i
			return
		}
	}
	if groupRow >= 0 {
		p.cursor = groupRow
	}
}

// toggleGroupAt expands or collapses a group and keeps the cursor on
// whatever it pointed at.
func (p *Picker) toggleGroupAt(rows []treeRow, cursor int) {
	target := treeCursorTarget(rows, cursor)
	if !target.valid {
		return
	}
	p.expandGroup(target.group, !p.expanded[target.group])
	p.treeFocusTarget(p.treeRows(), target)
}

// toggleGrouped switches between the workspace tree and the flat
// conversation list, keeping the cursor on the same conversation.
//
// 【一手】 agy 1.4.7: ctrl+f switches the view. The tree's hint calls the
// flat list the "Conversation View", the flat list's hint calls the tree
// the "Grouped View".
func (p *Picker) toggleGrouped(height int) {
	id := p.cursorConversationID()
	p.grouped = !p.grouped
	p.window, p.cursor = 0, 0
	p.focusConversation(id, height)
}

// cursorConversationID is the conversation under the cursor, or the
// empty string when the cursor is not on one.
func (p *Picker) cursorConversationID() string {
	if p.treeActive() {
		rows := p.treeRows()
		if p.cursor >= 0 && p.cursor < len(rows) && rows[p.cursor].item != nil {
			return rows[p.cursor].item.ID
		}
		return ""
	}
	rows := p.Filtered()
	if p.cursor >= 0 && p.cursor < len(rows) {
		return rows[p.cursor].ID
	}
	return ""
}

// focusConversation moves the cursor onto a conversation.
func (p *Picker) focusConversation(id string, height int) {
	if id == "" {
		return
	}
	if p.treeActive() {
		rows := p.treeRows()
		for i, row := range rows {
			if row.item != nil && row.item.ID == id {
				p.cursor = i
				p.syncWindow(len(rows), height)
				return
			}
		}
		return
	}
	rows := p.Filtered()
	for i, r := range rows {
		if r.ID == id {
			p.cursor = i
			p.syncWindow(len(rows), height)
			return
		}
	}
}

// handleTreeKeys handles the keys of the workspace tree.
//
// 【一手】 agy 1.4.7:
//
//   - enter on a group row expands or collapses the group and keeps the
//     cursor on the group row;
//   - ctrl+e expands or collapses the group under the cursor — the hint
//     reads "Collapse" while that group is expanded;
//   - f2 and f4 do nothing on a group row.
func (p *Picker) handleTreeKeys(key tea.Key, height int) tea.Cmd {
	rows := p.treeRows()
	total := len(rows)
	switch key.Code {
	case tea.KeyUp:
		p.listUp(total, height)
		return nil
	case tea.KeyDown:
		p.listDown(total, height)
		return nil
	case tea.KeyRight:
		p.listPage(total, 1, height)
		return nil
	case tea.KeyLeft:
		p.listPage(total, -1, height)
		return nil
	case tea.KeyTab:
		p.switchTab()
		return nil
	case tea.KeyEsc:
		if p.query != "" {
			p.query = ""
			p.window, p.cursor = 0, 0
			return nil
		}
		p.Close()
		return nil
	case tea.KeyEnter:
		if total == 0 || p.cursor < 0 || p.cursor >= total {
			p.Close()
			return nil
		}
		row := rows[p.cursor]
		if row.isMore {
			p.showAllGroup(row.group.Key)
			return nil
		}
		if row.item == nil {
			p.toggleGroupAt(rows, p.cursor)
			return nil
		}
		picked := p.metaFor(row.item.ID)
		p.Close()
		return p.resume(picked)
	case tea.KeyF2, tea.KeyF4:
		if total == 0 || p.cursor < 0 || p.cursor >= total {
			return nil
		}
		row := rows[p.cursor]
		if row.item == nil || row.isMore {
			// f2 and f4 act on a conversation; a group or more row has none.
			return nil
		}
		picked := p.metaFor(row.item.ID)
		if key.Code == tea.KeyF4 {
			if picked.ID != "" {
				// The delete asks first: F4 arms the confirmation
				// and the picker draws "[Delete? (y/n)]" next to
				// the row's title.
				p.deleteConfirm = picked.ID
			}
			return nil
		}
		p.Close()
		p.rename(picked)
		return nil
	}
	if key.Code == 'f' && key.Mod.Contains(tea.ModCtrl) {
		p.toggleGrouped(height)
		return nil
	}
	if key.Code == 'e' && key.Mod.Contains(tea.ModCtrl) {
		p.toggleGroupAt(rows, p.cursor)
		return nil
	}
	if key.Code == 'u' && key.Mod.Contains(tea.ModCtrl) {
		p.query = ""
		p.window, p.cursor = 0, 0
		return nil
	}
	if keybind.IsTypedText(key) {
		p.query += key.Text
		p.window, p.cursor = 0, 0
		p.cursorOn = true
		return BlinkCmd()
	}
	return nil
}
