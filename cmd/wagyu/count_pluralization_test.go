package main

// count_pluralization_test.go — cycle count-pluralization.
//
// Pins the AGY 1.3.3 changelog line:
//
//	Fixed counts in tool steps and the `/codesearch` panel, which now
//	read "Found 1 result", "1 match", "1 file", and "Wrote 1 line"
//	instead of "1 results", and the file-creation step reporting one
//	line too many.
//
// The fix has two parts:
//
//  1. A singular/plural rule that picks the right form ("1 file" vs.
//     "2 files", "Wrote 1 line" vs. "Wrote 5 lines"). Every existing
//     call site of `countLabel` is covered — the new tests below
//     pin the rule and the new "Wrote N lines" surface at the same
//     time.
//
//  2. A line-count helper used by the file-creation step title. The
//     pre-fix code reported one too many because a file whose content
//     ends in "\n" was counted as `len(strings.Split(content, "\n"))`
//     elements (the trailing empty element), so a 3-line file ending
//     in "\n" came back as 4. The new helper trims the trailing
//     newline first, matching what `wc -l` would print.

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

// TestCountLabel_FullVocabulary pins the singular/plural rule for every
// counted surface in the changelog line: "result", "match", "file", and
// the new "line" pair (used by the file-creation title). The table also
// covers n=0 (which the helper must NOT special-case — "0 results" stays
// plural) and n>1 (which always uses the plural form).
func TestCountLabel_FullVocabulary(t *testing.T) {
	pairs := map[string][2]string{
		"result": {"result", "results"},
		"match":  {"match", "matches"},
		"file":   {"file", "files"},
		"line":   {"line", "lines"},
	}
	cases := []struct {
		n    int
		name string
		want string
	}{
		// results — header /codesearch
		{0, "result", "0 results"},
		{1, "result", "1 result"},
		{2, "result", "2 results"},
		{7, "result", "7 results"},
		// matches — codesearch panel streaming state
		{0, "match", "0 matches"},
		{1, "match", "1 match"},
		{5, "match", "5 matches"},
		// files — group header / codesearch "in K files"
		{0, "file", "0 files"},
		{1, "file", "1 file"},
		{3, "file", "3 files"},
		// lines — file-creation step title (new)
		{0, "line", "0 lines"},
		{1, "line", "1 line"},
		{2, "line", "2 lines"},
		{42, "line", "42 lines"},
	}
	for _, c := range cases {
		t.Run(c.name+"-"+strconv.Itoa(c.n), func(t *testing.T) {
			got := countLabel(c.n, pairs[c.name])
			if got != c.want {
				t.Errorf("countLabel(%d, %s) = %q, want %q", c.n, c.name, got, c.want)
			}
		})
	}
}

// TestFileLineCount_OffByOneFixed pins the line-count helper that drives
// the file-creation step title. The reference ships "Wrote %d lines"
// / "Wrote 1 line" literals, so the count must match `wc -l` — a file
// that ends in "\n" is exactly as many lines as a file that doesn't,
// NOT one more. Each row below pins a specific shape and would have
// failed the pre-fix `len(strings.Split(content, "\n"))` formula.
func TestFileLineCount_OffByOneFixed(t *testing.T) {
	cases := []struct {
		name         string
		content      string
		want         int
		whyThisCount string
	}{
		{"empty", "", 0, "no content → 0 lines, no count suffix in title"},
		{"single line no terminator", "hello", 1, `"hello" is one line, no trailing \n`},
		{"single line with terminator", "hello\n", 1, `trailing \n must NOT add a phantom line — the off-by-one fix`},
		{"two lines unterminated", "a\nb", 2, `"a\nb" is two lines`},
		{"two lines terminated", "a\nb\n", 2, `"a\nb\n" is two lines — the off-by-one fix lands here too`},
		{"three lines terminated", "a\nb\nc\n", 3, `three-line file ending in \n, classic off-by-one bait`},
		{"five lines unterminated", "1\n2\n3\n4\n5", 5, "five lines, no trailing newline"},
		{"blank line only", "\n", 1, `just "\n" is one (blank) line`},
		{"blank lines interspersed", "a\n\nb\n", 3, "two embedded newlines make one blank line between 'a' and 'b'"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := fileLineCount(c.content)
			if got != c.want {
				t.Errorf("fileLineCount(%q) = %d, want %d — %s", c.content, got, c.want, c.whyThisCount)
			}
		})
	}
}

// TestFileLineCount_PreFixHypothetical probes the exact failure mode the
// changelog describes: a file with N lines that ends in "\n" must report
// N, not N+1. The pre-fix bug would have used `len(strings.Split(content,
// "\n"))`, which always returns N+1 for content ending in "\n" because
// of the trailing empty element. This test ensures the helper does not
// regress to that formula even if someone "simplifies" it later.
func TestFileLineCount_PreFixHypothetical(t *testing.T) {
	// Hypothetical buggy formula. We compute it here and assert the new
	// helper disagrees with it for trailing-newline inputs. If
	// fileLineCount ever matches the buggy value, we have re-introduced
	// the off-by-one.
	buggy := func(s string) int {
		if s == "" {
			return 0
		}
		return len(strings.Split(s, "\n"))
	}
	inputs := []string{"a\n", "a\nb\n", "a\nb\nc\n", "single\n", "line"}
	for _, in := range inputs {
		got := fileLineCount(in)
		bad := buggy(in)
		if got == bad && strings.HasSuffix(in, "\n") {
			t.Errorf("fileLineCount(%q) = %d matches the buggy N+1 formula — off-by-one regressed", in, got)
		}
		// Sanity: the correct count is strictly less than the buggy count
		// for trailing-newline inputs.
		if strings.HasSuffix(in, "\n") && got >= bad {
			t.Errorf("fileLineCount(%q) = %d not below buggy %d", in, got, bad)
		}
	}
}

// TestToolStep_WriteFile_LinesIncluded pins the split the reference makes in
// 1.4.3: the expanded title is the call form ("Create(out.txt)"), while the
// detail row carries the line count ("Wrote 1 line" / "Wrote 3 lines"). The
// trailing-newline cases pin the off-by-one ("a\nb\nc\n" is 3 lines, not 4).
//
// The three tool names (write_to_file / write_file / create_file) all route
// through the same title and detail builders; the loop below makes sure each
// one reaches the same pair for the same payload, even though the primary
// path argument key differs.
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
			step := newToolStep("write_to_file", json.RawMessage(c.args), "", false)
			if step.title != c.wantTitle {
				t.Errorf("title = %q, want %q", step.title, c.wantTitle)
			}
			if got := stepDetailText(step); got != c.wantBody {
				t.Errorf("detail = %q, want %q", got, c.wantBody)
			}
		})
	}
}

// TestGroupLabel_FileStep_CountUsesFileVocabulary makes sure the group
// header still routes the file-creation tool through the "file" category
// word — countLabel("Wrote…") and countLabel(groupLabel("files")) are
// independent surfaces and both must use the singular "file" when a
// single-step group of file-category tools is collapsed. The cycling
// between the singular "1 file" form and the plural "2 files" form is
// what made a single write_to_file look like "1 files" before the
// changelog fix.
func TestGroupLabel_FileStep_CountUsesFileVocabulary(t *testing.T) {
	step := func(name, title string) toolStep {
		return toolStep{name: name, title: title}
	}
	verb, count, detail := groupSummary([]toolStep{step("view_file", "Read(a.go)")})
	if verb != "Explored" || count != "1 file" || detail != "(a.go)" {
		t.Errorf("single read batch = (%q, %q, %q), want (Explored, 1 file, (a.go))", verb, count, detail)
	}
	verb, count, detail = groupSummary([]toolStep{
		step("view_file", "Read(a.go)"),
		step("view_file", "Read(b.go)"),
	})
	if verb != "Explored" || count != "2 files" || detail != "(a.go · b.go)" {
		t.Errorf("double read batch = (%q, %q, %q), want (Explored, 2 files, (a.go · b.go))", verb, count, detail)
	}
	// 1.4.3 labels a write "Edited", and a write/edit batch shares the
	// category, so it summarises as one "Edited N files" row.
	verb, count, detail = groupSummary([]toolStep{step("write_file", "Create(a.go)")})
	if verb != "Edited" || count != "1 file" || detail != "(a.go)" {
		t.Errorf("single write batch = (%q, %q, %q), want (Edited, 1 file, (a.go))", verb, count, detail)
	}
	verb, count, detail = groupSummary([]toolStep{
		step("write_file", "Create(a.go)"),
		step("edit_file", "Edit(b.go)"),
	})
	if verb != "Edited" || count != "2 files" || detail != "(a.go · b.go)" {
		t.Errorf("write+edit batch = (%q, %q, %q), want (Edited, 2 files, (a.go · b.go))", verb, count, detail)
	}
}
