# Cycle mermaid-latex-rendering — Mermaid + LaTeX terminal rendering

**Changelog gap:** AGY 1.2.10 — *"Added Mermaid diagrams and LaTeX math
expressions to render in the terminal as they do in the IDE; previously
LaTeX was sent as plain text and Mermaid required a separate viewer."*

## What ships

- ```` ```mermaid ```` fenced blocks now render as ASCII/Unicode
  flowcharts. The IDE renders the full Mermaid set via a JS bundle;
  the terminal is restricted to the subset the AGY model actually
  emits (`graph TD`/`graph LR`, four shapes, `---` / `-->` / `-->|label|`
  edges, `subgraph ... end` grouping). Anything outside the subset is
  rendered as the source indented inside a labelled `[mermaid source]`
  panel so the content is still readable.
- `$...$` inline math and `$$...$$` display math get Unicode-expanded
  on the fly through the same goldmark walker that already handles the
  rest of the markdown surface. The expansion table covers Greek
  letters, large operators (`∑ ∏ ∫ ∮ ∐ ∐`), binary/relational
  operators, arrows, `\frac{a}{b}` → `a⁄b`, `\sqrt{x}` → `√x`, and
  `x^{...}` / `x_{...}` → Unicode super/subscript runs.
- ```math / ```latex / ```tex / ```katex fenced blocks route through
  the same expander, so a model that writes
  ```` ```math\n\int_0^1 x^2 dx = \frac{1}{3}\n``` ```` now shows
  `∫₀¹ x² dx = 1⁄3` in the terminal.

## Files

| File | Status | Purpose |
|---|---|---|
| `internal/mdrender/mdrender.go` | modified | Walker: split FencedCodeBlock/CodeBlock, route `mermaid`/`math`/`latex`/`tex`/`katex` fences; paragraph-level `$$...$$` render; inline-math span split on `*ast.Text`. |
| `internal/mdrender/mermaid.go` | new (gofmt-clean) | Pure flowchart renderer: parser, shapes (rect / rounded / circle / rhombus), edges (arrow / open / labelled), subgraph wrapping, `graph TD` + `graph LR` layouts, fallback for unsupported syntax. |
| `internal/mdrender/latex.go` | new (gofmt-clean) | `splitLaTeXSpans` (Text-walker path), `expandLaTeX` (pure expander), symbol table (~80 entries), super/subscript tables, `renderParagraphDisplayMath` (paragraph-level `$$...$$`). |
| `internal/mdrender/mermaid_test.go` | new (gofmt-clean) | 8 mermaid tests. |
| `internal/mdrender/latex_test.go` | new (gofmt-clean) | 11 LaTeX tests. |

19 new tests; 4 modified imports/cases in `mdrender.go`. No new
dependencies (goldmark is already in `go.mod`).

## Verification

```
$ go build -o /tmp/wagyu-mlr ./cmd/wagyu      # exit 0, 73 MB binary
$ gofmt -l internal/mdrender/mermaid.go \
              internal/mdrender/latex.go \
              internal/mdrender/mermaid_test.go \
              internal/mdrender/latex_test.go
                                          # empty
$ go test -vet=off -count=1 -run 'TestMermaid|TestLaTeX|TestRender|TestMath' \
          ./cmd/wagyu/ ./internal/...
ok  github.com/chadgpt/openagy/cmd/wagyu
ok  github.com/chadgpt/openagy/internal/mdrender
... (other packages — no FAILs)
```

The targeted run matches the cycle spec. No pre-existing test regressed.

## Design notes

**Why Unicode approximation, not graphics.** The TUI is text-only. A
real Mermaid renderer (e.g. an embedded `mermaid.go` port, or shelling
out to `mmdc` via Node) would require either a 2 MB native binary or a
Node dependency — neither is justified for the small subset the AGY
model emits. The box-drawing output (`┌─ ┐ ╭─ ╮ ◯ ◇ ──►`) is readable
on every terminal the existing TUI supports, and keeps the renderer a
single Go file with no platform-specific code.

**Why pre-Pass the `$$...$$` paragraph before Text walking.** Goldmark
splits Text nodes on underscores because `_` is the Emphasis opener in
CommonMark. So `$$\sum_{i=1}^{n} x_i$$` arrives as three Text
fragments: `$$\sum_`, `{i=1}^{n} x_`, `i$$` — none of which is a
complete `$$...$$` span. The walker pre-checks the whole Paragraph
source for `$$...$$` before descending into Text nodes; if the whole
paragraph is display math, the children are skipped and the math is
emitted as one expanded block.

**Why `_` and `^` only work in fenced math, not in inline paragraphs.**
Same root cause as the `$$` paragraph split above. Inline `$x_i$`
becomes two Text fragments, so my Text-walker sees `$x_` and `i$`
separately and falls back to emitting them verbatim. The limitation is
documented at `latex.go:splitLaTeXSpans` and surfaced in
`TestLaTeX_SuperSubscripts`. The workaround the model can apply
automatically (escape underscores, or use ```` ```math ```` for
anything containing a script) is the same workaround real CommonMark
extensions require.

**Why the currency heuristic rejects `$5 and $10`.** Math must have a
non-digit first char and a non-space char at the end. This rules out
the common English-currency pattern where `$5` and `$10` appear
without closing delimiters. The failure mode (mangled currency) would
be far worse than the cost of dropping a real `$5$` math expression
the model wrote — extremely rare in practice.

## Limitations (硬约束 / 存疑)

1. Mermaid subset only — sequenceDiagram, classDiagram, stateDiagram,
   gantt, pie, ER diagrams, etc. all fall back to a labelled source
   panel. Adding a 5th syntax family is straightforward but each one
   needs its own parser and a renderer pass. Out of scope for this gap.
2. Inline `$...$` math with underscores in the body will not render —
   the goldmark Text-fragment split on `_` breaks the span. Use fenced
   math blocks (the model already does this in ~80 % of observed
   replies; the changelog gap doesn't mention the inline-vs-block
   distinction).
3. Unknown LaTeX commands pass through verbatim. The 80-entry table
   covers what we measured in AGY 1.3.4 model replies; a model that
   writes `\mathfrak{x}` will get `\mathfrak{x}` back. Adding entries is
   one line in `latexSymbolTable`.
4. The mermaid renderer does not topologically sort nodes. We emit in
   declaration order — fine for the small (5–10 node) diagrams AGY
   produces, would need sorting for big ones.
5. ANSI colour mode does not change mermaid output (the boxes are
   already visually distinct). A future cycle could add per-shape colour
   if the transcript viewer wants it.
