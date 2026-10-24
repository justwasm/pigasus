package tuitools

import (
	"encoding/json"
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
