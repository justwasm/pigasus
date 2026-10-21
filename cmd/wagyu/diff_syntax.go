// diff_syntax.go — package-simplification cycle 1: per-token syntax
// highlighting for the /diff file view, now backed by chroma/v2
// instead of the cycle diff-syntax-highlight hand-rolled tokenizer.
//
// AGY 1.3.5 ships Aurora-Dark syntax highlighting inside the /diff
// file panel: keywords, strings, numbers, comments, and types each get
// their own colour so a hunk is faster to scan. The closed-source
// implementation pulls in chroma (the v1.3.6 binary recovers the
// chroma init function plus the five Aurora hex tokens via
// `strings ~/.local/bin/agy | grep chroma`).
//
// The first version of this file (cycle diff-syntax-highlight) hand-rolled
// a five-language tokenizer (Go/Py/JS/TS/Rs, ~625 lines) to avoid the
// chroma dependency. This cycle restores chroma as the implementation:
// the hand-rolled keyword sets, comment walk, string scan, identifier
// classifier, and language table are all replaced by `chroma.Lexer` +
// `chroma.Coalesce`, mapped to the same five-word public token classes
// (plain / keyword / string / number / comment / type).
//
// The public surface is unchanged:
//   - diffTokenClass (the 5-class enum)
//   - diffToken (the {Text, Class} pair)
//   - tokenizeDiffLine (filename + line -> []diffToken)
//   - diffTokenOpen (class -> Aurora-Dark ANSI open)
//   - renderDiffInlineTokenStream (token stream -> ANSI string)
//   - renderDiffInlineLine (filename + line -> ANSI string in one call)
//
// Diff metadata lines (`@@ ...`, `--- a/...`, `+++ b/...`,
// `diff --git ...`) are still emitted as a single plain token so the
// unified-diff framing is never misread as code. The first diff-marker
// byte (`+`, `-`, ` `) is preserved as a plain token at position 0 so
// the caller's existing green / red per-line marker colour keeps
// working unchanged. The marker byte is a no-op for chroma: chroma
// never sees the marker, only the bytes after it.
//
// The output remains a flat []diffToken of text/class pairs. Rendering
// is kept out of this file: see renderDiffInlineLine in
// slash_viewer_panels.go (cycle diff-folding) and diffInlineStyle
// (cycle diff-syntax-highlight) for the per-token ANSI emission that
// wraps the tokens with the colour constants declared in colors.go.
//
// Language coverage: chroma ships 270+ lexers via its embedded XML
// catalogue (lexers.GlobalLexerRegistry) plus a handful of hand-rolled
// lexers (Go, Markdown, HTML, PHP, ...). The hand-rolled 5-language
// set is a strict subset; everything that worked before still works,
// and we now also light up .json / .yaml / .md / .sh / .sql / .css /
// .html / .xml / .toml / .ini and so on for free.
package main

import (
	"strings"

	"github.com/alecthomas/chroma/v2"
	lexers "github.com/alecthomas/chroma/v2/lexers"
)

// diffTokenClass is the high-level token kind returned by
// tokenizeDiffLine. The values are intentionally short so they survive
// a snapshot test (`plain`, `keyword`, `string`, `number`, `comment`,
// `type`) without any extra parsing on the consumer side.
type diffTokenClass string

const (
	diffPlain   diffTokenClass = "plain"
	diffKeyword diffTokenClass = "keyword"
	diffString  diffTokenClass = "string"
	diffNumber  diffTokenClass = "number"
	diffComment diffTokenClass = "comment"
	diffType    diffTokenClass = "type"
)

// diffToken is one element of a tokenised diff line. Text is the raw
// (non-ANSI) substring; Class drives the colour applied by the caller.
type diffToken struct {
	Text  string
	Class diffTokenClass
}

// isDiffMarker reports whether line starts with a unified-diff body
// marker (`+`, `-`, or the leading space on a context line). The
// renderer in renderDiffInlineTokenStream and the plain-mode
// fast-path in renderDiffInlineLine both rely on this check to emit
// the per-line green/red colour around the marker byte.
func isDiffMarker(line string) bool {
	if line == "" {
		return false
	}
	switch line[0] {
	case '+', '-', ' ':
		return true
	}
	return false
}

// isDiffMetadataLine reports whether line is a unified-diff framing
// row (`diff --git`, `--- a/...`, `+++ b/...`, `@@ ... @@`). These are
// always returned as a single plain token so the framing never gets
// mis-classed as code (a `@@ ... @@` hunk header legitimately contains
// what look like minus signs, and `--- a/foo.go` would otherwise be
// flagged for `--` arithmetic).
func isDiffMetadataLine(line string) bool {
	if line == "" {
		return false
	}
	switch line[0] {
	case '@':
		// hunk header. Allow either a leading "@@ ..." or the legacy
		// "@ ..." form (the reference binary accepts both).
		return strings.HasPrefix(line, "@@") || strings.HasPrefix(line, "@ ")
	case '-':
		return strings.HasPrefix(line, "--- ")
	case '+':
		return strings.HasPrefix(line, "+++ ")
	case 'd':
		return strings.HasPrefix(line, "diff ")
	}
	return false
}

// chromaLexerForFilename looks up a chroma lexer by filename. The
// lookup uses chroma.Match (which scans Filenames / AliasFilenames
// globs across the 270-lexer catalogue). Returns nil when the
// filename is empty or no lexer claims the extension — the caller
// short-circuits to "single plain token" for those cases.
func chromaLexerForFilename(filename string) chroma.Lexer {
	if filename == "" {
		return nil
	}
	return lexers.Match(filename)
}

// chromaToDiffClass maps chroma's TokenType hierarchy to our 5 syntax
// classes. chroma exposes ~80 TokenType values organised as a
// category (1000-step) / sub-category (100-step) / leaf tree (see
// types.go: "Categories of types are grouped in ranges of 1000, while
// sub-categories are in ranges of 100"). We bucket via SubCategory()
// so every LiteralString.* (backtick, double, single, heredoc, doc,
// regex, ...) lands in diffString, every LiteralNumber.* in
// diffNumber, etc.
//
// The exact-match overrides catch the cases where chroma splits what
// would otherwise be a `keyword` or `plain` token into our `type`
// bucket — Go's `string`/`int`/`error` are KeywordType, Rust's
// `Vec`/`Option`/`Result` are NameBuiltin, any user-defined class
// name is NameClass. The Aurora-Dark type colour (#e5c07b) is the
// yellow that the AGY changelog screenshot calls out for `Vec<T>` /
// `Option<T>` / `Result<T,E>` specifically.
func chromaToDiffClass(tt chroma.TokenType) diffTokenClass {
	// Exact-type overrides first.
	switch tt {
	case chroma.KeywordType,
		chroma.NameClass,
		chroma.NameBuiltin,
		chroma.NameBuiltinPseudo,
		chroma.NameVariableClass,
		chroma.NameVariableMagic,
		chroma.LiteralStringSymbol:
		return diffType
	}
	switch tt.SubCategory() {
	case chroma.Keyword:
		return diffKeyword
	case chroma.LiteralString:
		return diffString
	case chroma.LiteralNumber:
		return diffNumber
	case chroma.Comment:
		return diffComment
	}
	return diffPlain
}

// chromaTokenize runs chroma on text and returns our []diffToken.
// The lexer is wrapped in chroma.Coalesce so adjacent same-type
// tokens are merged into one (chroma's raw output is finer-grained
// than ours — e.g. a Python `"""docstring"""` is 4 chroma tokens
// (LiteralStringAffix + LiteralStringDouble × 3) but coalesces into
// one LiteralStringDouble that we map to a single diffString).
//
// After coalescing we apply our 5-class mapping (chromaToDiffClass)
// and merge consecutive output tokens whose mapped class is the
// same — chroma's Coalesce merges by raw chroma type, but our
// `type` bucket groups KeywordType + NameBuiltin + NameClass, so
// `let v: Vec<u8>` coalesces into `{keyword "let "}` + `{plain "v: "}`
// + `{type "Vec<u8>"}` instead of three separate `type` tokens.
//
// On any error we fall back to a single plain token covering text.
// Diff lines are short enough that this branch is rare.
func chromaTokenize(lex chroma.Lexer, text string) []diffToken {
	c := chroma.Coalesce(lex)
	it, err := c.Tokenise(nil, text)
	if err != nil {
		return []diffToken{{Text: text, Class: diffPlain}}
	}
	var out []diffToken
	for tok := it(); tok != chroma.EOF; tok = it() {
		if tok.Value == "" {
			continue
		}
		cls := chromaToDiffClass(tok.Type)
		// Merge consecutive same-class tokens. chroma.Coalesce
		// already merged by chroma type, but our mapping
		// collapses across sub-types (e.g. KeywordType and
		// NameBuiltin both -> diffType), so we re-coalesce here.
		if n := len(out); n > 0 && out[n-1].Class == cls {
			out[n-1].Text += tok.Value
			continue
		}
		out = append(out, diffToken{Text: tok.Value, Class: cls})
	}
	if len(out) == 0 {
		return []diffToken{{Text: text, Class: diffPlain}}
	}
	return out
}

// tokenizeDiffLine splits one diff line into a flat token stream. The
// caller decides what to do with the markers (the diff panel's
// per-line renderer prepends the existing +/- green / - red colour;
// the foreground cycle-127 tool-step renderer passes plain). Empty
// input returns one empty plain token.
//
// The decision tree:
//
//	1. empty input         -> [{plain ""}]
//	2. diff metadata line  -> [{plain "<whole line>"}]
//	3. unknown / no lexer  -> [{plain "<whole line>"}]
//	4. diff-marker prefix  -> [{plain <marker>}] + chromaTokenize(<rest>)
//	5. plain code line     -> chromaTokenize(<whole line>)
//
// Step 4 is the load-bearing piece: chroma doesn't know about
// unified-diff markers, so we strip the leading byte ourselves and
// prepend it as a plain token. The renderer's per-line green/red
// then colours the marker alone, and chroma's syntax colours light
// up the code after it.
func tokenizeDiffLine(filename, line string) []diffToken {
	if line == "" {
		return []diffToken{{Text: "", Class: diffPlain}}
	}
	if isDiffMetadataLine(line) {
		return []diffToken{{Text: line, Class: diffPlain}}
	}
	lex := chromaLexerForFilename(filename)
	if lex == nil {
		// No filename or no recognised extension -> render plain.
		// We deliberately do NOT route through lexers.Fallback
		// here because chroma.Fallback still walks every byte and
		// emits Text tokens — same visible output but with extra
		// allocations and a per-line registry lookup we don't need.
		return []diffToken{{Text: line, Class: diffPlain}}
	}
	if isDiffMarker(line) {
		marker := line[:1]
		rest := line[1:]
		out := []diffToken{{Text: marker, Class: diffPlain}}
		if rest != "" {
			out = append(out, chromaTokenize(lex, rest)...)
		}
		return out
	}
	return chromaTokenize(lex, line)
}

// diffTokenOpen returns the ANSI opens for a non-plain token class.
// The plain class falls through to "" here; the renderer in
// slash_viewer_panels.go is responsible for emitting the per-line
// +/- green/red around the leading marker byte.
func diffTokenOpen(class diffTokenClass) string {
	switch class {
	case diffKeyword:
		return cSyntaxKeywordOpen
	case diffString:
		return cSyntaxStringOpen
	case diffNumber:
		return cSyntaxNumberOpen
	case diffComment:
		return cSyntaxCommentOpen
	case diffType:
		return cSyntaxTypeOpen
	}
	return ""
}

// renderDiffInlineTokenStream walks a []diffToken produced by
// tokenizeDiffLine and returns the ANSI-coloured string the /diff
// file-view panel renders for one line. The leading plain token, if
// its first byte is `+` / `-`, gets the existing per-line
// green/red diff colour so the marker stays semantically green for
// "added" and red for "removed". Every non-plain token is wrapped
// with its class-specific Aurora-Dark escape. Plain tokens in the
// body are emitted verbatim — no extra foreground.
//
// The renderer is intentionally a single function shared by both
// the file-view and the future commit-view panel: splitting the
// escape emission away from the token walk keeps tokenizeDiffLine
// pure (no I/O, no terminal knowledge) and lets the panel tests
// pin each side independently.
func renderDiffInlineTokenStream(tokens []diffToken) string {
	var b strings.Builder
	for i, tok := range tokens {
		if tok.Class != diffPlain {
			b.WriteString(diffTokenOpen(tok.Class))
			b.WriteString(tok.Text)
			b.WriteString(cSyntaxTokenClose)
			continue
		}
		// Plain token. The first plain token (always the marker
		// byte emitted by tokenizeDiffLine, never anything else)
		// gets the existing per-line +/- colour so the visual
		// contract of the cycle-127 tool-step renderer carries
		// over unchanged.
		if i == 0 && len(tok.Text) > 0 {
			switch tok.Text[0] {
			case '+':
				b.WriteString(panelEffLowOpen)
				b.WriteString(tok.Text)
				b.WriteString("\x1b[39m")
				continue
			case '-':
				b.WriteString(cToolBulletErrOpen)
				b.WriteString(tok.Text)
				b.WriteString("\x1b[39m")
				continue
			}
		}
		b.WriteString(tok.Text)
	}
	return b.String()
}

// renderDiffInlineLine is the panel-side convenience: tokenise and
// render in one call. filename is the path the /diff panel is
// currently showing (used to pick the language); an empty filename
// — which the cycle commits case passes — disables language
// detection so the line is rendered plain.
func renderDiffInlineLine(filename, line string) string {
	if line == "" {
		return line
	}
	if filename == "" {
		// Empty filename signals "no language available" — fall back
		// to the existing per-marker-only colouring without any
		// syntactic walk, so commit-tree panels stay cheap and
		// unchanged for those who don't bind a filename.
		switch line[0] {
		case '+':
			return panelEffLowOpen + line + "\x1b[39m"
		case '-':
			return cToolBulletErrOpen + line + "\x1b[39m"
		}
		return line
	}
	tokens := tokenizeDiffLine(filename, line)
	return renderDiffInlineTokenStream(tokens)
}