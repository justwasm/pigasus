// Package convstore — append.go: adding a turn to a conversation that
// already exists on disk.
//
// The reference's language server appends to the conversation file
// when a message is sent into a conversation it did not start; the
// clone's own TUI rewrites the file from the session's steps. Both
// write the same store, so the file has to be able to take either
// shape.
package convstore

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// AppendConversationSteps appends steps to the conversation database
// under conversations/<id>.db and refreshes the summary row.
//
// 【一手】 2026-10-26: the reference's own language server answers a
// message sent into a conversation it did not start — the
// conversation file is the state, and the reference appends the new
// turn to it. The file of the conversation the reference continued
// grew from three steps to five, and the earlier steps were still
// there.
//
// The clone appends instead of rewriting. A rewrite would replace the
// reference's rows with the clone's own Step shape, and that shape
// carries less: the model's thinking, the usage metadata and the user
// config live in the reference's step blobs and have no field in
// convstore.Step.
//
// A conversation the clone has never seen is written from scratch:
// there is nothing to append to.
func AppendConversationSteps(id, workspace string, steps []Step) error {
	if id == "" || len(steps) == 0 {
		return nil
	}
	path := ConversationPath(id)
	if !HasConversation(id) {
		if err := SaveConversation(id, workspace, steps); err != nil {
			return err
		}
		nameConversation(id, steps)
		return nil
	}
	sdb, err := sql.Open("sqlite3", "file:"+path+"?mode=rwc")
	if err != nil {
		return fmt.Errorf("convstore: append %s: %w", path, err)
	}
	defer sdb.Close()

	next, err := lastStepIndex(sdb)
	if err != nil {
		return err
	}
	for i, s := range steps {
		idx := next + 1 + int64(i)
		if _, err := sdb.Exec(
			`INSERT INTO steps (idx, step_type, status, step_payload) VALUES (?, ?, ?, ?)
			 ON CONFLICT(idx) DO UPDATE SET
			   step_type=excluded.step_type,
			   status=excluded.status,
			   step_payload=excluded.step_payload`,
			idx, s.Type, s.Status, encodeForkPayload(s)); err != nil {
			return fmt.Errorf("convstore: append step %d: %w", idx, err)
		}
	}
	bumpConversationSummary(id, next+1+int64(len(steps)))
	return nil
}

// nameConversation gives a conversation the clone has just created the
// title the reference generates at creation time.
//
// 【一手】 2026-10-26: the reference's v1.4.0 changelog notes that the
// title is generated at conversation-creation time, so a conversation
// shows a name instead of a placeholder. The Web UI's own fallback
// string is "Untitled Conversation" (it lives in the app bundle,
// cli-1.4.7-main.js, and in no reference binary), and the Web UI shows
// it whenever the instance's summary message carries no title — the
// summary message the clone serves is built from the summaries row's
// title column.
//
// The Web UI's JetboxWriteSummary request carries no title of its own,
// so nothing else would ever fill the column on this path. The clone
// generates the title heuristically (the reference uses the LLM) and
// writes it where the TUI path writes it: the summaries row's title and
// preview columns, and the metadata cache's Title.
//
// A conversation the reference created keeps the reference's own title:
// the title is only written when the clone creates the conversation.
func nameConversation(id string, steps []Step) {
	prompt := ""
	for _, s := range steps {
		if s.Type == StepUserInput && strings.TrimSpace(s.Text) != "" {
			prompt = s.Text
			break
		}
	}
	title := GenerateConversationTitle(prompt)
	if title == "" {
		return
	}
	_ = WriteSummariesTitle(id, title)
	_ = WriteCacheTitle(id, title)
}

// ConversationStepCount returns the number of steps the conversation
// file holds, or zero when there is no such conversation.
//
// The file is the ground truth for the step count: the reference's own
// summary row, its step count and its file's row count always agree
// (【一手】 the conversation the reference continued had five rows in
// the file and five in the summary row).
func ConversationStepCount(id string) int {
	if id == "" {
		return 0
	}
	path := ConversationPath(id)
	if !HasConversation(id) {
		return 0
	}
	sdb, err := sql.Open("sqlite3", "file:"+path+"?mode=ro")
	if err != nil {
		return 0
	}
	defer sdb.Close()
	var count int
	if err := sdb.QueryRow(`SELECT COUNT(*) FROM steps`).Scan(&count); err != nil {
		return 0
	}
	return count
}

// lastStepIndex returns the highest step index in the database, or -1
// when the table holds no step.
func lastStepIndex(sdb *sql.DB) (int64, error) {
	var idx sql.NullInt64
	if err := sdb.QueryRow(`SELECT MAX(idx) FROM steps`).Scan(&idx); err != nil {
		return 0, fmt.Errorf("convstore: last step index: %w", err)
	}
	if !idx.Valid {
		return -1, nil
	}
	return idx.Int64, nil
}

// bumpConversationSummary records the step count of a conversation the
// clone just appended to.
//
// Only the two columns the append changes are touched. The reference's
// row also carries the AI-written title, the preview and the raw
// summary blob, and the clone must not drop them: the title is the
// reference's own, and the Web UI's sidebar shows it.
//
// A row the clone has never written is left alone: the Web UI asks the
// server to write the summary right after the turn (JetboxWriteSummary)
// and that request carries the title.
func bumpConversationSummary(id string, count int64) {
	path := filepath.Join(StoreDir(), "conversation_summaries.db")
	if _, err := os.Stat(path); err != nil {
		return
	}
	sdb, err := sql.Open("sqlite3", "file:"+path+"?mode=rwc")
	if err != nil {
		return
	}
	defer sdb.Close()
	_, _ = sdb.Exec(
		`UPDATE conversation_summaries
		    SET step_count = ?,
		        last_modified_time = ?
		  WHERE conversation_id = ?`,
		count, summaryTime(time.Now()), id)
}

// summaryTime renders a timestamp in the reference's own shape for the
// summaries table: "2026-09-27 13:49:55.812582+00:00".
//
// 【一手】 the reference's rows carry UTC in this format, and
// parseSummaryTime reads it back.
func summaryTime(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04:05.999999-07:00")
}

// SummaryRow is one row of the summaries table, in the shape the
// reference writes.
type SummaryRow struct {
	ConversationID string
	// Title is the conversation's title. The reference stores the
	// model's summary in the preview column and mirrors it into the
	// title once the conversation has one.
	Title string
	// Preview is the short line the sidebar shows.
	Preview string
	// StepCount is the number of stored steps.
	StepCount int
	// LastModifiedTime is when the conversation last changed.
	LastModifiedTime time.Time
	// WorkspaceURIs are the workspaces the conversation ran in, as
	// file:// URIs.
	WorkspaceURIs []string
	// Status is the reference's own enum name, e.g.
	// "CASCADE_RUN_STATUS_IDLE".
	Status string
	// Source is the reference's own enum name, e.g.
	// "CORTEX_TRAJECTORY_SOURCE_JETBOX".
	Source string
	// ProjectID is the project the conversation belongs to.
	ProjectID string
	// LastUserInputTime is when the user last typed.
	LastUserInputTime time.Time
}

// WriteSummaryRow upserts one row of the summaries table.
//
// Only the columns the row carries are written. The reference's table
// holds more — the raw summary blob, the agent name, the parent
// conversation — and those are left as they are: a row the reference
// wrote must keep its blob.
//
// A write with an empty title or preview leaves the stored one alone.
// The Web UI's JetboxWriteSummary request carries the summary the
// instance gave it, and a summary with no title of its own must not
// erase the title the instance stored: the Web UI shows the title
// column of the row this function upserts, and the same rule is what
// bumpConversationSummary follows.
func WriteSummaryRow(row SummaryRow) error {
	if row.ConversationID == "" {
		return nil
	}
	path := filepath.Join(StoreDir(), "conversation_summaries.db")
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	sdb, err := sql.Open("sqlite3", "file:"+path+"?mode=rwc")
	if err != nil {
		return fmt.Errorf("convstore: summary %s: %w", row.ConversationID, err)
	}
	defer sdb.Close()

	// A store the clone made before the reference's table shape was
	// known has fewer columns; the write below names ten of them.
	ensureSummariesColumns(sdb)

	modified := row.LastModifiedTime
	if modified.IsZero() {
		modified = time.Now()
	}
	uris := ""
	if len(row.WorkspaceURIs) > 0 {
		encoded, err := json.Marshal(row.WorkspaceURIs)
		if err != nil {
			return fmt.Errorf("convstore: summary %s: %w", row.ConversationID, err)
		}
		uris = string(encoded)
	}
	lastInput := row.LastUserInputTime
	if lastInput.IsZero() {
		lastInput = modified
	}
	if _, err := sdb.Exec(
		`INSERT INTO conversation_summaries
		   (conversation_id, title, preview, step_count, workspace_uris,
		    last_modified_time, status, source, project_id, last_user_input_time)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(conversation_id) DO UPDATE SET
		   title=CASE WHEN excluded.title='' THEN conversation_summaries.title ELSE excluded.title END,
		   preview=CASE WHEN excluded.preview='' THEN conversation_summaries.preview ELSE excluded.preview END,
		   step_count=excluded.step_count,
		   workspace_uris=excluded.workspace_uris,
		   last_modified_time=excluded.last_modified_time,
		   status=excluded.status,
		   source=excluded.source,
		   project_id=excluded.project_id,
		   last_user_input_time=excluded.last_user_input_time`,
		row.ConversationID, row.Title, row.Preview, row.StepCount, uris,
		summaryTime(modified), row.Status, row.Source, row.ProjectID,
		summaryTime(lastInput)); err != nil {
		return fmt.Errorf("convstore: summary %s: %w", row.ConversationID, err)
	}
	return nil
}
