// Copyright (c) 2026, the openagy project authors.
//
// slash_diff_folding_test.go — cycle diff-folding tests for the /diff
// file-panel hunk fold (AGY 1.3.5, changelog line 4):
//
//	"Added folding to the /diff file view: hunk headers can be collapsed
//	 with the f key, which hides the lines inside the hunk and shows just
//	 the @@ header with a + count; pressing f again expands the hunk and
//	 the cursor stays on the same header."
//
// The tests stage a single-file git repo with two non-adjacent change
// blocks (so `git diff` emits two `@@ ... @@` hunks) and exercise the
// cycle's binding surfaces:
//
//	toggle              — f collapses and re-expands one hunk
//	count indicator     — folded header carries the "+N" line-count suffix
//	cursor stays        — f on a folded header leaves the cursor on the header
//	all folded          — folding every hunk leaves only @@ headers + no body lines
//	f on non-hunk no-op — pressing f on a +/- line is a no-op
//
// All tests skip cleanly when git is unavailable so the targeted regex
// stays runnable in environments without git.

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// initDiffFoldingRepo stages a single-file git repo with two
// non-adjacent change blocks so `git diff` emits exactly two
// `@@ ... @@` hunks. The repo is rooted at dir; the file is "f.txt"
// and the delta is:
//   - L5–L7 (3 lines) changed: each becomes +CHANGED-A1 / +CHANGED-A2 / +CHANGED-A3
//   - L25–L27 (3 lines) changed: each becomes +CHANGED-B1 / +CHANGED-B2 / +CHANGED-B3
//
// The hunk separation (15 unchanged lines) keeps git from merging
// them into one big hunk via its default 3-context heuristic.
func initDiffFoldingRepo(t *testing.T, dir string) {
	t.Helper()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_AUTHOR_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	// Construct a 30-line file with two change blocks at known
	// locations. Block A occupies lines 5..7; block B occupies
	// lines 25..27. The middle 17 lines (8..24) are unchanged
	// context, which forces git to emit two separate @@ hunks.
	lines := make([]string, 30)
	for i := range lines {
		lines[i] = "ctx"
	}
	for i := 4; i <= 6; i++ {
		lines[i] = "oldA"
	}
	for i := 24; i <= 26; i++ {
		lines[i] = "oldB"
	}
	body := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	git("init", "-q")
	git("add", "-A")
	git("commit", "-qm", "init")
	// Now mutate the same lines so the diff has two hunks in the
	// modified file (not added/removed whole blocks).
	lines[4] = "CHANGED-A1"
	lines[5] = "CHANGED-A2"
	lines[6] = "CHANGED-A3"
	lines[24] = "CHANGED-B1"
	lines[25] = "CHANGED-B2"
	lines[26] = "CHANGED-B3"
	body2 := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte(body2), 0o644); err != nil {
		t.Fatal(err)
	}
}

// openFoldingDiffSubview sets up m with cwd=dir, opens the diff
// panel on tab 0 (All Changes), moves the cursor to row 0 (the
// single tracked file "f.txt") and presses Enter — the standard
// hand-off from the file list to the inline-diff subview. The
// returned *runMode is ready for f/j/k bindings.
func openFoldingDiffSubview(t *testing.T, dir string) *runMode {
	t.Helper()
	m := mkPanel(panelDiff, "/diff", 0)
	m.cwd = dir
	m.panel.tab = 0
	m.panel.cursor = 0
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEnter}); !handled {
		t.Fatal("Enter not handled")
	}
	if m.panel.diffView != 1 {
		t.Fatalf("diffView = %d, want 1 (file inline)", m.panel.diffView)
	}
	return m
}

// indexOfPrefix returns the first index where plain[i] starts with
// prefix — skipping the leading two-space indent + " > " cursor
// marker — or -1 if no row matches. Used by the tests to locate the
// @@ header / specific body lines without depending on row indices
// the renderer is free to rearrange.
func indexOfPrefix(plain []string, prefix string) int {
	for i, l := range plain {
		if rowStartsWith(l, prefix) {
			return i
		}
	}
	return -1
}

// diffInlineBodyRows returns the rendered rows minus the title row,
// leading blank row, trailing blank row, and legend row — the rows
// whose cursor index is diffInlineRow. Used by every test to map
// the cursor onto a stable row set.
//
// Each body row has a leading 2-space indent ("  " before @@/+/-/context)
// plus, on the cursor row, an extra " > " blue marker prefix. The
// helper strips neither — tests inspect prefix/content directly
// and account for the indent + cursor row explicitly via the
// rowStartsWith helper below.
//
// Layout: panelDiffInlineLines emits [title, blank, body..., blank,
// legend]. body is rows[2..N-3] — the slice between the leading
// blank and the trailing blank. The trailing blank + legend pair
// is dropped by walking back from the end to find the legend (the
// last non-empty row).
func diffInlineBodyRows(plain []string) []string {
	if len(plain) < 5 {
		return nil
	}
	// Find the legend: the last non-empty row.
	legendIdx := -1
	for i := len(plain) - 1; i >= 0; i-- {
		if plain[i] != "" {
			legendIdx = i
			break
		}
	}
	if legendIdx < 0 || legendIdx < 3 {
		return nil
	}
	// Everything between row 2 and row legendIdx-2 is body content:
	//   [title=0, blank=1, body=2..legendIdx-2, blank=legendIdx-1, legend=legendIdx]
	bodyStart := 2
	bodyEnd := legendIdx - 1
	if bodyStart >= bodyEnd {
		return nil
	}
	return plain[bodyStart:bodyEnd]
}

// rowStartsWith reports whether s has the given content prefix
// once the leading two-space indent (and any " > " cursor marker)
// have been skipped. The renderer's standard indent is two spaces,
// and the cursor-marked row adds a third space + "> ", producing
// rows like " >   diff --git..."; this helper treats both forms
// identically so tests don't have to special-case the cursor row.
func rowStartsWith(s, prefix string) bool {
	return strings.HasPrefix(stripRowPrefix(s), prefix)
}

// stripRowPrefix returns s with the leading "  " (renderer
// standard indent) and any " > " cursor marker stripped, leaving
// the content tail. rowStartsWith is built on top of this helper so
// the prefix-stripping logic only lives in one place.
func stripRowPrefix(s string) string {
	strip := s
	// Skip leading spaces.
	for len(strip) > 0 && strip[0] == ' ' {
		strip = strip[1:]
	}
	// Skip the cursor marker, if present.
	if strings.HasPrefix(strip, "> ") {
		strip = strip[2:]
	}
	// Re-skip any spaces that followed the cursor marker — the
	// cursor-row prefix is " > " + "  " + content, so once the
	// marker is gone there are still two spaces of indent left.
	for len(strip) > 0 && strip[0] == ' ' {
		strip = strip[1:]
	}
	return strip
}

// TestDiffFolding_Toggle: two-hunk diff, f on first hunk hides it,
// f again unhides. The body rows go from N (with both hunks
// expanded) to 1 (just the first @@ header) on first f, then back
// to N on second f.
func TestDiffFolding_Toggle(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	initDiffFoldingRepo(t, dir)

	m := openFoldingDiffSubview(t, dir)

	// Sanity: both hunks show their @@ and body lines when no fold.
	lines0 := stripInlineLines(t, m)
	body0 := diffInlineBodyRows(lines0)
	if n := countPrefix(body0, "@@"); n != 2 {
		t.Fatalf("baseline @@ count = %d, want 2", n)
	}
	// All expanded: body rows include both hunks' plus/minus/context.
	if !anyContains(lines0, "CHANGED-A1") || !anyContains(lines0, "CHANGED-B1") {
		t.Fatalf("baseline body missing A/B content: %q", lines0)
	}

	// Move cursor onto the first @@ header. The body rows start at
	// rendered row index 2 (after title + blank); @@ headers are at
	// body rows 4 (after 4 pre-header lines: "diff --git", "index",
	// "---", "+++") and later. We find the first @@ row in the body
	// and snap the cursor onto it.
	cursorOnFirstHunkHeader(t, m, lines0)

	// Press f — first hunk collapses.
	pressF(t, m)
	lines1 := stripInlineLines(t, m)
	body1 := diffInlineBodyRows(lines1)
	if n := countPrefix(body1, "@@"); n != 2 {
		t.Errorf("after f: @@ count = %d, want 2", n)
	}
	// First hunk's body should be gone; second hunk's body should still be visible.
	if anyContains(body1, "CHANGED-A1") || anyContains(body1, "CHANGED-A2") || anyContains(body1, "CHANGED-A3") {
		t.Errorf("after f: hunk A still showing body content: %q", body1)
	}
	if !anyContains(body1, "CHANGED-B1") {
		t.Errorf("after f: hunk B body missing: %q", body1)
	}

	// Press f again — first hunk re-expands.
	pressF(t, m)
	lines2 := stripInlineLines(t, m)
	body2 := diffInlineBodyRows(lines2)
	if !anyContains(body2, "CHANGED-A1") || !anyContains(body2, "CHANGED-B1") {
		t.Errorf("after f twice: hunk content missing: %q", body2)
	}
	if n := countPrefix(body2, "@@"); n != 2 {
		t.Errorf("after f twice: @@ count = %d, want 2", n)
	}
}

// TestDiffFolding_CountIndicator: folded hunk's header includes
// "+N" line-count suffix — the prompt explicitly mentions "+5" as
// the example. The exact N is whatever len(body) is for the hunk
// being folded; this test pins it as part of the regression.
func TestDiffFolding_CountIndicator(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	initDiffFoldingRepo(t, dir)

	m := openFoldingDiffSubview(t, dir)

	// Find the first @@ header in the body; capture its dedented
	// plain form before folding so we can compare against the
	// folded form (skip the leading "  " indent and any " > "
	// cursor marker — see rowStartsWith).
	lines0 := stripInlineLines(t, m)
	body0 := diffInlineBodyRows(lines0)
	idx0 := indexOfPrefix(body0, "@@")
	if idx0 < 0 {
		t.Fatalf("no @@ header in body: %q", body0)
	}
	header0 := stripRowPrefix(body0[idx0])

	cursorOnFirstHunkHeader(t, m, lines0)
	pressF(t, m)

	lines1 := stripInlineLines(t, m)
	body1 := diffInlineBodyRows(lines1)

	// The first @@ header line should now carry "+N" suffix.
	idx1 := indexOfPrefix(body1, "@@")
	if idx1 < 0 {
		t.Fatalf("folded body has no @@ header: %q", body1)
	}
	got := stripRowPrefix(body1[idx1])
	if !strings.HasPrefix(got, header0+" ") {
		t.Fatalf("folded header %q does not start with unfolded header %q", got, header0)
	}
	// The " +N" suffix must be present and numeric. Regex-free
	// parse: the suffix lives after the trailing "@@" — count it.
	if !strings.Contains(got, "+") {
		t.Errorf("folded header missing + count: %q", got)
	}
	// Extract the trailing numeric token: split on whitespace and
	// take the last field.
	fields := strings.Fields(got)
	if len(fields) < 2 {
		t.Fatalf("folded header has no count token: %q", got)
	}
	tail := fields[len(fields)-1]
	if !strings.HasPrefix(tail, "+") {
		t.Errorf("folded header tail %q does not start with +", tail)
	}
	numStr := strings.TrimPrefix(tail, "+")
	if numStr == "" {
		t.Errorf("folded header tail %q has no digits after +", tail)
	}
	for _, r := range numStr {
		if r < '0' || r > '9' {
			t.Errorf("folded header tail %q has non-digit %q", tail, r)
			break
		}
	}
}

// TestDiffFolding_CursorStays: cursor on a folded hunk header,
// pressing f expands; cursor remains on the header.
//
// The first phase folds the first hunk, leaving the cursor (per the
// design) on the (now-collapsed) @@ header row. The second phase
// presses f again to expand; the cursor must stay on the @@ header
// of the same hunk — that's the "cursor remains on the same header"
// rule from the changelog.
func TestDiffFolding_CursorStays(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	initDiffFoldingRepo(t, dir)

	m := openFoldingDiffSubview(t, dir)
	lines0 := stripInlineLines(t, m)
	cursorOnFirstHunkHeader(t, m, lines0)
	startCursor := m.panel.diffInlineRow

	// Fold — cursor should remain on the @@ header.
	pressF(t, m)
	if m.panel.diffInlineRow != startCursor {
		t.Errorf("after fold: cursor = %d, want %d (header)", m.panel.diffInlineRow, startCursor)
	}
	linesF := stripInlineLines(t, m)
	bodyF := diffInlineBodyRows(linesF)
	if m.panel.diffInlineRow >= len(bodyF) {
		t.Fatalf("cursor row %d out of range, body len %d", m.panel.diffInlineRow, len(bodyF))
	}
	if !rowStartsWith(bodyF[m.panel.diffInlineRow], "@@") {
		t.Errorf("cursor row %d after fold not @@: %q", m.panel.diffInlineRow, bodyF[m.panel.diffInlineRow])
	}

	// Unfold — cursor must stay on the @@ header row (which is
	// now pushed down by len(body) of the unfolded hunk; the
	// clamping in toggleDiffHunkFoldAtCursor re-anchors it
	// onto the same header).
	pressF(t, m)
	if m.panel.diffInlineRow != startCursor {
		// The startCursor was relative to the unfolded body. After
		// unfolding, the layout returns to the unfolded shape with
		// the same row indices, so startCursor must still point at
		// the same @@ header row.
		t.Errorf("after unfold: cursor = %d, want %d (header)", m.panel.diffInlineRow, startCursor)
	}
	linesU := stripInlineLines(t, m)
	bodyU := diffInlineBodyRows(linesU)
	if m.panel.diffInlineRow >= len(bodyU) {
		t.Fatalf("cursor row %d out of range after unfold, body len %d",
			m.panel.diffInlineRow, len(bodyU))
	}
	if !rowStartsWith(bodyU[m.panel.diffInlineRow], "@@") {
		t.Errorf("cursor row %d after unfold not @@: %q",
			m.panel.diffInlineRow, bodyU[m.panel.diffInlineRow])
	}
}

// TestDiffFolding_AllFolded: pressing f on every hunk in sequence
// leaves only @@ headers (one per hunk) and no body lines. The
// folded state is achieved by walking each @@ header and toggling
// fold; the final body should have N @@ rows (where N == hunks)
// and zero +/- or context lines.
func TestDiffFolding_AllFolded(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	initDiffFoldingRepo(t, dir)

	m := openFoldingDiffSubview(t, dir)
	lines := stripInlineLines(t, m)
	body := diffInlineBodyRows(lines)
	hunkCount := countPrefix(body, "@@")
	if hunkCount != 2 {
		t.Fatalf("baseline @@ count = %d, want 2 for the two-hunk test fixture", hunkCount)
	}

	// The number of pre-header rows is whatever the diff has
	// before the first @@. For a modified tracked file, that's
	// the four lines "diff --git", "index", "---", "+++". Pin
	// via the baseline render so we don't miscount for unusual
	// diff shapes later on.
	preHeaderCount := 0
	for _, l := range body {
		if rowStartsWith(l, "@@") {
			break
		}
		preHeaderCount++
	}

	// Fold each @@ in turn: navigate to the next @@ header (the
	// one at or after the cursor's row + 1, so re-pressing f on
	// the just-folded @@ does not toggle it back open), then press
	// f. The cursor follows the @@ header across the toggle.
	for i := 0; i < hunkCount; i++ {
		lines = stripInlineLines(t, m)
		body = diffInlineBodyRows(lines)
		// Start the search one row past the cursor — the cursor
		// is on the @@ header of the just-folded hunk (or, on
		// the first iteration, the unfolded hunk A header), and
		// we want the NEXT @@ in document order, not the same
		// one again.
		cur := m.panel.diffInlineRow
		target := cur + 1
		// Clamp into range before scanning so we report the
		// correct row count if no further @@ exists.
		if target >= len(body) {
			t.Fatalf("no further @@ after cursor row %d (i=%d, body=%q)", cur, i, body)
		}
		for target < len(body) && !rowStartsWith(body[target], "@@") {
			target++
		}
		if target >= len(body) {
			t.Fatalf("could not find next @@ at i=%d, body=%q", i, body)
		}
		m.panel.diffInlineRow = target
		// Toggle.
		pressF(t, m)
	}

	// Final state: every @@ is folded.
	lines = stripInlineLines(t, m)
	body = diffInlineBodyRows(lines)
	if n := countPrefix(body, "@@"); n != hunkCount {
		t.Errorf("final @@ count = %d, want %d", n, hunkCount)
	}
	// No body content remains.
	for _, want := range []string{"CHANGED-A1", "CHANGED-A2", "CHANGED-A3", "CHANGED-B1", "CHANGED-B2", "CHANGED-B3"} {
		if anyContains(body, want) {
			t.Errorf("after all-folded: body still contains %q: %q", want, body)
		}
	}
	// The remaining rendered rows (between body and legend) should
	// be exactly the pre-header lines plus the hunkCount folded
	// @@ headers (no hunk bodies and no trailing context).
	wantLen := preHeaderCount + hunkCount
	if len(body) != wantLen {
		t.Errorf("after all-folded: body length = %d, want %d (pre-header %d + hunkCount %d)",
			len(body), wantLen, preHeaderCount, hunkCount)
	}
}

// TestDiffFolding_FOnNonHunkNoOp: pressing f on a +/- line does
// nothing. The fold state stays at zero entries (no header has
// been folded), and the body shape is byte-identical to the
// pre-f baseline.
func TestDiffFolding_FOnNonHunkNoOp(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	initDiffFoldingRepo(t, dir)

	m := openFoldingDiffSubview(t, dir)
	lines0 := stripInlineLines(t, m)
	body0 := diffInlineBodyRows(lines0)

	// Find a "+" or "-" line in the body, advance the cursor onto
	// it. The change blocks both use the +CHANGED-* markers.
	idxPlus := -1
	for i, l := range body0 {
		if rowStartsWith(l, "+CHANGED-") {
			idxPlus = i
			break
		}
	}
	if idxPlus < 0 {
		t.Fatalf("no +CHANGED- line in body: %q", body0)
	}
	m.panel.diffInlineRow = idxPlus

	pressF(t, m)

	// After f, render and confirm body shape is unchanged: same
	// @@ count (both expanded), same change lines visible.
	lines1 := stripInlineLines(t, m)
	body1 := diffInlineBodyRows(lines1)
	if n := countPrefix(body1, "@@"); n != 2 {
		t.Errorf("after f on +/- line: @@ count = %d, want 2 (no fold)", n)
	}
	if !anyContains(body1, "CHANGED-A1") || !anyContains(body1, "CHANGED-B1") {
		t.Errorf("after f on +/- line: body content lost: %q", body1)
	}
	// Folded map should be empty.
	if len(m.panel.diffFolded) != 0 {
		t.Errorf("after f on +/- line: diffFolded map has %d entries, want 0",
			len(m.panel.diffFolded))
	}
}

// pressF sends a bare 'f' key through handlePanelKey and asserts
// the panel still owns the key (returns true). The diff subview's
// 'f' binding returns true on both the toggle branch and the
// fall-through no-op branch, so this is the same contract as every
// other panel-owning letter.
func pressF(t *testing.T, m *runMode) {
	t.Helper()
	if handled, _ := m.handlePanelKey(tea.Key{Text: "f", Code: 'f'}); !handled {
		t.Fatal("f not handled")
	}
}

// cursorOnFirstHunkHeader advances m.panel.diffInlineRow to the
// first "@@" row in the body. Assumes lines have just been
// rendered (so the cursor mapping is current). Reports the
// obvious failure when the body has no @@ rows.
func cursorOnFirstHunkHeader(t *testing.T, m *runMode, lines []string) {
	t.Helper()
	body := diffInlineBodyRows(lines)
	idx := indexOfPrefix(body, "@@")
	if idx < 0 {
		t.Fatalf("no @@ header in body: %q", body)
	}
	m.panel.diffInlineRow = idx
}

// stripInlineLines renders the inline diff and strips SGR. A
// t.Helper so failures include the caller's line.
func stripInlineLines(t *testing.T, m *runMode) []string {
	t.Helper()
	raw := m.panelDiffInlineLines()
	out := make([]string, len(raw))
	for i, l := range raw {
		out[i] = stripSGR(l)
	}
	return out
}

// countPrefix returns the number of rows in lines that start with
// prefix — skipping the leading two-space indent + " > " cursor
// marker. Used by the tests to count "@@" headers regardless of
// their internal variation (different hunk ranges produce
// different "@@ -A,B +C,D @@" forms).
func countPrefix(lines []string, prefix string) int {
	n := 0
	for _, l := range lines {
		if rowStartsWith(l, prefix) {
			n++
		}
	}
	return n
}

// anyContains reports whether at least one row of lines contains
// substr. A small helper for the positive/negative change-line
// presence checks.
func anyContains(lines []string, substr string) bool {
	for _, l := range lines {
		if strings.Contains(l, substr) {
			return true
		}
	}
	return false
}
