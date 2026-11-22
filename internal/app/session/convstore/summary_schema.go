// Package convstore — summary_schema.go: the summaries table's shape.
//
// 【一手】 2026-10-26: the reference creates this table itself, with
// 21 columns; the DDL below is the reference's own, recovered from the
// store's sqlite_master (a clone-created table and a
// reference-created table are not the same shape).
//
// The clone writes the columns the reference writes, so a store the
// clone made on its own gets the reference's table: the reference can
// open it, and the reference's writer can add its raw_summary blob to
// a conversation the clone started.
//
// A store written by an older clone has a six-column table. The
// migration below adds the columns that are missing, so the clone's
// own write path keeps working on such a store.
package convstore

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ncruces/go-sqlite3"
	_ "github.com/ncruces/go-sqlite3/driver"
	"github.com/ncruces/go-sqlite3/ext/fts5"
)

func init() {
	sqlite3.AutoExtension(fts5.Register)
}

// openSummariesDB opens an SQLite database with FTS5 virtual table support enabled,
// ensuring compatibility with agy 1.5.14 conversationsearch.
func openSummariesDB(dsn string) (*sql.DB, error) {
	return sql.Open("sqlite3", dsn)
}

// summariesTableDDL is the reference's own CREATE TABLE for the
// summaries index.
const summariesTableDDL = `CREATE TABLE IF NOT EXISTS conversation_summaries (
	conversation_id text,
	title text NOT NULL DEFAULT "",
	preview text NOT NULL DEFAULT "",
	step_count integer NOT NULL DEFAULT 0,
	last_modified_time datetime NOT NULL,
	workspace_uris text NOT NULL,
	status text NOT NULL DEFAULT "",
	source text NOT NULL DEFAULT "",
	project_id text NOT NULL DEFAULT "",
	agent_name text NOT NULL DEFAULT "",
	parent_conversation_id text NOT NULL DEFAULT "",
	nesting_depth integer NOT NULL DEFAULT 0,
	battle_id text NOT NULL DEFAULT "",
	winning_conversation_id text NOT NULL DEFAULT "",
	not_fully_idle numeric NOT NULL DEFAULT false,
	killed numeric NOT NULL DEFAULT false,
	last_user_input_time datetime NOT NULL,
	last_user_input_step_index integer NOT NULL DEFAULT -1,
	app_data_dir text NOT NULL DEFAULT "",
	raw_summary blob,
	group_id text NOT NULL DEFAULT "",
	PRIMARY KEY (conversation_id))`

// summaryColumn is one column of the reference's table: its name and
// the definition an ALTER TABLE would add.
type summaryColumn struct {
	name       string
	definition string
}

// summaryColumns is the reference's column list, in the reference's
// order. conversation_id is left out: it is the primary key, and every
// table already has it.
var summaryColumns = []summaryColumn{
	{"title", `title text NOT NULL DEFAULT ""`},
	{"preview", `preview text NOT NULL DEFAULT ""`},
	{"step_count", `step_count integer NOT NULL DEFAULT 0`},
	{"last_modified_time", `last_modified_time datetime NOT NULL DEFAULT ""`},
	{"workspace_uris", `workspace_uris text NOT NULL DEFAULT ""`},
	{"status", `status text NOT NULL DEFAULT ""`},
	{"source", `source text NOT NULL DEFAULT ""`},
	{"project_id", `project_id text NOT NULL DEFAULT ""`},
	{"agent_name", `agent_name text NOT NULL DEFAULT ""`},
	{"parent_conversation_id", `parent_conversation_id text NOT NULL DEFAULT ""`},
	{"nesting_depth", `nesting_depth integer NOT NULL DEFAULT 0`},
	{"battle_id", `battle_id text NOT NULL DEFAULT ""`},
	{"winning_conversation_id", `winning_conversation_id text NOT NULL DEFAULT ""`},
	{"not_fully_idle", `not_fully_idle numeric NOT NULL DEFAULT false`},
	{"killed", `killed numeric NOT NULL DEFAULT false`},
	{"last_user_input_time", `last_user_input_time datetime NOT NULL DEFAULT ""`},
	{"last_user_input_step_index", `last_user_input_step_index integer NOT NULL DEFAULT -1`},
	{"app_data_dir", `app_data_dir text NOT NULL DEFAULT ""`},
	{"raw_summary", `raw_summary blob`},
	{"group_id", `group_id text NOT NULL DEFAULT ""`},
}

// ensureSummariesColumns adds the reference's columns to a summaries
// table that predates them. It is a no-op on the reference's own table
// and on a table the clone has already migrated.
func ensureSummariesColumns(sdb *sql.DB) {
	have, ok := summariesColumnNames(sdb)
	if !ok {
		return
	}
	for _, col := range summaryColumns {
		if have[col.name] {
			continue
		}
		_, _ = sdb.Exec(`ALTER TABLE conversation_summaries ADD COLUMN ` + col.definition)
	}
}

// summariesColumnNames lists the columns the summaries table has.
func summariesColumnNames(sdb *sql.DB) (map[string]bool, bool) {
	rows, err := sdb.Query(`PRAGMA table_info(conversation_summaries)`)
	if err != nil {
		return nil, false
	}
	defer rows.Close()
	have := map[string]bool{}
	for rows.Next() {
		var cid, notnull, pk int
		var name, ctype string
		var dflt any
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return nil, false
		}
		have[name] = true
	}
	return have, true
}

// ensureSearchSchema ensures that search_schema_version, conversation_parents,
// and conversations_fts (FTS5 virtual table) exist in conversation_summaries.db.
// This matches the schema used by agy 1.5.14 conversationsearch for /resume.
func ensureSearchSchema(sdb *sql.DB) {
	_, _ = sdb.Exec(`CREATE TABLE IF NOT EXISTS search_schema_version (version INTEGER PRIMARY KEY)`)
	_, _ = sdb.Exec(`INSERT OR IGNORE INTO search_schema_version (version) VALUES (1)`)
	_, _ = sdb.Exec(`CREATE TABLE IF NOT EXISTS conversation_parents (
		conversation_id TEXT PRIMARY KEY,
		parent_id TEXT NOT NULL
	) WITHOUT ROWID`)
	_, _ = sdb.Exec(`CREATE VIRTUAL TABLE IF NOT EXISTS conversations_fts USING fts5(
		conversation_id UNINDEXED,
		chunk_index UNINDEXED,
		chunk_first_step UNINDEXED,
		chunk_last_step UNINDEXED,
		last_modified_time UNINDEXED,
		step_count UNINDEXED,
		last_step_created_at UNINDEXED,
		archived UNINDEXED,
		user_step_byte_offsets UNINDEXED,
		agent_step_byte_offsets UNINDEXED,
		title,
		workspace,
		project,
		user_body,
		agent_body,
		tokenize = 'unicode61 remove_diacritics 2',
		prefix = '1 2 3'
	)`)
}

// updateConversationsFTS indexes the conversation into conversations_fts so that
// agy 1.5.14's /resume picker (which queries chunk_index = -1 ordered by last_modified_time DESC)
// and full-text keyword search immediately discover this conversation.
func updateConversationsFTS(sdb *sql.DB, id, title, workspace string, steps []Step) {
	ensureSearchSchema(sdb)

	nowNanoStr := fmt.Sprintf("%d", time.Now().UnixNano())
	wsName := ""
	if workspace != "" {
		wsName = filepath.Base(filepath.Clean(workspace))
	}

	// 1. Delete old chunk -1 for this conversation and insert the header chunk
	_, _ = sdb.Exec(`DELETE FROM conversations_fts WHERE conversation_id = ? AND chunk_index = -1`, id)
	_, _ = sdb.Exec(`INSERT INTO conversations_fts (
		conversation_id, chunk_index, chunk_first_step, chunk_last_step,
		last_modified_time, step_count, last_step_created_at, archived,
		user_step_byte_offsets, agent_step_byte_offsets,
		title, workspace, project, user_body, agent_body
	) VALUES (?, -1, 0, 0, ?, ?, ?, 0, '', '', ?, ?, 'CLI Project', '', '')`,
		id, nowNanoStr, len(steps), nowNanoStr, title, wsName)

	// 2. Index user prompts and agent responses into chunk 0 for keyword search in /resume
	var userBody strings.Builder
	var agentBody strings.Builder
	for _, s := range steps {
		switch s.Type {
		case StepUserInput:
			if s.Text != "" {
				userBody.WriteString(s.Text)
				userBody.WriteString("\n")
			}
		case StepModelResponse:
			if s.Text != "" {
				agentBody.WriteString(s.Text)
				agentBody.WriteString("\n")
			}
		}
	}
	if userBody.Len() > 0 || agentBody.Len() > 0 {
		_, _ = sdb.Exec(`DELETE FROM conversations_fts WHERE conversation_id = ? AND chunk_index = 0`, id)
		_, _ = sdb.Exec(`INSERT INTO conversations_fts (
			conversation_id, chunk_index, chunk_first_step, chunk_last_step,
			last_modified_time, step_count, last_step_created_at, archived,
			user_step_byte_offsets, agent_step_byte_offsets,
			title, workspace, project, user_body, agent_body
		) VALUES (?, 0, 0, ?, 0, 0, 0, 0, '', '', ?, ?, 'CLI Project', ?, ?)`,
			id, len(steps)-1, title, wsName, userBody.String(), agentBody.String())
	}
}

// repairRecentFTSHeaders repairs missing or zero-timestamp chunk_index = -1 rows
// in conversations_fts for recent conversations, ensuring historical pigasus sessions
// show up in agy /resume in correct chronological order.
func repairRecentFTSHeaders(sdb *sql.DB) {
	rows, err := sdb.Query(`
		SELECT conversation_id, title, workspace_uris, step_count, last_modified_time
		FROM conversation_summaries
		WHERE step_count > 0
		ORDER BY rowid DESC
		LIMIT 100`)
	if err != nil {
		return
	}
	defer rows.Close()

	type item struct {
		id, title, uris string
		stepCount       int
		lmt             string
	}
	var items []item
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.id, &it.title, &it.uris, &it.stepCount, &it.lmt); err == nil {
			items = append(items, it)
		}
	}

	for _, it := range items {
		var ftsLMT string
		err := sdb.QueryRow(`SELECT last_modified_time FROM conversations_fts WHERE conversation_id = ? AND chunk_index = -1`, it.id).Scan(&ftsLMT)
		if err == sql.ErrNoRows || ftsLMT == "" || ftsLMT == "0" {
			t := parseSummaryTime(it.lmt)
			if t.IsZero() || t.Year() <= 1 {
				convPath := filepath.Join(StoreDir(), "conversations", it.id+".db")
				if fi, err := os.Stat(convPath); err == nil {
					t = fi.ModTime()
				}
			}
			var nanoStr string
			if t.IsZero() || t.Year() <= 1 {
				nanoStr = fmt.Sprintf("%d", time.Now().UnixNano())
			} else {
				nanoStr = fmt.Sprintf("%d", t.UnixNano())
			}
			ws := ""
			if uris := parseWorkspaceURIs(it.uris); len(uris) > 0 {
				ws = projectFromURIs(uris[0])
			}
			_, _ = sdb.Exec(`DELETE FROM conversations_fts WHERE conversation_id = ? AND chunk_index = -1`, it.id)
			_, _ = sdb.Exec(`INSERT INTO conversations_fts (
				conversation_id, chunk_index, chunk_first_step, chunk_last_step,
				last_modified_time, step_count, last_step_created_at, archived,
				user_step_byte_offsets, agent_step_byte_offsets,
				title, workspace, project, user_body, agent_body
			) VALUES (?, -1, 0, 0, ?, ?, ?, 0, '', '', ?, ?, 'CLI Project', '', '')`,
				it.id, nanoStr, it.stepCount, nanoStr, it.title, ws)
		}
	}
}
