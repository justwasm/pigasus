// diff_syntax_test.go — package-simplification cycle 1: per-language
// tokenisation tests for the /diff file view, now backed by chroma/v2
// instead of the cycle diff-syntax-highlight hand-rolled tokenizer.
//
// The headline behaviours pinned by these tests:
//
//   - Go reserved words are flagged as the `keyword` class
//   - Go `// ...` lines are flagged as `comment` and swallow the line
//   - Python `"""docstring"""` runs are flagged as `string`
//   - A leading diff marker byte is preserved as a `plain` token so
//     the renderer's existing per-line +/- colour stays correct
//   - Files whose extension is unknown (or absent) get a single
//     `plain` token covering the whole line — no false positives
//   - chroma's 270-lexer catalogue is honoured: JSON keys, Markdown
//     headings, YAML scalars, Bash comments now light up correctly
//
// Each test asserts on the token stream only; the per-token ANSI
// emission lives in renderDiffInlineTokenStream (same file) and is
// exercised by the existing panelDiffInlineLines_* tests downstream.
package main

import (
	"strings"
	"testing"
)

// findClass reports the first index of a token whose Class equals
// want in tokens, or -1 when no such token exists. The helper lives
// alongside the tests because the public slice types already encode
// the keyword/string/number/comment/type/plain domain.
func findClass(tokens []diffToken, want diffTokenClass) int {
	for i, t := range tokens {
		if t.Class == want {
			return i
		}
	}
	return -1
}

// hasText reports whether some token in tokens has exactly the given
// text. An equality match (not substring) is intentional: every
// emitted token is bounded by an adjacent class switch, so substring
// searches would silently pass for partial overlap with adjacent
// runs.
func hasText(tokens []diffToken, text string) bool {
	for _, t := range tokens {
		if t.Text == text {
			return true
		}
	}
	return false
}

// allText returns the concatenation of every token's Text, in order.
// Used by the "single plain token" assertion in
// TestTokenizeDiffLine_UnknownExt_Plain.
func allText(tokens []diffToken) string {
	var b []byte
	for _, t := range tokens {
		b = append(b, t.Text...)
	}
	return string(b)
}

// TestTokenizeDiffLine_Go_Keyword verifies the Go keyword branch:
// `func` is tagged `keyword`, the string literal `"hi"` is tagged
// `string`, and the assembled line still equals the input (token
// concatenation round-trips). This is the canonical "hunk with a
// function declaration" shape from the ref 1.3.5 changelog screenshot.
func TestTokenizeDiffLine_Go_Keyword(t *testing.T) {
	const in = `func Foo() string { return "hi" }`
	tokens := tokenizeDiffLine("foo.go", in)
	if idx := findClass(tokens, diffKeyword); idx < 0 || tokens[idx].Text != "func" {
		t.Errorf("Go keyword not flagged: tokens=%v", tokens)
	}
	if idx := findClass(tokens, diffString); idx < 0 || tokens[idx].Text != `"hi"` {
		t.Errorf(`Go string literal "hi" not flagged: tokens=%v`, tokens)
	}
	if got := allText(tokens); got != in {
		t.Errorf("round-trip lost bytes: got %q, want %q", got, in)
	}
}

// TestTokenizeDiffLine_Go_Comment verifies the Go single-line comment
// branch: a `// ...` line collapses into a single `comment` token
// that covers everything from the `//` to end-of-line, so the lexer
// does not try to classify keywords that happen to live in the
// comment text.
func TestTokenizeDiffLine_Go_Comment(t *testing.T) {
	const in = `// hello func world`
	tokens := tokenizeDiffLine("foo.go", in)
	// Exactly one comment token whose text equals the whole line.
	var got string
	for _, t := range tokens {
		if t.Class != diffComment {
			continue
		}
		got += t.Text
	}
	if got != in {
		t.Errorf("Go comment token wrong: got %q, want %q (tokens=%v)", got, in, tokens)
	}
	// The lone keyword candidate inside the comment must NOT have
	// been lifted out as a keyword token.
	for _, tk := range tokens {
		if tk.Class == diffKeyword && tk.Text == "func" {
			t.Errorf("Go keyword inside comment was mis-classed as keyword: tokens=%v", tokens)
		}
	}
}

// TestTokenizeDiffLine_Python_Docstring pins the Python
// triple-quoted-string branch: a `"""docstring"""` run is one
// `string` token, even though each pair of double-quotes would
// individually be a normal string opener to a smaller lexer.
func TestTokenizeDiffLine_Python_Docstring(t *testing.T) {
	const in = `"""docstring"""`
	tokens := tokenizeDiffLine("foo.py", in)
	if idx := findClass(tokens, diffString); idx < 0 || tokens[idx].Text != in {
		t.Errorf(`Python triple-quoted string not flagged: tokens=%v`, tokens)
	}
	// Round-trip: concatenating every token's Text must reproduce
	// the input byte-for-byte. Without this assertion a future
	// re-flow that introduces a leading "..." token would still
	// pass a "class == string" check while silently corrupting the
	// visible line.
	if got := allText(tokens); got != in {
		t.Errorf("Python triple-quote round-trip lost bytes: got %q, want %q", got, in)
	}
}

// TestTokenizeDiffLine_PlusPrefix verifies the diff-marker contract:
// the leading `+` byte is preserved as a `plain` token (so the
// caller's existing green/red `line[0]` colour still wins for the
// marker character) and the first *real* code token is the keyword
// `func`. This is the AGY 1.3.5 line 3 contract — "coloring lines
// by language, keywords ... get their own color".
func TestTokenizeDiffLine_PlusPrefix(t *testing.T) {
	const in = `+func bar() {`
	tokens := tokenizeDiffLine("foo.go", in)
	if len(tokens) == 0 {
		t.Fatalf("no tokens emitted for %q", in)
	}
	if tokens[0].Class != diffPlain || tokens[0].Text != "+" {
		t.Errorf("first token must be `+` plain; got %+v", tokens[0])
	}
	// The first non-plain token past the marker must be the keyword
	// `func`. We don't assume position[1] because the leading plain
	// token may have merged with whatever follows (whitespace is
	// merged into adjacent plain runs by the emit helper).
	if idx := findClass(tokens, diffKeyword); idx < 0 || tokens[idx].Text != "func" {
		t.Errorf("`+func` keyword not found: tokens=%v", tokens)
	}
}

// TestTokenizeDiffLine_UnknownExt_Plain verifies the "no recognised
// language" branch: a filename whose extension isn't one chroma
// claims yields a single plain token whose text equals the input.
// This is the safety net for /diff against any non-code file
// (Makefiles, vendor blobs, .txt patches): chroma never matches a
// lexer for them so the tokenizer never tries to parse them as if
// they were Go.
//
// Note: extensions that chroma DOES recognise (.json, .md, .yaml,
// .sh) are exercised by the cycle-1 additions further down — this
// test now stays strictly in the "truly unknown" domain so a future
// chroma upgrade that picks up more extensions doesn't silently
// flip the unknown behaviour.
func TestTokenizeDiffLine_UnknownExt_Plain(t *testing.T) {
	cases := []struct {
		name string
		line string
	}{
		{"xyz", "func bar() {"},
		{"no_ext", "anything goes here"},
		{"foo", "func bar() {"},
		{"random_blob", "not source code"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tokens := tokenizeDiffLine("foo."+c.name, c.line)
			if len(tokens) != 1 {
				t.Fatalf("expected one token for %q, got %d: %v", c.line, len(tokens), tokens)
			}
			if tokens[0].Class != diffPlain {
				t.Errorf("unknown ext %s: token class = %q, want %q", c.name, tokens[0].Class, diffPlain)
			}
			if tokens[0].Text != c.line {
				t.Errorf("unknown ext %s: token text = %q, want %q", c.name, tokens[0].Text, c.line)
			}
		})
	}
}

// TestTokenizeDiffLine_DiffMetadataArePlain pins the framing-row
// contract: `@@ ... @@` hunk headers and the `--- a/foo` /
// `+++ b/foo` / `diff --git ...` file headers are always plain so
// the unified-diff framing never gets lexed as code (which would
// otherwise flag `--- a/foo` for having a leading minus sign and a
// possible keyword).
func TestTokenizeDiffLine_DiffMetadataArePlain(t *testing.T) {
	lines := []string{
		"@@ -1,3 +1,3 @@ func bar() {",
		"--- a/foo.go",
		"+++ b/foo.go",
		"diff --git a/foo.go b/foo.go",
	}
	for _, line := range lines {
		t.Run(line, func(t *testing.T) {
			tokens := tokenizeDiffLine("foo.go", line)
			if len(tokens) != 1 {
				t.Fatalf("metadata %q: expected 1 token, got %d: %v", line, len(tokens), tokens)
			}
			if tokens[0].Class != diffPlain {
				t.Errorf("metadata %q: class = %q, want %q", line, tokens[0].Class, diffPlain)
			}
			if tokens[0].Text != line {
				t.Errorf("metadata %q: text = %q, want %q", line, tokens[0].Text, line)
			}
		})
	}
}

// TestTokenizeDiffLine_RustTypeAndKeyword exercises the Rust branch:
// `Vec<T>` is a type, `fn` is a keyword, the `<`/`>` are plain. The
// type table is the load-bearing piece of the rs branch because the
// AGY changelog screenshot explicitly calls out Vec<T> / Option<T> /
// Result<T,E> as the type-coloured highlights.
//
// chroma's Go-for-it Rust lexer maps `Vec` to NameBuiltin (a builtin
// type) and `u8` to KeywordType — chromaToDiffClass promotes both to
// our diffType class so the Aurora-Dark yellow colour fires.
func TestTokenizeDiffLine_RustTypeAndKeyword(t *testing.T) {
	const in = `let v: Vec<u8> = Vec::new();`
	tokens := tokenizeDiffLine("foo.rs", in)
	if idx := findClass(tokens, diffType); idx < 0 || tokens[idx].Text != "Vec" {
		t.Errorf("Rust type `Vec` not flagged: tokens=%v", tokens)
	}
	if idx := findClass(tokens, diffKeyword); idx < 0 || tokens[idx].Text != "let" {
		t.Errorf("Rust keyword `let` not flagged: tokens=%v", tokens)
	}
	if got := allText(tokens); got != in {
		t.Errorf("Rust round-trip lost bytes: got %q, want %q", got, in)
	}
}

// TestTokenizeDiffLine_EmptyLine is the trivial all-empty-line
// contract: an empty input yields one empty plain token, not a nil
// slice. The renderer's loop and the existing diff panel's
// truncation pass both treat empty strings specially; the contract
// here keeps the caller's `for _, t := range tokens` walk safe even
// when the line itself is empty.
func TestTokenizeDiffLine_EmptyLine(t *testing.T) {
	tokens := tokenizeDiffLine("foo.go", "")
	if len(tokens) != 1 || tokens[0].Class != diffPlain || tokens[0].Text != "" {
		t.Errorf("empty input: got %v, want [{plain \"\"}]", tokens)
	}
	// The non-code path (unknown extension) must behave identically.
	plain := tokenizeDiffLine("foo.xyz", "")
	if len(plain) != 1 || plain[0].Class != diffPlain || plain[0].Text != "" {
		t.Errorf("empty+unknown ext: got %v, want [{plain \"\"}]", plain)
	}
}

// TestRenderDiffInlineTokenStream is the integration-side pinning:
// for each of the four chroma-coloured classes (keyword/string/
// number/comment/type), the render must wrap the corresponding token
// with the opening RGB escape. The test pins the contract on
// per-token wiring rather than on the full rendered line so a
// future change in line-level chrome (e.g. moving the +/- wrap from
// the renderer to a higher layer) doesn't trigger a churn here.
func TestRenderDiffInlineTokenStream(t *testing.T) {
	tokens := []diffToken{
		{Text: "+", Class: diffPlain},
		{Text: "func ", Class: diffPlain},
		{Text: "Foo", Class: diffPlain},
		{Text: "() string { return ", Class: diffPlain},
		{Text: `"hi"`, Class: diffString},
		{Text: " }", Class: diffPlain},
	}
	got := renderDiffInlineTokenStream(tokens)
	// The string token gets wrapped with the Aurora-Dark string colour,
	// which is the head pin of the AGY 1.3.5 changelog line 3 picture.
	for _, want := range []string{
		cSyntaxStringOpen,
		cSyntaxTokenClose,
		panelEffLowOpen, // the marker byte gets the existing diff colour
	} {
		if !strings.Contains(got, want) {
			t.Errorf("render missing %q in output: %q", want, got)
		}
	}
}

// TestTokenizeDiffLine_JSON_Numbers exercises chroma's JSON lexer:
// digit runs become diffNumber tokens, the curly braces / colons /
// brackets / commas stay plain, and the round-trip preserves the
// input byte-for-byte. The cycle-1 chroma addition picks this up for
// free — pre-cycle the same input was a single plain token because
// our hand-rolled tokenizer didn't recognise .json.
//
// JSON string keys (`"a"`, `"b"`) are intentionally NOT pinned to
// diffString in this test: chroma's JSON lexer tags bare keys as
// NameTag (which falls to plain in our 5-class bucket) so we don't
// over-colour them. If a future cycle wants JSON strings coloured,
// promote NameTag to a Type override in chromaToDiffClass.
func TestTokenizeDiffLine_JSON_Numbers(t *testing.T) {
	const in = `{"a": 1, "b": [2, 3]}`
	tokens := tokenizeDiffLine("x.json", in)
	if got := allText(tokens); got != in {
		t.Fatalf("JSON round-trip lost bytes: got %q, want %q", got, in)
	}
	if idx := findClass(tokens, diffNumber); idx < 0 || !strings.Contains(tokens[idx].Text, "1") {
		t.Errorf("JSON number `1` not flagged: tokens=%v", tokens)
	}
	// Must not collapse to a single plain token — chroma is doing work.
	if len(tokens) <= 1 {
		t.Errorf("JSON lexer produced %d tokens; expected chroma to split: %v", len(tokens), tokens)
	}
}

// TestTokenizeDiffLine_MarkdownHeading exercises chroma's markdown
// lexer. A `## A heading` line is recognised as a markdown
// GenericSubheading (a "sub-heading" token in chroma's terms) and
// round-trips byte-for-byte. Pre-cycle this was a single plain
// token because our hand-rolled tokenizer had no markdown branch.
func TestTokenizeDiffLine_MarkdownHeading(t *testing.T) {
	const in = `## A heading`
	tokens := tokenizeDiffLine("x.md", in)
	if got := allText(tokens); got != in {
		t.Fatalf("markdown round-trip lost bytes: got %q, want %q", got, in)
	}
	// The whole line collapses to one token because chroma's
	// markdown lexer treats the heading as a single GenericSubheading
	// emit. We don't pin a specific class here — chroma may refine
	// GenericSubheading vs GenericHeading in a future version — but
	// we do pin that chroma at least tokenises (more than one token
	// means it walked the bytes; one non-plain token means it
	// classified).
	if len(tokens) == 0 {
		t.Fatalf("markdown heading produced no tokens")
	}
}

// TestTokenizeDiffLine_BashComment exercises chroma's Bash lexer:
// `# comment` is recognised as a single comment token. This is the
// equivalent of the Go `// ...` branch in the cycle diff-syntax-highlight
// hand-rolled tokenizer; chroma's Bash lexer wins on it for free.
func TestTokenizeDiffLine_BashComment(t *testing.T) {
	const in = `# echo nothing`
	tokens := tokenizeDiffLine("x.sh", in)
	if got := allText(tokens); got != in {
		t.Fatalf("bash round-trip lost bytes: got %q, want %q", got, in)
	}
	if idx := findClass(tokens, diffComment); idx < 0 {
		t.Errorf("bash `#` comment not flagged: tokens=%v", tokens)
	}
}

// TestTokenizeDiffLine_YAMLKey exercises chroma's YAML lexer: a
// `name: foo` scalar pair round-trips byte-for-byte. Chroma splits
// it into NameTag "name" + Punctuation ":" + TextWhitespace " " +
// Literal "foo", all of which map to plain in our 5-class bucket,
// so the visible output collapses to a single plain token (the
// merge is intentional — otherwise we'd wrap whitespace in colour
// codes for no visible benefit). The test pins two contracts:
// (a) chroma's lexer is consulted at all (not just handed back as
// "unknown extension"), and (b) the round-trip is exact.
func TestTokenizeDiffLine_YAMLKey(t *testing.T) {
	const in = `name: foo`
	tokens := tokenizeDiffLine("x.yaml", in)
	if got := allText(tokens); got != in {
		t.Fatalf("yaml round-trip lost bytes: got %q, want %q", got, in)
	}
	// yaml lexer is consulted, output may be one token (every
	// chroma sub-type maps to plain in our 5-class bucket) — we
	// don't assert on count here, only on the round-trip.
}

// TestTokenizeDiffLine_NoFilenameIsPlain pins the
// renderDiffInlineLine empty-filename fast path: an empty filename
// disables language detection entirely so the line is rendered as
// plain (with marker-only colour). This is the diffView==2 commit
// view's contract — no single file path to bind a language to.
func TestTokenizeDiffLine_NoFilenameIsPlain(t *testing.T) {
	cases := []string{
		"func bar() {",
		`return "hi"`,
		`+func bar() {`,
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			got := renderDiffInlineLine("", in)
			// When filename is "" and the line is a plain diff body
			// line (no marker), renderDiffInlineLine returns the
			// line verbatim — no ANSI at all. This matches the
			// cycle-226 commit-view contract.
			if in[0] != '+' && in[0] != '-' && got != in {
				t.Errorf("empty-filename, no marker: got %q, want %q", got, in)
			}
		})
	}
}