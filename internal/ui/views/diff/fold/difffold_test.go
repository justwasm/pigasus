package fold

import (
	"strings"
	"testing"
)

// TestSplitHunks pins the partition: the pre-header block, then one Hunk
// per "@@" header with the lines up to the next header as its body.
func TestSplitHunks(t *testing.T) {
	body := []string{
		"diff --git a/x.go b/x.go",
		"index 111..222 100644",
		"--- a/x.go",
		"+++ b/x.go",
		"@@ -1,3 +1,4 @@",
		" ctx",
		"-old",
		"+new",
		"@@ -10,2 +11,2 @@ func f()",
		" ctx2",
		"-old2",
	}
	pre, hunks := SplitHunks(body)
	if len(pre) != 4 {
		t.Fatalf("pre = %q, want 4 lines", pre)
	}
	if pre[0] != "diff --git a/x.go b/x.go" {
		t.Errorf("pre[0] = %q", pre[0])
	}
	if len(hunks) != 2 {
		t.Fatalf("hunks = %d, want 2", len(hunks))
	}
	if hunks[0].Header != "@@ -1,3 +1,4 @@" {
		t.Errorf("hunks[0].Header = %q", hunks[0].Header)
	}
	if len(hunks[0].Body) != 3 {
		t.Errorf("hunks[0].Body = %q, want 3 lines", hunks[0].Body)
	}
	if hunks[1].Header != "@@ -10,2 +11,2 @@ func f()" {
		t.Errorf("hunks[1].Header = %q", hunks[1].Header)
	}
	if len(hunks[1].Body) != 2 {
		t.Errorf("hunks[1].Body = %q, want 2 lines", hunks[1].Body)
	}
}

// TestSplitHunksNoHunks pins the hunkless case: everything is pre-header.
func TestSplitHunksNoHunks(t *testing.T) {
	body := []string{"diff --git a/x b/x", "index 1..2 100644"}
	pre, hunks := SplitHunks(body)
	if len(hunks) != 0 {
		t.Errorf("hunks = %d, want 0", len(hunks))
	}
	if len(pre) != 2 {
		t.Errorf("pre = %q, want 2 lines", pre)
	}
}

// TestSplitHunksEmptyBody pins the hunk with no body lines.
func TestSplitHunksEmptyBody(t *testing.T) {
	pre, hunks := SplitHunks([]string{"@@ -1 +1 @@", "@@ -2 +2 @@"})
	if len(pre) != 0 {
		t.Errorf("pre = %q, want empty", pre)
	}
	if len(hunks) != 2 {
		t.Fatalf("hunks = %d, want 2", len(hunks))
	}
	if len(hunks[0].Body) != 0 {
		t.Errorf("hunks[0].Body = %q, want empty", hunks[0].Body)
	}
}

// TestCountAndFoldHint pins the "+N" indicator.
func TestCountAndFoldHint(t *testing.T) {
	h := Hunk{Header: "@@ -1,2 +1,3 @@", Body: []string{" a", " b"}}
	if got := Count(h); got != 2 {
		t.Errorf("Count() = %d, want 2", got)
	}
	if got := FoldHint(h, "<", ">"); got != "<+2>" {
		t.Errorf("FoldHint() = %q, want %q", got, "<+2>")
	}
}

// TestRenderBody pins the folded and expanded renderings.
func TestRenderBody(t *testing.T) {
	style := func(s string) string { return s }
	pre, hunks := SplitHunks([]string{"hdr", "@@ -1 +1 @@", " a", " b"})
	if pre == nil || len(hunks) != 1 {
		t.Fatalf("split = %q / %d hunks", pre, len(hunks))
	}

	// Expanded: the pre-header, the header and its body.
	rows, layout := RenderBody(pre, hunks, nil, style, "<", ">")
	if len(rows) != 4 {
		t.Fatalf("expanded rows = %q, want 4", rows)
	}
	if rows[1] != "  @@ -1 +1 @@" {
		t.Errorf("expanded header row = %q", rows[1])
	}
	if len(layout) != 1 || layout[0].RowCount != 3 {
		t.Errorf("expanded layout = %+v, want one hunk of 3 rows", layout)
	}
	if layout[0].StartRow != 1 {
		t.Errorf("expanded StartRow = %d, want 1", layout[0].StartRow)
	}

	// Folded: the header plus the "+N" hint, and no body.
	rows, layout = RenderBody(pre, hunks, map[int]bool{0: true}, style, "<", ">")
	if len(rows) != 2 {
		t.Fatalf("folded rows = %q, want 2", rows)
	}
	if rows[1] != "  @@ -1 +1 @@ <+2>" {
		t.Errorf("folded header row = %q", rows[1])
	}
	if len(layout) != 1 || layout[0].RowCount != 1 {
		t.Errorf("folded layout = %+v, want one hunk of 1 row", layout)
	}
}

// TestHunkIdxForRow pins the cursor-to-hunk lookup: only a header row
// resolves, a body row does not.
func TestHunkIdxForRow(t *testing.T) {
	layout := []Layout{
		{StartRow: 1, RowCount: 3},
		{StartRow: 4, RowCount: 2},
	}
	for _, tc := range []struct {
		row  int
		want int
		ok   bool
	}{
		{1, 0, true},
		{4, 1, true},
		{2, -1, false},
		{3, -1, false},
		{5, -1, false},
		{99, -1, false},
		{-1, -1, false},
	} {
		got, ok := HunkIdxForRow(layout, tc.row)
		if got != tc.want || ok != tc.ok {
			t.Errorf("HunkIdxForRow(%d) = (%d, %v), want (%d, %v)", tc.row, got, ok, tc.want, tc.ok)
		}
	}
}

// TestFoldedLayoutCursorRoundTrip pins the invariant the toggler relies
// on: a folded hunk's header row still resolves to the same hunk index.
func TestFoldedLayoutCursorRoundTrip(t *testing.T) {
	style := func(s string) string { return s }
	pre, hunks := SplitHunks([]string{"@@ -1 +1 @@", " a", "@@ -5 +5 @@", " b"})
	rows, layout := RenderBody(pre, hunks, map[int]bool{0: true}, style, "<", ">")
	if len(rows) != 3 {
		t.Fatalf("rows = %q, want 3", rows)
	}
	idx, ok := HunkIdxForRow(layout, 0)
	if !ok || idx != 0 {
		t.Errorf("HunkIdxForRow(0) = (%d, %v), want (0, true)", idx, ok)
	}
	idx, ok = HunkIdxForRow(layout, 1)
	if !ok || idx != 1 {
		t.Errorf("HunkIdxForRow(1) = (%d, %v), want (1, true)", idx, ok)
	}
	if !strings.HasPrefix(rows[0], "  @@ -1 +1 @@ ") {
		t.Errorf("folded row = %q", rows[0])
	}
}
