// chroma_inline.go -- cycle 233 chroma streaming syntax highlighting
// for fenced code blocks in the chat transcript.
//
// Why this exists. Cycle 232 wired the glamour renderer into the
// mid-stream TUI pass so each completed stream line lands in the
// transcript already styled (internal/mdrender/stream_glamour.go:
// LineANSI). Cycle 232's glamour handles prose -- headings, lists,
// bold/italic, blockquotes -- but glamour v2.0.1 renders ```go /
// ```python / ```ts fenced blocks as a 4-space-indented code fence
// with no per-token colour: every byte inside the fence reaches the
// terminal in the default foreground, so a 50-line Go function in
// the chat transcript looks like 50 lines of monochrome text. That is
// not what AGY 1.4.2 does: AGY 1.4.2 ships chroma inside its embedded
// renderer pipeline (recoverable as the chroma init function plus
// the five Aurora hex tokens via `strings ~/.local/bin/agy | grep
// chroma`; see also `cmd/wagyu/diff_syntax.go:42` for the diff-side
// chroma mapping) and applies it to fenced code blocks as part of
// the live-streaming glamour pass.
//
// The cycle restores that behaviour for the wagyu clone's chat
// transcript:
//
//   - A per-language chroma lexer cache keyed by language name
//     (chromaLexerCache) so 100 fenced blocks of the same
//     language pay the chroma lexer-construction cost once.
//   - HighlightCodeBlock(language, body) -- the public entry point
//     that runs chroma on a body and returns ANSI-coloured text.
//   - HighlightStreamingFences -- the buffer-level scanner the
//     mid-stream glamour pass calls: given the running transcript
//     buffer, find the most recently completed ```fenced``` block
//     and replace those rows in the buffer with chroma output.
//   - A graceful fallback when chroma cannot tokenise the input:
//     the raw source is re-emitted with a dim `!` warning prefix
//     so the user always sees a non-blank code block. This mirrors
//     the mermaid cycle-234 fallback contract (recover() + bordered
//     box); see mermaid_terminal.go:wrapMermaidFallback.
//
// Why not glamour v2's own fenced-block colouring: chroma ships
// its own renderer that handles fences (chroma.Highlight(w, src,
// lexer, "terminal16m", "monokai") is exactly what
// HighlightCodeBlock calls), but glamour does not call chroma on
// the fences it sees -- it treats ```go / ```text fences as opaque
// indented code blocks. The cycle's approach is to strip every
// language-tagged fence in the cycle-234 strip pass (so glamour
// never sees it), then chroma-render the body in the restore pass
// and re-insert the coloured text. Same shape as the cycle-234
// mermaid handler -- the placeholder + restore pattern already in
// glamour_preprocess_blocks.go is reused.
//
// Pin in: TestChromaInline_* (this file's _test.go).
package mdrender

import (
	"bytes"
	"fmt"
	"strings"
	"sync"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/quick"
	"github.com/alecthomas/chroma/v2/styles"
)

// chromaStyle is the chroma style used for streaming code-block
// highlighting. Cycle 233 picked monokai from a visual judgement;
// cycle 271 replaced it with solarized-light after recovering the
// AGY 1.4.2 token bytes from a PTY capture. The reference's Go
// fence renders `package` as ESC[1m ESC[38;5;166m (bold #d75f00,
// KeywordNamespace), `func` as ESC[38;5;100m (#878700, Keyword /
// KeywordDeclaration) and `main` as ESC[38;5;32m (#0087d7, Name),
// with punctuation falling back to the 242 base colour
// (solarized-light's Text #586e75 quantises to 242). That triple is
// exactly `chroma/styles solarized-light` rendered by the
// terminal256 formatter, and no other embedded style matches it.
//
// solarized-light is queried once at package init. A nil result
// (which would mean the embedded styles registry was stripped)
// falls back to chroma.Fallback (= swapoff), which produces the
// same shape of output without colour. Style.Get never errors at
// runtime in v2.24.1.
var (
	chromaMu    sync.RWMutex
	chromaStyle = func() *chroma.Style {
		if s := styles.Get("solarized-light"); s != nil {
			return s
		}
		return styles.Fallback
	}()
)

func setChromaScheme(scheme string) {
	chromaMu.Lock()
	defer chromaMu.Unlock()
	chromaStyleName := ChromaStyleForScheme(scheme)
	if s := styles.Get(chromaStyleName); s != nil {
		chromaStyle = s
	} else {
		chromaStyle = styles.Fallback
	}
}

// ChromaStyleForScheme maps an Antigravity color scheme name to its corresponding Chroma style.
func ChromaStyleForScheme(scheme string) string {
	switch strings.ToLower(strings.TrimSpace(scheme)) {
	case "terminal":
		return "swapoff"
	case "light":
		return "github"
	case "solarized light":
		return "solarized-light"
	case "colorblind-friendly light":
		return "solarized-light"
	case "dark":
		return "solarized-dark"
	case "solarized dark":
		return "solarized-dark"
	case "colorblind-friendly dark":
		return "solarized-dark"
	case "tokyo night":
		return "tokyonight-moon"
	default:
		return "solarized-light"
	}
}

// chromaLexerCache is the lexer cache keyed by language name.
// chroma's lexer constructor is non-trivial -- it walks the global
// registry, looks up alias / filename / mime globs, and constructs
// the regex-rule chain -- so we cache once per language. sync.Map is
// the right primitive here: per-line reads dominate writes (a fresh
// language is rare), and the cache is process-lifetime so there is
// no eviction concern.
//
// The cached value is the chroma.Coalesce-wrapped lexer; chroma's
// raw output is finer-grained than ours (e.g. Python's
// `"""docstring"""` is 4 chroma tokens, one Coalesce-wrapped
// LiteralStringDouble token) and Coalesce is the documented
// wrapper for the streaming use case.
//
// TestChromaInline_LexerCacheReused pins that this map has exactly
// 1 entry after 100 renders of the same language.
var chromaLexerCache sync.Map

// getChromaLexer returns the chroma lexer for lang, falling back
// to lexers.Fallback when the language is unknown. The returned
// lexer is Coalesce-wrapped; cache hits return the same wrapped
// pointer, so callers don't pay the Coalesce cost per render.
//
// An empty lang returns the fallback lexer too -- a bare ``` ```
// fence with no language tag is treated as plain text rather than
// raising an error.
func getChromaLexer(lang string) chroma.Lexer {
	if l, ok := chromaLexerCache.Load(lang); ok {
		return l.(chroma.Lexer)
	}
	var l chroma.Lexer
	if lang == "" {
		l = lexers.Fallback
	} else {
		if got := lexers.Get(lang); got != nil {
			l = got
		} else {
			l = lexers.Fallback
		}
	}
	l = chroma.Coalesce(l)
	actual, _ := chromaLexerCache.LoadOrStore(lang, l)
	return actual.(chroma.Lexer)
}

// HighlightCodeBlock runs chroma on body for the named language
// and returns the ANSI-coloured text. Width is 0 for "no word-wrap"
// -- chroma's terminal256 formatter does not word-wrap by default,
// so the rendered block is the same width as the input. Indentation
// is the caller's responsibility (HighlightCodeBlock emits raw
// chroma output; the public HighlightStreamingFences helper applies
// the 2-space indent that matches glamour's code-block style).
//
// Cycle 271: the formatter is terminal256, not terminal16m. The
// AGY 1.4.2 capture carries 256-colour indices (ESC[38;5;166m),
// not 24-bit RGB, so terminal256 is the byte-faithful choice; it
// also means the colourprofile degradation note above no longer
// applies to this path (chroma quantises from the style's own
// palette).
//
// The language argument is chroma's lexer alias; "go", "python",
// "typescript", "tsx", "javascript", "jsx", "rust", "ruby", "java",
// "kotlin", "swift", "c", "cpp", "csharp", "bash", "sh", "json",
// "yaml", "toml", "xml", "html", "css", "scss", "sql", "markdown",
// "dockerfile", "makefile", "terraform" all resolve to a registered
// lexer in chroma v2.24.1; see TestChromaInline_LanguagesMinimumCount
// for the capability-fence assertion. An unknown language falls
// back to the plaintext lexer (chroma.Fallback) -- we deliberately
// do NOT raise an error, so a model that emits ```mylang ... ```
// still produces a coloured-but-not-tokenised block rather than an
// empty transcript row.
//
// Errors fall back to the raw body with a `!` warning prefix
// emitted in dim yellow; the function never returns an empty
// string. Callers that need the error can read it from the second
// return value.
func HighlightCodeBlock(language, body string) (string, error) {
	if body == "" {
		return "", nil
	}
	lex := getChromaLexer(language)
	if lex == nil {
		return chromaFallbackBlock(language, body, fmt.Errorf("no chroma lexer")), nil
	}
	var buf bytes.Buffer
	chromaMu.RLock()
	styleName := chromaStyle.Name
	chromaMu.RUnlock()
	err := quick.Highlight(&buf, body, lex.Config().Name, "terminal256", styleName)
	if err != nil {
		return chromaFallbackBlock(language, body, err), err
	}
	return buf.String(), nil
}

// chromaFallbackBlock is the "render failed -- here is the raw
// source" payload used when chroma raises a non-recoverable error.
// The format:
//
//	\x1b[33m! chroma: <message>\x1b[0m
//	<raw source indented by 4 spaces, one line per source line>
//
// The user always sees a non-blank block; the `!` prefix marks it
// as a fallback so a user reading the transcript knows chroma
// didn't engage. Mirrors the cycle-234 mermaid
// wrapMermaidFallback contract (bordered source box on parse
// failure); the cycle-233 chroma path is single-pass on the
// streaming lane and a bordered box would be over-engineered.
func chromaFallbackBlock(language, body string, err error) string {
	var b strings.Builder
	b.WriteString("\x1b[33m! chroma")
	if language != "" {
		b.WriteString("[")
		b.WriteString(language)
		b.WriteString("]")
	}
	if err != nil {
		fmt.Fprintf(&b, ": %v", err)
	}
	b.WriteString("\x1b[0m\n")
	for _, line := range strings.Split(body, "\n") {
		b.WriteString("    ")
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

// indentChromaBlock applies a fixed-width indent to every line of
// the chroma output, so the coloured block aligns with the rest of
// glamour's code-block style (the cycle-228 4-space indent;
// glamour_adapter.go:332 buildANSIConfig.CodeBlock.BlockPrefix =
// "    "). An empty chroma output stays empty; a single-line chroma
// output is prefixed once.
//
// The function is intentionally simple -- chroma's terminal16m
// formatter emits no leading blank line, and the trailing newline
// is preserved so the caller's split('\n') works the same as for
// glamour's emit.
func indentChromaBlock(s, indent string) string {
	if s == "" || indent == "" {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if line == "" {
			continue
		}
		lines[i] = indent + line
	}
	return strings.Join(lines, "\n")
}

// HighlightStreamingFences scans rows[replyStart:] for the most
// recently closed ```fenced``` block and, if a complete block is
// found, runs chroma on the body and replaces those rows in-place
// in a returned copy. The function is the cycle-233 mid-stream
// chroma pass; the cycle-232 mid-stream glamour pass continues to
// run alongside, and the two are independent code paths.
//
// The "most recently completed" rule matches the cycle 232
// per-line glamour pass: only completed lines get re-styled, so an
// unclosed ```fence is left as raw markdown until the closing
// fence arrives. The function walks back from the end of the buffer
// looking for a ` ``` ` (with optional language) closing fence that
// has a matching opening fence further back.
//
// Returns the rows with any chroma-coloured blocks swapped in. If
// no complete fence is found, returns a copy of rows unchanged.
// The function never panics; chroma errors are caught at the
// HighlightCodeBlock level and the fallback path returns the raw
// source -- which is exactly what we want as the mid-stream fallback.
//
// replyStart is the index of the first row of the assistant turn
// (i.e. m.replyStart). Rows before that index are unchanged (and
// the returned slice mirrors the input).
func HighlightStreamingFences(rows []string, replyStart int) []string {
	if len(rows) == 0 || replyStart < 0 || replyStart >= len(rows) {
		return append([]string(nil), rows...)
	}
	// Walk back from the end looking for a closing fence.
	end := len(rows)
	// Find the most recent closing fence. The row may have a
	// language tag (e.g. "```go") -- we accept both bare and tagged
	// closers.
	closerIdx := -1
	for i := end - 1; i >= replyStart; i-- {
		if isClosingFenceRow(rows[i]) {
			closerIdx = i
			break
		}
	}
	if closerIdx <= replyStart {
		// No closer, or the closer is the very first row (no body).
		return append([]string(nil), rows...)
	}
	// Walk back from closerIdx-1 looking for the matching opening
	// fence. The opening fence line is the row whose trimmed form
	// starts with "```" and is NOT a closer (no non-whitespace
	// after the backticks). We accept the closest opener before
	// the closer.
	openerIdx := -1
	lang := ""
	for i := closerIdx - 1; i >= replyStart; i-- {
		l, ok := parseFenceRow(rows[i])
		if !ok {
			continue
		}
		// An opener: trim starts with "```" and the rest is either
		// empty (bare ``` opener) or a language tag.
		if l != "" {
			lang = l
		}
		openerIdx = i
		break
	}
	if openerIdx < 0 {
		return append([]string(nil), rows...)
	}
	// Body is rows[openerIdx+1 : closerIdx]. Body language was
	// captured above (or empty for bare ``` fence).
	body := strings.Join(rows[openerIdx+1:closerIdx], "\n")
	rendered, err := HighlightCodeBlock(lang, body)
	if err != nil || rendered == "" {
		// Fallback path: HighlightCodeBlock already returned the
		// raw source wrapped in a `!` banner. Keep that as the
		// emitted block.
		rendered, _ = HighlightCodeBlock(lang, body)
	}
	// Split the rendered block into rows so each row fits in one
	// transcript cell. Indent each row by 2 spaces to match
	// glamour's CodeBlock.Margin (cycle 271; was 4 spaces).
	renderedRows := strings.Split(indentChromaBlock(rendered, "  "), "\n")
	// Replace rows[openerIdx : closerIdx+1] with renderedRows.
	// The opening fence and closing fence rows are gone -- chroma
	// output doesn't include them, and glamour would have
	// stripped them anyway at turn-end.
	out := make([]string, 0, len(rows)+len(renderedRows)-(closerIdx-openerIdx+1))
	out = append(out, rows[:openerIdx]...)
	out = append(out, renderedRows...)
	if closerIdx+1 < len(rows) {
		out = append(out, rows[closerIdx+1:]...)
	}
	return out
}

// isClosingFenceRow reports whether row is a closing fence line:
// trimmed form starts with three or more backticks and has no
// non-whitespace content after. The opening row's language tag is
// discarded (a row like "```go" matches parseFenceRow but not
// isClosingFenceRow because "go" is non-whitespace after the
// backticks).
func isClosingFenceRow(row string) bool {
	trim := strings.TrimLeft(row, " \t")
	if !strings.HasPrefix(trim, "```") {
		return false
	}
	rest := trim[3:]
	// Strip additional backticks (a six-backtick fence closes a
	// three-backtick fence -- CommonMark 4.5).
	rest = strings.TrimLeft(rest, "`")
	if strings.TrimSpace(rest) != "" {
		return false
	}
	return true
}

// parseFenceRow parses a row that starts with "```" and returns
// the language tag (or "" for a bare opener). Returns ("", false)
// when the row is not a fence line at all. The opening row's
// language tag is captured; a closing fence (with no info after
// the backticks) returns ("", true).
func parseFenceRow(row string) (lang string, ok bool) {
	trim := strings.TrimLeft(row, " \t")
	if !strings.HasPrefix(trim, "```") {
		return "", false
	}
	rest := strings.TrimLeft(trim[3:], "`")
	rest = strings.TrimRight(rest, " \t\r")
	return rest, true
}
