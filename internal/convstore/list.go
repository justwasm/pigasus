package convstore

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

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

	cached, err := listFromCache()
	if err != nil {
		return nil, err
	}
	out = appendUniqueMetas(out, seen, cached)

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
	out = appendUniqueMetas(out, seen, indexed)

	unindexed, err := listFilesMissingFromIndex(seen, hidden)
	if err != nil {
		return nil, err
	}
	out = appendUniqueMetas(out, seen, unindexed)

	// No final listFromDir() sweep: the two merges already cover every
	// readable file, and a blind re-scan would resurrect the subagent rows
	// the index just hid.
	//
	// Newest first. The identity tie-break matters: entries whose timestamp
	// is zero (see the UpdatedAt note in listFromCache) all compare equal,
	// and REF orders those by id ascending rather than by map iteration.
	sortConversations(out)
	return out, nil
}

func appendUniqueMetas(out []ConversationMeta, seen map[string]bool, items []ConversationMeta) []ConversationMeta {
	for _, c := range items {
		if c.ID != "" && !seen[c.ID] {
			seen[c.ID] = true
			out = append(out, c)
		}
	}
	return out
}

func sortConversations(out []ConversationMeta) {
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].ModTime.Equal(out[j].ModTime) {
			return out[i].ModTime.After(out[j].ModTime)
		}
		return out[i].ID < out[j].ID
	})
}

type cacheConvSummary struct {
	Title         string   `json:"Title"`
	Preview       string   `json:"Preview"`
	NumSteps      int      `json:"NumSteps"`
	UpdatedAt     string   `json:"UpdatedAt"`
	WorkspaceURIs []string `json:"WorkspaceURIs"`
}

type cacheConvEntry struct {
	Summary          cacheConvSummary `json:"summary"`
	IsInternal       bool             `json:"is_internal"`
	LastModifiedTime string           `json:"last_modified_time"`
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
		Conversations map[string]cacheConvEntry `json:"conversations"`
	}
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, nil
	}
	dir := filepath.Join(StoreDir(), "conversations")
	var out []ConversationMeta
	for id, e := range cache.Conversations {
		if meta, ok := parseCacheEntry(dir, id, e); ok {
			out = append(out, meta)
		}
	}
	return out, nil
}

func parseCacheEntry(dir, id string, e cacheConvEntry) (ConversationMeta, bool) {
	if e.IsInternal || e.Summary.NumSteps <= 0 {
		return ConversationMeta{}, false
	}
	if id == "" || strings.ContainsAny(id, `/\`) {
		return ConversationMeta{}, false
	}
	// Cycle 278: the cache outlives the conversation files it
	// describes. A cache entry whose .db is gone cannot be resumed, so
	// listing it only produces "Failed to load <id>" on Enter (seen on
	// a store after the file was removed). The index path already
	// stat-checks; the cache path did not.
	p := filepath.Join(dir, id+".db")
	if _, err := os.Stat(p); err != nil {
		return ConversationMeta{}, false
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
	return ConversationMeta{
		ID:        id,
		Path:      p,
		Title:     conversationLabel(e.Summary.Title, e.Summary.Preview, id),
		StepCount: e.Summary.NumSteps,
		Project:   project,
		ModTime:   mod,
	}, true
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
