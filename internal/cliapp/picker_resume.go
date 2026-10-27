package cliapp

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/justwasm/pigasus/internal/convstore"
	"github.com/justwasm/pigasus/palette"
)

// The reference's /resume picker colours.
//
// 【一手】 agy 1.4.7, tmux capture (out/resume-tree/ref/tree.txt): the
// cursor and the row under it are the brand blue (38;2;66;133;244), the
// right-hand cells are the dimmed body gray (38;2;60;64;67), the inactive
// tab and the rule above the picker are (38;2;218;220;224) and the hint
// labels are (38;2;95;99;104).
// The row's colours are the selected scheme's slots — the accent, the
// muted foreground, the dim foreground and the border.
var (
	resumeBlueOpen     = palette.BlueOpen
	resumeCellOpen     = palette.SubtleOpen
	resumeLabelOpen    = palette.DimGrayOpen
	resumeInactiveOpen = palette.SeparatorOpen
)

func resumeListWindowFor(height int) int {
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

const (
	resumeRightWidth     = 15 + 2 + 12 + 2 + 12
	resumeNoProjectWidth = 12 + 2 + 12
)

type resumeBlinkMsg struct{}

func resumeBlink() tea.Cmd {
	return tea.Tick(530*time.Millisecond, func(time.Time) tea.Msg { return resumeBlinkMsg{} })
}

func resumeProjectCell(project string) string {
	project = ansi.Truncate(project, 15, "\u2026")
	return fmt.Sprintf("%*s", 15, project)
}

func resumeStepsCell(steps int) string {
	return fmt.Sprintf("%*s", 12, fmt.Sprintf("%d steps", steps))
}

func resumeAgeCell(mod time.Time) string {
	return fmt.Sprintf("%*s", 12, resumeAgeLabel(mod))
}

var resumeAgeLoc = func() *time.Location {
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		return time.UTC
	}
	return loc
}()

func resumeAgeLabel(mod time.Time) string {
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
		local := mod.In(resumeAgeLoc)
		if local.Year() == time.Now().In(resumeAgeLoc).Year() {
			return local.Format("Jan 2")
		}
		return local.Format("Jan 2, 2006")
	}
}

func (m *runMode) resumeFiltered() []convstore.ConversationMeta {
	if m.resumeTab != 0 {
		return nil
	}
	if m.resumeQuery == "" {
		return m.resumeList
	}
	q := strings.ToLower(m.resumeQuery)
	out := make([]convstore.ConversationMeta, 0, len(m.resumeList))
	for _, c := range m.resumeList {
		if strings.Contains(strings.ToLower(c.Title), q) {
			out = append(out, c)
		}
	}
	return out
}

func (m *runMode) resumeListDown(total int) {
	if total <= 0 {
		return
	}
	size := resumeListWindowFor(m.height)
	if m.resumeCursor < total-1 {
		m.resumeCursor++
	}
	if m.resumeCursor >= m.resumeWindow+size {
		m.resumeWindow = m.resumeCursor - size + 1
	}
}

func (m *runMode) resumeListUp(total int) {
	if total <= 0 {
		return
	}
	if m.resumeCursor > 0 {
		m.resumeCursor--
	}
	if m.resumeCursor < m.resumeWindow {
		m.resumeWindow = m.resumeCursor
	}
}

func (m *runMode) resumeListPage(total, dir int) {
	if total <= 0 {
		return
	}
	size := resumeListWindowFor(m.height)
	maxWindow := total - size
	if maxWindow < 0 {
		maxWindow = 0
	}
	m.resumeWindow += dir * size
	if m.resumeWindow > maxWindow {
		m.resumeWindow = maxWindow
	}
	if m.resumeWindow < 0 {
		m.resumeWindow = 0
	}
	m.resumeCursor += dir * size
	if m.resumeCursor > total-1 {
		m.resumeCursor = total - 1
	}
	if m.resumeCursor < 0 {
		m.resumeCursor = 0
	}
}

func renderResumeTabBar(b *strings.Builder, activeTab int) {
	writeTab := func(name string, active bool) {
		if active {
			b.WriteString("\x1b[1;4m" + resumeBlueOpen + name + pickerKeyClose)
			return
		}
		b.WriteString(resumeInactiveOpen + name + pickerReset)
	}
	b.WriteString("   ")
	writeTab("CLI", activeTab == 0)
	b.WriteString("    ")
	writeTab("Other", activeTab == 1)
	b.WriteString(" " + resumeInactiveOpen + "  (tab to cycle)" + pickerReset + "\n\n\x1b[1m  Conversations\x1b[0m\n")
}

func (m *runMode) renderResumeSearchRow(b *strings.Builder, storeEmpty bool) {
	if m.resumeTab != 0 || storeEmpty {
		return
	}
	if m.resumeQuery == "" {
		b.WriteString(resumeCellOpen + "  Type to search conversations..." + pickerReset + "\n")
		return
	}
	b.WriteString(resumeCellOpen + "  Search: " + pickerReset + m.resumeQuery)
	if m.resumeCursorOn {
		b.WriteString("\x1b[7m\x1b[37m \x1b[0m")
	} else {
		b.WriteString(" ")
	}
	b.WriteString("\n")
}

func (m *runMode) renderResumeEmpty(b *strings.Builder, storeEmpty bool) string {
	b.WriteString(resumeCellOpen)
	if m.resumeTab == 0 && !storeEmpty {
		b.WriteString("  No matching conversations.")
	} else {
		b.WriteString("  No conversations available.")
	}
	b.WriteString(pickerReset + "\n")
	if m.resumeTab != 0 || storeEmpty {
		b.WriteString(resumeCellOpen + "  Press Enter to return." + pickerReset + "\n")
	}
	b.WriteString("\n" + resumeKeyboardHint(m.resumeTab == 0 && !storeEmpty, false) + "\n\n")
	return b.String()
}

func renderResumeRowCursor(b *strings.Builder, title string, pad int, c convstore.ConversationMeta, hasAge bool) {
	b.WriteString(resumeBlueOpen + "> " + title + pickerReset + strings.Repeat(" ", pad))
	if c.Project != "" {
		b.WriteString(resumeCellOpen + resumeProjectCell(c.Project) + pickerReset + "  ")
	}
	b.WriteString("\x1b[3m" + resumeBlueOpen + resumeStepsCell(c.StepCount))
	if hasAge {
		b.WriteString(pickerKeyClose + "  \x1b[3m" + resumeBlueOpen + resumeAgeCell(c.ModTime))
	}
	b.WriteString(pickerKeyClose + "\n")
}

func renderResumeRowNormal(b *strings.Builder, title string, pad int, c convstore.ConversationMeta, hasAge bool) {
	b.WriteString(pickerKeyClose + "  " + title + strings.Repeat(" ", pad) + resumeCellOpen)
	if c.Project != "" {
		b.WriteString(resumeProjectCell(c.Project) + "  ")
	}
	b.WriteString(resumeStepsCell(c.StepCount))
	if hasAge {
		b.WriteString("  " + resumeAgeCell(c.ModTime))
	}
	b.WriteString(pickerReset + "\n")
}

func renderResumeRow(b *strings.Builder, c convstore.ConversationMeta, isCursor bool, width int) {
	title := c.Title
	if title == "" {
		title = "(no title)"
	}
	hasAge := resumeAgeLabel(c.ModTime) != ""
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
	title = ansi.Truncate(title, titleWidth, "\u2026")
	pad := titleWidth - ansi.StringWidth(title)
	if pad < 0 {
		pad = 0
	}
	if isCursor {
		renderResumeRowCursor(b, title, pad, c, hasAge)
	} else {
		renderResumeRowNormal(b, title, pad, c, hasAge)
	}
}

func (m *runMode) clampResumeWindow(total, window int) {
	if m.resumeWindow > total-window {
		m.resumeWindow = total - window
	}
	if m.resumeWindow < 0 {
		m.resumeWindow = 0
	}
	if m.resumeCursor >= total {
		m.resumeCursor = total - 1
	}
	if m.resumeCursor < 0 {
		m.resumeCursor = 0
	}
}

func (m *runMode) renderResumeList(b *strings.Builder, rows []convstore.ConversationMeta, total, window, width int) {
	end := m.resumeWindow + window
	if end > total {
		end = total
	}
	for i := m.resumeWindow; i < end; i++ {
		renderResumeRow(b, rows[i], i == m.resumeCursor, width)
	}
	if total > window {
		fmt.Fprintf(b, "%s  [%d-%d of %d items]%s\n",
			resumeLabelOpen, m.resumeWindow+1, end, total, pickerReset)
	}
	b.WriteString("\n" + m.resumeFlatHint(rows) + "\n\n")
}

// resumeFlatHint is the flat list's keyboard hint.
//
// 【一手】 agy 1.4.7: the flat list offers "ctrl+f Grouped View", and a
// search keeps the grouped flag — so the filtered list still offers
// "ctrl+f Conversation View" and drops the ctrl+e element.
func (m *runMode) resumeFlatHint(rows []convstore.ConversationMeta) string {
	if m.resumeGrouped {
		return resumeKeyboardHintParts(true, "Conversation View", "")
	}
	return resumeKeyboardHint(true, resumeProjectGroups(rows) >= 2)
}

func (m *runMode) resumeView() string {
	if !m.resumeMode {
		return ""
	}
	width := m.width
	if width <= 0 {
		width = 220
	}
	var b strings.Builder
	renderResumeTabBar(&b, m.resumeTab)
	rows := m.resumeFiltered()
	total := len(rows)
	window := resumeListWindowFor(m.height)
	storeEmpty := total == 0 && m.resumeQuery == ""
	m.renderResumeSearchRow(&b, storeEmpty)
	if total == 0 {
		return m.renderResumeEmpty(&b, storeEmpty)
	}
	if m.resumeTreeActive() {
		tree := m.resumeTreeRows()
		m.clampResumeWindow(len(tree), window)
		m.renderResumeTree(&b, tree, len(tree), window, width)
		return b.String()
	}
	m.clampResumeWindow(total, window)
	m.renderResumeList(&b, rows, total, window, width)
	return b.String()
}

func resumeProjectGroups(rows []convstore.ConversationMeta) int {
	seen := make(map[string]bool, len(rows))
	for _, r := range rows {
		if r.Project != "" {
			seen[r.Project] = true
		}
	}
	return len(seen)
}

// resumeKeyboardHint is the flat list's hint.
//
// withNav adds the navigation keys â the CLI tab with a visible list.
// grouped advertises the workspace tree as the view ctrl+f switches to.
//
// 【一手】 agy 1.4.2 showed "ctrl+f Grouped View" over a store whose rows
// carry project names and omitted it when no visible row has one.
func resumeKeyboardHint(withNav, grouped bool) string {
	view := ""
	if grouped {
		view = "Grouped View"
	}
	return resumeKeyboardHintParts(withNav, view, "")
}

// resumeKeyboardHintParts renders the picker's keyboard hint.
//
// view names the view ctrl+f switches to â "Grouped View" for the workspace
// tree, "Conversation View" for the flat list â and is empty when the
// picker does not offer the switch. collapse names the ctrl+e action.
//
// 【一手】 agy 1.4.7 (out/resume-tree/ref/tree.txt):
//
//	Keyboard: ↑/↓ Navigate  ←/→ Page  ctrl+f Conversation View  ctrl+e
//	Collapse  enter Select  f2 Rename  f4 Delete  tab Switch Tab  esc Go
//	back / Clear search
func resumeKeyboardHintParts(withNav bool, view, collapse string) string {
	key := func(s string) string {
		return resumeBlueOpen + s + pickerReset
	}
	label := func(s string) string {
		return resumeLabelOpen + s + pickerReset
	}
	var b strings.Builder
	b.WriteString("[1m")
	b.WriteString("Keyboard: ")
	b.WriteString("[0m")
	if withNav {
		b.WriteString(key("↑/↓") + " " + label("Navigate") + "  ")
		b.WriteString(key("←/→") + " " + label("Page") + "  ")
		if view != "" {
			b.WriteString(key("ctrl+f") + " " + label(view) + "  ")
		}
		if collapse != "" {
			b.WriteString(key("ctrl+e") + " " + label(collapse) + "  ")
		}
	}
	b.WriteString(key("enter") + " " + label("Select") + "  ")
	if withNav {
		b.WriteString(key("f2") + " " + label("Rename") + "  ")
		b.WriteString(key("f4") + " " + label("Delete") + "  ")
	}
	b.WriteString(key("tab") + " " + label("Switch Tab") + "  ")
	b.WriteString(key("esc") + " " + label("Go back / Clear search"))
	return b.String()
}
