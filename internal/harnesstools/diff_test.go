// diff_test.go — the harness's diff blocks.
package harnesstools

import "testing"

// TestDiff_SeveralChangesMakeSeveralBlocks pins the real line diff.
//
// The reference computes a real line diff, so a file with two changes
// that are more than a context window apart produces two blocks — the
// protocol's diff_block field is a repeated one and carries them all.
// The prefix/suffix diff this replaced reported both changes as one
// region.
func TestDiff_SeveralChangesMakeSeveralBlocks(t *testing.T) {
	old := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", ""}
	updated := []string{"a", "B", "c", "d", "e", "f", "g", "h", "i", "J", "k", "l", ""}

	blocks, ok := Diff(old, updated)
	if !ok {
		t.Fatal("expected a diff")
	}
	if len(blocks) != 2 {
		t.Fatalf("diff blocks = %d, want 2", len(blocks))
	}

	// The first block covers the change at "b" with three lines of
	// context on each side.
	if blocks[0].StartLine != 0 || blocks[0].EndLine != 5 {
		t.Errorf("first block range = %d..%d, want 0..5", blocks[0].StartLine, blocks[0].EndLine)
	}
	wantFirst := []DiffLine{
		{Text: "a", Action: LineNone},
		{Text: "b", Action: LineDelete},
		{Text: "B", Action: LineInsert},
		{Text: "c", Action: LineNone},
		{Text: "d", Action: LineNone},
		{Text: "e", Action: LineNone},
	}
	if len(blocks[0].Lines) != len(wantFirst) {
		t.Fatalf("first block lines = %d, want %d", len(blocks[0].Lines), len(wantFirst))
	}
	for i, line := range blocks[0].Lines {
		if line != wantFirst[i] {
			t.Errorf("first block line %d = %+v, want %+v", i, line, wantFirst[i])
		}
	}

	// The second block covers the change at "j", again with three lines
	// of context on each side.
	if blocks[1].StartLine != 6 || blocks[1].EndLine != 13 {
		t.Errorf("second block range = %d..%d, want 6..13", blocks[1].StartLine, blocks[1].EndLine)
	}
	wantSecond := []DiffLine{
		{Text: "g", Action: LineNone},
		{Text: "h", Action: LineNone},
		{Text: "i", Action: LineNone},
		{Text: "j", Action: LineDelete},
		{Text: "J", Action: LineInsert},
		{Text: "k", Action: LineNone},
		{Text: "l", Action: LineNone},
		{Text: "", Action: LineNone},
	}
	if len(blocks[1].Lines) != len(wantSecond) {
		t.Fatalf("second block lines = %d, want %d", len(blocks[1].Lines), len(wantSecond))
	}
	for i, line := range blocks[1].Lines {
		if line != wantSecond[i] {
			t.Errorf("second block line %d = %+v, want %+v", i, line, wantSecond[i])
		}
	}
}

// TestDiff_UnchangedFilesHaveNoBlock pins the "no change" answer.
func TestDiff_UnchangedFilesHaveNoBlock(t *testing.T) {
	if _, ok := Diff([]string{"a", "b", ""}, []string{"a", "b", ""}); ok {
		t.Error("expected no diff for two identical files")
	}
}
