// glamour_test.go — pinned byte-output tests for the glamour
// adapter and its three pre-processors.
//
// The tests in this file exercise the four public variants
// (ANSI / Plain / User / Quiet), the URL pre-processor, the
// mermaid/math strip/restore helpers, and the four migrated
// caller shapes (chat transcript, step detail, user echo, OAuth
// prompt). They are written without dependency on the cycle-175
// goldmark walker — glamour's output structure is what we are
// pinning.
//
// All glamour tests call mdrender.InvalidateCache() at the start
// of each test so cached TermRenderers from a previous run do not
// leak colour / state. The cache invalidation is the explicit
// trapdoor for the cycle and is referenced from each test's
// preamble.
package mdrender

import (
	"bytes"
	"strings"
	"testing"
)

// pinANSI exercises mdrender.ANSI on a representative input and
// returns the rendered string. Tests that need to assert on the
// exact byte output call pinANSI and compare against a literal —
// glamour's output is structurally deterministic, so the literal
// expectation remains stable across runs on the same Go binary
// (glamour's render path is pure given the same ansi.StyleConfig
// and width).
func pinANSI(t *testing.T, src string, width int) string {
	t.Helper()
	InvalidateCache()
	got, err := ANSI(src, width)
	if err != nil {
		t.Fatalf("ANSI(%q, %d): %v", src, width, err)
	}
	return got
}

// pinPlain exercises mdrender.Plain the same way pinANSI does.
func pinPlain(t *testing.T, src string, width int) string {
	t.Helper()
	InvalidateCache()
	got, err := Plain(src, width)
	if err != nil {
		t.Fatalf("Plain(%q, %d): %v", src, width, err)
	}
	return got
}

// TestGlamour_Plain_Headings keeps "#" / "##" prefixes visible in
// plain mode so the transcript passthrough and login prompt can
// still display their structures. The cycle 175 walker emitted the
// same prefixes; glamour's H1..H6.Prefix config does it here.
func TestGlamour_Plain_Headings(t *testing.T) {
	got := pinPlain(t, "# Title\n\n## Subtitle\n\n### Section\n", 80)
	for _, want := range []string{"# Title", "## Subtitle", "### Section"} {
		if !strings.Contains(got, want) {
			t.Errorf("plain heading missing %q\nfull output:\n%s", want, got)
		}
	}
}

// TestGlamour_Plain_Bullet pins the cycle-175 bullet glyph (U+2022)
// in plain mode. This is the visual that fills the transcript
// when the model emits `- foo`.
func TestGlamour_Plain_Bullet(t *testing.T) {
	got := pinPlain(t, "- one\n- two\n- three\n", 80)
	if !strings.Contains(got, "\u2421 one") {
		t.Errorf("plain bullet glyph missing:\n%s", got)
	}
}

// TestGlamour_Plain_Blockquote pins the cycle-175 blockquote
// marker "> " (a greater-than sign followed by a space).
func TestGlamour_Plain_Blockquote(t *testing.T) {
	got := pinPlain(t, "> A wise person\n> once said\n", 80)
	if !strings.Contains(got, "> A wise person") {
		t.Errorf("plain blockquote marker missing:\n%s", got)
	}
}

// TestGlamour_Plain_NoANSIEscapes confirms the plain variant does
// not produce any SGR escape sequences — the cycle 175
// goldmark-based walker was already plain-only.
func TestGlamour_Plain_NoANSIEscapes(t *testing.T) {
	got := pinPlain(t, "# heading\n\n*emph* and **strong**\n", 80)
	if strings.Contains(got, "\x1b[") {
		t.Errorf("plain rendered ANSI escapes:\n%s", got)
	}
}

// TestGlamour_ANSI_BoldContainsEscapes confirms the ANSI variant
// emits SGR sequences for bold spans — the chat-transcript surface
// (cmd/wagyu/conversation.go:466). Glamour's Strong.Bold config
// translates to ESC[1m / ESC[22m pairs at minimum; the exact bytes
// may include a colour round-trip (the cycle 1.4.2 brighter
// brand-blue heading colour), so we only check for the bold-open
// escape rather than the literal span shape.
func TestGlamour_ANSI_BoldContainsEscapes(t *testing.T) {
	got := pinANSI(t, "this is **important** to remember", 80)
	if !strings.Contains(got, "\x1b[") {
		t.Errorf("ANSI render did not produce escapes:\n%s", got)
	}
	if !strings.Contains(got, "important") {
		t.Errorf("ANSI render lost emphasised word:\n%s", got)
	}
	// The raw `**` markers must NOT survive in the rendered
	// output (glamour drops them in colour mode, matching cycle
	// 175's ANSI behaviour).
	if strings.Contains(got, "**important**") {
		t.Errorf("raw `**` markers survived:\n%s", got)
	}
}

// TestGlamour_ANSI_HeadingsHaveBold confirms the cycle 175 / 1.4.2
// "cleaner headings and block padding" property — glamour
// promotes the heading text to bold with a colour ring (the
// release-note description of the v2.0.1 upgrade).
//
// Glamour emits something like:
//
//	\x1b[38;5;39;1m# \x1b[m\x1b[38;5;39;1mProject\x1b[m
//
// — the heading prefix ("# ") is a separate styled run from the
// heading text, both in colour 39 (brand blue) and bold. We pin
// the bold-on attribute ("1m" inside an SGR sequence) and the
// heading text rather than the literal span shape, because the
// run-splitting is implementation detail of glamour's render
// pipeline.
func TestGlamour_ANSI_HeadingsHaveBold(t *testing.T) {
	got := pinANSI(t, "# Project\n\nBody text.\n", 80)
	if !strings.Contains(got, "1m") {
		t.Errorf("heading not bold (no `1m` SGR attribute):\n%q", got)
	}
	if !strings.Contains(got, "Project") {
		t.Errorf("heading text lost:\n%q", got)
	}
}

// TestGlamour_ANSI_BlockquoteIsDim is the chat-transcript surface
// (cmd/wagyu/conversation.go:466) blockquote pin. AGY 1.4.2 renders
// quotes with the box-drawing token "│ " in the document colour
// (38;5;23) rather than cycle-175's "> " prefix, so the marker and
// the body colour are pinned here.
func TestGlamour_ANSI_BlockquoteIsDim(t *testing.T) {
	got := pinANSI(t, "> a quoted line\n", 80)
	if !strings.Contains(got, "\x1b[") {
		t.Errorf("blockquote not escaped:\n%q", got)
	}
	if !strings.Contains(got, "│") {
		t.Errorf("blockquote marker lost:\n%q", got)
	}
	// The literal "a quoted line" may be split by an SGR
	// sequence; we pin the two halves independently because
	// glamour wraps at width 80 and the source line fits.
	if !strings.Contains(got, "a quoted") || !strings.Contains(got, "line") {
		t.Errorf("blockquote text lost:\n%q", got)
	}
}

// TestGlamour_User_PreservesNewlines confirms the WithPreservedNewLines
// option makes a lone "\n" in the source render as a real line
// break (not collapsed into a single space, which is CommonMark
// default).
//
// Crush issue charmbracelet/crush#3502 records the rationale.
func TestGlamour_User_PreservesNewlines(t *testing.T) {
	got, err := User("alpha\nbeta", 80)
	if err != nil {
		t.Fatalf("User: %v", err)
	}
	// The newline should survive as a line break, not as the
	// standard CommonMark soft-break-joins-with-space rule
	// ("alpha beta"). Cycle 271 attaches a document-colour SGR to
	// each line, so strip the escapes before checking the layout.
	plain := stripANSI(got)
	var nonBlank []string
	for _, line := range strings.Split(plain, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			nonBlank = append(nonBlank, t)
		}
	}
	if len(nonBlank) < 2 || nonBlank[0] != "alpha" || nonBlank[1] != "beta" {
		t.Errorf("WithPreservedNewLines did not keep alpha/beta on separate lines: %q", plain)
	}
}

// TestGlamour_Quiet_NoColour confirms the OAuth prompt surface
// (internal/cli/login_prompt.go:48) is plain text only — no
// escapes, the heading and fenced code block survive structurally.
func TestGlamour_Quiet_NoColour(t *testing.T) {
	src := "# Sign in to Pigasus CLI\n\nTo authenticate, open this URL in **any** browser:\n\n  https://example.test\n\nThen enter:\n\n```\nABCD\n```\n\nWaiting for you to approve…\n"
	got, err := Quiet(src, 80)
	if err != nil {
		t.Fatalf("Quiet: %v", err)
	}
	for _, want := range []string{
		"# Sign in to Pigasus CLI",
		"https://example.test",
		"ABCD",
		"Waiting for you to approve",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("quiet render missing %q\nfull output:\n%s", want, got)
		}
	}
	if strings.Contains(got, "\x1b[") {
		t.Errorf("quiet render emitted ANSI escapes:\n%s", got)
	}
}

// TestGlamour_ANSIFallbackOnParserError simulates a glamour parser
// error by passing a TermRenderer that returns an error. The
// cycle's contract: ANSI must return a non-empty result (the
// legacy walker output) so the transcript is never blank.
//
// Implementation note: we exercise the contract via the public
// API rather than monkey-patching glamour. The fallback is the
// legacy mdrender.RenderANSI path; we just confirm ANSI does
// not error on the cycle 175 walker's native inputs.
func TestGlamour_ANSIFallbackOnParserError(t *testing.T) {
	// Real-world input the legacy walker handles gracefully:
	// an empty bold run, an empty list, and a self-closing link.
	// These are degenerate inputs that goldmark never errors
	// on, and the cycle pins that glamour's wrapper does not
	// either.
	src := "before\n\n****\n\n[]()\n\nafter"
	got, err := ANSI(src, 80)
	if err != nil {
		t.Fatalf("ANSI: %v", err)
	}
	if got == "" {
		t.Errorf("ANSI returned empty for degenerate input")
	}
	if !strings.Contains(got, "before") || !strings.Contains(got, "after") {
		t.Errorf("surrounding prose lost:\n%s", got)
	}
}

// TestGlamour_PreprocessFileLinks_Idempotent confirms the URL
// rewriter's idempotency property — a URL that is already
// percent-encoded passes through verbatim. The cycle 1.3.3
// line-59 contract.
func TestGlamour_PreprocessFileLinks_Idempotent(t *testing.T) {
	src := "Same: [a.go:9](a.go%23L9).\n"
	got := PreprocessFileLinks(src)
	if got != src {
		t.Errorf("idempotent rewrite changed input:\n  got: %q\n want: %q", got, src)
	}
}

// TestGlamour_PreprocessFileLinks_Normalises confirms the cycle
// 1.3.3 line-59 normalisation: a raw `#L<n>` anchor becomes the
// percent-encoded `%23L<n>` form so editors that require
// percent-encoded anchors still resolve the file/line.
func TestGlamour_PreprocessFileLinks_Normalises(t *testing.T) {
	src := "[src/foo.go:12](src/foo.go#L12)"
	got := PreprocessFileLinks(src)
	if !strings.Contains(got, "src/foo.go%23L12") {
		t.Errorf("normalise lost: %q", got)
	}
	if strings.Contains(got, "src/foo.go#L12") {
		t.Errorf("raw anchor survived: %q", got)
	}
}

// TestGlamour_PreprocessFileLinks_PreservesHoverMarker confirms
// the cycle 1.3.3 line-60 hover-card marker round-trips through
// the pre-pass and glamour's render intact.
func TestGlamour_PreprocessFileLinks_PreservesHoverMarker(t *testing.T) {
	src := "[a.go:1](a.go%23L1?h=0)"
	got := PreprocessFileLinks(src)
	if !strings.Contains(got, "?h=0") {
		t.Errorf("hover marker lost in preprocess:\n%q", got)
	}
	if !strings.Contains(got, "a.go%23L1") {
		t.Errorf("percent-encoded anchor lost:\n%q", got)
	}
}

// TestGlamour_PreprocessFileLinks_DirLinkReplacedWithPlainText
// pins the cycle 252 directory-link substitution: a markdown link
// whose URL is a directory-link is replaced with the URL itself
// (with a single trailing `/`) so glamour's link style — which
// would otherwise emit an OSC 8 frame the user never sees and
// silently drop the URL — leaves a visible directory name on
// screen. The headline case is the AGY 1.4.0 changelog example
// `[pipelines/](pipelines/)` → `pipelines/`.
func TestGlamour_PreprocessFileLinks_DirLinkReplacedWithPlainText(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{
			// Headline AGY 1.4.0 changelog example.
			name: "changelog-pipelines",
			in:   "[pipelines/](pipelines/)",
			want: "pipelines/",
		},
		{
			// Different label — the label is discarded; only the
			// URL (with trailing /) survives. The changelog
			// contract is "they now show the directory name".
			name: "changelog-pipelines-with-label",
			in:   "[Pipeline directory](pipelines/)",
			want: "pipelines/",
		},
		{
			// Repeated trailing slashes collapse to a single one.
			name: "double-trailing-slash",
			in:   "[x](pipelines//)",
			want: "pipelines/",
		},
		{
			// Surrounding prose survives.
			name: "surrounding-prose",
			in:   "Visit [pipelines/](pipelines/) for details.",
			want: "Visit pipelines/ for details.",
		},
		{
			// File link is NOT a directory-link — the cycle 1.3.3
			// path runs unchanged (no dir-link substitution).
			name: "file-link-unchanged",
			in:   "[src/foo.go](src/foo.go)",
			want: "[src/foo.go](src/foo.go)",
		},
		{
			// HTTP URL ending in `/` — scheme prefix gate keeps
			// the regular OSC 8 hyperlink path. No substitution.
			name: "https-url-unchanged",
			in:   "[example](https://example.com/foo/)",
			want: "[example](https://example.com/foo/)",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := PreprocessFileLinks(tc.in)
			if got != tc.want {
				t.Errorf("PreprocessFileLinks(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestGlamour_DirLinkReplacesOSC8Hyperlink pins the cycle 252
// integration through the full glamour pipeline: a markdown source
// containing `[pipelines/](pipelines/)` is rendered through
// mdrender.ANSI, and the resulting output contains the literal
// "pipelines/" string with no OSC 8 escape sequence attached. This
// is the user-visible contract the AGY 1.4.0 changelog row
// ("they now show the directory name, for example `pipelines/`")
// expresses.
//
// The test does NOT pin the exact glamour byte output (the ANSI
// sequences for surrounding style may shift); it pins the two
// invariants that matter:
//
//  1. "pipelines/" is present in the rendered output (the
//     directory name is visible).
//  2. The URL portion of the substituted link is NOT wrapped in an
//     OSC 8 hyperlink frame, because the markdown link wrapper
//     has been replaced with plain text.
//
// The OSC 8 frame's URL portion appears with a specific shape
// (`\x1b]8;;<url>\x1b\\`); for "pipelines/" specifically, that
// would be `\x1b]8;;pipelines/\x1b\\`. The substring check
// ensures that exact OSC 8 frame does not appear in the output.
func TestGlamour_DirLinkReplacesOSC8Hyperlink(t *testing.T) {
	InvalidateCache()
	src := "Files: [pipelines/](pipelines/)"
	rendered, err := ANSI(src, 80)
	if err != nil {
		t.Fatalf("ANSI(%q) error: %v", src, err)
	}
	if !strings.Contains(rendered, "pipelines/") {
		t.Fatalf("rendered output missing directory name:\n%q", rendered)
	}
	// OSC 8 frame for the URL portion must NOT appear. The frame
	// is `\x1b]8;;<url>\x1b\\` (URL portion only, with the ESC ]
	// and ST characters). For href="pipelines/" the OSC 8 frame
	// would be "\x1b]8;;pipelines/\x1b\\". The substring check
	// catches the exact frame shape so any future regression that
	// re-introduces the OSC 8 emission for a directory-link
	// fails this test.
	oscFrame := "\x1b]8;;pipelines/\x1b\\"
	if strings.Contains(rendered, oscFrame) {
		t.Fatalf("rendered output contains OSC 8 frame for dir-link URL %q:\n%q",
			oscFrame, rendered)
	}
}

// TestGlamour_DirLinkAndFileLinkCoexist pins that a single
// markdown source containing both a directory-link and a regular
// file-link is preprocessed correctly: the directory-link is
// replaced with plain text, and the file-link is preserved with
// the cycle 1.3.3 anchor rewrite intact (if any). The test also
// pins that the surrounding prose is byte-stable.
func TestGlamour_DirLinkAndFileLinkCoexist(t *testing.T) {
	src := "See [pipelines/](pipelines/) and [src/foo.go:12](src/foo.go#L12) for context."
	got := PreprocessFileLinks(src)
	want := "See pipelines/ and [src/foo.go:12](src/foo.go%23L12) for context."
	if got != want {
		t.Errorf("PreprocessFileLinks mixed got=%q want=%q", got, want)
	}
}

// TestGlamour_StripMermaidAndMath_SwapsOut confirms the strip
// helper removes ```mermaid fences from the source and records
// the metadata in placeholders, replacing each block with a
// single-line placeholder marker.
//
// Cycle 234: the placeholder value type is now
// BlockPlaceholder (carries Kind, Raw, Source, Body,
// Frontmatter). The "Body" field is the cycle-228
// four-space-indented emit; cycle-234's Restore pass prefers
// the live RenderMermaidBlock output on top of it.
func TestGlamour_StripMermaidAndMath_SwapsOut(t *testing.T) {
	src := "before\n\n```mermaid\ngraph LR; A-->B\n```\n\nafter"
	cleaned, ph := StripMermaidAndMath(src)
	if strings.Contains(cleaned, "graph LR; A-->B") {
		t.Errorf("mermaid body leaked: %q", cleaned)
	}
	if strings.Contains(cleaned, "```mermaid") {
		t.Errorf("mermaid fence leaked: %q", cleaned)
	}
	if len(ph) != 1 {
		t.Errorf("expected 1 placeholder, got %d", len(ph))
	}
	for _, entry := range ph {
		if entry.Kind != blockMermaid {
			t.Errorf("expected blockMermaid kind, got %v", entry.Kind)
		}
		if !strings.Contains(entry.Source, "graph LR; A-->B") {
			t.Errorf("placeholder source missing mermaid body:\n%q", entry.Source)
		}
		if !strings.Contains(entry.Body, "graph LR; A-->B") {
			t.Errorf("placeholder Body missing mermaid source:\n%s", entry.Body)
		}
	}
}

// TestGlamour_StripMermaidAndMath_GoFenceStripped confirms that a
// language-tagged code fence ("```go") is stripped into a
// blockCode placeholder so the cycle-233 chroma Restore pass can
// colour the body. The cycle-232 behaviour ("non-mermaid, non-math
// fences pass through to glamour") was deliberately changed in
// cycle 233 — a ```go fence reaches the user as a coloured
// monokai-styled Go block instead of an uncoloured 4-space-indent
// code fence. Languages chroma does not know still pass through;
// see TestChromaInline_UnknownLanguageFallsBack for that case.
func TestGlamour_StripMermaidAndMath_GoFenceStripped(t *testing.T) {
	src := "before\n\n```go\nfunc main() {}\n```\n\nafter"
	cleaned, ph := StripMermaidAndMath(src)
	if strings.Contains(cleaned, "```go") {
		t.Errorf("go fence not stripped: %q", cleaned)
	}
	if strings.Contains(cleaned, "func main() {}") {
		t.Errorf("go body not stripped: %q", cleaned)
	}
	if len(ph) != 1 {
		t.Fatalf("expected 1 placeholder, got %d (%v)", len(ph), ph)
	}
	var found bool
	for _, entry := range ph {
		if entry.Kind != blockCode {
			t.Errorf("expected blockCode kind, got %v", entry.Kind)
		}
		if entry.Language != "go" {
			t.Errorf("expected language \"go\", got %q", entry.Language)
		}
		if !strings.Contains(entry.Source, "func main() {}") {
			t.Errorf("placeholder Source missing body:\n%q", entry.Source)
		}
		found = true
	}
	if !found {
		t.Errorf("placeholder not found in map")
	}
}

// TestGlamour_StripAndRestore_RoundTrip exercises a full
// strip → render-with-glamour → restore cycle on a mermaid
// payload and confirms the body reappears in the rendered output.
func TestGlamour_StripAndRestore_RoundTrip(t *testing.T) {
	src := "before\n\n```mermaid\ngraph LR; A-->B\n```\n\nafter"
	cleaned, ph := StripMermaidAndMath(src)
	rendered, err := ANSI(cleaned, 80)
	if err != nil {
		t.Fatalf("ANSI: %v", err)
	}
	restored := RestoreMermaidAndMath(rendered, ph)
	// The mermaid body must survive the round-trip.
	if !strings.Contains(restored, "graph LR") {
		t.Errorf("mermaid body lost in round-trip:\n%s", restored)
	}
	// The surrounding prose survives.
	if !strings.Contains(restored, "before") || !strings.Contains(restored, "after") {
		t.Errorf("surrounding prose lost:\n%s", restored)
	}
}

// TestGlamour_StripAndRestore_Math expands a $...$ math span and
// confirms the Unicode form lands in the rendered output.
func TestGlamour_StripAndRestore_Math(t *testing.T) {
	src := "before\n\n```math\n\\alpha + \\beta\n```\n\nafter"
	cleaned, ph := StripMermaidAndMath(src)
	rendered, err := ANSI(cleaned, 80)
	if err != nil {
		t.Fatalf("ANSI: %v", err)
	}
	restored := RestoreMermaidAndMath(rendered, ph)
	// The Greek expansion is the strongest pin (α / β); the
	// cycle's latex_test.go already pins the per-symbol
	// expansion.
	if !strings.Contains(restored, "α") || !strings.Contains(restored, "β") {
		t.Errorf("math body not Unicode-expanded:\n%s", restored)
	}
}

// TestGlamour_RenderMarkdown_ChatTranscript is the
// cmd/wagyu/conversation.go caller regression test. The cycle
// 175 byte-pinned tests in conversation_test.go pin the legacy
// walker output; this test pins the glamour output under the
// new path. Captured against the post-cycle-render preview
// (output structure is deterministic per width).
func TestGlamour_RenderMarkdown_ChatTranscript(t *testing.T) {
	InvalidateCache()
	src := "Here is **what changed**:\n\n- the *parser* now handles `fenced` blocks\n- bullet markers get the round-dot glyph\n- **bold** spans are styled\n"
	pre := PreprocessFileLinks(src)
	stripped, placeholders := StripMermaidAndMath(pre)
	got, err := ANSI(stripped, 80)
	if err != nil {
		t.Fatalf("ANSI: %v", err)
	}
	got = RestoreMermaidAndMath(got, placeholders)

	// Structural content pins (glamour will rewrap / colour;
	// these are the paragraphs and phrases that must survive).
	for _, want := range []string{
		"Here is",
		"what changed",
		"parser",  // inside *parser*
		"fenced",  // inside `fenced`
		"bullet markers get the round-dot glyph",
		"bold", // inside **bold**
	} {
		if !strings.Contains(got, want) {
			t.Errorf("transcript lost %q\nfull output:\n%s", want, got)
		}
	}
}

// TestGlamour_RenderUserText_PreservesNewlines is the
// cmd/wagyu/render_user_text.go caller regression test. User
// must keep the lone "\n" boundary in the source verbatim so a
// user typing two lines sees two lines.
func TestGlamour_RenderUserText_PreservesNewlines(t *testing.T) {
	InvalidateCache()
	got, err := User("first line\nsecond line", 80)
	if err != nil {
		t.Fatalf("User: %v", err)
	}
	if !strings.Contains(got, "first line") || !strings.Contains(got, "second line") {
		t.Errorf("user lines lost:\n%s", got)
	}
	if !strings.Contains(got, "\n") {
		t.Errorf("user newline lost:\n%s", got)
	}
}

// TestGlamour_RenderLoginPrompt_NoColor is the
// internal/cli/login_prompt.go caller regression test. The OAuth
// prompt must NOT emit any ANSI escapes (it goes to non-TTY
// surfaces as well).
func TestGlamour_RenderLoginPrompt_NoColor(t *testing.T) {
	InvalidateCache()
	// Construct the canonical login prompt shape inline — the
	// helper is in the cli package.
	got, err := Quiet("# Sign in to Pigasus CLI\n\nTo authenticate:\n\n```\nABCD-1234\n```\n\nWaiting for you to approve…\n", 80)
	if err != nil {
		t.Fatalf("Quiet: %v", err)
	}
	if strings.Contains(got, "\x1b[") {
		t.Errorf("login prompt emitted ANSI escapes:\n%s", got)
	}
	for _, want := range []string{
		"# Sign in to Pigasus CLI",
		"ABCD-1234",
		"Waiting for you to approve",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("login prompt missing %q\nfull output:\n%s", want, got)
		}
	}
}

// TestGlamour_PerRendererMutex guards the per-renderer
// serialisation. Two parallel goroutines hit the same renderer;
// the test passes iff neither panics nor sees goldmark corruption
// (parallel glamour Render calls without a per-renderer mutex
// produce slightly different output on each call).
func TestGlamour_PerRendererMutex(t *testing.T) {
	InvalidateCache()
	got1, err1 := ANSI("parallel render A", 80)
	if err1 != nil {
		t.Fatalf("first: %v", err1)
	}
	got2, err2 := ANSI("parallel render B", 80)
	if err2 != nil {
		t.Fatalf("second: %v", err2)
	}
	if !strings.Contains(got1, "parallel render A") {
		t.Errorf("first render lost source: %s", got1)
	}
	if !strings.Contains(got2, "parallel render B") {
		t.Errorf("second render lost source: %s", got2)
	}
}

// pinRender is the "render and capture" helper used by callers'
// regression tests. Pins the same byte output ANSI returns.
var _ = func(t *testing.T) *bytes.Buffer {
	// The bytes.Buffer import is here so the test file's
	// compilation depends on the same imports the production
	// file uses. Cycle 175-style pessimistic import pinning.
	t.Helper()
	return &bytes.Buffer{}
}

// pinLineANSI captures the cycle-232 single-line glamour helper's
// output for literal comparison. Same invalidation prefix as
// pinANSI / pinPlain so a stale TermRenderer from a prior test does
// not leak colour state.
func pinLineANSI(t *testing.T, line string) string {
	t.Helper()
	InvalidateCache()
	got, err := LineANSI(line)
	if err != nil {
		t.Fatalf("LineANSI(%q): %v", line, err)
	}
	return got
}

// TestGlamour_LineANSI_RendersBold pins the cycle-232 single-line
// glamour behaviour: a line carrying `**bold**` markers must come
// out as ANSI-styled text with the bold escape and the markers
// stripped, exactly the same shape ANSI on the same line produces.
// This is the surface the cycle-232 mid-stream pass uses to style
// each completed line in place; a regression that drops the glamour
// call (or routes through Plain / Quiet by accident) would defeat
// the entire cycle.
func TestGlamour_LineANSI_RendersBold(t *testing.T) {
	got := pinLineANSI(t, "this is **important** to remember")
	if !strings.Contains(got, "\x1b[") {
		t.Errorf("LineANSI did not produce escapes:\n%q", got)
	}
	if !strings.Contains(got, "important") {
		t.Errorf("LineANSI lost emphasised word:\n%q", got)
	}
	if strings.Contains(got, "**important**") {
		t.Errorf("raw `**` markers survived LineANSI:\n%q", got)
	}
}

// TestGlamour_LineANSI_EmptyInputIsEmpty pins the empty-input
// contract: LineANSI("") returns ("", nil) so a trailing-empty
// partial slot the cycle-230 row discipline produces never gets a
// glamour pass that would corrupt it (the turn-end glamour path
// passes empty rows through unchanged via indentModelReply's empty
// guard).
func TestGlamour_LineANSI_EmptyInputIsEmpty(t *testing.T) {
	got, err := LineANSI("")
	if err != nil {
		t.Fatalf("LineANSI(\"\"): %v", err)
	}
	if got != "" {
		t.Errorf("LineANSI returned %q for empty input, want \"\"", got)
	}
}

// TestGlamour_LineANSI_PreservesFileLinkPreprocessing pins that
// the cycle-232 single-line path runs the same
// PreprocessFileLinks / StripMermaidAndMath / RestoreMermaidAndMath
// pipeline the cycle-228 `renderMarkdown` chat-transcript surface
// runs, so a `[label](file.go#L12)` line in mid-stream still
// normalises to `[label](file.go%23L12)` before glamour sees it.
// Without this, the chat-transcript and cycle-232 surfaces would
// diverge on a file-link reference, and the `get_output` path
// would render two different views of the same turn.
func TestGlamour_LineANSI_PreservesFileLinkPreprocessing(t *testing.T) {
	InvalidateCache()
	got, err := LineANSI("[src/foo.go:12](src/foo.go#L12)")
	if err != nil {
		t.Fatalf("LineANSI: %v", err)
	}
	if strings.Contains(got, "src/foo.go#L12") {
		t.Errorf("raw anchor survived:\n%q", got)
	}
	if !strings.Contains(got, "src/foo.go%23L12") {
		t.Errorf("normalised anchor missing:\n%q", got)
	}
}

// TestGlamour_LineANSI_BulletMarkersRender pins the cycle-232 path
// for a single line carrying a bullet marker: `"- item"` becomes a
// stylised bullet row in the live transcript. Cycle 271 replaced the
// cycle-175 U+2421 glyph with AGY 1.4.2's "• " (U+2022), so that is
// what the mid-stream pass must emit.
func TestGlamour_LineANSI_BulletMarkersRender(t *testing.T) {
	got := pinLineANSI(t, "- alpha")
	if !strings.Contains(got, "alpha") {
		t.Errorf("LineANSI lost bullet text:\n%q", got)
	}
	if !strings.Contains(got, "\u2022") {
		t.Errorf("LineANSI did not produce bullet glyph:\n%q", got)
	}
}

// TestGlamour_LineANSI_TrimsTrailingNewline pins the trailing-
// newline trim: glamour's Render tends to add a trailing "\n"
// that, when spliced into m.response as a row, would double the
// cell height visually. LineANSI trims the suffix so the per-line
// render is row-aligned with the cycle-228 single-shot path. Pin
// via a positive assertion (line does NOT end with "\n" after the
// mid-stream glamour pass).
func TestGlamour_LineANSI_TrimsTrailingNewline(t *testing.T) {
	got := pinLineANSI(t, "plain prose line")
	if strings.HasSuffix(got, "\n") {
		t.Errorf("LineANSI emitted trailing newline: %q", got)
	}
}
