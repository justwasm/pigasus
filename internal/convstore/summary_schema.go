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

import "database/sql"

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
