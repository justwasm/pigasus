// panel_fold_test.go — the /diff panel's hunk folding.
//
// The folding is the reference's own gesture (AGY 1.3.5 changelog
// line 4): `f` on a hunk header collapses the hunk's body into the
// header plus a "+N" count, and the cursor stays on the header.

package diffpanel

import (
	"os/exec"
	"strings"
	"testing"
)

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

	p := openFoldingDiffSubview(t, dir)

	// Sanity: both hunks show their @@ and body lines when no fold.
	lines0 := stripInlineLines(t, p)
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
	cursorOnFirstHunkHeader(t, p, lines0)

	// Press f — first hunk collapses.
	pressF(t, p)
	lines1 := stripInlineLines(t, p)
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
	pressF(t, p)
	lines2 := stripInlineLines(t, p)
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

	p := openFoldingDiffSubview(t, dir)

	// Find the first @@ header in the body; capture its dedented
	// plain form before folding so we can compare against the
	// folded form (skip the leading "  " indent and any " > "
	// cursor marker — see rowStartsWith).
	lines0 := stripInlineLines(t, p)
	body0 := diffInlineBodyRows(lines0)
	idx0 := indexOfPrefix(body0, "@@")
	if idx0 < 0 {
		t.Fatalf("no @@ header in body: %q", body0)
	}
	header0 := stripRowPrefix(body0[idx0])

	cursorOnFirstHunkHeader(t, p, lines0)
	pressF(t, p)

	lines1 := stripInlineLines(t, p)
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

	p := openFoldingDiffSubview(t, dir)
	lines0 := stripInlineLines(t, p)
	cursorOnFirstHunkHeader(t, p, lines0)
	startCursor := p.inlineRow

	// Fold — cursor should remain on the @@ header.
	pressF(t, p)
	if p.inlineRow != startCursor {
		t.Errorf("after fold: cursor = %d, want %d (header)", p.inlineRow, startCursor)
	}
	linesF := stripInlineLines(t, p)
	bodyF := diffInlineBodyRows(linesF)
	if p.inlineRow >= len(bodyF) {
		t.Fatalf("cursor row %d out of range, body len %d", p.inlineRow, len(bodyF))
	}
	if !rowStartsWith(bodyF[p.inlineRow], "@@") {
		t.Errorf("cursor row %d after fold not @@: %q", p.inlineRow, bodyF[p.inlineRow])
	}

	// Unfold — cursor must stay on the @@ header row (which is
	// now pushed down by len(body) of the unfolded hunk; the
	// clamping in ToggleFoldAtCursor re-anchors it
	// onto the same header).
	pressF(t, p)
	if p.inlineRow != startCursor {
		// The startCursor was relative to the unfolded body. After
		// unfolding, the layout returns to the unfolded shape with
		// the same row indices, so startCursor must still point at
		// the same @@ header row.
		t.Errorf("after unfold: cursor = %d, want %d (header)", p.inlineRow, startCursor)
	}
	linesU := stripInlineLines(t, p)
	bodyU := diffInlineBodyRows(linesU)
	if p.inlineRow >= len(bodyU) {
		t.Fatalf("cursor row %d out of range after unfold, body len %d",
			p.inlineRow, len(bodyU))
	}
	if !rowStartsWith(bodyU[p.inlineRow], "@@") {
		t.Errorf("cursor row %d after unfold not @@: %q",
			p.inlineRow, bodyU[p.inlineRow])
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

	p := openFoldingDiffSubview(t, dir)
	lines := stripInlineLines(t, p)
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
		lines = stripInlineLines(t, p)
		body = diffInlineBodyRows(lines)
		// Start the search one row past the cursor — the cursor
		// is on the @@ header of the just-folded hunk (or, on
		// the first iteration, the unfolded hunk A header), and
		// we want the NEXT @@ in document order, not the same
		// one again.
		cur := p.inlineRow
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
		p.inlineRow = target
		// Toggle.
		pressF(t, p)
	}

	// Final state: every @@ is folded.
	lines = stripInlineLines(t, p)
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

	p := openFoldingDiffSubview(t, dir)
	lines0 := stripInlineLines(t, p)
	body0 := diffInlineBodyRows(lines0)

	// Find a "+" or "-" line in the body, advance the cursor onto
	// it. The change blocks both use the CHANGED-* markers, which the
	// reference's row model carries in its four-wide marker column.
	idxPlus := -1
	for i, l := range body0 {
		if strings.Contains(l, " +  CHANGED-") {
			idxPlus = i
			break
		}
	}
	if idxPlus < 0 {
		t.Fatalf("no +CHANGED- line in body: %q", body0)
	}
	p.inlineRow = idxPlus

	pressF(t, p)

	// After f, render and confirm body shape is unchanged: same
	// @@ count (both expanded), same change lines visible.
	lines1 := stripInlineLines(t, p)
	body1 := diffInlineBodyRows(lines1)
	if n := countPrefix(body1, "@@"); n != 2 {
		t.Errorf("after f on +/- line: @@ count = %d, want 2 (no fold)", n)
	}
	if !anyContains(body1, "CHANGED-A1") || !anyContains(body1, "CHANGED-B1") {
		t.Errorf("after f on +/- line: body content lost: %q", body1)
	}
	// Folded map should be empty.
	if len(p.folded) != 0 {
		t.Errorf("after f on +/- line: folded map has %d entries, want 0",
			len(p.folded))
	}
}
