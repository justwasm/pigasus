package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEditDiff_MatchesReferenceHunk pins the hunk 1.4.3 printed for an
// edit_file call against a five-line file. Raw capture (ref_edit3.bin):
//
//	\x1b[38;2;88;110;117m  ⎿  \x1b[38;2;133;153;0m+1\x1b[m / \x1b[38;2;220;50;47m-1\x1b[m lines
//	\x1b[6G\x1b[38;2;131;148;150m   1\x1b[m    alpha
//	\x1b[6G\x1b[38;2;131;148;150m   2\x1b[m    beta
//	\x1b[6G\x1b[38;2;131;148;150;48;2;238;232;213m   3\x1b[38;2;220;50;47m -  gamma\x1b[K\x1b[m
//	\x1b[6G\x1b[38;2;131;148;150;48;2;238;232;213m   3\x1b[38;2;133;153;0m +  GAMMA\x1b[K\x1b[m
//	\x1b[6G\x1b[38;2;131;148;150m   4\x1b[m    delta
//	\x1b[6G\x1b[38;2;131;148;150m   5\x1b[m    epsilon
//	\x1b[6G\x1b[38;2;131;148;150m   6\x1b[m     (ctrl+o to collapse)
//
// The trailing empty string of a newline-terminated file is a real line to
// the reference (line 6), so it shows up as context.
func TestEditDiff_MatchesReferenceHunk(t *testing.T) {
	d := buildEditDiff(
		"alpha\nbeta\ngamma\ndelta\nepsilon\n",
		"alpha\nbeta\nGAMMA\ndelta\nepsilon\n",
	)
	if d == nil {
		t.Fatal("buildEditDiff returned nil")
	}
	if d.added != 1 || d.removed != 1 {
		t.Fatalf("counts = +%d/-%d, want +1/-1", d.added, d.removed)
	}
	want := []string{" 1 alpha", " 2 beta", "-3 gamma", "+3 GAMMA", " 4 delta", " 5 epsilon", " 6 "}
	if len(d.rows) != len(want) {
		t.Fatalf("rows = %d (%v), want %d", len(d.rows), d.rows, len(want))
	}
	for i, w := range want {
		line := string(d.rows[i].kind) + itoa(d.rows[i].num) + " " + d.rows[i].text
		if strings.Join(strings.Fields(line), " ") != strings.Join(strings.Fields(w), " ") {
			t.Errorf("row %d = %q, want %q", i, line, w)
		}
	}

	lines := d.lines(0)
	if len(lines) != 8 { // header + 7 rows
		t.Fatalf("lines = %d, want 8", len(lines))
	}
	if lines[0] != detailBulletOpen+cToolAddOpen+"+1\x1b[m / "+cToolRemoveOpen+"-1\x1b[m lines" {
		t.Errorf("header = %q", lines[0])
	}
	if lines[1] != "     "+cDiffNumOpen+"   1\x1b[m    alpha" {
		t.Errorf("context row = %q", lines[1])
	}
	if lines[3] != "     "+cDiffStripeOpen+"   3"+cToolRemoveOpen+" -  gamma\x1b[m" {
		t.Errorf("removed row = %q", lines[3])
	}
	if lines[4] != "     "+cDiffStripeOpen+"   3"+cToolAddOpen+" +  GAMMA\x1b[m" {
		t.Errorf("added row = %q", lines[4])
	}
}

// TestEditDiff_SingleLineFile pins the shape the reference printed for a
// one-line file: only the changed pair, and the collapse hint on its own
// row (a changed row would inherit the base3 stripe).
func TestEditDiff_SingleLineFile(t *testing.T) {
	// No trailing newline: the reference file was written by write_file with
	// the exact content, so there is no line 2 to show as context.
	d := buildEditDiff("hello world", "goodbye world")
	if d == nil {
		t.Fatal("buildEditDiff returned nil")
	}
	lines := d.lines(0)
	if len(lines) != 4 {
		t.Fatalf("lines = %d (%q), want 4 (header, -, +, hint spacer)", len(lines), lines)
	}
	if !strings.Contains(lines[1], " -  hello world") {
		t.Errorf("removed row = %q", lines[1])
	}
	if !strings.Contains(lines[2], " +  goodbye world") {
		t.Errorf("added row = %q", lines[2])
	}
	if lines[3] != "    " {
		t.Errorf("hint spacer = %q, want four spaces", lines[3])
	}
}

// TestEditDiff_NoChange covers the no-op edit (both sides equal).
func TestEditDiff_NoChange(t *testing.T) {
	if d := buildEditDiff("same\n", "same\n"); d != nil {
		t.Errorf("buildEditDiff(same,same) = %v, want nil", d)
	}
}

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
	s := newToolStep("edit_file", args, "Successfully edited", false)
	if s.diff == nil {
		t.Fatal("edit_file step has no diff")
	}
	if s.diff.added != 1 || s.diff.removed != 1 {
		t.Errorf("counts = +%d/-%d, want +1/-1", s.diff.added, s.diff.removed)
	}
	lines := stepDetailLines(s, 0)
	if len(lines) == 0 || !strings.Contains(lines[0], "+1") {
		t.Fatalf("detail rows = %q, want a +1 header", lines)
	}
	if s.title != "Edit("+path+")" {
		t.Errorf("title = %q", s.title)
	}
}

// TestReadFile_OneLineHasNoDetail pins the 1.4.3 rule that a single-line
// read renders no "⎿" row at all; the hint moves onto the call head.
func TestReadFile_OneLineHasNoDetail(t *testing.T) {
	one := newToolStep("read_file", json.RawMessage(`{"Path":"/tmp/x"}`), "lonely\n", false)
	if got := stepDetailText(one); got != "" {
		t.Errorf("one-line read detail = %q, want none", got)
	}
	if one.title != "Read(/tmp/x)" || one.summary != "Explored 1 file (x)" {
		t.Errorf("read forms = title %q summary %q", one.title, one.summary)
	}
	two := newToolStep("read_file", json.RawMessage(`{"Path":"/tmp/x"}`), "a\nb\n", false)
	if got := stepDetailText(two); got != "Read 2 lines" {
		t.Errorf("two-line read detail = %q, want %q", got, "Read 2 lines")
	}
}

// TestGroupLines_PerCategoryRows pins the mixed-batch shape: one summary row
// per category, in first-appearance order, then (expanded only) each call.
func TestGroupLines_PerCategoryRows(t *testing.T) {
	steps := []toolStep{
		{kind: stepKindTool, name: "read_file", title: "Read(/tmp/a)", summary: "Explored 1 file (a)", detail: "one\ntwo\n"},
		{kind: stepKindTool, name: "edit_file", title: "Edit(/tmp/a)", summary: "Edited 1 file (a)", detail: "ok"},
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
func TestSearchWeb_ReferenceForms(t *testing.T) {
	args := json.RawMessage(`{"query":"golang"}`)
	if got := toolStepTitle("search_web", args); got != "WebSearch(golang)" {
		t.Errorf("title = %q", got)
	}
	if got := toolStepSummary("search_web", args); got != "Explored 1 search (golang)" {
		t.Errorf("summary = %q", got)
	}
	if got := runningSummary("search_web", args); got != "Exploring 1 search (golang)" {
		t.Errorf("running summary = %q", got)
	}
	if got := runningSpinnerText("search_web"); got != "Searching web" {
		t.Errorf("spinner = %q, want %q", got, "Searching web")
	}
	verb, count, detail := groupSummary([]toolStep{
		{name: "search_web", title: "WebSearch(a)"},
		{name: "search_web", title: "WebSearch(b)"},
	})
	if verb != "Explored" || count != "2 searches" || detail != "(a · b)" {
		t.Errorf("batch = (%q, %q, %q), want (Explored, 2 searches, (a · b))", verb, count, detail)
	}
}

// TestFailedNonCommand_NoFailedWord pins the difference between a failed
// command ("… failed", red bullet) and a failed search (red bullet only).
func TestFailedNonCommand_NoFailedWord(t *testing.T) {
	search := toolStep{name: "search_web", title: "WebSearch(x)", summary: "Explored 1 search (x)", err: true}
	line := strings.Join(stepLines(search, false, 0), "\n")
	if strings.Contains(line, "failed") {
		t.Errorf("failed search should not print the word: %q", line)
	}
	if !strings.HasPrefix(line, cToolBulletErrOpen) {
		t.Errorf("failed search bullet should be red: %q", line)
	}
	cmd := toolStep{name: "run_command", title: "Bash(false)", summary: "Ran (false)", err: true}
	if got := strings.Join(stepLines(cmd, false, 0), "\n"); !strings.Contains(got, "failed") {
		t.Errorf("failed command lost the word: %q", got)
	}
}

// TestEditDiff_141_SyntaxHighlightAndUntintedGutter verifies the AGY 1.4.1
// changelog requirement:
// "Improved the inline diff preview when the agent asks to edit or create a
// file so it renders like the /diff file view, with syntax highlighting for
// your color scheme, background tints on added and removed lines (picked from
// your terminal's light or dark background when using the Terminal color scheme),
// and an untinted line-number gutter."
func TestEditDiff_141_SyntaxHighlightAndUntintedGutter(t *testing.T) {
	d := buildEditDiff(
		"package main\n\nfunc oldFunction() int {\n\treturn 1\n}\n",
		"package main\n\nfunc newFunction() int {\n\treturn 2\n}\n",
	)
	if d == nil {
		t.Fatal("buildEditDiff returned nil")
	}
	d.path = "main.go"

	lines := d.lines(80)
	if len(lines) < 5 {
		t.Fatalf("expected at least 5 lines, got %d: %q", len(lines), lines)
	}

	// Line-number gutter must be untinted (cDiffNumOpen with NO background tint).
	for i, l := range lines[1:] {
		if strings.TrimSpace(l) == "" {
			continue
		}
		// Must NOT have the whole-row gutter stripe background.
		if strings.HasPrefix(l, "     "+cDiffStripeOpen) {
			t.Errorf("line %d has tinted gutter: %q", i+1, l)
		}
		// Must use untinted line-number gutter prefix.
		if !strings.HasPrefix(l, "     "+cDiffNumOpen) {
			t.Errorf("line %d missing untinted gutter prefix: %q", i+1, l)
		}
	}

	// Removed line must contain red background tint (cDiffRemoveTintOpen)
	// and syntax-highlighted keywords (such as func / return).
	var hasRemovedTint, hasAddedTint, hasFuncSyntax bool
	for _, l := range lines {
		if strings.Contains(l, cDiffRemoveTintOpen) {
			hasRemovedTint = true
		}
		if strings.Contains(l, cDiffAddTintOpen) {
			hasAddedTint = true
		}
		if strings.Contains(l, diffTokenOpen(diffKeyword)+"func"+cSyntaxTokenClose) {
			hasFuncSyntax = true
		}
	}

	if !hasRemovedTint {
		t.Errorf("missing removed background tint (%q)", cDiffRemoveTintOpen)
	}
	if !hasAddedTint {
		t.Errorf("missing added background tint (%q)", cDiffAddTintOpen)
	}
	if !hasFuncSyntax {
		t.Errorf("missing syntax-highlighted keyword 'func' in diff lines")
	}
}

// TestCreateFile_InlineDiffPreview verifies that file-creation tools
// (write_to_file / create_file) produce an addition-only inline diff preview.
func TestCreateFile_InlineDiffPreview(t *testing.T) {
	args := json.RawMessage(`{
		"path": "test.go",
		"content": "package main\n\nfunc main() {}\n"
	}`)
	step := newToolStep("write_to_file", args, "Successfully wrote file", false)
	if step.diff == nil {
		t.Fatal("expected write_to_file step to generate diff preview, got nil")
	}
	if step.diff.added != 3 || step.diff.removed != 0 {
		t.Errorf("got added=%d removed=%d, want added=3 removed=0", step.diff.added, step.diff.removed)
	}
	if step.diff.path != "test.go" {
		t.Errorf("diff path = %q, want 'test.go'", step.diff.path)
	}

	lines := step.diff.lines(80)
	if len(lines) != 5 { // 1 header + 3 added lines + 1 trailing newline context
		t.Fatalf("expected 5 lines, got %d: %q", len(lines), lines)
	}
	if !strings.Contains(lines[0], "+3") || !strings.Contains(lines[0], "-0") {
		t.Errorf("header = %q, want +3 / -0 lines", lines[0])
	}
	for i, l := range lines[1:4] {
		if !strings.Contains(l, cDiffAddTintOpen) {
			t.Errorf("row %d missing added tint: %q", i+1, l)
		}
		if !strings.Contains(l, " +  ") {
			t.Errorf("row %d missing diff add marker: %q", i+1, l)
		}
	}
}
