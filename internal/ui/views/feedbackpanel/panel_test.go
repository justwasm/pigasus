// panel_test.go — the /feedback panel's own half.
//
// Cycle 381 moved the panel out of internal/cliapp: the two stages, the
// six categories, the cursor, the free-text buffer and the keys are the
// package's, and the host keeps only the seams. The behaviour is the
// clone's own from cycle 205 on, so the tests move with the code they
// exercise.
//
// The tests build the panel directly — there is no runMode behind it —
// so a seam is a closure over a test value.

package feedbackpanel

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	termcolor "github.com/justwasm/pigasus/internal/ui/render/color"
	"github.com/justwasm/pigasus/palette"
)

// ---------------------------------------------------------------- helpers

// stripSGR removes every ANSI SGR sequence.
func stripSGR(s string) string {
	return termcolor.StripSGR(s)
}

// plainLines strips the SGR spans off a rendered row slice.
func plainLines(rows []string) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, stripSGR(r))
	}
	return out
}

// assertLines compares a rendered row slice with the expected rows.
func assertLines(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("line count: got %d, want %d\n got: %q\nwant: %q",
			len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d:\n got: %q\nwant: %q", i, got[i], want[i])
		}
	}
}

// newTestPanel builds a /feedback panel whose seams record what the
// panel did: the submits are counted and the close is a no-op.
func newTestPanel() (*Panel, *int, *bool) {
	submitted := 0
	closed := false
	p := New(Seams{
		Submit: func() { submitted++ },
		Close:  func() tea.Cmd { closed = true; return nil },
	})
	return p, &submitted, &closed
}

// ---------------------------------------------------------------- tests

// The reference's /feedback stage-1 category picker, verbatim from
// /tmp/agycmp/panels/ppref.feedback.raw.txt (SGR stripped).
func TestPickerLayout(t *testing.T) {
	p, _, _ := newTestPanel()
	assertLines(t, plainLines(p.Lines()), []string{
		"Submit Feedback",
		"",
		"What went wrong? Select a category to assist your report:",
		"",
		"> 1. Code doesn't work (e.g., syntax errors, bad logic)",
		"  2. Repeated itself (e.g., stuck running the same tool over again)",
		"  3. Took too many steps (e.g., progress was much too slow)",
		"  4. Ignored instructions (e.g., missed specific constraints)",
		"  5. Got stuck (e.g., unable to recover from an error)",
		"  6. Other",
		"",
		"  esc Cancel · 1-6 Select & Continue · enter Continue",
	})
	// The selected row carries the blue codes; the unselected rows do not.
	rows := p.Lines()
	for i, line := range rows {
		if i == 4 {
			if !strings.Contains(line, palette.BlueOpen) || !strings.Contains(line, palette.BlueClose) {
				t.Errorf("row %d should be blue: %q", i, line)
			}
		} else if i >= 5 && i <= 9 {
			if strings.Contains(line, palette.BlueOpen) {
				t.Errorf("row %d should not be blue: %q", i, line)
			}
		}
	}
	// The dot hint's key glyphs (esc, 1-6, enter) are bold blue.
	hint := rows[11]
	if !strings.Contains(hint, palette.PanelBoldBlueOpen) {
		t.Errorf("dot hint keys should be bold blue: %q", hint)
	}
}

// Pressing Down at the last row of /feedback wraps to the first; REF
// behaves the same way (verified at /tmp/agycmp/panels/ppfb_wrap.raw.txt).
// Up is symmetric.
func TestPickerWraps(t *testing.T) {
	p, _, _ := newTestPanel()
	down := tea.Key{Code: tea.KeyDown}
	for i := 1; i < 6; i++ {
		if handled, _ := p.HandleKey(down); !handled || p.Cursor() != i {
			t.Fatalf("Down #%d: handled=%v cursor=%d want %d", i, handled, p.Cursor(), i)
		}
	}
	// One more Down wraps 5 -> 0.
	if handled, _ := p.HandleKey(down); !handled || p.Cursor() != 0 {
		t.Errorf("wrap Down: handled=%v cursor=%d want 0", handled, p.Cursor())
	}
	// Up from 0 wraps to 5.
	up := tea.Key{Code: tea.KeyUp}
	if handled, _ := p.HandleKey(up); !handled || p.Cursor() != 5 {
		t.Errorf("wrap Up: handled=%v cursor=%d want 5", handled, p.Cursor())
	}
}

// The reference's stage-2 form: the chosen category is echoed back
// without the picker's index, the buffer takes the typing, Esc returns
// to the picker with the draft discarded and the category preserved,
// and Enter (or ctrl+s) submits.
//
// Cycle 221 changed stage-2 Enter from inert (cycle-208 REF behaviour)
// to submit — the clone's divergence.
func TestFormStage(t *testing.T) {
	p, submitted, _ := newTestPanel()
	if handled, _ := p.HandleKey(tea.Key{Text: "3"}); !handled {
		t.Fatal("1-6 key not handled")
	}
	if p.Stage() != 1 || p.Cursor() != 2 {
		t.Fatalf("stage=%d cursor=%d, want stage 1 cursor 2", p.Stage(), p.Cursor())
	}
	text := strings.Join(plainLines(p.Lines()), "\n")
	// Cycle 208: REF's stage-2 `Feedback:` line echoes the category *label*
	// without the picker's `N. ` index (fin_fb_stage2.raw.txt).
	if !strings.Contains(text, CategoryLabel(Categories[2])) {
		t.Errorf("stage 2 should echo the chosen category:\n%s", text)
	}
	if strings.Contains(text, "3. Took too many steps") {
		t.Errorf("stage 2 should drop the picker index:\n%s", text)
	}
	if !strings.Contains(p.Lines()[7], searchCursorBlock) {
		t.Errorf("stage 2 should render the input cursor: %q", p.Lines()[7])
	}
	for _, r := range []string{"h", "i"} {
		p.HandleKey(tea.Key{Text: r})
	}
	if p.Text() != "hi" {
		t.Fatalf("text = %q, want %q", p.Text(), "hi")
	}
	p.HandleKey(tea.Key{Code: tea.KeyBackspace})
	if p.Text() != "h" {
		t.Fatalf("backspace text = %q, want %q", p.Text(), "h")
	}
	// Esc returns to the picker with the draft discarded but the
	// category (cursor index) preserved.
	p.HandleKey(tea.Key{Code: tea.KeyEsc})
	if p.Stage() != 0 || p.Text() != "" {
		t.Fatalf("esc: stage=%d text=%q", p.Stage(), p.Text())
	}
	if p.Cursor() != 2 {
		t.Fatalf("esc should preserve category: cursor=%d, want 2", p.Cursor())
	}
	// Enter on the picker advances; in stage 2 enter submits (cycle 221
	// openagy divergence from REF's ctrl+s-only textarea semantics).
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if p.Stage() != 1 {
		t.Fatalf("enter did not advance: stage=%d", p.Stage())
	}
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if *submitted != 1 {
		t.Fatalf("enter did not submit: submitted=%d", *submitted)
	}
	// ctrl+s also submits (REF parity), and ctrl+c still falls through.
	p2, submitted2, _ := newTestPanel()
	p2.HandleKey(tea.Key{Code: tea.KeyEnter})
	if handled, _ := p2.HandleKey(tea.Key{Code: 'c', Mod: tea.ModCtrl}); handled {
		t.Error("ctrl+c must fall through in stage 2")
	}
	if handled, _ := p2.HandleKey(tea.Key{Code: 's', Mod: tea.ModCtrl}); !handled {
		t.Fatal("ctrl+s not handled")
	}
	if *submitted2 != 1 {
		t.Error("ctrl+s did not submit")
	}
}

// TestFormLayout pins the stage-2 frame, transcribed from the
// reference's fin_fb_stage2.raw.txt capture.
func TestFormLayout(t *testing.T) {
	p, _, _ := newTestPanel()
	p.SetCursor(0)
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	assertLines(t, p.Lines(), []string{
		panelTitleOpen + "Review Submission Data" + panelTitleClose + "  " +
			panelGray241 + "(You can always provide manual feedback via /feedback)" + panelGray241C,
		"",
		"Your feedback helps improve Pigasus CLI. The following sanitized data will be sent:",
		"",
		"Feedback: " + palette.BlueOpen + "Code doesn't work (e.g., syntax errors, bad logic)" + palette.BlueClose,
		"",
		"Additional Details (optional):",
		searchCursorBlock,
		"",
		"",
		"",
		submitRowOpen + " ctrl+s Submit Feedback " + submitRowClose + "  " +
			panelGray241 + "esc Back" + panelGray241C,
	})
}

func TestFormCtrlSSubmits(t *testing.T) {
	p, submitted, _ := newTestPanel()
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	p.HandleKey(tea.Key{Text: "the model repeated itself"})
	p.HandleKey(tea.Key{Code: 's', Mod: tea.ModCtrl})
	if *submitted != 1 {
		t.Fatalf("ctrl+s did not submit: submitted=%d", *submitted)
	}
}

func TestFormBufferEmpty(t *testing.T) {
	p, _, _ := newTestPanel()
	p.SetCursor(2)
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	lines := p.Lines()
	if got := lines[7]; got != searchCursorBlock {
		t.Errorf("empty buffer row = %q, want %q", got, searchCursorBlock)
	}
	if p.Text() != "" {
		t.Errorf("empty buffer text = %q, want empty", p.Text())
	}
}

func TestFormBufferTyping(t *testing.T) {
	p, _, _ := newTestPanel()
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	for _, r := range []string{"h", "e", "l", "l", "o", " ", "w", "o", "r", "l", "d"} {
		if handled, _ := p.HandleKey(tea.Key{Text: r}); !handled {
			t.Fatalf("printable key %q not handled", r)
		}
	}
	if p.Text() != "hello world" {
		t.Errorf("buffer = %q, want %q", p.Text(), "hello world")
	}
	p.HandleKey(tea.Key{Code: tea.KeyBackspace})
	if p.Text() != "hello worl" {
		t.Errorf("after backspace = %q, want %q", p.Text(), "hello worl")
	}
	p.HandleKey(tea.Key{Code: tea.KeyBackspace})
	p.HandleKey(tea.Key{Code: tea.KeyBackspace})
	p.HandleKey(tea.Key{Code: tea.KeyBackspace})
	p.HandleKey(tea.Key{Code: tea.KeyBackspace})
	p.HandleKey(tea.Key{Code: tea.KeyBackspace})
	p.HandleKey(tea.Key{Code: tea.KeyBackspace})
	p.HandleKey(tea.Key{Code: tea.KeyBackspace})
	p.HandleKey(tea.Key{Code: tea.KeyBackspace})
	p.HandleKey(tea.Key{Code: tea.KeyBackspace})
	p.HandleKey(tea.Key{Code: tea.KeyBackspace})
	p.HandleKey(tea.Key{Code: tea.KeyBackspace})
	p.HandleKey(tea.Key{Code: tea.KeyBackspace})
	if p.Text() != "" {
		t.Errorf("backspace on empty buffer = %q, want empty", p.Text())
	}
}

func TestFormEnterSubmits(t *testing.T) {
	p, submitted, _ := newTestPanel()
	p.SetCursor(2)
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	p.HandleKey(tea.Key{Text: "model got stuck in a loop"})
	if handled, _ := p.HandleKey(tea.Key{Code: tea.KeyEnter}); !handled {
		t.Fatal("enter not handled in stage 2")
	}
	if *submitted != 1 {
		t.Fatalf("enter did not submit: submitted=%d", *submitted)
	}
}

func TestFormEnterSubmitsEmpty(t *testing.T) {
	p, submitted, _ := newTestPanel()
	p.SetCursor(5)
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if *submitted != 1 {
		t.Fatalf("enter on empty buffer did not submit: submitted=%d", *submitted)
	}
}

func TestFormEscReturnsToPicker(t *testing.T) {
	p, _, _ := newTestPanel()
	p.SetCursor(4)
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	p.HandleKey(tea.Key{Text: "some draft"})
	if handled, _ := p.HandleKey(tea.Key{Code: tea.KeyEsc}); !handled {
		t.Fatal("esc not handled in stage 2")
	}
	if p.Stage() != 0 {
		t.Errorf("esc did not return to stage 1: stage=%d", p.Stage())
	}
	if p.Text() != "" {
		t.Errorf("esc did not discard the draft: text=%q", p.Text())
	}
	if p.Cursor() != 4 {
		t.Errorf("esc lost the category: cursor=%d, want 4", p.Cursor())
	}
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if p.Stage() != 1 {
		t.Fatalf("re-enter did not advance: stage=%d", p.Stage())
	}
	if p.Text() != "" {
		t.Errorf("re-enter should leave the buffer empty: text=%q", p.Text())
	}
	if p.Cursor() != 4 {
		t.Errorf("re-enter lost the category: cursor=%d, want 4", p.Cursor())
	}
}

func TestPickerBindingsRetained(t *testing.T) {
	for _, k := range []string{"1", "3", "5"} {
		p, _, _ := newTestPanel()
		p.HandleKey(tea.Key{Text: k})
		want := int(k[0] - '1')
		if p.Stage() != 1 || p.Cursor() != want {
			t.Fatalf("'%s' select-and-continue: stage=%d cursor=%d, want 1, %d",
				k, p.Stage(), p.Cursor(), want)
		}
	}

	p, _, _ := newTestPanel()
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if p.Stage() != 1 || p.Cursor() != 0 {
		t.Fatalf("plain enter: stage=%d cursor=%d, want 1, 0", p.Stage(), p.Cursor())
	}
	p.HandleKey(tea.Key{Code: tea.KeyEsc})

	p.HandleKey(tea.Key{Code: tea.KeyUp})
	if p.Cursor() != 5 {
		t.Errorf("up at cursor 0 should wrap to 5, got %d", p.Cursor())
	}
	p.HandleKey(tea.Key{Code: tea.KeyDown})
	if p.Cursor() != 0 {
		t.Errorf("down at cursor 5 should wrap to 0, got %d", p.Cursor())
	}
}

// TestPickerEscCloses pins the first stage's Esc: the panel leaves
// without submitting.
func TestPickerEscCloses(t *testing.T) {
	p, submitted, closed := newTestPanel()
	if handled, _ := p.HandleKey(tea.Key{Code: tea.KeyEsc}); !handled {
		t.Fatal("esc not handled")
	}
	if !*closed {
		t.Error("esc did not close the panel")
	}
	if *submitted != 0 {
		t.Error("esc must not submit")
	}
}
