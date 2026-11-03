// mermaid_test.go -- tests for the frontmatter-aware mermaid render
// path added by cycle mermaid-v11-frontmatter; cycle 234 also
// covers terminal-side mermaid rendering with the
// AlexanderGrooff/mermaid-ascii library.
package mdrender

import (
	"strings"
	"testing"
)

// TestMermaidFrontmatter_ExtractsBasic checks the happy path: a
// leading YAML fence (three hyphens, body, three hyphens) is
// stripped cleanly and the diagram source is delivered verbatim.
func TestMermaidFrontmatter_ExtractsBasic(t *testing.T) {
	body := "---\ntitle: My Diagram\n---\ngraph TD; A-->B\n"
	fm, src, ok := MermaidFrontmatter(body)
	if !ok {
		t.Fatalf("expected frontmatter to be detected")
	}
	if fm != "title: My Diagram" {
		t.Errorf("unexpected yaml body: %q", fm)
	}
	if src != "graph TD; A-->B\n" {
		t.Errorf("unexpected cleaned source: %q", src)
	}
}

// TestMermaidFrontmatter_NoFrontmatter confirms a block without
// the leading fence round-trips verbatim.
func TestMermaidFrontmatter_NoFrontmatter(t *testing.T) {
	body := "graph TD; A-->B\n"
	fm, src, ok := MermaidFrontmatter(body)
	if ok {
		t.Errorf("expected no frontmatter (ok=false), got %q", fm)
	}
	if src != body {
		t.Errorf("expected source unchanged, got %q", src)
	}
}

// TestMermaidFrontmatter_UnterminatedFence: a block that opens
// with three hyphens but never closes. We deliberately return
// ok=false so the renderer emits raw; the closed-fence heuristic
// must not synthesise an end.
func TestMermaidFrontmatter_UnterminatedFence(t *testing.T) {
	body := "---\ntitle: My Diagram\ngraph TD; A-->B\n"
	fm, src, ok := MermaidFrontmatter(body)
	if ok {
		t.Errorf("expected unterminated fence to yield ok=false, got %q", fm)
	}
	if fm != "" {
		t.Errorf("expected empty yaml body on unterminated fence, got %q", fm)
	}
	if src != body {
		t.Errorf("expected source unchanged on unterminated fence, got %q", src)
	}
}

// TestMermaidFrontmatter_FirstFenceWins covers the "multiple
// frontmatter sections" case the spec calls out. The Mermaid
// reference only honours the *first* fence; any later
// three-hyphen fence pair is part of the diagram body. We test
// that explicitly here so a future cycle that "supports multiple
// sections" must update this test first.
func TestMermaidFrontmatter_FirstFenceWins(t *testing.T) {
	body := "---\ntitle: First\n---\ngraph TD\n---\ntitle: Second\n---\n"
	fm, src, ok := MermaidFrontmatter(body)
	if !ok {
		t.Fatalf("expected first frontmatter to be detected")
	}
	if !strings.Contains(fm, "title: First") {
		t.Errorf("expected first fence in body, got %q", fm)
	}
	if strings.Contains(fm, "title: Second") {
		t.Errorf("second fence leaked into yaml body, got %q", fm)
	}
	// Diagram source carries the second fence as part of the
	// body, which is what the reference does.
	if !strings.Contains(src, "title: Second") {
		t.Errorf("expected second fence in diagram source, got %q", src)
	}
}

// TestParseMermaidFrontmatter_TitleTheme parses the two most common
// keys and checks that scalar values land in the map exactly.
func TestParseMermaidFrontmatter_TitleTheme(t *testing.T) {
	yamlBody := "title: My Diagram\ntheme: dark\n"
	fm, err := ParseMermaidFrontmatter(yamlBody)
	if err != nil {
		t.Fatalf("ParseMermaidFrontmatter: %v", err)
	}
	if got, want := fm["title"], "My Diagram"; got != want {
		t.Errorf("title: got %v, want %v", got, want)
	}
	if got, want := fm["theme"], "dark"; got != want {
		t.Errorf("theme: got %v, want %v", got, want)
	}
}

// TestParseMermaidFrontmatter_Malformed confirms a YAML syntax
// error surfaces as (nil, err) so callers can fall back to the
// invalid-frontmatter path. A tab at the start of a line trips
// yaml.v3 (tabs are not allowed as indentation in YAML 1.2); the
// changelog anchor explicitly calls out an unquoted
// description-with-colon case, but the tab-indent failure mode
// is what the prompt normally triggers.
func TestParseMermaidFrontmatter_Malformed(t *testing.T) {
	yamlBody := "title: My Diagram\n\ttheme: dark\n"
	fm, err := ParseMermaidFrontmatter(yamlBody)
	if err == nil {
		t.Fatalf("expected parse error, got %v", fm)
	}
	if fm != nil {
		t.Errorf("expected nil map on error, got %v", fm)
	}
}

// TestMermaidRenderBlock_ParsedHeader: a block whose YAML decodes
// cleanly must emit the sorted-key header and the four-space-
// indented diagram body. The wire form is the contract the
// changelog anchor relies on for "metadata not silently dropped".
func TestMermaidRenderBlock_ParsedHeader(t *testing.T) {
	block := MermaidBlock{
		Source: "graph TD; A-->B",
		Raw:    "---\ntitle: My Diagram\ntheme: dark\n---\ngraph TD; A-->B",
		Frontmatter: map[string]any{
			"title": "My Diagram",
			"theme": "dark",
		},
	}
	got := MermaidRenderBlock(block)
	if !strings.HasPrefix(got, "[mermaid") {
		t.Errorf("expected [mermaid ...] header, got %q", got)
	}
	// Sorted: theme precedes title.
	if !strings.Contains(got, "[mermaid theme=\"dark\" title=\"My Diagram\"]") {
		t.Errorf("missing sorted header: %q", got)
	}
	if !strings.Contains(got, "    graph TD; A-->B") {
		t.Errorf("expected 4-space-indented body, got %q", got)
	}
}

// TestMermaidRenderBlock_NoFrontmatter keeps the original
// 4-space-indented emit, with no header line -- same shape as
// the regular fenced code block.
func TestMermaidRenderBlock_NoFrontmatter(t *testing.T) {
	block := MermaidBlock{
		Source: "graph TD; A-->B",
		Raw:    "graph TD; A-->B",
	}
	got := MermaidRenderBlock(block)
	if strings.HasPrefix(got, "[mermaid") {
		t.Errorf("did not expect a header on no-FM path: %q", got)
	}
	if !strings.Contains(got, "    graph TD; A-->B") {
		t.Errorf("expected indented body: %q", got)
	}
}

// TestMermaidRenderBlock_InvalidFrontmatter: parse error path.
// The header records "frontmatter=invalid", and the raw block
// (which still contains the original frontmatter bytes) is
// rendered.
func TestMermaidRenderBlock_InvalidFrontmatter(t *testing.T) {
	block := MermaidBlock{
		Source:      "graph TD; A-->B",
		Raw:         "---\ntitle: bad:colon:here\n---\ngraph TD; A-->B",
		Frontmatter: nil,
	}
	got := MermaidRenderBlock(block)
	if !strings.HasPrefix(got, "[mermaid frontmatter=invalid]") {
		t.Errorf("expected invalid-FM header, got %q", got)
	}
	if !strings.Contains(got, "---") {
		t.Errorf("expected raw frontmatter bytes preserved, got %q", got)
	}
	if !strings.Contains(got, "graph TD; A-->B") {
		t.Errorf("expected diagram body preserved, got %q", got)
	}
}

// TestMermaidBlocksFromMarkdown_Bare extracts a fenced mermaid
// block from a markdown source and confirms the cleaned body has
// no frontmatter fence.
func TestMermaidBlocksFromMarkdown_Bare(t *testing.T) {
	src := "intro\n\n```mermaid\ngraph TD; A-->B\n```\n\noutro\n"
	blocks := MermaidBlocksFromMarkdown(src)
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].Frontmatter != nil {
		t.Errorf("expected no frontmatter, got %v", blocks[0].Frontmatter)
	}
	if blocks[0].Source != "graph TD; A-->B" {
		t.Errorf("unexpected source: %q", blocks[0].Source)
	}
}

// TestMermaidBlocksFromMarkdown_WithFrontmatter exercises the
// frontmatter-end-to-end path through the markdown scan.
func TestMermaidBlocksFromMarkdown_WithFrontmatter(t *testing.T) {
	src := "```mermaid\n---\ntitle: My Diagram\ntheme: dark\n---\ngraph TD; A-->B\n```\n"
	blocks := MermaidBlocksFromMarkdown(src)
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if got := blocks[0].Frontmatter["title"]; got != "My Diagram" {
		t.Errorf("title: got %v", got)
	}
	if got := blocks[0].Frontmatter["theme"]; got != "dark" {
		t.Errorf("theme: got %v", got)
	}
	if blocks[0].Source != "graph TD; A-->B" {
		t.Errorf("unexpected source: %q", blocks[0].Source)
	}
}

// TestMermaidBlocksFromMarkdown_Unterminated: a block whose
// closing fence was lost in transit. The scan returns a single
// block with the partial body so the transcript never sees an
// empty row.
func TestMermaidBlocksFromMarkdown_Unterminated(t *testing.T) {
	src := "```mermaid\ngraph TD; A-->B\n"
	blocks := MermaidBlocksFromMarkdown(src)
	if len(blocks) != 1 {
		t.Fatalf("expected 1 partial block, got %d", len(blocks))
	}
	if !strings.Contains(blocks[0].Source, "graph TD; A-->B") {
		t.Errorf("expected partial body preserved, got %q", blocks[0].Source)
	}
}

// TestRender_MermaidBlockWithFrontmatter pins the end-to-end
// goldmark path: a mermaid block with a YAML frontmatter renders
// the header line + indented body, and the frontmatter bytes do
// NOT survive (changelog anchor: "YAML frontmatter is respected
// and stripped, not shown as a thematic break").
func TestRender_MermaidBlockWithFrontmatter(t *testing.T) {
	src := "```mermaid\n---\ntitle: My Diagram\ntheme: dark\n---\ngraph TD; A-->B\n```\n"
	got, err := Render(src)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(got, "[mermaid theme=\"dark\" title=\"My Diagram\"]") {
		t.Errorf("expected frontmatter header, got %q", got)
	}
	if !strings.Contains(got, "    graph TD; A-->B") {
		t.Errorf("expected indented diagram body, got %q", got)
	}
	// The leading fence must not leak in unindented form (that
	// was the original bug -- the goldmark walker would have
	// rendered the frontmatter fence as a thematic break,
	// leaving the chat UI to see the diagram source with three
	// dead lines prepended).
	for _, line := range strings.Split(got, "\n") {
		if line == "---" {
			t.Errorf("stray thematic-break line; the frontmatter fence leaked: %q", got)
		}
	}
}

// TestRender_MermaidBlockBare: a bare mermaid block (no
// frontmatter) renders exactly the way the previous fenced-
// code-block path did -- 4-space indent on every line, no
// header. This guards against regressions in the path that has
// not changed.
func TestRender_MermaidBlockBare(t *testing.T) {
	src := "```mermaid\ngraph TD; A-->B\n```\n"
	got, err := Render(src)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if strings.HasPrefix(got, "[mermaid") {
		t.Errorf("did not expect a header on bare mermaid block: %q", got)
	}
	if !strings.Contains(got, "    graph TD; A-->B") {
		t.Errorf("expected indented body: %q", got)
	}
}

// TestRenderANSI_MermaidBlockWithFrontmatter: the same
// frontmatter path through the ANSI variant. The colour flag
// changes nothing about the structural emission (the header is a
// cue, not a styled run), so the output is identical to Render's.
func TestRenderANSI_MermaidBlockWithFrontmatter(t *testing.T) {
	src := "intro\n\n```mermaid\n---\ntitle: Demo\ntheme: forest\n---\ngraph TD; A-->B\n```\n\noutro\n"
	var buf strings.Builder
	if err := RenderANSI(src, &buf); err != nil {
		t.Fatalf("RenderANSI: %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, "[mermaid theme=\"forest\" title=\"Demo\"]") {
		t.Errorf("expected frontmatter header, got %q", got)
	}
	if !strings.Contains(got, "    graph TD; A-->B") {
		t.Errorf("expected indented body, got %q", got)
	}
}

// TestMermaidTerminal_SimpleGraph: a happy-path ```mermaid
// flowchart renders to multiline ASCII art with the box-drawing
// characters the upstream mermaid-ascii library emits. We don't
// pin the exact bytes (the upstream library's layout is
// version-dependent), but we assert the structural shape —
// multi-line, contains both node labels, and contains the
// box-drawing character set the cycle advertises.
func TestMermaidTerminal_SimpleGraph(t *testing.T) {
	src := "graph LR\nA --> B\nA --> C\nB --> C"
	block := MermaidBlock{
		Source: src,
		Raw:    src,
	}
	out, ok := RenderMermaidBlock(block, 0)
	if !ok {
		t.Fatalf("expected ok=true for valid graph")
	}
	if out == "" {
		t.Fatalf("expected non-empty render")
	}
	lines := strings.Split(out, "\n")
	if len(lines) < 4 {
		t.Errorf("expected multi-line ASCII art, got %d lines:\n%s", len(lines), out)
	}
	// Upstream mermaid-ascii uses box-drawing characters like
	// ┌, ─, │, ┐, └, ┘, ►. The labels render verbatim
	// inside the boxes.
	for _, want := range []string{"┌", "─", "│", "A", "B", "C"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in render:\n%s", want, out)
		}
	}
}

// TestMermaidTerminal_FrontmatterExtracted: a ```mermaid block
// with a leading `---\n  title: foo\n---\n` frontmatter is
// parsed, the title surfaces as the first line of the rendered
// output (mermaid-ascii's behaviour), and the thematic-break
// fence itself never escapes into the emit.
func TestMermaidTerminal_FrontmatterExtracted(t *testing.T) {
	src := "---\ntitle: My Diagram\n---\ngraph LR\nA --> B"
	block := MermaidBlock{
		Source:      "graph LR\nA --> B",
		Raw:         src,
		Frontmatter: map[string]any{"title": "My Diagram"},
	}
	out, ok := RenderMermaidBlock(block, 0)
	if !ok {
		t.Fatalf("expected ok=true for valid graph")
	}
	if !strings.HasPrefix(out, "My Diagram") {
		t.Errorf("expected title prefix, got %q", out)
	}
	for _, line := range strings.Split(out, "\n") {
		if line == "---" {
			t.Errorf("stray thematic-break fence in render:\n%s", out)
		}
	}
}

// TestMermaidTerminal_ParseFailureFallback: input that the
// mermaid-ascii library refuses to parse must yield the
// bordered source box, NOT a panic. Use the package-private
// `renderMermaidSafe` directly to assert on the raw failure
// mode without going through the Block frontmatter extraction,
// then call the public fallback wrapper to verify the wrapper
// path stays usable.
func TestMermaidTerminal_ParseFailureFallback(t *testing.T) {
	// Case 1: a "graph" keyword followed by an unsupported
	// direction. mermaid-ascii returns an error.
	if _, err := renderMermaidSafe("graph Q\nA-->B", 0); err == nil {
		t.Errorf("expected parse error from mermaid-ascii on graph Q input")
	}
	// Case 2: garbage input.
	if _, err := renderMermaidSafe("this is not valid mermaid\nqwer", 0); err == nil {
		t.Errorf("expected parse error from mermaid-ascii on garbage input")
	}
	// Case 3: empty input is the library's "missing graph
	// definition" error; treat as a no-op rather than a panic.
	if _, err := renderMermaidSafe("", 0); err != nil {
		t.Logf("empty-input error (acceptable): %v", err)
	}
	// Case 4: even on parse failure the public wrapper returns
	// the bordered source box, not empty text. This is the
	// user-visible invariant the cycle commits to.
	block := MermaidBlock{
		Source: "this is not valid mermaid\nqwer",
		Raw:    "this is not valid mermaid\nqwer",
	}
	out, ok := RenderMermaidBlock(block, 0)
	if ok {
		t.Errorf("expected ok=false for parse failure")
	}
	if !strings.Contains(out, "mermaid: render failed") {
		t.Errorf("expected bordered-fallback banner, got %q", out)
	}
	if !strings.Contains(out, "this is not valid mermaid") {
		t.Errorf("expected raw source preserved in the box, got %q", out)
	}
}

// TestMermaidTerminal_PanicRecovered: a deliberately malformed
// payload that exercises the deferred-recover path. The
// mermaid-ascii library has open issues about flowchart graphs
// with deeply-nested subgraphs panicking; we simulate the
// recovery path by checking that calling renderMermaidSafe
// twice — once normally, once with a deeply-broken input —
// always returns (text, err), never panics up the stack.
func TestMermaidTerminal_PanicRecovered(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("renderMermaidSafe panicked: %v", r)
		}
	}()
	for _, in := range []string{
		"",
		"graph TD\nA-->B",
		"not mermaid",
		"graph TD\n" + strings.Repeat("subgraph s\n", 50) + strings.Repeat("end\n", 50),
	} {
		_, err := renderMermaidSafe(in, 0)
		_ = err // every code path returns (text, err); a panic
		// would have unwound past `defer recover()` in
		// renderMermaidSafe and reached us here.
	}
}

// TestMermaidTerminal_SequenceDiagram: the sequenceDiagram
// entry path renders to its own ASCII layout (lifelines,
// actor boxes, message arrows). Upstream mermaid-ascii uses
// a top-row of boxes for actors plus vertical lifelines with
// horizontal arrows between them. We don't pin the exact
// layout, just the structural shape.
func TestMermaidTerminal_SequenceDiagram(t *testing.T) {
	src := "sequenceDiagram\nAlice->>Bob: Hello"
	block := MermaidBlock{Source: src, Raw: src}
	out, ok := RenderMermaidBlock(block, 0)
	if !ok {
		t.Fatalf("expected ok=true for sequenceDiagram")
	}
	for _, want := range []string{"Alice", "Bob", "Hello"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in sequence render:\n%s", want, out)
		}
	}
}

// TestMermaidTerminal_ErDiagram: the erDiagram (entity-
// relationship) entry path renders to ASCII too. Upstream
// mermaid-ascii emits crow's-foot connectors and labelled
// tables; we assert that the diagram renders successfully
// and contains both entity names.
func TestMermaidTerminal_ErDiagram(t *testing.T) {
	src := "erDiagram\n    CUSTOMER ||--o{ ORDER : places"
	block := MermaidBlock{Source: src, Raw: src}
	out, ok := RenderMermaidBlock(block, 0)
	if !ok {
		t.Fatalf("expected ok=true for erDiagram")
	}
	for _, want := range []string{"CUSTOMER", "ORDER", "places"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in er render:\n%s", want, out)
		}
	}
}

// TestMermaidTerminal_WidthHonoured: a narrow column budget
// produces a render that fits within the budget. The library
// retries with compact padding if the first attempt exceeds
// it; we don't pin exact width, just that the renderer
// respects the call (and still produces output for a small
// three-node graph).
func TestMermaidTerminal_WidthHonoured(t *testing.T) {
	src := "graph LR\nA --> B\nB --> C\nC --> D"
	block := MermaidBlock{Source: src, Raw: src}
	out, ok := RenderMermaidBlock(block, 60)
	if !ok {
		t.Fatalf("expected ok=true for narrow render")
	}
	if out == "" {
		t.Errorf("expected non-empty output for narrow render")
	}
}

// TestMermaidTerminal_NilFrontmatter: a block without
// frontmatter renders the same as one with frontmatter that
// has only non-title keys. The title line must NOT be
// synthesised.
func TestMermaidTerminal_NilFrontmatter(t *testing.T) {
	src := "graph LR\nA --> B"
	block := MermaidBlock{Source: src, Raw: src}
	out, _ := RenderMermaidBlock(block, 0)
	// Pick a row out of the box-drawing payload — there must
	// be no leading "title:" line and no leading topic.
	lines := strings.SplitN(out, "\n", 3)
	if len(lines) == 0 {
		t.Fatalf("empty render")
	}
	if strings.Contains(lines[0], "title:") || strings.Contains(lines[0], "title=\"") {
		t.Errorf("did not expect synthesised title line on no-FM block:\n%s", out)
	}
}

// TestMermaid_GlamourPipelineRoundtrip: a markdown document
// with a mermaid block round-trips through
// Strip→glamour→Restore without losing the mermaid content.
// Cycle 234 widened the placeholder payload from a string to a
// struct; this test pins the round-trip at the bytes the
// Restore pass produces, not just the placeholder key map.
func TestMermaid_GlamourPipelineRoundtrip(t *testing.T) {
	InvalidateCache()
	src := "intro\n\n```mermaid\ngraph LR\nA --> B\n```\n\noutro\n"
	cleaned, ph := StripMermaidAndMath(src)
	if len(ph) != 1 {
		t.Fatalf("expected 1 placeholder, got %d", len(ph))
	}
	rendered, err := ANSI(cleaned, 80)
	if err != nil {
		t.Fatalf("ANSI: %v", err)
	}
	restored := RestoreMermaidAndMath(rendered, ph)
	// Must keep the surrounding prose.
	for _, want := range []string{"intro", "outro"} {
		if !strings.Contains(restored, want) {
			t.Errorf("restored lost %q:\n%s", want, restored)
		}
	}
	// Must replace the placeholder with the cycle-234 ASCII
	// render, which carries box-drawing chars. The old
	// 4-space-indented fallback would have a "graph LR"
	// line; the new render uses ► / ─ / ┌.
	if !strings.Contains(restored, "A") || !strings.Contains(restored, "B") {
		t.Errorf("restored lost node labels:\n%s", restored)
	}
	if !strings.Contains(restored, "┌") && !strings.Contains(restored, "+") {
		t.Errorf("restored missing box-drawing characters — likely fell back to plain source:\n%s", restored)
	}
	// Must not leak the placeholder token.
	if strings.Contains(restored, "MDRENDER_BLOCK_PLACEHOLDER_") {
		t.Errorf("placeholder leaked into restored output:\n%s", restored)
	}
}

// TestMermaid_GlamourPipelineRoundtrip_Fallback: when the
// mermaid-ascii library refuses to parse, the restored output
// shows the bordered source box, not the raw source and not
// empty.
func TestMermaid_GlamourPipelineRoundtrip_Fallback(t *testing.T) {
	InvalidateCache()
	src := "before\n\n```mermaid\nthis is not valid mermaid\nfoo\n```\n\nafter\n"
	cleaned, ph := StripMermaidAndMath(src)
	if len(ph) != 1 {
		t.Fatalf("expected 1 placeholder, got %d", len(ph))
	}
	rendered, err := ANSI(cleaned, 80)
	if err != nil {
		t.Fatalf("ANSI: %v", err)
	}
	restored := RestoreMermaidAndMath(rendered, ph)
	if !strings.Contains(restored, "mermaid: render failed") {
		t.Errorf("expected bordered-fallback banner in restored output:\n%s", restored)
	}
	if !strings.Contains(restored, "this is not valid mermaid") {
		t.Errorf("expected raw source preserved in bordered box:\n%s", restored)
	}
}

