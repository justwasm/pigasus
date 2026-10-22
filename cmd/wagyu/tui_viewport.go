package main

// Cycle 119: the alt-screen transcript viewport.
//
// The user-visible bug this fixes: in alt-screen mode the clone had no notion
// of terminal size at all. Every turn appended rows to m.response and View()
// re-emitted the entire screen, so as soon as the view grew taller than the
// terminal bubbletea's renderer lost track of the cursor and the newest
// content became invisible — the clone was unusable for anything longer than
// one screen.
//
// Cycle 122: the viewport was correct but a single scroll keypress on a
// resumed conversation took hundreds of milliseconds. The cause was
// wrapRows calling ansi.Cut in a loop, which re-scans the line from the
// start on every iteration; for a 70 KB model reply the cost is O(w²/width)
// per line. This file replaces the loop with a single-pass cell→byte index
// and adds a per-width wrap cache so scrolling is a cache hit.
//
// Recovered evidence that the reference has a real viewport (1.3.6):
//
//   - pclntab: the reference renders the transcript through bubbles' viewport
//     component —
//     google3/.../charm_land/bubbles/v/v2/viewport/viewport.(*Model)
//     .PageUp / .PageDown / .HalfPageUp / .HalfPageDown / .GotoTop /
//     .GotoBottom / .AtBottom / .AtTop / .ScrollPercent / .SetYOffset /
//     .YOffset / .VisibleLineCount / .TotalLineCount
//   - google3/.../jetski/cli/viewport/viewport.(*AltScreenStrategy)
//     .ShouldHandleWheel / .ShouldHandlePageScroll / .ScrollbarWidth /
//     .FirstViewportIdx / .FirstViewportLineOffset / .OnResize /
//     .OnContentUpdate, plus the package helpers buildScrollbackContent /
//     computeSplitPoint / extractLines / prepareScrollbackContent
//   - google3/.../jetski/cli/store/store.(*Manager)
//     .AutoScroll / .SetAutoScroll / .ScrollOffsets / .SetScrollOffsets
//   - google3/.../jetski/cli/render/render.Scrollbar /
//     .ComputeScrollbarGeometry, and (*mouseState).RenderScrollbar
//   - a View debug format string names the scrollbar:
//     "[View] total=%v styles=%v pending=%v input=%v renderItems=%v
//     splitJoin=%v scrollbar=%v finalJoin=%v"
//   - the /help SHORTCUTS tab (already reproduced in helpShortcuts) lists
//     pgup / pgdown / shift+up (half page up) / shift+down (half page down) /
//     ctrl+home (go to top) / ctrl+end (go to bottom)
//
// What is 【一手】: that a viewport, an auto-scroll flag, a scrollbar and those
// six key bindings exist. What is 【推测】: this file's exact geometry — the
// scrollbar glyphs, the page/half-page row counts, and the choice to
// soft-wrap rather than horizontally scroll. No reference TUI frame could be
// captured (the reference needs a live authenticated session and its quota is
// exhausted), so the rendering cannot be byte-compared.

import (
	"github.com/justwasm/pigasus/internal/tuilib"
	"strings"

)

// Cycle 134: the scrollbar was speculation, not evidence. Every capture in
// /tmp/agycmp/hotpath/*/ref.*.txt has *zero* `│` / `█` characters in the
// transcript area — `grep -c │ ref.*.txt` returns 0 for every scenario, while
// `our.*.txt` returns 7-25. Removing the scrollbar: (a) eliminates the
// one-column wrap offset that was breaking line breaks, (b) fixes the
// "scrollbar broken in the middle" symptom (the thumb formula divided by
// `total` and produced a 1-row thumb on long transcripts, which looked
// like a gap), and (c) reclaims the last cell of every viewport row so the
// input field below the transcript aligns with the rows above it.
//
// If a future capture shows a scrollbar in the reference, reintroduce
// it here with whatever glyph and color the capture pins.

// mouseWheelRows is how many rows one wheel notch scrolls. bubbles'
// viewport.Model defaults MouseWheelDelta to 3; the reference embeds the same
// component, so the same default is the best available evidence.
const mouseWheelRows = 3

// viewRows counts the terminal rows a rendered block occupies. It counts
// logical lines only: every line the view emits is clamped to the terminal
// width before it reaches the terminal, so no emitted line wraps.
func viewRows(s string) int { return tuilib.ViewRows(s) }
func fitWidth(s string, w int) string { return tuilib.FitWidth(s, w) }
func fitLines(s string, w int) string { return tuilib.FitLines(s, w) }
func wrapRows(line string, width int) []string { return tuilib.WrapRows(line, width) }
func cellByteIndex(s string) []int { return tuilib.CellByteIndex(s) }
func skipEscape(s string, i int) int { return tuilib.SkipEscape(s, i) }

func (m *runMode) scrollHeaderRows(width int) []string {
	var b strings.Builder
	// top blank line (preserves the cycle-119 banner's leading empty row)
	b.WriteString("\n")
	renderBanner(&b, m.email, m.model, m.tier, m.cwd)
	// No extra trailing "\n" here: the banner's last Fprintln already
	// terminates with one, so Split("\n") turns it into the trailing
	// empty element that the caller renders as the "blank between banner
	// and top sep" row, matching the pre-124 cursor Y math
	// (top blank + 5 banner + extra blank + top sep = 8 rows before
	// the transcript).
	if m.agentWarning != "" {
		b.WriteString("\n")
		b.WriteString(renderAgentWarning(m.agentWarning))
	}
	if m.fallbackFrom != "" && m.fallbackTo != "" {
		// Cycle 129: prepend a blank row before the warning so the
		// layout matches the reference (banner row 5 → blank row →
		// ⚠ Warning header → ⎿  body → blank row → top sep). The
		// pre-129 banner ended without a blank separator, so the
		// warning visually butted up against the last banner row.
		b.WriteString("\n")
		b.WriteString(renderWarning(m.fallbackFrom, m.fallbackTo))
	}
	var rows []string
	for _, line := range strings.Split(b.String(), "\n") {
		if width > 0 {
			rows = append(rows, wrapRows(line, width)...)
		} else {
			rows = append(rows, line)
		}
	}
	return rows
}

// Cycle 281: incremental wrap + composed-row caches.
//
// The recovered 1.1.2 changelog entry is explicit about what the reference
// fixed: "switching hot-path line-count methods to pointer receivers, cutting
// the per-frame prefix-sum cost and eliminating sustained 99% CPU and
// keystroke lag" on 5000+ step conversations in no-flickering (altscreen)
// mode. The clone's transcriptWindow used to rebuild three O(total rows)
// structures on every render — the composed row slice, its parallel
// screen-row count slice, and the user-prompt index — so a frame cost grew
// with the conversation rather than with the viewport.
//
// Two caches replace that work, both pointer-receiver types, mirroring the
// reference's own `(*lineWidthCache).width` / `(*detailRowsCache).sync` /
// `(*entriesCache).invalidate` shape:
//
//   - wrapSlot holds the wrap of m.response at one width. A rebuild reuses
//     every entry whose source string did not change, so appending a
//     streamed chunk (or invalidating on a keystroke that only touches the
//     input) re-wraps only the tail instead of the whole transcript.
//   - composedRows holds header + response + live rows, their screen-row
//     counts, the prefix sum of those counts, and the user-prompt row
//     indices. A frame that finds it valid does no work proportional to the
//     transcript at all.

// wrapSpan records where one m.response entry's wrapped rows live inside
// wrapSlot.rows.
