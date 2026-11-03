// tree_test.go — the /resume picker's workspace tree.
//
// Every case here was observed live in the reference binary (agy 1.4.7) in a
// tmux pane at 200x50; the captures live in out/resume-tree/ref/. The
// comment on each test records the reference behaviour it pins.

package resumepicker

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/justwasm/pigasus/internal/app/session/convstore"
	_ "github.com/ncruces/go-sqlite3/driver"
)

// treeConv is one conversation of a scratch store.
type treeConv struct {
	ID        string
	Title     string
	Preview   string
	Steps     int
	Modified  time.Time
	Workspace string
}

// writeTreeStore writes a scratch store whose conversations carry the
// workspaces the picker groups by.
//
// The workspace of a conversation lives in the reference's conversation
// index, so the test writes the same rows the reference's index holds.
func writeTreeStore(t *testing.T, dir string, convs []treeConv) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "conversations"), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite3", filepath.Join(dir, "conversation_summaries.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE conversation_summaries (
		conversation_id TEXT PRIMARY KEY, title TEXT, preview TEXT,
		step_count INTEGER, last_modified_time TEXT, workspace_uris TEXT)`); err != nil {
		t.Fatal(err)
	}
	for _, c := range convs {
		steps := []convstore.Step{
			{Idx: 0, Type: convstore.StepUserInput, Status: 3, Text: c.Preview},
		}
		if err := convstore.WriteConversation(
			filepath.Join(dir, "conversations", c.ID+".db"), "traj-"+c.ID, steps); err != nil {
			t.Fatal(err)
		}
		uris := "[]"
		if c.Workspace != "" {
			uris = `["` + c.Workspace + `"]`
		}
		if _, err := db.Exec(`INSERT INTO conversation_summaries
			(conversation_id, title, preview, step_count, last_modified_time, workspace_uris)
			VALUES (?,?,?,?,?,?)`,
			c.ID, c.Title, c.Preview, c.Steps,
			c.Modified.Format("2006-01-02 15:04:05"), uris); err != nil {
			t.Fatal(err)
		}
	}
}

// treeStore writes the fixture's scratch store and points the store
// resolver at it.
func treeStore(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", dir)
	// The base sits two hours before now, so the age cell reads "2h ago"
	// for the whole hour; a fixed calendar date would turn the age
	// assertion into a time bomb.
	base := time.Now().UTC().Add(-2 * time.Hour)
	writeTreeStore(t, dir, []treeConv{
		{ID: "aaaa0000-0000-4000-8000-000000000001", Title: "Probe One", Preview: "probe one",
			Steps: 17, Modified: base, Workspace: "file:///tmp/agyprobe"},
		{ID: "aaaa0000-0000-4000-8000-000000000002", Title: "Probe Two", Preview: "probe two",
			Steps: 5, Modified: base.Add(-time.Hour), Workspace: "file:///tmp/agyprobe"},
		{ID: "bbbb0000-0000-4000-8000-000000000003", Title: "Repo Work", Preview: "repo work",
			Steps: 61, Modified: base.Add(-2 * time.Hour), Workspace: "file:///Users/gear/GitHub/openagy"},
		{ID: "cccc0000-0000-4000-8000-000000000004", Title: "No Workspace", Preview: "no workspace",
			Steps: 2, Modified: base.Add(-3 * time.Hour)},
	})
	return dir
}

// treeFixture is a scratch store with three workspace groups.
func treeFixture(t *testing.T) *Picker {
	t.Helper()
	treeStore(t)
	p := newTestPicker(t)
	if err := p.Open(); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !p.Active() {
		t.Fatal("/resume should open the picker")
	}
	return p
}

// TestTreeGroupsByWorkspace pins the tree the reference draws: one
// row per workspace group, labelled by the workspace, and the group of the
// conversations without a workspace last.
//
// 【一手】 agy 1.4.7 (out/resume-tree/ref/tree.txt): the live picker lists
// "gear", "dir", "openagy", "tmp/agyprobe", "tmp/agyprobe" and
// "[No Workspace]".
func TestTreeGroupsByWorkspace(t *testing.T) {
	p := treeFixture(t)
	if len(p.groups) != 3 {
		t.Fatalf("got %d groups, want 3", len(p.groups))
	}
	want := []struct {
		label string
		count int
	}{
		{"agyprobe", 2},
		{"openagy", 1},
		{"[No Workspace]", 1},
	}
	for i, w := range want {
		g := p.groups[i]
		if g.Label != w.label {
			t.Errorf("group %d is labelled %q, want %q", i, g.Label, w.label)
		}
		if len(g.Items) != w.count {
			t.Errorf("group %q holds %d conversations, want %d", g.Label, len(g.Items), w.count)
		}
	}
}

// TestTreeRows pins the rows the tree draws: the group row first, its
// conversations underneath when it is expanded.
//
// 【一手】 agy 1.4.7: the tree opens with the group of the current
// conversation expanded and the cursor on its first conversation.
func TestTreeRows(t *testing.T) {
	p := treeFixture(t)
	rows := p.treeRows()
	// Three groups, the first one expanded with its two conversations.
	if len(rows) != 5 {
		t.Fatalf("got %d rows, want 5", len(rows))
	}
	if rows[0].item != nil || rows[0].group == nil {
		t.Fatalf("row 0 is not a group row: %+v", rows[0])
	}
	if rows[1].item == nil {
		t.Fatal("row 1 is not a conversation row")
	}
	if p.cursor != 1 {
		t.Errorf("the cursor opens on row %d, want the first conversation (1)", p.cursor)
	}
}

// TestTreeKeepsFiveChildrenPerGroup pins the reference's cap: a group
// shows at most five of its conversations, whatever the group holds.
//
// 【一手】 agy 1.4.7: expanding the 486-conversation "[No Workspace]" group
// grows the tree from 12 to 15 rows — five of its conversations, not all
// 486.
func TestTreeKeepsFiveChildrenPerGroup(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", dir)
	base := time.Date(2026, 10, 26, 15, 0, 0, 0, time.UTC)
	var convs []treeConv
	for i := 0; i < 8; i++ {
		convs = append(convs, treeConv{
			ID:    "dddd0000-0000-4000-8000-00000000000" + string(rune('a'+i)),
			Title: "Many", Preview: "many", Steps: 2,
			Modified: base.Add(-time.Duration(i) * time.Minute),
		})
	}
	writeTreeStore(t, dir, convs)
	p := newTestPicker(t)
	if err := p.Open(); err != nil {
		t.Fatalf("Open: %v", err)
	}
	rows := p.treeRows()
	// One group row plus five conversations.
	if len(rows) != 6 {
		t.Fatalf("got %d rows, want 6 (the group and five conversations)", len(rows))
	}
}

// TestTreeRowGeometry pins the cell layout of the tree's rows.
//
// 【一手】 agy 1.4.7 at 200 columns: both rows are 200 cells wide. The
// group row's count cell ends at column 185 and its age cell at column 199;
// the conversation row's step count ends at column 185 and its age at
// column 199 as well.
func TestTreeRowGeometry(t *testing.T) {
	p := treeFixture(t)
	var groupRow, childRow string
	for _, l := range strings.Split(p.View(200, 50), "\n") {
		switch {
		case groupRow == "" && strings.Contains(l, "agyprobe"):
			groupRow = l
		case childRow == "" && strings.Contains(l, "steps"):
			childRow = l
		}
	}
	if groupRow == "" || childRow == "" {
		t.Fatalf("missing rows: group=%q child=%q", groupRow, childRow)
	}
	if got := ansi.StringWidth(groupRow); got != 200 {
		t.Errorf("the group row is %d cells wide, want 200", got)
	}
	if got := ansi.StringWidth(childRow); got != 200 {
		t.Errorf("the conversation row is %d cells wide, want 200", got)
	}
	plain := ansi.Strip(groupRow)
	if i := runeIndex(plain, "[2 convs]"); i < 0 || i+len("[2 convs]") != 186 {
		t.Errorf("the count cell ends at %d, want 186: %q", i+len("[2 convs]"), plain)
	}
	if !strings.HasSuffix(strings.TrimRight(plain, " "), "2h ago") {
		t.Errorf("the age cell does not end the group row: %q", plain)
	}
	plainChild := ansi.Strip(childRow)
	if i := runeIndex(plainChild, "17 steps"); i < 0 || i+len("17 steps") != 186 {
		t.Errorf("the step count ends at %d, want 186: %q", i+len("17 steps"), plainChild)
	}
}

// runeIndex finds a needle in a string by cell, not by byte — the
// reference's rows carry the box-drawing characters, which take three bytes
// each.
func runeIndex(s, needle string) int {
	r := []rune(s)
	n := []rune(needle)
	for i := 0; i+len(n) <= len(r); i++ {
		if string(r[i:i+len(n)]) == needle {
			return i
		}
	}
	return -1
}

// TestTreeEnterTogglesGroup pins the reference's Enter behaviour on a
// group row: it expands or collapses the group and keeps the cursor on the
// group row.
//
// 【一手】 agy 1.4.7: Enter on a collapsed group row expands it and the
// cursor stays where it was.
func TestTreeEnterTogglesGroup(t *testing.T) {
	p := treeFixture(t)
	p.cursor = 3 // the "openagy" group row
	before := len(p.treeRows())
	p.HandleKey(tea.Key{Code: tea.KeyEnter}, 50)
	if p.cursor != 3 {
		t.Errorf("the cursor moved to %d, want to stay on the group row (3)", p.cursor)
	}
	if got := len(p.treeRows()); got != before+1 {
		t.Errorf("the group holds %d rows now, want %d", got, before+1)
	}
	p.HandleKey(tea.Key{Code: tea.KeyEnter}, 50)
	if got := len(p.treeRows()); got != before {
		t.Errorf("the group holds %d rows now, want %d", got, before)
	}
}

// TestTreeCtrlETogglesCursorGroup pins the reference's ctrl+e
// behaviour: it expands or collapses the group under the cursor and leaves
// the other groups alone.
//
// 【一手】 agy 1.4.7: with the cursor on a collapsed group and another group
// expanded, ctrl+e expands the cursor's group and keeps the other one
// expanded.
func TestTreeCtrlETogglesCursorGroup(t *testing.T) {
	p := treeFixture(t)
	// The cursor sits on the first group's conversation; the group is
	// expanded.
	if hint := ansi.Strip(p.treeHint(p.treeRows())); !strings.Contains(hint, "ctrl+e Collapse") {
		t.Errorf("the hint does not offer to collapse the expanded group: %q", hint)
	}
	p.HandleKey(tea.Key{Code: 'e', Mod: tea.ModCtrl}, 50)
	if p.expanded[p.groups[0].Key] {
		t.Error("ctrl+e did not collapse the group under the cursor")
	}
	if hint := ansi.Strip(p.treeHint(p.treeRows())); !strings.Contains(hint, "ctrl+e Expand") {
		t.Errorf("the hint does not offer to expand the collapsed group: %q", hint)
	}
	p.HandleKey(tea.Key{Code: 'e', Mod: tea.ModCtrl}, 50)
	if !p.expanded[p.groups[0].Key] {
		t.Error("ctrl+e did not expand the group under the cursor")
	}
}

func TestTreeExpandsNoWorkspaceGroup(t *testing.T) {
	p := treeFixture(t)
	rows := p.treeRows()
	noWsIndex := -1
	for i, r := range rows {
		if r.group != nil && r.group.Key == "" && r.item == nil {
			noWsIndex = i
			break
		}
	}
	if noWsIndex < 0 {
		t.Fatalf("could not find [No Workspace] group row in %v", rows)
	}

	// Move cursor to [No Workspace] group row
	p.cursor = noWsIndex

	// Verify hint offers Expand
	if hint := ansi.Strip(p.treeHint(p.treeRows())); !strings.Contains(hint, "ctrl+e Expand") {
		t.Errorf("expected hint to offer 'ctrl+e Expand', got: %q", hint)
	}

	// Press Enter to expand
	p.HandleKey(tea.Key{Code: tea.KeyEnter}, 50)

	// Now p.expanded[""] must be true
	if !p.expanded[""] {
		t.Fatalf("expected [No Workspace] group (key '') to be expanded")
	}

	// Now the tree must have an additional row for the child item
	newRows := p.treeRows()
	if len(newRows) != len(rows)+1 {
		t.Fatalf("expected len(newRows) == %d, got %d", len(rows)+1, len(newRows))
	}

	// Child row should be right below the group row
	childRow := newRows[noWsIndex+1]
	if childRow.item == nil || childRow.item.Title != "No Workspace" {
		t.Fatalf("expected child row to be 'No Workspace', got %+v", childRow)
	}

	// Press Enter on group row again to collapse
	p.cursor = noWsIndex
	p.HandleKey(tea.Key{Code: tea.KeyEnter}, 50)
	if p.expanded[""] {
		t.Fatalf("expected [No Workspace] group to be collapsed after second Enter")
	}
}


// TestTreeF2F4OnGroupRowDoNothing pins that the rename and delete
// keys do nothing on a group row.
//
// 【一手】 agy 1.4.7: f2 and f4 on a group row leave the picker alone —
// the group holds no conversation to rename or delete.
func TestTreeF2F4OnGroupRowDoNothing(t *testing.T) {
	treeStore(t)
	var renamed []string
	p := New(Seams{
		CurrentConversationID: func() string { return "" },
		ListConversations: func() ([]convstore.ConversationMeta, error) {
			return convstore.ListConversations()
		},
		ResumeConversation: func(meta convstore.ConversationMeta) tea.Cmd { return nil },
		RenameConversation: func(meta convstore.ConversationMeta) {
			renamed = append(renamed, meta.ID)
		},
	})
	if err := p.Open(); err != nil {
		t.Fatalf("Open: %v", err)
	}
	// The cursor sits on the first group's row — a group row, not a
	// conversation.
	p.cursor = 0
	rows := p.treeRows()
	if rows[0].item != nil {
		t.Fatalf("row 0 is not a group row: %+v", rows[0])
	}

	p.HandleKey(tea.Key{Code: tea.KeyF2}, 50)
	p.HandleKey(tea.Key{Code: tea.KeyF4}, 50)

	if len(renamed) != 0 {
		t.Errorf("the host was asked to rename %v on a group row", renamed)
	}
	if !p.Active() {
		t.Error("f2/f4 on a group row must leave the picker open")
	}
	// Nothing was deleted: the store still holds its four conversations.
	list, err := convstore.ListConversations()
	if err != nil {
		t.Fatalf("ListConversations: %v", err)
	}
	if len(list) != 4 {
		t.Errorf("the store holds %d conversations, want 4", len(list))
	}
}

// TestTreeF4ArmsTheDeleteConfirmation pins the tree's delete key: F4
// arms the confirmation and draws it next to the conversation's title,
// and nothing is deleted until the confirmation is answered.
//
// 【一手】 official-docs/cli/commands/resume.md:55 — "Press F4. A
// `[Delete? (y/n)]` confirmation prompt appears next to the title."
func TestTreeF4ArmsTheDeleteConfirmation(t *testing.T) {
	p := treeFixture(t)
	rows := p.treeRows()
	if rows[p.cursor].item == nil {
		t.Fatalf("the cursor is not on a conversation row: %+v", rows[p.cursor])
	}
	id := rows[p.cursor].item.ID

	p.HandleKey(tea.Key{Code: tea.KeyF4}, 50)
	if p.deleteConfirm != id {
		t.Errorf("the confirmation is armed for %q, want %q", p.deleteConfirm, id)
	}
	view := ansi.Strip(p.View(200, 50))
	if !strings.Contains(view, "[Delete? (y/n)]") {
		t.Errorf("the confirmation prompt is not drawn next to the title: %q", view)
	}
	// Nothing was deleted: the store still holds its four conversations.
	list, err := convstore.ListConversations()
	if err != nil {
		t.Fatalf("ListConversations: %v", err)
	}
	if len(list) != 4 {
		t.Errorf("the store holds %d conversations, want 4", len(list))
	}

	// Esc cancels the confirmation and the prompt goes away.
	p.HandleKey(tea.Key{Code: tea.KeyEsc}, 50)
	if p.deleteConfirm != "" {
		t.Errorf("the confirmation is still armed for %q", p.deleteConfirm)
	}
	if strings.Contains(ansi.Strip(p.View(200, 50)), "[Delete? (y/n)]") {
		t.Error("the cancelled confirmation is still drawn")
	}
}

// TestTreeDeletePromptKeepsTheRowGeometry pins the confirmation prompt's
// place in the tree's conversation row: it sits next to the title, and the
// row keeps its width so the two right-hand cells do not move.
//
// 【一手】 agy 1.4.7 at 200 columns: both rows are 200 cells wide.
func TestTreeDeletePromptKeepsTheRowGeometry(t *testing.T) {
	p := treeFixture(t)
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
	if !strings.HasPrefix(row, ">   Probe One [Delete? (y/n)]") {
		t.Errorf("the prompt does not sit next to the title: %q", row)
	}
	if got := ansi.StringWidth(row); got != 200 {
		t.Errorf("the row is %d cells wide, want 200", got)
	}
}

// TestTreeHintAndViewSwitch pins the tree's keyboard hint and the
// ctrl+f switch between the tree and the flat list.
//
// 【一手】 agy 1.4.7: the tree's hint reads "ctrl+f Conversation View" and
// "ctrl+e Collapse"; the flat list's hint reads "ctrl+f Grouped View".
func TestTreeHintAndViewSwitch(t *testing.T) {
	p := treeFixture(t)
	hint := ansi.Strip(p.View(200, 50))
	if !strings.Contains(hint, "ctrl+f Conversation View") {
		t.Errorf("the tree does not advertise the flat view: %q", hint)
	}
	if !strings.Contains(hint, "ctrl+e") {
		t.Errorf("the tree does not advertise ctrl+e: %q", hint)
	}
	p.HandleKey(tea.Key{Code: 'f', Mod: tea.ModCtrl}, 50)
	if p.grouped {
		t.Fatal("ctrl+f did not leave the tree")
	}
	hint = ansi.Strip(p.View(200, 50))
	if !strings.Contains(hint, "ctrl+f Grouped View") {
		t.Errorf("the flat list does not advertise the tree: %q", hint)
	}
	if strings.Contains(hint, "ctrl+e") {
		t.Errorf("the flat list still advertises ctrl+e: %q", hint)
	}
	p.HandleKey(tea.Key{Code: 'f', Mod: tea.ModCtrl}, 50)
	if !p.grouped {
		t.Fatal("ctrl+f did not come back to the tree")
	}
}

// TestTreeSearchSwitchesToFlatList pins that a search draws the flat
// list while the grouped flag stays on.
//
// 【一手】 agy 1.4.7: typing into the search field leaves the tree and shows
// the filtered conversation list; the hint still offers "ctrl+f Conversation
// View" and no ctrl+e.
func TestTreeSearchSwitchesToFlatList(t *testing.T) {
	p := treeFixture(t)
	p.HandleKey(tea.Key{Code: 'P', Text: "P"}, 50)
	if p.query != "P" {
		t.Fatalf("query = %q, want %q", p.query, "P")
	}
	body := ansi.Strip(p.View(200, 50))
	if strings.Contains(body, "conv]") {
		t.Errorf("the search still draws the tree: %q", body)
	}
	if !strings.Contains(body, "ctrl+f Conversation View") {
		t.Errorf("the search does not advertise the flat view: %q", body)
	}
	if strings.Contains(body, "ctrl+e") {
		t.Errorf("the search still advertises ctrl+e: %q", body)
	}
}
