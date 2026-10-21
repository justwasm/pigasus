// glamour_adapter.go — charm.land/glamour/v2 v2.0.1 wrappers for the
// hand-rolled internal/mdrender package.
//
// Why glamour (cycle: AGY 1.4.2 markdown rendering alignment). The
// recovery notes from out/local-macos/strings-goldmark.txt show the
// reference embeds github.com/yuin/goldmark; the 1.4.2 release note
// is explicit:
//
//	"Improved markdown rendering by upgrading glamour to v2.0.1 for
//	 cleaner headings and block padding."
//
// The binary confirms 152 glamour pclntab symbols including
// google3/third_party/golang/charm_land/glamour/v/v2/... runtime
// symbols. The hand-rolled mdrender walker produces visibly
// different output (cycle 175 visual diffs against the rebuilt
// transcript). This file is the four public renderers the clone's
// callers consume, all built on top of glamour:
//
//	ANSI  — chat-transcript rendering (cmd/wagyu/conversation.go).
//	        ANSI bold / italic / link styling; what the cycle-155
//	        hand-rolled pipeline produced, now via glamour.
//	Plain — Transcript passthrough / file-link rendering / --print
//	        path. Same structure as ANSI but no colour, with the
//	        cycle 175 plain-mode "**bold**" → "*bold*" marker
//	        preservation (the legacy walker emits literal "*bold*"
//	        when colour mode is off; glamour would drop the markers
//	        entirely).
//	User — Render-as-typed for render_user_text.go. Preserves soft
//	        line breaks so a user typing two lines sees two lines
//	        (crush's WithPreservedNewLines).
//	Quiet — OAuth device-flow prompt. No colour, no styling —
//	        equivalent to internal/cli/login_prompt.go's plain
//	        mdrender.Render call.
//
// The four variants are memoized per (variant, width) — a glamour
// TermRenderer carries goldmark BlockStack state across Render and
// is NOT safe for concurrent calls; the per-renderer mutex pattern
// is borrowed from charmbracelet/crush's common.MarkdownRenderer.
//
// Fallback: any glamour error falls through to the legacy
// mdrender.Render / RenderANSI so the user always sees something
// rendered instead of an empty transcript (cycle 1.4.2 "fallback to
// raw text with warning banner" spirit). Pin in:
//
//	TestGlamour_ANSIFallbackOnParserError  and
//	TestGlamour_*_ErrorContract tests below.
package mdrender

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"charm.land/glamour/v2"
	"charm.land/glamour/v2/ansi"
	"golang.org/x/term"
)

// osc8Re matches an OSC 8 hyperlink open/close token. glamour emits
// these for every link; the AGY 1.4.2 capture contains none (the URL
// is printed as underlined 36 text after the label instead), so the
// ANSI transcript strips them to stay byte-aligned and to keep row
// height math from seeing zero-width control strings. Explicit
// clickable surfaces (elig_inline.go) still build OSC 8 through
// internal/render.Hyperlink.
var osc8Re = regexp.MustCompile(`\x1b\]8;[^\x1b\x07]*(?:\x07|\x1b\\)`)

// mdVariant is the discriminator for the four flavours of renderer
// this package exposes. Each variant has its own ansi.StyleConfig
// (and, for User, its own WithPreservedNewLines flag), so caches
// are keyed per-variant per-width — same shape as crush's
// common.MarkdownRenderer (lines 28-34 of that file).
type mdVariant int

const (
	variantANSI mdVariant = iota
	variantPlain
	variantUser
	variantQuiet
)

// variantCount is the number of distinct renderer flavours. Caches
// are sized once at init so the lookup table never grows.
const variantCount = 4

// mdCacheMu guards mdCache (the four per-variant renderer maps).
// The same lock that crush holds — concurrent tests can race for a
// newly-minted TermRenderer, but once mint finishes the underlying
// renderer's per-instance mutex serialises Render calls.
//
// Lock ordering: when InvalidateCache needs both mdCacheMu and the
// underlying cache slots, acquire mdCacheMu first. Right now
// InvalidateCache only touches mdCache, so the rule is documented
// for future maintainers.
var mdCacheMu sync.Mutex

// mdCache is the per-variant, per-width glamour TermRenderer pool.
// Nil entries are filled lazily on first Render for that key, and
// invalidated wholesale by InvalidateCache. The index is
// [variant][width] but because width is not known at init time, we
// use map[int]*glamour.TermRenderer and pay the hash cost — width
// has at most a few hundred distinct values across the TUI's life
// (one per terminal width per user resize).
var mdCache [variantCount]map[int]*glamour.TermRenderer

func init() {
	for i := range mdCache {
		mdCache[i] = map[int]*glamour.TermRenderer{}
	}
}

// InvalidateCache drops every cached renderer. Call after a style
// change so subsequent renders mint TermRenderers with the new
// ansi.StyleConfig. Held under mdCacheMu so the reset is atomic
// from concurrent Render calls.
//
// In production this is a no-op (the styles do not change at
// runtime); the export exists for tests that flip styles between
// sub-tests.
func InvalidateCache() {
	mdCacheMu.Lock()
	defer mdCacheMu.Unlock()
	for i := range mdCache {
		mdCache[i] = map[int]*glamour.TermRenderer{}
	}
}

var (
	currentColorSchemeMu sync.RWMutex
	currentColorScheme   = "solarized light"
)

// SetColorScheme configures the active color scheme for markdown and code rendering.
// It invalidates cached renderers and updates the chroma code-block syntax style.
func SetColorScheme(scheme string) {
	currentColorSchemeMu.Lock()
	currentColorScheme = scheme
	currentColorSchemeMu.Unlock()
	setChromaScheme(scheme)
	InvalidateCache()
}

// CurrentColorScheme returns the currently active color scheme name.
func CurrentColorScheme() string {
	currentColorSchemeMu.RLock()
	defer currentColorSchemeMu.RUnlock()
	return currentColorScheme
}

// getRenderer returns the renderer for (variant, width), minting it
// on first call and caching the result. The returned renderer is
// shared across callers; per-renderer concurrency is serialised by
// the caller via the locker returned by lockRenderer.
//
// Width handling: cycle-268 made the explicit-width path the
// hot one — callers thread m.width / detected terminal width
// through ANSI / Plain / User / Quiet — but the `<= 0 → 80`
// safety fallback stays in place so a stray call site that
// forgets to plumb a width still produces something readable
// (glamour's hard default) rather than a width=0 panic from
// WithWordWrap. TestGlamour_WidthPin_FallbackToEighty pins the
// safety-net behaviour.
func getRenderer(v mdVariant, width int) (*glamour.TermRenderer, error) {
	widthVal := width
	if widthVal <= 0 {
		widthVal = 80 // safety net — glamour default
	}
	mdCacheMu.Lock()
	if r, ok := mdCache[v][widthVal]; ok {
		mdCacheMu.Unlock()
		return r, nil
	}
	mdCacheMu.Unlock()

	cfg := styleConfigFor(v)
	opts := []glamour.TermRendererOption{
		glamour.WithStyles(cfg),
		glamour.WithWordWrap(widthVal),
	}
	if v == variantANSI || v == variantUser {
		opts = append(opts, glamour.WithPreservedNewLines())
	}
	r, err := glamour.NewTermRenderer(opts...)
	if err != nil {
		return nil, fmt.Errorf("mdrender: glamour NewTermRenderer: %w", err)
	}

	mdCacheMu.Lock()
	mdCache[v][widthVal] = r
	mdCacheMu.Unlock()
	return r, nil
}

// rendererLocksMu guards rendererLocks — the per-renderer mutex
// table crush uses (common.LockMarkdownRenderer). One
// [*sync.Mutex] per TermRenderer pointer.
var (
	rendererLocksMu sync.Mutex
	rendererLocks   = map[*glamour.TermRenderer]*sync.Mutex{}
)

// lockRenderer returns the per-renderer mutex serialising concurrent
// Render calls on a shared TermRenderer. Tests parallelise over the
// four variants; the production TUI Update loop is single-threaded
// but the lock is harmless there.
func lockRenderer(r *glamour.TermRenderer) *sync.Mutex {
	rendererLocksMu.Lock()
	defer rendererLocksMu.Unlock()
	if mu, ok := rendererLocks[r]; ok {
		return mu
	}
	mu := &sync.Mutex{}
	rendererLocks[r] = mu
	return mu
}

// styleConfigFor builds the ansi.StyleConfig for variant v.
//
// Style decisions documented inline. The blocks mimic crush's
// internal/ui/styles/quickstyle.go (lines 231-339) plus the cycle
// 175 / 1.3.3 file-link contract:
//
//   - headings keep cycle-175 "# …" prefix in plain mode
//   - blockquote "> " per line (cycle 175 contract from the legacy
//     walker)
//   - "•" U+2022 unordered bullet (cycle 175)
//   - "1." ordered-list marker (cycle 175)
//   - 4-space code indent (cycle 175)
//   - "*bold*" / "_italic_" marker rewrites for plain variants
func styleConfigFor(v mdVariant) ansi.StyleConfig {
	switch v {
	case variantANSI:
		return buildANSIConfig()
	case variantPlain:
		return buildPlainConfig()
	case variantUser:
		return buildUserConfig()
	case variantQuiet:
		return buildQuietConfig()
	}
	return buildANSIConfig()
}

// boolPtr is a tiny helper for glamour's *bool fields. The pointer
// is what glamour checks (nil = "default"); we can't pass literal
// false because glamour's cascade treats `nil` differently from
// `&false`.
func boolPtr(b bool) *bool { return &b }

// uintPtr is the *uint counterpart. ansi.StyleBlock.Indent is *uint
// (glamour zero = no indent). We need a pointer to disambiguate
// "no indent" (nil) from "indent 0" (impossible).
func uintPtr(u uint) *uint { return &u }

// strPtr is the *string counterpart. ansi uses *string for Color /
// BackgroundColor. nil = "no colour override"; &"" = "explicit
// empty" (glamour's cascade treats both the same in practice).
func strPtr(s string) *string { return &s }

// buildLegacyShapeConfig is the cycle-175 marker-preserving shape:
// "# " headings, "> " blockquotes, the U+2421 bullet, the "---"
// horizontal rule and a 4-space code fence. It is the base for the
// Plain / Quiet variants, whose consumers (the --print non-TTY path
// and the OAuth login prompt) pin those marker bytes. The ANSI
// transcript palette lives in buildANSIConfig below; ANSI no longer
// derives from this shape because the AGY 1.4.2 glamour style is
// structurally different (2-space document margin, box-drawing rule,
// "•" bullet, "│ " quote).
func buildLegacyShapeConfig() ansi.StyleConfig {
	return ansi.StyleConfig{
		Document: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				// No leading/trailing blank — cycle 175 walker
				// does not, and the post-pass collapses blanks
				// anyway.
			},
		},
		BlockQuote: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color: strPtr("240"), // dim gray
			},
			Indent:      uintPtr(1),
			IndentToken: strPtr("> "), // cycle 175 contract
		},
		List: ansi.StyleList{
			LevelIndent: 4, // cycle 175 visual
		},
		Heading: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				BlockSuffix: "\n", // cycle 175
				Bold:        boolPtr(true),
				Color:       strPtr("39"), // bright blue, brand-blue family
			},
		},
		H1: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "# ", // cycle 175 plaintext parity
				Bold:   boolPtr(true),
			},
		},
		H2: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "## ",
				Bold:   boolPtr(true),
			},
		},
		H3: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "### ",
				Bold:   boolPtr(true),
			},
		},
		H4: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "#### ",
				Bold:   boolPtr(true),
			},
		},
		H5: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "##### ",
				Bold:   boolPtr(true),
			},
		},
		H6: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "###### ",
				Bold:   boolPtr(true),
			},
		},
		Strikethrough: ansi.StylePrimitive{
			CrossedOut: boolPtr(true),
		},
		Emph: ansi.StylePrimitive{
			Italic: boolPtr(true),
		},
		Strong: ansi.StylePrimitive{
			Bold: boolPtr(true),
		},
		HorizontalRule: ansi.StylePrimitive{
			Format: "\n---\n", // cycle 175 contract
		},
		Item: ansi.StylePrimitive{
			BlockPrefix: "\u2421 ", // cycle 175 — bullet
		},
		Enumeration: ansi.StylePrimitive{
			BlockPrefix: ". ", // cycle 175 — "1." stays; glamour adds "1"
		},
		Task: ansi.StyleTask{
			Ticked:   "[✓] ",
			Unticked: "[ ] ",
		},
		Link: ansi.StylePrimitive{
			Color:     strPtr("39"),
			Underline: boolPtr(false),
			// BlockPrefix: "" and BlockSuffix: "" — no OSC 8 by
			// default; the cycle's render.Hyperlink wraps externally
			// when clickable links are wanted.
		},
		LinkText: ansi.StylePrimitive{
			Color: strPtr("135"), // magenta-ish, distinguishes from URLs
		},
		Code: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				BlockPrefix: "`",
				BlockSuffix: "`",
				Color:       strPtr("220"), // gold on default bg
			},
		},
		CodeBlock: ansi.StyleCodeBlock{
			StyleBlock: ansi.StyleBlock{
				StylePrimitive: ansi.StylePrimitive{
					// 4-space indent — cycle 175 walker contract.
					BlockPrefix: "    ",
				},
			},
		},
	}
}

// buildPlainConfig is the plain-text variant. Same structure as
// ANSI, but with no Colour / Bold / Italics set so output reaches
// a file or pipe unstyled.
//
// The "*bold*" / "_italic_" rewrite is handled by PostProcessPlain
// (below): glamour's Strong / Emph primitives default to "no
// markers", so a plain-mode render of "**bold**" would emit literally
// "bold" (with no `*` chars). The post-pass restores the cycle 175
// marker convention that the clone's transcript passthrough and
// login-prompt consumers expect.
func buildPlainConfig() ansi.StyleConfig {
	cfg := buildLegacyShapeConfig()
	// Drop every Colour and the Strong / Emph / Bold attrs that
	// would otherwise produce ANSI in plain mode.
	cfg.Document.StylePrimitive.Color = nil
	cfg.Heading.StylePrimitive.Color = nil
	cfg.Heading.StylePrimitive.Bold = nil
	cfg.Link.Color = nil
	cfg.LinkText.Color = nil
	cfg.Code.StylePrimitive.Color = nil
	cfg.BlockQuote.StylePrimitive.Color = nil
	cfg.H1.StylePrimitive.Bold = nil
	cfg.H2.StylePrimitive.Bold = nil
	cfg.H3.StylePrimitive.Bold = nil
	cfg.H4.StylePrimitive.Bold = nil
	cfg.H5.StylePrimitive.Bold = nil
	cfg.H6.StylePrimitive.Bold = nil
	cfg.Emph.Italic = nil
	cfg.Strong.Bold = nil
	return cfg
}

// buildANSIConfig is the chat-transcript palette, recovered byte-for-
// byte from the AGY 1.4.2 `--conversation` PTY capture (cycle 271).
// The reference is glamour's LightStyleConfig with four deliberate
// overrides, each pinned by a captured escape sequence:
//
//   - Document.Color #073642 (Light's base02) — the reference renders
//     body prose, table cells and code-block indents in Solarized
//     base02. The reference emits the 24-bit form on a truecolour
//     terminal (\x1b[38;2;7;54;66m) and lets the terminal quantise it;
//     the old "23" literal produced 38;5;23 unconditionally, which
//     diverged from the reference under COLORTERM=truecolor.
//   - Document.Margin 2 — every rendered row is indented two cells
//     and the reply block spans (terminal width − 3) cells.
//   - HorizontalRule.Format "──────" (Light's "\n--------\n") — the
//     reference emits six box-drawing horizontals coloured 249. The
//     format carries no surrounding newlines: glamour's block
//     renderer already emits them, and adding them produced a blank
//     row above and below the rule that the reference does not have.
//   - Code 32-on-254 with no prefix/suffix (Light's 203-on-254 with
//     a non-breaking-space pad) — the reference wraps inline code in
//     a blue 32 / grey 254 chip with no surrounding spaces.
//
// Everything else (H1 " 228 on 63" chip, Heading 27 bold, H2–H6 "## "
// prefixes, "• " bullets, "│ " quotes, "36"/"29" links, CodeBlock
// margin 2 + base 242) is LightStyleConfig verbatim. Tagged fenced
// code blocks are chroma-solarized (chroma_inline.go), not glamour's
// built-in chroma, because the reference's Go token colours match
// solarized-light exactly (166/100/32/242).
func buildANSIConfig() ansi.StyleConfig {
	scheme := CurrentColorScheme()
	docColor := "#073642"
	headColor := "27"
	h1Bg := "63"
	switch strings.ToLower(strings.TrimSpace(scheme)) {
	case "dark", "solarized dark", "colorblind-friendly dark":
		docColor = "#839496"
		headColor = "33"
		h1Bg = "24"
	case "tokyo night":
		docColor = "#c8d3f5"
		headColor = "75"
		h1Bg = "60"
	case "terminal":
		docColor = ""
		headColor = "33"
		h1Bg = "24"
	default:
		docColor = "#073642"
		headColor = "27"
		h1Bg = "63"
	}

	var docColorPtr *string
	if docColor != "" {
		docColorPtr = strPtr(docColor)
	}

	return ansi.StyleConfig{
		Document: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color: docColorPtr,
			},
			Margin: uintPtr(2),
		},
		BlockQuote: ansi.StyleBlock{
			Indent:      uintPtr(1),
			IndentToken: strPtr("│ "),
		},
		List: ansi.StyleList{
			LevelIndent: 4,
		},
		Heading: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				BlockSuffix: "\n",
				Color:       strPtr(headColor),
				Bold:        boolPtr(true),
			},
		},
		H1: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix:          " ",
				Suffix:          " ",
				Color:           strPtr("228"),
				BackgroundColor: strPtr(h1Bg),
				Bold:            boolPtr(true),
			},
		},
		H2: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "## ",
			},
		},
		H3: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "### ",
			},
		},
		H4: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "#### ",
			},
		},
		H5: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "##### ",
			},
		},
		H6: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "###### ",
				Bold:   boolPtr(false),
			},
		},
		Strikethrough: ansi.StylePrimitive{
			CrossedOut: boolPtr(true),
		},
		Emph: ansi.StylePrimitive{
			Italic: boolPtr(true),
		},
		Strong: ansi.StylePrimitive{
			Bold: boolPtr(true),
		},
		HorizontalRule: ansi.StylePrimitive{
			Color:  strPtr("249"),
			Format: "──────",
		},
		Item: ansi.StylePrimitive{
			BlockPrefix: "• ",
		},
		Enumeration: ansi.StylePrimitive{
			BlockPrefix: ". ",
		},
		Task: ansi.StyleTask{
			Ticked:   "[✓] ",
			Unticked: "[ ] ",
		},
		Link: ansi.StylePrimitive{
			Color:     strPtr("36"),
			Underline: boolPtr(true),
		},
		LinkText: ansi.StylePrimitive{
			Color: strPtr("29"),
			Bold:  boolPtr(true),
		},
		Code: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color:           strPtr("32"),
				BackgroundColor: strPtr("254"),
			},
		},
		CodeBlock: ansi.StyleCodeBlock{
			StyleBlock: ansi.StyleBlock{
				StylePrimitive: ansi.StylePrimitive{
					Color: strPtr("242"),
				},
				Margin: uintPtr(2),
			},
		},
	}
}

// buildUserConfig is the shape used for user-typed markdown echoes
// (/learn, /goal). It deliberately keeps the cycle-175 markers
// ("# ", "> ", "•" via U+2421) and the no-margin layout: cycle 174
// pins that plain user text round-trips byte-identically through
// renderUserText, and the reply palette's 2-space document margin +
// 38;5;23 body colour would break that contract. getRenderer adds
// WithPreservedNewLines for this variant, so a multi-line prompt
// keeps its line breaks.
func buildUserConfig() ansi.StyleConfig {
	return buildLegacyShapeConfig()
}

// buildQuietConfig is for the OAuth login prompt. No colour, no
// styling, no horizontal-rule format. Just enough structure for the
// heading to read as a heading and the fenced code to render as a
// 4-space-indented code block.
func buildQuietConfig() ansi.StyleConfig {
	cfg := buildPlainConfig()
	cfg.Heading.StylePrimitive.BlockSuffix = "\n"
	cfg.HorizontalRule.Format = "\n--------\n"
	return cfg
}

// coalesceSGR collapses glamour's per-run "reset then re-apply the same
// style" idiom. glamour's ANSI writer wraps every text run (and every
// padding space) in its own escape pair, so a single coloured paragraph
// reaches us as
//
//	ESC[38;5;23mfirstESC[m ESC[38;5;23m lineESC[m ...
//
// The reference binary emits the same visual result as one run per line
// (ESC[38;5;23mfirst lineESC[39m). Coalescing is not cosmetic: the
// interrupt-free text is what makes RestoreMermaidAndMath's
// strings.Contains(key) and the transcript tests' substring assertions
// work, and it shrinks a 120-col reply from ~60 KB to a few hundred
// bytes.
//
// The pass is conservative. It only drops a reset plus the escape(s)
// that immediately follow it when those escapes re-apply exactly the
// style that was active before the reset. Anything that changes the
// active attributes is emitted verbatim, so a bold run followed by a
// non-bold run still carries the intermediate reset.
func coalesceSGR(s string) string {
	if !strings.Contains(s, "\x1b[") {
		return s
	}
	var out strings.Builder
	out.Grow(len(s))
	cur := ""            // escape sequences active since the last reset
	prev := ""           // cur captured at the last reset
	pendingReset := false
	flushReset := func() {
		if pendingReset {
			out.WriteString("\x1b[m")
			pendingReset = false
			cur = ""
		}
	}
	i := 0
	for i < len(s) {
		if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && (s[j] == ';' || (s[j] >= '0' && s[j] <= '9')) {
				j++
			}
			if j < len(s) && s[j] == 'm' {
				params := s[i+2 : j]
				seq := s[i : j+1]
				if params == "" || params == "0" {
					// Defer the reset: the next escape may
					// re-establish the identical style, in
					// which case both are redundant.
					prev = cur
					cur = ""
					pendingReset = true
					i = j + 1
					continue
				}
				cur += seq
				if pendingReset && cur == prev {
					// reset + re-apply of the same style: drop both.
					pendingReset = false
					cur = prev
					i = j + 1
					continue
				}
				if pendingReset {
					out.WriteString("\x1b[m")
					pendingReset = false
				}
				out.WriteString(seq)
				i = j + 1
				continue
			}
		}
		flushReset()
		out.WriteByte(s[i])
		i++
	}
	flushReset()
	return out.String()
}

// renderWithFallthrough is the implementation shared by ANSI /
// Plain / User / Quiet. It mints / retrieves the renderer, locks
// the per-renderer mutex around the single Render call (so parallel
// tests do not corrupt goldmark's BlockStack), and falls back to
// the legacy walker on any glamour error.
//
// width <= 0 means "use glamour's default (80)". The caller-side
// wrappers (ANSI/Plain/...) that already enforce width > 0 are
// unchanged.
func renderWithFallthrough(v mdVariant, src string, width int) (string, error) {
	if src == "" {
		return "", nil
	}
	r, err := getRenderer(v, width)
	if err != nil {
		// Mint failed — surface as a wrapped glamour error so the
		// caller can fall back.
		return "", err
	}
	mu := lockRenderer(r)
	mu.Lock()
	out, gErr := r.Render(src)
	mu.Unlock()
	if gErr != nil {
		// Cycle 1.4.2 fallback to legacy walker. The walker is
		// goldmark-based and never errors on valid markdown, so
		// on the rare glamour parser failure we keep the user
		// from seeing an empty transcript row.
		return "", fmt.Errorf("mdrender: glamour: %w", gErr)
	}
	if v == variantANSI || v == variantUser {
		out = osc8Re.ReplaceAllString(out, "")
		out = coalesceSGR(out)
	}
	if v == variantPlain || v == variantQuiet || v == variantUser {
		out = postProcessPlain(out)
	} else {
		// Cycle 269: ANSI variant also trims the lone trailing
		// newline glamour appends so a reply with N content
		// lines renders to N rows, not N+1. Pre-cycle-269 the
		// trailing newline was kept, so splitting
		// "Para\n\nPara\n" produced [Para, blank, Para, ""]
		// — an extra empty row at the end of the transcript.
		// Streaming via LineANSI was unaffected because
		// LineANSI's own TrimRight newline
		// (stream_glamour.go:190) hid the asymmetry; the bug
		// only showed up at final render.
		out = strings.TrimRight(out, "\n")
	}
	return out, nil
}

// currentVariant returns the glamour variant the cycle-232 /
// cycle-243 mid-stream glamour pass renders against. Cycle 232
// baked in variantANSI (the chat-transcript palette); cycle 243
// exposes this as a function so the prefix cache's key field
// (prefixCacheKey.Variant) can be populated without leaking
// mdVariant out of the package.
//
// Future cycles that introduce a width / variant parameter on
// LineANSIBounded will change this helper; the cache key
// automatically picks up the new value via the function call.
func currentVariant() string {
	return "ansi"
}

// currentWidth returns the terminal width the cycle-232 /
// cycle-243 mid-stream glamour pass uses, and the width the package
// applies to every Render call that does NOT receive an explicit
// width argument.
//
// Resolution order (cycle 268):
//
//   1. The value last set via SetCurrentWidth. The TUI's
//      tea.WindowSizeMsg handler calls SetCurrentWidth(m.width)
//      once per terminal resize so the cycle-242 / cycle-243
//      mid-stream glamour pass picks up the actual terminal
//      width — fixing the cycle-232 / cycle-243 bug that every
//      LineANSI call rendered at width=0 (glamour's hard default
//      of 80 columns) regardless of how wide the terminal
//      actually was.
//   2. The $COLUMNS environment variable, parsed as a positive
//      integer. Print mode (`-p`) does not have a bubbletea
//      WindowSizeMsg, so the production wiring reads $COLUMNS —
//      a value most shells forward automatically.
//   3. The width of os.Stdout, when connected to a TTY, read via
//      golang.org/x/term's GetSize. Falls back silently to 0
//      when Stdout is not a TTY (a piped consumer cannot ask the
//      kernel for column geometry).
//   4. 0 — caller must decide how to handle; getRenderer's
//      `<= 0 → 80` safety fallback keeps the renderer off the
//      glamour default's wrong-without-context half (gloss word
//      wrap at 80 is "good enough", but not what the user asked
//      for).
//
// Width <= 0 means "let glamour auto-fit", which is what
// the cycle-232 / cycle-243 wrapper did originally.
//
// Pin in: TestGlamour_WidthPin_CurrentWidth_* in
// cycle268_width_test.go.
func currentWidth() int {
	// 1. The TUI's pinned value. Read first because the TUI is
	// the consumer that cares most (see deliverable §"decision").
	if pinned := readPinnedWidth(); pinned > 0 {
		return pinned
	}
	// 2. $COLUMNS — print mode's natural source.
	if cols := os.Getenv("COLUMNS"); cols != "" {
		if n, err := strconv.Atoi(cols); err == nil && n > 0 {
			return n
		}
	}
	// 3. x/term.GetSize on os.Stdout when connected to a TTY.
	if term.IsTerminal(int(os.Stdout.Fd())) {
		if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
			return w
		}
	}
	// 4. Caller's problem.
	return 0
}

// pinnedWidthMu guards pinnedWidth. SetCurrentWidth is called from
// bubbletea's WindowSizeMsg handler in the TUI (rare; once per
// terminal resize), so the lock contention is negligible. The lock
// exists so a concurrent CurrentWidth / SetCurrentWidth pair from
// `go test -parallel` does not race.
var pinnedWidthMu sync.RWMutex

// pinnedWidth is the value last passed to SetCurrentWidth. 0 means
// "unset — fall through to $COLUMNS / x/term". Stored as a
// separate variable rather than as an extra return value on
// currentVariant so the resolution order stays self-documenting
// and the in-package callers (currentWidth + the cycle-242 /
// cycle-243 mid-stream cache key) read without changing a
// signature.
var pinnedWidth int = 0

// readPinnedWidth atomically reads the package-pinned width. The
// helper exists so currentWidth's resolution order is easy to
// follow; everything funnels through here.
func readPinnedWidth() int {
	pinnedWidthMu.RLock()
	defer pinnedWidthMu.RUnlock()
	return pinnedWidth
}

// CurrentWidth returns the package's current view of the terminal
// width, resolved via the same chain currentWidth uses. The
// helper is exported so callers that want to "fall back to
// mdrender's detection" can do so without re-implementing the
// env/x/term logic (cmd/wagyu/print_glamour.go's renderPrintText
// is the primary caller — print mode has no TUI state to read).
//
// Returns 0 when no width source is available.
func CurrentWidth() int {
	return currentWidth()
}

// SetCurrentWidth pins the package-wide terminal width to width.
// The TUI's tea.WindowSizeMsg handler wires this in so every
// downstream glamour render (ANSI / Plain / User / Quiet /
// LineANSI / LineANSIBounded / LineANSIBoundedWithCache) renders
// at the actual terminal width rather than glamour's hard
// default. Setting width <= 0 clears the pin — subsequent
// callers fall back to the $COLUMNS / x/term chain.
//
// Safety: SetCurrentWidth is a write to a single int guarded by a
// RWMutex. The expensive glamour TermRenderer reuse is controlled
// by the per-(variant, width) cache in getRenderer, so each new
// width mints a fresh TermRenderer on first use — see
// TestGlamour_WidthPin_CacheKeyDifferentWidths for the pinning
// the cycle's regression test exercises.
//
// Concurrency: safe to call from any goroutine. The TUI calls it
// from the bubbletea Update loop (single-threaded); production
// callers do not need their own synchronisation.
func SetCurrentWidth(width int) {
	pinnedWidthMu.Lock()
	defer pinnedWidthMu.Unlock()
	if width <= 0 {
		pinnedWidth = 0
	} else {
		pinnedWidth = width
	}
}

// ANSI renders src as Markdown with colour, suitable for the chat
// transcript and the step-detail panel. Width is the terminal
// width; <= 0 falls back to glamour's default (80).
//
// On glamour errors, returns (legacy walker output, nil). The
// caller never sees an error, but the cycle's regression test
// pins the contract:
//
//	TestGlamour_ANSIFallbackOnParserError
func ANSI(src string, width int) (string, error) {
	out, err := renderWithFallthrough(variantANSI, src, width)
	if err != nil {
		// Fallback. Use the legacy ANSI variant so the fallback
		// keeps the cycle 175 styling (cBoldOpen / etc.) that the
		// transcript consumer expects.
		buf := newPooledBuffer()
		if wErr := RenderANSI(src, buf); wErr != nil {
			return src, nil // absolute last resort — pass through.
		}
		return buf.String(), nil
	}
	return out, nil
}

// Plain renders src as Markdown without colour, suitable for piped
// transcripts, file-link rendering, and the --print path. Same
// fallback contract as ANSI.
func Plain(src string, width int) (string, error) {
	out, err := renderWithFallthrough(variantPlain, src, width)
	if err != nil {
		fb, rErr := Render(src)
		if rErr != nil {
			return src, nil
		}
		return fb, nil
	}
	return out, nil
}

// User renders src as Markdown for user-typed echoes
// (cmd/wagyu/render_user_text.go). Same as ANSI but glamour is
// configured with WithPreservedNewLines so a lone "\n" in the
// source renders as a real line break rather than being collapsed
// by CommonMark's paragraph rule.
//
// Issue charmbracelet/crush#3502 records the rationale.
func User(src string, width int) (string, error) {
	out, err := renderWithFallthrough(variantUser, src, width)
	if err != nil {
		buf := newPooledBuffer()
		if wErr := RenderANSI(src, buf); wErr != nil {
			return src, nil
		}
		return buf.String(), nil
	}
	return out, nil
}

// Quiet renders src as Markdown without colour or styling, suitable
// for the OAuth device-flow prompt (internal/cli/login_prompt.go)
// where the output may be redirected to a file or pipe and must
// remain readable either way. "Quiet" is crush's
// common.QuietMarkdownRenderer concept.
func Quiet(src string, width int) (string, error) {
	out, err := renderWithFallthrough(variantQuiet, src, width)
	if err != nil {
		fb, rErr := Render(src)
		if rErr != nil {
			return src, nil
		}
		return fb, nil
	}
	return out, nil
}
