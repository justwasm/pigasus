package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/justwasm/pigasus/internal/convstore"
)

func renderResumeTabBar(b *strings.Builder, activeTab int) {
	writeTab := func(name string, active bool) {
		if active {
			b.WriteString("\x1b[1;4m" + cBlueOpen + name + pickerKeyClose)
			return
		}
		b.WriteString(cSeparatorOpen + name + pickerReset)
	}
	b.WriteString("   ")
	writeTab("CLI", activeTab == 0)
	b.WriteString("    ")
	writeTab("Other", activeTab == 1)
	b.WriteString(" " + cSeparatorOpen + "  (tab to cycle)" + pickerReset + "\n\n\x1b[1m  Conversations\x1b[0m\n")
}

func (m *runMode) renderResumeSearchRow(b *strings.Builder, storeEmpty bool) {
	if m.resumeTab != 0 || storeEmpty {
		return
	}
	if m.resumeQuery == "" {
		b.WriteString(cHelpDescOpen + "  Type to search conversations..." + cHelpDescClose + "\n")
		return
	}
	b.WriteString(cHelpDescOpen + "  Search: " + cHelpDescClose + m.resumeQuery)
	if m.resumeCursorOn {
		b.WriteString("\x1b[7m\x1b[37m \x1b[0m")
	} else {
		b.WriteString(" ")
	}
	b.WriteString("\n")
}

func (m *runMode) renderResumeEmpty(b *strings.Builder, storeEmpty bool) string {
	b.WriteString(cHelpDescOpen)
	if m.resumeTab == 0 && !storeEmpty {
		b.WriteString("  No matching conversations.")
	} else {
		b.WriteString("  No conversations available.")
	}
	b.WriteString(cHelpDescClose + "\n")
	if m.resumeTab != 0 || storeEmpty {
		b.WriteString(cHelpDescOpen + "  Press Enter to return." + cHelpDescClose + "\n")
	}
	b.WriteString("\n" + resumeKeyboardHint(m.resumeTab == 0 && !storeEmpty, false) + "\n\n")
	return b.String()
}

func renderResumeRowCursor(b *strings.Builder, title string, pad int, c convstore.ConversationMeta, hasAge bool) {
	b.WriteString(cBlueOpen + "> " + title + pickerReset + strings.Repeat(" ", pad))
	if c.Project != "" {
		b.WriteString(cHelpDescOpen + resumeProjectCell(c.Project) + cHelpDescClose + "  ")
	}
	b.WriteString("\x1b[3m" + cBlueOpen + resumeStepsCell(c.StepCount))
	if hasAge {
		b.WriteString(pickerKeyClose + "  \x1b[3m" + cBlueOpen + resumeAgeCell(c.ModTime))
	}
	b.WriteString(pickerKeyClose + "\n")
}

func renderResumeRowNormal(b *strings.Builder, title string, pad int, c convstore.ConversationMeta, hasAge bool) {
	b.WriteString(pickerKeyClose + "  " + title + strings.Repeat(" ", pad) + cHelpDescOpen)
	if c.Project != "" {
		b.WriteString(resumeProjectCell(c.Project) + "  ")
	}
	b.WriteString(resumeStepsCell(c.StepCount))
	if hasAge {
		b.WriteString("  " + resumeAgeCell(c.ModTime))
	}
	b.WriteString(cHelpDescClose + "\n")
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
			cDimGrayOpen, m.resumeWindow+1, end, total, pickerReset)
	}
	b.WriteString("\n" + resumeKeyboardHint(true, resumeProjectGroups(rows) >= 2) + "\n\n")
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
	m.clampResumeWindow(total, window)
	m.renderResumeList(&b, rows, total, window, width)
	return b.String()
}

// resumeProjectGroups counts the distinct project names on the rows the
// picker is about to draw. Used to decide whether "Grouped View" is worth
// advertising (see resumeKeyboardHint).
func resumeProjectGroups(rows []convstore.ConversationMeta) int {
	seen := make(map[string]bool, len(rows))
	for _, r := range rows {
		if r.Project != "" {
			seen[r.Project] = true
		}
	}
	return len(seen)
}

// resumeKeyboardHint renders the picker's bottom hint row: bold
// "Keyboard: ", brand-blue keys and dim-gray labels. The navigation
// half is dropped on the empty tab (REF's OTHER tab hint reads
// "Keyboard: enter Select  tab Switch Tab  esc Go back / Clear search").
func resumeKeyboardHint(withNav, grouped bool) string {
	key := func(s string) string {
		return cBlueOpen + s + pickerReset
	}
	label := func(s string) string {
		return cDimGrayOpen + s + pickerReset
	}
	var b strings.Builder
	b.WriteString("\x1b[1m")
	b.WriteString("Keyboard: ")
	b.WriteString("\x1b[0m")
	if withNav {
		b.WriteString(key("\u2191/\u2193") + " " + label("Navigate") + "  ")
		b.WriteString(key("\u2190/\u2192") + " " + label("Page") + "  ")
	}
	// Cycle 278: "ctrl+f Grouped View" is contextual, not constant. REF
	// advertises it over a 500-row store whose rows carry project names and
	// omits it over a 68-row store where every visible row has no project
	// (both observed at 200x40; the filtered page matters, not the store).
	// Grouping by a single project would be a no-op, so the key appears from
	// two distinct projects up. 【推测】 on the exact threshold — the
	// zero-project and many-project cases are the ones measured.
	if withNav && grouped {
		b.WriteString(key("ctrl+f") + " " + label("Grouped View") + "  ")
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
