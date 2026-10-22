package main

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/justwasm/pigasus/internal/convstore"
)

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
