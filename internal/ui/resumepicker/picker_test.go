// picker_test.go — the /resume picker's own behaviour.
//
// The picker is presentation: it draws the conversations and it asks the
// session to act on the one the user picked. These cases drive the picker
// directly; the host's own cases live in internal/cliapp.

package resumepicker

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/justwasm/pigasus/internal/app/session/convstore"
)

// newTestPicker returns a picker bound to a scratch store.
//
// The seams answer the picker's three questions: which conversation the
// session is showing, what the store holds, and what to do when the user
// picks or renames a conversation.
func newTestPicker(t *testing.T) *Picker {
	t.Helper()
	return New(Seams{
		CurrentConversationID: func() string { return "" },
		ListConversations: func() ([]convstore.ConversationMeta, error) {
			return convstore.ListConversations()
		},
		ResumeConversation: func(meta convstore.ConversationMeta) tea.Cmd { return nil },
		RenameConversation: func(meta convstore.ConversationMeta) {},
	})
}

// TestRowDropsBlankAgeCell pins the row geometry when the age is blank.
//
// Cache entries whose summary.UpdatedAt is 0001-01-01 have no age in the
// reference. agy 1.4.2 then omits the 12-cell age field entirely and lets
// the title grow by the 14 columns it frees, so the steps cell sits flush
// against the last column. The clone kept a blank 12-cell field and lost
// those columns (rows measured 186 cells wide against the reference's 200).
func TestRowDropsBlankAgeCell(t *testing.T) {
	p := newTestPicker(t)
	p.mode = true
	p.list = []convstore.ConversationMeta{{
		ID:        "0495d3b3-fb08-b3c0-9290-41ec9c5f0251",
		Title:     "0495d3b3-fb08-b3c0-9290-41ec9c5f0251",
		StepCount: 2,
		// ModTime zero: no age column.
	}}

	var row string
	for _, l := range strings.Split(p.View(200, 40), "\n") {
		if strings.Contains(l, "steps") {
			row = l
		}
	}
	if row == "" {
		t.Fatal("no row rendered")
	}
	if got := ansi.StringWidth(row); got != 200 {
		t.Errorf("row width = %d cells, want 200 (the blank age field must be dropped)", got)
	}
	if !strings.HasSuffix(ansi.Strip(row), "     2 steps") {
		t.Errorf("steps cell is not flush against the right edge: %q", ansi.Strip(row))
	}
}

// TestKeyboardHintHidesGroupedView pins the contextual keyboard hint.
//
// agy 1.4.2 shows "ctrl+f Grouped View" over a store whose rows carry project
// names and omits it when no visible row has one (both observed at 200x40).
func TestKeyboardHintHidesGroupedView(t *testing.T) {
	without := ansi.Strip(keyboardHint(true, false))
	if strings.Contains(without, "Grouped View") {
		t.Errorf("hint with no project groups advertises grouped view: %q", without)
	}
	for _, want := range []string{"Navigate", "Page", "enter", "Select", "f2", "f4", "tab", "esc"} {
		if !strings.Contains(without, want) {
			t.Errorf("hint missing %q: %q", want, without)
		}
	}
	with := ansi.Strip(keyboardHint(true, true))
	if !strings.Contains(with, "ctrl+f") || !strings.Contains(with, "Grouped View") {
		t.Errorf("hint with project groups must advertise grouped view: %q", with)
	}
	// The empty/other-tab hint keeps its reduced form.
	empty := ansi.Strip(keyboardHint(false, false))
	for _, bad := range []string{"Navigate", "Page", "f2", "f4", "Grouped View"} {
		if strings.Contains(empty, bad) {
			t.Errorf("reduced hint contains %q: %q", bad, empty)
		}
	}
}

// TestProjectGroups pins the count the hint decision reads.
func TestProjectGroups(t *testing.T) {
	rows := []convstore.ConversationMeta{
		{ID: "a", Project: "openagy"},
		{ID: "b", Project: "openagy"},
		{ID: "c", Project: ""},
	}
	if got := projectGroups(rows); got != 1 {
		t.Errorf("projectGroups = %d, want 1", got)
	}
	rows = append(rows, convstore.ConversationMeta{ID: "d", Project: "opencode"})
	if got := projectGroups(rows); got != 2 {
		t.Errorf("projectGroups = %d, want 2", got)
	}
}

// TestSearchKeepsShiftedCharacters pins the search-field input rule.
//
// Terminals deliver an uppercase letter (and shifted symbols such as "_")
// with ModShift set. The old guard required `key.Mod == 0`, so typing
// "Fixing" into the reference's own search box produced "ixing" in the clone
// while agy 1.4.2 kept every character (verified side by side at 200x40:
// "Fixing", "A_B", "Hi").
func TestSearchKeepsShiftedCharacters(t *testing.T) {
	p := newTestPicker(t)
	p.mode = true
	p.grouped = true

	for _, k := range []tea.Key{
		{Code: 'F', Text: "F", Mod: tea.ModShift},
		{Code: 'i', Text: "i"},
		{Code: 'x', Text: "x"},
		{Code: 'i', Text: "i"},
		{Code: 'n', Text: "n"},
		{Code: 'g', Text: "g"},
	} {
		p.HandleKey(k, 40)
	}
	if p.query != "Fixing" {
		t.Fatalf("query = %q, want %q (shifted characters must be inserted)", p.query, "Fixing")
	}

	// Ctrl-modified keys are commands, not text, and must stay out of the
	// query even though some terminals attach Text to them.
	p.HandleKey(tea.Key{Code: 'u', Mod: tea.ModCtrl}, 40)
	p.HandleKey(tea.Key{Code: 'v', Text: "v", Mod: tea.ModCtrl}, 40)
	if p.query != "" {
		t.Fatalf("query = %q after ctrl+v, want empty", p.query)
	}
}

// TestF2AsksTheHostToRename pins the picker's rename action: the picker
// hands the conversation under the cursor to the host and closes.
func TestF2AsksTheHostToRename(t *testing.T) {
	var renamed convstore.ConversationMeta
	p := New(Seams{
		CurrentConversationID: func() string { return "" },
		ListConversations:     func() ([]convstore.ConversationMeta, error) { return nil, nil },
		ResumeConversation:    func(meta convstore.ConversationMeta) tea.Cmd { return nil },
		RenameConversation: func(meta convstore.ConversationMeta) {
			renamed = meta
		},
	})
	p.mode = true
	p.list = []convstore.ConversationMeta{{ID: "c1", Title: "Original Title", StepCount: 3}}

	p.HandleKey(tea.Key{Code: tea.KeyF2}, 40)
	if renamed.ID != "c1" {
		t.Errorf("the host was asked to rename %q, want c1", renamed.ID)
	}
	if p.Active() {
		t.Error("f2 must close the picker")
	}
}

// TestF4AsksBeforeDeletingTheConversation pins the picker's delete
// action: F4 arms a confirmation and draws it next to the title, and the
// conversation goes away only when the confirmation is answered.
//
// 【一手】 official-docs/cli/commands/resume.md:55-57 — "Press F4. A
// `[Delete? (y/n)]` confirmation prompt appears next to the title. Press
// Enter (or Y) to confirm deletion, or Esc (or N) to cancel."
func TestF4AsksBeforeDeletingTheConversation(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", dir)
	if err := convstore.SaveConversation("test-conv-f4", "/workspace",
		[]convstore.Step{{Idx: 0, Type: convstore.StepUserInput, Status: 1, Text: "hello"}}); err != nil {
		t.Fatalf("SaveConversation: %v", err)
	}

	p := newTestPicker(t)
	if err := p.Open(); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if len(p.list) != 1 {
		t.Fatalf("the picker holds %d conversations, want 1", len(p.list))
	}
	// The tree is the picker's default view: the group row, then the
	// conversation underneath it.
	if p.cursor != 1 {
		t.Fatalf("the cursor is on row %d, want the conversation (1)", p.cursor)
	}

	p.HandleKey(tea.Key{Code: tea.KeyF4}, 40)

	// The delete asks first: the conversation is still there.
	if len(p.list) != 1 {
		t.Errorf("the picker holds %d conversations, want 1 (f4 only arms the confirmation)", len(p.list))
	}
	if p.deleteConfirm != "test-conv-f4" {
		t.Errorf("the confirmation is armed for %q, want %q", p.deleteConfirm, "test-conv-f4")
	}
	view := ansi.Strip(p.View(200, 40))
	if !strings.Contains(view, "[Delete? (y/n)]") {
		t.Errorf("the confirmation prompt is not drawn next to the title: %q", view)
	}

	p.HandleKey(tea.Key{Code: tea.KeyEnter}, 40)
	if len(p.list) != 0 {
		t.Errorf("the picker still holds %d conversations", len(p.list))
	}
	if p.deleteConfirm != "" {
		t.Errorf("the confirmation is still armed for %q", p.deleteConfirm)
	}
}

// TestDeleteConfirmationAnswers pins the three answers the armed
// confirmation takes: Enter and Y delete the conversation, Esc and N
// leave it alone.
//
// 【一手】 official-docs/cli/commands/resume.md:57 — "Press Enter (or Y)
// to confirm deletion, or Esc (or N) to cancel."
func TestDeleteConfirmationAnswers(t *testing.T) {
	cases := []struct {
		name    string
		key     tea.Key
		deleted bool
	}{
		{"enter", tea.Key{Code: tea.KeyEnter}, true},
		{"y", tea.Key{Code: 'y', Text: "y"}, true},
		{"upper y", tea.Key{Code: 'Y', Text: "Y", Mod: tea.ModShift}, true},
		{"esc", tea.Key{Code: tea.KeyEsc}, false},
		{"n", tea.Key{Code: 'n', Text: "n"}, false},
		{"upper n", tea.Key{Code: 'N', Text: "N", Mod: tea.ModShift}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("ANTIGRAVITY_HOME", dir)
			if err := convstore.SaveConversation("test-conv-answer", "/workspace",
				[]convstore.Step{{Idx: 0, Type: convstore.StepUserInput, Status: 1, Text: "hello"}}); err != nil {
				t.Fatalf("SaveConversation: %v", err)
			}

			p := newTestPicker(t)
			if err := p.Open(); err != nil {
				t.Fatalf("Open: %v", err)
			}
			p.HandleKey(tea.Key{Code: tea.KeyF4}, 40)
			if p.deleteConfirm == "" {
				t.Fatal("f4 did not arm the confirmation")
			}
			p.HandleKey(tc.key, 40)

			if p.deleteConfirm != "" {
				t.Errorf("the confirmation is still armed for %q", p.deleteConfirm)
			}
			if tc.deleted && len(p.list) != 0 {
				t.Errorf("the picker still holds %d conversations, want none", len(p.list))
			}
			if !tc.deleted && len(p.list) != 1 {
				t.Errorf("the picker holds %d conversations, want 1", len(p.list))
			}
			if !tc.deleted && strings.Contains(ansi.Strip(p.View(200, 40)), "[Delete? (y/n)]") {
				t.Error("the cancelled confirmation is still drawn")
			}
		})
	}
}

// TestDeleteConfirmationOwnsTheKeys pins that the armed confirmation is a
// stage of its own: the keys it does not answer with do not reach the
// picker, so a typed letter never lands in the search field.
//
// The confirmation is modal in the same way the reference's overlays are:
// it claims every key while it is up.
func TestDeleteConfirmationOwnsTheKeys(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", dir)
	if err := convstore.SaveConversation("test-conv-modal", "/workspace",
		[]convstore.Step{{Idx: 0, Type: convstore.StepUserInput, Status: 1, Text: "hello"}}); err != nil {
		t.Fatalf("SaveConversation: %v", err)
	}

	p := newTestPicker(t)
	if err := p.Open(); err != nil {
		t.Fatalf("Open: %v", err)
	}
	p.HandleKey(tea.Key{Code: tea.KeyF4}, 40)
	p.HandleKey(tea.Key{Code: 'q', Text: "q"}, 40)
	if p.query != "" {
		t.Errorf("the typed letter reached the search field: %q", p.query)
	}
	if p.deleteConfirm == "" {
		t.Error("an unrelated key cancelled the confirmation")
	}
	if len(p.list) != 1 {
		t.Errorf("the picker holds %d conversations, want 1", len(p.list))
	}
}

// TestDeletePromptBytes pins the confirmation prompt's own bytes.
//
// 【一手】 official-docs/cli/commands/resume.md:55 — the prompt is
// "[Delete? (y/n)]"; the binary's own format string is `[Delete? (%s/%s)]`
// — a string of the reference's own table, indexed in
// out/local-macos-1.4.11/strings-index.txt (the full dump is retired).
func TestDeletePromptBytes(t *testing.T) {
	if deletePrompt != "[Delete? (y/n)]" {
		t.Errorf("the prompt reads %q, want %q", deletePrompt, "[Delete? (y/n)]")
	}
}

// TestDeletePromptKeepsTheFlatRowGeometry pins the confirmation prompt's
// place in the flat list's row: it sits next to the title, and the row
// keeps its width so the right-hand cells do not move.
func TestDeletePromptKeepsTheFlatRowGeometry(t *testing.T) {
	p := treeFixture(t)
	p.HandleKey(tea.Key{Code: 'f', Mod: tea.ModCtrl}, 50)
	p.HandleKey(tea.Key{Code: tea.KeyF4}, 50)
	var row string
	for _, l := range strings.Split(ansi.Strip(p.View(200, 50)), "\n") {
		if strings.Contains(l, "[Delete? (y/n)]") {
			row = l
		}
	}
	if row == "" {
		t.Fatal("no row carries the confirmation prompt")
	}
	if !strings.HasPrefix(row, "> Probe One [Delete? (y/n)]") {
		t.Errorf("the prompt does not sit next to the title: %q", row)
	}
	if got := ansi.StringWidth(row); got != 200 {
		t.Errorf("the row is %d cells wide, want 200", got)
	}
}

// searchStore writes a scratch store whose conversations separate their
// title from their first prompt, so the search's three fields can be told
// apart.
func searchStore(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", dir)
	writeTreeStore(t, dir, []treeConv{
		{ID: "11110000-0000-4000-8000-000000000001", Title: "Alpha", Preview: "zebra crossing", Steps: 2},
		{ID: "22220000-0000-4000-8000-000000000002", Title: "Beta", Preview: "yak shave", Steps: 3},
		{ID: "33330000-0000-4000-8000-000000000003", Title: "Gamma", Preview: "first line here\nsecond line here", Steps: 1},
	})
}

// TestSearchMatchesTitlePromptAndID pins the picker's three search
// fields: the title, the first line of the first prompt and the
// conversation id.
//
// 【一手】 official-docs/cli/commands/resume.md:28 — "Start typing to
// instantly filter conversations by their title, the first line of the
// first prompt, or conversation ID."
func TestSearchMatchesTitlePromptAndID(t *testing.T) {
	searchStore(t)
	p := newTestPicker(t)
	if err := p.Open(); err != nil {
		t.Fatalf("Open: %v", err)
	}
	cases := []struct {
		query string
		want  string
	}{
		{"alpha", "11110000-0000-4000-8000-000000000001"},
		{"zebra", "11110000-0000-4000-8000-000000000001"},
		{"2222", "22220000-0000-4000-8000-000000000002"},
		{"first line", "33330000-0000-4000-8000-000000000003"},
	}
	for _, tc := range cases {
		p.query = tc.query
		rows := p.Filtered()
		if len(rows) != 1 {
			t.Fatalf("query %q matched %d rows, want 1", tc.query, len(rows))
		}
		if rows[0].ID != tc.want {
			t.Errorf("query %q matched %q, want %q", tc.query, rows[0].ID, tc.want)
		}
	}
	// Only the *first* line of the first prompt is searched.
	p.query = "second line"
	if rows := p.Filtered(); len(rows) != 0 {
		t.Errorf("a query on the second line matched %d rows, want none", len(rows))
	}
	p.query = "nothing here"
	if rows := p.Filtered(); len(rows) != 0 {
		t.Errorf("a query that matches nothing returned %d rows", len(rows))
	}
}
