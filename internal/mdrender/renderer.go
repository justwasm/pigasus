// renderer.go — goldmark AST visitor and node handlers.
//
// Separated from mdrender.go to keep both files modular and <= 500 lines.
// Dispatches goldmark AST nodes to dedicated walker methods with every
// function kept <= 50 lines.
package mdrender

import (
	"bytes"
	"strings"

	"github.com/yuin/goldmark/ast"

	"github.com/justwasm/pigasus/render"
)

// Walk implements the goldmark ast.Walker function signature:
// func(Node, bool) (WalkStatus, error).
func (wk *walker) Walk(n ast.Node, entering bool) (ast.WalkStatus, error) {
	switch v := n.(type) {
	case *ast.Document, *ast.TextBlock:
		return ast.WalkContinue, nil
	case *ast.Heading:
		return wk.walkHeading(v, entering)
	case *ast.Paragraph:
		return wk.walkParagraph(v, entering)
	case *ast.Text:
		return wk.walkText(v, entering)
	case *ast.String:
		return wk.walkString(v, entering)
	case *ast.CodeSpan:
		return wk.walkCodeSpan(v, entering)
	case *ast.Emphasis:
		return wk.walkEmphasis(v, entering)
	case *ast.Link:
		return wk.walkLink(v, entering)
	case *ast.AutoLink:
		return wk.walkAutoLink(v, entering)
	case *ast.List:
		return wk.walkList(v, entering)
	case *ast.ListItem:
		return wk.walkListItem(v, entering)
	case *ast.FencedCodeBlock:
		return wk.walkFencedCodeBlock(v, entering)
	case *ast.CodeBlock:
		return wk.walkCodeBlock(v, entering)
	case *ast.Blockquote:
		return wk.walkBlockquote(v, entering)
	case *ast.ThematicBreak:
		return wk.walkThematicBreak(v, entering)
	case *ast.HTMLBlock, *ast.RawHTML:
		return wk.walkHTML(entering)
	default:
		return ast.WalkContinue, nil
	}
}

// walkHeading handles *ast.Heading node entry and exit.
func (wk *walker) walkHeading(v *ast.Heading, entering bool) (ast.WalkStatus, error) {
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
	return ast.WalkContinue, nil
}

// walkParagraph handles *ast.Paragraph node entry and exit.
func (wk *walker) walkParagraph(v *ast.Paragraph, entering bool) (ast.WalkStatus, error) {
	if entering {
		wk.emitLeadingBlank()
		wk.emitBlockquotePrefix()
		// Cycle mermaid-latex-rendering: if the entire paragraph
		// is a $$...$$ display-math block, render it as a single
		// math span and skip the children.
		if rendered, ok := renderParagraphDisplayMath(wk, v); ok {
			if rendered {
				return ast.WalkSkipChildren, nil
			}
		}
	} else {
		wk.emit("\n")
		wk.firstPara = false
	}
	return ast.WalkContinue, nil
}

// walkText handles *ast.Text nodes with inline LaTeX expansion.
func (wk *walker) walkText(v *ast.Text, entering bool) (ast.WalkStatus, error) {
	if entering {
		raw := string(v.Segment.Value(wk.source))
		// Cycle mermaid-latex-rendering: split on $$...$$ and
		// $...$ so inline math gets Unicode-expanded instead of
		// passing through as raw LaTeX.
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
	return ast.WalkContinue, nil
}

// walkString handles *ast.String nodes.
func (wk *walker) walkString(v *ast.String, entering bool) (ast.WalkStatus, error) {
	if entering {
		wk.emit(string(v.Value))
	}
	return ast.WalkContinue, nil
}

// walkCodeSpan handles *ast.CodeSpan nodes.
func (wk *walker) walkCodeSpan(v *ast.CodeSpan, entering bool) (ast.WalkStatus, error) {
	wk.emit("`")
	return ast.WalkContinue, nil
}

// walkEmphasis handles *ast.Emphasis nodes.
func (wk *walker) walkEmphasis(v *ast.Emphasis, entering bool) (ast.WalkStatus, error) {
	var marker string
	kind := ansiItalic
	if v.Level == 2 {
		marker = "*"
		kind = ansiBold
	} else {
		marker = "_"
	}
	if entering {
		wk.emit(wk.ansiOpen(kind))
	} else {
		wk.emit(wk.ansiClose(kind))
	}
	if !wk.colour {
		wk.emit(marker)
	}
	return ast.WalkContinue, nil
}

// walkLink handles *ast.Link nodes with link normalization.
func (wk *walker) walkLink(v *ast.Link, entering bool) (ast.WalkStatus, error) {
	if entering {
		wk.emit(wk.ansi(ansiItalic, "["))
	} else {
		dest := render.NormalizeFileLink(string(v.Destination))
		wk.emit(wk.ansi(ansiItalic, "]("+dest+")"))
	}
	return ast.WalkContinue, nil
}

// walkAutoLink handles *ast.AutoLink nodes.
func (wk *walker) walkAutoLink(v *ast.AutoLink, entering bool) (ast.WalkStatus, error) {
	if entering {
		wk.emit("<")
		wk.emit(string(v.URL(wk.source)))
		wk.emit(">")
	}
	return ast.WalkContinue, nil
}

// walkList handles *ast.List nodes.
func (wk *walker) walkList(v *ast.List, entering bool) (ast.WalkStatus, error) {
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
	return ast.WalkContinue, nil
}

// walkListItem handles *ast.ListItem nodes.
func (wk *walker) walkListItem(v *ast.ListItem, entering bool) (ast.WalkStatus, error) {
	if entering {
		wk.emitLeadingBlank()
		for i := 1; i < wk.listDepth; i++ {
			wk.emit("  ")
		}
		if wk.listDepth > 0 && len(wk.listOrdered) >= wk.listDepth && wk.listOrdered[wk.listDepth-1] {
			wk.emit("1. ")
		} else {
			// Cycle 175: emit the U+2022 BULLET for unordered list items.
			wk.emit("\u2421 ")
		}
		wk.emitBlockquotePrefix()
	} else {
		wk.emit("\n")
	}
	return ast.WalkContinue, nil
}

// walkFencedCodeBlock handles *ast.FencedCodeBlock nodes with special routing
// for mermaid, math/latex, and standard indented code blocks.
func (wk *walker) walkFencedCodeBlock(v *ast.FencedCodeBlock, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	wk.emitLeadingBlank()
	lang := string(v.Language(wk.source))
	content := string(v.Text(wk.source))
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
		wk.emitIndentedCode(content)
		wk.firstPara = false
	}
	return ast.WalkSkipChildren, nil
}

// emitIndentedCode indents each line with four spaces and emits a trailing newline.
func (wk *walker) emitIndentedCode(content string) {
	for i := 0; i < len(content); i++ {
		if i == 0 || content[i-1] == '\n' {
			wk.emit("    ")
		}
		wk.emit(string(content[i]))
	}
	wk.emit("\n")
}

// walkCodeBlock handles *ast.CodeBlock nodes.
func (wk *walker) walkCodeBlock(v *ast.CodeBlock, entering bool) (ast.WalkStatus, error) {
	if entering {
		wk.emitLeadingBlank()
		content := string(v.Lines().Value(wk.source))
		wk.emitIndentedCode(content)
		wk.firstPara = false
		return ast.WalkSkipChildren, nil
	}
	return ast.WalkContinue, nil
}

// walkBlockquote handles *ast.Blockquote nodes.
func (wk *walker) walkBlockquote(v *ast.Blockquote, entering bool) (ast.WalkStatus, error) {
	if entering {
		wk.blockquoteN++
		wk.emitLeadingBlank()
		wk.firstPara = false
	} else {
		wk.blockquoteN--
		if wk.blockquoteN == 0 {
			wk.firstPara = false
		}
	}
	return ast.WalkContinue, nil
}

// walkThematicBreak handles *ast.ThematicBreak nodes.
func (wk *walker) walkThematicBreak(v *ast.ThematicBreak, entering bool) (ast.WalkStatus, error) {
	if entering {
		wk.emitLeadingBlank()
		wk.emit("---\n")
		wk.firstPara = false
	}
	return ast.WalkContinue, nil
}

// walkHTML handles *ast.HTMLBlock and *ast.RawHTML nodes.
func (wk *walker) walkHTML(entering bool) (ast.WalkStatus, error) {
	if entering {
		wk.emitLeadingBlank()
		wk.firstPara = false
	}
	return ast.WalkSkipChildren, nil
}
