package convstore

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Summary is one row of the reference's `conversation_summaries`
// table — the index the Antigravity Web UI's conversation list is
// built from.
//
// 【一手】 2026-10-26: the table lives in
// `~/.gemini/antigravity-cli/conversation_summaries.db` and its
// columns are exactly the fields the reference's own
// `exa.jetski_cortex_pb.CascadeTrajectorySummary` carries — the
// `raw_summary` blob is that message, serialised.
//
// The row is the cheap half of the summary (title, step count,
// timestamps, the project the conversation belongs to); the blob is
// the whole message, including the workspace metadata and the
// trajectory metadata the frontend groups by.
type Summary struct {
	// ConversationID is the row's primary key — the uuid the
	// conversation's file is named after.
	ConversationID string
	// Title is the AI-written title, empty when the conversation
	// was never titled.
	Title string
	// Preview is the first user message, or the model's opening
	// line.
	Preview string
	// StepCount is the number of stored steps.
	StepCount int
	// LastModifiedTime is when the conversation last changed.
	LastModifiedTime time.Time
	// WorkspaceURIs are the workspaces the conversation ran in.
	WorkspaceURIs []string
	// Status is the reference's own enum name, e.g.
	// "CASCADE_RUN_STATUS_IDLE".
	Status string
	// Source is the reference's own enum name, e.g.
	// "CORTEX_TRAJECTORY_SOURCE_JETBOX". Empty means
	// UNSPECIFIED, which the frontend counts as local.
	Source string
	// ProjectID is the project the conversation belongs to —
	// "default-cli-project" or a workspace project's uuid.
	ProjectID string
	// ParentConversationID is set for a subagent's row.
	ParentConversationID string
	// LastUserInputTime is when the user last typed.
	LastUserInputTime time.Time
	// RawSummary is the serialised CascadeTrajectorySummary the
	// reference wrote for this conversation. It is the authoritative
	// shape; the columns above are the same information flattened.
	RawSummary []byte
}

// ListSummaries reads the rows of the reference's
// `conversation_summaries` table that still have a conversation on
// disk, newest first.
//
// A row whose conversation file is gone is not a conversation the
// reference can open, so it is dropped — the same rule the
// reference's own list applies. The subagent rows are kept: the
// frontend drops them itself, by their parent link.
//
// A missing store is not an error: the reference's list is empty in
// that case too.
func ListSummaries() ([]Summary, error) {
	path := filepath.Join(StoreDir(), "conversation_summaries.db")
	if _, err := os.Stat(path); err != nil {
		return nil, nil
	}
	db, err := Open(path)
	if err != nil {
		if isUnreadableStore(err) {
			return nil, nil
		}
		return nil, err
	}
	defer db.Close()

	onDisk := conversationIDs()
	var out []Summary
	serr := db.Scan("conversation_summaries", func(r *Row) error {
		id := r.Text("conversation_id")
		if id == "" || strings.ContainsAny(id, `/\`) || !onDisk[id] {
			return nil
		}
		out = append(out, Summary{
			ConversationID:       id,
			Title:                r.Text("title"),
			Preview:              r.Text("preview"),
			StepCount:            int(r.Int("step_count")),
			LastModifiedTime:     parseSummaryTime(r.Text("last_modified_time")),
			WorkspaceURIs:        parseWorkspaceURIs(r.Text("workspace_uris")),
			Status:               r.Text("status"),
			Source:               r.Text("source"),
			ProjectID:            r.Text("project_id"),
			ParentConversationID: r.Text("parent_conversation_id"),
			LastUserInputTime:    parseSummaryTime(r.Text("last_user_input_time")),
			RawSummary:           r.Bytes("raw_summary"),
		})
		return nil
	})
	if serr != nil {
		return nil, serr
	}
	sortSummaries(out)
	fillTitlesFromCache(out)
	return out, nil
}

// fillTitlesFromCache gives a row without a title the name the
// metadata cache holds.
//
// 【一手】 2026-10-26: the reference's own daemon reconciles the
// summaries table when it starts — its log says
// "[Summaries] failed to generate summary for <id> while reconciling:
// conversation <id> not found" (`reconcile_summaries.go`) — and it
// rewrites the rows it can read in the reference's own shape, with an
// **empty title** when it cannot generate one. Two binaries sharing one
// store therefore blank each other's titles.
//
// The reconcile pass leaves the metadata cache alone, so the name the
// clone wrote is still there, and the row takes it back from there. The
// row's own title always wins: a conversation the reference named keeps
// the reference's name.
func fillTitlesFromCache(rows []Summary) {
	meta := LoadMetadata()
	if meta == nil {
		return
	}
	for i := range rows {
		if rows[i].Title != "" {
			continue
		}
		if entry, ok := meta.Conversations[rows[i].ConversationID]; ok {
			rows[i].Title = entry.Summary.Title
		}
	}
}

// conversationIDs reads the ids of the conversations on disk in one
// pass. The store's directory holds one `<id>.db` per conversation.
func conversationIDs() map[string]bool {
	out := map[string]bool{}
	entries, err := os.ReadDir(filepath.Join(StoreDir(), "conversations"))
	if err != nil {
		return out
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".db") {
			continue
		}
		out[strings.TrimSuffix(name, ".db")] = true
	}
	return out
}

// sortSummaries orders the rows newest first, with the conversation
// id as the tie-break — the reference's own list order.
func sortSummaries(out []Summary) {
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if !a.LastModifiedTime.Equal(b.LastModifiedTime) {
			return a.LastModifiedTime.After(b.LastModifiedTime)
		}
		return a.ConversationID < b.ConversationID
	})
}

// parseWorkspaceURIs reads the `workspace_uris` column — a JSON
// array of file:// URIs.
func parseWorkspaceURIs(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var uris []string
	if err := json.Unmarshal([]byte(raw), &uris); err != nil {
		return nil
	}
	return uris
}
