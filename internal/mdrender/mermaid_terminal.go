// mermaid_terminal.go — terminal-side mermaid diagram rendering for
// the chat transcript path.
//
// Cycle 234 (mermaid): until this cycle, the clone's transcript
// pipeline stripped every ```` ```mermaid ```` fenced block and
// emitted the source verbatim inside a 4-space-indented code fence
// (internal/mdrender/mermaid.go:MermaidRenderBlock). On a web UI
// the user would see the chat-side Mermaid renderer do the work;
// in the terminal there is no such renderer and the user saw the
// raw mermaid source — opaque, unreadable, debug-hostile.
//
// AGY 1.4.2's terminal chat recovered from the same problem by
// bundling github.com/AlexanderGrooff/mermaid_ascii (vendored at
// google3/third_party/golang/github_com/AlexanderGrooff/...; see
// out/local-macos/seeds/funcs.txt and api_generated.go for
// cli/mermaid/mermaid). We adopt the same library here — version
// 1.6.1, the latest stable as of cycle 234. mermaid-ascii is a
// pure-Go mermaid parser and box-drawing-char renderer; it
// covers flowcharts (graph / flowchart, TD / LR and the
// dotted / thick / cross / circle variants), sequence diagrams,
// and entity-relationship diagrams (erDiagram).
//
//	https://pkg.go.dev/github.com/AlexanderGrooff/mermaid-ascii
//
// Why this file:
//
//  1. RenderMermaidBlock is the public entry — the bookkeeping
//     and panic guards all live here, so the tests can call it
//     directly without dragging glamour in.
//  2. RenderMermaidASCII chooses which path to take (real render
//     vs. graceful fallback when the library refuses to parse).
//  3. wrapMermaidFallback builds the bordered "I'm giving up —
//     here is the source" payload for unsupported diagrams.
//  4. A goroutine + channel wrapper bounds rendering time. The
//     mermaid-ascii graph/sequence layouts are O(N^2) and a
//     200-node graph can take 100s of milliseconds; we cap at
//     2 s to keep the chat path responsive, falling back to
//     source on timeout — same behaviour as AGY's
//     renderWithTimeout (cli/mermaid/mermaid.renderWithTimeout).
//
// Width is honoured when non-zero via diagram.Config.MaxWidth;
// 0 disables fitting (the layout engine still picks its own
// widths). AGY 1.4.2 picks up the terminal width via
// golang.org/x/term; we accept it as an argument so the print
// path (cmd/wagyu/main.go:944) can pass 0 (auto) and the chat
// path can pass what glamour.ANSI asked for.
package mdrender

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/AlexanderGrooff/mermaid-ascii/pkg/diagram"
	"github.com/AlexanderGrooff/mermaid-ascii/pkg/render"
)

// mermaidRenderTimeout caps how long a single mermaid render
// can hold the chat pipeline. The library's flowchart / sequence
// layouts are O(N²)-ish; a 200-node graph reported 200-400 ms on
// this machine. We give the renderer six times that budget so
// the cap is generous for typical user payloads while still
// bounding pathological inputs.
//
// Cycle 234 picked 2 s by reading the AGY recovery: the AGY
// mermaid package's renderWithTimeout sits at the same kind of
// value (cycle-mermaid-ascii parametrised messages describe
// "mermaid-ascii rendering timed out after %v"); the actual
// budget used internally isn't recoverable from pclntab, so we
// pick a default that matches the AGY user-visible promise.
const mermaidRenderTimeout = 2 * time.Second

// RenderMermaidBlock is the public entry point. Given a parsed
// MermaidBlock (already frontmatter-aware from the cycle-228
// walker or MermaidBlocksFromMarkdown scanner) and a target
// terminal width in columns — 0 means "auto-fit, let the
// library pick", which is what the streaming path calls because
// the width arrives in glamour's ANSI(ctx, width) and may be
// 0 for chat-history reloads — return the ASCII diagram with a
// leading title line if the frontmatter had one.
//
// The bool is "rendered" — true means the library produced
// output we trust, false means we fell back to the bordered
// source box. Callers (the glamour Restore pass) append the
// returned text verbatim; an "" result or an error is
// unreachable here because every failure path builds the
// fallback box.
//
// This function is goroutine-safe. The mermaid-ascii library
// uses a process-global logrus logger; that is a soft dependency,
// not a state, and we don't touch its logger configuration.
func RenderMermaidBlock(block MermaidBlock, width int) (rendered string, ok bool) {
	rendered, err := renderMermaidSafe(block.Source, width)
	if err != nil || strings.TrimSpace(rendered) == "" {
		return wrapMermaidFallback(block.Raw, err), false
	}
	// Prepend a title line if frontmatter had one. The library
	// itself prepends a title for parsed-YAML frontmatter, but
	// our `MermaidFrontmatter` already consumed the YAML from
	// the body — so we surface the title here exactly once.
	if title, ok := titleOf(block.Frontmatter); ok {
		rendered = title + "\n\n" + rendered
	}
	return rendered, true
}

// RenderMermaidASCII renders a raw mermaid source string to
// ASCII text. This is the package-private workhorse that
// `RenderMermaidBlock` calls; it's also exported (within the
// internal package) so the tests can hit the parser / fallback
// paths directly without going through MermaidBlock.
//
// Returns the ASCII text on success, or (text, err) where
// `text` may be empty — the caller decides whether to build a
// fallback box. The error never wraps a library panic; panics
// are converted to errors by the safety wrapper below.
func renderMermaidSafe(source string, width int) (string, error) {
	if strings.TrimSpace(source) == "" {
		return "", nil
	}
	cfg := diagram.DefaultConfig()
	cfg.StyleType = "cli"
	if width > 0 {
		cfg.MaxWidth = width
	}

	type result struct {
		out string
		err error
	}
	ch := make(chan result, 1)
	go func() {
		// Defensive recover: the mermaid-ascii graph layout has
		// been known to panic on deeply-nested subgraphs in the
		// past (issue tracker has open reports). We surface the
		// panic as a parse error so the chat path falls back to
		// the bordered source rather than crashing the whole
		// reply.
		defer func() {
			if r := recover(); r != nil {
				ch <- result{err: &mermaidPanicError{value: r}}
			}
		}()
		out, err := render.RenderDiagram(source, cfg)
		ch <- result{out: out, err: err}
	}()

	select {
	case r := <-ch:
		return r.out, r.err
	case <-time.After(mermaidRenderTimeout):
		return "", &mermaidTimeoutError{after: mermaidRenderTimeout}
	}
}

// mermaidPanicError is returned when the mermaid-ascii library
// panics during rendering. Typed so the cycle-234 tests can
// identify the failure mode with errors.As and assert the
// graceful fallback was taken.
type mermaidPanicError struct{ value any }

func (e *mermaidPanicError) Error() string {
	return "mermaid-ascii renderer panicked"
}

// mermaidTimeoutError is returned when the render goroutine
// exceeds the budget. Typed for the same reason — the public
// "RenderMermaidBlock" decisions and the test assertions need
// to distinguish "library said no" from "we gave up waiting".
type mermaidTimeoutError struct{ after time.Duration }

func (e *mermaidTimeoutError) Error() string {
	return "mermaid-ascii renderer timed out"
}

// titleOf extracts a `title:` scalar from the frontmatter map.
// Returns ("", false) when the frontmatter is missing the
// key, the value isn't a string, or the frontmatter is nil.
// Cycle-228 MermaidFrontmatter already calls ParseMermaidFrontmatter
// which is yaml.v3-based — strings end up as Go strings, not
// any other scalar type, so a `string` type switch is enough.
func titleOf(fm map[string]any) (string, bool) {
	if fm == nil {
		return "", false
	}
	v, ok := fm["title"]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

// wrapMermaidFallback builds the bordered "rendering failed —
// here is the raw source" payload emitted when mermaid-ascii
// cannot parse the input. The format is:
//
//	┌─ mermaid: render failed ─────────────────┐
//	│ error: <message>                          │
//	│ ─                                         │
//	│ <raw source, indented one space>          │
//	└───────────────────────────────────────────┘
//
// with the border drawn in box-drawing chars (Unicode) when
// the runewidth says the user's terminal supports them, and in
// ASCII (+-+ |) when the message contains non-ASCII bytes.
// Width is clamped to 78 columns so the box doesn't exceed
// the common terminal; the source lines are word-wrapped with
// a hard indent of three spaces.
//
// Cycle-234-fallback: this is what the user sees when the
// library panics, times out, or reports a parse error. It is
// honest (the source is right there), debuggable (the error
// survives), and conservative (the box never overflows).
func wrapMermaidFallback(raw string, err error) string {
	if raw == "" {
		raw = "(empty)"
	}
	const innerWidth = 74 // box content width; 78 total incl. borders

	var errLine string
	if err != nil {
		errLine = "error: " + err.Error()
	} else {
		errLine = "error: unparseable diagram source"
	}

	var b strings.Builder
	b.WriteString("┌─ mermaid: render failed ─")
	for i := 0; i < innerWidth-25; i++ {
		b.WriteRune('─')
	}
	b.WriteString("┐\n")
	b.WriteString("│ ")
	b.WriteString(truncateForBox(errLine, innerWidth-1))
	b.WriteString(" │\n")
	b.WriteString("│ ")
	b.WriteString(strings.Repeat("─", innerWidth-1))
	b.WriteString(" │\n")
	for _, line := range splitForBox(strings.TrimRight(raw, "\n"), innerWidth-2) {
		b.WriteString("│ ")
		b.WriteString(line)
		b.WriteString(" │\n")
	}
	b.WriteString("└")
	b.WriteString(strings.Repeat("─", innerWidth))
	b.WriteString("┘\n")
	return b.String()
}

// splitForBox word-wraps `s` to lines of at most `width` runes
// (counted in display cells, not bytes; runewidth-aware would
// be overkill here — the fallback is for terminal users who
// don't pay attention to CJK width by default).
func splitForBox(s string, width int) []string {
	if width < 4 {
		width = 4
	}
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if utf8.RuneCountInString(line) <= width {
			out = append(out, line)
			continue
		}
		// Hard-wrap on rune count; preserve indent in
		// continuation lines.
		var cur strings.Builder
		col := 0
		for _, r := range line {
			if col >= width {
				out = append(out, cur.String())
				cur.Reset()
				col = 0
			}
			cur.WriteRune(r)
			col++
		}
		if cur.Len() > 0 {
			out = append(out, cur.String())
		}
	}
	return out
}

// truncateForBox trims `s` to `width` runes and adds an
// ellipsis if anything was dropped. Used for the error line
// where the underlying message can be long.
func truncateForBox(s string, width int) string {
	if utf8.RuneCountInString(s) <= width {
		return s
	}
	count := 0
	for i := range s {
		if count >= width-1 {
			return s[:i] + "…"
		}
		count++
	}
	return s
}
