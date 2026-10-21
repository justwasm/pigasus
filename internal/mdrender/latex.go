// latex.go — LaTeX math source → Unicode approximation.
//
// Cycle (mermaid-latex-rendering). The terminal cannot run TeX; what the
// model wrote as `\frac{\partial f}{\partial x} = \alpha \cdot \beta^2`
// needs to be rendered readably in plain text. We expand a curated set of
// commands into their Unicode equivalents:
//
//	\alpha \beta \gamma \delta …        Greek letters
//	\sum \prod \int \oint \partial      Large operators
//	\pm \mp \times \div \cdot \circ     Binary operators
//	\leq \geq \neq \approx \equiv \sim  Relations
//	\leftarrow \rightarrow \Rightarrow  Arrows
//	\infty \partial \nabla \forall …    Misc math symbols
//	\frac{a}{b}        → a⁄b           Fraction slash (U+2044)
//	\sqrt{x}           → √x            Square root
//	x^{2n+1}           → x²ⁿ⁺¹         Superscript run (when each char has
//	                                    a Unicode superscript counterpart)
//	x_{ij}             → xᵢⱼ           Subscript run (similarly)
//
// Anything we don't expand passes through verbatim. Malformed LaTeX
// (unmatched braces, unterminated $ delimiters, control sequences without
// a known expansion) also passes through verbatim — better to show the
// raw $...$ than to silently mangle it.
//
// The splitLaTeXSpans function is what the walker calls for inline math
// inside a Text node: it finds $$...$$ blocks first (display math), then
// $...$ (inline math), and returns a slice of (kind, text) segments the
// walker emits one at a time. Anything tagged mathKindMath gets routed
// through renderLaTeX; everything else goes out verbatim.

package mdrender

import (
	"strings"

	"github.com/yuin/goldmark/ast"
)

// mathKind labels a span inside a Text node. The walker treats mathKindText
// as ordinary prose and mathKindMath as LaTeX to be expanded.
type mathKind int

const (
	mathKindText mathKind = iota
	mathKindMath
)

// mathSpan is one piece of a Text node after splitting on math delimiters.
type mathSpan struct {
	kind mathKind
	text string
}

// splitLaTeXSpans scans src for $$...$$ (display math) and $...$ (inline
// math). Math spans are returned with kind=mathKindMath; everything else
// (and any unterminated $) stays kind=mathKindText. We intentionally
// require non-empty math content and a closing $ that is preceded by a
// non-space, non-newline character — this avoids the obvious currency
// false-positives ("$5 and $10") where the lone "$5" never has a closing
// "$".
//
// IMPORTANT: the goldmark parser splits a Text node on `_` inside math
// expressions (because `_` opens an Emphasis in CommonMark), so the
// walker may see e.g. "Display math:\n\n$$\\sum_" as one Text node and
// "{i=1}^{n} x_" as another. We handle that by treating each Text
// fragment independently — fragments that don't start with a complete
// $$ are emitted verbatim. The Math block path goes through the fenced
// ```` ```math ```` block instead, which the parser never splits.
func splitLaTeXSpans(src string) []mathSpan {
	var out []mathSpan
	for len(src) > 0 {
		// Display math first ($$ ... $$).
		if strings.HasPrefix(src, "$$") {
			if span, rest, ok := takeLaTeXSpan(src, true); ok {
				out = append(out, mathSpan{kind: mathKindMath, text: span})
				src = rest
				continue
			}
			// Display math failed — fall through to emit a single
			// "$" as text and try again with the remainder. This
			// keeps the loop making forward progress on fragments
			// like "$$\sum_" (which goldmark splits out of the
			// middle of a $$...$$ block).
		}
		// Inline math ($ ... $). When takeLaTeXSpan rejects the input
		// (currency-style, unterminated, etc.) we MUST make forward
		// progress by consuming one byte, or the loop spins forever
		// on input that starts with "$" but isn't real math.
		if strings.HasPrefix(src, "$") {
			if span, rest, ok := takeLaTeXSpan(src, false); ok {
				out = append(out, mathSpan{kind: mathKindMath, text: span})
				src = rest
				continue
			}
			// Lone "$" — not math. Emit it as text and advance.
			out = append(out, mathSpan{kind: mathKindText, text: "$"})
			src = src[1:]
			continue
		}
		// No math delimiter here — emit up to the next "$".
		next := strings.IndexByte(src, '$')
		var piece string
		if next < 0 {
			piece = src
			src = ""
		} else {
			piece = src[:next]
			src = src[next:]
		}
		out = append(out, mathSpan{kind: mathKindText, text: piece})
	}
	return out
}

// takeLaTeXSpan scans from the opening delimiter for the matching closing
// delimiter. Returns the body (without the delimiters) and the rest of
// the input. ok is false if no matching close is found, in which case the
// caller treats the source as plain text.
func takeLaTeXSpan(src string, display bool) (body, rest string, ok bool) {
	delim := "$"
	skip := 1
	if display {
		delim = "$$"
		skip = 2
	}
	end := strings.Index(src[skip:], delim)
	if end < 0 {
		return "", src, false
	}
	body = src[skip : skip+end]
	// For inline math we require a non-empty body whose closing $ is
	// preceded by a non-space, non-newline char. "$ $" is not math
	// either, but "$x $" is also weird — we accept "$ x $" because
	// the open $ has whitespace after it; reject "$x " (no closing
	// non-space) too.
	if !display {
		if len(body) == 0 {
			return "", src, false
		}
		// Closing $ must not be preceded by a space.
		if body[len(body)-1] == ' ' {
			return "", src, false
		}
		// Opening $ must not be followed by a digit if the body
		// starts with one and the closing $ also starts a number
		// — i.e. "$5 and $10" should not be math. We approximate
		// this by requiring the body not to *start* with a digit.
		if len(body) > 0 && body[0] >= '0' && body[0] <= '9' {
			return "", src, false
		}
	}
	return body, src[skip+end+skip:], true
}

// renderLaTeXBlock writes src (the body of a ```math or ```latex fenced
// block) as expanded Unicode math.
func renderLaTeXBlock(wk *walker, src string) {
	wk.emit("\n    ")
	wk.emit(expandLaTeX(src))
	wk.emit("\n")
	wk.firstPara = false
}

// expandLaTeX is the pure-function form: returns the Unicode-expanded form
// of src with no I/O. Used by both the fence-block path and the inline
// Text-node path.
func expandLaTeX(src string) string {
	// Trim a single leading/trailing newline (common when the model
	// writes $$ on its own line).
	src = strings.TrimPrefix(src, "\n")
	src = strings.TrimSuffix(src, "\n")
	// First pass: expand \frac{a}{b}, \sqrt{x}, \bar{x}, \hat{x}, etc.
	src = expandLaTeXBraced(src)
	// Second pass: expand bare commands (\alpha, \sum, …).
	src = expandLaTeXCommands(src)
	// Third pass: expand x^{...} and x_{...} into Unicode super/subscript.
	src = expandLaTeXScripts(src)
	return src
}

// expandLaTeXBraced handles the braced forms first so the command-expansion
// pass never sees unbalanced braces.
func expandLaTeXBraced(src string) string {
	var out strings.Builder
	out.Grow(len(src))
	i := 0
	for i < len(src) {
		// \frac{a}{b}
		if strings.HasPrefix(src[i:], `\frac{`) {
			close := findMatchingBrace(src, i+len(`\frac{`))
			if close > 0 {
				next := close + 1
				if next < len(src) && src[next] == '{' {
					close2 := findMatchingBrace(src, next+1)
					if close2 > 0 {
						num := src[i+len(`\frac{`) : close]
						den := src[next+1 : close2]
						out.WriteString(num)
						out.WriteRune('⁄') // U+2044 FRACTION SLASH
						out.WriteString(den)
						i = close2 + 1
						continue
					}
				}
			}
		}
		// \sqrt{x} or \sqrt[n]{x}
		if strings.HasPrefix(src[i:], `\sqrt{`) {
			close := findMatchingBrace(src, i+len(`\sqrt{`))
			if close > 0 {
				out.WriteRune('√')
				out.WriteString(src[i+len(`\sqrt{`) : close])
				i = close + 1
				continue
			}
		}
		// \bar{x} / \hat{x} / \tilde{x} / \vec{x} / \dot{x} / \ddot{x}
		// all carry semantic meaning (accent over the letter) that
		// ASCII can't reproduce. We pass them through verbatim — the
		// \alpha / \sum pass later will still expand any nested
		// commands. Falling through to the byte-write at the bottom
		// of the loop handles these naturally.
		out.WriteByte(src[i])
		i++
	}
	return out.String()
}

// expandLaTeXCommands replaces known \name commands with their Unicode
// equivalents. Unknown commands pass through verbatim — better fidelity
// than guessing.
func expandLaTeXCommands(src string) string {
	return latexCommands.Replace(src)
}

// latexCommands is the lookup table used by expandLaTeXCommands. Built
// once at package init.
var latexCommands = newLaTeXReplacer()

// newLaTeXReplacer constructs a strings.Replacer with all the entries in
// latexSymbolTable. Centralised so a future cycle can add entries without
// hunting through the package.
func newLaTeXReplacer() *strings.Replacer {
	pairs := make([]string, 0, len(latexSymbolTable)*2)
	for _, e := range latexSymbolTable {
		pairs = append(pairs, e.src, e.dst)
	}
	return strings.NewReplacer(pairs...)
}

type latexSymbol struct{ src, dst string }

// latexSymbolTable is the curated command → Unicode mapping. We pick the
// subset that the AGY model actually emits; expanding all of TeX would
// be a separate package.
var latexSymbolTable = []latexSymbol{
	// Greek lowercase.
	{`\alpha`, "α"}, {`\beta`, "β"}, {`\gamma`, "γ"}, {`\delta`, "δ"},
	{`\epsilon`, "ε"}, {`\varepsilon`, "ε"}, {`\zeta`, "ζ"}, {`\eta`, "η"},
	{`\theta`, "θ"}, {`\vartheta`, "ϑ"}, {`\iota`, "ι"}, {`\kappa`, "κ"},
	{`\lambda`, "λ"}, {`\mu`, "μ"}, {`\nu`, "ν"}, {`\xi`, "ξ"},
	{`\omicron`, "ο"}, {`\pi`, "π"}, {`\varpi`, "ϖ"}, {`\rho`, "ρ"},
	{`\varrho`, "ϱ"}, {`\sigma`, "σ"}, {`\varsigma`, "ς"}, {`\tau`, "τ"},
	{`\upsilon`, "υ"}, {`\phi`, "φ"}, {`\varphi`, "ϕ"}, {`\chi`, "χ"},
	{`\psi`, "ψ"}, {`\omega`, "ω"},
	// Greek uppercase (those that differ from Latin).
	{`\Gamma`, "Γ"}, {`\Delta`, "Δ"}, {`\Theta`, "Θ"}, {`\Lambda`, "Λ"},
	{`\Xi`, "Ξ"}, {`\Pi`, "Π"}, {`\Sigma`, "Σ"}, {`\Upsilon`, "Υ"},
	{`\Phi`, "Φ"}, {`\Psi`, "Ψ"}, {`\Omega`, "Ω"},

	// Large operators.
	{`\sum`, "∑"}, {`\prod`, "∏"}, {`\coprod`, "∐"},
	{`\int`, "∫"}, {`\iint`, "∬"}, {`\iiint`, "∭"}, {`\oint`, "∮"},
	{`\bigcup`, "⋃"}, {`\bigcap`, "⋂"},

	// Binary operators.
	{`\pm`, "±"}, {`\mp`, "∓"}, {`\times`, "×"}, {`\div`, "÷"},
	{`\cdot`, "·"}, {`\circ`, "∘"}, {`\bullet`, "•"}, {`\ast`, "∗"},
	{`\oplus`, "⊕"}, {`\ominus`, "⊖"}, {`\otimes`, "⊗"}, {`\odot`, "⊙"},

	// Relations.
	{`\leq`, "≤"}, {`\le`, "≤"}, {`\geq`, "≥"}, {`\ge`, "≥"},
	{`\neq`, "≠"}, {`\ne`, "≠"}, {`\approx`, "≈"}, {`\equiv`, "≡"},
	{`\sim`, "∼"}, {`\simeq`, "≃"}, {`\cong`, "≅"}, {`\propto`, "∝"},
	{`\prec`, "≺"}, {`\succ`, "≻"}, {`\preceq`, "⪯"}, {`\succeq`, "⪰"},
	{`\subset`, "⊂"}, {`\supset`, "⊃"}, {`\subseteq`, "⊆"}, {`\supseteq`, "⊇"},
	{`\in`, "∈"}, {`\notin`, "∉"}, {`\ni`, "∋"},

	// Arrows.
	{`\leftarrow`, "←"}, {`\rightarrow`, "→"}, {`\Leftarrow`, "⇐"},
	{`\Rightarrow`, "⇒"}, {`\leftrightarrow`, "↔"}, {`\Leftrightarrow`, "⇔"},
	{`\mapsto`, "↦"}, {`\to`, "→"}, {`\gets`, "←"},

	// Misc math.
	{`\infty`, "∞"}, {`\partial`, "∂"}, {`\nabla`, "∇"},
	{`\forall`, "∀"}, {`\exists`, "∃"}, {`\nexists`, "∄"},
	{`\emptyset`, "∅"}, {`\varnothing`, "∅"},
	{`\neg`, "¬"}, {`\lnot`, "¬"}, {`\land`, "∧"}, {`\lor`, "∨"},
	{`\angle`, "∠"}, {`\perp`, "⊥"}, {`\parallel`, "∥"},
	{`\hbar`, "ℏ"}, {`\ell`, "ℓ"}, {`\Re`, "ℜ"}, {`\Im`, "ℑ"},
	{`\aleph`, "ℵ"}, {`\wp`, "℘"}, {`\mho`, "℧"},

	// Punctuation that often appears in math prose.
	{`\,`, " "}, {`\;`, " "}, {`\!`, ""}, {`\quad`, "  "}, {`\qquad`, "    "},
}

// expandLaTeXScripts turns x^{2n+1} into x²ⁿ⁺¹ and x_{ij} into xᵢⱼ. The
// bracketed content is passed through expandLaTeX recursively so a
// \frac inside a script still expands.
func expandLaTeXScripts(src string) string {
	var out strings.Builder
	out.Grow(len(src))
	for i := 0; i < len(src); {
		// Find the next "^{" or "_{" that isn't preceded by a backslash.
		if i+1 < len(src) && (src[i] == '^' || src[i] == '_') && src[i+1] == '{' {
			close := findMatchingBrace(src, i+2)
			if close > 0 {
				body := expandLaTeXScripts(expandLaTeX(src[i+2 : close]))
				if src[i] == '^' {
					out.WriteString(toSuperscript(body))
				} else {
					out.WriteString(toSubscript(body))
				}
				i = close + 1
				continue
			}
		}
		// Single-char scripts (most common): "x^2", "x_i".
		if i+1 < len(src) && (src[i] == '^' || src[i] == '_') && src[i+1] != '{' && src[i+1] != '\\' {
			body := string(src[i+1])
			if src[i] == '^' {
				out.WriteString(toSuperscript(body))
			} else {
				out.WriteString(toSubscript(body))
			}
			i += 2
			continue
		}
		out.WriteByte(src[i])
		i++
	}
	return out.String()
}

// toSuperscript maps ASCII / Greek to the Unicode superscript block. Any
// character without a superscript counterpart is left as-is, prefixed by
// the "^" so the user can still read it.
func toSuperscript(s string) string {
	var out strings.Builder
	for _, r := range s {
		if sup, ok := superscriptTable[r]; ok {
			out.WriteRune(sup)
		} else {
			out.WriteByte('^')
			out.WriteRune(r)
		}
	}
	return out.String()
}

// toSubscript maps ASCII / Greek / digits to the Unicode subscript block.
func toSubscript(s string) string {
	var out strings.Builder
	for _, r := range s {
		if sub, ok := subscriptTable[r]; ok {
			out.WriteRune(sub)
		} else {
			out.WriteByte('_')
			out.WriteRune(r)
		}
	}
	return out.String()
}

// superscriptTable is the Unicode superscript mapping. Limited to the
// characters that actually appear in math.
var superscriptTable = map[rune]rune{
	'0': '⁰', '1': '¹', '2': '²', '3': '³', '4': '⁴',
	'5': '⁵', '6': '⁶', '7': '⁷', '8': '⁸', '9': '⁹',
	'+': '⁺', '-': '⁻', '=': '⁼', '(': '⁽', ')': '⁾',
	'a': 'ᵃ', 'b': 'ᵇ', 'c': 'ᶜ', 'd': 'ᵈ', 'e': 'ᵉ',
	'f': 'ᶠ', 'g': 'ᵍ', 'h': 'ʰ', 'i': 'ⁱ', 'j': 'ʲ',
	'k': 'ᵏ', 'l': 'ˡ', 'm': 'ᵐ', 'n': 'ⁿ', 'o': 'ᵒ',
	'p': 'ᵖ', 'r': 'ʳ', 's': 'ˢ', 't': 'ᵗ', 'u': 'ᵘ',
	'v': 'ᵛ', 'w': 'ʷ', 'x': 'ˣ', 'y': 'ʸ', 'z': 'ᶻ',
	'α': 'ᵅ', 'β': 'ᵝ', 'γ': 'ᵞ', 'δ': 'ᵟ', 'θ': 'ᶿ',
	'ι': 'ᶥ', 'φ': 'ᵠ', 'χ': 'ᵡ',
}

// subscriptTable is the Unicode subscript mapping.
var subscriptTable = map[rune]rune{
	'0': '₀', '1': '₁', '2': '₂', '3': '₃', '4': '₄',
	'5': '₅', '6': '₆', '7': '₇', '8': '₈', '9': '₉',
	'+': '₊', '-': '₋', '=': '₌', '(': '₍', ')': '₎',
	'a': 'ₐ', 'e': 'ₑ', 'h': 'ₕ', 'i': 'ᵢ', 'j': 'ⱼ',
	'k': 'ₖ', 'l': 'ₗ', 'm': 'ₘ', 'n': 'ₙ', 'o': 'ₒ',
	'p': 'ₚ', 'r': 'ᵣ', 's': 'ₛ', 't': 'ₜ', 'u': 'ᵤ',
	'v': 'ᵥ', 'x': 'ₓ',
	'β': 'ᵦ', 'γ': 'ᵧ', 'ρ': 'ᵨ', 'φ': 'ᵩ', 'χ': 'ᵪ',
}

// findMatchingBrace returns the index of the '}' that matches the '{' at
// openIdx, scanning forward and respecting one level of nesting. Returns
// -1 if no match.
func findMatchingBrace(src string, openIdx int) int {
	depth := 1
	for i := openIdx; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// renderParagraphDisplayMath checks whether the entire paragraph is a
// $$...$$ display-math block and, if so, emits it expanded and returns
// (true, true). Returns (true, false) if the walker should continue
// normally (paragraph is not display math). Returning (false, _) is
// impossible — callers always get the first return value.
func renderParagraphDisplayMath(wk *walker, n *ast.Paragraph) (handled, isMath bool) {
	src := string(n.Lines().Value(wk.source))
	if !strings.HasPrefix(src, "$$") {
		return true, false
	}
	// Find the closing "$$". The heuristic is the same one the
	// inline-math span splitter uses: closing $$ must exist, body must
	// be non-empty.
	end := strings.Index(src[2:], "$$")
	if end < 0 {
		return true, false // unterminated — fall through to ordinary paragraph rendering.
	}
	body := src[2 : 2+end]
	wk.emit("\n    ")
	wk.emit(expandLaTeX(body))
	wk.emit("\n")
	wk.firstPara = false
	return true, true
}
