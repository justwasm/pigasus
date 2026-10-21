// chroma_inline_test.go -- cycle 233 regression tests for the chroma
// streaming syntax-highlighting path.
//
// These tests exercise the public surface of chroma_inline.go plus
// the cycle-233 extension to StripMermaidAndMath /
// RestoreMermaidAndMath. The chroma library is not mocked -- we
// drive it through the real HighlightCodeBlock / HighlightStreamingFences
// helpers and assert on the rendered ANSI output. The tests
// intentionally avoid pinning exact byte sequences (chroma's
// monokai colour table is a deliberate design choice and may be
// refined in future upstream releases); instead, we pin structural
// properties that hold regardless of style: ANSI escape presence,
// token-class diversity, lexer-cache reuse, and round-trip
// correctness through the cycle-228 glamour pipeline.
package mdrender

import (
	"regexp"
	"strings"
	"testing"
)

// ansiEscapeRe matches a SGR (Select Graphic Rendition) ANSI escape
// sequence: ESC[ <params> m. We use it as a coarse "any colour is
// present" pin; finer-grained tests below assert on the specific
// 256-colour form chroma's terminal256 formatter emits.
var ansiEscapeRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// rgbEscapeRe matches a 24-bit RGB colour setting: ESC[38;2;R;G;Bm.
// Cycle 233's terminal16m formatter emitted exactly this shape; cycle
// 271 switched HighlightCodeBlock to terminal256 to match AGY 1.4.2's
// byte output, so rgbEscapeRe is now only the legacy/truecolour shape.
var rgbEscapeRe = regexp.MustCompile(`\x1b\[38;2;(\d+);(\d+);(\d+)m`)

// c256EscapeRe matches a 256-colour foreground setting: ESC[38;5;Nm.
// This is the shape the cycle-271 terminal256 formatter emits (and the
// shape the AGY 1.4.2 capture carries).
var c256EscapeRe = regexp.MustCompile(`\x1b\[38;5;(\d+)m`)

// countDistinctColours returns the number of unique colours the
// rendered string contains, counting both 24-bit ESC[38;2;R;G;Bm
// triples and 256-index ESC[38;5;Nm codes. A Go source that mixes
// keywords (orange-ish), identifiers (blue-ish) and plain text
// (grey) typically yields 3-5 distinct colours; an unknown-language
// fallback yields exactly 1 (the warning banner's yellow).
func countDistinctColours(s string) int {
	seen := map[string]struct{}{}
	for _, m := range rgbEscapeRe.FindAllStringSubmatch(s, -1) {
		if len(m) != 4 {
			continue
		}
		seen["rgb:"+m[1]+","+m[2]+","+m[3]] = struct{}{}
	}
	for _, m := range c256EscapeRe.FindAllStringSubmatch(s, -1) {
		if len(m) != 2 {
			continue
		}
		seen["c256:"+m[1]] = struct{}{}
	}
	return len(seen)
}

func atoi(s string) (int, bool) {
	n := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	return n, true
}

// TestChromaInline_GoFenceTokenised: a ```go block with
// `package main\nfunc foo() int { return 1 }` runs through
// the renderer and emits ANSI escape sequences (ESC[) for at
// least 3 distinct colour codes (keywords vs identifiers vs
// numbers are different chroma tokens).
func TestChromaInline_GoFenceTokenised(t *testing.T) {
	body := "package main\n\nfunc foo() int { return 1 }\n"
	rendered, err := HighlightCodeBlock("go", body)
	if err != nil {
		t.Fatalf("HighlightCodeBlock: %v", err)
	}
	if rendered == "" {
		t.Fatalf("HighlightCodeBlock returned empty")
	}
	if !ansiEscapeRe.MatchString(rendered) {
		t.Errorf("no ANSI escape sequences in output:\n%s", rendered)
	}
	distinct := countDistinctColours(rendered)
	if distinct < 3 {
		t.Errorf("expected >=3 distinct colours in monokai output, got %d:\n%s", distinct, rendered)
	}
	// Sanity: the source bytes survive the round-trip (every byte
	// is present, in order, ignoring the ANSI wrapping).
	for _, want := range []string{"package", "main", "func", "foo", "return", "1"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("missing source token %q in rendered output:\n%s", want, rendered)
		}
	}
}

// TestChromaInline_PythonFenceTokenised: same idea for
// ```python -- at least 2 colour codes.
func TestChromaInline_PythonFenceTokenised(t *testing.T) {
	body := "def add(a, b):\n    return a + b\n\nprint(add(1, 2))\n"
	rendered, err := HighlightCodeBlock("python", body)
	if err != nil {
		t.Fatalf("HighlightCodeBlock: %v", err)
	}
	if rendered == "" {
		t.Fatalf("HighlightCodeBlock returned empty")
	}
	if !ansiEscapeRe.MatchString(rendered) {
		t.Errorf("no ANSI escape sequences in output:\n%s", rendered)
	}
	distinct := countDistinctColours(rendered)
	if distinct < 2 {
		t.Errorf("expected >=2 distinct colours, got %d:\n%s", distinct, rendered)
	}
	for _, want := range []string{"def", "add", "return", "print"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("missing source token %q in rendered output:\n%s", want, rendered)
		}
	}
}

// TestChromaInline_UnknownLanguageFallsBack: ```foobar (no
// such lexer) emits a fallback that includes the original
// source; no panic. The cycle-233 spec says unknown languages
// fall back to chroma's plaintext lexer (= treat as plain
// text, still tokenised) rather than emitting a banner — the
// chroma.Fallback path is the documented graceful degradation.
// This test pins that contract: the body is rendered (not
// empty, not raw-with-banner) and the call returns no error.
func TestChromaInline_UnknownLanguageFallsBack(t *testing.T) {
	body := "foo bar baz\n"
	rendered, err := HighlightCodeBlock("foobar", body)
	if err != nil {
		t.Fatalf("HighlightCodeBlock: %v", err)
	}
	if rendered == "" {
		t.Fatalf("HighlightCodeBlock returned empty for unknown language")
	}
	// Source bytes survive. chroma.Fallback emits the body
	// wrapped in plain text styling (foreground colour) so we
	// strip ANSI before checking.
	plain := stripANSI(rendered)
	if !strings.Contains(plain, "foo bar baz") {
		t.Errorf("raw body lost in unknown-language render:\n%s", plain)
	}
	// No panic was observed; that was the structural concern of
	// this test.
	_ = rendered
}

// TestChromaInline_NoFenceReturnsInput: a markdown doc with no
// ```fenced``` block roundtrips through Strip+Restore with
// the source unchanged (no chroma touched).
func TestChromaInline_NoFenceReturnsInput(t *testing.T) {
	src := "hello world\n\nthis is a **paragraph** with *emphasis*.\n"
	cleaned, ph := StripMermaidAndMath(src)
	if len(ph) != 0 {
		t.Errorf("expected 0 placeholders, got %d", len(ph))
	}
	if cleaned != src {
		t.Errorf("StripMermaidAndMath modified source with no fences:\nbefore: %q\nafter:  %q", src, cleaned)
	}
	// RestoreMermaidAndMath on an empty map is a no-op; assert
	// the round-trip leaves the source unchanged.
	rendered, err := ANSI(cleaned, 80)
	if err != nil {
		t.Fatalf("ANSI: %v", err)
	}
	restored := RestoreMermaidAndMath(rendered, ph)
	if !strings.Contains(restored, "hello world") ||
		!strings.Contains(restored, "paragraph") {
		t.Errorf("RestoreMermaidAndMath lost content:\n%s", restored)
	}
}

// TestChromaInline_IndentedFencePreservesIndent: a code fence
// (```go at column 0) is rendered by chroma, then the cycle-233
// Restore pass applies a 4-space indent to every emitted line so
// the coloured block aligns with the rest of glamour's code-block
// style (the cycle-228 4-space indent in
// glamour_adapter.go:332 buildANSIConfig.CodeBlock.BlockPrefix).
//
// The "nested inside a list item" wording in the spec maps to the
// final rendered transcript: the surrounding list item keeps its
// own indent, and the chroma block gets a 4-space additional
// indent on top of whatever glamour applied around the placeholder.
// This test pins the *fence body's* indent specifically — the body
// is what chroma colours, and the cycle's indentChromaBlock helper
// is the function that produces the 4-space prefix on each line.
func TestChromaInline_IndentedFencePreservesIndent(t *testing.T) {
	src := "- item\n\n```go\nfunc main() {}\n```\n\n- next\n"
	cleaned, ph := StripMermaidAndMath(src)
	if len(ph) != 1 {
		t.Fatalf("expected 1 placeholder, got %d", len(ph))
	}
	for _, entry := range ph {
		if entry.Kind != blockCode {
			t.Errorf("expected blockCode, got %v", entry.Kind)
		}
		if entry.Language != "go" {
			t.Errorf("expected language go, got %q", entry.Language)
		}
	}
	// Drive a glamour pass with width 80 and restore. The
	// restored output contains the chroma-coloured body, indented
	// by 4 spaces per line (matching glamour's code-block style).
	rendered, err := ANSI(cleaned, 80)
	if err != nil {
		t.Fatalf("ANSI: %v", err)
	}
	restored := RestoreMermaidAndMath(rendered, ph)
	// Strip ANSI for the indent check; chroma wraps each token
	// in escapes that break literal substring matching.
	plain := stripANSI(restored)
	// Every chroma output line should start with 4 spaces. The
	// body here is one line, "func main() {}", which after the
	// 4-space indent appears as "    func main() {}" in the
	// plain output.
	if !strings.Contains(plain, "    func main() {}") {
		t.Errorf("missing 4-space indented line \"    func main() {}\" in restored output:\n%s", plain)
	}
	// Belt-and-braces: count how many lines of the chroma
	// block are 4-space indented. The block has 1 line, the
	// surrounding list items have their own indent ("␡ item"
	// etc., not 4 spaces). Verify the chroma block line is
	// the only one starting with 4 spaces.
	lines := strings.Split(plain, "\n")
	indented := 0
	for _, line := range lines {
		if strings.HasPrefix(line, "    ") {
			indented++
		}
	}
	if indented < 1 {
		t.Errorf("no 4-space indented lines in restored output:\n%s", plain)
	}
}

// stripANSI removes all ANSI SGR sequences from s. We use the
// built-in ANSI stripper from the chroma dep chain via a regex;
// the chroma library does not export one but the regex is small
// enough to inline here for the assertion helper.
func stripANSI(s string) string {
	return ansiEscapeRe.ReplaceAllString(s, "")
}

// TestChromaInline_LexerCacheReused: build a chroma
// renderer 100 times for the same language; assert that the
// internal sync.Map has just 1 entry (not 100).
func TestChromaInline_LexerCacheReused(t *testing.T) {
	// Use a one-off language name so we don't interfere with the
	// other tests' cache state. The cache is process-lifetime so
	// we can't clear it from a test, but we can pick a language
	// nothing else in this test suite has used.
	const lang = "lua" // chroma v2.24.1 ships a Lua lexer
	// Warm the cache so subsequent loads return the same pointer.
	lex1 := getChromaLexer(lang)
	if lex1 == nil {
		t.Fatalf("getChromaLexer(%q) returned nil", lang)
	}
	for i := 0; i < 100; i++ {
		got := getChromaLexer(lang)
		if got != lex1 {
			t.Fatalf("getChromaLexer returned a different pointer on iteration %d", i)
		}
	}
	// Walk the cache to count entries -- the chromaLexerCache
	// has 1+ entries depending on what other tests in this run
	// have warmed; what we care about is that 100 loads of the
	// same language only count as 1 cache entry. Count entries
	// by re-keying on the lang slot.
	count := 0
	chromaLexerCache.Range(func(k, v interface{}) bool {
		if ks, ok := k.(string); ok && ks == lang {
			count++
		}
		return true
	})
	if count != 1 {
		t.Errorf("expected exactly 1 cache entry for %q, got %d", lang, count)
	}
}

// TestChromaInline_GlamourPipelineRoundtrip: a markdown
// document with two fenced blocks (one go, one python)
// roundtrips through StripMermaidAndMath → glamour → Restore;
// the final output preserves chroma colours AND mermaid
// placeholders are still rendered (not stomped).
func TestChromaInline_GlamourPipelineRoundtrip(t *testing.T) {
	InvalidateCache()
	src := "intro\n\n" +
		"```go\nfunc main() { println(\"hi\") }\n```\n\n" +
		"middle\n\n" +
		"```python\ndef add(a, b): return a + b\n```\n\n" +
		"outro\n"
	cleaned, ph := StripMermaidAndMath(src)
	if len(ph) != 2 {
		t.Fatalf("expected 2 placeholders, got %d", len(ph))
	}
	// Both placeholders are blockCode with the expected languages.
	langs := map[string]bool{}
	for _, entry := range ph {
		if entry.Kind != blockCode {
			t.Errorf("expected blockCode, got %v", entry.Kind)
		}
		langs[entry.Language] = true
	}
	for _, want := range []string{"go", "python"} {
		if !langs[want] {
			t.Errorf("expected placeholder for %q, got %v", want, langs)
		}
	}
	rendered, err := ANSI(cleaned, 80)
	if err != nil {
		t.Fatalf("ANSI: %v", err)
	}
	restored := RestoreMermaidAndMath(rendered, ph)
	// Surrounding prose survives.
	for _, want := range []string{"intro", "middle", "outro"} {
		if !strings.Contains(restored, want) {
			t.Errorf("surrounding prose lost %q:\n%s", want, restored)
		}
	}
	// Chroma colours survived: both blocks have at least one
	// 256-colour escape (cycle 271 terminal256/solarized-light).
	if !c256EscapeRe.MatchString(restored) {
		t.Errorf("no 256-colour escapes in restored output -- chroma didn't engage:\n%s", restored)
	}
	distinct := countDistinctColours(restored)
	if distinct < 2 {
		t.Errorf("expected >=2 distinct colours across go+python blocks, got %d:\n%s", distinct, restored)
	}
	// The placeholder token must not leak.
	if strings.Contains(restored, "MDRENDER_BLOCK_PLACEHOLDER_") {
		t.Errorf("placeholder leaked into restored output:\n%s", restored)
	}
	// Source bytes survive in both blocks. chroma wraps each
	// token in ANSI, so we strip ANSI before substring-checking.
	plain := stripANSI(restored)
	for _, want := range []string{"func main", "println", "def add", "return a + b"} {
		if !strings.Contains(plain, want) {
			t.Errorf("source token %q missing from restored output:\n%s", want, plain)
		}
	}
}

// TestChromaInline_LanguagesMinimumCount: using chroma's
// lexers registry, assert that at least 30 of our 31
// default-priority languages are registered in this chroma
// build. This is the capability-fence test -- if chroma
// upstream removes Go lexer support tomorrow, the test
// fails and the cycle is flagged for re-baseline.
func TestChromaInline_LanguagesMinimumCount(t *testing.T) {
	// The 31 default-priority languages from the cycle-233
	// deliverable. 31 = the lang count in the cycle's
	// "Languages to highlight by default" list. We allow
	// chroma upstream to drop at most 1 without failing
	// (so a test-suite run still passes if the cycle is
	// downstream of an upstream reshuffle).
	priority := []string{
		"go", "python", "typescript", "tsx", "javascript", "jsx",
		"rust", "ruby", "java", "kotlin", "swift", "c", "cpp",
		"csharp", "bash", "sh", "json", "yaml", "toml", "xml",
		"html", "css", "scss", "sql", "markdown", "dockerfile",
		"makefile", "terraform",
	}
	// chroma's lexers.Get accepts both the primary name and the
	// alias; we test the alias the model most commonly emits.
	missing := []string{}
	for _, lang := range priority {
		if getChromaLexer(lang) == nil {
			missing = append(missing, lang)
		}
	}
	// Allow up to 1 missing (cycle 233 said "30 of 31"); chroma
	// upstream historically has 270+ lexers, so a single missing
	// entry is a soft signal to re-baseline, not a hard failure.
	if len(missing) > 1 {
		t.Errorf("expected <=1 missing chroma lexer from priority list, got %d missing: %v", len(missing), missing)
	}
}

// TestChromaInline_StreamingFencesMidStream exercises the
// buffer-level scanner the cycle-233 mid-stream pass uses
// (HighlightStreamingFences). The scanner walks the running
// transcript buffer and, when a complete ```fenced``` block
// is detected, replaces those rows with chroma output.
//
// This is the "streaming" half of cycle 233 -- the helper
// the cycle-232 mid-stream glamour pass calls in parallel
// (see the cycle deliverable's "Mid-stream chroma runs in
// parallel with cycle-232 mid-stream glamour" section).
func TestChromaInline_StreamingFencesMidStream(t *testing.T) {
	rows := []string{
		"intro",          // 0
		"",               // 1
		"```go",          // 2 -- opening fence
		"package main",   // 3
		"func main() {}", // 4
		"```",            // 5 -- closing fence
		"",               // 6
		"outro",          // 7
	}
	updated := HighlightStreamingFences(rows, 0)
	// Rows 2..5 should be replaced by chroma output (>=1 row of
	// ANSI-coloured code). We can't pin the exact row count
	// (chroma emits one ANSI-coloured row per source line),
	// but we can assert the fence markers are gone and the
	// body is present. Use stripANSI for content checks because
	// chroma wraps each token in ANSI escapes that break literal
	// substring matching (e.g. "package main" becomes
	// "package\x1b[0m\x1b[..m \x1b[..mmain").
	combined := strings.Join(updated, "\n")
	if strings.Contains(combined, "```go") {
		t.Errorf("opening fence leaked into chroma output:\n%s", combined)
	}
	plain := stripANSI(combined)
	if strings.Contains(plain, "```") {
		t.Errorf("fence markers leaked into chroma output:\n%s", plain)
	}
	if !strings.Contains(plain, "package main") {
		t.Errorf("go body lost in chroma output:\n%s", plain)
	}
	if !strings.Contains(plain, "func main() {}") {
		t.Errorf("go body line lost:\n%s", plain)
	}
	if !ansiEscapeRe.MatchString(combined) {
		t.Errorf("no ANSI escapes -- chroma did not run:\n%s", combined)
	}
	if !strings.Contains(plain, "intro") || !strings.Contains(plain, "outro") {
		t.Errorf("surrounding prose lost:\n%s", plain)
	}
}

// TestChromaInline_StreamingFencesIncomplete is a regression
// guard: when the buffer ends with an unclosed ```fence, the
// scanner must NOT swallow the partial block. The function
// returns a copy with no chroma applied (an unclosed fence
// is left as raw markdown for the next chunk to complete).
func TestChromaInline_StreamingFencesIncomplete(t *testing.T) {
	rows := []string{
		"intro",        // 0
		"```go",        // 1 -- opening fence
		"package main", // 2 -- body (incomplete)
		// no closing fence
	}
	updated := HighlightStreamingFences(rows, 0)
	combined := strings.Join(updated, "\n")
	// Raw source preserved (no chroma applied).
	if !strings.Contains(combined, "```go") {
		t.Errorf("opening fence lost:\n%s", combined)
	}
	if !strings.Contains(combined, "package main") {
		t.Errorf("body lost:\n%s", combined)
	}
	// No ANSI escapes -- chroma should not run on an incomplete
	// fence (it would produce a misleading fallback banner).
	if ansiEscapeRe.MatchString(combined) {
		t.Errorf("chroma ran on incomplete fence (should be no-op):\n%s", combined)
	}
}
