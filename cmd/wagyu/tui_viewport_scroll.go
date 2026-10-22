package main

import (
	tea "charm.land/bubbletea/v2"
	"sort"
	"strings"
)

func (m *runMode) scrollBy(delta int) {
	if delta == 0 {
		return
	}
	m.autoScroll = false
	m.scroll += delta
	if m.scroll < 0 {
		m.scroll = 0
	}
}

// isUserPromptRow reports whether row is a row that begins a user
// prompt. The user-prompt style wraps `cWarningBlueOpen + "> " +
// text + cWarningBlueClose`, so the row starts with the cWarningBlueOpen
// escape and the byte immediately after it is `>`. Continuation rows of
// a wrapped user prompt don't match (no cWarningBlueOpen at row start)
// and are ignored — only the FIRST row of a user prompt counts.
func isUserPromptRow(row string) bool {
	if !strings.HasPrefix(row, cWarningBlueOpen) {
		return false
	}
	rest := row[len(cWarningBlueOpen):]
	// Strip any leading ANSI that may precede the literal ">".
	for len(rest) >= 2 && rest[0] == '\x1b' {
		// Skip "\x1b[...m" — find the trailing 'm'.
		end := strings.IndexByte(rest, 'm')
		if end < 0 {
			return false
		}
		rest = rest[end+1:]
	}
	return strings.HasPrefix(rest, "> ")
}

// stickyUserPromptRow returns the index (in the full `rows` array) of
// the user-prompt row that should anchor the sticky header, or -1 if
// no sticky header should be shown.
//
// Cycle 179: the sticky header is rendered as a SEPARATE row above
// the scrollable content (one row, prepended by transcriptWindow).
// It is NOT a snap of m.scroll — scrolling stays smooth. The
// "owning" user prompt for the current view is the most recent
// user-prompt row index <= m.scroll: scrolling past a user prompt
// switches the sticky header to that new turn; scrolling within a
// turn keeps the same header.
//
// No sticky header when:
//   - there are no user prompts in the transcript yet (fresh session);
//   - m.scroll == 0 (still inside the banner area, not in any turn);
//   - m.autoScroll is true (pinned to the bottom, the input chrome
//     already has its own "> " prompt at the bottom and a second one
//     at the top would be confusing — recovered behaviour).
func (m *runMode) stickyUserPromptRow() int {
	if len(m.userPromptRows) == 0 || m.autoScroll || m.scroll <= 0 {
		return -1
	}
	// Cycle 281: the index list is sorted, so the owning prompt is a binary
	// search instead of a walk over every prompt in the transcript.
	i := sort.SearchInts(m.userPromptRows, m.scroll+1) - 1
	if i < 0 {
		return -1
	}
	return m.userPromptRows[i]
}

// jumpToPinnedPrompt scrolls the viewport so the producing
// user-prompt row lands at the top of the visible area. It is the
// cycle-253 click/Enter action — the user clicks (or presses Enter
// on) the pinned-prompt header and the viewport jumps to the prompt
// that produced the response they were reading.
//
// The jump is a snap of m.scroll, not a smooth animation: the
// reference behaviour matches the cycle-179 "owning prompt"
// invariant that the prompt should sit at the top of the visible
// region once the user has chosen to jump. autoScroll is cleared
// so subsequent streaming chunks don't tug the viewport back down.
//
// Setting m.scroll = pinnedPromptRowIdx puts the producing prompt
// row at visible[0]; on screen the prompt lands one row below the
// sticky / pinned header that wraps it. The next render re-derives
// stickyUserPromptRow() and keeps the same prompt pinned, so the
// visual state is stable after the jump (no flicker).
//
// Returns true when a jump actually happened, false when no pinned
// prompt is currently shown (callers use this to decide whether to
// fall through to the existing Enter-submits-prompt path).
func (m *runMode) jumpToPinnedPrompt() bool {
	if m.pinnedPromptRowIdx < 0 {
		return false
	}
	m.autoScroll = false
	m.scroll = m.pinnedPromptRowIdx
	return true
}

// pinnedPromptJumpHint is the suffix rendered after the prompt text on
// the pinned-prompt header. The "↳ jump to prompt" hint tells the user
// the row is interactive; the dim-gray wrapper (cDimGrayOpen / Close)
// reads as a small annotation rather than another piece of content.
// The hint is rendered through the same padTo/cellByte helpers the rest
// of the transcript uses so the byte count is independent of prompt
// length.
const pinnedPromptJumpHint = "  ↳ jump to prompt"

// renderPinnedPrompt wraps the producing user-prompt row in the
// cycle-253 pinned-prompt header style. The header is dim/borderless
// (no separator rule below it — the cycle-179 sticky bar adds the
// separator rule, the cycle-253 header doesn't), and it carries a
// trailing hint that names the interactive behaviour.
//
// width is the terminal width in cells. It is currently unused
// (renderPinnedPrompt emits the prompt row verbatim plus the hint)
// but is kept in the API so a future cycle that needs to truncate
// the prompt to fit on narrow terminals can plug in without breaking
// callers.
//
// The function does NOT call padTo; transcriptWindow's append prepends
// the rendered string to the visible slice and padTo runs on every
// row of the slice together, which is cheaper than padding inside
// the helper.
func renderPinnedPrompt(promptRow string, width int) string {
	_ = width
	return promptRow + cDimGrayOpen + pinnedPromptJumpHint + "\x1b[39m"
}

// gotoTop / gotoBottom are the recovered ctrl+home / ctrl+end bindings.
func (m *runMode) gotoTop() {
	m.autoScroll = false
	m.scroll = 0
}

func (m *runMode) gotoBottom() {
	m.autoScroll = true
	m.scroll = 1 << 30
}

// pageRows is the recovered page-scroll distance. A full page keeps one row
// of overlap (the standard pager convention, and what bubbles' PageUp /
// PageDown do); half a page is viewport/2.
func (m *runMode) pageRows() int {
	return clampInt(m.vpRows-1, 1, 1<<20)
}

func (m *runMode) halfPageRows() int {
	return clampInt(m.vpRows/2, 1, 1<<20)
}

// isScrollKey reports whether k is one of the recovered transcript scroll
// bindings (pgup / pgdown / shift+up / shift+down / ctrl+home / ctrl+end).
func isScrollKey(k tea.Key) bool {
	switch k.Code {
	case tea.KeyPgUp, tea.KeyPgDown:
		return true
	case tea.KeyUp:
		return k.Mod.Contains(tea.ModShift)
	case tea.KeyDown:
		return k.Mod.Contains(tea.ModShift)
	case tea.KeyHome:
		return k.Mod.Contains(tea.ModCtrl)
	case tea.KeyEnd:
		return k.Mod.Contains(tea.ModCtrl)
	}
	return false
}

// isScrollWheel reports whether m is a wheel event the alt-screen viewport
// owns (viewport.AltScreenStrategy.ShouldHandleWheel).
func isScrollWheel(m tea.MouseMsg) bool {
	switch m.Mouse().Button {
	case tea.MouseWheelUp, tea.MouseWheelDown:
		return true
	}
	return false
}

// isScrollMsg reports whether msg can only move the viewport and never
// mutates the response transcript. The wrap cache only relies on this being
// a strict subset of the scroll-only set: if a message that mutates slips
// through, the cache will be stale and a later render will show the wrong
// wrap, not a crash. Today the set is the 6 scroll keys plus the 2 wheel
// events.
func isScrollMsg(msg tea.Msg) bool {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return isScrollKey(msg.Key())
	case tea.MouseMsg:
		return isScrollWheel(msg)
	}
	return false
}

// handleScrollKey consumes the recovered transcript scroll bindings:
//
//	pgup / pgdown            page up / page down
//	shift+up / shift+down    half page up / half page down
//	ctrl+home / ctrl+end     go to top / go to bottom
//
// It reports whether the key was consumed. The bindings only apply while the
// alt-screen strategy owns the scroll (viewport.AltScreenStrategy
// .ShouldHandlePageScroll): in inline mode the terminal's own scrollback
// handles them and the keys keep their pre-119 meaning.
func (m *runMode) handleScrollKey(key tea.Key) bool {
	if !m.altScreen {
		return false
	}
	switch {
	case key.Code == tea.KeyPgUp:
		m.scrollBy(-m.pageRows())
		return true
	case key.Code == tea.KeyPgDown:
		m.scrollBy(m.pageRows())
		return true
	case key.Code == tea.KeyUp && key.Mod.Contains(tea.ModShift):
		m.scrollBy(-m.halfPageRows())
		return true
	case key.Code == tea.KeyDown && key.Mod.Contains(tea.ModShift):
		m.scrollBy(m.halfPageRows())
		return true
	case key.Code == tea.KeyHome && key.Mod.Contains(tea.ModCtrl):
		m.gotoTop()
		return true
	case key.Code == tea.KeyEnd && key.Mod.Contains(tea.ModCtrl):
		m.gotoBottom()
		return true
	}
	return false
}

// handleMouseWheel scrolls the transcript on a wheel event. Recovered:
// AltScreenStrategy.ShouldHandleWheel (the alt-screen strategy consumes wheel
// events; the inline strategy leaves them to the terminal) and
// (*mouseState).RenderScrollbar.
//
// Cycle 232: a wheel event landing on the picker overlay scrolls the
// picker (the user is "in the picker") rather than the transcript.
// Outside the picker rectangle the existing transcript-scroll behaviour
// takes over.
func (m *runMode) handleMouseWheel(msg tea.MouseMsg) bool {
	if !m.altScreen {
		return false
	}
	if m.handlePickerWheel(msg.Mouse()) {
		return true
	}
	switch msg.Mouse().Button {
	case tea.MouseWheelUp:
		m.scrollBy(-mouseWheelRows)
		return true
	case tea.MouseWheelDown:
		m.scrollBy(mouseWheelRows)
		return true
	}
	return false
}
