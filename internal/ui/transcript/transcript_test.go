// transcript_test.go — the package's own tests.
//
// The transcript's row vocabulary is pinned here, on its own terms: the
// prompt rows, the turn separator and the block's render. The host's
// tests cover the host's use of them; these cover the package itself.
package transcript

import (
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/ui/toolsteps"
	"github.com/justwasm/pigasus/palette"
)

// fakeBash is a BashBlock whose rows the test controls.
type fakeBash struct {
	rows []string
}

func (f *fakeBash) Lines(expanded bool, height int) []string {
	return f.rows
}

func TestUserPromptRow_SingleLine(t *testing.T) {
	got := UserPromptRow("hello")
	want := palette.UserFgBoldOpen + "> hello" + palette.BlueBoldClose
	if got != want {
		t.Errorf("UserPromptRow = %q, want %q", got, want)
	}
}

func TestUserPromptRow_MultiLineIndentsTheTail(t *testing.T) {
	got := UserPromptRow("one\ntwo\nthree")
	lines := strings.Split(got, "\n")
	if len(lines) != 3 {
		t.Fatalf("rows = %d, want 3: %q", len(lines), got)
	}
	if lines[0] != palette.UserFgBoldOpen+"> one"+palette.BlueBoldClose {
		t.Errorf("first row = %q", lines[0])
	}
	wantTwo := palette.UserFgBoldOpen + "  two" + palette.BlueBoldClose
	if lines[1] != wantTwo {
		t.Errorf("second row = %q, want %q", lines[1], wantTwo)
	}
	wantThree := palette.UserFgBoldOpen + "  three" + palette.BlueBoldClose
	if lines[2] != wantThree {
		t.Errorf("third row = %q, want %q", lines[2], wantThree)
	}
}

func TestRenderUserPrompt_EmptyTextIsOneStyledRow(t *testing.T) {
	got := RenderUserPrompt("", 80)
	if len(got) != 1 {
		t.Fatalf("rows = %d, want 1: %q", len(got), got)
	}
	if got[0] != palette.UserFgBoldOpen+"> "+palette.BlueBoldClose {
		t.Errorf("row = %q", got[0])
	}
}

func TestRenderUserPrompt_SlashCommandUsesTheWarningColour(t *testing.T) {
	got := RenderUserPrompt("/model gemini", 80)
	if len(got) != 1 {
		t.Fatalf("rows = %d, want 1: %q", len(got), got)
	}
	if !strings.HasPrefix(got[0], palette.WarningBlueOpen) {
		t.Errorf("slash prompt = %q, want the warning-blue prefix", got[0])
	}
}

func TestRenderUserPrompt_WrapsAndIndentsTheTail(t *testing.T) {
	got := RenderUserPrompt(strings.Repeat("word ", 40), 40)
	if len(got) < 2 {
		t.Fatalf("rows = %d, want the prompt to wrap: %q", len(got), got)
	}
	if !strings.HasPrefix(got[0], palette.UserFgBoldOpen+"> ") {
		t.Errorf("first row = %q", got[0])
	}
	for i, row := range got[1:] {
		if !strings.HasPrefix(row, palette.UserFgBoldOpen+"  ") {
			t.Errorf("row %d = %q, want styled two-space indent", i+1, row)
		}
	}
	for _, row := range got {
		if strings.Contains(row, "\n") {
			t.Errorf("row carries an embedded newline: %q", row)
		}
	}
}

func TestRenderUserPrompt_NonPositiveWidthKeepsTheRawLines(t *testing.T) {
	got := RenderUserPrompt("one\ntwo", 0)
	if len(got) != 2 {
		t.Fatalf("rows = %d, want 2: %q", len(got), got)
	}
	if !strings.HasSuffix(got[0], palette.BlueBoldClose) {
		t.Errorf("first row = %q", got[0])
	}
	wantTwo := palette.UserFgBoldOpen + "  two" + palette.BlueBoldClose
	if got[1] != wantTwo {
		t.Errorf("second row = %q, want %q", got[1], wantTwo)
	}
}

func TestTurnSeparator_NarrowAndWide(t *testing.T) {
	wide := TurnSeparator(220)
	if n := strings.Count(wide, "─"); n != 60 {
		t.Errorf("wide separator = %d cells, want 60", n)
	}
	narrow := TurnSeparator(40)
	if n := strings.Count(narrow, "─"); n != 37 {
		t.Errorf("narrow separator = %d cells, want 37 (width-3)", n)
	}
	tiny := TurnSeparator(5)
	if n := strings.Count(tiny, "─"); n != 5 {
		t.Errorf("tiny separator = %d cells, want 5 (the width itself)", n)
	}
	if TurnSeparator(0) != TurnSeparator(0) {
		t.Fatal("the separator is not deterministic")
	}
}

// dumpSteps are the fixtures the block render is driven with.
func dumpSteps() []toolsteps.Step {
	return []toolsteps.Step{
		{
			Kind:    toolsteps.KindTool,
			Name:    "run_command",
			Title:   "Bash(echo one)",
			Summary: "Ran (echo one)",
			Detail:  "one",
			Result:  "one",
		},
		{
			Kind:    toolsteps.KindThought,
			Title:   "thinking",
			Summary: "thought",
			Detail:  "because",
			Result:  "because",
		},
	}
}

func TestBlockRender_CollapsedAndExpanded(t *testing.T) {
	steps := dumpSteps()
	b := NewBlock(Block{Start: 0, Lines: 1, Steps: steps}, nil, 0, toolsteps.VerbosityMedium)

	collapsed := b.Render(false, 80, 24, toolsteps.VerbosityMedium)
	expanded := b.Render(true, 80, 24, toolsteps.VerbosityMedium)
	if len(collapsed) == 0 {
		t.Fatal("the collapsed block rendered no rows")
	}
	if len(expanded) <= len(collapsed) {
		t.Errorf("expanded rows = %d, want more than the collapsed %d", len(expanded), len(collapsed))
	}
}

func TestBlockRender_EmptyBlockRendersNoRows(t *testing.T) {
	b := Block{}
	if got := b.Render(false, 80, 24, toolsteps.VerbosityMedium); len(got) != 0 {
		t.Errorf("rows = %q, want none", got)
	}
}

func TestBlockRender_CacheAnswersTheSameWidth(t *testing.T) {
	steps := dumpSteps()
	rendered := toolsteps.RenderBlock(steps, toolsteps.RenderOptions{
		Expanded:  false,
		Width:     80,
		Verbosity: toolsteps.VerbosityMedium,
	})
	b := NewBlock(Block{Start: 0, Lines: len(rendered), Steps: steps}, rendered, 80, toolsteps.VerbosityMedium)

	got := b.Render(false, 80, 24, toolsteps.VerbosityMedium)
	if len(got) != len(rendered) {
		t.Fatalf("cached rows = %d, want %d", len(got), len(rendered))
	}
	for i := range got {
		if got[i] != rendered[i] {
			t.Errorf("cached row %d = %q, want %q", i, got[i], rendered[i])
		}
	}

	// A different width must not be answered from the cache.
	other := b.Render(false, 40, 24, toolsteps.VerbosityMedium)
	fresh := toolsteps.RenderBlock(steps, toolsteps.RenderOptions{
		Expanded:  false,
		Width:     40,
		Verbosity: toolsteps.VerbosityMedium,
	})
	if len(other) != len(fresh) {
		t.Fatalf("re-widthed rows = %d, want %d", len(other), len(fresh))
	}
	for i := range other {
		if other[i] != fresh[i] {
			t.Errorf("re-widthed row %d = %q, want %q", i, other[i], fresh[i])
		}
	}
}

func TestBlockRender_BashBlockOwnsItsRows(t *testing.T) {
	fake := &fakeBash{rows: []string{"● ! echo hi", "  ⎿  hi"}}
	b := Block{Start: 0, Lines: 2, Bash: fake}
	got := b.Render(true, 80, 24, toolsteps.VerbosityMedium)
	if len(got) != 2 || got[0] != fake.rows[0] {
		t.Errorf("rows = %q, want the bash block's own rows", got)
	}
}

func TestBlockRender_LowVerbosityUsesTheTokenMetrics(t *testing.T) {
	b := Block{
		Start:   0,
		Lines:   1,
		Steps:   dumpSteps(),
		Metrics: toolsteps.TokenMetrics{Input: 10, Output: 20},
	}
	got := b.Render(false, 80, 24, toolsteps.VerbosityLow)
	joined := strings.Join(got, "\n")
	if !strings.Contains(joined, "10 in") || !strings.Contains(joined, "20 out") {
		t.Errorf("low-verbosity rows = %q, want the token metrics summary", joined)
	}
}
