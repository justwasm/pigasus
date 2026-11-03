// panel_test.go — the /permissions editor's own half.
//
// Cycle 381 moved the panel out of internal/cliapp: the scope picker,
// the per-scope lists, the add/edit rule form, the cursor, the keys and
// the writes are the package's, and the host keeps only the seam (the
// way out of the panel). The behaviour is the clone's own from cycle 205
// on, so the tests move with the code they exercise.
//
// The tests build the panel directly — there is no runMode behind it —
// so a seam is a closure over a test value.

package permissionspanel

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/core/policy/permissions"
	termcolor "github.com/justwasm/pigasus/internal/ui/render/color"
)

// TestMain points the configuration home at a scratch directory for the
// whole package.
//
// The permission policy reads two files: settings.json (through
// internal/core/confhome, which honours $ANTIGRAVITY_HOME and then
// $HOME) and the reference's default CLI project file (through
// os.UserHomeDir, which is $HOME on unix). A test that read the user's
// own home would see the user's own rules — and a test that wrote to it
// would edit them. Both are avoided by moving the home.
func TestMain(m *testing.M) {
	root, err := os.MkdirTemp("", "pigasus-permissionspanel-home-")
	if err != nil {
		panic(err)
	}
	if err := os.Setenv("HOME", root); err != nil {
		panic(err)
	}
	if err := os.Setenv("ANTIGRAVITY_HOME",
		filepath.Join(root, ".gemini", "antigravity-cli")); err != nil {
		panic(err)
	}
	code := m.Run()
	_ = os.RemoveAll(root)
	os.Exit(code)
}

// ---------------------------------------------------------------- helpers

// newTestPanel builds a /permissions panel whose way out is a no-op —
// none of these tests closes the panel.
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

// TestPermissionsScopeLayout pins the stage-0 frame — the scope picker —
// and the blurb that follows the cursor.
func TestPermissionsScopeLayout(t *testing.T) {
	p := newTestPanel()
	assertLines(t, plainLines(p.Lines()), []string{
		"Permission Config Editor",
		"",
		"Select a config scope to edit:",
		"",
		"> Project",
		"  Shared with Pigasus",
		"  Global",
		"",
		"  Rules that apply only to this project (highest priority)",
		"",
		"Keyboard: ↑/↓ Navigate  enter Save  esc Close",
	})
	// The blurb follows the cursor. Recovered by driving REF's panel
	// with Down (/tmp/agycmp/nav-permissions.log).
	want := []string{
		"  Rules that apply only to this project (highest priority)",
		"  Rules that apply to all Pigasus products",
		"  Global rules",
	}
	for cursor, wantBlurb := range want {
		p.SetCursor(cursor)
		if got := plainLines(p.Lines())[8]; got != wantBlurb {
			t.Errorf("cursor %d blurb = %q, want %q", cursor, got, wantBlurb)
		}
	}
}

// TestCycle205PermissionsScopeWrap: /permissions stage 1 wraps ↑/↓
// (probe_verify2.py: Up at Project lands on Global).
func TestCycle205PermissionsScopeWrap(t *testing.T) {
	p := newTestPanel()
	if handled, _ := p.HandleKey(tea.Key{Code: tea.KeyUp}); !handled {
		t.Fatal("Up not handled")
	}
	if p.Cursor() != 2 {
		t.Fatalf("Up at row 0: cursor = %d, want 2 (Global)", p.Cursor())
	}
	p.HandleKey(tea.Key{Code: tea.KeyDown})
	if p.Cursor() != 0 {
		t.Fatalf("Down at last row: cursor = %d, want 0 (Project)", p.Cursor())
	}
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if p.Stage() != StageLists {
		t.Fatalf("Enter on a scope: stage = %d, want 1", p.Stage())
	}
	if p.Scope() != permissions.PermScopeProject {
		t.Fatalf("Enter on Project: scope = %d, want 0", p.Scope())
	}
}

// TestCycle205PermissionsTabView asserts the stage-2 frame for a populated
// scope, transcribed from perm-after-enter.raw.txt / permissions-global
// stage2.
func TestCycle205PermissionsTabView(t *testing.T) {
	p := newTestPanel()
	p.stage = StageLists
	p.scope = permissions.PermScopeShared
	p.tab = 0
	p.shared[0] = []string{"command(grep)", "command(ls)"}
	p.cursor = 1
	assertLines(t, plainLines(p.Lines()), []string{
		"Permissions — Shared with Pigasus",
		" allowlist (2)    denylist (0)    asklist (0)   (←/→ to switch)",
		"",
		"  command(grep)",
		"> command(ls)",
		"",
		"Keyboard: ↑/↓ Navigate  ←/→ Switch View  a Add rule  e Edit rule  d/⌫ Delete rule  esc Close",
	})
}

// TestCycle205PermissionsEmptyState: the reference prints `No <list> rules.`
// with no leading space (perm-after-enter.raw.txt line 3).
func TestCycle205PermissionsEmptyState(t *testing.T) {
	p := newTestPanel()
	p.stage = StageLists
	p.scope = permissions.PermScopeProject
	p.tab = 2
	assertLines(t, plainLines(p.Lines()), []string{
		"Permissions — Project",
		" allowlist (0)    denylist (0)    asklist (0)   (←/→ to switch)",
		"",
		"No asklist rules.",
		"",
		"Keyboard: ↑/↓ Navigate  ←/→ Switch View  a Add rule  e Edit rule  d/⌫ Delete rule  esc Close",
	})
}

// TestCycle205PermissionsTabSwitchWraps: ←/→ wrap (0 → 2 → 1 → 0).
func TestCycle205PermissionsTabSwitchWraps(t *testing.T) {
	p := newTestPanel()
	p.stage = StageLists
	p.HandleKey(tea.Key{Code: tea.KeyLeft})
	if p.Tab() != 2 {
		t.Fatalf("Left from allowlist: tab = %d, want 2", p.Tab())
	}
	p.HandleKey(tea.Key{Code: tea.KeyRight})
	if p.Tab() != 0 {
		t.Fatalf("Right from asklist: tab = %d, want 0", p.Tab())
	}
}

// TestCycle205PermissionsFormRender: the add form's frame. The reference's
// form legend separates the first two legend pairs with a TAB
// (probe_perm_add.py, 3/3 runs); the clone uses two spaces there because its
// cell renderer drops raw control characters — see 存疑 in the cycle report.
func TestCycle205PermissionsFormRender(t *testing.T) {
	p := newTestPanel()
	p.stage = StageForm
	p.form = FormAdd
	p.scope = permissions.PermScopeGlobal
	p.tab = 0
	assertLines(t, plainLines(p.Lines()), []string{
		"Add Rule — Global — allowlist",
		"",
		"Enter a permission rule:",
		"",
		" ",
		"",
		"Format: action(target)",
		"",
		"Keyboard: ↑/↓ Navigate  enter Select  esc Close",
	})
	// The input row must carry the reverse-video cursor.
	raw := p.Lines()[4]
	if !strings.Contains(raw, "\x1b[7m\x1b[37m \x1b[0m") {
		t.Errorf("input row missing reverse-video cursor: %q", raw)
	}
}

// TestCycle205PermissionsFormError: a rejected rule keeps the form open and
// inserts the error row between the input and the Format hint
// (perm-edit-after.raw.txt).
func TestCycle205PermissionsFormError(t *testing.T) {
	p := newTestPanel()
	p.stage = StageForm
	p.form = FormEdit
	p.scope = permissions.PermScopeGlobal
	p.text = "command(grep)ZZ"
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if p.Stage() != StageForm {
		t.Fatalf("invalid rule closed the form: stage = %d", p.Stage())
	}
	assertLines(t, plainLines(p.Lines()), []string{
		"Edit Rule — Global — allowlist",
		"",
		"Enter a permission rule:",
		"",
		"command(grep)ZZ ",
		"",
		`✗ invalid grant string: "command(grep)ZZ"`,
		"",
		"Format: action(target)",
		"",
		"Keyboard: ↑/↓ Navigate  enter Select  esc Close",
	})
}

// TestCycle205PermissionsAddDelete exercises the add/delete actions on the
// session-local Shared scope so the test never touches settings.json.
func TestCycle205PermissionsAddDelete(t *testing.T) {
	p := newTestPanel()
	p.scope = permissions.PermScopeShared
	p.stage = StageForm
	p.form = FormAdd
	for _, r := range "command(grep)" {
		p.HandleKey(tea.Key{Text: string(r)})
	}
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if got := p.shared[0]; len(got) != 1 || got[0] != "command(grep)" {
		t.Fatalf("after add: shared[allow] = %q", got)
	}
	if p.Stage() != StageLists {
		t.Fatalf("after add: stage = %d, want 1", p.Stage())
	}
	p.cursor = 0
	p.HandleKey(tea.Key{Text: "d"})
	if got := p.shared[0]; len(got) != 0 {
		t.Fatalf("after delete: shared[allow] = %q, want empty", got)
	}
	// Backspace is the second advertised delete key.
	p.shared[0] = []string{"command(ls)"}
	p.HandleKey(tea.Key{Code: tea.KeyBackspace})
	if got := p.shared[0]; len(got) != 0 {
		t.Fatalf("after backspace delete: shared[allow] = %q, want empty", got)
	}
}

// TestCycle205PermissionsEditDeletesThenAppends: Edit removes the old rule
// and appends the new one at the end.
func TestCycle205PermissionsEditDeletesThenAppends(t *testing.T) {
	p := newTestPanel()
	p.scope = permissions.PermScopeShared
	p.stage = StageLists
	p.shared[0] = []string{"command(a)", "command(b)"}
	p.cursor = 0
	p.HandleKey(tea.Key{Text: "e"})
	if p.Stage() != StageForm || p.Form() != FormEdit {
		t.Fatalf("e on a row: stage=%d form=%d, want 2/2", p.Stage(), p.Form())
	}
	if p.Text() != "command(a)" {
		t.Fatalf("edit prefill = %q, want %q", p.Text(), "command(a)")
	}
	p.text = "command(c)"
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	got := p.shared[0]
	want := []string{"command(b)", "command(c)"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("after edit: %q, want %q", got, want)
	}
	// `e` with no rows selected opens the Add form.
	p.shared[0] = nil
	p.cursor = 0
	p.HandleKey(tea.Key{Text: "e"})
	if p.Form() != FormAdd {
		t.Fatalf("e with no rows: form = %d, want 1 (add)", p.Form())
	}
}

// TestPermissionsCtrlGIsTheEditAlias pins the reference's own second
// edit key: "Press E (or Ctrl + G) to edit the highlighted rule"
// (official-docs/cli/commands/permissions.md:41). Ctrl+G opens the same
// edit form as `e`, with the same prefill.
func TestPermissionsCtrlGIsTheEditAlias(t *testing.T) {
	p := newTestPanel()
	p.scope = permissions.PermScopeShared
	p.stage = StageLists
	p.shared[0] = []string{"command(a)", "command(b)"}
	p.cursor = 0
	p.HandleKey(tea.Key{Code: 'g', Mod: tea.ModCtrl})
	if p.Stage() != StageForm || p.Form() != FormEdit {
		t.Fatalf("ctrl+g on a row: stage=%d form=%d, want 2/2", p.Stage(), p.Form())
	}
	if p.Text() != "command(a)" {
		t.Fatalf("edit prefill = %q, want %q", p.Text(), "command(a)")
	}
}

// TestPermissionsPanelRowCount pins the cursor bound of the three stages:
// the scope picker has three rows, the tab view has as many rows as the
// selected list holds and the form has one.
func TestPermissionsPanelRowCount(t *testing.T) {
	p := newTestPanel()
	if got := p.RowCount(); got != 3 {
		t.Errorf("stage 0 row count = %d, want 3", got)
	}
	p.stage = StageLists
	p.scope = permissions.PermScopeShared
	p.shared[0] = []string{"command(a)", "command(b)"}
	if got := p.RowCount(); got != 2 {
		t.Errorf("stage 1 row count = %d, want 2", got)
	}
	p.stage = StageForm
	if got := p.RowCount(); got != 1 {
		t.Errorf("stage 2 row count = %d, want 1", got)
	}
}

// TestPermissionsPanelEscCloses pins that Esc on the scope picker leaves
// the panel: the panel owns the way out, and the host's Close seam is
// what it calls.
func TestPermissionsPanelEscCloses(t *testing.T) {
	closed := false
	p := New(Seams{Close: func() tea.Cmd { closed = true; return nil }})
	if handled, _ := p.HandleKey(tea.Key{Code: tea.KeyEsc}); !handled {
		t.Fatal("Esc not handled")
	}
	if !closed {
		t.Error("Esc on the scope picker should close the panel")
	}
}

// TestPermissionsPanelEscFromListReturns pins that Esc on the tab view
// goes *back* to the scope picker — with the cursor on the scope the
// user came from — rather than closing the panel.
func TestPermissionsPanelEscFromListReturns(t *testing.T) {
	closed := false
	p := New(Seams{Close: func() tea.Cmd { closed = true; return nil }})
	p.stage = StageLists
	p.scope = permissions.PermScopeShared
	p.HandleKey(tea.Key{Code: tea.KeyEsc})
	if closed {
		t.Error("Esc on the tab view should not close the panel")
	}
	if p.Stage() != StageScopes {
		t.Errorf("stage = %d, want %d (the scope picker)", p.Stage(), StageScopes)
	}
	if p.Cursor() != permissions.PermScopeShared {
		t.Errorf("cursor = %d, want %d (the scope it came from)",
			p.Cursor(), permissions.PermScopeShared)
	}
}
