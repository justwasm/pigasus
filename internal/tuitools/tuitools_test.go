package tuitools

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestToolStepTitle(t *testing.T) {
	args1 := json.RawMessage(`{"CommandLine":"echo hello"}`)
	if got := ToolStepTitle("run_command", args1); got != "Bash(echo hello)" {
		t.Fatalf("unexpected title for run_command: %q", got)
	}

	args2 := json.RawMessage(`{"AbsolutePath":"/tmp/test.go"}`)
	if got := ToolStepTitle("read_file", args2); got != "Read(/tmp/test.go)" {
		t.Fatalf("unexpected title for read_file: %q", got)
	}

	args3 := json.RawMessage(`{"Query":"golang"}`)
	if got := ToolStepTitle("search_web", args3); got != "WebSearch(golang)" {
		t.Fatalf("unexpected title for search_web: %q", got)
	}
}

func TestToolCategoriesAndGroups(t *testing.T) {
	if got := ToolCategory("run_command"); got != "command" {
		t.Fatalf("expected command, got %q", got)
	}
	if got := ToolCategory("read_file"); got != "file" {
		t.Fatalf("expected file, got %q", got)
	}
	if got := ToolCategory("search_web"); got != "search" {
		t.Fatalf("expected search, got %q", got)
	}

	if paren := ParenArg("Bash(echo hi)"); paren != "echo hi" {
		t.Fatalf("unexpected paren arg: %q", paren)
	}

	segs := SplitShellSegments("echo 1 && echo 2; echo 3")
	if len(segs) != 3 || segs[0] != "echo 1" || segs[1] != "echo 2" || segs[2] != "echo 3" {
		t.Fatalf("unexpected shell segments: %v", segs)
	}
}

func TestBuildEditDiff(t *testing.T) {
	oldT := "line1\nline2\nline3\n"
	newT := "line1\nline2 modified\nline3\n"
	diff := BuildEditDiff(oldT, newT)
	if diff == nil {
		t.Fatal("expected non-nil diff")
	}
	if diff.Added != 1 || diff.Removed != 1 {
		t.Fatalf("expected +1/-1 lines, got +%d/-%d", diff.Added, diff.Removed)
	}
	lines := RenderDiffLines(diff, 80)
	if len(lines) == 0 || !strings.Contains(lines[0], "+1") || !strings.Contains(lines[0], "-1") {
		t.Fatalf("unexpected diff lines: %v", lines)
	}
}

func TestBuildEditDiff_EdgeCases(t *testing.T) {
	if d := BuildEditDiff("same", "same"); d != nil {
		t.Fatalf("expected nil for identical text, got %v", d)
	}
	if d := BuildEditDiff("", ""); d != nil {
		t.Fatalf("expected nil for empty text, got %v", d)
	}
	createDiff := BuildEditDiff("", "new line 1\nnew line 2\n")
	if createDiff == nil || createDiff.Added != 2 || createDiff.Removed != 0 {
		t.Fatalf("expected create diff +2/-0, got %v", createDiff)
	}
	deleteDiff := BuildEditDiff("old line 1\nold line 2\n", "")
	if deleteDiff == nil || deleteDiff.Added != 0 || deleteDiff.Removed != 2 {
		t.Fatalf("expected delete diff +0/-2, got %v", deleteDiff)
	}
}

func TestBuildEditDiff_ReferenceHunk(t *testing.T) {
	oldT := "alpha\nbeta\ngamma\ndelta\nepsilon\n"
	newT := "alpha\nbeta\nGAMMA\ndelta\nepsilon\n"
	d := BuildEditDiff(oldT, newT)
	if d == nil || d.Added != 1 || d.Removed != 1 {
		t.Fatalf("expected +1/-1 diff, got %v", d)
	}
	if len(d.Rows) != 7 {
		t.Fatalf("expected 7 rows (including trailing empty line context), got %d (%v)", len(d.Rows), d.Rows)
	}
}

func TestRenderDiffLines_ReferenceShape(t *testing.T) {
	// 【一手】 agy 1.4.3's captured diff
	// (docs/cycles/cycle-285-tool-display/deliverable.md) shows the
	// reference's row shape: the line number in the gutter, the marker
	// column and the text. The 1.4.8 binary pads an added or a removed
	// body to the content width, so those rows are one column
	// narrower than the width they were given.
	oldT := "alpha\nbeta\ngamma\ndelta\nepsilon\n"
	newT := "alpha\nbeta\nGAMMA\ndelta\nepsilon\n"
	d := BuildEditDiff(oldT, newT)
	if d == nil {
		t.Fatal("expected a diff")
	}
	lines := RenderDiffLines(d, 80)
	want := []string{
		"  ⎿  +1 / -1 lines",
		"        1    alpha",
		"        2    beta",
		"        3 -  gamma",
		"        3 +  GAMMA",
		"        4    delta",
		"        5    epsilon",
		"        6    ",
	}
	if len(lines) != len(want) {
		t.Fatalf("got %d lines, want %d:\n%q", len(lines), len(want), plainLines(lines))
	}
	for i, l := range lines {
		got := stripANSI(l)
		// The reference pads an added or a removed body to the content
		// width; an unchanged body is not padded.
		if i > 0 && (strings.Contains(want[i], "-  ") || strings.Contains(want[i], "+  ")) {
			if !strings.HasPrefix(got, want[i]) {
				t.Errorf("line %d = %q, want the prefix %q", i, got, want[i])
			}
			if n := len([]rune(got)); n != 80-1 {
				t.Errorf("line %d is %d columns wide, want %d", i, n, 80-1)
			}
			continue
		}
		if got != want[i] {
			t.Errorf("line %d = %q, want %q", i, got, want[i])
		}
	}
}

func TestRenderDiffLines_SummaryForms(t *testing.T) {
	// 【反编译】 the reference's per-change summary is one of three
	// forms: "Added %d line%s", "Removed %d line%s" and the
	// "+%d / -%d lines" pair.
	for _, tc := range []struct {
		name       string
		old, new   string
		wantPrefix string
	}{
		{"create", "", "a\nb\n", "Added 2 lines"},
		{"delete", "a\nb\n", "", "Removed 2 lines"},
		{"change", "a\nb\n", "a\nc\n", "+1 / -1 lines"},
	} {
		d := BuildEditDiff(tc.old, tc.new)
		if d == nil {
			t.Fatalf("%s: expected a diff", tc.name)
		}
		lines := RenderDiffLines(d, 80)
		if len(lines) == 0 {
			t.Fatalf("%s: expected a head line", tc.name)
		}
		if got := stripANSI(lines[0]); !strings.HasSuffix(got, tc.wantPrefix) {
			t.Errorf("%s: head = %q, want the summary %q", tc.name, got, tc.wantPrefix)
		}
	}
}

func TestRenderDiffLines_CapsTheRows(t *testing.T) {
	// 【反编译】 the reference shows at most MaxDetailLines rows of a
	// change and counts the rest in its "… and %d more lines" marker.
	var oldT, newT strings.Builder
	for i := 0; i < 40; i++ {
		oldT.WriteString("line\n")
		newT.WriteString("LINE\n")
	}
	d := BuildEditDiff(oldT.String(), newT.String())
	if d == nil {
		t.Fatal("expected a diff")
	}
	lines := RenderDiffLines(d, 80)
	if len(lines) != 1+MaxDetailLines+1 {
		t.Fatalf("got %d lines, want %d", len(lines), 1+MaxDetailLines+1)
	}
	rows := len(d.Hunks[0].Rows)
	// 【反编译】 the reference formats the marker with
	// "%*s    … and %d more lines" (0x10358a97d) — the gutter's width
	// of spaces, four more, the ellipsis and the count of the rows it
	// did not show. The clone's own indent sits in front of it.
	want := fmt.Sprintf("     %s    … and %d more lines", strings.Repeat(" ", 4), rows-MaxDetailLines)
	if got := stripANSI(lines[len(lines)-1]); got != want {
		t.Errorf("last line = %q, want %q", got, want)
	}
}

// plainLines strips the escape sequences from a rendered block.
func plainLines(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		out = append(out, stripANSI(l))
	}
	return out
}

// stripANSI removes the escape sequences from a rendered line.
func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			for i < len(s) && s[i] != 'm' {
				i++
			}
			i++
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

func TestCommandExitCode(t *testing.T) {
	res1 := "Some output.\nThe command exited with code 0."
	if code, ok := CommandExitCode(res1); !ok || code != 0 {
		t.Fatalf("expected code 0, ok true; got %d, %v", code, ok)
	}

	res2 := "Failed.\nThe command exited with code 2."
	if code, ok := CommandExitCode(res2); !ok || code != 2 {
		t.Fatalf("expected code 2, ok true; got %d, %v", code, ok)
	}

	res3 := "No exit code."
	if _, ok := CommandExitCode(res3); ok {
		t.Fatal("expected ok false for missing exit code")
	}
}
