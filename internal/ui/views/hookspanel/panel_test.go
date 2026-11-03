// panel_test.go — the /hooks editor's own half.
//
// Cycle 381 moved the panel out of internal/cliapp: the hook-type list,
// the per-type matcher list, the add-matcher and add-hook forms, the
// cursor, the keys and the state are the package's, and the host keeps
// only the seam (the way out of the panel). The behaviour is the clone's
// own from cycle 205 on, so the tests move with the code they exercise.
//
// The tests build the panel directly — there is no runMode behind it —
// so a seam is a closure over a test value.

package hookspanel

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	corehooks "github.com/justwasm/pigasus/internal/core/policy/hooks"
	termcolor "github.com/justwasm/pigasus/internal/ui/render/color"
)

// ---------------------------------------------------------------- helpers

// newTestPanel builds a /hooks panel whose way out is a no-op — none of
// these tests closes the panel.
func newTestPanel() *Panel {
	return New(Seams{Close: func() tea.Cmd { return nil }})
}

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

// ---------------------------------------------------------------- tests

// TestHooksTypeListLayout pins the stage-0 frame — the hook types and
// the padded description column.
func TestHooksTypeListLayout(t *testing.T) {
	p := newTestPanel()
	assertLines(t, plainLines(p.Lines()), []string{
		" Hooks",
		"   6 hook types",
		"",
		" > PreToolUse      Before tool execution",
		"   PostToolUse     After tool execution",
		"   PreInvocation   Before each LLM invocation",
		"   PostInvocation  After each LLM invocation",
		"   Stop            When agent tries to exit",
		"   SessionStart    When a session starts",
		"",
		"   ↑/↓ Navigate · enter Select",
	})
	// Moving the cursor moves the highlight, not the text.
	p.SetCursor(4)
	got := plainLines(p.Lines())
	if !strings.HasPrefix(got[7], " > Stop") {
		t.Errorf("row 7 should be selected: %q", got[7])
	}
	if !strings.HasPrefix(got[3], "   PreToolUse") {
		t.Errorf("row 3 should be deselected: %q", got[3])
	}
}

// TestCycle205HooksMatchersTool: the PreToolUse matcher list
// (hooks-enter.raw.txt).
func TestCycle205HooksMatchersTool(t *testing.T) {
	p := newTestPanel()
	p.stage = StageMatchers
	p.idx = 0
	assertLines(t, plainLines(p.Lines()), []string{
		" PreToolUse — Matchers",
		"   Before tool execution",
		"",
		" > + Add new matcher...",
		"   + Match all (no filter)",
		"   No hooks configured",
		"",
		"   ↑/↓ Navigate · enter Select · e Toggle · backspace Delete · esc Back",
	})
}

// TestCycle205HooksMatchersLifecycle: a non-tool hook offers
// "+ Add new hook..." and no "Match all" row (probe_hooks.py).
func TestCycle205HooksMatchersLifecycle(t *testing.T) {
	p := newTestPanel()
	p.stage = StageMatchers
	p.idx = 2 // PreInvocation
	assertLines(t, plainLines(p.Lines()), []string{
		" PreInvocation — Matchers",
		"   Before each LLM invocation",
		"",
		" > + Add new hook...",
		"   No hooks configured",
		"",
		"   ↑/↓ Navigate · enter Select · e Toggle · backspace Delete · esc Back",
	})
}

// TestCycle205HooksMatcherForm: the add-matcher form
// (hooks-add-enter.raw.txt).
func TestCycle205HooksMatcherForm(t *testing.T) {
	p := newTestPanel()
	p.stage = StageForm
	p.idx = 0
	assertLines(t, plainLines(p.Lines()), []string{
		" Add new matcher for PreToolUse",
		"",
		"   Before tool execution",
		"",
		"   Matcher:",
		" >  ",
		"",
		"   Examples:",
		"   • Write (single tool)",
		"   • Write|Edit (multiple tools)",
		"   • Web.* (regex pattern)",
		"",
		"   enter Confirm · esc Cancel",
	})
}

// TestCycle205HooksHookForm: the add-hook form for a lifecycle hook
// (probe_hooks.py, PreInvocation).
func TestCycle205HooksHookForm(t *testing.T) {
	p := newTestPanel()
	p.stage = StageForm
	p.idx = 2
	assertLines(t, plainLines(p.Lines()), []string{
		" Add new hook",
		"",
		"   Event: PreInvocation — Before each LLM invocation",
		"",
		"   Name:",
		" >  ",
		"",
		"   Command:",
		"      ",
		"",
		"   Timeout (seconds):",
		"     default: 30",
		"",
		"   ⚠ Hooks run shell commands with your full permissions.",
		"",
		"   tab Switch field · enter Confirm · esc Cancel",
	})
}

// TestCycle205HooksNavigation: list → matchers → form → back.
func TestCycle205HooksNavigation(t *testing.T) {
	p := newTestPanel()
	p.SetCursor(1)
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if p.Stage() != StageMatchers || p.Idx() != 1 {
		t.Fatalf("Enter on PostToolUse: stage=%d idx=%d, want 1/1", p.Stage(), p.Idx())
	}
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if p.Stage() != StageForm {
		t.Fatalf("Enter on the add row: stage=%d, want 2", p.Stage())
	}
	p.HandleKey(tea.Key{Code: tea.KeyEsc})
	if p.Stage() != StageMatchers {
		t.Fatalf("Esc from the form: stage=%d, want 1", p.Stage())
	}
	p.HandleKey(tea.Key{Code: tea.KeyEsc})
	if p.Stage() != StageTypes {
		t.Fatalf("Esc from the matcher list: stage=%d, want 0", p.Stage())
	}
	// Esc from the matcher list puts the cursor back on the type the user
	// came from.
	if p.Cursor() != 1 {
		t.Errorf("cursor = %d, want 1 (the type it came from)", p.Cursor())
	}
}

// TestHooksPanelTypeListClamps: ↑/↓ clamp at the two ends of the type
// list — the reference does not wrap here.
func TestHooksPanelTypeListClamps(t *testing.T) {
	p := newTestPanel()
	up := tea.Key{Code: tea.KeyUp}
	if handled, _ := p.HandleKey(up); !handled || p.Cursor() != 0 {
		t.Errorf("Up at 0 should clamp: handled=%v cursor=%d", handled, p.Cursor())
	}
	for i := 0; i < 10; i++ {
		p.HandleKey(tea.Key{Code: tea.KeyDown})
	}
	if p.Cursor() != 5 {
		t.Errorf("Down should clamp at last row: cursor=%d want 5", p.Cursor())
	}
}

// TestHooksPanelRowCount pins the cursor bound of the three stages: the
// type list has one row per hook type, a tool type's matcher list two
// and a lifecycle type's one, and the form one.
func TestHooksPanelRowCount(t *testing.T) {
	p := newTestPanel()
	if got := p.RowCount(); got != 6 {
		t.Errorf("stage 0 row count = %d, want 6", got)
	}
	p.stage = StageMatchers
	p.idx = 0
	if got := p.RowCount(); got != 2 {
		t.Errorf("stage 1 row count (tool hook) = %d, want 2", got)
	}
	p.idx = 2
	if got := p.RowCount(); got != 1 {
		t.Errorf("stage 1 row count (lifecycle hook) = %d, want 1", got)
	}
	p.stage = StageForm
	if got := p.RowCount(); got != 1 {
		t.Errorf("stage 2 row count = %d, want 1", got)
	}
}

// TestHooksPanelFormTypes: the add-matcher form takes the matcher text
// and the add-hook form takes the name; Tab cycles the add-hook form's
// three fields but is inert on the matcher form.
func TestHooksPanelFormTypes(t *testing.T) {
	p := newTestPanel()
	p.stage = StageForm
	p.idx = 0
	for _, r := range "Write|Edit" {
		p.HandleKey(tea.Key{Text: string(r)})
	}
	if p.Match() != "Write|Edit" {
		t.Errorf("matcher text = %q, want %q", p.Match(), "Write|Edit")
	}
	// Backspace deletes the last character.
	p.HandleKey(tea.Key{Code: tea.KeyBackspace})
	if p.Match() != "Write|Edit"[:len("Write|Edit")-1] {
		t.Errorf("after backspace: matcher text = %q", p.Match())
	}
	// Tab is inert on a tool hook's form.
	p.HandleKey(tea.Key{Code: tea.KeyTab})
	if p.Field() != 0 {
		t.Errorf("Tab on the matcher form: field = %d, want 0", p.Field())
	}

	// The add-hook form cycles its three fields and types the name.
	q := newTestPanel()
	q.stage = StageForm
	q.idx = 2
	for _, r := range "hook" {
		q.HandleKey(tea.Key{Text: string(r)})
	}
	if q.Name() != "hook" {
		t.Errorf("hook name = %q, want %q", q.Name(), "hook")
	}
	q.HandleKey(tea.Key{Code: tea.KeyTab})
	if q.Field() != 1 {
		t.Errorf("after Tab: field = %d, want 1", q.Field())
	}
	q.HandleKey(tea.Key{Code: tea.KeyTab})
	if q.Field() != 2 {
		t.Errorf("after two Tabs: field = %d, want 2", q.Field())
	}
	q.HandleKey(tea.Key{Code: tea.KeyTab})
	if q.Field() != 0 {
		t.Errorf("after three Tabs: field = %d, want 0", q.Field())
	}
}

// TestHooksPanelEscCloses pins that Esc on the type list leaves the
// panel: the panel owns the way out, and the host's Close seam is what
// it calls.
func TestHooksPanelEscCloses(t *testing.T) {
	closed := false
	p := New(Seams{Close: func() tea.Cmd { closed = true; return nil }})
	if handled, _ := p.HandleKey(tea.Key{Code: tea.KeyEsc}); !handled {
		t.Fatal("Esc not handled")
	}
	if !closed {
		t.Error("Esc on the type list should close the panel")
	}
}

// TestHooksPanelTypeTable pins the panel's vocabulary: the six events the
// documentation names, in the order the panel lists them, and the two
// tool events among them.
//
// The names themselves belong to the core tier
// (internal/core/policy/hooks) — the panel reads them from there, so it
// cannot drift away from the loader and the validator.
func TestHooksPanelTypeTable(t *testing.T) {
	want := []string{
		string(corehooks.EventPreToolUse),
		string(corehooks.EventPostToolUse),
		string(corehooks.EventPreInvocation),
		string(corehooks.EventPostInvocation),
		string(corehooks.EventStop),
		string(corehooks.EventSessionStart),
	}
	p := newTestPanel()
	rows := plainLines(p.Lines())
	// The rows start after the title, the count and the blank line.
	for i, name := range want {
		row := rows[3+i]
		if !strings.Contains(row, name) {
			t.Errorf("row %d = %q, want the %s row", 3+i, row, name)
		}
	}
	// The two tool events use the matcher form; the other four do not.
	for i, name := range want {
		tool := i == 0 || i == 1
		if got := IsTool(i); got != tool {
			t.Errorf("%s: IsTool = %v, want %v", name, got, tool)
		}
	}
}
