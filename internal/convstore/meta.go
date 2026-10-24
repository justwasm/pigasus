package convstore

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ConversationMeta is the per-file metadata the /resume picker shows.
// It is intentionally cheap: ID, title, step count, mtime. The full
// step list is loaded only when the user actually picks the row.
type ConversationMeta struct {
	// ID is the basename without the ".db" suffix — the same id
	// --conversation and --continue accept on the command line.
	ID string
	// Path is the absolute path to the database file. Pass to
	// LoadConversation when the user selects this row.
	Path string
	// Title is a one-line human label: the first user-input step's
	// text, trimmed and truncated. Falls back to the basename when
	// the database has no user message (e.g. an empty store).
	Title string
	// StepCount is the number of stored steps. Cheap: it is the
	// len(c.Steps) of the loaded conversation.
	StepCount int
	// Project is the workspace directory name the conversation ran in
	// (e.g. "openagy"), shown in the picker's right-hand column. Empty
	// when the store carries no workspace URI.
	Project string
	// ModTime is the file's modification time — matches the
	// reference's "X minutes ago" relative time format.
	ModTime time.Time
}

// conversationLabel picks the picker's row label, in the reference's
// fallback order: the AI-written summary, then the raw first user input,
// then the conversation id.
//
// Cycle 278: the id fallback was the visible difference on a store full of
// sessions the agent never titled. REF prints the bare id in that case
// (verified at 200x40 against agy 1.4.2: rows with `Title` and `Preview`
// both empty render as `fcda7ad5-…`); the clone printed "(no title)".
func conversationLabel(title, preview, id string) string {
	label := strings.Join(strings.Fields(title), " ")
	if label == "" {
		label = strings.Join(strings.Fields(preview), " ")
	}
	if label == "" {
		label = id
	}
	return label
}

// projectFromURIs pulls the workspace directory name out of a workspace URI
// list — either the index's `workspace_uris` column (a JSON array) or one
// entry of the cache's `WorkspaceURIs` array:
//
//	["file:///Users/gear/Documents/GitHub/openagy"]  ->  "openagy"
//
// Only the first URI is used — it is the workspace the conversation started
// in, which is what REF prints.
//
// REF v1.4.0 deliberately omits the project for conversations that were
// started in the *current* working directory: on this machine, that blanks
// the cell for the two /resume rows whose `WorkspaceURIs` happens to be the
// real path of the cwd (file:///Users/gear/Documents/GitHub/openagy), while
// rows stored under the symlink path
// (file:///Users/gear/GitHub/openagy, same directory) keep the project
// because REF compares the URI string to the cwd string literally, without
// resolving symlinks. The renderer downstream already drops the project
// cell when this returns "", so the picker just needs the empty string.
var currentWorkspace = sync.OnceValue(func() string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return wd
})

func projectFromURIs(raw string) string {
	const marker = "file://"
	i := strings.Index(raw, marker)
	if i < 0 {
		return ""
	}
	rest := raw[i+len(marker):]
	if j := strings.IndexAny(rest, "\"]"); j >= 0 {
		rest = rest[:j]
	}
	rest = strings.TrimSuffix(rest, "/")
	if rest == "" {
		return ""
	}
	// Literal path comparison against the cwd: see currentWorkspace's
	// doc comment. A path that does not match the cwd keeps its
	// basename; a path that does returns "" so the renderer drops the
	// project cell.
	if rest == currentWorkspace() {
		return ""
	}
	return filepath.Base(rest)
}

// parseSummaryTime parses the index's `last_modified_time` column, e.g.
// "2026-09-27 13:49:55.812582+00:00". Returns the zero time when the value is
// empty or in an unrecognised shape; the picker then prints no age.
func parseSummaryTime(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	for _, layout := range []string{
		"2006-01-02 15:04:05.999999-07:00",
		"2006-01-02 15:04:05.999999Z07:00",
		"2006-01-02 15:04:05-07:00",
		"2006-01-02 15:04:05.999999",
		"2006-01-02 15:04:05",
		time.RFC3339Nano,
	} {
		if t, err := time.Parse(layout, raw); err == nil {
			return t
		}
	}
	return time.Time{}
}
