// desktop_test.go — the Antigravity desktop app's conversation store.
//
// The store is the sibling of the CLI's own: the CLI's is
// ~/.gemini/antigravity-cli and the desktop app's is ~/.gemini/antigravity.
// The tests below point $ANTIGRAVITY_HOME at a scratch home, so both
// stores live under it and the user's own files are never touched.

package convstore

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver"
)

// desktopConv is one conversation of a scratch desktop store.
type desktopConv struct {
	ID      string
	Title   string
	Preview string
	Steps   int
}

// writeDesktopStore writes a scratch desktop store: the conversations
// directory, the summary index and the annotation file.
func writeDesktopStore(t *testing.T, root string, convs []desktopConv) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "conversations"), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite3", filepath.Join(root, "conversation_summaries.db"))
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
		steps := []Step{{Idx: 0, Type: StepUserInput, Status: 3, Text: c.Preview}}
		if err := WriteConversation(
			filepath.Join(root, "conversations", c.ID+".db"), "traj-"+c.ID, steps); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO conversation_summaries
			(conversation_id, title, preview, step_count, last_modified_time, workspace_uris)
			VALUES (?,?,?,?,?,?)`,
			c.ID, c.Title, c.Preview, c.Steps,
			time.Now().UTC().Format("2006-01-02 15:04:05"), "[]"); err != nil {
			t.Fatal(err)
		}
		ann := filepath.Join(root, "annotations")
		if err := os.MkdirAll(ann, 0o755); err != nil {
			t.Fatal(err)
		}
		body := `title:"` + c.Title + `"`
		if err := os.WriteFile(filepath.Join(ann, c.ID+".pbtxt"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// desktopHome points the CLI's store and the desktop app's store at a
// scratch home.
func desktopHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	t.Setenv("HOME", home)
	return home
}

// TestDesktopStoreRootIsTheSiblingOfTheCLIStore pins where the Antigravity
// desktop app's store lives.
//
// 【反编译】 agy 1.4.12: backend.(*ServerBackend).fallbackConvImportDirs
// (0x102776500) joins the literal "antigravity" onto the backend's own
// path and returns a one-element slice — the fallback import directory is
// the app data directory's "antigravity" child and nothing else.
func TestDesktopStoreRootIsTheSiblingOfTheCLIStore(t *testing.T) {
	home := desktopHome(t)
	if got, want := DesktopStoreRoot(), filepath.Join(home, "antigravity"); got != want {
		t.Errorf("DesktopStoreRoot = %q, want %q", got, want)
	}
}

// TestListDesktopConversationsReadsTheDesktopStore pins that the Other
// tab's rows come from the desktop app's store, with their previews.
func TestListDesktopConversationsReadsTheDesktopStore(t *testing.T) {
	home := desktopHome(t)
	writeDesktopStore(t, DesktopStoreRoot(), []desktopConv{
		{ID: "11110000-0000-4000-8000-000000000001", Title: "Casual Laughter Response",
			Preview: "tell me a joke", Steps: 2},
	})
	metas, previews, err := ListDesktopConversations()
	if err != nil {
		t.Fatalf("ListDesktopConversations: %v", err)
	}
	if len(metas) != 1 {
		t.Fatalf("the desktop store holds %d conversations, want 1", len(metas))
	}
	if metas[0].Title != "Casual Laughter Response" {
		t.Errorf("the conversation is titled %q, want %q", metas[0].Title, "Casual Laughter Response")
	}
	if metas[0].StepCount != 2 {
		t.Errorf("the conversation carries %d steps, want 2", metas[0].StepCount)
	}
	if previews[metas[0].ID] != "tell me a joke" {
		t.Errorf("the preview is %q, want %q", previews[metas[0].ID], "tell me a joke")
	}
	if _, err := os.Stat(filepath.Join(home, "conversations")); err == nil {
		t.Error("the listing wrote into the CLI's own store")
	}
}

// TestListDesktopConversationsSkipsWhatTheCLIStoreHolds pins that a
// conversation the CLI's own store already holds is not offered again.
func TestListDesktopConversationsSkipsWhatTheCLIStoreHolds(t *testing.T) {
	desktopHome(t)
	writeDesktopStore(t, DesktopStoreRoot(), []desktopConv{
		{ID: "11110000-0000-4000-8000-000000000001", Title: "Casual Laughter Response",
			Preview: "tell me a joke", Steps: 2},
	})
	if err := SaveConversation("11110000-0000-4000-8000-000000000001", "", []Step{
		{Idx: 0, Type: StepUserInput, Status: 1, Text: "already here"},
	}); err != nil {
		t.Fatalf("SaveConversation: %v", err)
	}
	metas, _, err := ListDesktopConversations()
	if err != nil {
		t.Fatalf("ListDesktopConversations: %v", err)
	}
	if len(metas) != 0 {
		t.Errorf("the desktop store offers %d conversations, want none: %+v", len(metas), metas)
	}
}

// TestImportConversationCopiesTheConversation pins the import: the
// conversation lands in the CLI's own store, its annotation comes with it,
// and the CLI's own index knows the row.
func TestImportConversationCopiesTheConversation(t *testing.T) {
	home := desktopHome(t)
	const id = "11110000-0000-4000-8000-000000000001"
	writeDesktopStore(t, DesktopStoreRoot(), []desktopConv{
		{ID: id, Title: "Casual Laughter Response", Preview: "tell me a joke", Steps: 2},
	})
	metas, _, err := ListDesktopConversations()
	if err != nil {
		t.Fatalf("ListDesktopConversations: %v", err)
	}
	imported, err := ImportConversation(metas[0])
	if err != nil {
		t.Fatalf("ImportConversation: %v", err)
	}
	if imported.ID != id {
		t.Errorf("the imported conversation is %q, want %q", imported.ID, id)
	}
	if imported.Path != filepath.Join(home, "conversations", id+".db") {
		t.Errorf("the imported conversation lives at %q", imported.Path)
	}
	if imported.Title != "Casual Laughter Response" {
		t.Errorf("the imported conversation is titled %q, want %q",
			imported.Title, "Casual Laughter Response")
	}
	// The conversation is the desktop app's own, step for step.
	c, err := LoadConversation(imported.Path)
	if err != nil {
		t.Fatalf("LoadConversation: %v", err)
	}
	if c == nil || len(c.Steps) != 1 || c.Steps[0].Text != "tell me a joke" {
		t.Fatalf("the imported conversation does not carry the desktop app's step: %+v", c)
	}
	// The annotation came with it, so the AI-written title survives.
	if _, err := os.Stat(filepath.Join(home, "annotations", id+".pbtxt")); err != nil {
		t.Errorf("the annotation was not copied: %v", err)
	}
	// The CLI's own index knows the row, so the /resume picker lists it.
	rows, err := ListSummaries()
	if err != nil {
		t.Fatalf("ListSummaries: %v", err)
	}
	found := false
	for _, r := range rows {
		if r.ConversationID == id {
			found = true
			if r.Title != "Casual Laughter Response" {
				t.Errorf("the index row is titled %q, want %q", r.Title, "Casual Laughter Response")
			}
			if r.Preview != "tell me a joke" {
				t.Errorf("the index row's preview is %q, want %q", r.Preview, "tell me a joke")
			}
		}
	}
	if !found {
		t.Errorf("the CLI's own index has no row for %q", id)
	}
}

// TestImportConversationRefusesAnExistingID pins that the import never
// overwrites a conversation the user already has.
func TestImportConversationRefusesAnExistingID(t *testing.T) {
	desktopHome(t)
	const id = "11110000-0000-4000-8000-000000000001"
	writeDesktopStore(t, DesktopStoreRoot(), []desktopConv{
		{ID: id, Title: "Casual Laughter Response", Preview: "tell me a joke", Steps: 2},
	})
	if err := SaveConversation(id, "", []Step{
		{Idx: 0, Type: StepUserInput, Status: 1, Text: "mine"},
	}); err != nil {
		t.Fatalf("SaveConversation: %v", err)
	}
	metas, _, err := ListDesktopConversations()
	if err != nil {
		t.Fatalf("ListDesktopConversations: %v", err)
	}
	if len(metas) != 0 {
		t.Fatalf("the desktop store offers %d conversations, want none", len(metas))
	}
	// The import is asked for by hand.
	meta := ConversationMeta{
		ID:   id,
		Path: filepath.Join(DesktopStoreRoot(), "conversations", id+".db"),
	}
	if _, err := ImportConversation(meta); err == nil {
		t.Error("the import overwrote a conversation the CLI's store already holds")
	}
	c, err := LoadConversation(ConversationPath(id))
	if err != nil {
		t.Fatalf("LoadConversation: %v", err)
	}
	if c == nil || len(c.Steps) != 1 || c.Steps[0].Text != "mine" {
		t.Errorf("the CLI's own conversation was changed: %+v", c)
	}
}

// TestImportConversationRefusesAForeignFile pins that only a conversation
// of the desktop app's store can be imported.
func TestImportConversationRefusesAForeignFile(t *testing.T) {
	desktopHome(t)
	outside := filepath.Join(t.TempDir(), "somewhere.db")
	if err := WriteConversation(outside, "traj-x", []Step{
		{Idx: 0, Type: StepUserInput, Status: 1, Text: "elsewhere"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := ImportConversation(ConversationMeta{ID: "x", Path: outside}); err == nil {
		t.Error("the import accepted a file outside the Antigravity app's store")
	}
}
