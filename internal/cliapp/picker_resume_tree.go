package cliapp

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/justwasm/pigasus/internal/convstore"
	"github.com/justwasm/pigasus/internal/keybind"
	"github.com/justwasm/pigasus/internal/resumetree"
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
	// resumeTreeMaxChildren is how many conversations the reference keeps
	// per group in the tree model.
	resumeTreeMaxChildren = 5
	// resumeTreeCountCell is the width of a group row's "[N convs]" cell.
	resumeTreeCountCell = 12
	// resumeTreeGroupAgeCell is the width of a group row's age cell.
	resumeTreeGroupAgeCell = 14
	// resumeTreeStepsCell is the width of a child row's step count cell.
	resumeTreeStepsCell = 12
	// resumeTreeChildAgeCell is the width of a child row's age cell.
	resumeTreeChildAgeCell = 12
)

// resumeTreeRow is one row of the tree: a group row, or one of the group's
// conversations.
type resumeTreeRow struct {
	group *resumetree.Group
	item  *resumetree.Item
}

// resumeTreeActive reports whether the picker draws the workspace tree.
//
// The tree is the reference's default view (agy 1.4.7). A search switches
// the picker to the flat filtered list and keeps the grouped flag, so the
// tree only shows when there is no query.
func (m *runMode) resumeTreeActive() bool {
	return m.resumeMode && m.resumeTab == 0 && m.resumeGrouped && m.resumeQuery == ""
}

// resumeTreeRows flattens the groups into the rows the picker draws: one
// row per group, followed by the group's conversations when the group is
// expanded.
func (m *runMode) resumeTreeRows() []resumeTreeRow {
	rows := make([]resumeTreeRow, 0, len(m.resumeGroups))
	for i := range m.resumeGroups {
		g := &m.resumeGroups[i]
		rows = append(rows, resumeTreeRow{group: g})
		if !m.resumeExpanded[g.Key] {
			continue
		}
		limit := len(g.Items)
		if limit > resumeTreeMaxChildren {
			limit = resumeTreeMaxChildren
		}
		for j := 0; j < limit; j++ {
			rows = append(rows, resumeTreeRow{group: g, item: &g.Items[j]})
		}
	}
	return rows
}

// resumeCursorGroup is the group the cursor sits in — the group row itself
// or the parent of a conversation row.
func resumeCursorGroup(rows []resumeTreeRow, cursor int) *resumetree.Group {
	if cursor < 0 || cursor >= len(rows) {
		return nil
	}
	return rows[cursor].group
}

// resumeTreeCountText is the "[N conv]" / "[N convs]" cell of a group row.
func resumeTreeCountText(n int) string {
	if n == 1 {
		return "[1 conv]"
	}
	return fmt.Sprintf("[%d convs]", n)
}

// resumeTreeCountWidth is the width of the group rows' count cell: the
// reference's fixed eleven cells, or the widest count when a group outgrows
// it.
func resumeTreeCountWidth(groups []resumetree.Group) int {
	width := resumeTreeCountCell
	for _, g := range groups {
		if n := ansi.StringWidth(resumeTreeCountText(len(g.Items))); n > width {
			width = n
		}
	}
	return width
}

// renderResumeTreeGroupRow draws one group row.
//
// 【一手】 The reference bolds the label of a group that is not under the
// cursor and paints the whole row blue when it is.
func (m *runMode) renderResumeTreeGroupRow(b *strings.Builder, g *resumetree.Group, isCursor bool, width, countWidth int) {
	marker := "▸ "
	if m.resumeExpanded[g.Key] {
		marker = "▾ "
	}
	labelWidth := width - 4 - countWidth - resumeTreeGroupAgeCell
	if labelWidth < 1 {
		labelWidth = 1
	}
	label := ansi.Truncate(g.Label, labelWidth, "…")
	pad := labelWidth - ansi.StringWidth(label)
	if pad < 0 {
		pad = 0
	}
	count := fmt.Sprintf("%*s", countWidth, resumeTreeCountText(len(g.Items)))
	age := fmt.Sprintf("%*s", resumeTreeGroupAgeCell, resumeAgeLabel(g.LastActive))
	if isCursor {
		b.WriteString(resumeBlueOpen + "> " + marker + label + pickerReset)
	} else {
		b.WriteString("  \x1b[1m" + marker + label + pickerKeyClose)
	}
	b.WriteString(strings.Repeat(" ", pad))
	b.WriteString(resumeCellOpen + count + age + pickerReset + "\n")
}

// renderResumeTreeChildRow draws one conversation of a group.
//
// 【一手】 The reference paints the title blue and the two right-hand cells
// italic when the row is under the cursor, and leaves the whole row in the
// default colours otherwise.
func (m *runMode) renderResumeTreeChildRow(b *strings.Builder, item *resumetree.Item, isCursor bool, width int) {
	// The child row's right-hand block is the same width as the group
	// row's — the step count, a gap and the age — so both rows end at the
	// last column of the window and both titles are the same width.
	//
	// 【一手】 agy 1.4.7 at 200 columns: the title starts at column 4, the
	// step count ends at column 185 and the age at column 199.
	titleWidth := width - 4 - resumeTreeStepsCell - 2 - resumeTreeChildAgeCell
	if titleWidth < 1 {
		titleWidth = 1
	}
	title := item.Title
	if title == "" {
		title = "(no title)"
	}
	title = ansi.Truncate(title, titleWidth, "…")
	pad := titleWidth - ansi.StringWidth(title)
	if pad < 0 {
		pad = 0
	}
	steps := fmt.Sprintf("%*s", resumeTreeStepsCell, fmt.Sprintf("%d steps", item.Steps))
	age := fmt.Sprintf("%*s", resumeTreeChildAgeCell, resumeAgeLabel(item.Time))
	if isCursor {
		b.WriteString(resumeBlueOpen + "> " + pickerReset + "  " + resumeBlueOpen + title + strings.Repeat(" ", pad))
		b.WriteString("\x1b[3m" + steps + pickerKeyClose + "  \x1b[3m" + resumeBlueOpen + age + pickerKeyClose + "\n")
		return
	}
	b.WriteString("    " + title + strings.Repeat(" ", pad))
	b.WriteString(resumeCellOpen + steps + "  " + age + pickerReset + "\n")
}

// renderResumeTree draws the tree and its keyboard hint.
func (m *runMode) renderResumeTree(b *strings.Builder, rows []resumeTreeRow, total, window, width int) {
	end := m.resumeWindow + window
	if end > total {
		end = total
	}
	countWidth := resumeTreeCountWidth(m.resumeGroups)
	for i := m.resumeWindow; i < end; i++ {
		row := rows[i]
		if row.item == nil {
			m.renderResumeTreeGroupRow(b, row.group, i == m.resumeCursor, width, countWidth)
			continue
		}
		m.renderResumeTreeChildRow(b, row.item, i == m.resumeCursor, width)
	}
	if total > window {
		fmt.Fprintf(b, "%s  [%d-%d of %d items]%s\n",
			resumeLabelOpen, m.resumeWindow+1, end, total, pickerReset)
	}
	b.WriteString("\n" + m.resumeTreeHint(rows) + "\n\n")
}

// resumeTreeHint is the tree's keyboard hint.
//
// 【一手】 agy 1.4.7: the tree offers "ctrl+f Conversation View" and
// "ctrl+e Collapse" — the latter turns into "Expand" when the group under
// the cursor is collapsed.
func (m *runMode) resumeTreeHint(rows []resumeTreeRow) string {
	collapse := "Expand"
	if g := resumeCursorGroup(rows, m.resumeCursor); g != nil && m.resumeExpanded[g.Key] {
		collapse = "Collapse"
	}
	return resumeKeyboardHintParts(true, "Conversation View", collapse)
}

// buildResumeGroups rebuilds the workspace tree from the store.
//
// The workspace of a conversation comes from the reference's conversation
// index; the conversations themselves come from the conversation list the
// picker already holds.
func (m *runMode) buildResumeGroups() {
	if len(m.resumeList) == 0 {
		m.resumeGroups = nil
		return
	}
	ids := make([]string, 0, len(m.resumeList)+1)
	for _, c := range m.resumeList {
		ids = append(ids, c.ID)
	}
	if m.conversationID != "" {
		ids = append(ids, m.conversationID)
	}
	summaries := resumetree.LookupSummaries(ids)
	cache := resumetree.LookupCacheSummaries(ids)
	items := make([]resumetree.Item, 0, len(m.resumeList))
	for _, c := range m.resumeList {
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
		}
		items = append(items, item)
	}
	pinned := m.resumePinnedKey(summaries)
	m.resumeGroups = resumetree.Build(items, pinned)
	if m.resumeExpanded == nil {
		m.resumeExpanded = map[string]bool{}
	}
}

// resumePinnedKey is the workspace group the reference keeps at the top of
// the tree: the group that holds the conversation the session is showing.
//
// 【反编译】 model.preferredGroup (0x102af8a20) picks that group and
// model.defaultExpandedGroup (0x102af8b90) expands it.
//
// 【一手】 The reference's live picker keeps the "/Users/gear" group first
// whatever its timestamps say — that group holds the conversation the
// reference's session was showing.
func (m *runMode) resumePinnedKey(summaries map[string]resumetree.Summary) string {
	if m.conversationID == "" {
		return ""
	}
	summary, ok := summaries[m.conversationID]
	if !ok {
		return ""
	}
	return resumetree.ItemWorkspaceURI(summary.WorkspaceURIs)
}

// resetResumeTreeState prepares the tree of a freshly opened picker.
//
// 【一手】 The reference opens the tree with the group of the current
// conversation expanded and the cursor on its first conversation.
func (m *runMode) resetResumeTreeState() {
	m.resumeGrouped = true
	m.resumeExpanded = map[string]bool{}
	m.resumeGroups = nil
	m.resumeCursor = 0
	m.resumeWindow = 0
}

// expandResumeGroup expands or collapses a group.
func (m *runMode) expandResumeGroup(key string, expanded bool) {
	if m.resumeExpanded == nil {
		m.resumeExpanded = map[string]bool{}
	}
	m.resumeExpanded[key] = expanded
}

// resumeTreeTarget is what the cursor points at: a group row, or a
// conversation inside a group.
type resumeTreeTarget struct {
	group string
	item  string
}

// resumeTreeCursorTarget remembers what the cursor points at, so the cursor
// can follow it when the tree changes shape — a group that expands adds rows
// under itself, a group that collapses takes them away.
func resumeTreeCursorTarget(rows []resumeTreeRow, cursor int) resumeTreeTarget {
	if cursor < 0 || cursor >= len(rows) {
		return resumeTreeTarget{}
	}
	row := rows[cursor]
	if row.item == nil {
		return resumeTreeTarget{group: row.group.Key}
	}
	return resumeTreeTarget{group: row.group.Key, item: row.item.ID}
}

// resumeTreeFocusTarget moves the cursor back onto a remembered target. A
// conversation whose group collapsed is gone, so the cursor falls back to
// the group row.
func (m *runMode) resumeTreeFocusTarget(rows []resumeTreeRow, target resumeTreeTarget) {
	groupRow := -1
	for i, row := range rows {
		if row.group.Key != target.group {
			continue
		}
		if groupRow < 0 {
			groupRow = i
		}
		if target.item != "" && row.item != nil && row.item.ID == target.item {
			m.resumeCursor = i
			return
		}
	}
	if groupRow >= 0 {
		m.resumeCursor = groupRow
	}
}

// toggleResumeGroupAt expands or collapses a group and keeps the cursor on
// whatever it pointed at.
func (m *runMode) toggleResumeGroupAt(rows []resumeTreeRow, cursor int) {
	target := resumeTreeCursorTarget(rows, cursor)
	if target.group == "" {
		return
	}
	m.expandResumeGroup(target.group, !m.resumeExpanded[target.group])
	m.resumeTreeFocusTarget(m.resumeTreeRows(), target)
}

// handleResumeTreeKeys handles the keys of the workspace tree.
//
// 【一手】 agy 1.4.7:
//
//   - enter on a group row expands or collapses the group and keeps the
//     cursor on the group row;
//   - ctrl+e expands or collapses the group under the cursor — the hint
//     reads "Collapse" while that group is expanded;
//   - f2 and f4 do nothing on a group row.
func (m *runMode) handleResumeTreeKeys(key tea.Key) (tea.Model, tea.Cmd, bool) {
	rows := m.resumeTreeRows()
	total := len(rows)
	switch key.Code {
	case tea.KeyUp:
		m.resumeListUp(total)
		return m, nil, true
	case tea.KeyDown:
		m.resumeListDown(total)
		return m, nil, true
	case tea.KeyRight:
		m.resumeListPage(total, 1)
		return m, nil, true
	case tea.KeyLeft:
		m.resumeListPage(total, -1)
		return m, nil, true
	case tea.KeyTab:
		m.resumeTab = (m.resumeTab + 1) % 2
		m.resumeWindow, m.resumeCursor = 0, 0
		return m, nil, true
	case tea.KeyEsc:
		if m.resumeQuery != "" {
			m.resumeQuery = ""
			m.resumeWindow, m.resumeCursor = 0, 0
			return m, nil, true
		}
		m.closeResumePicker()
		return m, nil, true
	case tea.KeyEnter:
		if total == 0 || m.resumeCursor < 0 || m.resumeCursor >= total {
			m.closeResumePicker()
			return m, nil, true
		}
		row := rows[m.resumeCursor]
		if row.item == nil {
			m.toggleResumeGroupAt(rows, m.resumeCursor)
			return m, nil, true
		}
		picked := m.resumeMetaFor(row.item.ID)
		m.closeResumePicker()
		return m, m.resumeConversation(picked), true
	case tea.KeyF2, tea.KeyF4:
		if total == 0 || m.resumeCursor < 0 || m.resumeCursor >= total {
			return m, nil, true
		}
		row := rows[m.resumeCursor]
		if row.item == nil {
			// f2 and f4 act on a conversation; a group row has none.
			return m, nil, true
		}
		picked := m.resumeMetaFor(row.item.ID)
		if key.Code == tea.KeyF4 {
			if picked.ID != "" {
				_ = convstore.DeleteConversation(picked.ID)
				m.reloadResumeList()
				m.buildResumeGroups()
				m.clampResumeCursor(len(m.resumeTreeRows()))
			}
			return m, nil, true
		}
		m.closeResumePicker()
		m.resumeList = nil
		m.conversationName = picked.Title
		m.openRenamePanel()
		return m, nil, true
	}
	if key.Code == 'f' && key.Mod.Contains(tea.ModCtrl) {
		m.toggleResumeGrouped()
		return m, nil, true
	}
	if key.Code == 'e' && key.Mod.Contains(tea.ModCtrl) {
		m.toggleResumeGroupAt(rows, m.resumeCursor)
		return m, nil, true
	}
	if key.Code == 'u' && key.Mod.Contains(tea.ModCtrl) {
		m.resumeQuery = ""
		m.resumeWindow, m.resumeCursor = 0, 0
		return m, nil, true
	}
	if keybind.IsTypedText(key) {
		m.resumeQuery += key.Text
		m.resumeWindow, m.resumeCursor = 0, 0
		m.resumeCursorOn = true
		return m, resumeBlink(), true
	}
	return m, nil, true
}

// resumeMetaFor finds the conversation a tree row points at.
func (m *runMode) resumeMetaFor(id string) convstore.ConversationMeta {
	for _, c := range m.resumeList {
		if c.ID == id {
			return c
		}
	}
	return convstore.ConversationMeta{ID: id}
}

// toggleResumeGrouped switches between the workspace tree and the flat
// conversation list, keeping the cursor on the same conversation.
//
// 【一手】 agy 1.4.7: ctrl+f switches the view. The tree's hint calls the
// flat list the "Conversation View", the flat list's hint calls the tree
// the "Grouped View".
func (m *runMode) toggleResumeGrouped() {
	id := m.resumeCursorConversationID()
	m.resumeGrouped = !m.resumeGrouped
	m.resumeWindow, m.resumeCursor = 0, 0
	m.resumeFocusConversation(id)
}

// resumeCursorConversationID is the conversation under the cursor, or the
// empty string when the cursor is not on one.
func (m *runMode) resumeCursorConversationID() string {
	if m.resumeTreeActive() {
		rows := m.resumeTreeRows()
		if m.resumeCursor >= 0 && m.resumeCursor < len(rows) && rows[m.resumeCursor].item != nil {
			return rows[m.resumeCursor].item.ID
		}
		return ""
	}
	rows := m.resumeFiltered()
	if m.resumeCursor >= 0 && m.resumeCursor < len(rows) {
		return rows[m.resumeCursor].ID
	}
	return ""
}

// resumeFocusConversation moves the cursor onto a conversation.
func (m *runMode) resumeFocusConversation(id string) {
	if id == "" {
		return
	}
	if m.resumeTreeActive() {
		rows := m.resumeTreeRows()
		for i, row := range rows {
			if row.item != nil && row.item.ID == id {
				m.resumeCursor = i
				m.resumeSyncWindow(len(rows))
				return
			}
		}
		return
	}
	rows := m.resumeFiltered()
	for i, r := range rows {
		if r.ID == id {
			m.resumeCursor = i
			m.resumeSyncWindow(len(rows))
			return
		}
	}
}

// resumeSyncWindow keeps the visible window around the cursor.
func (m *runMode) resumeSyncWindow(total int) {
	size := resumeListWindowFor(m.height)
	if m.resumeCursor >= m.resumeWindow+size {
		m.resumeWindow = m.resumeCursor - size + 1
	}
	if m.resumeCursor < m.resumeWindow {
		m.resumeWindow = m.resumeCursor
	}
}

// clampResumeCursor keeps the cursor inside the tree after the list changed.
func (m *runMode) clampResumeCursor(total int) {
	if m.resumeCursor >= total {
		m.resumeCursor = total - 1
	}
	if m.resumeCursor < 0 {
		m.resumeCursor = 0
	}
	if m.resumeWindow > m.resumeCursor {
		m.resumeWindow = m.resumeCursor
	}
}
