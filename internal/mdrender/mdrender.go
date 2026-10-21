// Package mdrender turns Markdown source into a terminal-friendly text form.
//
// Cycle 168. The recovered build has no Markdown rendering (agy itself
// embeds github.com/yuin/goldmark per the binary's string table — see
// out/local-macos/strings-goldmark.txt). The two functions here are the
// minimum needed for plain text feedback panels and the transcript viewer:
//
//	Render(s)        — Markdown → text with paragraph breaks preserved,
//	                   "#" / "##" / ... heading prefixes, "- " bullets,
//	                   "    " code indentation, and "_emph_" / "*strong*"
//	                   emphasis markers.
//
//	RenderANSI(s, w) — same walker, but writes ANSI SGR escape codes
//	                   around headings (bold) and emphasis (italic / bold)
//	                   spans. The renderer writes only the 3-bit / 4-bit
//	                   codes goldmark considers unambiguous; terminals
//	                   without ANSI support ignore them.
//
// Both forms preserve the source's paragraph structure verbatim: a blank
// line in the source becomes a blank line in the output. This matters for
// the transcript viewer and any /<command> feedback body that surfaces
// model output (which frequently contains ``` fenced code blocks and
// bulleted lists).
//
// This package is a thin layer over goldmark. CommonMark compliance, link
// reference definitions, raw HTML handling, and autolinks all behave the
// way the upstream parser defines them.
package mdrender

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"

	"github.com/justwasm/pigasus/internal/render"
)

// Render turns Markdown source into terminal-friendly plain text. See
// the package comment for the conventions used.
//
// The returned string always ends with exactly one trailing newline if
// any content was produced, so callers can concatenate the result with
// other text without special-casing the empty case.
func Render(src string) (string, error) {
	if src == "" {
		return "", nil
	}
	var buf bytes.Buffer
	if err := walk(src, &buf, false); err != nil {
		return "", fmt.Errorf("mdrender: %w", err)
	}
	return trimTrailingNL(buf.String()) + "\n", nil
}

// RenderANSI writes Markdown source as text wrapped in ANSI SGR escape
// sequences to w. The escape sequences written are limited to:
//
//	\033[1m / \033[22m    bold  (h1..h6, **strong**)
//	\033[3m / \033[23m    italic (*emphasis*, link text)
//
// Terminals without ANSI support (CI runners, dumb pipes) ignore them.
func RenderANSI(src string, w io.Writer) error {
	if src == "" {
		return nil
	}
	var buf bytes.Buffer
	if err := walk(src, &buf, true); err != nil {
		return fmt.Errorf("mdrender: %w", err)
	}
	out := trimTrailingNL(buf.String()) + "\n"
	_, err := io.WriteString(w, out)
	return err
}

// walk parses src, walks the AST, and writes the resulting text to buf.
// If colourMode is true the text is decorated with ANSI SGR sequences.
func walk(src string, buf *bytes.Buffer, colourMode bool) error {
	source := []byte(src)
	md := goldmark.New()
	root := md.Parser().Parse(text.NewReader(source))

	wk := &walker{
		out:       buf,
		colour:    colourMode,
		source:    source,
		firstPara: true, // cycle 174: suppress the leading blank before the
		// first block (the comment on the field already says "true
		// until the first block-level emit happens" but the struct
		// literal forgot to set it).
	}
	// ast.Walker is a function type: func(Node, bool) (WalkStatus, error)
	return ast.Walk(root, wk.Walk)
}

// walker is the goldmark AST visitor used by both Render and RenderANSI.
//
// Goldmark calls Walk for every Node, both on entry and on exit. We use
// entry to emit block-level framing (headings, lists, code blocks,
// blockquotes) and exit to close structural prefixes.
//
// The two counters below (blockquoteN, listDepth) let us:
//   - know when to indent under a list
//   - re-emit "> " at the start of every line inside a blockquote
//   - decide whether a new block needs a blank-line separator from
//     its previous sibling at the document's top level
type walker struct {
	out         *bytes.Buffer
	colour      bool
	source      []byte // original input; needed for Text.Segment.Value
	listDepth   int
	listOrdered []bool // per-depth: ordered (true) vs unordered (false)
	blockquoteN int    // nesting depth of blockquotes
	firstPara   bool   // true until the first block-level emit happens
}

// ansiKind selects which ANSI SGR sequence the renderer emits.
type ansiKind int

const (
	ansiBold ansiKind = iota
	ansiItalic
)

// ansiOpen returns just the opening half of the bold/italic ANSI
// escape sequence ("\033[1m" or "\033[3m") when colour mode is on,
// otherwise the empty string. Used by the Emphasis walker to bracket
// the content span with the codes without wrapping the marker
// characters themselves — see cycle 174 for the rationale.
func (wk *walker) ansiOpen(kind ansiKind) string {
	if !wk.colour {
		return ""
	}
	switch kind {
	case ansiBold:
		return "\033[1m"
	case ansiItalic:
		return "\033[3m"
	}
	return ""
}

// ansiClose returns just the closing half of the bold/italic ANSI
// escape sequence ("\033[22m" or "\033[23m") when colour mode is on,
// otherwise the empty string.
func (wk *walker) ansiClose(kind ansiKind) string {
	if !wk.colour {
		return ""
	}
	switch kind {
	case ansiBold:
		return "\033[22m"
	case ansiItalic:
		return "\033[23m"
	}
	return ""
}

// ansi wraps s in bold/italic ANSI codes if colour mode is on,
// otherwise returns s verbatim. Kept for callers (Link, etc.) that
// want a one-shot wrap rather than separate open/close halves.
func (wk *walker) ansi(kind ansiKind, s string) string {
	if !wk.colour {
		return s
	}
	switch kind {
	case ansiBold:
		return "\033[1m" + s + "\033[22m"
	case ansiItalic:
		return "\033[3m" + s + "\033[23m"
	}
	return s
}

// emit writes raw bytes to the output buffer.
func (wk *walker) emit(s string) { wk.out.WriteString(s) }

// emitMermaid renders a mermaid code block body (the four-backtick
// + "mermaid" variant is the form the AGY gateway emits) into the
// output buffer. Cycle mermaid-v11-frontmatter: the AGY 1.3.6 fix
// strips a leading YAML frontmatter from mermaid blocks so the
// Mermaid v11 renderer (chat UI) sees the diagram source without the
// `--`-style thematic break, and threads the parsed metadata
// (commonly "title:", "theme:", "config:") through as a render
// hint. The TUI can't render SVG; we emit a one-line header carrying
// the frontmatter keys back to the user and a four-space-indented
// version of the cleaned diagram source, matching the regular
// fenced-block render so the surrounding transcript stays visually
// consistent.
//
// When the body has no frontmatter we behave exactly like the bare
// fenced-code-block path (4-space indent on every line). When the
// YAML is malformed the body, *including* the original frontmatter
// bytes, round-trips verbatim under the
// `[mermaid frontmatter=invalid]` header so the user is never shown
// an empty block as a side effect of the parser.
//
// The colour-mode flag doesn't change the emission; the header is a
// structural cue, not a styled run.
func (wk *walker) emitMermaid(body string) {
	yamlBody, cleaned, ok := MermaidFrontmatter(body)
	if !ok {
		// No frontmatter → just the bare indent; same shape as the
		// generic FencedCodeBlock path below.
		for i := 0; i < len(cleaned); i++ {
			if i == 0 || cleaned[i-1] == '\n' {
				wk.emit("    ")
			}
			wk.emit(string(cleaned[i]))
		}
		wk.emit("\n")
		return
	}
	fm, err := ParseMermaidFrontmatter(yamlBody)
	if err != nil {
		// Render raw. Keep the frontmatter bytes inside the
		// indented block so users see exactly what the model
		// emitted.
		wk.emit(MermaidRenderBlock(MermaidBlock{
			Source: body,
			Raw:    body,
		}))
		return
	}
	wk.emit(MermaidRenderBlock(MermaidBlock{
		Source:      cleaned,
		Raw:         body,
		Frontmatter: fm,
	}))
}

// emitLeadingBlank writes a blank line before a new top-level block
// unless this is the very first block-level node in the document or
// we are nested inside a list / blockquote that already started a
// line for us.
func (wk *walker) emitLeadingBlank() {
	if wk.firstPara || wk.blockquoteN > 0 || wk.listDepth > 0 {
		return
	}
	wk.emit("\n")
}

// emitLineBreak writes "\n" followed by one "> " per open blockquote
// level, so every line in a blockquote gets the prefix.
func (wk *walker) emitLineBreak() {
	wk.emit("\n")
	for i := 0; i < wk.blockquoteN; i++ {
		wk.emit("> ")
	}
}

// emitBlockquotePrefix writes "> " if we are about to emit the first
// character of a fresh line inside a blockquote. Used at the start of
// every Paragraph / ListItem inside a Blockquote.
func (wk *walker) emitBlockquotePrefix() {
	for i := 0; i < wk.blockquoteN; i++ {
		wk.emit("> ")
	}
}

// Walk implements the goldmark ast.Walker function signature:
// func(Node, bool) (WalkStatus, error).
func (wk *walker) Walk(n ast.Node, entering bool) (ast.WalkStatus, error) {
	switch v := n.(type) {

	case *ast.Document:
		// No framing at the document level.

	case *ast.Heading:
		if entering {
			wk.emitLeadingBlank()
			prefix := bytes.Repeat([]byte("#"), v.Level-1)
			wk.emit(string(prefix) + "# ")
			if wk.colour {
				wk.emit("\033[1m")
			}
		} else {
			if wk.colour {
				wk.emit("\033[22m")
			}
			wk.emit("\n")
			wk.firstPara = false
		}

	case *ast.Paragraph:
		if entering {
			wk.emitLeadingBlank()
			wk.emitBlockquotePrefix()
			// Cycle mermaid-latex-rendering: if the entire paragraph
			// is a $$...$$ display-math block, render it as a single
			// math span and skip the children. This is necessary
			// because goldmark splits the paragraph's Text nodes on
			// underscores (which are Emphasis openers in CommonMark),
			// so e.g. "$\sum_{i=1}$" arrives as three Text fragments
			// — but as a Paragraph it reads intact.
			if rendered, ok := renderParagraphDisplayMath(wk, v); ok {
				if rendered {
					return ast.WalkSkipChildren, nil
				}
			}
		} else {
			wk.emit("\n")
			wk.firstPara = false
		}

	case *ast.TextBlock:
		// Container for one or more Text segments (used inside list
		// items, where the parser does not produce a Paragraph). The
		// text segments themselves handle their own line breaks via
		// SoftLineBreak / HardLineBreak flags.
		return ast.WalkContinue, nil

	case *ast.Text:
		if entering {
			raw := string(v.Segment.Value(wk.source))
			// Cycle mermaid-latex-rendering: split on $$...$$ and
			// $...$ so inline math gets Unicode-expanded instead of
			// passing through as raw LaTeX. Math spans go through
			// expandLaTeX; everything else goes out verbatim.
			spans := splitLaTeXSpans(raw)
			for _, sp := range spans {
				if sp.kind == mathKindMath {
					wk.emit(expandLaTeX(sp.text))
				} else {
					wk.emit(sp.text)
				}
			}
			if v.HardLineBreak() || v.SoftLineBreak() {
				wk.emitLineBreak()
			}
		}

	case *ast.String:
		if entering {
			wk.emit(string(v.Value))
		}

	case *ast.CodeSpan:
		if entering {
			wk.emit("`")
		} else {
			wk.emit("`")
		}

	case *ast.Emphasis:
		// Cycle 174: emit the ANSI open/close *around the content*,
		// not around the marker byte. The previous implementation
		// wrapped the `*` / `_` marker itself, which left the
		// emphasised text unstyled in the rendered output.
		//
		// Cycle 175: in colour mode the marker bytes are *omitted*
		// entirely from the output. Cycle 155's hand-rolled
		// renderMarkdown emitted `ESC[1mboldESC[22m` (no markers) and
		// callers routing model replies through the new mdrender path
		// need to match that byte-for-byte. In plain-text mode the
		// markers are kept so `**bold**` still round-trips as `*bold*`
		// for non-TTY consumers (login_prompt, piped logs).
		if entering {
			var marker string
			if v.Level == 2 {
				marker = "*"
				wk.emit(wk.ansiOpen(ansiBold))
			} else {
				marker = "_"
				wk.emit(wk.ansiOpen(ansiItalic))
			}
			if !wk.colour {
				wk.emit(marker)
			}
		} else {
			var marker string
			if v.Level == 2 {
				marker = "*"
				wk.emit(wk.ansiClose(ansiBold))
			} else {
				marker = "_"
				wk.emit(wk.ansiClose(ansiItalic))
			}
			if !wk.colour {
				wk.emit(marker)
			}
		}

	case *ast.Link:
		if entering {
			wk.emit(wk.ansi(ansiItalic, "["))
		} else {
			// Cycle file-link-percent-encoded-anchor: normalise the URL so a
			// destination like `src/foo.go#L12` is rendered as
			// `src/foo.go%23L12`. The reference binary's link-formatter does
			// this rewrite (AGY 1.3.3 changelog line 59); terminals that
			// recognise the `#L<n>` fragment but reject `%23L<n>` are rare,
			// but the reverse (accept `%23L<n>`, reject `#L<n>`) is the
			// common JetBrains/VS Code case. NormaliseFileLink is a no-op
			// on already-encoded URLs, so calling it on every link is safe.
			dest := render.NormalizeFileLink(string(v.Destination))
			wk.emit(wk.ansi(ansiItalic, "]("+dest+")"))
		}

	case *ast.AutoLink:
		if entering {
			wk.emit("<")
			wk.emit(string(v.URL(wk.source)))
			wk.emit(">")
		}

	case *ast.List:
		if entering {
			wk.emitLeadingBlank()
			wk.listDepth++
			wk.listOrdered = append(wk.listOrdered, v.IsOrdered())
		} else {
			wk.listDepth--
			if len(wk.listOrdered) > 0 {
				wk.listOrdered = wk.listOrdered[:len(wk.listOrdered)-1]
			}
			if wk.listDepth == 0 {
				wk.firstPara = false
			}
		}

	case *ast.ListItem:
		if entering {
			wk.emitLeadingBlank()
			for i := 1; i < wk.listDepth; i++ {
				wk.emit("  ")
			}
			if wk.listDepth > 0 && len(wk.listOrdered) >= wk.listDepth && wk.listOrdered[wk.listDepth-1] {
				wk.emit("1. ")
			} else {
				// Cycle 175: emit the U+2022 BULLET for unordered
				// list items, matching the cycle-155 reference's
				// `- ` → `• ` rewrite. Numbered items are left
				// alone (1. / 2. / ...) as in the reference.
				wk.emit("\u2421 ")
			}
			wk.emitBlockquotePrefix()
		} else {
			wk.emit("\n")
		}

	case *ast.FencedCodeBlock:
		if entering {
			wk.emitLeadingBlank()
			lang := string(v.Language(wk.source))
			content := string(v.Text(wk.source))
// Cycle mermaid-v11-frontmatter: ```mermaid blocks get the
			// YAML-frontmatter-aware path. The bare fenced-block
			// indent below would have leaked the frontmatter bytes
			// through as a leading thematic break, which is exactly
			// the AGY 1.3.6 changelog fix.
			// Cycle mermaid-latex-rendering: route fenced blocks
			// tagged `mermaid`, `math`, or `latex` to their Unicode
			// renderers. Everything else keeps the original 4-space
			// indented behaviour.
			switch strings.ToLower(lang) {
			case "mermaid":
				fm, _ := ParseMermaidFrontmatter(content)
				wk.emit(MermaidRenderBlock(MermaidBlock{
					Source:      content,
					Frontmatter: fm,
				}))
				wk.firstPara = false
			case "math", "latex", "tex", "katex":
				renderLaTeXBlock(wk, content)
				wk.firstPara = false
			default:
				for i := 0; i < len(content); i++ {
					if i == 0 || content[i-1] == '\n' {
						wk.emit("    ")
					}
					wk.emit(string(content[i]))
				}
				wk.emit("\n")
				wk.firstPara = false
			}
			return ast.WalkSkipChildren, nil
		}

	case *ast.CodeBlock:
		if entering {
			wk.emitLeadingBlank()
			content := string(v.Lines().Value(wk.source))
			for i := 0; i < len(content); i++ {
				if i == 0 || content[i-1] == '\n' {
					wk.emit("    ")
				}
				wk.emit(string(content[i]))
			}
			wk.emit("\n")
			wk.firstPara = false
			return ast.WalkSkipChildren, nil
		}

	case *ast.Blockquote:
		if entering {
			wk.blockquoteN++
			wk.emitLeadingBlank()
			// The "> " prefix is emitted by the first child block
			// (Paragraph / ListItem) via emitBlockquotePrefix; we
			// only need to register the depth and emit the leading
			// blank line.
			wk.firstPara = false
		} else {
			wk.blockquoteN--
			if wk.blockquoteN == 0 {
				wk.firstPara = false
			}
		}

	case *ast.ThematicBreak:
		if entering {
			wk.emitLeadingBlank()
			wk.emit("---\n")
			wk.firstPara = false
		}

	case *ast.HTMLBlock, *ast.RawHTML:
		// Drop raw HTML — terminal renderers can't display it.
		if entering {
			wk.emitLeadingBlank()
			wk.firstPara = false
		}
		return ast.WalkSkipChildren, nil

	default:
		// Other nodes (custom extensions): recurse children.
	}

	return ast.WalkContinue, nil
}

// trimTrailingNL strips all trailing newlines from s.
func trimTrailingNL(s string) string {
	for len(s) > 0 && s[len(s)-1] == '\n' {
		s = s[:len(s)-1]
	}
	return s
}
