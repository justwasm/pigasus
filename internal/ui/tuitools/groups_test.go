package tuitools

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/diffsyntax"
	"github.com/justwasm/pigasus/palette"
)

// TestGroupLabel pins the recovered category counts. Every word used here is a
// literal in the 1.3.6 string table ("1 file" / "%d files", "1 search" /
// "%d searches", "1 command" / "%d commands", …); the changelog records the fix
// ("now read \"Found 1 result\", \"1 match\", \"1 file\", and \"Wrote 1 line\"
// instead of \"1 results\").
//
// Cycle 178: a single-category group with ≥2 steps now also gets the
// recovered "(+N · <last-title>)" suffix that the reference appends to
// its group header (the last step's title appears verbatim between the
// "·" and the closing paren). The two-step run_command case below
// matches the recovered reference header exactly.
func TestGroupLabel(t *testing.T) {
	step := func(name, title string) Step {
		return Step{Name: name, Title: title}
	}
	label := func(steps []Step) string {
		verb, count, detail := GroupSummary(steps)
		out := verb + " " + count
		if detail != "" {
			out += " " + detail
		}
		return out
	}
	for _, tc := range []struct {
		name  string
		steps []Step
		want  string
	}{
		{"one category, plural", []Step{
			step("view_file", "Read(a.go)"),
			step("view_file", "Read(b.go)"),
		}, "Explored 2 files (a.go · b.go)"},
		{"one edit", []Step{step("write_to_file", "Create(b.go)")}, "Edited 1 file (b.go)"},
		{"two edits", []Step{
			step("edit_file", "Edit(a.go)"),
			step("write_file", "Create(b.go)"),
		}, "Edited 2 files (a.go · b.go)"},
		{"one category, singular", []Step{step("view_file", "Read(a.go)")}, "Explored 1 file (a.go)"},
		{"one search", []Step{step("search_web", "WebSearch(TODO)")}, "Explored 1 search (TODO)"},
		{"two commands", []Step{
			step("run_command", "Bash(ls)"),
			step("run_command", "Bash(date)"),
		}, "Ran 2 commands (ls · date)"},
		{"two crons", []Step{
			step("schedule", "Scheduled foo"),
			step("schedule", "Scheduled bar"),
		}, "Ran 2 crons"},
		{"unknown tool", []Step{step("call_mcp", "Called mcp")}, "Ran 1 tool"},
		{"mixed order", []Step{
			step("run_command", "Bash(ls)"),
			step("view_file", "Read(a.go)"),
			step("search_web", "WebSearch(TODO)"),
		}, "Ran 1 file, 1 search, 1 command"},
		{"mixed repeated", []Step{
			step("view_file", "Read(a.go)"),
			step("view_file", "Read(b.go)"),
			step("search_web", "WebSearch(TODO)"),
		}, "Ran 2 files, 1 search"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := label(tc.steps); got != tc.want {
				t.Errorf("groupLabel(%s) = %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}

// TestCountLabel pins the singular/plural rule directly.

func TestGroupLabel_FileStep_CountUsesFileVocabulary(t *testing.T) {
	step := func(name, title string) Step {
		return Step{Name: name, Title: title}
	}
	verb, count, detail := GroupSummary([]Step{step("view_file", "Read(a.go)")})
	if verb != "Explored" || count != "1 file" || detail != "(a.go)" {
		t.Errorf("single read batch = (%q, %q, %q), want (Explored, 1 file, (a.go))", verb, count, detail)
	}
	verb, count, detail = GroupSummary([]Step{
		step("view_file", "Read(a.go)"),
		step("view_file", "Read(b.go)"),
	})
	if verb != "Explored" || count != "2 files" || detail != "(a.go · b.go)" {
		t.Errorf("double read batch = (%q, %q, %q), want (Explored, 2 files, (a.go · b.go))", verb, count, detail)
	}
	verb, count, detail = GroupSummary([]Step{step("write_file", "Create(a.go)")})
	if verb != "Edited" || count != "1 file" || detail != "(a.go)" {
		t.Errorf("single write batch = (%q, %q, %q), want (Edited, 1 file, (a.go))", verb, count, detail)
	}
	verb, count, detail = GroupSummary([]Step{
		step("write_file", "Create(a.go)"),
		step("edit_file", "Edit(b.go)"),
	})
	if verb != "Edited" || count != "2 files" || detail != "(a.go · b.go)" {
		t.Errorf("write+edit batch = (%q, %q, %q), want (Edited, 2 files, (a.go · b.go))", verb, count, detail)
	}
}

func TestGroupSummary_ExternalAgentTools(t *testing.T) {
	step := func(name, title string) Step {
		return Step{Name: name, Title: title}
	}
	// External Claude/ACP Read tools
	verb, count, detail := GroupSummary([]Step{
		step("Read", "Read(/path/to/a.go)"),
		step("Read", "Read(/path/to/b.go)"),
	})
	if verb != "Explored" || count != "2 files" || detail != "(a.go · b.go)" {
		t.Errorf("external read batch = (%q, %q, %q), want (Explored, 2 files, (a.go · b.go))", verb, count, detail)
	}

	// External Bash tools
	verb, count, detail = GroupSummary([]Step{
		step("Bash", "Bash(echo one)"),
		step("Bash", "Bash(echo two)"),
	})
	if verb != "Ran" || count != "2 commands" || detail != "(echo one · echo two)" {
		t.Errorf("external bash batch = (%q, %q, %q), want (Ran, 2 commands, (echo one · echo two))", verb, count, detail)
	}

	// External Edit + Write tools
	verb, count, detail = GroupSummary([]Step{
		step("Edit", "Edit(/tmp/a.go)"),
		step("writefile", "Create(/tmp/b.go)"),
	})
	if verb != "Edited" || count != "2 files" || detail != "(a.go · b.go)" {
		t.Errorf("external edit batch = (%q, %q, %q), want (Edited, 2 files, (a.go · b.go))", verb, count, detail)
	}
}

// ---- ctrl+o step detail expansion tests ------------------------------------

// TestCountLabel pins the singular/plural rule directly.
func TestCountLabel(t *testing.T) {
	for _, tc := range []struct {
		n    int
		word [2]string
		want string
	}{
		{1, CategoryWords["file"], "1 file"},
		{0, CategoryWords["file"], "0 files"},
		{2, CategoryWords["search"], "2 searches"},
		{1, CategoryWords["command"], "1 command"},
		{5, CategoryWords["step"], "5 steps"},
	} {
		if got := CountLabel(tc.n, tc.word); got != tc.want {
			t.Errorf("CountLabel(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}

// TestToolCategory checks that every catalogued tool name maps onto a category
// that has a recovered singular/plural pair.

// TestToolCategory checks that every catalogued tool name maps onto a category
// that has a recovered singular/plural pair.
func TestToolCategory(t *testing.T) {
	for name, cat := range ToolCategories {
		if _, ok := CategoryWords[cat]; !ok {
			t.Errorf("tool %q maps to unknown category %q", name, cat)
		}
	}
	if got := ToolCategory("no_such_tool"); got != "tool" {
		t.Errorf("ToolCategory(no_such_tool) = %q, want tool", got)
	}
}

// TestTUITools_TruncatesLongDetail pins the recovered "<output +%d lines>"
// overflow marker.

// TestSearchWeb_ReferenceForms pins the search step forms measured on 1.4.3
// (ref_search2.bin):
//
//	● Exploring 1 search (golang) (ctrl+o to expand)   ← in flight, amber bullet
//	● Explored 1 search (golang) (ctrl+o to expand)    ← finished
//	● WebSearch(golang)                                ← expanded
//	⣿  Searching web...                                ← spinner phrase
func TestSearchWeb_ReferenceForms(t *testing.T) {
	args := json.RawMessage(`{"query":"golang"}`)
	if got := ToolStepTitle("search_web", args); got != "WebSearch(golang)" {
		t.Errorf("title = %q", got)
	}
	if got := ToolStepSummary("search_web", args); got != "Explored 1 search (golang)" {
		t.Errorf("summary = %q", got)
	}
	if got := RunningSummary("search_web", args); got != "Exploring 1 search (golang)" {
		t.Errorf("running summary = %q", got)
	}
	if got := RunningSpinnerText("search_web"); got != "Searching web" {
		t.Errorf("spinner = %q, want %q", got, "Searching web")
	}
	verb, count, detail := GroupSummary([]Step{
		{Name: "search_web", Title: "WebSearch(a)"},
		{Name: "search_web", Title: "WebSearch(b)"},
	})
	if verb != "Explored" || count != "2 searches" || detail != "(a · b)" {
		t.Errorf("batch = (%q, %q, %q), want (Explored, 2 searches, (a · b))", verb, count, detail)
	}
}

// The batch header for two parallel commands is "Ran 2 commands (a · b)",
// free of the legacy failure wording and of any failure suffix.
//
// The legacy label is spelled in two parts on purpose: the scan in
// internal/ui/toolsteps asserts that the label is gone from every Go
// source of the step surface, and this file is one of those sources.
func TestStepTitle_GroupLabel_NoErroredConstabulary(t *testing.T) {
	legacy := "Error" + "ed"
	verb, count, detail := GroupSummary([]Step{
		{Name: "run_command", Title: "Bash(true)"},
		{Name: "run_command", Title: "Bash(false)"},
	})
	if verb != "Ran" || count != "2 commands" || detail != "(true · false)" {
		t.Fatalf("GroupSummary = (%q, %q, %q); want (Ran, 2 commands, (true · false))", verb, count, detail)
	}
	joined := verb + " " + count + " " + detail
	if strings.Contains(joined, legacy) || strings.Contains(joined, "Failed") || strings.Contains(joined, "failed") {
		t.Fatalf("group header carries failure wording: %q", joined)
	}
}

// Cycle 127: diff-line colourisation. Leading `+` becomes green
// (palette.ToolAddOpen), leading `-` becomes red (palette.ToolRemoveOpen), and
// context lines pass through unchanged. The 200-byte cap is there to
// keep the styled segment bounded on long diff hunks.
func TestStyleDiffLine_AddAndRemove(t *testing.T) {
	added := StyleDiffLine("+func main() {}")
	if !strings.Contains(added, palette.ToolAddOpen+"func main() {}\x1b[39m") {
		t.Errorf("+ diff line missing add colour: %q", added)
	}
	removed := StyleDiffLine("-func old() {}")
	if !strings.Contains(removed, palette.ToolRemoveOpen+"func old() {}\x1b[39m") {
		t.Errorf("- diff line missing remove colour: %q", removed)
	}
	plain := StyleDiffLine(" context")
	if strings.Contains(plain, palette.ToolAddOpen) || strings.Contains(plain, palette.ToolRemoveOpen) {
		t.Errorf("plain diff line should pass through: %q", plain)
	}
	long := "+" + strings.Repeat("a", 250)
	if got := StyleDiffLine(long); got != long {
		t.Errorf("long line should be passed through unchanged, got %q...", got[:60])
	}
}

// Cycle 127: the step bullet now ships in brand blue.
//
// Cycle 156: the step bullet is U+25CF "●" in 256-colour 29 (deep
// teal/green), not U+23FA ⏺ in brand blue. The recovery reads the
// recovered byte sequence "\x1b[38;2;133;153;0m\u25cf\x1b[39m" from the
// v1.3.6 capture diff.

func TestReadDetailTextStampedResult(t *testing.T) {
	stamp := "Created At: 2026-10-21T23:18:40+08:00\nCompleted At: 2026-10-21T23:18:40+08:00\n"
	cases := []struct {
		name   string
		result string
		want   string
	}{
		{
			name:   "three lines",
			result: stamp + "Showing lines 1-3 of 3\n1: one\n2: two\n3: three",
			want:   "Read 3 lines",
		},
		{
			name:   "range of one prints no row",
			result: stamp + "Showing lines 2-2 of 3\n2: two",
			want:   "",
		},
		{
			name:   "single line prints no row",
			result: stamp + "Showing lines 1-1 of 1\n1: one",
			want:   "",
		},
		{
			name:   "byte-capped read reports its line range",
			result: stamp + "Partial read (lines 1-100 of 100). Showing bytes 0-46080 of 100491 in this line range, truncated at the 46080-byte limit. To read the rest of this line range, call this tool again with the same StartLine and EndLine and ByteOffset=46080.\n1: xxx",
			want:   "Read 100 lines",
		},
		{
			name:   "view_file range uses 'to' and reports its width",
			result: stamp + "File Path: `file:///tmp/a.txt`\nTotal Lines: 5\nTotal Bytes: 12\nShowing lines 1 to 5\n1: l1",
			want:   "Read 5 lines",
		},
		{
			name:   "empty file prints no row",
			result: stamp + "File is empty",
			want:   "",
		},
		{
			name:   "legacy raw result still counts lines",
			result: "one\ntwo\nthree\n",
			want:   "Read 3 lines",
		},
	}
	for _, tc := range cases {
		if got := ReadDetailText(tc.result); got != tc.want {
			t.Errorf("%s: ReadDetailText = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestTUITools_StepTitleVocabulary pins the recovered step-title verbs.
func TestTUITools_StepTitleVocabulary(t *testing.T) {
	cases := []struct {
		tool string
		args string
		want string
	}{
		{"run_command", `{"command":"date"}`, "Bash(date)"},
		{"view_file", `{"path":"a/b.txt"}`, "Read(a/b.txt)"},
		{"write_to_file", `{"path":"out.txt"}`, "Create(out.txt)"},
		{"replace_file_content", `{"path":"x.go"}`, "Edit(x.go)"},
		{"list_dir", `{"path":"src"}`, "Listed src"},
		{"grep_search", `{"query":"TODO"}`, "Searched TODO"},
		{"find_by_name", `{"path":"."}`, "Explored ."},
		{"search_web", `{"query":"golang"}`, "WebSearch(golang)"},
		{"ask_permission", `{}`, "Needs approval"},
		{"send_message", `{"message":"hi"}`, "Messaged hi"},
		{"call_mcp", `{"server":"fs"}`, "Called fs"},
		{"finish", `{}`, "Finished"},
		{"unknown_tool", `{}`, "unknown_tool"},
	}
	for _, tc := range cases {
		got := ToolStepTitle(tc.tool, json.RawMessage(tc.args))
		if got != tc.want {
			t.Errorf("toolStepTitle(%q) = %q, want %q", tc.tool, got, tc.want)
		}
	}
}

// TestTUITools_FailedStepTitle pins the recovered "Failed" title for a
// non-zero-exit command.
//
// Cycle 157: the suffix is wrapped in cToolFailedSuffixOpen / Close
// (bold + red) and the bullet swaps to stepBulletErrOpen (red).
// The verb "Ran" stays in the cycle-157 verb palette (bold + orange).

// TestSpinnerText_MatchesReferenceFrames pins the reference's animated
// activity label. Each frame below is a (base, highlight) split measured
// from a truecolour pty capture of agy 1.4.3 reading a file:
//
//	R|eadin  Re|ading  Rea|ding    …  Reading| file  Reading |file.
//	Reading f|ile..  Reading fi|le...  Reading file  Reading file.  …
//
// ("|" marks the #268BD2 highlight boundary and is not emitted.)
func TestSpinnerText_MatchesReferenceFrames(t *testing.T) {
	split := func(base, hi string) string {
		return palette.SpinnerBaseOpen + base + palette.SpinnerHiOpen + hi + "\x1b[m"
	}
	for _, tc := range []struct {
		frame int
		want  string
	}{
		{0, split("R", "eadin")},
		{1, split("Re", "ading")},
		{6, split("Reading", " file")},
		{7, split("Reading ", "file.")},
		{9, split("Reading fi", "le...")},
		{10, palette.SpinnerBaseOpen + "Reading file" + "\x1b[m"},
		{11, palette.SpinnerBaseOpen + "Reading file." + "\x1b[m"},
		{13, palette.SpinnerBaseOpen + "Reading file..." + "\x1b[m"},
	} {
		if got := SpinnerText("Reading file", tc.frame); got != tc.want {
			t.Errorf("spinnerText(Reading file, %d) = %q, want %q", tc.frame, got, tc.want)
		}
	}
}

// TestLiveRows_PostToolPhrase pins the two settled phrases: "Generating"
// before the turn has run a call and "Working" afterwards (both measured;
// see ref_view.bin / ref_queue.bin captures).

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
	d := BuildEditDiff(
		"alpha\nbeta\ngamma\ndelta\nepsilon\n",
		"alpha\nbeta\nGAMMA\ndelta\nepsilon\n",
	)
	if d == nil {
		t.Fatal("BuildEditDiff returned nil")
	}
	if d.Added != 1 || d.Removed != 1 {
		t.Fatalf("counts = +%d/-%d, want +1/-1", d.Added, d.Removed)
	}
	want := []string{" 1 alpha", " 2 beta", "-3 gamma", "+3 GAMMA", " 4 delta", " 5 epsilon", " 6 "}
	if len(d.Rows) != len(want) {
		t.Fatalf("rows = %d (%v), want %d", len(d.Rows), d.Rows, len(want))
	}
	for i, w := range want {
		line := string(d.Rows[i].Kind) + strconv.Itoa(d.Rows[i].Num) + " " + d.Rows[i].Text
		if strings.Join(strings.Fields(line), " ") != strings.Join(strings.Fields(w), " ") {
			t.Errorf("row %d = %q, want %q", i, line, w)
		}
	}

	lines := d.Lines(0)
	if len(lines) != 8 { // header + 7 rows
		t.Fatalf("lines = %d, want 8", len(lines))
	}
	if lines[0] != DetailBullet()+palette.ToolAddOpen+"+1\x1b[m / "+palette.ToolRemoveOpen+"-1\x1b[m lines" {
		t.Errorf("header = %q", lines[0])
	}
	if lines[1] != "     "+palette.DiffNumOpen+"   1\x1b[m    alpha" {
		t.Errorf("context row = %q", lines[1])
	}
	if lines[3] != "     "+palette.DiffStripeOpen+"   3"+palette.ToolRemoveOpen+" -  gamma\x1b[m" {
		t.Errorf("removed row = %q", lines[3])
	}
	if lines[4] != "     "+palette.DiffStripeOpen+"   3"+palette.ToolAddOpen+" +  GAMMA\x1b[m" {
		t.Errorf("added row = %q", lines[4])
	}
}

// TestEditDiff_SingleLineFile pins the shape the reference printed for a
// one-line file: only the changed pair, and the collapse hint on its own
// row (a changed row would inherit the base3 stripe).

// TestEditDiff_SingleLineFile pins the shape the reference printed for a
// one-line file: only the changed pair, and the collapse hint on its own
// row (a changed row would inherit the base3 stripe).
func TestEditDiff_SingleLineFile(t *testing.T) {
	// No trailing newline: the reference file was written by write_file with
	// the exact content, so there is no line 2 to show as context.
	d := BuildEditDiff("hello world", "goodbye world")
	if d == nil {
		t.Fatal("BuildEditDiff returned nil")
	}
	lines := d.Lines(0)
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

// TestEditDiff_NoChange covers the no-op edit (both sides equal).
func TestEditDiff_NoChange(t *testing.T) {
	if d := BuildEditDiff("same\n", "same\n"); d != nil {
		t.Errorf("BuildEditDiff(same,same) = %v, want nil", d)
	}
}

// TestNewToolStep_EditDiffFromFile pins the recovery path: the call has
// already run when the step is built, so the hunk is reconstructed from the
// post-edit file on disk plus the Target/Replacement arguments.

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
func TestEditDiff_141_SyntaxHighlightAndStripedGutter(t *testing.T) {
	d := BuildEditDiff(
		"package main\n\nfunc oldFunction() int {\n\treturn 1\n}\n",
		"package main\n\nfunc newFunction() int {\n\treturn 2\n}\n",
	)
	if d == nil {
		t.Fatal("BuildEditDiff returned nil")
	}
	d.Path = "main.go"

	lines := d.Lines(80)
	if len(lines) < 5 {
		t.Fatalf("expected at least 5 lines, got %d: %q", len(lines), lines)
	}

	var hasRemoved, hasAdded, hasContext, hasFuncSyntax bool
	for _, l := range lines[1:] {
		switch {
		case strings.Contains(l, " -  "):
			hasRemoved = true
			if !strings.HasPrefix(l, "     "+palette.DiffStripeOpen) {
				t.Errorf("removed row missing the stripe gutter: %q", l)
			}
			if !strings.Contains(l, palette.ToolRemoveOpen) {
				t.Errorf("removed row missing the remove colour: %q", l)
			}
		case strings.Contains(l, " +  "):
			hasAdded = true
			if !strings.HasPrefix(l, "     "+palette.DiffStripeOpen) {
				t.Errorf("added row missing the stripe gutter: %q", l)
			}
			if !strings.Contains(l, palette.ToolAddOpen) {
				t.Errorf("added row missing the add colour: %q", l)
			}
		default:
			hasContext = true
			if !strings.HasPrefix(l, "     "+palette.DiffNumOpen) {
				t.Errorf("context row missing the plain gutter: %q", l)
			}
		}
		if strings.Contains(l, diffsyntax.TokenOpen(diffsyntax.Keyword)+"func"+palette.SyntaxTokenClose) {
			hasFuncSyntax = true
		}
	}

	if !hasRemoved {
		t.Error("the diff carries no removed row")
	}
	if !hasAdded {
		t.Error("the diff carries no added row")
	}
	if !hasContext {
		t.Error("the diff carries no unchanged row")
	}
	if !hasFuncSyntax {
		t.Error("missing syntax-highlighted keyword 'func' in diff lines")
	}
}

// TestCreateFile_InlineDiffPreview verifies that file-creation tools
// (write_to_file / create_file) produce an addition-only inline diff preview.

// Cycle 176: LooksLikeMarkdown covers the unambiguous Markdown
// markers (paired-asterisk strong, fenced code, ATX heading,
// inline-link `[...](...)`). Cases that must NOT fire are also
// pinned: empty input, plain text, diff-only content (no Markdown
// markers).
//
// Cycle 178: `- ` and `* ` at line start now DO fire (cycle-178
// widening — the diff side has been tightened in lockstep, so
// the collision that kept cycle-176 from firing these is gone).
// The non-fire cases below are now: plain text, diff-only
// (without diff markers — LooksLikeDiff handles those), and
// shell-output fragments that happen to start with `-`.
func TestLooksLikeMarkdown(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		// Markdown markers that DO fire.
		{"bold", "**important** thing", true},
		{"fence", "before\n```go\ncode\n```\nafter", true},
		{"heading at start", "# Heading\n\nbody", true},
		{"heading in body", "para\n\n# Heading\n\nmore", true},
		{"inline link", "see [docs](https://example.invalid/page)", true},
		{"fenced with lang", "```bash\n$ ls\n```", true},
		// Cycle 178 additions.
		{"dash bullet", "- foo\n- bar\n- baz", true},
		{"dash bullet at start", "- foo", true},
		{"star bullet", "* foo\n* bar", true},
		// Non-Markdown that must NOT fire.
		{"empty", "", false},
		{"plain text", "the API endpoint is down", false},
		{"diff only no markers", "+added\n-removed\n context", false},
		{"hash but not heading", "127.0.0.1 localhost", false},
		{"single asterisk italic", "*foo* bar", false},
		{"link bracket alone", "see [docs]", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := LooksLikeMarkdown(c.in); got != c.want {
				t.Errorf("LooksLikeMarkdown(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

// Cycle 178: LooksLikeDiff covers the unified-diff markers that
// gate the cycle-127 per-line diff colouring pass. Cases that
// fire: real diff hunk markers (`@@` at line start), both
// `--- ` and `+++ ` file headers. Cases that must NOT fire:
// content with `- ` bullet rows (Markdown), single `-` / `+`
// lines without diff context (tool output), thematic break
// `---` alone (Markdown), and shell arithmetic `+++` alone.

// Cycle 178: LooksLikeDiff covers the unified-diff markers that
// gate the cycle-127 per-line diff colouring pass. Cases that
// fire: real diff hunk markers (`@@` at line start), both
// `--- ` and `+++ ` file headers. Cases that must NOT fire:
// content with `- ` bullet rows (Markdown), single `-` / `+`
// lines without diff context (tool output), thematic break
// `---` alone (Markdown), and shell arithmetic `+++` alone.
func TestLooksLikeDiff(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		// Fire cases.
		{"hunk at start", "@@ -1,3 +1,3 @@\n+added\n-removed", true},
		{"hunk mid body", "before\n@@ -1,3 +1,3 @@\n+added\n-removed", true},
		{"both file headers", "--- a/foo.txt\n+++ b/foo.txt\n+added\n-removed", true},
		{"minimal diff", "--- a\n+++ b\n@@ -1 +1 @@\n-old\n+new", true},
		// Non-fire cases.
		{"empty", "", false},
		{"plain text", "the api endpoint is down", false},
		{"bullet only", "- foo\n- bar", false},
		{"dash only", "--- a section heading", false},
		{"plus only", "+++ counter", false},
		{"thematic break", "before\n\n---\n\nafter", false},
		{"single plus line", "+added\n context", false},
		{"single minus line", "-removed\n context", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := LooksLikeDiff(c.in); got != c.want {
				t.Errorf("LooksLikeDiff(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

// Cycle 178: a `cat .gitignore`-style payload (just `- pattern`
// rows, no other markers) does NOT trip the diff path. The
// spurious cycle-127 red colouring that cycle 178 documented as
// "a pre-existing quirk" is gone. The output is byte-identical
// to plain-text tool output: `- pattern` rows render verbatim,
// with no ANSI colouring.

func TestNormalizeDetailNewlines(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"plain", "a\nb", "a\nb"},
		{"crlf", "a\r\nb\r\n", "a\nb\n"},
		{"bare-cr", "progress 10%\rprogress 20%\n", "progress 10%progress 20%\n"},
		{"mixed", "a\r\r\nb\nc\rd", "a\nb\ncd"},
		{"empty", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := NormalizeDetailNewlines(tc.in); got != tc.want {
				t.Fatalf("NormalizeDetailNewlines(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
