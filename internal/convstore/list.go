package convstore

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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

// ListConversations enumerates the conversations the /resume picker shows,
// newest first.
//
// Cycle 282 (corrects the cycle-278 "cache only" ruling): with the
// `summary-store-reads` feature flag off — the state observed on agy 1.4.2 —
// REF's ServerBackend.ListConversations calls scanConversations, and that
// scan's listConversationsFromDir walks conversations/*.db, stats every file
// and merges the ones the cache does not know into the returned list (then
// saves the refreshed cache). The picker is therefore disk-driven with the
// JSON cache as a summary cache on top, not a cache-only view. Cycle 278
// measured the opposite because on that store every .db already had a cache
// entry; the index-only rows it excluded had no file on disk at all.
//
// Cycle 184: two derived indexes describe the store:
//
//	cache/conversation_metadata.json  REF's conversation cache, flushed to
//	                                  disk. One entry per conversation:
//	                                  summary{Title, Preview, NumSteps,
//	                                  UpdatedAt, WorkspaceURIs} plus
//	                                  last_modified_time and is_internal.
//	conversation_summaries.db         the agent's SQLite index, one row per
//	                                  conversation (title, preview,
//	                                  step_count, last_modified_time,
//	                                  workspace_uris, ...).
//
// Measured against a live store at 220x30, REF prints the conversations that
// have both a .db on disk and steps — 159 rows, where the index has 164 rows
// with step_count > 0. Five of those index rows are synthetic (preview "x",
// conversation file absent) and REF does not list them, because the scan is
// file-driven: a row with no .db never enters the list. Cycle 278's 38-entry
// cache vs 168-row index measurement is consistent with the same rule — the
// 130 index-only rows had no files — and does not imply a cache-only list.
//
// Every column the picker draws comes from those indexes:
//
//	title           Title (an AI-written summary) with Preview (the raw first
//	                user input) as the fallback when Title is empty
//	project column  basename of the first WorkspaceURIs entry
//	step count      NumSteps / step_count
//	age             last_modified_time
//
// Reading the per-conversation files instead (the cycle-167 implementation,
// still the last-resort fallback) gives raw first-input titles — "/mouse of"
// where REF prints "Mouse Power Troubleshooting" — and no project column at
// all, because the workspace URI does not live in the conversation file.
func ListConversations() ([]ConversationMeta, error) {
	seen := make(map[string]bool)
	var out []ConversationMeta
	add := func(c ConversationMeta) {
		if c.ID == "" || seen[c.ID] {
			return
		}
		seen[c.ID] = true
		out = append(out, c)
	}

	cached, err := listFromCache()
	if err != nil {
		return nil, err
	}
	for _, c := range cached {
		add(c)
	}
	// Cycle 282: merge the disk, the way the reference's scan does. The index
	// is the cheap source for on-disk files the cache has never seen; files
	// with no index row are read directly. Without this, a conversation
	// written by another client (the wrapper's agy, or a build of the
	// reference that did not flush its cache) is invisible here even though
	// the reference lists it.
	indexed, hidden, err := scanSummaries()
	if err != nil {
		return nil, err
	}
	for _, c := range indexed {
		add(c)
	}
	unindexed, err := listFilesMissingFromIndex(seen, hidden)
	if err != nil {
		return nil, err
	}
	for _, c := range unindexed {
		add(c)
	}
	// No final listFromDir() sweep: the two merges already cover every
	// readable file, and a blind re-scan would resurrect the subagent rows
	// the index just hid.
	//
	// Newest first. The identity tie-break matters: entries whose timestamp
	// is zero (see the UpdatedAt note in listFromCache) all compare equal,
	// and REF orders those by id ascending rather than by map iteration.
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].ModTime.Equal(out[j].ModTime) {
			return out[i].ModTime.After(out[j].ModTime)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// listFromCache reads cache/conversation_metadata.json.
//
// Returns nil with no error when the file is absent or unparseable: it is a
// derived file, so its absence just means the agent has not flushed one yet
// and ListConversations should use the SQLite index instead.
func listFromCache() ([]ConversationMeta, error) {
	path := filepath.Join(StoreDir(), "cache", "conversation_metadata.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil
	}
	var cache struct {
		Conversations map[string]struct {
			Summary struct {
				Title         string   `json:"Title"`
				Preview       string   `json:"Preview"`
				NumSteps      int      `json:"NumSteps"`
				UpdatedAt     string   `json:"UpdatedAt"`
				WorkspaceURIs []string `json:"WorkspaceURIs"`
			} `json:"summary"`
			IsInternal       bool   `json:"is_internal"`
			LastModifiedTime string `json:"last_modified_time"`
		} `json:"conversations"`
	}
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, nil
	}
	dir := filepath.Join(StoreDir(), "conversations")
	var out []ConversationMeta
	for id, e := range cache.Conversations {
		if e.IsInternal || e.Summary.NumSteps <= 0 {
			continue
		}
		if id == "" || strings.ContainsAny(id, `/\`) {
			continue
		}
		// Cycle 278: the cache outlives the conversation files it
		// describes. A cache entry whose .db is gone cannot be resumed, so
		// listing it only produces "Failed to load <id>" on Enter (seen on
		// a store after the file was removed). The index path already
		// stat-checks; the cache path did not.
		p := filepath.Join(dir, id+".db")
		if _, err := os.Stat(p); err != nil {
			continue
		}
		// Cycle 278: the age comes from summary.UpdatedAt, NOT from
		// last_modified_time, and there is no fallback between them.
		// Verified against agy 1.4.2: a freshly written cache entry with
		// UpdatedAt "0001-01-01T00:00:00Z" and a real last_modified_time
		// renders with a BLANK age column in REF (and sorts to the bottom
		// of an UpdatedAt-descending list). Falling back to
		// last_modified_time printed "just now" on 68 rows REF leaves
		// blank.
		mod := parseSummaryTime(e.Summary.UpdatedAt)
		project := ""
		if len(e.Summary.WorkspaceURIs) > 0 {
			project = projectFromURIs(e.Summary.WorkspaceURIs[0])
		}
		out = append(out, ConversationMeta{
			ID:        id,
			Path:      filepath.Join(dir, id+".db"),
			Title:     conversationLabel(e.Summary.Title, e.Summary.Preview, id),
			StepCount: e.Summary.NumSteps,
			Project:   project,
			ModTime:   mod,
		})
	}
	return out, nil
}

// listFromSummaries reads conversation_summaries.db.
//
// Rows whose conversation file is not on disk are skipped: the index keeps
// rows for conversations that were never written or have since been deleted,
// and the picker could not resume them anyway.
//
// Returns nil with no error when the database is absent.
func listFromSummaries() ([]ConversationMeta, error) {
	metas, _, err := scanSummaries()
	return metas, err
}

// scanSummaries is listFromSummaries plus the set of conversations the index
// marks as hidden (subagent rows). The disk scan needs that set: a subagent
// file that the index filtered out would otherwise be re-added by the
// file walk, which does not see the parent link.
func scanSummaries() ([]ConversationMeta, map[string]bool, error) {
	hidden := map[string]bool{}
	path := filepath.Join(StoreDir(), "conversation_summaries.db")
	if _, err := os.Stat(path); err != nil {
		return nil, hidden, nil
	}
	db, err := Open(path)
	if err != nil {
		if isUnreadableStore(err) {
			return nil, hidden, nil
		}
		return nil, hidden, err
	}
	defer db.Close()

	dir := filepath.Join(StoreDir(), "conversations")
	var out []ConversationMeta
	serr := db.Scan("conversation_summaries", func(r *Row) error {
		steps := int(r.Int("step_count"))
		if steps <= 0 {
			return nil
		}
		// Cycle 282: subagent conversations never enter the reference's list
		// (its scan marks them internal and the picker filters those rows).
		// The index carries the parent link; drop them here too, including
		// on the merged path. Older stores have no such column — Row.Text
		// answers "" for a missing one.
		if r.Text("parent_conversation_id") != "" {
			if id := r.Text("conversation_id"); id != "" {
				hidden[id] = true
			}
			return nil
		}
		id := r.Text("conversation_id")
		if id == "" || strings.ContainsAny(id, `/\`) {
			return nil
		}
		p := filepath.Join(dir, id+".db")
		if _, err := os.Stat(p); err != nil {
			return nil
		}
		out = append(out, ConversationMeta{
			ID:        id,
			Path:      p,
			Title:     conversationLabel(r.Text("title"), r.Text("preview"), id),
			StepCount: steps,
			Project:   projectFromURIs(r.Text("workspace_uris")),
			ModTime:   parseSummaryTime(r.Text("last_modified_time")),
		})
		return nil
	})
	if serr != nil {
		return nil, hidden, serr
	}
	return out, hidden, nil
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

// listFilesMissingFromIndex handles the files the two indexes know nothing
// about: a .db written by another client while this store's cache and
// summaries table were both in place. The reference reaches them through
// listConversationsFromDir, which stats every file in the conversations
// directory; this is the same rule, one file at a time.
//
// The title is the annotation file's AI title when the annotation exists
// (annotations/<id>.pbtxt, `title:"..."` — that is what the reference's
// scan reads through cortex.AnnotationsManager), otherwise the first user
// input, exactly like the label chain the picker draws.
func listFilesMissingFromIndex(seen, hidden map[string]bool) ([]ConversationMeta, error) {
	dir := filepath.Join(StoreDir(), "conversations")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("convstore: list %s: %w", dir, err)
	}
	var out []ConversationMeta
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".db") {
			continue
		}
		id := strings.TrimSuffix(name, ".db")
		if id == "" || strings.ContainsAny(id, `/\`) || seen[id] || hidden[id] {
			continue
		}
		path := filepath.Join(dir, name)
		info, ierr := e.Info()
		if ierr != nil {
			continue
		}
		title, project, steps, lerr := peekConversation(path)
		if lerr != nil || steps == 0 {
			// Unreadable and empty stores are silently omitted, like the
			// reference's scan.
			continue
		}
		out = append(out, ConversationMeta{
			ID:        id,
			Path:      path,
			Title:     conversationLabel(annotationTitle(id), title, id),
			StepCount: steps,
			Project:   project,
			ModTime:   info.ModTime(),
		})
	}
	return out, nil
}

// annotationTitle reads the AI-written title the reference's scan picks up
// from annotations/<id>.pbtxt. Returns "" when the file is absent or the
// payload carries no title.
func annotationTitle(id string) string {
	data, err := os.ReadFile(filepath.Join(StoreDir(), "annotations", id+".pbtxt"))
	if err != nil {
		return ""
	}
	const key = `title:"`
	i := bytes.Index(data, []byte(key))
	if i < 0 {
		return ""
	}
	rest := data[i+len(key):]
	var b strings.Builder
	for j := 0; j < len(rest); j++ {
		c := rest[j]
		if c == '\\' && j+1 < len(rest) {
			j++
			switch rest[j] {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			default:
				b.WriteByte(rest[j])
			}
			continue
		}
		if c == '"' {
			break
		}
		b.WriteByte(c)
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// listFromDir is the cycle-167 fallback: scan conversations/*.db directly.
// It is used only when the summaries index is absent, and cannot recover the
// project column.
func listFromDir() ([]ConversationMeta, error) {
	dir := filepath.Join(StoreDir(), "conversations")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("convstore: list %s: %w", dir, err)
	}
	var out []ConversationMeta
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".db") {
			continue
		}
		// Strip ".db" for the ID; keep the original basename
		// for HasConversation (which expects <id>.db).
		id := strings.TrimSuffix(name, ".db")
		if id == "" || strings.ContainsAny(id, `/\`) {
			continue
		}
		path := filepath.Join(dir, name)
		info, ierr := e.Info()
		if ierr != nil {
			continue
		}
		mt := info.ModTime()
		title, project, steps, lerr := peekConversation(path)
		if lerr != nil {
			// Skip unreadable stores — matches the reference's
			// silent omission.
			continue
		}
		// Cycle 184: the reference's picker lists only conversations
		// that actually carry steps. The store keeps an empty .db for
		// every session that was opened and abandoned (84 of 233 on
		// the reference machine), and those have no title to show.
		if steps == 0 {
			continue
		}
		out = append(out, ConversationMeta{
			ID:        id,
			Path:      path,
			Title:     title,
			StepCount: steps,
			Project:   project,
			ModTime:   mt,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ModTime.After(out[j].ModTime)
	})
	return out, nil
}

// peekConversation opens path just enough to read the first
// user-input step's text (used as the picker title), the workspace
// project name, and the step count. Returns ("", "", 0, nil) for an
// empty or unreadable store — the same conditions LoadConversation
// tolerates.
func peekConversation(path string) (string, string, int, error) {
	c, err := LoadConversation(path)
	if err != nil {
		return "", "", 0, err
	}
	if c == nil || len(c.Steps) == 0 {
		return "", "", 0, nil
	}
	title := ""
	for _, s := range c.Steps {
		if s.Role() == "user" {
			t := strings.TrimSpace(s.Text)
			if t == "" {
				continue
			}
			// First line only, capped at 60 chars to fit the
			// picker row without truncation ellipsis noise.
			if i := strings.IndexAny(t, "\n\r"); i >= 0 {
				t = t[:i]
			}
			t = strings.Join(strings.Fields(t), " ")
			if len(t) > 60 {
				t = t[:57] + "..."
			}
			title = t
			break
		}
	}
	return title, c.Project, len(c.Steps), nil
}
