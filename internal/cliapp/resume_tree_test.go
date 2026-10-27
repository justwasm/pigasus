// resume_tree_test.go — cycle 336: the /resume picker's workspace tree.
//
// Every case here was observed live in the reference binary (agy 1.4.7) in a
// tmux pane at 200x50; the captures live in out/resume-tree/ref/. The
// comment on each test records the reference behaviour it pins.
package cliapp

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/justwasm/pigasus/internal/convstore"
	_ "github.com/ncruces/go-sqlite3/driver"
)

// resumeTreeConv is one conversation of a scratch store.
type resumeTreeConv struct {
	ID        string
	Title     string
	Preview   string
	Steps     int
	Modified  time.Time
	Workspace string
}

// writeResumeTreeStore writes a scratch store whose conversations carry the
// workspaces the picker groups by.
//
// The workspace of a conversation lives in the reference's conversation
// index, so the test writes the same rows the reference's index holds.
func writeResumeTreeStore(t *testing.T, dir string, convs []resumeTreeConv) {
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

// resumeTreeFixture is a scratch store with three workspace groups.
func resumeTreeFixture(t *testing.T) *runMode {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", dir)
	// The base sits two hours before now, so the age cell reads "2h ago"
	// for the whole hour; a fixed calendar date would turn the age
	// assertion into a time bomb.
	base := time.Now().UTC().Add(-2 * time.Hour)
	writeResumeTreeStore(t, dir, []resumeTreeConv{
		{ID: "aaaa0000-0000-4000-8000-000000000001", Title: "Probe One", Preview: "probe one",
			Steps: 17, Modified: base, Workspace: "file:///tmp/agyprobe"},
		{ID: "aaaa0000-0000-4000-8000-000000000002", Title: "Probe Two", Preview: "probe two",
			Steps: 5, Modified: base.Add(-time.Hour), Workspace: "file:///tmp/agyprobe"},
		{ID: "bbbb0000-0000-4000-8000-000000000003", Title: "Repo Work", Preview: "repo work",
			Steps: 61, Modified: base.Add(-2 * time.Hour), Workspace: "file:///Users/gear/GitHub/openagy"},
		{ID: "cccc0000-0000-4000-8000-000000000004", Title: "No Workspace", Preview: "no workspace",
			Steps: 2, Modified: base.Add(-3 * time.Hour)},
	})
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.width, m.height = 200, 50
	if handled, _ := m.handleSlashCommand("/resume"); !handled {
		t.Fatal("/resume should be handled")
	}
	if !m.resumeMode {
		t.Fatal("/resume should open the picker")
	}
	return m
}

// TestResumeTreeGroupsByWorkspace pins the tree the reference draws: one
// row per workspace group, labelled by the workspace, and the group of the
// conversations without a workspace last.
//
// 【一手】 agy 1.4.7 (out/resume-tree/ref/tree.txt): the live picker lists
// "gear", "dir", "openagy", "tmp/agyprobe", "tmp/agyprobe" and
// "[No Workspace]".
func TestResumeTreeGroupsByWorkspace(t *testing.T) {
	m := resumeTreeFixture(t)
	if len(m.resumeGroups) != 3 {
		t.Fatalf("got %d groups, want 3", len(m.resumeGroups))
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
		g := m.resumeGroups[i]
		if g.Label != w.label {
			t.Errorf("group %d is labelled %q, want %q", i, g.Label, w.label)
		}
		if len(g.Items) != w.count {
			t.Errorf("group %q holds %d conversations, want %d", g.Label, len(g.Items), w.count)
		}
	}
}

// TestResumeTreeRows pins the rows the tree draws: the group row first, its
// conversations underneath when it is expanded.
//
// 【一手】 agy 1.4.7: the tree opens with the group of the current
// conversation expanded and the cursor on its first conversation.
func TestResumeTreeRows(t *testing.T) {
	m := resumeTreeFixture(t)
	rows := m.resumeTreeRows()
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
	if m.resumeCursor != 1 {
		t.Errorf("the cursor opens on row %d, want the first conversation (1)", m.resumeCursor)
	}
}

// TestResumeTreeKeepsFiveChildrenPerGroup pins the reference's cap: a group
// shows at most five of its conversations, whatever the group holds.
//
// 【一手】 agy 1.4.7: expanding the 486-conversation "[No Workspace]" group
// grows the tree from 12 to 15 rows — five of its conversations, not all
// 486.
func TestResumeTreeKeepsFiveChildrenPerGroup(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", dir)
	base := time.Date(2026, 10, 26, 15, 0, 0, 0, time.UTC)
	var convs []resumeTreeConv
	for i := 0; i < 8; i++ {
		convs = append(convs, resumeTreeConv{
			ID:    "dddd0000-0000-4000-8000-00000000000" + string(rune('a'+i)),
			Title: "Many", Preview: "many", Steps: 2,
			Modified: base.Add(-time.Duration(i) * time.Minute),
		})
	}
	writeResumeTreeStore(t, dir, convs)
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.width, m.height = 200, 50
	if handled, _ := m.handleSlashCommand("/resume"); !handled {
		t.Fatal("/resume should be handled")
	}
	rows := m.resumeTreeRows()
	// One group row plus five conversations.
	if len(rows) != 6 {
		t.Fatalf("got %d rows, want 6 (the group and five conversations)", len(rows))
	}
}

// TestResumeTreeRowGeometry pins the cell layout of the tree's rows.
//
// 【一手】 agy 1.4.7 at 200 columns: both rows are 200 cells wide. The
// group row's count cell ends at column 185 and its age cell at column 199;
// the conversation row's step count ends at column 185 and its age at
// column 199 as well.
func TestResumeTreeRowGeometry(t *testing.T) {
	m := resumeTreeFixture(t)
	var groupRow, childRow string
	for _, l := range strings.Split(m.resumeView(), "\n") {
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
	if i := resumeRuneIndex(plain, "[2 convs]"); i < 0 || i+len("[2 convs]") != 186 {
		t.Errorf("the count cell ends at %d, want 186: %q", i+len("[2 convs]"), plain)
	}
	if !strings.HasSuffix(strings.TrimRight(plain, " "), "2h ago") {
		t.Errorf("the age cell does not end the group row: %q", plain)
	}
	plainChild := ansi.Strip(childRow)
	if i := resumeRuneIndex(plainChild, "17 steps"); i < 0 || i+len("17 steps") != 186 {
		t.Errorf("the step count ends at %d, want 186: %q", i+len("17 steps"), plainChild)
	}
}

// resumeRuneIndex finds a needle in a string by cell, not by byte — the
// reference's rows carry the box-drawing characters, which take three bytes
// each.
func resumeRuneIndex(s, needle string) int {
	r := []rune(s)
	n := []rune(needle)
	for i := 0; i+len(n) <= len(r); i++ {
		if string(r[i:i+len(n)]) == needle {
			return i
		}
	}
	return -1
}

// TestResumeTreeEnterTogglesGroup pins the reference's Enter behaviour on a
// group row: it expands or collapses the group and keeps the cursor on the
// group row.
//
// 【一手】 agy 1.4.7: Enter on a collapsed group row expands it and the
// cursor stays where it was.
func TestResumeTreeEnterTogglesGroup(t *testing.T) {
	m := resumeTreeFixture(t)
	m.resumeCursor = 3 // the "openagy" group row
	before := len(m.resumeTreeRows())
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	if m.resumeCursor != 3 {
		t.Errorf("the cursor moved to %d, want to stay on the group row (3)", m.resumeCursor)
	}
	if got := len(m.resumeTreeRows()); got != before+1 {
		t.Errorf("the group holds %d rows now, want %d", got, before+1)
	}
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	if got := len(m.resumeTreeRows()); got != before {
		t.Errorf("the group holds %d rows now, want %d", got, before)
	}
}

// TestResumeTreeCtrlETogglesCursorGroup pins the reference's ctrl+e
// behaviour: it expands or collapses the group under the cursor and leaves
// the other groups alone.
//
// 【一手】 agy 1.4.7: with the cursor on a collapsed group and another group
// expanded, ctrl+e expands the cursor's group and keeps the other one
// expanded.
func TestResumeTreeCtrlETogglesCursorGroup(t *testing.T) {
	m := resumeTreeFixture(t)
	// The cursor sits on the first group's conversation; the group is
	// expanded.
	if hint := ansi.Strip(m.resumeTreeHint(m.resumeTreeRows())); !strings.Contains(hint, "ctrl+e Collapse") {
		t.Errorf("the hint does not offer to collapse the expanded group: %q", hint)
	}
	m.Update(tea.KeyPressMsg(tea.Key{Code: 'e', Mod: tea.ModCtrl}))
	if m.resumeExpanded[m.resumeGroups[0].Key] {
		t.Error("ctrl+e did not collapse the group under the cursor")
	}
	if hint := ansi.Strip(m.resumeTreeHint(m.resumeTreeRows())); !strings.Contains(hint, "ctrl+e Expand") {
		t.Errorf("the hint does not offer to expand the collapsed group: %q", hint)
	}
	m.Update(tea.KeyPressMsg(tea.Key{Code: 'e', Mod: tea.ModCtrl}))
	if !m.resumeExpanded[m.resumeGroups[0].Key] {
		t.Error("ctrl+e did not expand the group under the cursor")
	}
}

// TestResumeTreeF2F4OnGroupRowDoNothing pins that the rename and delete
// keys do nothing on a group row.
//
// 【一手】 agy 1.4.7: f2 and f4 on a group row leave the picker alone.
func TestResumeTreeF2F4OnGroupRowDoNothing(t *testing.T) {
	m := resumeTreeFixture(t)
	m.resumeCursor = 3 // a group row
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyF2}))
	if m.panel.kind == panelRename {
		t.Error("f2 on a group row opened the rename panel")
	}
	if !m.resumeMode {
		t.Error("f2 on a group row closed the picker")
	}
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyF4}))
	if !m.resumeMode {
		t.Error("f4 on a group row closed the picker")
	}
	if len(m.resumeList) != 4 {
		t.Errorf("f4 on a group row deleted a conversation (%d left)", len(m.resumeList))
	}
}

// TestResumeTreeHintAndViewSwitch pins the tree's keyboard hint and the
// ctrl+f switch between the tree and the flat list.
//
// 【一手】 agy 1.4.7: the tree's hint reads "ctrl+f Conversation View" and
// "ctrl+e Collapse"; the flat list's hint reads "ctrl+f Grouped View".
func TestResumeTreeHintAndViewSwitch(t *testing.T) {
	m := resumeTreeFixture(t)
	hint := ansi.Strip(m.resumeView())
	if !strings.Contains(hint, "ctrl+f Conversation View") {
		t.Errorf("the tree does not advertise the flat view: %q", hint)
	}
	if !strings.Contains(hint, "ctrl+e") {
		t.Errorf("the tree does not advertise ctrl+e: %q", hint)
	}
	m.Update(tea.KeyPressMsg(tea.Key{Code: 'f', Mod: tea.ModCtrl}))
	if m.resumeGrouped {
		t.Fatal("ctrl+f did not leave the tree")
	}
	hint = ansi.Strip(m.resumeView())
	if !strings.Contains(hint, "ctrl+f Grouped View") {
		t.Errorf("the flat list does not advertise the tree: %q", hint)
	}
	if strings.Contains(hint, "ctrl+e") {
		t.Errorf("the flat list still advertises ctrl+e: %q", hint)
	}
	m.Update(tea.KeyPressMsg(tea.Key{Code: 'f', Mod: tea.ModCtrl}))
	if !m.resumeGrouped {
		t.Fatal("ctrl+f did not come back to the tree")
	}
}

// TestResumeTreeSearchSwitchesToFlatList pins that a search draws the flat
// list while the grouped flag stays on.
//
// 【一手】 agy 1.4.7: typing into the search field leaves the tree and shows
// the filtered conversation list; the hint still offers "ctrl+f Conversation
// View" and no ctrl+e.
func TestResumeTreeSearchSwitchesToFlatList(t *testing.T) {
	m := resumeTreeFixture(t)
	m.Update(tea.KeyPressMsg(tea.Key{Code: 'P', Text: "P"}))
	if m.resumeQuery != "P" {
		t.Fatalf("resumeQuery = %q, want %q", m.resumeQuery, "P")
	}
	body := ansi.Strip(m.resumeView())
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
