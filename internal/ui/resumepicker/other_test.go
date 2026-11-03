// other_test.go — the /resume picker's Other tab.
//
// The Other tab is the Antigravity desktop app's conversations: the rows
// the user imports into the CLI's own store. Every case here was observed
// in the reference binary (agy 1.4.7) in a tmux pane at 200x50; the
// capture lives in out/resume-tree/ref/tree-other-tab.plain.txt.

package resumepicker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/justwasm/pigasus/internal/app/session/convstore"
)

// desktopID is the conversation the scratch desktop store holds.
const desktopID = "eeee0000-0000-4000-8000-00000000000a"

// desktopStore writes a scratch Antigravity desktop store and points both
// the CLI's own store and the desktop store at a scratch home.
//
// The home is $ANTIGRAVITY_HOME: the CLI's store is the home itself and
// the desktop app's store is its "antigravity" child, exactly as the two
// stores sit side by side under ~/.gemini.
func desktopStore(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	t.Setenv("HOME", home)
	root := filepath.Join(home, "antigravity")
	// The reference's own capture shows a conversation nine minutes old
	// with two steps.
	base := time.Now().UTC().Add(-9 * time.Minute)
	writeTreeStore(t, root, []treeConv{{
		ID: desktopID, Title: "Casual Laughter Response",
		Preview: "tell me a joke", Steps: 2, Modified: base,
	}})
	annDir := filepath.Join(root, "annotations")
	if err := os.MkdirAll(annDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `title:"Casual Laughter Response"`
	if err := os.WriteFile(filepath.Join(annDir, desktopID+".pbtxt"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return home
}

// recordingPicker returns a picker whose host records what it was asked to
// resume.
func recordingPicker(t *testing.T) (*Picker, *[]convstore.ConversationMeta) {
	t.Helper()
	var resumed []convstore.ConversationMeta
	p := New(Seams{
		CurrentConversationID: func() string { return "" },
		ListConversations:     func() ([]convstore.ConversationMeta, error) { return convstore.ListConversations() },
		ResumeConversation: func(meta convstore.ConversationMeta) tea.Cmd {
			resumed = append(resumed, meta)
			return nil
		},
		RenameConversation: func(meta convstore.ConversationMeta) {},
	})
	return p, &resumed
}

// TestOtherTabListsTheDesktopConversations pins the Other tab's content:
// the Antigravity desktop app's conversations, and the search row above
// them.
//
// 【一手】 agy 1.4.7 (out/resume-tree/ref/tree-other-tab.plain.txt): the
// Other tab lists "Casual Laughter Response", 2 steps, 9m ago, under a
// "Type to search conversations..." row.
func TestOtherTabListsTheDesktopConversations(t *testing.T) {
	desktopStore(t)
	p, _ := recordingPicker(t)
	if err := p.Open(); err != nil {
		t.Fatalf("Open: %v", err)
	}
	p.HandleKey(tea.Key{Code: tea.KeyTab}, 50)

	if p.tab != 1 {
		t.Fatalf("tab = %d, want the Other tab (1)", p.tab)
	}
	rows := p.Filtered()
	if len(rows) != 1 {
		t.Fatalf("the Other tab holds %d rows, want 1", len(rows))
	}
	if rows[0].Title != "Casual Laughter Response" {
		t.Errorf("the row is titled %q, want %q", rows[0].Title, "Casual Laughter Response")
	}
	if rows[0].StepCount != 2 {
		t.Errorf("the row carries %d steps, want 2", rows[0].StepCount)
	}
	view := ansi.Strip(p.View(200, 50))
	if !strings.Contains(view, "Type to search conversations...") {
		t.Errorf("the Other tab has no search row: %q", view)
	}
	if !strings.Contains(view, "Casual Laughter Response") {
		t.Errorf("the Other tab does not draw the desktop conversation: %q", view)
	}
	if !strings.Contains(view, "2 steps") {
		t.Errorf("the Other tab does not draw the step count: %q", view)
	}
}

// TestOtherTabHint pins the Other tab's keyboard hint.
//
// 【一手】 agy 1.4.7 (out/resume-tree/ref/tree-other-tab.plain.txt):
//
//	Keyboard: ↑/↓ Navigate  enter Select  tab Switch Tab  esc Go back / Clear
//	search
//
// The Other tab offers no page, no view switch, no rename and no delete.
func TestOtherTabHint(t *testing.T) {
	desktopStore(t)
	p, _ := recordingPicker(t)
	if err := p.Open(); err != nil {
		t.Fatalf("Open: %v", err)
	}
	p.HandleKey(tea.Key{Code: tea.KeyTab}, 50)

	hint := ansi.Strip(p.View(200, 50))
	for _, want := range []string{"↑/↓ Navigate", "enter Select", "tab Switch Tab", "esc Go back / Clear search"} {
		if !strings.Contains(hint, want) {
			t.Errorf("the hint is missing %q: %q", want, hint)
		}
	}
	for _, bad := range []string{"Page", "ctrl+f", "f2", "f4"} {
		if strings.Contains(hint, bad) {
			t.Errorf("the Other tab's hint offers %q: %q", bad, hint)
		}
	}
}

// TestTabSwitchesBackToTheCLI pins that the tab bar cycles: the Other tab
// holds the desktop conversations, the CLI tab holds the store's own.
func TestTabSwitchesBackToTheCLI(t *testing.T) {
	desktopStore(t)
	p, _ := recordingPicker(t)
	if err := p.Open(); err != nil {
		t.Fatalf("Open: %v", err)
	}
	p.HandleKey(tea.Key{Code: tea.KeyTab}, 50)
	if p.tab != 1 {
		t.Fatalf("tab = %d, want 1", p.tab)
	}
	p.HandleKey(tea.Key{Code: tea.KeyTab}, 50)
	if p.tab != 0 {
		t.Fatalf("tab = %d, want the CLI tab (0)", p.tab)
	}
	if rows := p.Filtered(); len(rows) != 0 {
		t.Errorf("the CLI tab holds %d rows, want none (the scratch store is empty)", len(rows))
	}
}

// TestOtherTabSearchMatchesThePreview pins that the Other tab's search
// reaches the first prompt, the way the CLI tab's does.
//
// 【一手】 official-docs/cli/commands/resume.md:28 — the search filters on
// "the first line of the first prompt".
func TestOtherTabSearchMatchesThePreview(t *testing.T) {
	desktopStore(t)
	p, _ := recordingPicker(t)
	if err := p.Open(); err != nil {
		t.Fatalf("Open: %v", err)
	}
	p.HandleKey(tea.Key{Code: tea.KeyTab}, 50)

	p.HandleKey(tea.Key{Code: 'j', Text: "j"}, 50)
	rows := p.Filtered()
	if len(rows) != 1 || rows[0].ID != desktopID {
		t.Fatalf("the query matched %d rows, want the desktop conversation: %+v", len(rows), rows)
	}
	p.query = "nothing here"
	if rows := p.Filtered(); len(rows) != 0 {
		t.Errorf("a query that matches nothing returned %d rows", len(rows))
	}
}

// TestOtherTabEnterAsksBeforeImporting pins the import's confirmation: the
// prompt is drawn next to the title, and nothing is imported until the
// confirmation is answered.
//
// 【一手】 official-docs/cli/commands/resume.md:69 — "Press Enter. A
// confirmation prompt `[Import this conversation? (y/n)]` appears."
func TestOtherTabEnterAsksBeforeImporting(t *testing.T) {
	home := desktopStore(t)
	p, resumed := recordingPicker(t)
	if err := p.Open(); err != nil {
		t.Fatalf("Open: %v", err)
	}
	p.HandleKey(tea.Key{Code: tea.KeyTab}, 50)
	p.HandleKey(tea.Key{Code: tea.KeyEnter}, 50)

	if p.importConfirm != desktopID {
		t.Fatalf("the confirmation is armed for %q, want %q", p.importConfirm, desktopID)
	}
	view := ansi.Strip(p.View(200, 50))
	if !strings.Contains(view, "[Import this conversation? (y/n)]") {
		t.Errorf("the confirmation prompt is not drawn next to the title: %q", view)
	}
	if len(*resumed) != 0 {
		t.Errorf("the host was asked to resume %d conversations before the import", len(*resumed))
	}
	if _, err := os.Stat(filepath.Join(home, "conversations", desktopID+".db")); err == nil {
		t.Error("the conversation was imported before the confirmation was answered")
	}
}

// TestImportPromptBytes pins the import prompt's own bytes.
//
// 【一手】 official-docs/cli/commands/resume.md:69 — the prompt is
// "[Import this conversation? (y/n)]"; the binary's own format string is
// `[Import this conversation? (%s/%s)]` — a string of the reference's own
// table, indexed in out/local-macos-1.4.11/strings-index.txt (the full
// dump is retired).
func TestImportPromptBytes(t *testing.T) {
	if importPrompt != "[Import this conversation? (y/n)]" {
		t.Errorf("the prompt reads %q, want %q", importPrompt, "[Import this conversation? (y/n)]")
	}
}

// TestImportPromptKeepsTheFlatRowGeometry pins the import prompt's place
// in the Other tab's row: it sits next to the title, and the row keeps its
// width so the right-hand cells do not move.
func TestImportPromptKeepsTheFlatRowGeometry(t *testing.T) {
	desktopStore(t)
	p, _ := recordingPicker(t)
	if err := p.Open(); err != nil {
		t.Fatalf("Open: %v", err)
	}
	p.HandleKey(tea.Key{Code: tea.KeyTab}, 50)
	p.HandleKey(tea.Key{Code: tea.KeyEnter}, 50)

	var row string
	for _, l := range strings.Split(ansi.Strip(p.View(200, 50)), "\n") {
		if strings.Contains(l, "[Import this conversation? (y/n)]") {
			row = l
		}
	}
	if row == "" {
		t.Fatal("no row carries the confirmation prompt")
	}
	if !strings.HasPrefix(row, "> Casual Laughter Response [Import this conversation? (y/n)]") {
		t.Errorf("the prompt does not sit next to the title: %q", row)
	}
	if got := ansi.StringWidth(row); got != 200 {
		t.Errorf("the row is %d cells wide, want 200", got)
	}
}

// TestImportConfirmationAnswers pins the three answers the armed import
// confirmation takes: Enter and Y import the conversation and resume it,
// Esc and N leave it alone.
//
// 【一手】 official-docs/cli/commands/resume.md:69 — "Press Enter (or Y) to
// confirm."
func TestImportConfirmationAnswers(t *testing.T) {
	cases := []struct {
		name     string
		key      tea.Key
		imported bool
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
			home := desktopStore(t)
			p, resumed := recordingPicker(t)
			if err := p.Open(); err != nil {
				t.Fatalf("Open: %v", err)
			}
			p.HandleKey(tea.Key{Code: tea.KeyTab}, 50)
			p.HandleKey(tea.Key{Code: tea.KeyEnter}, 50)
			if p.importConfirm == "" {
				t.Fatal("enter did not arm the confirmation")
			}
			p.HandleKey(tc.key, 50)

			if p.importConfirm != "" {
				t.Errorf("the confirmation is still armed for %q", p.importConfirm)
			}
			imported := filepath.Join(home, "conversations", desktopID+".db")
			_, err := os.Stat(imported)
			if tc.imported && err != nil {
				t.Fatalf("the conversation was not imported: %v", err)
			}
			if !tc.imported && err == nil {
				t.Error("the conversation was imported by a cancelled confirmation")
			}
			if tc.imported {
				if len(*resumed) != 1 {
					t.Fatalf("the host was asked to resume %d conversations, want 1", len(*resumed))
				}
				if (*resumed)[0].ID != desktopID {
					t.Errorf("the host was asked to resume %q, want %q", (*resumed)[0].ID, desktopID)
				}
				if (*resumed)[0].Path != imported {
					t.Errorf("the host was asked to resume %q, want the imported file %q",
						(*resumed)[0].Path, imported)
				}
				if p.Active() {
					t.Error("the picker must close when the import worked")
				}
				return
			}
			if len(*resumed) != 0 {
				t.Errorf("the host was asked to resume %d conversations", len(*resumed))
			}
			if !p.Active() {
				t.Error("a cancelled import must leave the picker open")
			}
			if strings.Contains(ansi.Strip(p.View(200, 50)), "[Import this conversation? (y/n)]") {
				t.Error("the cancelled confirmation is still drawn")
			}
		})
	}
}

// TestImportConfirmationOwnsTheKeys pins that the armed confirmation is a
// stage of its own: the keys it does not answer with do not reach the
// picker, so a typed letter never lands in the search field.
func TestImportConfirmationOwnsTheKeys(t *testing.T) {
	desktopStore(t)
	p, _ := recordingPicker(t)
	if err := p.Open(); err != nil {
		t.Fatalf("Open: %v", err)
	}
	p.HandleKey(tea.Key{Code: tea.KeyTab}, 50)
	p.HandleKey(tea.Key{Code: tea.KeyEnter}, 50)
	p.HandleKey(tea.Key{Code: 'q', Text: "q"}, 50)
	if p.query != "" {
		t.Errorf("the typed letter reached the search field: %q", p.query)
	}
	if p.importConfirm == "" {
		t.Error("an unrelated key cancelled the confirmation")
	}
}

// TestImportedConversationIsLoadable pins that the imported conversation is
// the desktop app's own conversation, step for step.
func TestImportedConversationIsLoadable(t *testing.T) {
	home := desktopStore(t)
	p, resumed := recordingPicker(t)
	if err := p.Open(); err != nil {
		t.Fatalf("Open: %v", err)
	}
	p.HandleKey(tea.Key{Code: tea.KeyTab}, 50)
	p.HandleKey(tea.Key{Code: tea.KeyEnter}, 50)
	p.HandleKey(tea.Key{Code: tea.KeyEnter}, 50)

	if len(*resumed) != 1 {
		t.Fatalf("the host was asked to resume %d conversations, want 1", len(*resumed))
	}
	c, err := convstore.LoadConversation((*resumed)[0].Path)
	if err != nil {
		t.Fatalf("LoadConversation: %v", err)
	}
	if c == nil || len(c.Steps) != 1 {
		t.Fatalf("the imported conversation holds %d steps, want 1", len(c.Steps))
	}
	if c.Steps[0].Text != "tell me a joke" {
		t.Errorf("the imported step reads %q, want %q", c.Steps[0].Text, "tell me a joke")
	}
	// The CLI's own store now holds the conversation: the /resume picker
	// lists it on the CLI tab.
	list, err := convstore.ListConversations()
	if err != nil {
		t.Fatalf("ListConversations: %v", err)
	}
	if len(list) != 1 || list[0].ID != desktopID {
		t.Fatalf("the CLI's store holds %d conversations, want the imported one: %+v", len(list), list)
	}
	if list[0].Title != "Casual Laughter Response" {
		t.Errorf("the imported conversation is titled %q, want %q",
			list[0].Title, "Casual Laughter Response")
	}
	if _, err := os.Stat(filepath.Join(home, "annotations", desktopID+".pbtxt")); err != nil {
		t.Errorf("the annotation was not copied: %v", err)
	}
}

// TestOtherTabSkipsWhatTheCLIStoreHolds pins that a conversation the
// CLI's own store already holds is not offered for import again.
func TestOtherTabSkipsWhatTheCLIStoreHolds(t *testing.T) {
	desktopStore(t)
	if err := convstore.SaveConversation(desktopID, "", []convstore.Step{
		{Idx: 0, Type: convstore.StepUserInput, Status: 1, Text: "already here"},
	}); err != nil {
		t.Fatalf("SaveConversation: %v", err)
	}
	p, _ := recordingPicker(t)
	if err := p.Open(); err != nil {
		t.Fatalf("Open: %v", err)
	}
	p.HandleKey(tea.Key{Code: tea.KeyTab}, 50)
	if rows := p.Filtered(); len(rows) != 0 {
		t.Errorf("the Other tab offers %d rows, want none: %+v", len(rows), rows)
	}
}
