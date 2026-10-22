package main

import (
	"sort"
	"time"
	"strings"

)

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
