package diffsyntax

import (
	"strings"
	"testing"
)

func findClass(tokens []Token, want TokenClass) int {
	for i, t := range tokens {
		if t.Class == want {
			return i
		}
	}
	return -1
}

func allText(tokens []Token) string {
	var b []byte
	for _, t := range tokens {
		b = append(b, t.Text...)
	}
	return string(b)
}

func TestTokenizeDiffLine_Go_Keyword(t *testing.T) {
	const in = `func Foo() string { return "hi" }`
	tokens := TokenizeLine("foo.go", in)
	if idx := findClass(tokens, Keyword); idx < 0 || tokens[idx].Text != "func" {
		t.Errorf("Go keyword not flagged: tokens=%v", tokens)
	}
	if idx := findClass(tokens, String); idx < 0 || tokens[idx].Text != `"hi"` {
		t.Errorf(`Go string literal "hi" not flagged: tokens=%v`, tokens)
	}
	if got := allText(tokens); got != in {
		t.Errorf("round-trip lost bytes: got %q, want %q", got, in)
	}
}

func TestTokenizeDiffLine_Go_Comment(t *testing.T) {
	const in = `// hello func world`
	tokens := TokenizeLine("foo.go", in)
	var got string
	for _, t := range tokens {
		if t.Class != Comment {
			continue
		}
		got += t.Text
	}
	if got != in {
		t.Errorf("Go comment token wrong: got %q, want %q (tokens=%v)", got, in, tokens)
	}
	for _, tk := range tokens {
		if tk.Class == Keyword && tk.Text == "func" {
			t.Errorf("Go keyword inside comment was mis-classed as keyword: tokens=%v", tokens)
		}
	}
}

func TestTokenizeDiffLine_Python_Docstring(t *testing.T) {
	const in = `"""docstring"""`
	tokens := TokenizeLine("foo.py", in)
	if idx := findClass(tokens, String); idx < 0 || tokens[idx].Text != in {
		t.Errorf(`Python triple-quoted string not flagged: tokens=%v`, tokens)
	}
	if got := allText(tokens); got != in {
		t.Errorf("Python triple-quote round-trip lost bytes: got %q, want %q", got, in)
	}
}

func TestTokenizeDiffLine_PlusPrefix(t *testing.T) {
	const in = `+func bar() {`
	tokens := TokenizeLine("foo.go", in)
	if len(tokens) == 0 {
		t.Fatalf("no tokens emitted for %q", in)
	}
	if tokens[0].Class != Plain || tokens[0].Text != "+" {
		t.Errorf("first token must be `+` plain; got %+v", tokens[0])
	}
	if idx := findClass(tokens, Keyword); idx < 0 || tokens[idx].Text != "func" {
		t.Errorf("`+func` keyword not found: tokens=%v", tokens)
	}
}

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
			tokens := TokenizeLine("foo."+c.name, c.line)
			if len(tokens) != 1 {
				t.Fatalf("expected one token for %q, got %d: %v", c.line, len(tokens), tokens)
			}
			if tokens[0].Class != Plain {
				t.Errorf("unknown ext %s: token class = %q, want %q", c.name, tokens[0].Class, Plain)
			}
			if tokens[0].Text != c.line {
				t.Errorf("unknown ext %s: token text = %q, want %q", c.name, tokens[0].Text, c.line)
			}
		})
	}
}

func TestTokenizeDiffLine_DiffMetadataArePlain(t *testing.T) {
	lines := []string{
		"@@ -1,3 +1,3 @@ func bar() {",
		"--- a/foo.go",
		"+++ b/foo.go",
		"diff --git a/foo.go b/foo.go",
	}
	for _, line := range lines {
		t.Run(line, func(t *testing.T) {
			tokens := TokenizeLine("foo.go", line)
			if len(tokens) != 1 {
				t.Fatalf("metadata %q: expected 1 token, got %d: %v", line, len(tokens), tokens)
			}
			if tokens[0].Class != Plain {
				t.Errorf("metadata %q: class = %q, want %q", line, tokens[0].Class, Plain)
			}
			if tokens[0].Text != line {
				t.Errorf("metadata %q: text = %q, want %q", line, tokens[0].Text, line)
			}
		})
	}
}

func TestTokenizeDiffLine_RustTypeAndKeyword(t *testing.T) {
	const in = `let v: Vec<u8> = Vec::new();`
	tokens := TokenizeLine("foo.rs", in)
	if idx := findClass(tokens, Type); idx < 0 || tokens[idx].Text != "Vec" {
		t.Errorf("Rust type `Vec` not flagged: tokens=%v", tokens)
	}
	if idx := findClass(tokens, Keyword); idx < 0 || tokens[idx].Text != "let" {
		t.Errorf("Rust keyword `let` not flagged: tokens=%v", tokens)
	}
	if got := allText(tokens); got != in {
		t.Errorf("Rust round-trip lost bytes: got %q, want %q", got, in)
	}
}

func TestTokenizeDiffLine_EmptyLine(t *testing.T) {
	tokens := TokenizeLine("foo.go", "")
	if len(tokens) != 1 || tokens[0].Class != Plain || tokens[0].Text != "" {
		t.Errorf("empty input: got %v, want [{plain \"\"}]", tokens)
	}
	plain := TokenizeLine("foo.xyz", "")
	if len(plain) != 1 || plain[0].Class != Plain || plain[0].Text != "" {
		t.Errorf("empty+unknown ext: got %v, want [{plain \"\"}]", plain)
	}
}

func TestRenderDiffInlineTokenStream(t *testing.T) {
	tokens := []Token{
		{Text: "+", Class: Plain},
		{Text: "func ", Class: Plain},
		{Text: "Foo", Class: Plain},
		{Text: "() string { return ", Class: Plain},
		{Text: `"hi"`, Class: String},
		{Text: " }", Class: Plain},
	}
	got := RenderInlineTokenStream(tokens)
	for _, want := range []string{
		ColorStringOpen,
		ColorTokenClose,
		ColorMarkerAddedOpen,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("render missing %q in output: %q", want, got)
		}
	}
}

func TestTokenizeDiffLine_JSON_Numbers(t *testing.T) {
	const in = `{"a": 1, "b": [2, 3]}`
	tokens := TokenizeLine("x.json", in)
	if got := allText(tokens); got != in {
		t.Fatalf("JSON round-trip lost bytes: got %q, want %q", got, in)
	}
	if idx := findClass(tokens, Number); idx < 0 || !strings.Contains(tokens[idx].Text, "1") {
		t.Errorf("JSON number `1` not flagged: tokens=%v", tokens)
	}
	if len(tokens) <= 1 {
		t.Errorf("JSON lexer produced %d tokens; expected chroma to split: %v", len(tokens), tokens)
	}
}

func TestTokenizeDiffLine_MarkdownHeading(t *testing.T) {
	const in = `## A heading`
	tokens := TokenizeLine("x.md", in)
	if got := allText(tokens); got != in {
		t.Fatalf("markdown round-trip lost bytes: got %q, want %q", got, in)
	}
	if len(tokens) == 0 {
		t.Fatalf("markdown heading produced no tokens")
	}
}

func TestTokenizeDiffLine_BashComment(t *testing.T) {
	const in = `# echo nothing`
	tokens := TokenizeLine("x.sh", in)
	if got := allText(tokens); got != in {
		t.Fatalf("bash round-trip lost bytes: got %q, want %q", got, in)
	}
	if idx := findClass(tokens, Comment); idx < 0 {
		t.Errorf("bash `#` comment not flagged: tokens=%v", tokens)
	}
}

func TestTokenizeDiffLine_YAMLKey(t *testing.T) {
	const in = `name: foo`
	tokens := TokenizeLine("x.yaml", in)
	if got := allText(tokens); got != in {
		t.Fatalf("yaml round-trip lost bytes: got %q, want %q", got, in)
	}
}

func TestTokenizeDiffLine_NoFilenameIsPlain(t *testing.T) {
	cases := []string{
		"func bar() {",
		`return "hi"`,
		`+func bar() {`,
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			got := RenderInlineLine("", in)
			if in[0] != '+' && in[0] != '-' && got != in {
				t.Errorf("empty-filename, no marker: got %q, want %q", got, in)
			}
		})
	}
}
