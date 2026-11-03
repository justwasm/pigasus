// desktop.go — the Antigravity desktop app's conversation store.
//
// The /resume picker has two tabs: the CLI's own conversations and the
// conversations the Antigravity desktop app (Antigravity 2.0) wrote. The
// second tab is the store this file reads.
//
// 【反编译】 agy 1.4.12: backend.(*ServerBackend).fallbackConvImportDirs
// (0x102776500) joins the literal "antigravity" onto the backend's own
// path and returns a one-element slice — the fallback import directory is
// the app data directory's "antigravity" child and nothing else. The
// CLI's own store is ~/.gemini/antigravity-cli, so the desktop app's store
// is its sibling: ~/.gemini/antigravity.
//
// 【一手】 The store's layout is the CLI's own: conversations/<id>.db, one
// database per conversation; annotations/<id>.pbtxt, the AI-written title;
// and conversation_summaries.db, the index.
//
// The desktop app writes its conversations in WAL mode, so the write-ahead
// log beside the database holds the frames the database file does not carry
// yet. An import copies both and folds the log into the copy.

package convstore

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DesktopStoreRoot returns the root of the Antigravity desktop app's
// conversation store.
//
// The desktop app's store is a sibling of the CLI's own store: the CLI's
// is <home>/antigravity-cli and the desktop app's is <home>/antigravity.
// $ANTIGRAVITY_HOME names the home both of them live in.
func DesktopStoreRoot() string {
	if h := os.Getenv("ANTIGRAVITY_HOME"); h != "" {
		return filepath.Join(h, "antigravity")
	}
	if h := os.Getenv("HOME"); h != "" {
		return filepath.Join(h, ".gemini", "antigravity")
	}
	return filepath.Join(os.TempDir(), ".gemini", "antigravity")
}

// ListDesktopConversations lists the conversations of the Antigravity
// desktop app's store that the CLI's own store does not hold yet, newest
// first.
//
// A conversation the CLI's own store already holds is left out: it is
// already importable, and offering it twice would let the user import a
// conversation over itself.
//
// The previews — the raw first user message of every conversation — are
// returned beside the rows: the Other tab's search matches them the same
// way the CLI tab's search matches its own.
func ListDesktopConversations() ([]ConversationMeta, map[string]string, error) {
	metas, previews, err := ListConversationsWithPreviewsIn(DesktopStoreRoot())
	if err != nil {
		return nil, nil, err
	}
	have := conversationIDs()
	out := make([]ConversationMeta, 0, len(metas))
	for _, m := range metas {
		if have[m.ID] {
			continue
		}
		out = append(out, m)
	}
	return out, previews, nil
}

// ImportConversation copies a conversation of the Antigravity desktop
// app's store into the CLI's own store, and returns the imported
// conversation — the row the picker resumes.
//
// The copy is byte for byte: the conversation's database, its write-ahead
// log and its annotation file. Nothing is re-encoded, so a tool call, a
// thinking block and a file edit survive the import exactly as the desktop
// app wrote them.
//
// The import refuses to overwrite: a conversation whose id the CLI's own
// store already holds is left alone.
//
// 【strings】 The reference's own strings name the same three steps — the
// import ("Importing conversation %s from %s"), the save of the imported
// trajectory ("Failed to save imported trajectory %s: %v") and the update
// of its summary row ("Failed to update summary for imported trajectory
// %s: %v") — and the failure it reports when the import itself cannot be
// done ("Failed to import conversation %s from %s: %v"). The event it
// records when the import worked is "conversation_imported". The surviving
// pruned index of that string table is
// out/local-macos-1.4.11/strings-index.txt (the full dump is retired).
func ImportConversation(meta ConversationMeta) (ConversationMeta, error) {
	if meta.ID == "" {
		return ConversationMeta{}, errors.New("convstore: import: the conversation has no id")
	}
	root := DesktopStoreRoot()
	if !withinStore(root, meta.Path) {
		return ConversationMeta{}, fmt.Errorf("convstore: import: %s is not in the Antigravity app's store", meta.Path)
	}
	if HasConversation(meta.ID) {
		return ConversationMeta{}, fmt.Errorf("convstore: import: conversation %s is already in the CLI's store", meta.ID)
	}

	dst := ConversationPath(meta.ID)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return ConversationMeta{}, fmt.Errorf("convstore: import %s: %w", meta.ID, err)
	}
	if err := copyFile(meta.Path, dst, 0o644); err != nil {
		os.Remove(dst)
		return ConversationMeta{}, fmt.Errorf("convstore: import %s: %w", meta.ID, err)
	}
	// The desktop app writes in WAL mode: the log beside the database
	// holds the frames the database file does not carry yet. It is copied
	// with the database and folded into it, so the imported conversation is
	// one self-contained file.
	if _, err := os.Stat(meta.Path + "-wal"); err == nil {
		_ = copyFile(meta.Path+"-wal", dst+"-wal", 0o644)
	}
	foldWriteAheadLog(dst)

	// The annotation carries the AI-written title; the title is what the
	// imported row shows.
	title := annotationTitle(root, meta.ID)
	if title != "" {
		src := filepath.Join(root, "annotations", meta.ID+".pbtxt")
		if data, err := os.ReadFile(src); err == nil {
			annDir := filepath.Join(StoreDir(), "annotations")
			if err := os.MkdirAll(annDir, 0o755); err == nil {
				_ = os.WriteFile(filepath.Join(annDir, meta.ID+".pbtxt"), data, 0o644)
			}
		}
	}

	steps, preview, project := readImportedConversation(dst)
	imported := ConversationMeta{
		ID:        meta.ID,
		Path:      dst,
		Title:     title,
		StepCount: steps,
		Project:   project,
		ModTime:   meta.ModTime,
	}
	if imported.ModTime.IsZero() {
		if info, err := os.Stat(dst); err == nil {
			imported.ModTime = info.ModTime()
		}
	}
	if title == "" {
		imported.Title = conversationLabel("", preview, meta.ID)
	}

	row := SummaryRow{
		ConversationID:    imported.ID,
		Title:             imported.Title,
		Preview:           preview,
		StepCount:         imported.StepCount,
		LastModifiedTime:  imported.ModTime,
		LastUserInputTime: imported.ModTime,
	}
	if project != "" {
		row.WorkspaceURIs = []string{"file://" + filepath.Clean(project)}
	}
	if err := writeImportedSummary(row); err != nil {
		return ConversationMeta{}, err
	}
	return imported, nil
}

// writeImportedSummary writes the imported conversation's row into the
// CLI's own summaries index.
//
// The index is created when the CLI's store has none yet: the reference
// creates the table itself, and an imported conversation the index does not
// know is a conversation the CLI's own list cannot show.
func writeImportedSummary(row SummaryRow) error {
	path := filepath.Join(StoreDir(), "conversation_summaries.db")
	sdb, err := sql.Open("sqlite3", "file:"+path+"?mode=rwc")
	if err != nil {
		return fmt.Errorf("convstore: import summary %s: %w", row.ConversationID, err)
	}
	defer sdb.Close()
	if _, err := sdb.Exec(summariesTableDDL); err != nil {
		return fmt.Errorf("convstore: import summary %s: %w", row.ConversationID, err)
	}
	ensureSummariesColumns(sdb)
	return upsertSummaryRow(sdb, row)
}

// readImportedConversation reads the imported file back: the number of
// steps it holds, the first user message (the preview) and the workspace
// it ran in.
//
// An unreadable file is not an error here: the import is a copy, and the
// row it writes still names the conversation.
func readImportedConversation(path string) (int, string, string) {
	c, err := LoadConversation(path)
	if err != nil || c == nil {
		return 0, "", ""
	}
	preview := ""
	for _, s := range c.Steps {
		if s.Role() != "user" {
			continue
		}
		if text := strings.TrimSpace(s.Text); text != "" {
			preview = text
			break
		}
	}
	return len(c.Steps), preview, c.Project
}

// foldWriteAheadLog folds a conversation file's write-ahead log into the
// file itself, so the imported conversation is one file.
//
// A log that cannot be folded is left alone: the log holds frames the
// database file does not carry yet, and dropping it would lose them.
func foldWriteAheadLog(path string) {
	sdb, err := sql.Open("sqlite3", "file:"+path+"?mode=rwc")
	if err != nil {
		return
	}
	defer sdb.Close()
	if _, err := sdb.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		return
	}
	os.Remove(path + "-wal")
}

// withinStore reports whether path is inside the store rooted at root.
func withinStore(root, path string) bool {
	if root == "" || path == "" {
		return false
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
