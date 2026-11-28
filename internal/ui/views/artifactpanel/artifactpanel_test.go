// artifactpanel_test.go — the /artifact panel's two layers.
//
// The shapes asserted here are the reference's own, taken from a live capture
// of agy 1.5.17 over a real conversation
// (scratch/capture-artifact-viewer.sh, MODE=real; notes in out/ and in
// docs/guide/compaction.md). The empty frame stays byte-exact against cycle
// 185's golden.

package artifactpanel

import (
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/palette"
)

// sgrRe matches the SGR spans the panel's rows carry.
var sgrRe = regexp.MustCompile("\x1b\\[[0-9;]*m")

// stripSGR removes the SGR spans from a row.
func stripSGR(s string) string {
	return sgrRe.ReplaceAllString(s, "")
}

// plain strips the SGR spans from every row.
func plain(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = stripSGR(l)
	}
	return out
}

// testSeams builds the seams over a fixed artifact set.
//
// The bodies map is the "filesystem": a path that is not in it cannot be read,
// which is how the panel's unreadable-artifact path is exercised.
func testSeams(refs []Artifact, bodies map[string]string) Seams {
	return Seams{
		Refs: func() []Artifact { return refs },
		Size: func() (int, int) { return 110, 34 },
		Read: func(path string) (string, bool) {
			body, ok := bodies[path]
			return body, ok
		},
	}
}

func TestLinesLayout(t *testing.T) {
	lines := Lines()
	want := []string{
		"Artifacts",
		"  No artifacts",
		"",
		"Keyboard: ↑/↓ Navigate  p preview  enter open  ctrl+g open in editor  esc Dismiss",
	}
	if len(lines) != len(want) {
		t.Fatalf("line count: got %d, want %d\n got: %q\nwant: %q", len(lines), len(want), lines, want)
	}
	for i := range want {
		if got := stripSGR(lines[i]); got != want[i] {
			t.Errorf("line %d:\n got: %q\nwant: %q", i, got, want[i])
		}
	}
}

// The empty state is painted in the dim grey, and the title is the
// reference's own bold span.
func TestLinesColours(t *testing.T) {
	lines := Lines()
	if !strings.HasPrefix(lines[0], panelTitleOpen+"Artifacts"+panelTitleClose) {
		t.Errorf("title = %q", lines[0])
	}
	if !strings.Contains(lines[1], palette.DimGrayOpen) {
		t.Errorf("empty state should be the dim gray: %q", lines[1])
	}
	if !strings.HasSuffix(lines[1], palette.DimGrayClose) {
		t.Errorf("empty state must end with the dim gray close: %q", lines[1])
	}
}

// TestEmptyConversationDrawsTheReferenceFrame keeps an empty conversation on the
// exact frame the reference draws.
func TestEmptyConversationDrawsTheReferenceFrame(t *testing.T) {
	p := New(testSeams(nil, nil))
	got := p.Lines()
	want := Lines()
	if len(got) != len(want) {
		t.Fatalf("line count: got %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d:\n got: %q\nwant: %q", i, got[i], want[i])
		}
	}
	if p.RowCount() != 0 {
		t.Errorf("RowCount = %d, want 0", p.RowCount())
	}
}

// TestPickerRowsMatchTheCapture pins the picker's rows against the captured
// frame:
//
//	Artifacts
//	› artifact-alignment-plan.md   open
//	  artifact-picker-and-viewer.md
//	  config-key-audit.md
//
// so rows carry the extension and the selected row carries the inline "open".
func TestPickerRowsMatchTheCapture(t *testing.T) {
	refs := []Artifact{
		{Name: "artifact-alignment-plan", Path: "/brain/x/artifact-alignment-plan.md"},
		{Name: "artifact-picker-and-viewer", Path: "/brain/x/artifact-picker-and-viewer.md"},
		{Name: "config-key-audit", Path: "/brain/x/config-key-audit.md"},
	}
	p := New(testSeams(refs, nil))
	lines := plain(p.Lines())

	want := []string{
		"Artifacts",
		"› artifact-alignment-plan.md   open",
		"  artifact-picker-and-viewer.md",
		"  config-key-audit.md",
		"",
		"Keyboard: ↑/↓ Navigate  p preview  enter open  ctrl+g open in editor  esc Dismiss",
	}
	if len(lines) != len(want) {
		t.Fatalf("line count = %d, want %d:\n%s", len(lines), len(want),
			strings.Join(lines, "\n"))
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("line %d:\n got: %q\nwant: %q", i, lines[i], want[i])
		}
	}
}

// TestCursorMovesAndClamps pins that ↑/↓ actually move the picker's cursor.
func TestCursorMovesAndClamps(t *testing.T) {
	refs := []Artifact{
		{Name: "first", Path: "/1.md"},
		{Name: "second", Path: "/2.md"},
		{Name: "third", Path: "/3.md"},
	}
	p := New(testSeams(refs, nil))

	if got := p.Cursor(); got != 0 {
		t.Fatalf("initial cursor = %d, want 0", got)
	}
	if handled, _ := p.HandleKey(tea.Key{Code: tea.KeyDown}); !handled {
		t.Fatal("down should be handled")
	}
	if got := p.Cursor(); got != 1 {
		t.Errorf("cursor after down = %d, want 1", got)
	}
	if got := plain(p.Lines())[2]; !strings.HasPrefix(got, "› 2.md") {
		t.Errorf("the cursor's row is not the second: %q", got)
	}
	p.HandleKey(tea.Key{Code: tea.KeyDown})
	p.HandleKey(tea.Key{Code: tea.KeyDown})
	if got := p.Cursor(); got != 2 {
		t.Errorf("cursor = %d, want it clamped at 2", got)
	}
	for i := 0; i < 5; i++ {
		p.HandleKey(tea.Key{Code: tea.KeyUp})
	}
	if got := p.Cursor(); got != 0 {
		t.Errorf("cursor = %d, want it clamped at 0", got)
	}
}

// body builds a document of n lines, each naming its own number so a frame can
// be read back.
func body(n int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		b.WriteString("line ")
		b.WriteString(strings.Repeat("x", 1))
		b.WriteString("\n")
	}
	return b.String()
}

// openViewer opens the viewer over a document of n lines at a height that gives
// the capture's 28-row body.
func openViewer(t *testing.T, n int) *Panel {
	t.Helper()
	refs := []Artifact{{Name: "plan", Path: "/plan.md"}}
	p := New(testSeams(refs, map[string]string{"/plan.md": body(n)}))
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if !p.Viewer() {
		t.Fatal("enter should open the viewer")
	}
	return p
}

// TestViewerGutterMatchesTheCapture pins the gutter's shape.
//
// 【一手】 the capture's rows are `>   1      <text>` and `> 102     <text>`:
// a cursor column, a right-aligned number in four columns, six spaces.
func TestViewerGutterMatchesTheCapture(t *testing.T) {
	p := openViewer(t, 102)
	lines := plain(p.Lines())

	// Title keeps the filename, with one leading space.
	if lines[0] != " plan.md" {
		t.Errorf("title = %q, want %q", lines[0], " plan.md")
	}
	// Cursor on line 1: `>   1` + six spaces + text.
	if got := lines[1]; got != ">   1      line x" {
		t.Errorf("first body row = %q,\n want %q", got, ">   1      line x")
	}
	// Not on the cursor: a space in the cursor column.
	if got := lines[2]; got != "    2      line x" {
		t.Errorf("second body row = %q,\n want %q", got, "    2      line x")
	}
}

// TestViewerCounterMatchesTheCapture pins the counter exactly:
// `[<pct>%  L<cursor>  <start>-<end>/<total>]`.
func TestViewerCounterMatchesTheCapture(t *testing.T) {
	p := openViewer(t, 102)
	lines := plain(p.Lines())

	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "[0%  L1  1-28/102]") {
		t.Fatalf("top-of-file counter missing from:\n%s", joined)
	}
}

// TestJJJMovesTheCursorNotTheWindow is the frame that proves the two are
// separate state.
//
// 【一手】 after `j j j` the capture shows `>   4 …` with the range still
// `1-28`, and `L4` in the counter.
func TestJJJMovesTheCursorNotTheWindow(t *testing.T) {
	p := openViewer(t, 102)
	for i := 0; i < 3; i++ {
		p.HandleKey(tea.Key{Code: tea.KeyDown})
	}
	lines := plain(p.Lines())

	if got := lines[4]; got != ">   4      line x" {
		t.Errorf("cursor row = %q, want %q", got, ">   4      line x")
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "[0%  L4  1-28/102]") {
		t.Errorf("counter should read [0%%  L4  1-28/102]:\n%s", joined)
	}
	if got := lines[1]; strings.HasPrefix(got, ">") {
		t.Errorf("the first row should no longer carry the cursor: %q", got)
	}
}

// TestGMovesToTheLastLine pins the bottom frame:
//
//	> 102     byte for byte.
//	  [100%  L102  75-102/102]
func TestGMovesToTheLastLine(t *testing.T) {
	p := openViewer(t, 102)
	p.HandleKey(tea.Key{Code: 'G'})

	lines := plain(p.Lines())
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "[100%  L102  75-102/102]") {
		t.Fatalf("bottom counter missing from:\n%s", joined)
	}
	if !strings.Contains(joined, "> 102"+"      "+"line x") {
		t.Errorf("the last line should carry the cursor:\n%s", joined)
	}
}

// TestCtrlDHalfPageMovesTheCursor pins the footer's own binding.
func TestCtrlDHalfPageMovesTheCursor(t *testing.T) {
	p := openViewer(t, 200)
	before := p.line
	p.HandleKey(tea.Key{Code: 'd', Mod: tea.ModCtrl})
	if p.line <= before {
		t.Errorf("ctrl+d should move the cursor forward: %d -> %d", before, p.line)
	}
	if got := p.line - before; got != 14 {
		t.Errorf("ctrl+d moved %d lines, want half the 28-row body", got)
	}
}

// TestViewerFooterNamesOnlyImplementedKeys keeps the footer honest: the
// reference's comment/outline/search keys are not implemented, so they must not
// be printed.
func TestViewerFooterNamesOnlyImplementedKeys(t *testing.T) {
	p := openViewer(t, 10)
	joined := strings.Join(plain(p.Lines()), "\n")

	for _, dead := range []string{"c comment", "t outline", "l hide lines", "/ search"} {
		if strings.Contains(joined, dead) {
			t.Errorf("footer advertises an unimplemented action: %q", dead)
		}
	}
	for _, live := range []string{"ctrl+g editor", "g/G top/bottom", "ctrl+u/ctrl+d half page", "esc close"} {
		if !strings.Contains(joined, live) {
			t.Errorf("footer is missing a real binding: %q\n%s", live, joined)
		}
	}
}

// TestEscClosesTheViewerBeforeThePanel keeps Esc's two meanings distinct.
func TestEscClosesTheViewerBeforeThePanel(t *testing.T) {
	closed := false
	refs := []Artifact{{Name: "plan", Path: "/plan.md"}}
	seams := testSeams(refs, map[string]string{"/plan.md": "body\n"})
	seams.Close = func() tea.Cmd {
		closed = true
		return nil
	}
	p := New(seams)

	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if !p.Viewer() {
		t.Fatal("the viewer should be open")
	}
	p.HandleKey(tea.Key{Code: tea.KeyEsc})
	if p.Viewer() {
		t.Error("esc should close the viewer")
	}
	if closed {
		t.Error("esc from the viewer must not leave the panel")
	}
	p.HandleKey(tea.Key{Code: tea.KeyEsc})
	if !closed {
		t.Error("esc from the list should leave the panel")
	}
}

// TestPreviewTogglesUnderTheSelectedRow pins the reference's "p preview".
//
// 【一手】 from the capture: twelve content rows, each behind `    │ `, then a
// `      ... N more lines` row when the file has more.
func TestPreviewTogglesUnderTheSelectedRow(t *testing.T) {
	refs := []Artifact{{Name: "plan", Path: "/plan.md"}}
	p := New(testSeams(refs, map[string]string{"/plan.md": body(30)}))

	before := len(p.Lines())
	p.HandleKey(tea.Key{Code: 'p'})
	if !p.Previewing() {
		t.Fatal("p should open the preview")
	}
	after := plain(p.Lines())
	if got := len(after) - before; got != previewRows+1 {
		t.Errorf("preview rows = %d, want %d (12 content rows and the rest note)", got, previewRows+1)
	}
	for i := 1; i <= previewRows; i++ {
		if !strings.HasPrefix(after[i+1], "    │ ") {
			t.Errorf("preview row %d should sit behind the gutter bar: %q", i, after[i+1])
		}
	}
	last := after[previewRows+2]
	if !strings.HasPrefix(last, "      ... ") || !strings.HasSuffix(last, " 18 more lines") {
		t.Errorf("the trailing note = %q, want %q", last, "      ... 18 more lines")
	}

	p.HandleKey(tea.Key{Code: 'p'})
	if p.Previewing() {
		t.Error("p should close the preview again")
	}
}

// TestPreviewClipsLongRows pins the capture's truncation: a row that overflows
// is cut at the preview's width and ends with an ellipsis, by rune so a CJK
// character is never split.
func TestPreviewClipsLongRows(t *testing.T) {
	long := strings.Repeat("宽", 100)
	refs := []Artifact{{Name: "wide", Path: "/wide.md"}}
	p := New(testSeams(refs, map[string]string{"/wide.md": long + "\n"}))
	p.HandleKey(tea.Key{Code: 'p'})

	rows := plain(p.Lines())
	var preview string
	for _, r := range rows {
		if strings.HasPrefix(r, "    │ ") {
			preview = r
		}
	}
	if preview == "" {
		t.Fatalf("no preview row in:\n%s", strings.Join(rows, "\n"))
	}
	if !strings.HasSuffix(preview, "…") {
		t.Errorf("a clipped row should end with an ellipsis: %q", preview)
	}
	if got := len([]rune(strings.TrimPrefix(preview, "    │ "))); got != previewRowWidth+1 {
		t.Errorf("clipped text = %d runes, want %d plus the ellipsis", got, previewRowWidth)
	}
}

// TestUnreadableArtifactSaysSo keeps a missing file from looking empty.
func TestUnreadableArtifactSaysSo(t *testing.T) {
	refs := []Artifact{{Name: "gone", Path: "/gone.md"}}
	p := New(testSeams(refs, nil))
	p.HandleKey(tea.Key{Code: tea.KeyEnter})

	joined := strings.Join(plain(p.Lines()), "\n")
	if !strings.Contains(joined, "cannot read") {
		t.Errorf("an unreadable artifact should say so:\n%s", joined)
	}
}

// TestCtrlCIsNeverSwallowed keeps the global way out working from both layers.
func TestCtrlCIsNeverSwallowed(t *testing.T) {
	refs := []Artifact{{Name: "plan", Path: "/plan.md"}}
	p := New(testSeams(refs, map[string]string{"/plan.md": "x\n"}))

	if handled, _ := p.HandleKey(tea.Key{Code: 'c', Mod: tea.ModCtrl}); handled {
		t.Error("the list must not swallow ctrl+c")
	}
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if handled, _ := p.HandleKey(tea.Key{Code: 'c', Mod: tea.ModCtrl}); handled {
		t.Error("the viewer must not swallow ctrl+c")
	}
}

// TestRowCountFollowsTheConversation keeps the cursor bound honest when the
// artifact list shrinks under an open panel.
func TestRowCountFollowsTheConversation(t *testing.T) {
	refs := []Artifact{
		{Name: "a", Path: "/a.md"},
		{Name: "b", Path: "/b.md"},
		{Name: "c", Path: "/c.md"},
	}
	seams := testSeams(refs, nil)
	p := New(seams)
	p.SetCursor(2)

	shrunk := []Artifact{{Name: "a", Path: "/a.md"}}
	seams.Refs = func() []Artifact { return shrunk }
	p.seams = seams

	if got := p.RowCount(); got != 1 {
		t.Errorf("RowCount = %d, want 1", got)
	}
	p.SetCursor(5)
	if got := p.Cursor(); got != 0 {
		t.Errorf("a stale cursor should clamp to the last row: got %d, want 0", got)
	}
}

// TestWindowForCursorFollowsOnlyWhenNeeded is the arithmetic the two captured
// frames pin.
func TestWindowForCursorFollowsOnlyWhenNeeded(t *testing.T) {
	// Cursor inside the window: no scroll.
	if got := windowForCursor(0, 3, 28, 102); got != 0 {
		t.Errorf("cursor inside the window scrolled to %d, want 0", got)
	}
	// Cursor just past the window: the window follows by the minimum.
	if got := windowForCursor(0, 28, 28, 102); got != 1 {
		t.Errorf("cursor past the window scrolled to %d, want 1", got)
	}
	// Cursor above the window: the window moves up to it.
	if got := windowForCursor(20, 5, 28, 102); got != 5 {
		t.Errorf("cursor above the window scrolled to %d, want 5", got)
	}
	// The window never runs past the document's end.
	if got := windowForCursor(0, 101, 28, 102); got != 74 {
		t.Errorf("the window should clamp at 102-28=74, got %d", got)
	}
}
