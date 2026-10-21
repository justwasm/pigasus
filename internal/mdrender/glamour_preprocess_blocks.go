// glamour_preprocess_blocks.go — fenced-block pre-processor for the
// glamour adapter.
//
// Glamour v2.0.1 does not know how to render ```` ```mermaid ```` or
// ```` ```math ```` blocks; goldmark parses them as ordinary fenced
// code, and glamour emits them with the same 4-space indent as
// every other code block, dropping the structural cue the model's
// payload carried.
//
// The cycle's preservation strategy:
//
//   - Strip every ```mermaid / ```math / ```latex / ```tex / ```katex
//     fenced block out of the source, replace it with a unique
//     placeholder token, and store the metadata in a struct map.
//   - Run glamour on the placeheld source.
//   - Sweep back through the rendered text and replace each
//     placeholder with the rendered content (or the bordered
//     fallback) for the matching kind.
//
// Cycle 234 changed the placeholder map from `map[string]string`
// (just the pre-rendered body) to `map[string]BlockPlaceholder`
// (carries the kind, the raw source, and parsed frontmatter),
// so the Restore pass can route each placeholder to the right
// renderer and degrade gracefully on parse failure:
//
//   - mermaid → RenderMermaidBlock (mermaid_terminal.go, which
//     calls AlexanderGrooff/mermaid-ascii 1.6.1 and falls back to
//     a bordered source box on error)
//   - math/latex/tex/katex → expandLaTeX (followed by 4-space
//     indent) — unchanged from cycle 232.
//
// Anything else is left as text in the source (glamour handles
// ordinary ```go / ```text fences with the standard 4-space
// indent; the post-pass leaves them alone).
//
// Idempotency:
//   - Strip must be called once (it produces a fresh map).
//   - Restore can be called multiple times — once a placeholder is
//     gone, the second pass has nothing to do.
//   - Blocks whose language is unknown (e.g. ```go) are passed
//     through verbatim into the source; glamour sees them, indents
//     them, and the post-pass leaves them alone.
package mdrender

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/alecthomas/chroma/v2/lexers"
)

// blockKind tags the renderer we use for a stripped block. The
// naming tracks the legacy FencedCodeBlock switch:
//
//	mermaid → RenderMermaidBlock / wrapMermaidFallback
//	math/latex/tex/katex → expandLaTeX (followed by 4-space indent)
//
// Anything else is left as text in the source (glamour will see
// the original fence and render it as a code block).
//
// Cycle 234 promotes blockKind from an internal enum to the
// BlockKind field on BlockPlaceholder so the Restore pass can
// dispatch per placeholder without re-parsing the source.
type blockKind int

const (
	blockUnknown blockKind = iota
	blockMermaid
	blockMath
	// blockCode (cycle 233): a fenced code block whose language
	// tag is recognised by the chroma lexer registry. Strip +
	// Restore passes the body through chroma.Highlight, producing
	// per-token ANSI styling; the language-agnostic
	// `` ```text `` / `` ``` `` case stays in the cycle-228
	// "leave to glamour" path.
	blockCode
)

// String returns the stable wire name for a block kind. The
// glamour renderer doesn't care about this, but tests that log
// a placeholder map do — keep it short and stable.
func (k blockKind) String() string {
	switch k {
	case blockMermaid:
		return "mermaid"
	case blockMath:
		return "math"
	case blockCode:
		return "code"
	default:
		return "unknown"
	}
}

// BlockPlaceholder is the metadata cycle-234's glamour adapter
// keeps for each fenced block it strips out of the source. The
// Restore pass consults Kind to choose a renderer, Raw as the
// "render failed" fallback input, and Frontmatter to surface
// diagram titles and audit what the model wrote.
//
// Source and Body are kept for backwards-compat with the
// pre-234 callers — Body is the pre-rendered text the cycle-228
// MermaidRenderBlock / expandLaTeX pipeline emitted, which
// callers that haven't migrated can still consume.
//
// Cycle 233 added the Language field; the chroma-aware Restore
// pass uses it to look up the lexer that will colour the body.
// Pre-cycle 233 code reads the value as a struct and ignores the
// empty Language case; see TestGlamour_StripMermaidAndMath_SwapsOut
// for the migration pattern.
type BlockPlaceholder struct {
	Kind        blockKind
	Raw         string
	Source      string
	Body        string
	Frontmatter map[string]any
	// Language is the bare info string from the opening fence
	// (`` ```go `` → "go"), or "" for a bare ``` fence. Used by
	// the cycle-233 chroma Restore pass to look up the lexer;
	// empty for mermaid / math blocks.
	Language string
}

// blockPlaceholderPrefix is the marker a stripped block leaves
// behind. Glamour's default StyleConfig has BlockPrefix/BlockSuffix
// of empty strings for paragraphs, so the placeholder survives
// verbatim through glamour's render. The numeric suffix is what
// StripMermaidAndMath uses to disambiguate between multiple
// stripped blocks in one source — keys are still unique after
// goroutine races across StripMermaidAndMath, but a numeric
// suffix is friendlier in panic traces.
const blockPlaceholderPrefix = "MDRENDER_BLOCK_PLACEHOLDER_"

// fencedRe matches a fenced code block — opening fence + language
// + body + closing fence — and reports the language string in
// match[1]. The body group is permissive (any character; the
// closing fence is greedy-matched by the closing-fence rule
// rather than by a fixed pattern).
//
// We deliberately do NOT enforce a minimum fence length, so an
// opening ``` closes the block at the next ``` rather than the
// longest fence, matching CommonMark's "the closing fence is the
// same length or longer" rule for the most common case of
// exactly-three.
var fencedRe = regexp.MustCompile("(?m)^```([a-zA-Z0-9_-]*)\n((?:[^\n]*\n)*?)```[ \t]*$")

// ensureFenceClosed appends a synthetic closing fence when src ends with an
// unterminated fenced code block. It exists so the clone matches the
// reference renderer on partial input: AGY chromas an *unclosed* fence
// (verified by resuming AGY on a stored reply whose last block is an open
// ```go fence — every token gets its solarized colour), whereas
// fencedRe requires a closing fence and the block otherwise falls through
// to glamour's monochrome 4-space-indented code path. Appending the closer
// lets the strip pass pull the partial body into a blockCode placeholder so
// RestoreMermaidAndMath chromas it. Complete input has an even fence count
// and is returned unchanged, so this is a no-op for every turn-end render.
//
// Only lines that start with ``` count (fencedRe anchors the opener at the
// line start). A line that merely contains ``` mid-line is code content and
// does not affect parity.
func ensureFenceClosed(src string) string {
	n := 0
	for _, line := range strings.Split(src, "\n") {
		if strings.HasPrefix(line, "```") {
			n++
		}
	}
	if n%2 == 0 {
		return src
	}
	if strings.HasSuffix(src, "\n") {
		return src + "```"
	}
	return src + "\n```"
}

// StripMermaidAndMath walks src, removes every ```mermaid, ```math,
// ```latex, ```tex, ```katex block, and returns:
//
//   - cleaned: src with each block replaced by a unique
//     placeholder line of the form
//     "MDRENDER_BLOCK_PLACEHOLDER_<n>".
//   - placeholders: a map from the placeholder marker to a
//     BlockPlaceholder carrying the kind, raw body, parsed
//     frontmatter (mermaid only) and the pre-rendered body for
//     callers that haven't moved to the cycle-234
//     RenderMermaidBlock path.
//
// Cycle 234 changed the value type from string to
// BlockPlaceholder so the Restore pass can route each
// placeholder by kind. The mermaid branch frontmatter-parses
// the body up-front and stores the parsed YAML on the
// placeholder — that way Restore doesn't have to re-parse.
//
// Non-mermaid/non-math code fences (e.g. ```go) are not stripped —
// glamour handles them with the standard 4-space indent, and the
// post-pass / RestoreMermaidAndMath leaves the placeholder-free
// output alone.
//
// Cycle 233: the default branch now ALSO catches language-tagged
// code fences whose language chroma recognises ("go", "python",
// "typescript", "tsx", "javascript", "jsx", "rust", "ruby", "java",
// "kotlin", "swift", "c", "cpp", "csharp", "bash", "sh", "json",
// "yaml", "toml", "xml", "html", "css", "scss", "sql", "markdown",
// "dockerfile", "makefile", "terraform", ... — the full chroma
// v2.24.1 catalogue via lexers.Get). The body is routed through
// chroma.Highlight in the Restore pass, producing per-token ANSI
// colouring. Languages chroma does not know (e.g. "```mylang")
// fall through the default branch and reach glamour as ordinary
// 4-space-indented code blocks; see TestChromaInline_UnknownLanguage
// for the fallback shape.
//
// Pin in: TestGlamour_StripAndRestore_Mermaid,
// TestGlamour_StripAndRestore_Math, and the cycle-233 chroma tests
// in chroma_inline_test.go.
func StripMermaidAndMath(src string) (cleaned string, placeholders map[string]BlockPlaceholder) {
	placeholders = map[string]BlockPlaceholder{}
	if src == "" {
		return src, placeholders
	}
	// Cycle 272: close an unterminated fence so partial input (the live
	// streaming buffer) chromas like the reference renderer does; see
	// ensureFenceClosed.
	src = ensureFenceClosed(src)
	out := src
	type sub struct {
		start, end int
		ph         BlockPlaceholder
	}
	var subs []sub
	for _, m := range fencedRe.FindAllStringSubmatchIndex(src, -1) {
		lang := strings.ToLower(src[m[2]:m[3]])
		body := src[m[4]:m[5]]
		var ph BlockPlaceholder
		switch lang {
		case "mermaid":
			yamlBody, cleanedSrc, hasFM := MermaidFrontmatter(body)
			var fm map[string]any
			if hasFM {
				parsed, err := ParseMermaidFrontmatter(yamlBody)
				if err == nil {
					fm = parsed
				}
			}
			ph = BlockPlaceholder{
				Kind:        blockMermaid,
				Raw:         body,
				Source:      cleanedSrc,
				Body:        MermaidRenderBlock(MermaidBlock{Source: cleanedSrc}),
				Frontmatter: fm,
			}
		case "math", "latex", "tex", "katex":
			ph = BlockPlaceholder{
				Kind:   blockMath,
				Raw:    body,
				Source: body,
				Body:   "    " + expandLaTeX(strings.TrimSpace(body)),
			}
		default:
			// Cycle 233: any other non-empty language tag that
			// chroma knows about is a chroma-targeted code block.
			// The lang field stays on the placeholder so the
			// Restore pass can look up the lexer; Source is the
			// raw body (no frontmatter / cleaning — code fences
			// don't carry YAML metadata).
			if lang != "" && lexers.Get(lang) != nil {
				ph = BlockPlaceholder{
					Kind:     blockCode,
					Raw:      body,
					Source:   body,
					Language: lang,
				}
				// Body is intentionally left empty here — the
				// Restore pass will set it via HighlightCodeBlock
				// once chroma has been queried. Leaving Body empty
				// is the documented "rendered during Restore"
				// marker.
			} else {
				continue
			}
		}
		subs = append(subs, sub{m[0], m[1], ph})
	}
	// Apply substitutions back-to-front so earlier indices stay
	// valid. The placeholder body in the cleaned source stays
	// empty; Restore fills it in.
	for i := len(subs) - 1; i >= 0; i-- {
		s := subs[i]
		key := blockPlaceholderPrefix + strconv.Itoa(i)
		placeholders[key] = s.ph
		out = out[:s.start] + key + "\n" + out[s.end:]
	}
	return out, placeholders
}

// RestoreMermaidAndMath takes a glamour-rendered string and the
// placeholders map from StripMermaidAndMath, and replaces every
// placeholder key with its rendered body — mermaid via
// RenderMermaidBlock (terminal ASCII, bordered fallback on parse
// failure), math via expandLaTeX. Substitution uses
// strings.ReplaceAll so a placeholder that no longer survives the
// glamour render (e.g. glamour decided to swallow the line) is
// silently dropped — restoring a key that is absent is a no-op.
//
// The returned string is the rendered transcript with the
// mermaid / math blocks restored in glamour's place. Indentation
// applied by glamour around the placeholder is preserved; the
// restored body replaces the placeholder verbatim.
//
// Cycle 234: the mermaid branch calls RenderMermaidBlock, which
// picks terminal ASCII when the library accepts the input and
// emits a bordered source box when it does not (parse error,
// panic, timeout). The math branch keeps the cycle-228
// expandLaTeX behaviour verbatim.
//
// Cycle 233: a new blockCode branch routes the body through
// HighlightCodeBlock (chroma_inline.go), indenting every emitted
// line by 2 spaces so the chroma output aligns with glamour's
// code-block margin. On chroma error the helper already
// emits a `!` warning banner with the raw source, so Restore
// doesn't need a separate fallback branch.
// visiblePrefix returns the leading whitespace of s with SGR sequences
// dropped, but only when the visible remainder is whitespace. Glamour
// writes the document margin as bare spaces *before* the paragraph's
// colour run, so the placeholder line is "  \x1b[38;5;23mKEY…" and the
// margin is "  ". Anything else (a list marker, a quote bar) is not a
// margin and yields "".
func visiblePrefix(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && s[j] != 'm' {
				j++
			}
			if j < len(s) {
				i = j + 1
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	v := b.String()
	if v == "" || strings.Trim(v, " \t") != "" {
		return ""
	}
	return v
}

func RestoreMermaidAndMath(rendered string, placeholders map[string]BlockPlaceholder) string {
	if rendered == "" || len(placeholders) == 0 {
		return rendered
	}
	for key, ph := range placeholders {
		if !strings.Contains(rendered, key) {
			continue
		}
		var body string
		switch ph.Kind {
		case blockMermaid:
			mb := MermaidBlock{
				Source:      ph.Source,
				Raw:         ph.Raw,
				Frontmatter: ph.Frontmatter,
			}
			out, _ := RenderMermaidBlock(mb, 0 /* width streamed */)
			if out == "" {
				out = ph.Body // backstop: cycle-228 four-space indent
			}
			body = out
		case blockMath:
			body = ph.Body
		case blockCode:
			// Cycle 233: chroma the body and indent each line
			// by 2 spaces (cycle 271; glamour's CodeBlock.Margin).
			// HighlightCodeBlock returns raw source with a `!`
			// banner on error, so the chat pipeline never
			// produces a blank block.
			out, _ := HighlightCodeBlock(ph.Language, ph.Source)
			body = indentChromaBlock(out, "  ")
		default:
			// Should not happen — Strip must drop unknown
			// kinds. If a placeholder with an unknown kind
			// shows up it's a programmer error, not a render
			// failure, so leave Body in place.
			body = ph.Body
		}
		// A block renderer returns its lines with a trailing newline.
		// The placeholder paragraph already supplies the line break,
		// so keeping the body's final newline would add a blank row
		// between the block and whatever follows (the reference has
		// exactly one blank at a code→table or code→paragraph
		// boundary, from the next block's prefix).
		body = strings.TrimRight(body, "\n")
		// The placeholder was rendered as its own paragraph, so its
		// first line already carries the document margin (two spaces
		// for the ANSI variant). The replacement's later lines do not,
		// because the margin is applied per rendered paragraph, not
		// inside the text we splice in. Reuse the placeholder line's
		// leading whitespace as the prefix for every line after the
		// first so a code block's rows stay aligned at
		// (document margin + chroma indent).
		if idx := strings.Index(rendered, key); idx >= 0 {
			lineStart := strings.LastIndex(rendered[:idx], "\n") + 1
			prefix := visiblePrefix(rendered[lineStart:idx])
			if prefix != "" {
				// Prefix only non-empty lines: a blank line
				// inside a code block stays empty, and the
				// body's trailing newline is a terminator,
				// not an indented line.
				lines := strings.Split(body, "\n")
				for i := 1; i < len(lines); i++ {
					if lines[i] != "" {
						lines[i] = prefix + lines[i]
					}
				}
				body = strings.Join(lines, "\n")
			}
		}
		rendered = strings.ReplaceAll(rendered, key, body)
	}
	return rendered
}
