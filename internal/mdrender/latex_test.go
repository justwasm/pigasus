package mdrender

import (
	"strings"
	"testing"
)

// renderIsolated exercises Render on src and returns the produced text.
func renderIsolated(t *testing.T, src string) string {
	t.Helper()
	got, err := Render(src)
	if err != nil {
		t.Fatalf("Render(%q) error: %v", src, err)
	}
	return got
}

// TestLaTeX_InlineRendered confirms $...$ math is Unicode-expanded inside a
// Text node: \alpha \beta \gamma → α β γ.
func TestLaTeX_InlineRendered(t *testing.T) {
	src := "Inline math $\\alpha + \\beta = \\gamma$ in a sentence."
	got := renderIsolated(t, src)
	for _, want := range []string{"α", "β", "γ"} {
		if !strings.Contains(got, want) {
			t.Errorf("inline math missing %q\nfull output:\n%s", want, got)
		}
	}
	// The dollar delimiters themselves should be gone (math was rendered).
	if strings.Contains(got, "$\\alpha") {
		t.Errorf("inline math delimiters not consumed:\n%s", got)
	}
}

// TestLaTeX_BlockRendered confirms $$...$$ is rendered as a centred-style
// block (the renderer emits it on its own indented line).
func TestLaTeX_BlockRendered(t *testing.T) {
	src := "Display math:\n\n$$\\sum_{i=1}^{n} x_i$$\n\nAfter."
	got := renderIsolated(t, src)
	// ∑ and the Unicode-converted "x" + the superscript "n" must appear.
	for _, want := range []string{"∑", "x", "ⁿ", "ᵢ"} {
		if !strings.Contains(got, want) {
			t.Errorf("block math missing %q\nfull output:\n%s", want, got)
		}
	}
	// The block delimiters $$ should be gone.
	if strings.Contains(got, "$$\\sum") {
		t.Errorf("block math delimiters not consumed:\n%s", got)
	}
}

// TestLaTeX_FencedMathBlock confirms ```math / ```latex / ```tex fenced
// blocks are routed through the LaTeX renderer.
func TestLaTeX_FencedMathBlock(t *testing.T) {
	src := "```math\n\\int_0^1 x^2 \\, dx = \\frac{1}{3}\n```\n"
	got := renderIsolated(t, src)
	// The integral sign, superscript 2, and fraction slash must appear.
	for _, want := range []string{"∫", "²", "⁄"} {
		if !strings.Contains(got, want) {
			t.Errorf("fenced math block missing %q\nfull output:\n%s", want, got)
		}
	}
	// And it must NOT have fallen through to the indented source path.
	if strings.Contains(got, "    \\int_0^1") {
		t.Errorf("fenced math block fell through to indented source:\n%s", got)
	}
}

// TestLaTeX_NonMathTextUnchanged confirms ordinary prose without dollar
// delimiters passes through verbatim — no spurious Unicode substitution.
func TestLaTeX_NonMathTextUnchanged(t *testing.T) {
	src := "This is a normal sentence with no math whatsoever."
	got := renderIsolated(t, src)
	if got != src+"\n" {
		t.Errorf("non-math text altered:\nwant: %q\ngot:  %q", src+"\n", got)
	}
}

// TestLaTeX_MalformedPassesThrough confirms unbalanced / unterminated math
// is emitted verbatim instead of munging the surrounding text.
func TestLaTeX_MalformedPassesThrough(t *testing.T) {
	// Unterminated inline math — no closing $.
	src := "Bad math $x + y stays raw"
	got := renderIsolated(t, src)
	if !strings.Contains(got, "$x + y stays raw") {
		t.Errorf("unterminated math was munged:\n%s", got)
	}
	// Unbalanced braces inside math: we should still emit something
	// reasonable rather than crashing or silently dropping bytes.
	src2 := "Bad $\\frac{1}{2 with no closing$"
	got2 := renderIsolated(t, src2)
	if !strings.Contains(got2, "Bad ") {
		t.Errorf("malformed math dropped prefix:\n%s", got2)
	}
}

// TestLaTeX_CurrencyFalsePositive checks that "$5 and $10" is not treated
// as math. The heuristic: math must contain a non-digit at the start, and
// the closing $ must be preceded by a non-space character.
func TestLaTeX_CurrencyFalsePositive(t *testing.T) {
	src := "Prices are $5 and $10 today."
	got := renderIsolated(t, src)
	if !strings.Contains(got, "$5 and $10") {
		t.Errorf("currency string was mis-rendered as math:\n%s", got)
	}
}

// TestLaTeX_GreekLetters exercises a representative slice of the Greek
// letter table — both lowercase and uppercase forms.
func TestLaTeX_GreekLetters(t *testing.T) {
	src := "$\\alpha \\beta \\Gamma \\Delta \\omega \\Omega$"
	got := renderIsolated(t, src)
	for _, want := range []string{"α", "β", "Γ", "Δ", "ω", "Ω"} {
		if !strings.Contains(got, want) {
			t.Errorf("Greek-letter expansion missing %q\nfull output:\n%s", want, got)
		}
	}
}

// TestLaTeX_Operators confirms the big-operator and binary-operator tables
// (∑, ∏, ∫, ×, ÷, ≤, ≥, ≠).
func TestLaTeX_Operators(t *testing.T) {
	src := "$\\sum \\prod \\int \\times \\div \\leq \\geq \\neq$"
	got := renderIsolated(t, src)
	for _, want := range []string{"∑", "∏", "∫", "×", "÷", "≤", "≥", "≠"} {
		if !strings.Contains(got, want) {
			t.Errorf("operator expansion missing %q\nfull output:\n%s", want, got)
		}
	}
}

// TestLaTeX_FractionAndSqrt confirms \frac{a}{b} → a⁄b and \sqrt{x} → √x.
func TestLaTeX_FractionAndSqrt(t *testing.T) {
	src := "$\\frac{1}{2}$ and $\\sqrt{2}$"
	got := renderIsolated(t, src)
	// fraction slash U+2044
	if !strings.Contains(got, "1⁄2") {
		t.Errorf("fraction not expanded:\n%s", got)
	}
	// square root U+221A followed by "2"
	if !strings.Contains(got, "√2") {
		t.Errorf("sqrt not expanded:\n%s", got)
	}
}

// TestLaTeX_SuperSubscripts checks that x^{2} becomes x² and that
// x_{i} becomes xᵢ through the fenced ```math block path (where
// goldmark does not split Text nodes on underscores). Note: goldmark's
// emphasis parser splits Text nodes on underscores inside paragraphs, so
// inline expressions like "$x_{ij}$" arrive as two fragments — see the
// limitation note in latex.go. We test the supported simple forms via
// the fenced block path here.
func TestLaTeX_SuperSubscripts(t *testing.T) {
	// Inline simple superscript — works because no underscores.
	got := renderIsolated(t, "$x^2$")
	if !strings.Contains(got, "x²") {
		t.Errorf("inline superscript not rendered:\n%s", got)
	}
	// Fenced block with superscripts AND subscripts — goldmark
	// doesn't split inside FencedCodeBlock so the whole expression
	// reaches the renderer intact.
	got2 := renderIsolated(t, "```math\nx^{2n+1}_{ij}\n```")
	for _, want := range []string{"x", "²", "ⁿ", "⁺", "¹", "ᵢ", "ⱼ"} {
		if !strings.Contains(got2, want) {
			t.Errorf("fenced super/subscript missing %q\nfull output:\n%s", want, got2)
		}
	}
}

// TestLaTeX_CodeSpanIsNotMath confirms backtick code spans are NOT routed
// through the LaTeX expander (goldmark splits them into CodeSpan nodes, so
// the literal `$x$` inside backticks should stay verbatim).
func TestLaTeX_CodeSpanIsNotMath(t *testing.T) {
	src := "Run `echo $x $y` to print."
	got := renderIsolated(t, src)
	if !strings.Contains(got, "`echo $x $y`") {
		t.Errorf("code span was mis-rendered:\n%s", got)
	}
}
