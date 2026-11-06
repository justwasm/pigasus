package tui

// The composer's selection and the prompt history — the two pieces this
// tranche moved into their own packages — are pinned here byte for byte.
//
// The golden file (testdata/tui_input_history.golden) holds the exact
// bytes of:
//
//   - the selection's range, its reverse-video highlight and the mouse
//     offset a click maps to,
//   - the cursor's position in a multi-line value,
//   - the value a selection's delete and replace produce,
//   - the prompt history's push, its navigation, its blank handling,
//     its cap and the overlays a recall closes.
//
// The reference's own quirks are part of the pin and are NOT defects:
// FindLineSpan with a negative line returns the LAST line's span, a
// selection that starts inside the value but ends past it is clamped,
// and a rendered row without the prompt gets the highlight pushed off
// its end. They are pinned so a later change cannot "fix" them
// silently.
//
// The bytes were proven by a diff of two dumps: the pre-tranche tree
// and this tree produced the same 8 603 bytes.

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/internal/app/session/convstore"
	"github.com/justwasm/pigasus/internal/ui/foundations/history"
)

func goldenMode(t *testing.T) *runMode {
	t.Helper()
	m := newRunMode(&fakeClient{}, "u@t", "m", "t", "/workspace", false)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	return m
}

type goldenClick struct {
	x, y int
}

type goldenSelFixture struct {
	name   string
	value  string
	view   string
	start  int
	end    int
	rep    string
	offs   []int
	clicks []goldenClick
}

var goldenSelFixtures = []goldenSelFixture{
	{
		name:  "single line, the whole value",
		value: "hello",
		view:  "> hello",
		start: 0, end: 5, rep: "X",
		offs:   []int{0, 1, 3, 5, 6, 9, -1, -5},
		clicks: []goldenClick{{2, 10}, {4, 10}, {9, 10}, {40, 10}, {2, 9}, {2, 11}},
	},
	{
		name:  "single line, the middle",
		value: "hello",
		view:  "> hello",
		start: 1, end: 3, rep: "XY",
		offs:   []int{0, 1, 2, 3, 4, 5},
		clicks: []goldenClick{{3, 10}, {5, 10}},
	},
	{
		name:  "single line, an empty selection",
		value: "hello",
		view:  "> hello",
		start: 2, end: 2, rep: "Z",
		offs:   []int{0, 2, 5},
		clicks: []goldenClick{{4, 10}},
	},
	{
		name:  "single line, past the end of the value",
		value: "hello",
		view:  "> hello",
		start: 0, end: 99, rep: "long",
		offs:   []int{0, 5},
		clicks: []goldenClick{{4, 10}},
	},
	{
		name:  "single line, a reversed selection",
		value: "hello",
		view:  "> hello",
		start: 3, end: 1, rep: "R",
		offs:   []int{0, 3},
		clicks: []goldenClick{{5, 10}},
	},
	{
		name:  "single line, a selection beyond the value",
		value: "hello",
		view:  "> hello",
		start: 9, end: 12, rep: "B",
		offs:   []int{0, 5},
		clicks: []goldenClick{{4, 10}},
	},
	{
		name:  "three lines, the whole value",
		value: "a\nb\nc",
		view:  "> a\n  b\n  c",
		start: 0, end: 5, rep: "Q",
		offs:   []int{0, 1, 2, 3, 4, 5, 6, 7},
		clicks: []goldenClick{{2, 10}, {4, 11}, {4, 12}, {4, 13}},
	},
	{
		name:  "three lines, the first row",
		value: "a\nb\nc",
		view:  "> a\n  b\n  c",
		start: 0, end: 1, rep: "F",
		offs:   []int{0, 1, 2},
		clicks: []goldenClick{{3, 10}},
	},
	{
		name:  "three lines, the second row only",
		value: "a\nb\nc",
		view:  "> a\n  b\n  c",
		start: 2, end: 3, rep: "S",
		offs:   []int{2, 3},
		clicks: []goldenClick{{4, 11}},
	},
	{
		name:  "an empty value",
		value: "",
		view:  "> ",
		start: 0, end: 0, rep: "E",
		offs:   []int{0, 1, -1},
		clicks: []goldenClick{{2, 10}, {4, 10}},
	},
	{
		name:  "wide runes",
		value: "日本語",
		view:  "> 日本語",
		start: 0, end: 9, rep: "漢",
		offs:   []int{0, 3, 6, 9, 12},
		clicks: []goldenClick{{2, 10}, {4, 10}, {8, 10}},
	},
	{
		name:  "a trailing newline",
		value: "aaa\nbbb\n",
		view:  "> aaa\n  bbb\n  ",
		start: 1, end: 5, rep: "T",
		offs:   []int{0, 3, 4, 7, 8},
		clicks: []goldenClick{{4, 10}, {4, 11}, {4, 12}},
	},
	{
		name:  "the prompt on every row",
		value: "aaa\nbbb",
		view:  "> aaa\n  bbb",
		start: 1, end: 5, rep: "P",
		offs:   []int{0, 3, 4, 7},
		clicks: []goldenClick{{4, 10}, {4, 11}},
	},
	{
		name:  "a value with surrounding spaces",
		value: "  spaced  ",
		view:  ">   spaced  ",
		start: 2, end: 8, rep: "S",
		offs:   []int{0, 2, 8, 10},
		clicks: []goldenClick{{4, 10}, {10, 10}},
	},
}

func goldenSelectionDump(b *bytes.Buffer, t *testing.T) {
	for _, f := range goldenSelFixtures {
		fmt.Fprintf(b, "sel %s\n", f.name)
		fmt.Fprintf(b, "  value=%q view=%q\n", f.value, f.view)

		m := goldenMode(t)
		m.composer.promptScreenY = 10
		m.composer.input.SetValue(f.value)
		m.viewport.InputSelStart = f.start
		m.viewport.InputSelEnd = f.end
		s, e := m.InputSelectionRange()
		fmt.Fprintf(b, "  range=%d,%d\n", s, e)
		fmt.Fprintf(b, "  highlight=%q\n", m.applyInputSelectionHighlight(f.view))
		for _, c := range f.clicks {
			fmt.Fprintf(b, "  offsetAt(%d,%d)=%d\n", c.x, c.y, m.inputOffsetAt(c.x, c.y))
		}

		for _, off := range f.offs {
			mc := goldenMode(t)
			mc.composer.input.SetValue(f.value)
			mc.setInputCursorOffset(off)
			fmt.Fprintf(b, "  cursor(%d)=%d,%d value=%q\n",
				off, mc.composer.input.Line(), mc.composer.input.Column(), mc.composer.input.Value())
		}

		md := goldenMode(t)
		md.composer.input.SetValue(f.value)
		md.viewport.InputSelStart = f.start
		md.viewport.InputSelEnd = f.end
		md.deleteInputSelection()
		fmt.Fprintf(b, "  delete=%q line=%d col=%d sel=%v\n",
			md.composer.input.Value(), md.composer.input.Line(), md.composer.input.Column(), md.hasInputSelection())

		mr := goldenMode(t)
		mr.composer.input.SetValue(f.value)
		mr.viewport.InputSelStart = f.start
		mr.viewport.InputSelEnd = f.end
		mr.replaceInputSelection(f.rep)
		fmt.Fprintf(b, "  replace=%q line=%d col=%d sel=%v\n",
			mr.composer.input.Value(), mr.composer.input.Line(), mr.composer.input.Column(), mr.hasInputSelection())
	}
}

func goldenHistoryDump(b *bytes.Buffer, t *testing.T) {
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	m := goldenMode(t)
	m.composer.input.SetValue("DRAFT")

	for _, p := range []string{"", " ", "  hello  ", "hello", "world", "multi\nline", "!echo hi", "  \t\n "} {
		m.pushHistory(p)
		fmt.Fprintf(b, "hist push %q\n", p)
	}

	for i := 1; i <= 5; i++ {
		m.historyUp()
		fmt.Fprintf(b, "hist up%d=%q\n", i, m.composer.input.Value())
	}
	m.historyUp()
	fmt.Fprintf(b, "hist up-past-the-end=%q\n", m.composer.input.Value())

	for i := 1; i <= 5; i++ {
		m.historyDown()
		fmt.Fprintf(b, "hist down%d=%q\n", i, m.composer.input.Value())
	}

	m.composer.promptHistory.Reset()
	m.historyUp()
	fmt.Fprintf(b, "hist reset-then-up=%q\n", m.composer.input.Value())

	// A recall closes the composer's overlays; it does not re-derive them.
	ms := goldenMode(t)
	ms.composer.input.SetValue("/help")
	ms.composer.picker.Update([]rune("/help"))
	fmt.Fprintf(b, "hist slash-before=%v\n", ms.composer.picker.Visible())
	ms.pushHistory("slash entry")
	ms.historyUp()
	fmt.Fprintf(b, "hist slash-after=%v value=%q\n", ms.composer.picker.Visible(), ms.composer.input.Value())

	mm := goldenMode(t)
	mm.composer.input.SetValue("note @zz")
	mm.composer.atFilePicker.Update()
	fmt.Fprintf(b, "hist mention-before=%v\n", mm.composer.atFilePicker.Visible())
	mm.pushHistory("mention entry")
	mm.historyUp()
	fmt.Fprintf(b, "hist mention-after=%v value=%q\n", mm.composer.atFilePicker.Visible(), mm.composer.input.Value())

	// The persistent store seeds an empty list.
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	for _, e := range []convstore.HistoryEntry{
		{Display: "first command", Workspace: "/workspace"},
		{Display: "second command", Workspace: "/workspace"},
		{Display: "/help", Workspace: "/workspace", Type: "slash_command"},
	} {
		if err := convstore.AppendHistoryEntry(e); err != nil {
			t.Fatalf("AppendHistoryEntry failed: %v", err)
		}
	}
	ml := goldenMode(t)
	ml.composer.promptHistory.Load(convstore.LoadHistory("", history.Cap))
	for i := 1; i <= 4; i++ {
		ml.historyUp()
		fmt.Fprintf(b, "hist load-up%d=%q\n", i, ml.composer.input.Value())
	}
	for i := 1; i <= 4; i++ {
		ml.historyDown()
		fmt.Fprintf(b, "hist load-down%d=%q\n", i, ml.composer.input.Value())
	}

	// The cap keeps the newest entries.
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	mc := goldenMode(t)
	for i := 0; i < 105; i++ {
		mc.pushHistory(fmt.Sprintf("prompt-%d", i))
	}
	for i := 1; i <= 101; i++ {
		mc.historyUp()
		fmt.Fprintf(b, "hist cap-up%d=%q\n", i, mc.composer.input.Value())
	}
}

func composerSelectionAndHistoryDump(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	goldenSelectionDump(&b, t)
	goldenHistoryDump(&b, t)
	return b.Bytes()
}

// TestComposerSelectionAndHistoryGolden pins the bytes the two moved
// packages produce through the host.
func TestComposerSelectionAndHistoryGolden(t *testing.T) {
	got := composerSelectionAndHistoryDump(t)
	want, err := os.ReadFile(filepath.Join("testdata", "tui_input_history.golden"))
	if err != nil {
		t.Fatalf("ReadFile(testdata/tui_input_history.golden): %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("the composer's selection and the prompt history changed:\n--- got (%d bytes)\n%s\n--- want (%d bytes)\n%s",
			len(got), got, len(want), want)
	}
}
