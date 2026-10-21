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
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
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
func viewRows(s string) int {
	if s == "" {
		return 0
	}
	n := strings.Count(s, "\n")
	if !strings.HasSuffix(s, "\n") {
		n++
	}
	return n
}

// fitWidth clamps one rendered line to w visible cells, padding with spaces
// when it is short. ANSI escape sequences are preserved and do not count
// toward the width.
func fitWidth(s string, w int) string {
	if strings.ContainsRune(s, '\r') {
		// Cycle 278: a bare CR has zero display width but moves the
		// terminal cursor to column 0; any padding appended after it
		// repaints the row from the left edge. A rendered row must
		// never carry one (see normalizeDetailNewlines).
		s = strings.ReplaceAll(s, "\r", "")
	}
	if w <= 0 {
		return s
	}
	switch sw := ansi.StringWidth(s); {
	case sw > w:
		return ansi.Truncate(s, w, "")
	case sw < w:
		// Cycle 164: trailing pad cells must carry a non-default
		// foreground (separator gray, 38;5;235) so ultraviolet's
		// renderer keeps them as "real" cells rather than collapsing
		// them via [ansi.EraseLineRight]. Without the colour, uv's
		// paintLine + transformLine path emits a single \x1b[K at the
		// last content column and the chrome rows end short of the
		// right wall. With it, the same path emits \x1b[38;5;235m
		// \x1b[K which renders gray-fill to the right edge — same
		// visual as writing 188 literal gray-space cells, but at
		// 11 bytes instead of 200+ (verified at 200×60).
		return s + "\x1b[38;5;235m" + strings.Repeat(" ", w-sw) + "\x1b[39m"
	}
	return s
}

// fitLines clamps every line of a rendered block to w cells. This is what
// makes the fixed-width chrome (the 220-column separator, the padded status
// row, the banner) terminal-size aware: without it those lines wrap on a
// narrow terminal and the renderer's row arithmetic silently breaks.
func fitLines(s string, w int) string {
	if w <= 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	// Cycle 207: a trailing newline leaves a final empty entry that is
	// not a row. Padding it with fitWidth would turn it into a real,
	// space-filled row and every overflowing view would gain a row —
	// which scrolled the /changelog title off the top at h=24.
	last := len(lines) - 1
	for i, line := range lines {
		if i == last && line == "" {
			continue
		}
		lines[i] = fitWidth(line, w)
	}
	return strings.Join(lines, "\n")
}

// wrapRows soft-wraps one rendered line into terminal rows no wider than
// width. width <= 0 disables wrapping. The input may contain embedded
// newlines (a resumed model reply is a single m.response entry with
// paragraphs separated by '\n'); each newline starts a fresh row, so the
// returned slice always contains one entry per terminal row.
//
// ANSI escape sequences that span a newline are dropped at the newline —
// the model-reply styling only colours inline backticks (which never span
// '\n'), so the loss is invisible in practice.
//
// Cycle 122: the previous version called ansi.Cut in a loop, which re-scans
// the line from the start on every iteration. For a 70 KB model reply the
// cost was O(w²/width) per line and a single scroll keypress took hundreds
// of milliseconds. This version builds a cell→byte index in one pass and
// slices from it.
//
// Cycle 153: the input is split on '\n' first. Without this, a multi-paragraph
// model message (a single m.response entry with N newlines) returned N+1 rows
// from the cache but rendered as N+1 terminal rows too — clipToScreenRows
// counted 1 entry while the actual terminal cost was N+1, so a 60-row
// viewport overflowed by (N+1-1)*visibleLines rows. The pre-153 clone dropped
// the difference from the top of View(), shaving the tool-call header off the
// screen on a 200×60 terminal (visible[0:19] = 14 tool rows + 42 model rows
// + 19 padding rows = 75 terminal rows but clipToScreenRows saw 56; View()
// dropped the first 19 rows to fit 60, hiding every tool call). Splitting
// on '\n' makes each cache entry exactly one terminal row so the bookkeeping
// matches the rendering.
func wrapRows(line string, width int) []string {
	if width <= 0 {
		if line == "" {
			return []string{""}
		}
		return strings.Split(line, "\n")
	}
	out := make([]string, 0, strings.Count(line, "\n")+1)
	for _, sub := range strings.Split(line, "\n") {
		if sub == "" {
			out = append(out, "")
			continue
		}
		w := ansi.StringWidth(sub)
		if w <= width {
			out = append(out, sub)
			continue
		}
		idx := cellByteIndex(sub)
		cells := len(idx) - 1
		if cells <= width {
			out = append(out, sub)
			continue
		}
		for start := 0; start < cells; start += width {
			end := start + width
			if end > cells {
				end = cells
			}
			out = append(out, sub[idx[start]:idx[end]])
		}
	}
	return out
}

// cellByteIndex returns idx where idx[c] is the byte offset of the byte just
// past the c-th visible cell, so s[idx[c-1]:idx[c]] is the c-th cell. idx[0]
// is 0 and the last entry is len(s). ANSI escape sequences (CSI, OSC, DCS,
// and 2-byte escapes) are skipped, never contributing cells. Combining marks
// (width 0) are absorbed into the preceding cell's slice so they don't
// desynchronise the boundary.
//
// Cost: O(len(s)), one walk. This is what makes a 70 KB reply wrap in
// microseconds instead of the ~400 ms the ansi.Cut loop took.
func cellByteIndex(s string) []int {
	idx := []int{0}
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			i = skipEscape(s, i)
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if size == 0 {
			i++
			continue
		}
		w := runewidth.RuneWidth(r)
		if w < 0 {
			w = 0
		}
		i += size
		for k := 0; k < w; k++ {
			idx = append(idx, i)
		}
	}
	if last := idx[len(idx)-1]; last != len(s) {
		idx = append(idx, len(s))
	}
	return idx
}

// skipEscape returns the byte index just past the escape sequence starting
// at s[i] (where s[i] == 0x1b). Covers the sequences the transcript actually
// emits: CSI (ESC [ … final), OSC (ESC ] … BEL/ST), DCS-family
// (ESC P/X/^/_ … ST), and 2-byte escapes (ESC + intermediate or final byte).
func skipEscape(s string, i int) int {
	if i >= len(s) || s[i] != 0x1b {
		return i
	}
	i++
	if i >= len(s) {
		return i
	}
	switch s[i] {
	case '[':
		// CSI: ESC [ params(0x30-0x3F) inter(0x20-0x2F) final(0x40-0x7E).
		i++
		for i < len(s) && s[i] >= 0x20 && s[i] <= 0x3F {
			i++
		}
		if i < len(s) && s[i] >= 0x40 && s[i] <= 0x7E {
			i++
		}
	case ']':
		// OSC: ESC ] … BEL or ST.
		i++
		for i < len(s) {
			if s[i] == 0x07 {
				return i + 1
			}
			if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
				return i + 2
			}
			i++
		}
	case 'P', 'X', '^', '_':
		// DCS/SOS/PM/APC: ESC P/X/^/_ … ST.
		i++
		for i < len(s) {
			if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
				return i + 2
			}
			i++
		}
	default:
		// 2-byte escape (ESC =, ESC >, ESC c, …).
		i++
	}
	return i
}

// scrollHeaderRows returns the banner + (optional) fallback warning as
// wrapped rows. They become the first rows of the scrollable area so they
// scroll away when the user scrolls down — the reference binary hides them
// past the first screen, the cycle-123 clone kept them sticky above the
// viewport. Cycle 124 fixes that.
//
// The header is recomputed on every render (it's small — a handful of
// rows — and may change when the email / model / cwd / fallback set
// changes), so no cache lives here.
//
// width <= 0 returns the unwrapped header — useful for the pre-119
// unclipped layout where the terminal size is still unknown.
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
type wrapSpan struct {
	off int
	n   int
}

// wrapSlot is the per-width wrap cache for one row source (m.response at the
// terminal width, or at width-1 when a narrower layout asks for it).
type wrapSlot struct {
	built  bool
	width  int
	src    []string   // snapshot of the source rows the spans were built from
	spans  []wrapSpan // one per src entry, parallel to src
	rows   []string   // flattened wrapped rows
	counts []int      // screen-row count per entry of rows

	// Rebuild scratch, swapped with rows/counts so a rebuild reuses the
	// previous buffers instead of allocating a fresh O(rows) pair.
	scratchRows   []string
	scratchCounts []int
}

// rebuild re-wraps resp at width, reusing the rows of every entry whose
// source string is unchanged. String equality short-circuits on the data
// pointer, so an unchanged entry costs one comparison.
//
// It reports whether anything changed. A false return means the cache still
// describes resp exactly, so the caller must not bump the generation that
// invalidates the composed-row cache: a keystroke that only touches the input
// line then leaves the whole transcript layout untouched.
func (s *wrapSlot) rebuild(resp []string, width int) bool {
	reuse := s.built && s.width == width && len(s.spans) == len(s.src)
	if reuse && len(resp) == len(s.src) {
		same := true
		for i := range resp {
			if resp[i] != s.src[i] {
				same = false
				break
			}
		}
		if same {
			return false
		}
	}
	oldRows, oldCounts := s.rows, s.counts
	oldSpans, oldSrc := s.spans, s.src

	rows := s.scratchRows[:0]
	counts := s.scratchCounts[:0]
	spans := make([]wrapSpan, len(resp))

	for i, line := range resp {
		if reuse && i < len(oldSrc) && oldSrc[i] == line {
			sp := oldSpans[i]
			spans[i] = wrapSpan{off: len(rows), n: sp.n}
			rows = append(rows, oldRows[sp.off:sp.off+sp.n]...)
			counts = append(counts, oldCounts[sp.off:sp.off+sp.n]...)
			continue
		}
		rs := wrapRows(line, width)
		spans[i] = wrapSpan{off: len(rows), n: len(rs)}
		rows = append(rows, rs...)
		for _, r := range rs {
			counts = append(counts, screenRows(r))
		}
	}

	s.scratchRows, s.scratchCounts = oldRows, oldCounts
	s.rows, s.counts = rows, counts
	s.spans = spans
	s.src = append(s.src[:0], resp...)
	s.width = width
	s.built = true
	return true
}

// composedRows is the per-frame layout cache: the rows the viewport scrolls
// (header + response + live rows) plus everything transcriptWindow needs to
// index them in O(viewport) instead of O(transcript).
type composedRows struct {
	valid  bool
	gen    int
	width  int
	header []string
	live   []string
	rows   []string
	counts []int
	prefix []int // prefix[i] == sum(counts[:i]); len(prefix) == len(rows)+1
	prompt []int // indices into rows of the first row of each user prompt

	scratchRows    []string
	scratchCounts  []int
	scratchPrefix  []int
	scratchPrompts []int
}

// fresh reports whether the cached composition still matches this frame.
// gen is bumped by every wrap-cache rebuild, so a response mutation
// invalidates the composition even when the header and live rows are
// byte-identical; the header/live comparisons cover the changes that do not
// go through m.response (model switch, fallback warning, spinner frame).
func (c *composedRows) fresh(gen, width int, header, live []string) bool {
	return c.valid && c.gen == gen && c.width == width &&
		equalRows(c.header, header) && equalRows(c.live, live)
}

func equalRows(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// rebuild recomposes rows/counts/prefix/prompt for this frame. respCounts is
// the cached per-entry screen-row count from the wrap slot (nil falls back to
// a scan).
func (c *composedRows) rebuild(gen, width int, header, resp, live []string, respCounts []int) {
	rows := c.scratchRows[:0]
	rows = append(rows, header...)
	rows = append(rows, resp...)
	rows = append(rows, live...)

	counts := c.scratchCounts[:0]
	for _, r := range header {
		counts = append(counts, screenRows(r))
	}
	if len(respCounts) == len(resp) {
		counts = append(counts, respCounts...)
	} else {
		for _, r := range resp {
			counts = append(counts, screenRows(r))
		}
	}
	for _, r := range live {
		counts = append(counts, screenRows(r))
	}

	prefix := c.scratchPrefix[:0]
	prefix = append(prefix, 0)
	total := 0
	for _, n := range counts {
		total += n
		prefix = append(prefix, total)
	}

	prompts := c.scratchPrompts[:0]
	for i, r := range resp {
		if isUserPromptRow(r) {
			prompts = append(prompts, len(header)+i)
		}
	}

	c.scratchRows, c.scratchCounts = c.rows, c.counts
	c.scratchPrefix, c.scratchPrompts = c.prefix, c.prompt
	c.rows, c.counts, c.prefix, c.prompt = rows, counts, prefix, prompts
	c.header = append(c.header[:0], header...)
	c.live = append(c.live[:0], live...)
	c.gen = gen
	c.width = width
	c.valid = true
}

// wrappedResponse returns the wrapped rows of m.response at the given width,
// served from the per-width wrap cache. The full-width slot is
// m.rowsCache; an explicitly requested narrower width (width-1, the
// scrollbar column of pre-cycle-134 layouts) gets its own slot so it never
// clobbers the main one.
//
// Cycle 281: the slot is rebuilt through (*wrapSlot).rebuild, which only
// re-wraps the entries that changed, and each rebuild bumps m.rowsGen so the
// composed-row cache above invalidates with it.
func (m *runMode) wrappedResponse(width int) []string {
	if width == m.width {
		if m.rowsCacheDirty || !m.rowsCache.built || m.rowsCache.width != width {
			if m.rowsCache.rebuild(m.response, width) {
				m.rowsGen++
			}
			m.rowsCacheDirty = false
		}
		return m.rowsCache.rows
	}
	if !m.rowsCacheN1.built || m.rowsCacheN1.width != width {
		if m.rowsCacheN1.rebuild(m.response, width) {
			m.rowsGen++
		}
	}
	return m.rowsCacheN1.rows
}

// wrappedResponseCounts returns the screen-row counts slice parallel to
// wrappedResponse(width). Cycle 124; nil means "no counts available", which
// the caller must treat as "scan the rows yourself".
func (m *runMode) wrappedResponseCounts(width int) []int {
	_ = m.wrappedResponse(width)
	if width == m.width {
		return m.rowsCache.counts
	}
	return m.rowsCacheN1.counts
}

// clipToScreenRowsPrefix is clipToScreenRows with the counts replaced by
// their prefix sum, so the trim is a binary search instead of a rescan of the
// visible window per trimmed row. Both must return the same slice; see
// TestClipToScreenRowsPrefixMatches.
func clipToScreenRowsPrefix(visible []string, prefix []int, scrollOff, maxRows int) []string {
	if len(visible) == 0 {
		return visible
	}
	limit := prefix[scrollOff] + maxRows
	// Largest k <= len(visible) whose rows still fit; counts are >= 1 so
	// the predicate is monotone.
	k := sort.Search(len(visible)+1, func(i int) bool {
		return prefix[scrollOff+i] > limit
	}) - 1
	if k < 0 {
		k = 0
	}
	return visible[:k]
}

// liveRows returns the rows that the model stream is currently appending:
// the thinking frame (when the model is inside a think block) and the
// spinner + active-tool hint. These change every frame during streaming, so
// they bypass the cache and are computed fresh on every render. When the
// stream is idle, liveRows returns nil.
func (m *runMode) liveRows(width int) []string {
	if !m.streaming {
		return nil
	}
	var rows []string
	// The reference shows the "Thinking…" live frame only while the model
	// itself is thinking; once a tool is in flight the spinner carries the
	// activity ("Running command...") and the thinking row is gone.
	if m.thinking && m.activeTool == "" {
		rows = append(rows, thinkingLiveLine(time.Since(m.thinkingSince)))
	}
	line := m.spinner.View()
	// Cycle 99: the spinner line names the activity currently in flight.
	// Cycle 285 restored the reference's labels and animation: "Generating"
	// before any call, "Working" once the turn has run a tool, and the
	// per-category "Running command" / "Reading file" while a call
	// executes — each in base0 with a sliding #268BD2 highlight.
	phrase := "Generating"
	if m.anyToolRun {
		phrase = "Working"
	}
	if m.activeTool != "" {
		phrase = m.activeTool
	}
	line += " " + spinnerText(phrase, m.spinFrame)
	return append(rows, wrapRows(line, width)...)
}

// transcriptRows is the unclipped, pre-119 row set the byte-exact snapshots
// and the unit tests assert. It is only consulted when the terminal size is
// unknown (m.height == 0); the live, sized path goes through
// transcriptWindow.
//
// Cycle 124: the banner + warning block are now part of the scrollable area
// (they scroll away when the user scrolls down), so transcriptRows prepends
// the header rows for consistency with transcriptWindow. The cursor Y math
// in the byte-exact snapshot tests still works because the new layout
// happens to put the prompt at the same row offset as the pre-124 layout:
// header (7 lines: top blank + 5 banner + trailing blank) + transcript + top
// sep + prompt = the same total as the pre-124 (top blank + 5 banner + blank
// + top sep + transcript + prompt).
func (m *runMode) transcriptRows(width int) []string {
	// The picker overlay replaces the transcript (pre-119 behaviour).
	if m.picker.Visible() {
		return nil
	}
	rows := m.scrollHeaderRows(width)
	for _, line := range m.response {
		rows = append(rows, wrapRows(line, width)...)
	}
	if m.streaming && m.thinking && m.activeTool == "" {
		rows = append(rows, thinkingLiveLine(time.Since(m.thinkingSince)))
	}
	if m.streaming {
		// Cycle 76: while the model stream is in flight the view appends the
		// Braille spinner + "  Generating..." row. The v1.3.5 binary emits
		// one spinner glyph followed by 2 spaces and the literal text
		// "Generating..." (ref capture /tmp/ref_streaming_t0.bin shows
		// "> prompt\n<BRAILLE>  Generating..." with NO leading indent).
		// The bubbles sp.Dot frames each carry a trailing space ("⣾ "), so
		// one more space lands on the ref pattern of 2 spaces between glyph
		// and word.
		line := m.spinner.View()
		if m.activeTool != "" {
			line += " " + m.activeTool
		} else {
			line += " Generating..."
		}
		rows = append(rows, wrapRows(line, width)...)
	}
	return rows
}

// screenRows returns the number of visible terminal rows an entry produces
// when written to the body — i.e. 1 + the number of embedded "\n"s. A flat
// line is one row, a multi-line entry is several. Cycle 124: the cycle-122
// wrap cache keeps each entry fitting inside width, but m.response rows can
// already contain newlines (multi-line prompts / replies). When they do,
// len(visible) under-counts what's actually rendered and View()'s drop
// logic would shave rows from the top of the body — including the banner —
// to make the height fit. screenRows + a clipToScreenRows floor fix this.
func screenRows(entry string) int {
	if entry == "" {
		return 1
	}
	return 1 + strings.Count(entry, "\n")
}

// screenRowCounts builds a screen-row count for every entry of `rows`.
// Cycle 124: clipToScreenRows is called on every render from
// transcriptWindow, so counting "\n"s per entry on the hot path would
// re-scan the entire transcript each scroll keypress. Pre-computing once
// when `rows` is built keeps the cache hit on a scroll as cheap as the
// cycle-122 wrap cache was — microseconds, not milliseconds — even on
// 35 KB model-reply entries.
func screenRowCounts(rows []string) []int {
	counts := make([]int, len(rows))
	for i, r := range rows {
		counts[i] = screenRows(r)
	}
	return counts
}

// clipToScreenRows trims visible from the right until its total rendered
// row count fits inside maxRows. Cycle 124.
//
//	visible       rows[m.scroll:m.scroll+avail] (the candidate slice)
//	screenCounts  pre-computed screen-row count for every entry of `rows`
//	scrollOff    the index into screenCounts of visible[0]
//	maxRows       the on-screen row budget for the scrollable area (avail)
//
// When every entry happens to be one screen row this is a no-op. When some
// entries span multiple rows, the budget is exceeded by however many
// extra rows those entries generate — clipping the tail keeps the prompt
// and status row anchored at the bottom and preserves the banner at the
// top when the user is scrolled to the very start.
func clipToScreenRows(visible []string, screenCounts []int, scrollOff, maxRows int) []string {
	if len(visible) == 0 {
		return visible
	}
	for {
		total := 0
		for i := range visible {
			total += screenCounts[scrollOff+i]
		}
		if total <= maxRows {
			return visible
		}
		visible = visible[:len(visible)-1]
		if len(visible) == 0 {
			return visible
		}
	}
}

// viewOverflowRows is how far past the fold REF lets the emitted view
// run before it stops emitting rows entirely. Recovered from the /help
// overlay (the only view tall enough to overflow) by scanning REF
// v1.4.0 heights: the status row stays visible while
// `content <= height + 8` and is truncated away beyond that. See
// View() for the measurements.
const viewOverflowRows = 8

// viewportHeight is the number of rows the transcript viewport may occupy:
// the terminal height minus everything else the view renders (top sep,
// input, bottom sep, picker/help overlays, status row).
//
// Cycle 182: while the /help overlay is open the panel is *not* charged
// against the transcript. REF renders the transcript, then the panel,
// then the status row, and simply lets the terminal scroll — the emitted
// view is capped at `height + viewOverflowRows` rows, so a tall panel
// truncates the panel itself rather than the banner above it. Charging
// the panel here (the pre-182 behaviour) clipped the transcript to a
// single row at 220×30 and scrolled the banner off, which REF never does.
func (m *runMode) viewportHeight() int {
	if m.height <= 0 {
		return 0
	}
	body, _ := m.viewBody(nil)
	extra := viewRows(body)
	budget := m.height
	if m.helpMode {
		extra -= viewRows(renderHelp(m.helpTab, m.model, m.tier, m.cwd, m.email, m.conversationID, m))
		budget += viewOverflowRows
	}
	avail := budget - extra
	if avail < 1 {
		avail = 1
	}
	return avail
}

// transcriptWindow returns the transcript rows to emit. When the terminal
// size is unknown it is the whole transcript (the pre-119 layout, which the
// byte-exact snapshots and the unit tests rely on). Otherwise the transcript
// is clipped to the viewport, scrolled to m.scroll.
//
// Cycle 122: the wrap of m.response is served from a per-width cache
// (wrappedResponse) so a scroll is a slice of an already-built slice rather
// than a full re-wrap.
//
// Cycle 124: the banner + fallback warning block are the first rows of the
// scrollable area, so they scroll away when the user scrolls down. The
// reference binary hides them past the first screen — the cycle-123 clone
// kept them sticky above the viewport.
func (m *runMode) transcriptWindow() []string {
	avail := m.viewportHeight()
	if avail <= 0 {
		m.vpRows = 0
		return m.transcriptRows(0)
	}

	// Scrollable content = header rows + wrapped response + live rows.
	// Cycle 281: the composition (rows, counts, prefix sums, prompt
	// indices) is cached and only rebuilt when the response, the width, or
	// the header/live rows changed. A warm scroll frame touches the
	// viewport, not the transcript.
	header := m.scrollHeaderRows(m.width)
	resp := m.wrappedResponse(m.width)
	live := m.liveRows(m.width)
	respCounts := m.wrappedResponseCounts(m.width)
	if !m.rowsView.fresh(m.rowsGen, m.width, header, live) {
		m.rowsView.rebuild(m.rowsGen, m.width, header, resp, live, respCounts)
	}
	rows := m.rowsView.rows
	// Cycle 179: the indices of user-prompt rows in `rows`. Used by the
	// sticky-bar logic (separate from the scrollable rows, so PgUp doesn't
	// jump to a user prompt — see scrollBy comment). Cycle 281: computed
	// once with the composition, not rescanned per frame, and aliased —
	// transcriptWindow must never append to it.
	m.userPromptRows = m.rowsView.prompt
	// Cycle 179: the sticky user-prompt bar takes one row above the
	// scrollable content when the viewport is scrolled away from the
	// bottom. m.stickyBarRow caches the rendered text so viewBody can
	// prepend it without re-walking the rows.
	//
	// Cycle 253: in altscreen mode the cycle-179 sticky bar is replaced
	// by the AGY 1.3.3 "pinned prompt header" — a single dim/borderless
	// row at the top of the viewport with the producing prompt + a
	// "↳ jump" hint. Inline mode keeps the cycle-179 sticky bar with
	// separator rule because the terminal's own scrollback already
	// provides context and the separator matches the rest of the
	// transcript chrome. Both fields are populated from the same
	// stickyUserPromptRow() computation; the only difference is the
	// altscreen branch stores m.pinnedPromptText + pinnedPromptRowIdx
	// and reserves 1 row instead of 2.
	m.stickyBarRow = ""
	m.pinnedPromptRowIdx = -1
	m.pinnedPromptText = ""
	if idx := m.stickyUserPromptRow(); idx >= 0 {
		// idx is an absolute index into `rows`. Subtract the header
		// length to index into `resp`.
		respIdx := idx - len(header)
		if respIdx >= 0 && respIdx < len(resp) {
			if m.altScreen {
				// Cycle 253: render the pinned prompt header (no
				// separator rule) and remember the row index so the
				// click/Enter handlers can jump back to it.
				m.pinnedPromptRowIdx = idx
				// width is plumbed in below as `effective`; until
				// that variable is declared we pass m.width which is
				// the same value (transcriptWindow has no other
				// knob for it).
				m.pinnedPromptText = renderPinnedPrompt(resp[respIdx], m.width)
			} else {
				m.stickyBarRow = resp[respIdx]
			}
		}
	}
	availScroll := avail
	// Cycle 179: the sticky user-prompt bar takes 2 rows above the
	// scrollable content when shown — the bar itself + a thin
	// separator rule in the same gray the other separators use
	// (cRule = 38;2;218;220;224). Verified against the reference on
	// `4638a7b0-…`: the sticky bar is always followed by a single
	// ────…─── rule, matching the visual layout the rest of the
	// transcript uses (separator between the prompt and the input
	// region). At the bottom (autoScroll on) the sticky bar is
	// absent so no row is reserved here.
	//
	// Cycle 253: in altscreen mode the pinned-prompt header is a
	// single row (no separator rule, dim/borderless styling) so the
	// scrollable area only loses 1 row, not 2.
	if m.altScreen && m.pinnedPromptText != "" {
		availScroll -= 1
	} else if m.stickyBarRow != "" {
		availScroll -= 2
	}
	overflow := len(rows) > availScroll
	effective := m.width
	// Cycle 134: the reference has no scrollbar column, so we no longer
	// reserve the last cell for one when overflowing. The transcript
	// wraps to the full terminal width at every state.
	_ = overflow
	// Cycle 281: counts and their prefix sum come from the composition
	// cache; a warm frame does not rescan the transcript here.
	if !overflow {
		m.scroll = 0
		m.autoScroll = true
		return rows
	}

	maxScroll := len(rows) - availScroll
	if m.autoScroll {
		m.scroll = maxScroll
	}
	m.scroll = clampInt(m.scroll, 0, maxScroll)
	// Reaching the bottom re-arms auto-scroll, mirroring
	// store.Manager.SetAutoScroll(true) on GotoBottom.
	if m.scroll >= maxScroll {
		m.autoScroll = true
	}

	// Cycle 281: copy only the viewport window out of the cached rows
	// before padding. `visible` used to be a fresh per-frame slice; now it
	// aliases the composition cache, and append() must not write past the
	// window into the next cached row.
	visible := append([]string(nil), rows[m.scroll:m.scroll+availScroll]...)
	// Cycle 124: a multi-line entry counts as more than one screen row, so
	// the raw slice can exceed avail. Trim from the tail to preserve the
	// banner at the top when scrolled to the very start (see cycle-123
	// deliverable: View()'s drop-from-top would otherwise shave the banner
	// itself off the screen). Cycle 281: the trim is a binary search over
	// the cached prefix sums.
	visible = clipToScreenRowsPrefix(visible, m.rowsView.prefix, m.scroll, availScroll)
	// Cycle 152: clipToScreenRows may return fewer than avail entries when
	// wrapped entries are trimmed. Without padding, the chrome (top sep +
	// input + bottom sep + status) sits immediately below the last
	// transcript row, leaving blank rows at the bottom of the screen — and
	// the input field visibly jumps up and down as the user scrolls because
	// the visible-row count fluctuates with wrapping. Pad the visible slice
	// with blanks so it always fills `avail` rows: the chrome then anchors
	// at the bottom of the screen regardless of how many transcript entries
	// happen to be visible. The blank rows read as empty transcript lines,
	// which is the same visual state the reference binary produces when the
	// conversation does not fill the viewport.
	for len(visible) < availScroll {
		visible = append(visible, "")
	}
	// Cycle 179: prepend the sticky user-prompt bar (and the
	// separator rule directly below it) when scrolled away from the
	// bottom. Both rows live outside the scrollable content so the
	// bar stays pinned at the top of the viewport while the rows
	// below scroll. The separator matches the gray the other
	// separators use so the sticky-bar block reads as a single
	// visual unit (bar + rule) above the response.
	//
	// Cycle 253: in altscreen mode prepend the pinned-prompt header
	// (a single dim/borderless row) instead. The header is rendered
	// with a "↳ jump to prompt" hint so the user knows the row is
	// interactive (click / Enter scrolls back to the prompt).
	// stickyBarRow stays empty in the altscreen branch above so the
	// two branches are mutually exclusive.
	if m.altScreen && m.pinnedPromptText != "" {
		visible = append([]string{padTo(m.pinnedPromptText, effective)}, visible...)
	} else if m.stickyBarRow != "" {
		sticky := []string{padTo(m.stickyBarRow, effective), cRule(effective)}
		visible = append(sticky, visible...)
	}
	out := make([]string, len(visible))
	for i, row := range visible {
		out[i] = padTo(row, effective)
	}
	// Cycle 179: vpRows is what the page-scroll helpers use; keep it
	// aligned with the FULL viewport (sticky bar + scrollable area)
	// so the recovered ctrl+home / ctrl+end / pgup / pgdown keys
	// keep their cycle-119 semantics. The sticky bar doesn't change
	// the per-keystroke scroll distance.
	m.vpRows = avail
	return out
}

// padTo clamps a line to exactly w cells (truncating or padding with spaces).
func padTo(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if strings.ContainsRune(s, '\r') {
		// Cycle 278: see fitWidth — a bare CR would make the pad
		// overwrite the row from column 0.
		s = strings.ReplaceAll(s, "\r", "")
	}
	switch sw := ansi.StringWidth(s); {
	case sw > w:
		return ansi.Truncate(s, w, "")
	case sw < w:
		// Cycle 164: see fitWidth — non-default fg keeps the cells
		// real in ultraviolet's transformLine so they render as
		// gray-fill to the right wall rather than collapsing via
		// [ansi.EraseLineRight] (which would still look gray if
		// the trailing pad cells matched `blank`, but with a
		// different fg would lose the colour entirely).
		return s + "\x1b[38;5;235m" + strings.Repeat(" ", w-sw) + "\x1b[39m"
	}
	return s
}

// clampInt clamps v into [lo, hi]. hi < lo collapses to lo.
func clampInt(v, lo, hi int) int {
	if hi < lo {
		hi = lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// scrollBy moves the transcript viewport by delta rows and clears
// auto-scroll. The upper bound is applied on the next render, which is where
// the row count is known.
//
// Cycle 179: scrolling is intentionally NOT snapped to a user-prompt
// row. The user-prompt anchor is rendered as a separate sticky bar in
// View() (one row above the scrollable content), so a scroll key
// moves the scrollable rows smoothly and the sticky bar updates
// underneath as the visible turn boundary changes. Snapping
// m.scroll itself turns PgUp into "jump to the previous turn" and
// breaks the smooth-scroll contract (raised in the cycle-179 review).
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
