package helppanel

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/ui/slashcommands"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/vim"
)

// fixture is a panel with the host's seams stubbed out.
type fixture struct {
	panel *Panel
	// lines collects what the panel echoed into the transcript.
	lines []string
	// selected collects commands completed via SelectCommand seam.
	selected []string
	// vimOn is the vim seam's answer.
	vimOn bool
}

// newFixture builds a panel at 220×30 — the geometry every byte-exact
// /help capture was taken at.
func newFixture(vimOn bool) *fixture {
	f := &fixture{vimOn: vimOn}
	f.panel = New(Seams{
		Model:          func() string { return "Gemini 3.8 Flash" },
		Tier:           func() string { return "" },
		Cwd:            func() string { return "/tmp" },
		Email:          func() string { return "u@t" },
		ConversationID: func() string { return "id" },
		Width:          func() int { return 220 },
		Height:         func() int { return 30 },
		VimEnabled:     func() bool { return f.vimOn },
		Respond:        func(lines []string) { f.lines = append(f.lines, lines...) },
		SelectCommand:  func(cmd string) { f.selected = append(f.selected, cmd) },
	})
	return f
}

// Test_HelpListNavigation pins the v1.4.0 window model that replaced
// the cycle-137 pagination: an 18-row window at h=30 with a cursor
// over the whole catalog, scrolling one row at a time once the cursor
// reaches an edge, wrapping at both ends.
func Test_HelpListNavigation(t *testing.T) {
	const total = 99
	f := newFixture(false)
	p := f.panel
	p.Open(1)

	size := p.listWindowSize()
	if size != 18 {
		t.Fatalf("window at h=30 = %d, want 18", size)
	}

	// Down 17 times keeps the window at the top and parks the cursor
	// on the last visible row.
	for i := 0; i < size-1; i++ {
		p.listDown(total)
	}
	if p.cursor != size-1 || p.window != 0 {
		t.Fatalf("after Down x%d: cursor=%d window=%d, want %d/0",
			size-1, p.cursor, p.window, size-1)
	}
	// One more Down scrolls the window by exactly one row.
	p.listDown(total)
	if p.cursor != size || p.window != 1 {
		t.Fatalf("after Down x%d: cursor=%d window=%d, want %d/1",
			size, p.cursor, p.window, size)
	}

	// Ctrl+End jumps to the last row and scrolls to the bottom.
	p.listEnd(total)
	if p.cursor != total-1 || p.window != total-size {
		t.Fatalf("Ctrl+End: cursor=%d window=%d, want %d/%d",
			p.cursor, p.window, total-1, total-size)
	}
	// Down at the last row wraps to the first row and the top window.
	p.listDown(total)
	if p.cursor != 0 || p.window != 0 {
		t.Fatalf("Down at last: cursor=%d window=%d, want 0/0",
			p.cursor, p.window)
	}
	// Up at the first row wraps to the last row and the bottom window.
	p.listUp(total)
	if p.cursor != total-1 || p.window != total-size {
		t.Fatalf("Up at first: cursor=%d window=%d, want %d/%d",
			p.cursor, p.window, total-1, total-size)
	}
	// Ctrl+Home resets both.
	p.listHome()
	if p.cursor != 0 || p.window != 0 {
		t.Fatalf("Ctrl+Home: cursor=%d window=%d, want 0/0",
			p.cursor, p.window)
	}
}

// Test_HelpListNavigationShortCatalog covers the SHORTCUTS tab (27
// rows as of v1.4.7), where the catalog scrolls exactly once past the
// window's edge.
func Test_HelpListNavigationShortCatalog(t *testing.T) {
	const total = 27
	f := newFixture(false)
	p := f.panel
	p.Open(2)
	for i := 0; i < total-1; i++ {
		p.listDown(total)
	}
	if p.cursor != total-1 {
		t.Fatalf("cursor = %d, want %d", p.cursor, total-1)
	}
	// A 27-row catalog in an 18-row window does scroll once past the
	// edge — but only to total-size.
	wantWindow := total - p.listWindowSize()
	if p.window != wantWindow {
		t.Fatalf("window = %d, want %d", p.window, wantWindow)
	}
	p.listDown(total) // wrap
	if p.cursor != 0 || p.window != 0 {
		t.Fatalf("wrap: cursor=%d window=%d, want 0/0", p.cursor, p.window)
	}
}

// Test_HelpListLenMatchesCatalogs guards the row counts the /help
// status indicator prints: REF v1.4.0 reports "of 99 items" on the
// COMMANDS tab and "of 34 items" on SHORTCUTS. v1.4.7 leaves the basic
// navigation keys out of the SHORTCUTS tab, so the reference reports
// "of 27 items" there now.
func Test_HelpListLenMatchesCatalogs(t *testing.T) {
	f := newFixture(false)
	p := f.panel

	p.Open(1)
	if got := p.listLen(); got != len(slashcommands.Default) {
		t.Errorf("listLen(1) = %d, want %d", got, len(slashcommands.Default))
	}
	p.Open(2)
	if got := p.listLen(); got != len(tuilib.HelpShortcuts) {
		t.Errorf("listLen(2) = %d, want %d", got, len(tuilib.HelpShortcuts))
	}
	p.Open(0)
	if got := p.listLen(); got != 0 {
		t.Errorf("listLen(0) = %d, want 0 (GENERAL has no list)", got)
	}
	// Cycle 383 (A-23, A-24): the list is 25 rows. The reference's
	// own /help shows 27 — the clone drops the two rows it cannot
	// honour (Undo, Redo) and corrects the two rows whose keys do
	// something else in the clone (alt+j, ctrl+k).
	if len(tuilib.HelpShortcuts) != 25 {
		t.Errorf("HelpShortcuts has %d rows, want 25 (cycle 383)", len(tuilib.HelpShortcuts))
	}
}

// Test_HelpOverlayKeys pins the overlay's own key handling: the
// vim-style letters walk the list and the tabs, and "q" closes the
// overlay.
//
// 【一手】 agy v1.4.0: the /help overlay answers j/k/h/l and q.
func Test_HelpOverlayKeys(t *testing.T) {
	f := newFixture(false)
	p := f.panel
	p.Open(1)

	p.HandleKey(tea.Key{Code: 'j', Text: "j"})
	if p.cursor != 1 {
		t.Errorf("help 'j': cursor=%d, want 1", p.cursor)
	}

	p.HandleKey(tea.Key{Code: 'k', Text: "k"})
	if p.cursor != 0 {
		t.Errorf("help 'k': cursor=%d, want 0", p.cursor)
	}

	p.HandleKey(tea.Key{Code: 'l', Text: "l"})
	if p.Tab() != 2 {
		t.Errorf("help 'l': tab=%d, want 2", p.Tab())
	}

	p.HandleKey(tea.Key{Code: 'h', Text: "h"})
	if p.Tab() != 1 {
		t.Errorf("help 'h': tab=%d, want 1", p.Tab())
	}

	p.HandleKey(tea.Key{Code: 'q', Text: "q"})
	if p.Active() {
		t.Errorf("help 'q': the overlay is still open, want closed")
	}
}

// Test_HelpOverlayClosedHandlesNoKey pins the panel's answer for a
// closed overlay: it handles no key, so the host's own pipeline keeps
// the key.
func Test_HelpOverlayClosedHandlesNoKey(t *testing.T) {
	f := newFixture(false)
	p := f.panel

	if p.HandleKey(tea.Key{Code: 'j', Text: "j"}) {
		t.Error("a closed overlay must not handle a key")
	}
	if p.Active() {
		t.Error("a closed overlay must not be active")
	}
	if p.Rows() != "" {
		t.Errorf("a closed overlay must render nothing, got %q", p.Rows())
	}
}

// Test_HelpOverlayCancelEchoes pins the exit echo: the transcript
// carries the "/help" block and the "Exited /help command" line —
// the reference's own wording.
func Test_HelpOverlayCancelEchoes(t *testing.T) {
	f := newFixture(false)
	p := f.panel
	p.Open(0)

	p.Cancel()
	if p.Active() {
		t.Error("Cancel must close the overlay")
	}
	if len(f.lines) == 0 {
		t.Fatal("Cancel must echo the exit line")
	}
	joined := strings.Join(f.lines, "\n")
	if !strings.Contains(joined, "Exited /help command") {
		t.Errorf("the exit echo is missing its line: %q", joined)
	}
	if !strings.Contains(joined, "/help") {
		t.Errorf("the exit echo is missing the command line: %q", joined)
	}
}

// Test_HelpOverlayTabCycle pins the tab cycle and the reset that
// follows it: the reference does not preserve a per-tab scroll.
func Test_HelpOverlayTabCycle(t *testing.T) {
	f := newFixture(false)
	p := f.panel
	p.Open(1)
	p.listDown(50)
	if p.cursor != 1 {
		t.Fatalf("cursor = %d, want 1", p.cursor)
	}

	p.HandleKey(tea.Key{Code: tea.KeyTab})
	if p.Tab() != 2 {
		t.Errorf("after Tab: tab=%d, want 2", p.Tab())
	}
	if p.cursor != 0 || p.window != 0 {
		t.Errorf("after Tab: cursor=%d window=%d, want 0/0", p.cursor, p.window)
	}

	p.HandleKey(tea.Key{Code: tea.KeyTab})
	if p.Tab() != 0 {
		t.Errorf("after Tab: tab=%d, want 0 (wrapped)", p.Tab())
	}
}

// TestHelpTabsGainVimOnlyWhenEnabled pins the tab bar: the vim
// catalogue tab is appended only when vim mode is on, so the default
// three-tab bar is unchanged for everyone else.
func TestHelpTabsGainVimOnlyWhenEnabled(t *testing.T) {
	off := newFixture(false)
	if got := off.panel.tabs(); len(got) != 3 {
		t.Fatalf("tabs with vim off = %v, want 3", got)
	}
	if got := tuilib.RenderTabBar(0); strings.Contains(got, vim.HelpTitle) {
		t.Fatal("the default tab bar must not mention vim")
	}

	on := newFixture(true)
	tabs := on.panel.tabs()
	if len(tabs) != 4 || tabs[3] != vim.HelpTitle {
		t.Fatalf("tabs with vim on = %v", tabs)
	}
	bar := tuilib.RenderTabBarTabs(3, tabs)
	if !strings.Contains(bar, vim.HelpTitle) {
		t.Fatalf("4-tab bar missing vim: %q", bar)
	}
	on.panel.Open(3)
	if got := on.panel.listLen(); got != len(vim.HelpRows) {
		t.Fatalf("listLen(3) = %d, want %d", got, len(vim.HelpRows))
	}
	out := on.panel.Rows()
	if !strings.Contains(out, "Vim Modal Shortcuts") {
		t.Fatalf("vim help tab output = %q", out)
	}
	// The row counts of the original tabs are unchanged.
	on.panel.Open(2)
	if got := on.panel.listLen(); got != len(tuilib.HelpShortcuts) {
		t.Fatalf("listLen(2) = %d, want %d", got, len(tuilib.HelpShortcuts))
	}
}

// Test_HelpOverlayOpenResets pins the open: the window and the cursor
// start at the top of the list, whatever the overlay was showing when
// it was closed.
func Test_HelpOverlayOpenResets(t *testing.T) {
	f := newFixture(false)
	p := f.panel
	p.Open(1)
	p.listDown(50)

	p.Open(1)
	if p.cursor != 0 || p.window != 0 {
		t.Errorf("after Open: cursor=%d window=%d, want 0/0", p.cursor, p.window)
	}
}

// Test_HelpOverlayOpenClampsTab pins the open's tab clamp: a tab that
// does not exist falls back to the GENERAL tab, so a vim-less client
// can never land on the vim tab.
func Test_HelpOverlayOpenClampsTab(t *testing.T) {
	f := newFixture(false)
	p := f.panel
	p.Open(3)
	if p.Tab() != 0 {
		t.Errorf("Open(3) without vim: tab=%d, want 0", p.Tab())
	}
	if !p.Active() {
		t.Error("Open must show the overlay")
	}
}

// Test_HelpOverlaySelectCommandOnEnter pins Enter on the COMMANDS tab:
// the highlighted command is passed to SelectCommand, the overlay
// closes, and no exit echo is produced.
func Test_HelpOverlaySelectCommandOnEnter(t *testing.T) {
	f := newFixture(false)
	p := f.panel
	p.Open(1) // COMMANDS tab

	cmds := slashcommands.Sorted()
	if len(cmds) < 2 {
		t.Fatalf("expected at least 2 slash commands, got %d", len(cmds))
	}

	// First command at cursor=0
	handled := p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if !handled {
		t.Fatal("Enter should be handled by open overlay")
	}
	if p.Active() {
		t.Error("Enter on COMMANDS tab must close the overlay")
	}
	if len(f.selected) != 1 || f.selected[0] != cmds[0].Name {
		t.Fatalf("selected = %v, want [%s]", f.selected, cmds[0].Name)
	}
	if len(f.lines) != 0 {
		t.Errorf("Enter must not produce exit echo, got %v", f.lines)
	}

	// Second command at cursor=1
	p.Open(1)
	p.HandleKey(tea.Key{Code: tea.KeyDown})
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if p.Active() {
		t.Error("Enter on COMMANDS tab must close the overlay")
	}
	if len(f.selected) != 2 || f.selected[1] != cmds[1].Name {
		t.Fatalf("selected = %v, want second element %s", f.selected, cmds[1].Name)
	}
	if len(f.lines) != 0 {
		t.Errorf("Enter must not produce exit echo, got %v", f.lines)
	}
}

// Test_HelpOverlayEnterOnOtherTabsClosesWithoutSelect pins Enter on
// non-COMMANDS tabs (0: General, 2: Shortcuts, 3: Vim): the overlay
// closes cleanly without calling SelectCommand or echoing exit line.
func Test_HelpOverlayEnterOnOtherTabsClosesWithoutSelect(t *testing.T) {
	for _, tc := range []struct {
		name  string
		tab   int
		vimOn bool
	}{
		{"General tab", 0, false},
		{"Shortcuts tab", 2, false},
		{"Vim tab", 3, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(tc.vimOn)
			p := f.panel
			p.Open(tc.tab)
			if !p.Active() {
				t.Fatalf("failed to open tab %d", tc.tab)
			}
			handled := p.HandleKey(tea.Key{Code: tea.KeyEnter})
			if !handled {
				t.Fatal("Enter should be handled")
			}
			if p.Active() {
				t.Errorf("Enter on tab %d must close the overlay", tc.tab)
			}
			if len(f.selected) != 0 {
				t.Errorf("Enter on tab %d must not call SelectCommand, got %v", tc.tab, f.selected)
			}
			if len(f.lines) != 0 {
				t.Errorf("Enter on tab %d must not produce exit echo, got %v", tc.tab, f.lines)
			}
		})
	}
}

// Test_HelpOverlayPureMessagePassing verifies the modern Elm/Bubbletea contract:
// without any Seams or fixture closures, the panel handles keys purely by returning
// state changes and typed tea.Cmd messages (CommandSelectedMsg, DismissedMsg).
func Test_HelpOverlayPureMessagePassing(t *testing.T) {
	// Zero seams panel!
	p := New(Seams{})
	p.Open(1) // COMMANDS tab

	cmds := slashcommands.Sorted()
	if len(cmds) < 2 {
		t.Fatalf("expected at least 2 slash commands, got %d", len(cmds))
	}

	// 1. Enter on command at cursor=0 yields CommandSelectedMsg
	handled, cmd := p.Update(tea.Key{Code: tea.KeyEnter})
	if !handled {
		t.Fatal("expected handled=true")
	}
	if p.Active() {
		t.Error("panel must be closed after selection")
	}
	if cmd == nil {
		t.Fatal("expected non-nil tea.Cmd producing CommandSelectedMsg")
	}
	msg := cmd()
	sel, ok := msg.(CommandSelectedMsg)
	if !ok || sel.Command != cmds[0].Name {
		t.Fatalf("expected CommandSelectedMsg with %q, got %#v", cmds[0].Name, msg)
	}

	// 2. Down and Enter yields the second command
	p.Open(1)
	p.Update(tea.Key{Code: tea.KeyDown})
	handled, cmd = p.Update(tea.Key{Code: tea.KeyEnter})
	if !handled || p.Active() || cmd == nil {
		t.Fatal("expected successful handled & closed second command selection")
	}
	msg = cmd()
	sel, ok = msg.(CommandSelectedMsg)
	if !ok || sel.Command != cmds[1].Name {
		t.Fatalf("expected CommandSelectedMsg with %q, got %#v", cmds[1].Name, msg)
	}

	// 3. Cancel ('q') on GENERAL tab yields DismissedMsg{EchoLine: true}
	p.Open(0)
	handled, cmd = p.Update(tea.Key{Code: 'q'})
	if !handled || p.Active() || cmd == nil {
		t.Fatal("expected successful handled & closed on 'q'")
	}
	msg = cmd()
	dis, ok := msg.(DismissedMsg)
	if !ok || !dis.EchoLine {
		t.Fatalf("expected DismissedMsg{EchoLine: true}, got %#v", msg)
	}

	// 4. Enter on SHORTCUTS tab yields DismissedMsg{EchoLine: false}
	p.Open(2)
	handled, cmd = p.Update(tea.Key{Code: tea.KeyEnter})
	if !handled || p.Active() || cmd == nil {
		t.Fatal("expected successful handled & closed on Enter")
	}
	msg = cmd()
	dis, ok = msg.(DismissedMsg)
	if !ok || dis.EchoLine {
		t.Fatalf("expected DismissedMsg{EchoLine: false}, got %#v", msg)
	}
}
