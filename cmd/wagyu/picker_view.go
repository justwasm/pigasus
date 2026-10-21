// picker_view.go — render the tuilib picker overlay above the prompt
// block. Cycle 74 / 126.
//
// /tmp/agy_slash.txt captured state when the user typed "/" into the
// prompt (v1.3.5 binary 1:1):
//
//   > /
//   ───────────────────────────────────────────────────────────────────────
//   > /add-dir          Add a directory to the workspace
//     /agents           List available custom agents
//     /artifact         View and review artifacts
//     /btw              Ask a side question without interrupting the current task
//     /changelog        Show release notes and changes
//     /compact          Compact the conversation now (cycle 237, AGY 1.4.2)
//      ↓ 89 more
//
//     ↑/↓ Navigate · enter Select · tab Complete
//   esc to cancel                                          Gemini 3.8 Flash · high
//
// Cycle 126 alignment with the v1.3.6 ref capture:
//   * Highlighted row: "> " is part of the command name in true-colour
//     blue (38;2;66;133;244) — same as the brand blue
//   * Non-highlighted: "  " prefix + name (no color), dim gray desc
//   * Name column padded to 51 chars ("> " + 49 or "  " + 49)
//   * "↓ N more" tail row in dim gray (same as descriptions)
//   * Blank row between "↓ N more" and the hint row
//   * "esc to cancel" stays in statusRow when picker is visible
//
// The original cycle-74 capture used 256-colour 38;5;69/238/241;
// cycle 126 normalises them to the recovered 24-bit RGB palette
// (see colors.go). The visible byte-for-byte shape is unchanged in
// tmux's colour cube (same shade), but a real Terminal distinguishes
// the two.
package main

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/convstore"
	"github.com/charmbracelet/x/ansi"
)

// Cycle 91 / 126: picker hint row ANSI escape sequences. The
// v1.3.6 ref binary emits 24-bit RGB blue (38;2;66;133;244) for
// the keys and 24-bit RGB dim gray (38;2;95;99;104) for the
// labels — matching the prompt and the tab-bar brand palette.
// See colors.go.
const (
	pickerNameHi   = cBlueOpen
	// Cycle 283: the reference renders picker descriptions and the
	// "↓ N more" counter in Solarized base01 #586E75 (verified from a
	// truecolour capture of the `/` picker: 38;2;88;110;117), one step
	// dimmer than the labels.
	pickerDescDim  = cSubtleOpen // #586E75 for descriptions
	pickerLabelDim = cDimGrayOpen // dim gray for keyboard labels (same gray)
	pickerKeyOpen  = "\x1b[1m" + cBlueOpen
	pickerKeyClose = "\x1b[0m"
	pickerReset    = "\x1b[39m"
)

// Cycle 131: picker description column width. The reference's
// description column is exactly 48 characters wide; descriptions
// that exceed it are truncated to 45 chars + "..." (recovered from
// /tmp/agycmp/hotpath/picker/ref.001-down5.txt where descriptions
// like `Ask a side question without interrupting the current task`
// (57 chars) truncates to `Ask a side question without interrupting the ...`
// (45 + `...`), and `Search code in the workspace (usage: /codesearch <query>)`
// (57 chars) truncates to `Search code in the workspace (usage: /codesea...`
// (45 + `...`).
const pickerDescWidth = 48

// truncatePickerDesc returns desc with at most pickerDescWidth
// characters, appending "..." when truncation occurred. Descriptions
// that already fit are returned unchanged. Cycle 131's width cap
// keeps the picker rows from overflowing into the hint row below.
func truncatePickerDesc(desc string) string {
	if len(desc) <= pickerDescWidth {
		return desc
	}
	return desc[:pickerDescWidth-3] + "..."
}

// pickerView renders the picker overlay body, returning the rows that
// go above the prompt block. Returns "" when the picker is hidden.
//
// Cycle 129: when the highlight (picker.Idx()) advances past the
// visible window (5 rows), the renderer needs to scroll the window
// so the highlight stays visible. pickerOffset is the index of the
// first row drawn; the loop walks [offset, offset+visible) and
// matches highlight against picker.Idx() == i. The "↓ N more" tail
// reflects how many rows lie past the visible window.
//
// Cycle 130: the reference centres the highlight in the visible
// window when the highlight passes the bottom row, instead of
// pinning it to the bottom (the previous pager-style behaviour).
// After pressing Down 5× with the picker on "/", the highlight
// lands on /clear (idx 5) and the reference's window shows rows
// [3..7] — `/btw`, `/changelog`, `> /clear (new)` (highlighted),
// `/codesearch`, `/config` — with "↑ 3 more" above the window
// and "↓ 91 more" below. Net: window_offset = idx - (visible/2).
// We replicate that here.
//
// Cycle 232: as a side effect, populates m.pickerLayoutLocal with the
// internal layout (row count, list-row range, hint row, scroll-tail
// flags). viewBody reads this back to derive the screen rectangle of
// the picker overlay so the mouse handlers can map a (x, y) screen
// coordinate onto a picker list row.
func (m *runMode) pickerView() string {
	var b strings.Builder
	const visible = 5
	const rowWidth = 220 // terminal width (cycle 80: bumped 200->220)
	// Cycle 232: reset the layout for this frame so a hidden picker
	// leaves the geom cleared from the previous (visible) frame.
	m.pickerLayoutLocal = pickerLayout{}
	if !m.picker.Visible() {
		return ""
	}
	total := m.picker.Len()
	// Cycle 130: centre the highlight once it passes the bottom
	// row.  visible/2 = 2, so the bottom-row anchor kicks in once
	// idx >= visible-1 (= 4), after which offset = idx - 2.
	hi := m.picker.Idx()
	if hi < 0 {
		hi = 0
	}
	wantOffset := 0
	if hi >= visible-1 {
		wantOffset = hi - visible/2
	}
	if wantOffset > hi {
		wantOffset = hi
	}
	if wantOffset > total-visible {
		wantOffset = total - visible
	}
	if wantOffset < 0 {
		wantOffset = 0
	}
	m.pickerOffset = wantOffset
	// Cycle 93: when buffer starts with "/" but no row matches
	// (e.g. "/xyz", "/exit/model"), ref still renders the picker
	// overlay: a single "   No matches" row in dim 238 padded with
	// spaces to the terminal width, followed by a blank row and the
	// hint row. No "↓ N more" tail in this state.
	if m.picker.Empty() {
		// Cycle 93: keep the overlay visible so it can render the
		// "No matches" row + hint row (matches ref byte-for-byte).
		// Trailing spaces must be inside dim 238 to survive bubbletea's
		// line-trim pass (verified empirically — see pickerView debug).
		b.WriteString(pickerDescDim)
		b.WriteString("   No matches")
		b.WriteString(strings.Repeat(" ", rowWidth-13))
		b.WriteString(pickerReset)
		b.WriteByte('\n')
		// Blank row separator (cycle 93 matches ref).
		b.WriteByte('\n')
		// Hint row (cycle 91 styling unchanged).
		b.WriteString("  ")
		b.WriteString(pickerKeyOpen + "\u2191/\u2193" + pickerKeyClose)
		b.WriteString(pickerLabelDim + " Navigate \u00b7 ")
		b.WriteString(pickerKeyOpen + "enter" + pickerKeyClose)
		b.WriteString(pickerLabelDim + " Select \u00b7 ")
		b.WriteString(pickerKeyOpen + "tab" + pickerKeyClose)
		b.WriteString(pickerLabelDim + " Complete" + pickerReset + "\n")
		// Cycle 232: layout bookkeeping for the empty branch.
		// 3 rows: "No matches", blank, hint. No list rows.
		m.pickerLayoutLocal.Rows = 3
		m.pickerLayoutLocal.HintRow = 2
		return b.String()
	}
	// Cycle 91: nameHi/descDim/reset are package-level constants
	// defined above.
	// Cycle 130: emit "↑ N more" header when the window has been
	// scrolled past row 0. Same dim 95,99,104 gray as the "↓ N more"
	// tail row, matching the reference capture (recovered via
	// /tmp/agycmp/hotpath/picker/ref.001-down5.txt).
	rowIdx := 0 // Cycle 232: offset of the row we are about to emit.
	if m.pickerOffset > 0 {
		more := m.pickerOffset
		b.WriteString(pickerDescDim + "   \u2191 ")
		if more == 1 {
			b.WriteString("1 more" + pickerReset + "\n")
		} else {
			fmt.Fprintf(&b, "%d more%s\n", more, pickerReset)
		}
		m.pickerLayoutLocal.UpMore = true
		rowIdx++
	}
	m.pickerLayoutLocal.ListTop = rowIdx
	listStart := rowIdx
	for i := 0; i < visible && i+m.pickerOffset < total; i++ {
		row := i + m.pickerOffset
		c := m.picker.SelectedAt(row)
		if c == nil {
			break
		}
		// Cycle 130: render the FIRST alias inline (e.g.
		// `/exit (quit)`, `/codesearch (cs)`, `/config (settings)`).
		// The cycle-130 first attempt joined all aliases with `, `
		// (`/codesearch (cs, search)`) but the reference's picker
		// only renders the first — captured at
		// /tmp/agycmp/hotpath/picker/ref.001-down5.txt where it
		// shows just `(cs)` even though the catalog has both `cs`
		// and `search`. Multi-alias commands are uncommon; the
		// reference compensates by listing the extra aliases only
		// in /help COMMANDS tab (cycle 83), not in the picker.
		aliasSuffix := ""
		if len(c.Aliases) > 0 {
			aliasSuffix = " (" + c.Aliases[0] + ")"
		}
		// Cycle 131: truncate descriptions longer than 50 chars
		// to 47 + "...". The reference capture at
		// /tmp/agycmp/hotpath/picker/ref.001-down5.txt shows
		// 53-char "Ask a side question without interrupting the
		// current task" → "Ask a side question without interrupting the ..."
		// and 55-char "Search code in the workspace (usage: /codesearch <query>)"
		// → "Search code in the workspace (usage: /codesea...".
		desc := truncatePickerDesc(c.Description)
		if row == m.picker.Idx() {
			// Highlighted row: "> " is part of the name in green,
			// then the alias (cycle 130, dim gray) is rendered
			// inline, then padding to nameWidth, then dim description.
			const nameWidth = 51
			name := "> " + c.Name + aliasSuffix
			for len(name) < nameWidth {
				name += " "
			}
			b.WriteString(pickerNameHi)
			b.WriteString(name)
			b.WriteString(pickerReset)
			b.WriteString(pickerDescDim)
			b.WriteString(desc)
			b.WriteString(pickerReset)
			b.WriteString("\n")
			rowIdx++
			continue
		}
		// Non-highlighted row: 2-space prefix + name + alias + dim desc.
		const nameWidth = 51
		name := "  " + c.Name + aliasSuffix
		for len(name) < nameWidth {
			name += " "
		}
		b.WriteString(name)
		b.WriteString(pickerDescDim)
		b.WriteString(desc)
		b.WriteString(pickerReset)
		b.WriteString("\n")
		rowIdx++
	}
	m.pickerLayoutLocal.ListBottom = rowIdx - 1
	m.pickerLayoutLocal.HasList = total > 0
	_ = listStart
	// "↓ N more" tail row when there are more rows beyond the visible
	// 5. Cycle 129: counts the rows below the current scroll window,
	// not just the rows below row 0. Matches the dim 238 row in
	// /tmp/agy_slash.txt.
	rowsBelowWindow := total - (m.pickerOffset + visible)
	if rowsBelowWindow > 0 {
		b.WriteString(pickerDescDim + "   \u2193 ")
		more := rowsBelowWindow
		if more == 1 {
			b.WriteString("1 more" + pickerReset + "\n")
		} else {
			fmt.Fprintf(&b, "%d more%s\n", more, pickerReset)
		}
		m.pickerLayoutLocal.DownMore = true
		rowIdx++
	}
	// Blank row separator matching the v1.3.5 binary (one blank line
	// between "↓ N more" and the hint row).
	b.WriteString("\n")
	rowIdx++
	// Hint row matching /tmp/agy_slash.txt + ref picker capture.
	// Cycle 91: keys (↑/↓, enter, tab) in bold+blue (38;5;69),
	// labels (Navigate, Select, Complete) in dim gray (38;2;101;123;131).
	// Leading 2 spaces are PLAIN TEXT (ref shows them unstyled),
	// separator dots (·) are dim 241, match ref byte-for-byte.
	b.WriteString("  ")
	b.WriteString(pickerKeyOpen + "\u2191/\u2193" + pickerKeyClose)
	b.WriteString(pickerLabelDim + " Navigate \u00b7 ")
	b.WriteString(pickerKeyOpen + "enter" + pickerKeyClose)
	b.WriteString(pickerLabelDim + " Select \u00b7 ")
	b.WriteString(pickerKeyOpen + "tab" + pickerKeyClose)
	b.WriteString(pickerLabelDim + " Complete" + pickerReset + "\n")
	m.pickerLayoutLocal.HintRow = rowIdx
	rowIdx++
	m.pickerLayoutLocal.Rows = rowIdx
	return b.String()
}

// resumeListWindowFor returns the number of conversation rows the
// v1.4.0 /resume picker shows. Recovered from REF at heights 14..40:
//
//	h : 14 15 16 17 18 19 20 21 22 24 30 40
//	n :  2  3  4  5  6  7  8  9 10 10 10 10
//
// i.e. min(10, h-12), clamped to >= 1. Unlike the /help list (cycle
// 182) the picker caps at 10 rows however tall the terminal is — REF
// leaves rows 30..39 blank at h=40.
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

// resumeRightWidth is the width of the picker row's right-hand metadata
// block when the project cell is present: project 15, two spaces,
// steps 12, two spaces, age 12. REF right-anchors it at the terminal's
// last column (col 178 of 220, verified from a PTY capture:
// `\x1b[16;178H`).
const resumeRightWidth = 15 + 2 + 12 + 2 + 12

// resumeNoProjectWidth is the width of the right-hand block when the
// project cell is empty: REF drops the 15-char project field *and* the
// two spaces that would follow it, leaving steps 12 + two + age 12 =
// 26 right-anchored at the terminal's last column.
//
// Verified against the rightmost page (indices 149-158): the three
// rows whose `WorkspaceURIs` is empty in the cache —
// "Checking Working Directory", "Greeting And Codebase Assistance",
// "what is this" — render `    28 steps        May 22` etc., with no
// 15-space project cell in front of the steps field. Rows with a
// project keep the full 43-char block.
const resumeNoProjectWidth = 12 + 2 + 12

// resumeBlinkMsg toggles the /resume search cursor.
//
// REF draws the cursor as a reverse-video space and blinks it — sampled at
// 0.15 s over six seconds the grid alternates `###...` with a period of
// roughly half a second each way, which is the same cadence as the prompt's
// own cursor (bubbles' 530 ms default). The clone reuses that speed.
type resumeBlinkMsg struct{}

// resumeBlink arms the next search-cursor toggle.
func resumeBlink() tea.Cmd {
	return tea.Tick(530*time.Millisecond, func(time.Time) tea.Msg { return resumeBlinkMsg{} })
}

// resumeProjectCell right-aligns the project name in its 15-cell field.
func resumeProjectCell(project string) string {
	project = ansi.Truncate(project, 15, "\u2026")
	return fmt.Sprintf("%*s", 15, project)
}

// resumeStepsCell right-aligns the step count in its 12-cell field. REF
// prints "1 steps" — the singular `%d step` string in the binary belongs
// to a different view (verified: the picker shows "1 steps").
func resumeStepsCell(steps int) string {
	return fmt.Sprintf("%*s", 12, fmt.Sprintf("%d steps", steps))
}

// resumeAgeCell right-aligns the relative age in its 12-cell field.
func resumeAgeCell(mod time.Time) string {
	return fmt.Sprintf("%*s", 12, resumeAgeLabel(mod))
}

// resumeAgeLoc is the timezone REF renders absolute dates in. Sampled
// against the picker: a cache entry with `last_modified_time` set to
// 2026-06-06T00:01Z (133 days ago from this writing) is rendered as
// "Jun 5", which matches America/Los_Angeles (UTC-7) and rules out
// both UTC ("Jun 6") and the machine's Asia/Shanghai ("Jun 6"). Other
// entries (Sep 27, May 22, Oct 13) corroborate. Loaded once; falls
// back to UTC if tzdata is missing.
var resumeAgeLoc = func() *time.Location {
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		return time.UTC
	}
	return loc
}()

// resumeAgeLabel renders the recovered relative-time ladder. REF v1.4.0
// prints "just now" under a minute, then `%dm ago` / `%dh ago` /
// `%dd ago` for the next three buckets, and finally an absolute
// "Mon D" date for anything three days or older — the latter rendered
// in America/Los_Angeles to match the reference.
//
// The 3-day boundary is the only one that needs empirical justification;
// mapping the picker page by page against the cache places it between
// 2.56 d ("2d ago") and 3.36 d ("Oct 13"), which a 72 h floor matches
// without ambiguity.
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

// resumeFiltered returns the rows the picker shows for the active tab
// and the current search query.
//
// The OTHER tab is always empty on this machine: every store in
// ~/.gemini/antigravity-cli/conversations is a CLI conversation, and
// REF's own OTHER tab renders "No conversations available." too.
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

// resumeListDown moves the cursor down one row, scrolling the window by one
// row once the cursor reaches its bottom edge. At the last row the cursor
// stops — REF does not wrap (verified: twelve extra Downs on the final row
// leave the cursor where it is).
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

// resumeListUp is the mirror of resumeListDown: it stops at the first row
// (verified: Up on row 0 leaves the window and the indicator untouched).
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

// resumeListPage pages the window by one screen (Right = +1, Left = -1) and
// moves the cursor by the same amount, both clamped at the ends.
//
// The cursor is absolute, not "top of the page": verified against REF, Right
// from row 0 gives [11-20] with the cursor on row 1 of the page, and the
// sixteenth Right — the first one that cannot scroll any further — leaves the
// window pinned on `total-size` while the cursor keeps advancing to the last
// row (and the seventeenth leaves it there).
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

// resumeView renders the v1.4.0 /resume conversation picker overlay.
// Returns "" when the picker is hidden.
//
// Cycle 184: rewritten from the cycle-167 placeholder. The v1.4.0
// layout, recovered from a true-colour PTY capture at 220x30, is
//
//	   CLI    Other   (tab to cycle)
//
//	  Conversations
//	  Type to search conversations...
//	> <title>                          <project>  <n steps>  <age>
//	  <title>                          <project>  <n steps>  <age>
//	  [1-10 of 152 items]
//
//	Keyboard: ↑/↓ Navigate  ←/→ Page  ctrl+f Grouped View  enter Select  f2 Rename  f4 Delete  tab Switch Tab  esc Go back / Clear search
//
// The panel is emitted between the prompt's bottom separator and the
// status row by viewBody, so it must NOT re-emit the prompt or a
// separator (the cycle-167 version did, adding a phantom `> /resume`
// row and a third rule).
//
// Cycle 184 also removes the m.viewportHeight() call this function used
// to size its list. viewportHeight measures itself with viewBody(nil),
// which renders this function — so asking it from inside the picker
// recursed until the goroutine stack overflowed and the process died.
// The window now comes straight from the terminal height.
func (m *runMode) resumeView() string {
	if !m.resumeMode {
		return ""
	}
	width := m.width
	if width <= 0 {
		width = 220
	}
	var b strings.Builder

	// Tab bar. The active tab is bold + underlined brand blue; the
	// inactive tab and the "(tab to cycle)" suffix share the separator
	// light gray (38;2;218;220;224).
	writeTab := func(name string, active bool) {
		if active {
			b.WriteString("\x1b[1;4m")
			b.WriteString(cBlueOpen)
			b.WriteString(name)
			b.WriteString(pickerKeyClose)
			return
		}
		b.WriteString(cSeparatorOpen)
		b.WriteString(name)
		b.WriteString(pickerReset)
	}
	b.WriteString("   ")
	writeTab("CLI", m.resumeTab == 0)
	b.WriteString("    ")
	writeTab("Other", m.resumeTab == 1)
	b.WriteString(" ")
	b.WriteString(cSeparatorOpen)
	b.WriteString("  (tab to cycle)")
	b.WriteString(pickerReset)
	b.WriteString("\n")

	// Blank row, then the bold header.
	b.WriteString("\n")
	b.WriteString("\x1b[1m")
	b.WriteString("  Conversations")
	b.WriteString("\x1b[0m")
	b.WriteString("\n")

	rows := m.resumeFiltered()
	total := len(rows)
	window := resumeListWindowFor(m.height)
	// Cycle 278: an empty store (no query, no rows) is a different screen
	// from "the query matched nothing": REF drops the search row and shows
	// the "return" hint. Verified against agy 1.4.2 with an empty store.
	storeEmpty := total == 0 && m.resumeQuery == ""

	// Search row: the placeholder while the query is empty, the live
	// query otherwise. Only the CLI tab has one — REF's OTHER tab (which
	// is always empty here) goes straight from the header to the
	// "No conversations available." rows.
	if m.resumeTab == 0 && !storeEmpty {
		if m.resumeQuery == "" {
			b.WriteString(cHelpDescOpen)
			b.WriteString("  Type to search conversations...")
			b.WriteString(cHelpDescClose)
		} else {
			b.WriteString(cHelpDescOpen)
			b.WriteString("  Search: ")
			b.WriteString(cHelpDescClose)
			b.WriteString(m.resumeQuery)
			// Block cursor at the end of the query. REF emits
			// `\x1b[37;7m \x1b[m` — reverse video, white on the
			// terminal's default background (verified with a
			// COLORTERM=truecolor PTY capture) — and blinks it at
			// roughly the prompt cursor's own cadence.
			if m.resumeCursorOn {
				b.WriteString("\x1b[7m\x1b[37m \x1b[0m")
			} else {
				b.WriteString(" ")
			}
		}
		b.WriteString("\n")
	}

	if total == 0 {
		// Two distinct empty states, both verified against REF:
		//
		//	OTHER tab (never any conversations on this machine)
		//	  No conversations available.
		//	  Press Enter to return.
		//	  <blank>
		//	  Keyboard: enter Select  tab Switch Tab  esc Go back / Clear search
		//
		//	CLI tab, search matches nothing
		//	  No matching conversations.
		//	  <blank>
		//	  Keyboard: ↑/↓ Navigate  ←/→ Page  ... (the full hint)
		//
		// The CLI hint keeps its navigation half: the list is still
		// navigable as soon as the query is cleared. (A CLI list can only
		// be empty through a non-matching search — /resume refuses to open
		// the picker at all when the store has no conversations.)
		b.WriteString(cHelpDescOpen)
		if m.resumeTab == 0 && !storeEmpty {
			b.WriteString("  No matching conversations.")
		} else {
			b.WriteString("  No conversations available.")
		}
		b.WriteString(cHelpDescClose)
		b.WriteString("\n")
		if m.resumeTab != 0 || storeEmpty {
			b.WriteString(cHelpDescOpen)
			b.WriteString("  Press Enter to return.")
			b.WriteString(cHelpDescClose)
			b.WriteString("\n")
		}
		b.WriteString("\n")
		// The navigation half of the hint is dropped on an empty screen:
		// there is nothing to navigate. REF's empty-store hint is
		// "Keyboard: enter Select  tab Switch Tab  esc Go back / Clear search".
		b.WriteString(resumeKeyboardHint(m.resumeTab == 0 && !storeEmpty, false))
		// Two trailing newlines: the panel carries one blank row before
		// the status row (REF: hint at row 19, blank 20, status 21), and
		// viewRows() counts the newlines so the transcript budget stays
		// right.
		b.WriteString("\n\n")
		return b.String()
	}

	// Clamp the window so the cursor stays inside it. The navigation
	// helpers keep this invariant; the clamp only guards against a
	// resize or a search that shrank the list.
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

	end := m.resumeWindow + window
	if end > total {
		end = total
	}
	for i := m.resumeWindow; i < end; i++ {
		c := rows[i]
		title := c.Title
		if title == "" {
			title = "(no title)"
		}
		// Row layout: the right-hand metadata block is right-anchored
		// at the terminal's last column. REF drops the 15-char project
		// cell (and the two spaces that follow it) when the project's
		// workspace URI is empty, so the block's width depends on the
		// row.
		//
		// Cycle 278: it drops the 12-char age cell the same way when the
		// age is blank, and the title grows by the 14 columns that frees.
		// Verified at 200x40 on a cache whose entries all carry
		// UpdatedAt 0001-01-01: REF's rows read
		//
		//	> 0495d3b3-…<158 spaces>     2 steps
		//
		// with the steps cell flush against column 200, where the clone
		// left 14 blank cells behind a 158-wide title.
		hasAge := resumeAgeLabel(c.ModTime) != ""
		rowRight := 12 // steps
		if c.Project != "" {
			rowRight += 15 + 2
		}
		if hasAge {
			rowRight += 2 + 12
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
		if i == m.resumeCursor {
			// Cursor row: "> " + title in brand blue, project in the
			// dark description gray (only when present), steps + age
			// italic blue.
			b.WriteString(cBlueOpen)
			b.WriteString("> ")
			b.WriteString(title)
			b.WriteString(pickerReset)
			b.WriteString(strings.Repeat(" ", pad))
			if c.Project != "" {
				b.WriteString(cHelpDescOpen)
				b.WriteString(resumeProjectCell(c.Project))
				b.WriteString(cHelpDescClose)
				b.WriteString("  ")
			}
			b.WriteString("\x1b[3m")
			b.WriteString(cBlueOpen)
			b.WriteString(resumeStepsCell(c.StepCount))
			if hasAge {
				b.WriteString(pickerKeyClose)
				b.WriteString("  ")
				b.WriteString("\x1b[3m")
				b.WriteString(cBlueOpen)
				b.WriteString(resumeAgeCell(c.ModTime))
			}
			// Full reset, not just fg: the age cell turned italic on and
			// the next row must not inherit it.
			b.WriteString(pickerKeyClose)
		} else {
			// Normal row: plain title, the whole metadata block in the
			// dark description gray (only the cells that are present —
			// the project cell is dropped when empty, as above). The
			// leading full reset clears the italic left on by a cursor
			// row above (REF's own normal rows start with `\x1b[0m` for
			// the same reason).
			b.WriteString(pickerKeyClose)
			b.WriteString("  ")
			b.WriteString(title)
			b.WriteString(strings.Repeat(" ", pad))
			b.WriteString(cHelpDescOpen)
			if c.Project != "" {
				b.WriteString(resumeProjectCell(c.Project))
				b.WriteString("  ")
			}
			b.WriteString(resumeStepsCell(c.StepCount))
			if hasAge {
				b.WriteString("  ")
				b.WriteString(resumeAgeCell(c.ModTime))
			}
			b.WriteString(cHelpDescClose)
		}
		b.WriteString("\n")
	}

	// Window indicator, only when the list does not fit the window.
	if total > window {
		fmt.Fprintf(&b, "%s  [%d-%d of %d items]%s\n",
			cDimGrayOpen, m.resumeWindow+1, end, total, pickerReset)
	}
	b.WriteString("\n")
	b.WriteString(resumeKeyboardHint(true, resumeProjectGroups(rows) >= 2))
	// Two trailing newlines: the panel carries one blank row before the
	// status row (REF: hint at row 27, blank 28, status 29), and
	// viewRows() counts the newlines so the transcript budget stays right.
	b.WriteString("\n\n")
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
