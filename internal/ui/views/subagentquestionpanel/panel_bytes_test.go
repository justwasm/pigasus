package subagentquestionpanel

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	subagentquestions "github.com/justwasm/pigasus/internal/app/subagent/questions"
	"github.com/justwasm/pigasus/palette"
)

// TestRenderedBytes pins the picker's rendered bytes.
//
// The picker is a pixel-for-pixel clone of the host's own overlay, so
// the escape sequences are part of the contract: a colour that moves
// changes what the user sees, and a segment that is dropped leaves a
// row in the wrong colour. The expectations below are the bytes the
// host's picker wrote before the panel took the row over.
//
// The colour scheme is pinned, because the bytes carry it.
func TestRenderedBytes(t *testing.T) {
	restore := palette.PushScheme("solarized light")
	defer restore()

	mgr := subagentquestions.NewManager(false)
	var lines []string
	input := ""
	p := New(Seams{
		Manager:    mgr,
		Respond:    func(line string) { lines = append(lines, line) },
		InputValue: func() string { return input },
		ResetInput: func() { input = "" },
	})
	mgr.Enqueue(subagentquestions.ParentKey, "Parent question", []string{"p1", "p2"})
	mgr.Enqueue("subagent-B", "B question", []string{"b1"})
	mgr.Enqueue("subagent-A", "A question", nil)

	wantAlerts := ALERTS
	got := p.AlertRows()
	if len(got) != len(wantAlerts) {
		t.Fatalf("alert rows = %d, want %d", len(got), len(wantAlerts))
	}
	for i := range wantAlerts {
		if got[i] != wantAlerts[i] {
			t.Errorf("alert row %d =\n%q\nwant\n%q", i, got[i], wantAlerts[i])
		}
	}

	p.SetScope("subagent-B")
	p.Toggle()
	p.cursor = 0
	p.optionCursor = 1
	wantPicker := PICKER
	got = p.PickerRows()
	if len(got) != len(wantPicker) {
		t.Fatalf("picker rows = %d, want %d", len(got), len(wantPicker))
	}
	for i := range wantPicker {
		if got[i] != wantPicker[i] {
			t.Errorf("picker row %d =\n%q\nwant\n%q", i, got[i], wantPicker[i])
		}
	}

	// The "no pending questions" notice.
	p.SetScope("subagent-none")
	p.close()
	lines = nil
	p.Toggle()
	if len(lines) != 1 || lines[0] != NO_PENDING {
		t.Errorf("notice = %q, want %q", lines, NO_PENDING)
	}

	// The "> Answer for …" prompt.
	p.SetScope(subagentquestions.ParentKey)
	lines = nil
	p.Toggle()
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if len(lines) != 1 || lines[0] != ANSWER_FOR {
		t.Errorf("answer prompt = %q, want %q", lines, ANSWER_FOR)
	}

	// The skip confirmation.
	lines = nil
	input = ""
	p.answerTarget = mgr.Pending(subagentquestions.ParentKey)[0].ID
	p.SubmitAnswer()
	if len(lines) != 1 || lines[0] != SKIP {
		t.Errorf("skip confirmation = %q, want %q", lines, SKIP)
	}

	// The answer confirmation.
	q2 := mgr.Enqueue(subagentquestions.ParentKey, "Second question", nil)
	lines = nil
	p.answerTarget = q2.ID
	input = "the answer"
	p.SubmitAnswer()
	if len(lines) != 1 || lines[0] != SENT {
		t.Errorf("answer confirmation = %q, want %q", lines, SENT)
	}
}

// The bytes the host's picker wrote before the panel took the rows
// over. 【一手】 the reference's own overlay, captured from the host
// implementation this panel replaces.
var (
	ALERTS = []string{
		"\x1b[38;2;38;139;210;1m  ⎿  \x1b[0m\x1b[38;2;101;123;131myou has a question · Parent question\x1b[39m",
		"\x1b[38;2;38;139;210;1m  ⎿  \x1b[0m\x1b[38;2;101;123;131msubagent-A has a question · A question\x1b[39m",
		"\x1b[38;2;38;139;210;1m  ⎿  \x1b[0m\x1b[38;2;101;123;131msubagent-B has a question · B question\x1b[39m",
	}

	PICKER = []string{
		"\x1b[38;2;38;139;210;1mPending questions for subagent-B\x1b[0m",
		"",
		"\x1b[38;2;38;139;210m> \x1b[0m\x1b[38;2;101;123;131mB question\x1b[39m",
		"      b1\x1b[39m",
		"",
		"\x1b[38;2;101;123;131mKeyboard: \x1b[39m\x1b[38;2;38;139;210m↑/↓\x1b[0m\x1b[38;2;101;123;131m Navigate  \x1b[39m\x1b[38;2;38;139;210m←/→\x1b[0m\x1b[38;2;101;123;131m Options  \x1b[39m\x1b[38;2;38;139;210menter\x1b[0m\x1b[38;2;101;123;131m Answer  \x1b[39m\x1b[38;2;38;139;210mesc\x1b[0m\x1b[38;2;101;123;131m Cancel\x1b[39m",
	}

	NO_PENDING = "\x1b[38;2;38;139;210;1m  ⎿  \x1b[0m\x1b[38;2;101;123;131mNo pending questions for subagent-none\x1b[39m"

	ANSWER_FOR = "\x1b[38;2;38;139;210;1m> \x1b[0m\x1b[38;2;38;139;210;1mAnswer for you: \x1b[0m\x1b[38;2;101;123;131mParent question\x1b[39m"

	SKIP = "\x1b[38;2;101;123;131m  ⎿  Question skipped.\x1b[39m"

	SENT = "\x1b[38;2;101;123;131m  ⎿  Answer sent to you.\x1b[39m"
)
