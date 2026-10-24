package tuiviewport

import (
	"github.com/justwasm/pigasus/internal/tuilib"
)

// WrapSpan records where one source entry's wrapped rows live inside WrapSlot.Rows.
type WrapSpan struct {
	Off int
	N   int
}

// WrapSlot is the per-width wrap cache for one row source.
type WrapSlot struct {
	Built  bool
	Width  int
	Src    []string   // snapshot of the source rows the spans were built from
	Spans  []WrapSpan // one per src entry, parallel to src
	Rows   []string   // flattened wrapped rows
	Counts []int      // screen-row count per entry of rows

	ScratchRows   []string
	ScratchCounts []int
}

// Rebuild re-wraps resp at width, reusing unchanged entries.
func (s *WrapSlot) Rebuild(resp []string, width int) bool {
	reuse := s.Built && s.Width == width && len(s.Spans) == len(s.Src)
	if reuse && len(resp) == len(s.Src) && equalRows(resp, s.Src) {
		return false
	}
	oldRows, oldCounts := s.Rows, s.Counts
	oldSpans, oldSrc := s.Spans, s.Src

	rows := s.ScratchRows[:0]
	counts := s.ScratchCounts[:0]
	spans := make([]WrapSpan, len(resp))

	for i, line := range resp {
		if reuse && i < len(oldSrc) && oldSrc[i] == line {
			sp := oldSpans[i]
			spans[i] = WrapSpan{Off: len(rows), N: sp.N}
			rows = append(rows, oldRows[sp.Off:sp.Off+sp.N]...)
			counts = append(counts, oldCounts[sp.Off:sp.Off+sp.N]...)
			continue
		}
		rs := tuilib.WrapRows(line, width)
		spans[i] = WrapSpan{Off: len(rows), N: len(rs)}
		rows = append(rows, rs...)
		for _, r := range rs {
			counts = append(counts, ScreenRows(r))
		}
	}

	s.ScratchRows, s.ScratchCounts = oldRows, oldCounts
	s.Rows, s.Counts = rows, counts
	s.Spans = spans
	s.Src = append(s.Src[:0], resp...)
	s.Width = width
	s.Built = true
	return true
}

// ComposedRows is the per-frame layout cache.
type ComposedRows struct {
	Valid  bool
	Gen    int
	Width  int
	Header []string
	Live   []string
	Rows   []string
	Counts []int
	Prefix []int // Prefix[i] == sum(Counts[:i]); len(Prefix) == len(Rows)+1
	Prompt []int // indices into Rows of the first row of each user prompt

	ScratchRows    []string
	ScratchCounts  []int
	ScratchPrefix  []int
	ScratchPrompts []int
}

// Fresh reports whether the cached composition still matches this frame.
func (c *ComposedRows) Fresh(gen, width int, header, live []string) bool {
	return c.Valid && c.Gen == gen && c.Width == width &&
		equalRows(c.Header, header) && equalRows(c.Live, live)
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

// Rebuild recomposes Rows/Counts/Prefix/Prompt for this frame.
func (c *ComposedRows) Rebuild(gen, width int, header, resp, live []string, respCounts []int) {
	rows := c.ScratchRows[:0]
	rows = append(rows, header...)
	rows = append(rows, resp...)
	rows = append(rows, live...)

	counts := c.ScratchCounts[:0]
	for _, r := range header {
		counts = append(counts, ScreenRows(r))
	}
	if len(respCounts) == len(resp) {
		counts = append(counts, respCounts...)
	} else {
		for _, r := range resp {
			counts = append(counts, ScreenRows(r))
		}
	}
	for _, r := range live {
		counts = append(counts, ScreenRows(r))
	}

	c.ScratchPrefix, c.ScratchPrompts = c.Prefix, c.Prompt
	prefix, prompts := buildPrefixAndPrompts(c.ScratchPrefix[:0], c.ScratchPrompts[:0], counts, resp, len(header))

	c.ScratchRows, c.ScratchCounts = c.Rows, c.Counts
	c.Rows, c.Counts, c.Prefix, c.Prompt = rows, counts, prefix, prompts
	c.Header = append(c.Header[:0], header...)
	c.Live = append(c.Live[:0], live...)
	c.Gen = gen
	c.Width = width
	c.Valid = true
}

func buildPrefixAndPrompts(prefix, prompts, counts []int, resp []string, headerLen int) ([]int, []int) {
	prefix = append(prefix, 0)
	total := 0
	for _, n := range counts {
		total += n
		prefix = append(prefix, total)
	}
	for i, r := range resp {
		if IsUserPromptRow(r) {
			prompts = append(prompts, headerLen+i)
		}
	}
	return prefix, prompts
}
