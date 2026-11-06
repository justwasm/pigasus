package toolsteps

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/ui/tuitools"
	"github.com/justwasm/pigasus/palette"
)

// hasSGRBold reports whether s carries a bold SGR attribute. The
// markdown renderer folds the document colour and the bold attribute
// into one SGR, so a bare ESC[1m never appears.
func hasSGRBold(s string) bool {
	for i := 0; i < len(s); {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && s[j] != 'm' {
				j++
			}
			if j < len(s) {
				for _, p := range strings.Split(s[i+2:j], ";") {
					if p == "1" {
						return true
					}
				}
				i = j + 1
				continue
			}
		}
		i++
	}
	return false
}

// TestTUITools_TruncatesLongDetail pins the recovered "<output +%d lines>"
// overflow marker.
func TestTUITools_TruncatesLongDetail(t *testing.T) {
	var lines []string
	for i := 0; i < tuitools.MaxDetailLines+7; i++ {
		lines = append(lines, "line")
	}
	got := strings.Join(SingleLines(Step{
		Name:   "run_command",
		Title:  "Ran seq",
		Detail: strings.Join(lines, "\n"),
	}, true, 0), "\n")
	if !strings.Contains(got, "<output +7 lines>") {
		t.Fatalf("overflow marker missing:\n%s", got)
	}
}

// TestTUITools_SpinnerNamesRunningTool pins the 1.4.3 running surface: the
// spinner line names the activity ("Running command...") and the transcript
// grows a provisional "● Running (…)" row that the finished step replaces.

// TestTUITools_FailedStepTitle pins the recovered "Failed" title for a
// non-zero-exit command.
//
// Cycle 157: the suffix is wrapped in cToolFailedSuffixOpen / Close
// (bold + red) and the bullet swaps to stepBulletErrOpen (red).
// The verb "Ran" stays in the cycle-157 verb palette (bold + orange).
func TestTUITools_FailedStepTitle(t *testing.T) {
	step := Step{
		Name:    "run_command",
		Title:   "Bash(false)",
		Summary: "Ran (false)",
		Detail:  "exit status 1",
		Err:     true,
	}
	collapsed := strings.Join(SingleLines(step, false, 0), "\n")
	if !strings.Contains(collapsed, "Ran") || !strings.Contains(collapsed, "false") || !strings.Contains(collapsed, "failed") {
		t.Fatalf("failed collapsed title missing:\n%s", collapsed)
	}
	if !strings.HasPrefix(collapsed, stepBulletErrOpen) {
		t.Fatalf("failed bullet should be red, got:\n%s", collapsed)
	}
	// The expanded call title stays clean, as in 1.4.3.
	expanded := strings.Join(SingleLines(step, true, 0), "\n")
	if strings.Contains(expanded, "Failed") || strings.Contains(expanded, "failed") {
		t.Fatalf("expanded title should not carry a failure word:\n%s", expanded)
	}
}

// TestTUITools_ThoughtStepFromStream pins the cycle-100 behaviour: the model's
// reasoning is folded into a leading "Thought for Ns, N tokens" step instead of
// being silently discarded, the reasoning tokens come from the gateway usage
// block, and the visible reply is still the stripped answer (never raw
//
//	thinking text).

// Cycle 127: the step bullet now ships in brand blue.
//
// Cycle 156: the step bullet is U+25CF "●" in 256-colour 29 (deep
// teal/green), not U+23FA ⏺ in brand blue. The recovery reads the
// recovered byte sequence "\x1b[38;2;133;153;0m\u25cf\x1b[39m" from the
// v1.3.6 capture diff.
func TestToolBlockLines_StepBulletIsStyled(t *testing.T) {
	s := Step{Name: "view_file", Title: "Read secret.txt"}
	lines := BlockLines([]Step{s}, false, 0)
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}
	if !strings.HasPrefix(lines[0], palette.ToolBulletOpen+"\u25cf"+palette.ToolBulletClose+" ") {
		t.Errorf("collapsed step should lead with teal ● bullet, got %q", lines[0])
	}
}

// Cycle 127: expanded detail with a diff body uses coloured +/- prefixes.
//
// Cycle 178: the fixture must contain `@@` hunk markers (or both
// `--- ` / `+++ ` file headers) for tuitools.LooksLikeDiff to fire. The
// pre-cycle-178 fixture of "+added\n-removed\n context" without
// the markers no longer triggers the diff path — that's the whole
// point of cycle 178's tightened heuristic.

// Cycle 127: expanded detail with a diff body uses coloured +/- prefixes.
//
// Cycle 178: the fixture must contain `@@` hunk markers (or both
// `--- ` / `+++ ` file headers) for tuitools.LooksLikeDiff to fire. The
// pre-cycle-178 fixture of "+added\n-removed\n context" without
// the markers no longer triggers the diff path — that's the whole
// point of cycle 178's tightened heuristic.
func TestStepDetailLines_ColoredDiff(t *testing.T) {
	s := Step{
		Name:   "replace_file_content",
		Title:  "Edit a.go",
		Detail: "@@ -1,3 +1,3 @@\n+func NewFunc() {}\n-oldFunc()\n context",
	}
	lines := stepDetailLines(s, 0)
	if len(lines) != 4 {
		t.Fatalf("expected 4 lines (header + 3 body), got %d (%v)", len(lines), lines)
	}
	if !strings.Contains(lines[0], palette.DetailBulletOpen+"  \u23bf  ") {
		t.Errorf("first detail line should lead with the dim \u23bf prefix, got %q", lines[0])
	}
	if !strings.Contains(lines[1], palette.ToolAddOpen+"func NewFunc() {}"+palette.ToolAddClose) {
		t.Errorf("+ diff body should be green, got %q", lines[1])
	}
	if !strings.Contains(lines[2], palette.ToolRemoveOpen+"oldFunc()"+palette.ToolRemoveClose) {
		t.Errorf("- diff body should be red, got %q", lines[2])
	}
}

// Cycle 176: tuitools.LooksLikeMarkdown covers the unambiguous Markdown
// markers (paired-asterisk strong, fenced code, ATX heading,
// inline-link `[...](...)`). Cases that must NOT fire are also
// pinned: empty input, plain text, diff-only content (no Markdown
// markers).
//
// Cycle 178: `- ` and `* ` at line start now DO fire (cycle-178
// widening — the diff side has been tightened in lockstep, so
// the collision that kept cycle-176 from firing these is gone).
// The non-fire cases below are now: plain text, diff-only
// (without diff markers — tuitools.LooksLikeDiff handles those), and
// shell-output fragments that happen to start with `-`.

// Cycle 178: a `cat .gitignore`-style payload (just `- pattern`
// rows, no other markers) does NOT trip the diff path. The
// spurious cycle-127 red colouring that cycle 178 documented as
// "a pre-existing quirk" is gone. The output is byte-identical
// to plain-text tool output: `- pattern` rows render verbatim,
// with no ANSI colouring.
func TestStepDetailLines_NoSpuriousDiffColoringOnGitignore(t *testing.T) {
	s := Step{
		Name:   "read_url_content",
		Title:  "Read .gitignore",
		Detail: "node_modules/\n*.log\n.env\n",
	}
	lines := stepDetailLines(s, 0)
	joined := strings.Join(lines, "\n")
	if strings.Contains(joined, palette.ToolRemoveOpen) || strings.Contains(joined, palette.ToolAddOpen) {
		t.Errorf("non-diff content should not have +/- colour escapes:\n%s", joined)
	}
	if strings.Contains(joined, palette.ToolRemoveClose) || strings.Contains(joined, palette.ToolAddClose) {
		t.Errorf("non-diff content should not have +/- close escapes:\n%s", joined)
	}
	// Sanity: the content is preserved verbatim.
	if !strings.Contains(joined, "node_modules/") {
		t.Errorf("node_modules/ missing:\n%s", joined)
	}
	if !strings.Contains(joined, "*.log") {
		t.Errorf("*.log missing:\n%s", joined)
	}
}

// Cycle 176: tool-result content that contains Markdown markers
// gets rendered through mdrender. The transcript rows show the
// bold / italic / heading styling instead of the raw markers.
//
// Cycle glamour-migration: bold spans now ship with glamour's
// closing escape (ESC[m = full style reset, vs the cycle-175
// walker which closed with ESC[22m to keep the bold attribute
// off only). The structural property — bold-on around the
// emphasised span, no raw markers surviving — is preserved.

// Cycle 176: tool-result content that contains Markdown markers
// gets rendered through mdrender. The transcript rows show the
// bold / italic / heading styling instead of the raw markers.
//
// Cycle glamour-migration: bold spans now ship with glamour's
// closing escape (ESC[m = full style reset, vs the cycle-175
// walker which closed with ESC[22m to keep the bold attribute
// off only). The structural property — bold-on around the
// emphasised span, no raw markers surviving — is preserved.
func TestStepDetailLines_RendersMarkdown(t *testing.T) {
	s := Step{
		Name:   "read_url_content",
		Title:  "Read README.md",
		Detail: "# Project\n\nThis is **important** text.\n\n- step one\n- step two\n",
	}
	lines := stepDetailLines(s, 0)
	if len(lines) == 0 {
		t.Fatal("stepDetailLines returned no rows for markdown input")
	}
	joined := strings.Join(lines, "\n")
	// Bold span: glamour emits the document colour and the bold
	// attribute in one SGR (cycle 271) and closes with ESC[m.
	if !hasSGRBold(joined) || !strings.Contains(joined, "important") {
		t.Errorf("bold span opening missing:\n%s", joined)
	}
	// Bullets are U+2022 round dots (AGY 1.4.2's Item.BlockPrefix,
	// cycle 271; the cycle-175 walker used U+2421).
	if !strings.Contains(joined, "\u2022 step one") {
		t.Errorf("bullet marker lost:\n%s", joined)
	}
	if !strings.Contains(joined, "\u2022 step two") {
		t.Errorf("second bullet marker lost:\n%s", joined)
	}
	// The raw markdown markers must NOT survive in the output.
	if strings.Contains(joined, "**important**") {
		t.Errorf("raw ** survived markdown rendering:\n%s", joined)
	}
	if strings.Contains(joined, "- step one") {
		t.Errorf("raw - survived bullet conversion:\n%s", joined)
	}
	// The detail bullet prefix is still emitted on the first row.
	if !strings.Contains(lines[0], tuitools.DetailBullet()) {
		t.Errorf("first row missing detail bullet:\n%s", lines[0])
	}
}

// Cycle 176: fenced code blocks from `cat <file>.md` get the
// 4-space indent and the ``` marker lines are stripped (cycle-127
// diff path doesn't apply because no `+` / `-` is at line start
// after mdrender runs).
//
// Cycle 233: a language-tagged fence ("```go") is now chroma-
// styled by the cycle-233 chroma Restore pass (see
// internal/mdrender/chroma_inline.go). The 4-space indent on each
// line of the rendered block survives chroma — the chroma output
// is wrapped through indentChromaBlock before insertion — so the
// shape of the result is the same as cycle 176/232, just with
// ANSI SGR escapes between tokens. The test strips ANSI before
// checking for "    func main()" to keep the structural pin.

// Cycle 176: fenced code blocks from `cat <file>.md` get the
// 4-space indent and the ``` marker lines are stripped (cycle-127
// diff path doesn't apply because no `+` / `-` is at line start
// after mdrender runs).
//
// Cycle 233: a language-tagged fence ("```go") is now chroma-
// styled by the cycle-233 chroma Restore pass (see
// internal/mdrender/chroma_inline.go). The 4-space indent on each
// line of the rendered block survives chroma — the chroma output
// is wrapped through indentChromaBlock before insertion — so the
// shape of the result is the same as cycle 176/232, just with
// ANSI SGR escapes between tokens. The test strips ANSI before
// checking for "    func main()" to keep the structural pin.
func TestStepDetailLines_FencedCodeInMarkdown(t *testing.T) {
	s := Step{
		Name:   "read_url_content",
		Title:  "Read docs.md",
		Detail: "before\n```go\nfunc main() {}\n```\nafter",
	}
	lines := stepDetailLines(s, 0)
	joined := strings.Join(lines, "\n")
	// Cycle 233: chroma wraps each token in ANSI escapes, so a
	// direct substring check for "    func main()" fails (the
	// ANSI codes break the literal sequence). Strip ANSI before
	// checking.
	plain := stripAnsiForTest(joined)
	if !strings.Contains(plain, "    func main()") {
		t.Errorf("code block not indented to 4 spaces:\n%s", joined)
	}
	if strings.Contains(joined, "```") {
		t.Errorf("fence marker survived:\n%s", joined)
	}
	if !strings.Contains(joined, "before") || !strings.Contains(joined, "after") {
		t.Errorf("surrounding prose lost:\n%s", joined)
	}
}

// stripAnsiForTest removes ANSI SGR sequences from s. Cycle 233
// added this helper because the cycle-176 step-detail code-fence
// test now has chroma in the path -- the chroma terminal16m
// formatter wraps each token in 24-bit RGB ANSI escapes, breaking
// literal substring matching for "    func main()".

// Cycle 176: plain-text tool output (no Markdown markers) is
// unchanged from cycle-127 — the heuristic returns false and the
// diff-style pass still applies to `+` / `-` lines. Cycle 178 also
// widened the first-line spacing from one space after the bullet to
// two ("  ⎿  content" vs the cycle-127 "  ⎿ content") so the
// recovered reference layout matches.
func TestStepDetailLines_PlainTextUnchanged(t *testing.T) {
	s := Step{
		Name:   "run_command",
		Title:  "Ran cat /etc/hostname",
		Detail: "myhost.local\n",
	}
	lines := stepDetailLines(s, 0)
	if len(lines) != 1 {
		t.Fatalf("expected 1 line for single-line plain text, got %d", len(lines))
	}
	want := tuitools.DetailBullet() + "myhost.local"
	if lines[0] != want {
		t.Errorf("plain-text detail byte mismatch:\n  got: %q\n want: %q", lines[0], want)
	}
}

// Cycle 176: an empty / whitespace-only detail returns nil, same
// as cycle-127 (no Markdown rendering happens because the
// stepDetailLines early-return fires before the heuristic).

// Cycle 176: an empty / whitespace-only detail returns nil, same
// as cycle-127 (no Markdown rendering happens because the
// stepDetailLines early-return fires before the heuristic).
func TestStepDetailLines_EmptyPassthrough(t *testing.T) {
	for _, body := range []string{"", "   ", "\n\n", " \t\n"} {
		s := Step{Name: "x", Title: "y", Detail: body}
		if got := stepDetailLines(s, 0); got != nil {
			t.Errorf("stepDetailLines(detail=%q) = %v, want nil", body, got)
		}
	}
}

// Cycle 178: bullet-only Markdown (e.g. `cat .gitignore` payload,
// or `cat TODO`) now trips the cycle-178-widened tuitools.LooksLikeMarkdown
// heuristic (because `- ` at line start is now a Markdown
// marker). The body is rendered through mdrender so `- foo`
// becomes `␡ foo`, and the cycle-127 diff colouring does NOT run
// (tuitools.LooksLikeDiff is false — no `@@` or `---`/`+++`).
//
// This is the deliberate cycle-178 trade: the spurious red colouring
// is gone (correct), and the bullet rows render as round-dot
// Markdown bullets (also correct for any payload that genuinely is
// a bulleted list).

// Cycle 178: bullet-only Markdown (e.g. `cat .gitignore` payload,
// or `cat TODO`) now trips the cycle-178-widened tuitools.LooksLikeMarkdown
// heuristic (because `- ` at line start is now a Markdown
// marker). The body is rendered through mdrender so `- foo`
// becomes `␡ foo`, and the cycle-127 diff colouring does NOT run
// (tuitools.LooksLikeDiff is false — no `@@` or `---`/`+++`).
//
// This is the deliberate cycle-178 trade: the spurious red colouring
// is gone (correct), and the bullet rows render as round-dot
// Markdown bullets (also correct for any payload that genuinely is
// a bulleted list).
func TestStepDetailLines_BulletOnlyRendersAsMarkdown(t *testing.T) {
	s := Step{
		Name:   "read_url_content",
		Title:  "Read .gitignore",
		Detail: "- foo\n- bar\n",
	}
	lines := stepDetailLines(s, 0)
	// Cycle glamour-migration: glamour wraps the source with a
	// leading blank line ("\n\n" before the first bullet) that
	// the legacy walker did not. The two real bullet rows are
	// at indices 1..2; the leading blank or whitespace row may
	// also be present. Pin >= 2 lines and verify the bullets
	// are present somewhere in the rendered set, rather than
	// asserting a strict count.
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 lines, got %d (%v)", len(lines), lines)
	}
	joined := strings.Join(lines, "\n")
	// The `- foo` line must have been converted to `• foo` by
	// the glamour Item.BlockPrefix config (cycle 271 marker).
	if !strings.Contains(joined, "\u2022 foo") {
		t.Errorf("bullet-only line should now get Markdown bullet conversion:\n%s", joined)
	}
	// The cycle-127 red diff colour must NOT survive anywhere
	// in the rendered set — that was the cycle-127 collision
	// bug cycle 178 fixed.
	if strings.Contains(joined, palette.ToolRemoveOpen) {
		t.Errorf("bullet-only line should NOT get cycle-127 red diff colour (cycle-178 trade):\n%s", joined)
	}
}

// Cycle 176: real `replace_file_content` diff output (with
// hunk / file headers) does NOT trip the Markdown heuristic —
// `@@` is not a Markdown marker, `---` / `+++` files are not
// Markdown markers, and the diff body has no `**` / ``` /
// `# ` / `[...](...)` content. The cycle-127 path renders the
// +/- colour and the user's snapshot expectations continue to
// hold for real diffs.

// Cycle 176: real `replace_file_content` diff output (with
// hunk / file headers) does NOT trip the Markdown heuristic —
// `@@` is not a Markdown marker, `---` / `+++` files are not
// Markdown markers, and the diff body has no `**` / ``` /
// `# ` / `[...](...)` content. The cycle-127 path renders the
// +/- colour and the user's snapshot expectations continue to
// hold for real diffs.
func TestStepDetailLines_RealDiffUnchanged(t *testing.T) {
	s := Step{
		Name:   "replace_file_content",
		Title:  "Edit foo.go",
		Detail: "--- a/foo.go\n+++ b/foo.go\n@@ -1,3 +1,3 @@\n-foo\n+bar\n context\n",
	}
	lines := stepDetailLines(s, 0)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, palette.ToolAddOpen+"bar"+palette.ToolAddClose) {
		t.Errorf("real diff +line lost green colour:\n%s", joined)
	}
	if !strings.Contains(joined, palette.ToolRemoveOpen+"foo"+palette.ToolRemoveClose) {
		t.Errorf("real diff -line lost red colour:\n%s", joined)
	}
	// The raw diff syntax must NOT be mdrender'd — `--- ` /
	// `+++ ` / `@@` are not Markdown markers.
	if strings.Contains(joined, "\u2421 a/foo.go") {
		t.Errorf("diff header got bullet conversion (regression):\n%s", joined)
	}
}

// TestSpinnerText_MatchesReferenceFrames pins the reference's animated
// activity label. Each frame below is a (base, highlight) split measured
// from a truecolour pty capture of agy 1.4.3 reading a file:
//
//	R|eadin  Re|ading  Rea|ding    …  Reading| file  Reading |file.
//	Reading f|ile..  Reading fi|le...  Reading file  Reading file.  …
//
// ("|" marks the #268BD2 highlight boundary and is not emitted.)

// TestNewToolStep_EditDiffFromFile pins the recovery path: the call has
// already run when the step is built, so the hunk is reconstructed from the
// post-edit file on disk plus the Target/Replacement arguments.
func TestNewToolStep_EditDiffFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\nthree\n"), 0644); err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(map[string]string{
		"Path":        path,
		"Target":      "two",
		"Replacement": "TWO",
	})
	s := NewToolStep("edit_file", args, "Successfully edited", false)
	if s.Diff == nil {
		t.Fatal("edit_file step has no diff")
	}
	if s.Diff.Added != 1 || s.Diff.Removed != 1 {
		t.Errorf("counts = +%d/-%d, want +1/-1", s.Diff.Added, s.Diff.Removed)
	}
	lines := stepDetailLines(s, 0)
	if len(lines) == 0 || !strings.Contains(lines[0], "+1") {
		t.Fatalf("detail rows = %q, want a +1 header", lines)
	}
	if s.Title != "Edit("+path+")" {
		t.Errorf("title = %q", s.Title)
	}
}

// TestReadFile_OneLineHasNoDetail pins the 1.4.3 rule that a single-line
// read renders no "⎿" row at all; the hint moves onto the call head.

// TestReadFile_OneLineHasNoDetail pins the 1.4.3 rule that a single-line
// read renders no "⎿" row at all; the hint moves onto the call head.
func TestReadFile_OneLineHasNoDetail(t *testing.T) {
	one := NewToolStep("read_file", json.RawMessage(`{"Path":"/tmp/x"}`), "lonely\n", false)
	if got := stepDetailText(one); got != "" {
		t.Errorf("one-line read detail = %q, want none", got)
	}
	if one.Title != "Read(/tmp/x)" || one.Summary != "Explored 1 file (x)" {
		t.Errorf("read forms = title %q summary %q", one.Title, one.Summary)
	}
	two := NewToolStep("read_file", json.RawMessage(`{"Path":"/tmp/x"}`), "a\nb\n", false)
	if got := stepDetailText(two); got != "Read 2 lines" {
		t.Errorf("two-line read detail = %q, want %q", got, "Read 2 lines")
	}
}

// TestGroupLines_PerCategoryRows pins the mixed-batch shape: one summary row
// per category, in first-appearance order, then (expanded only) each call.

// TestGroupLines_PerCategoryRows pins the mixed-batch shape: one summary row
// per category, in first-appearance order, then (expanded only) each call.
func TestGroupLines_PerCategoryRows(t *testing.T) {
	steps := []Step{
		{Kind: KindTool, Name: "read_file", Title: "Read(/tmp/a)", Summary: "Explored 1 file (a)", Detail: "one\ntwo\n"},
		{Kind: KindTool, Name: "edit_file", Title: "Edit(/tmp/a)", Summary: "Edited 1 file (a)", Detail: "ok"},
	}
	collapsed := groupLines(steps, false, 0)
	if len(collapsed) != 2 {
		t.Fatalf("collapsed rows = %d (%q), want 2", len(collapsed), collapsed)
	}
	if !strings.Contains(collapsed[0], "Explored") || !strings.Contains(collapsed[0], "1 file") {
		t.Errorf("first summary = %q", collapsed[0])
	}
	if !strings.Contains(collapsed[1], "Edited") || !strings.Contains(collapsed[1], "1 file") {
		t.Errorf("second summary = %q", collapsed[1])
	}
	expanded := groupLines(steps, true, 0)
	if len(expanded) < 4 {
		t.Fatalf("expanded rows = %d (%q), want the summaries plus both calls", len(expanded), expanded)
	}
	if !strings.Contains(expanded[0], "Explored") || !strings.Contains(expanded[1], "Edited") {
		t.Errorf("expanded summaries = %q", expanded[:2])
	}
	joined := strings.Join(expanded, "\n")
	plain := stripAnsiForTest(joined)
	if !strings.Contains(plain, "Read(/tmp/a)") || !strings.Contains(plain, "Edit(/tmp/a)") {
		t.Errorf("expanded calls missing:\n%s", joined)
	}
}

// TestSearchWeb_ReferenceForms pins the search step forms measured on 1.4.3
// (ref_search2.bin):
//
//	● Exploring 1 search (golang) (ctrl+o to expand)   ← in flight, amber bullet
//	● Explored 1 search (golang) (ctrl+o to expand)    ← finished
//	● WebSearch(golang)                                ← expanded
//	⣿  Searching web...                                ← spinner phrase

// TestFailedNonCommand_NoFailedWord pins the difference between a failed
// command ("… failed", red bullet) and a failed search (red bullet only).
func TestFailedNonCommand_NoFailedWord(t *testing.T) {
	search := Step{Name: "search_web", Title: "WebSearch(x)", Summary: "Explored 1 search (x)", Err: true}
	line := strings.Join(stepLines(search, false, 0), "\n")
	if strings.Contains(line, "failed") {
		t.Errorf("failed search should not print the word: %q", line)
	}
	if !strings.HasPrefix(line, palette.ToolBulletErrOpen) {
		t.Errorf("failed search bullet should be red: %q", line)
	}
	cmd := Step{Name: "run_command", Title: "Bash(false)", Summary: "Ran (false)", Err: true}
	if got := strings.Join(stepLines(cmd, false, 0), "\n"); !strings.Contains(got, "failed") {
		t.Errorf("failed command lost the word: %q", got)
	}
}

// TestEditDiff_141_SyntaxHighlightAndStripedGutter pins the inline diff
// preview's row colours.
//
// The AGY 1.4.1 changelog line is the promise:
// "Improved the inline diff preview when the agent asks to edit or create a
// file so it renders like the /diff file view, with syntax highlighting for
// your color scheme, background tints on added and removed lines (picked from
// your terminal's light or dark background when using the Terminal color scheme),
// and an untinted line-number gutter."
//
// 【一手】 the *shape* of that promise is settled by agy 1.4.3's captured
// diff (docs/cycles/cycle-285-tool-display/deliverable.md), and the
// capture supersedes the changelog's "untinted gutter": the changed rows
// carry the reference's stripe — the line number in the line-number grey
// on the row's background — and the body in the add or the remove colour:
//
//	\x1b[6G\x1b[38;2;131;148;150;48;2;238;232;213m   3\x1b[38;2;220;50;47m -  gamma\x1b[K\x1b[m
//
// 【strings】 the syntax highlighting is the 1.4.1 changelog's promise
// (internal/cli/testdata/changelog_reference.txt:88).

// TestCreateFile_InlineDiffPreview verifies that file-creation tools
// (write_to_file / create_file) produce an addition-only inline diff preview.
func TestCreateFile_InlineDiffPreview(t *testing.T) {
	args := json.RawMessage(`{
		"path": "test.go",
		"content": "package main\n\nfunc main() {}\n"
	}`)
	step := NewToolStep("write_to_file", args, "Successfully wrote file", false)
	if step.Diff == nil {
		t.Fatal("expected write_to_file step to generate diff preview, got nil")
	}
	if step.Diff.Added != 3 || step.Diff.Removed != 0 {
		t.Errorf("got added=%d removed=%d, want added=3 removed=0", step.Diff.Added, step.Diff.Removed)
	}
	if step.Diff.Path != "test.go" {
		t.Errorf("diff path = %q, want 'test.go'", step.Diff.Path)
	}

	lines := step.Diff.Lines(80)
	if len(lines) != 5 { // 1 header + 3 added lines + 1 trailing newline context
		t.Fatalf("expected 5 lines, got %d: %q", len(lines), lines)
	}
	// 【反编译】 the reference summarises a change that only adds lines
	// as "Added %d line%s" (0x1034b43b4) and one that only removes lines
	// as "Removed %d line%s" (0x1034dcd2d); the "+%d / -%d lines" pair
	// is the third form.
	if !strings.Contains(lines[0], "Added 3 lines") {
		t.Errorf("header = %q, want \"Added 3 lines\"", lines[0])
	}
	for i, l := range lines[1:4] {
		if !strings.HasPrefix(l, "     "+palette.DiffStripeOpen) {
			t.Errorf("row %d missing the stripe gutter: %q", i+1, l)
		}
		if !strings.Contains(l, " +  ") {
			t.Errorf("row %d missing diff add marker: %q", i+1, l)
		}
	}
}

// runCommandTitleArgs is the JSON arguments a tool call carrying a
// `false` command exit.

// TestStepTitle_NonZeroExit_HasFailedLabel pins the 1.4.3 failure surface:
// the collapsed summary carries a lowercase " failed" suffix and the red
// bullet, while the expanded call title stays `Bash(false)` with the exit
// code moved into the detail body.
func TestStepTitle_NonZeroExit_HasFailedLabel(t *testing.T) {
	step := NewToolStep("run_command", json.RawMessage(runCommandTitleArgs),
		runCommandResult("1", ""), false)
	if !step.Err {
		t.Fatalf("non-zero exit did not mark the step failed")
	}

	// (a) collapsed: red bullet + " failed" after the args.
	collapsed := strings.Join(SingleLines(step, false, 0), "\n")
	if !strings.HasPrefix(collapsed, stepBulletErrOpen) {
		t.Fatalf("expected red error bullet, got %q", collapsed)
	}
	if !strings.Contains(collapsed, "Ran") || !strings.Contains(collapsed, "(false)") {
		t.Fatalf("collapsed summary missing args: %q", collapsed)
	}
	if !strings.Contains(collapsed, "failed") || strings.Contains(collapsed, "(Failed)") {
		t.Fatalf("collapsed failure word = %q; want lowercase \"failed\"", collapsed)
	}

	// (b) expanded: clean title, exit code in the detail body.
	expanded := strings.Join(SingleLines(step, true, 0), "\n")
	if !strings.Contains(expanded, "Bash") || !strings.Contains(expanded, "(false)") {
		t.Fatalf("expanded title = %q; want Bash(false)", expanded)
	}
	if strings.Contains(expanded, "failed") || strings.Contains(expanded, "(Failed)") {
		t.Fatalf("expanded title should not carry a failure word:\n%s", expanded)
	}
	if !strings.Contains(expanded, "Command exited with code 1.") {
		t.Fatalf("expanded detail missing exit line:\n%s", expanded)
	}
	if !strings.Contains(expanded, "No output from the command.") {
		t.Fatalf("expanded detail missing empty-output line:\n%s", expanded)
	}
}

// TestStepTitle_ZeroExit_NoFailedLabel pins the happy path: teal bullet,
// no failure word, and the output body in the reference's "Command output:"
// shape.

// TestStepTitle_ZeroExit_NoFailedLabel pins the happy path: teal bullet,
// no failure word, and the output body in the reference's "Command output:"
// shape.
func TestStepTitle_ZeroExit_NoFailedLabel(t *testing.T) {
	step := NewToolStep("run_command", json.RawMessage(runCommandOKArgs),
		runCommandResult("0", "one"), false)
	if step.Err {
		t.Fatalf("zero exit marked the step failed")
	}

	collapsed := strings.Join(SingleLines(step, false, 0), "\n")
	if !strings.HasPrefix(collapsed, stepBulletOpen) {
		t.Fatalf("zero-exit should render with the teal success bullet, got %q", collapsed)
	}
	if strings.Contains(collapsed, "failed") {
		t.Fatalf("zero-exit leaked a failure word: %q", collapsed)
	}

	expanded := strings.Join(SingleLines(step, true, 0), "\n")
	if !strings.Contains(expanded, "Command exited with code 0.") ||
		!strings.Contains(expanded, "Command output:") ||
		!strings.Contains(expanded, "one ") {
		t.Fatalf("expanded detail = %q; want exit line + Command output body", expanded)
	}
	// The raw result's timestamps are model-facing only.
	if strings.Contains(expanded, "Created At") {
		t.Fatalf("expanded detail leaked the raw result timestamps:\n%s", expanded)
	}
}

func TestToolStep_WriteFile_LinesIncluded(t *testing.T) {
	cases := []struct {
		name      string
		args      string
		wantTitle string
		wantBody  string
	}{
		{"write_to_file no content", `{"path":"out.txt"}`, "Create(out.txt)", ""},
		{"write_to_file empty content", `{"path":"out.txt","content":""}`, "Create(out.txt)", ""},
		{"write_to_file one line", `{"path":"out.txt","content":"hello"}`, "Create(out.txt)", "Wrote 1 line"},
		{"write_to_file one line terminated", `{"path":"out.txt","content":"hello\n"}`, "Create(out.txt)", "Wrote 1 line"},
		{"write_to_file five lines", `{"path":"a.go","content":"1\n2\n3\n4\n5"}`, "Create(a.go)", "Wrote 5 lines"},
		{"write_to_file three lines terminated", `{"path":"out.txt","content":"a\nb\nc\n"}`, "Create(out.txt)", "Wrote 3 lines"},
		{"write_file singular", `{"absolute_path":"x.go","content":"hi"}`, "Create(x.go)", "Wrote 1 line"},
		{"create_file plural", `{"path":"y.go","content":"a\nb\n"}`, "Create(y.go)", "Wrote 2 lines"},
		{"write_to_file capitalized content key", `{"TargetFile":"z.go","Content":"single"}`, "Create(z.go)", "Wrote 1 line"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			step := NewToolStep("write_to_file", json.RawMessage(c.args), "", false)
			if step.Title != c.wantTitle {
				t.Errorf("title = %q, want %q", step.Title, c.wantTitle)
			}
			if got := stepDetailText(step); got != c.wantBody {
				t.Errorf("detail = %q, want %q", got, c.wantBody)
			}
		})
	}
}

// stripAnsiForTest removes ANSI SGR sequences from s. Cycle 233
// added this helper because the cycle-176 step-detail code-fence
// test now has chroma in the path -- the chroma terminal16m
// formatter wraps each token in 24-bit RGB ANSI escapes, breaking
// literal substring matching for "    func main()".
func stripAnsiForTest(s string) string {
	var b strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			// skip until 'm' (SGR terminator)
			j := i + 2
			for j < len(s) && s[j] != 'm' {
				j++
			}
			if j < len(s) {
				i = j + 1
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

const runCommandTitleArgs = `{"CommandLine":"false"}`
const runCommandOKArgs = `{"CommandLine":"true"}`

func runCommandResult(code string, output string) string {
	return "Created At: 2026-10-20T00:00:00+08:00\nCompleted At: 2026-10-20T00:00:01+08:00\n\n" +
		"The command exited with code " + code + ".\nOutput:\n" + output + "\n"
}

func TestFormatTokens(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{0, "0"},
		{450, "450"},
		{999, "999"},
		{1000, "1.0k"},
		{1200, "1.2k"},
		{12345, "12.3k"},
		{999999, "1000.0k"},
		{1000000, "1.0M"},
		{1500000, "1.5M"},
	}
	for _, tc := range tests {
		if got := FormatTokens(tc.n); got != tc.want {
			t.Errorf("FormatTokens(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}

func TestVerbosity_ParseAndString(t *testing.T) {
	if got := ParseVerbosity("low"); got != VerbosityLow || got.String() != "low" {
		t.Errorf("low verbosity = %v (%s)", got, got.String())
	}
	if got := ParseVerbosity("HIGH"); got != VerbosityHigh || got.String() != "high" {
		t.Errorf("high verbosity = %v (%s)", got, got.String())
	}
	if got := ParseVerbosity("medium"); got != VerbosityMedium || got.String() != "medium" {
		t.Errorf("medium verbosity = %v (%s)", got, got.String())
	}
	if got := ParseVerbosity(""); got != VerbosityMedium {
		t.Errorf("default verbosity = %v, want medium", got)
	}
}

func TestTokenMetrics_Summary(t *testing.T) {
	tm := TokenMetrics{
		Input:     12300,
		Output:    450,
		Reasoning: 1200,
	}
	if !tm.HasTokens() {
		t.Fatal("expected HasTokens to be true")
	}
	want := "12.3k in | 450 out | 1.2k thinking"
	if got := tm.Summary(); got != want {
		t.Errorf("TokenMetrics.Summary = %q, want %q", got, want)
	}
}

func TestRenderBlock_VerbosityLow_Collapsed(t *testing.T) {
	steps := []Step{
		{Kind: KindThought, Detail: "Thinking about file", Tokens: 1200},
		{Kind: KindTool, Name: "run_command", Title: "Bash(echo one)", Summary: "Ran (echo one)", Detail: "one\n"},
	}
	tm := TokenMetrics{
		Input:     12300,
		Output:    450,
		Reasoning: 1200,
	}

	lines := RenderBlock(steps, RenderOptions{
		Expanded:     false,
		Verbosity:    VerbosityLow,
		TokenMetrics: tm,
	})
	if len(lines) < 3 {
		t.Fatalf("expected at least 3 lines, got %d:\n%v", len(lines), lines)
	}

	// First line must contain the token metrics summary
	if !strings.Contains(lines[0], "12.3k in | 450 out | 1.2k thinking") {
		t.Errorf("line 0 missing token summary: %q", lines[0])
	}
	// Second line must contain Thinking...
	if !strings.Contains(lines[1], "Thinking...") {
		t.Errorf("line 1 missing Thinking...: %q", lines[1])
	}
	// Last line must carry ctrl+o to expand
	if !strings.Contains(lines[len(lines)-1], "ctrl+o to expand") {
		t.Errorf("last line missing hint: %q", lines[len(lines)-1])
	}
}

func TestRenderBlock_VerbosityLow_Expanded(t *testing.T) {
	steps := []Step{
		{Kind: KindThought, Detail: "Thinking about file", Tokens: 1200},
		{Kind: KindTool, Name: "run_command", Title: "Bash(echo one)", Summary: "Ran (echo one)", Detail: "one\n"},
	}
	tm := TokenMetrics{
		Input:     12300,
		Output:    450,
		Reasoning: 1200,
	}

	lines := RenderBlock(steps, RenderOptions{
		Expanded:     true,
		Verbosity:    VerbosityLow,
		TokenMetrics: tm,
	})
	joined := strings.Join(lines, "\n")
	plain := stripAnsiForTest(joined)
	// Expanded in low verbosity shows the details
	if !strings.Contains(plain, "Bash(echo one)") {
		t.Errorf("expanded low-verbosity missing Bash title:\n%s", joined)
	}
	if !strings.Contains(joined, "ctrl+o to collapse") {
		t.Errorf("expanded low-verbosity missing ctrl+o to collapse hint:\n%s", joined)
	}
}

func TestStepDetailLines_CommandOutputWrap(t *testing.T) {
	longLine := "This is a very long line of command output that exceeds forty columns by a substantial margin."
	s := Step{
		Name:   "run_command",
		Title:  "Bash(long_cmd)",
		Detail: fmt.Sprintf("Command exited with code 0.\n\nCommand output:\n%s\n", longLine),
	}
	lines := stepDetailLines(s, 40)
	if len(lines) < 3 {
		t.Fatalf("expected wrapped lines, got %d lines: %v", len(lines), lines)
	}
	if !strings.HasPrefix(lines[0], tuitools.DetailBullet()) {
		t.Errorf("line 0 missing DetailBullet: %q", lines[0])
	}
	for i := 1; i < len(lines); i++ {
		if !strings.HasPrefix(lines[i], tuitools.DetailIndent) {
			t.Errorf("line %d not indented with DetailIndent (7 spaces): %q", i, lines[i])
		}
		content := strings.TrimPrefix(lines[i], tuitools.DetailIndent)
		plainContent := stripAnsiForTest(content)
		if len(plainContent) > 33 {
			t.Errorf("line %d content width %d exceeds wrapWidth 33: %q", i, len(plainContent), content)
		}
	}
}

func TestStepDetailLines_DiffOutputWrap(t *testing.T) {
	longDiffLine := "+func TestPigcap_StdioHandshake_V2_WithExtraLongNameExceedingThirtyCharacters(t *testing.T) {"
	s := Step{
		Name:   "run_command",
		Title:  "Bash(git log -p)",
		Detail: fmt.Sprintf("Command exited with code 0.\n\nCommand output:\n@@ -1,1 +1,1 @@\n%s\n", longDiffLine),
	}
	lines := stepDetailLines(s, 40)
	if len(lines) < 4 {
		t.Fatalf("expected wrapped diff lines, got %d lines: %v", len(lines), lines)
	}
	// Lines 4..len-1 are the wrapped segments of longDiffLine; all must carry ToolAddOpen.
	for i := 4; i < len(lines); i++ {
		if !strings.Contains(lines[i], palette.ToolAddOpen) {
			t.Errorf("expected wrapped diff line %d to maintain ToolAddOpen styling: %q", i, lines[i])
		}
	}
	for i := 1; i < len(lines); i++ {
		if !strings.HasPrefix(lines[i], tuitools.DetailIndent) {
			t.Errorf("diff line %d not indented with DetailIndent: %q", i, lines[i])
		}
	}
}

func TestStepDetailLines_DiffRemoveOutputWrap(t *testing.T) {
	longRemoveLine := "-func TestPigcap_StdioHandshake_OldDeprecatedLongFunctionSignature(t *testing.T) {"
	s := Step{
		Name:   "run_command",
		Title:  "Bash(git log -p)",
		Detail: fmt.Sprintf("Command exited with code 0.\n\nCommand output:\n@@ -1,1 +1,1 @@\n%s\n", longRemoveLine),
	}
	lines := stepDetailLines(s, 40)
	if len(lines) < 4 {
		t.Fatalf("expected wrapped diff lines, got %d lines: %v", len(lines), lines)
	}
	// Lines 4..len-1 are the wrapped segments of longRemoveLine; all must carry ToolRemoveOpen.
	for i := 4; i < len(lines); i++ {
		if !strings.Contains(lines[i], palette.ToolRemoveOpen) {
			t.Errorf("expected wrapped diff remove line %d to maintain ToolRemoveOpen styling: %q", i, lines[i])
		}
	}
	for i := 1; i < len(lines); i++ {
		if !strings.HasPrefix(lines[i], tuitools.DetailIndent) {
			t.Errorf("diff line %d not indented with DetailIndent: %q", i, lines[i])
		}
	}
}
