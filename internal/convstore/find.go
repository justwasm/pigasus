package convstore

// Workspace-based conversation search.
//
// Why this file exists
// --------------------
// Cycle resume-continuation. The v1.4.0 changelog entry for `--continue`
// reads (verbatim, recovered from the reference's CHANGELOG):
//
//	"Fixed `--continue` starting a brand-new conversation when launched
//	 from a subdirectory, after a crash, or while another session is open
//	 in the same workspace; it now falls back to the most recent non-empty
//	 conversation in the current workspace or its parent/child directories."
//
// The pre-cycle-resume lookup in cmd/wagyu/conversation.go reads only the
// ref's `cache/last_conversations.json` and `cache/conversation_metadata.json`
// — both keyed by absolute path — and walks parents only, never children.
//
// This file adds two new primitives:
//
//   1. FindMostRecent — a single-workspace / parent / child walk that
//      returns the most recently active workspace candidate starting at
//      `start`. Used by `--continue` and the auto-resume prompt.
//
//   2. MostRecentInWorkspace — a one-workspace picker that returns the
//      most recently modified non-empty conversation whose recorded
//      workspace_uri matches the directory.
//
// Both run off the existing public index/db/cache machinery
// (ListConversations / HasConversation / ConversationPath), so they read
// the same data the /resume panel reads and stay consistent when the user
// runs `agy --continue` after seeing a row in the picker.
//
// Why a separate file (no edit to sqlite.go / list.go / write.go)
// -----------------------------------------------------------------
// The convstore package files are owned by other cycles: sqlite.go is on
// the cycle-175 read-only path, list.go on the cycle-184 picker path,
// write.go on the cycle-167 /fork write path, and agy.go on the cycle-163
// schema-recovery path. Cross-cutting additions live in their own file to
// keep ownership explicit. The bodies below call into ListConversations
// and HasConversation — both public, both cycle-184 — rather than touch
// sqlite.go's internals.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// WorkspaceCandidate is a directory the workspace search identified as
// owning at least one conversation in the store. LastUsed is the most
// recent modification time among that directory's conversations.
type WorkspaceCandidate struct {
	Dir       string    // absolute, cleaned path
	ConvCount int       // number of non-empty conversations recorded under this dir
	LastUsed  time.Time // max(last_modified_time) across those conversations
}

// FindMostRecent resolves the workspace `--continue` (or auto-resume) would
// pick, starting at `start` and walking parents first (up to `maxDepth`
// levels), then one level of immediate children. Returns (nil, nil) when no
// non-empty conversation exists anywhere under start.
//
// The order is exactly what the changelog prescribes:
//  1. `start` itself.
//  2. Each ancestor of `start`, walking toward "/" (max `maxDepth`).
//  3. If still nothing, immediate children of `start` (one level).
//
// `start` may be relative; it is expanded with filepath.Abs before the walk.
// `maxDepth` of 0 means "current directory only, no parent walk". The child
// walk is always one level deep regardless of maxDepth — it is the
// changelog's literal "child directories" fallback, not a recursive tree.
func FindMostRecent(start string, maxDepth int) (*WorkspaceCandidate, error) {
	if start == "" {
		return nil, errors.New("convstore: FindMostRecent: empty start")
	}
	abs, err := filepath.Abs(start)
	if err != nil {
		return nil, fmt.Errorf("convstore: FindMostRecent: %w", err)
	}
	abs = filepath.Clean(abs)
	if maxDepth < 0 {
		maxDepth = 0
	}

	cand, err := scanWorkspace(abs)
	if err != nil || cand != nil {
		return cand, err
	}

	cand, err = findAncestorCandidate(abs, maxDepth)
	if err != nil || cand != nil {
		return cand, err
	}

	return findImmediateChildCandidate(abs)
}

func findAncestorCandidate(abs string, maxDepth int) (*WorkspaceCandidate, error) {
	dir := abs
	for i := 0; i < maxDepth; i++ {
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
		cand, err := scanWorkspace(dir)
		if err != nil || cand != nil {
			return cand, err
		}
	}
	return nil, nil
}

func findImmediateChildCandidate(abs string) (*WorkspaceCandidate, error) {
	entries, err := os.ReadDir(abs)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrPermission) {
			return nil, nil
		}
		return nil, fmt.Errorf("convstore: FindMostRecent: read %s: %w", abs, err)
	}
	var best *WorkspaceCandidate
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		child, err := scanWorkspace(filepath.Join(abs, e.Name()))
		if err != nil {
			return nil, err
		}
		if child == nil {
			continue
		}
		if best == nil || child.LastUsed.After(best.LastUsed) ||
			(child.LastUsed.Equal(best.LastUsed) && child.Dir < best.Dir) {
			best = child
		}
	}
	return best, nil
}

// MostRecentInWorkspace returns the most recently modified non-empty
// conversation whose recorded workspace_uri is `dir`. Returns "" when no
// such conversation exists. The lookup is the heart of `--continue`'s
// "exact workspace" branch — the parent/child walk above only runs when
// this returns "".
func MostRecentInWorkspace(dir string) string {
	if dir == "" {
		return ""
	}
	want, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}
	want = filepath.Clean(want)
	wantURI := "file://" + want
	wantBase := filepath.Base(want)

	rows := listAll()
	// Load both workspace indexes exactly once — the per-row matcher below
	// reads them for every conversation otherwise, which made startup
	// O(N×M): once upon a time N picker rows × M cache entries (~2.1 MB on
	// a long-running install), and more recently N rows × the whole
	// conversation_summaries table plus one SQLite open per row. On an
	// install with 1.1 K conversations that pinned a CPU core for ~55 s
	// before the first TUI frame. Loaded unconditionally so the
	// once-per-scan invariant is testable even when the picker is empty;
	// the file-read cost is at most one 2 MB read and one table scan and
	// is only paid when this function is invoked.
	uris := loadWorkspaceURIsFromCacheFn()
	indexURIs := loadWorkspaceURIsFromIndexFn()
	if len(rows) == 0 {
		return ""
	}
	var bestID string
	var bestTime time.Time
	for _, c := range rows {
		if !rowMatchesWorkspace(c, wantURI, want, wantBase, uris, indexURIs) {
			continue
		}
		if c.ModTime.IsZero() {
			continue
		}
		if bestID == "" || c.ModTime.After(bestTime) {
			bestID = c.ID
			bestTime = c.ModTime
		}
	}
	return bestID
}

// listAll returns every conversation the picker would show — the same
// data ListConversations produces. We re-implement the loop inline so we
// can both filter by workspace and rank by mtime in one pass; routing
// through ListConversations would force a second scan.
func listAll() []ConversationMeta {
	all, err := ListConversations()
	if err != nil {
		return nil
	}
	return all
}

// rowMatchesWorkspace reports whether a /resume picker row matches a
// requested workspace directory. Three checks, in order:
//
//  1. The row carries a WorkspaceURIs array (cache/conversation_metadata.json
//     carried it via listFromCache) whose first entry equals the
//     absolute URI of `want`.
//  2. The conversation_summaries.db index row carries workspace_uris
//     whose first entry matches `want`'s absolute URI. (This is the
//     path the cycle-167 /fork writer produces.)
//  3. The row's Project equals want's basename — last-resort fallback for
//     the cycle-167 listFromDir path which does NOT record
//     WorkspaceURIs.
//
// Symlink-tolerant comparison: an EvalSymlinks resolution of both paths
// bridges the ~/GitHub -> ~/Documents/GitHub case the picker's cell-
// blanking rule (cycle 184) already documents.
func rowMatchesWorkspace(c ConversationMeta, wantURI, wantPath, wantBase string, uris, indexURIs map[string][]string) bool {
	if rowMatchesCacheURI(c.ID, wantURI, wantPath, uris) {
		return true
	}
	if rowMatchesIndexURI(c.ID, wantURI, wantPath, indexURIs) {
		return true
	}
	if c.Project != "" && c.Project == wantBase {
		return true
	}
	return false
}

// loadWorkspaceURIsFromCacheFn is the test seam for swapping in a stub
// (see internal/convstore/find_test.go). Production code reads through
// the function value, so swapping it in a test gives an exact call
// count without depending on the disk state.
var loadWorkspaceURIsFromCacheFn = loadWorkspaceURIsFromCache

// loadWorkspaceURIsFromCache reads cache/conversation_metadata.json once
// and returns the per-conversation WorkspaceURIs slice indexed by
// conversation id. A nil result means "missing or unreadable" — callers
// treat it the same as an empty map, falling back to the other matchers.
//
// The cache JSON on a long-running install grows to ~2 MB; parsing it on
// every per-row match made `MostRecentInWorkspace` and `scanWorkspace`
// O(N×M) and pinned a CPU core at 100 % at startup. Callers must
// invoke this helper exactly once per scan and pass the result down.
func loadWorkspaceURIsFromCache() map[string][]string {
	path := filepath.Join(StoreDir(), "cache", "conversation_metadata.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var cache struct {
		Conversations map[string]struct {
			Summary struct {
				WorkspaceURIs []string `json:"WorkspaceURIs"`
			} `json:"summary"`
		} `json:"conversations"`
	}
	if json.Unmarshal(data, &cache) != nil {
		return nil
	}
	out := make(map[string][]string, len(cache.Conversations))
	for id, entry := range cache.Conversations {
		if len(entry.Summary.WorkspaceURIs) > 0 {
			out[id] = entry.Summary.WorkspaceURIs
		}
	}
	return out
}

// rowMatchesCacheURI looks up the conversation's WorkspaceURIs array in
// the pre-parsed cache map (see loadWorkspaceURIsFromCache), comparing
// to wantURI literally and after EvalSymlinks. A nil uris map is
// treated as "no cache entries" and falls through to the other matchers.
func rowMatchesCacheURI(id, wantURI, wantPath string, uris map[string][]string) bool {
	if len(uris) == 0 {
		return false
	}
	entry, ok := uris[id]
	if !ok || len(entry) == 0 {
		return false
	}
	for _, uri := range entry {
		uri = strings.TrimSuffix(uri, "/")
		if uri == wantURI {
			return true
		}
		if ru, err := filepath.EvalSymlinks(strings.TrimPrefix(uri, "file://")); err == nil {
			if rw, err := filepath.EvalSymlinks(wantPath); err == nil {
				if filepath.Clean(ru) == filepath.Clean(rw) {
					return true
				}
			}
		}
	}
	return false
}

// rowMatchesIndexURI looks up the conversation's workspace_uris array in
// the pre-parsed index map (see loadWorkspaceURIsFromIndex) and compares
// each URI against wantURI. A nil map is treated as "no index entries"
// and falls through to the caller's other matchers.
func rowMatchesIndexURI(id, wantURI, wantPath string, indexURIs map[string][]string) bool {
	if len(indexURIs) == 0 {
		return false
	}
	for _, u := range indexURIs[id] {
		u = strings.TrimSuffix(u, "/")
		if u == wantURI {
			return true
		}
		if ru, err := filepath.EvalSymlinks(strings.TrimPrefix(u, "file://")); err == nil {
			if rw, err := filepath.EvalSymlinks(wantPath); err == nil {
				if filepath.Clean(ru) == filepath.Clean(rw) {
					return true
				}
			}
		}
	}
	return false
}

// loadWorkspaceURIsFromIndexFn is the test seam for swapping in a stub
// (see internal/convstore/find_test.go). Production code reads through
// the function value, so swapping it in a test gives an exact call
// count without depending on the disk state.
var loadWorkspaceURIsFromIndexFn = loadWorkspaceURIsFromIndex

// loadWorkspaceURIsFromIndex reads conversation_summaries.db once and
// returns the per-conversation workspace_uris array indexed by
// conversation id. A nil result means "missing or unreadable" — callers
// treat it the same as an empty map, falling back to the other matchers.
//
// This replaces the previous per-row implementation, which opened the
// database and scanned the whole table once for every picker row. On a
// long-running install that is O(N×M) SQLite work before the first TUI
// frame (~55 s for 1.1 K conversations); one open + one scan is O(N).
func loadWorkspaceURIsFromIndex() map[string][]string {
	indexPath := filepath.Join(StoreDir(), "conversation_summaries.db")
	db, err := Open(indexPath)
	if err != nil {
		return nil
	}
	defer db.Close()
	out := make(map[string][]string)
	_ = db.Scan("conversation_summaries", func(r *Row) error {
		id := r.Text("conversation_id")
		if id == "" {
			return nil
		}
		if uris := splitWorkspaceURIs(r.Text("workspace_uris")); len(uris) > 0 {
			out[id] = uris
		}
		return nil
	})
	return out
}

// splitWorkspaceURIs parses the workspace_uris column, which the index
// stores as a JSON array of one URI (the reference's cycle-184 case).
// Falls back to the bare URI if the column is not a JSON array.
func splitWorkspaceURIs(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if strings.HasPrefix(raw, "[") {
		var arr []string
		if json.Unmarshal([]byte(raw), &arr) == nil {
			return arr
		}
	}
	return []string{raw}
}

// scanWorkspace inspects the conversation index for `dir` and returns a
// WorkspaceCandidate when at least one non-empty conversation is recorded
// against it. Used by FindMostRecent to decide whether the directory
// "owns" a conversation worth resuming.
//
// The implementation calls the public ListConversations and reuses its
// already-decoded Project + WorkspaceURIs plumbing — convstore's
// internal row readers do not scan these without going through list.go's
// helpers. Returning (nil, nil) means "no conversation recorded under
// this directory"; an error is reserved for read failures.
func scanWorkspace(dir string) (*WorkspaceCandidate, error) {
	if dir == "" {
		return nil, nil
	}
	want, err := filepath.Abs(dir)
	if err != nil {
		return nil, nil
	}
	want = filepath.Clean(want)
	wantURI := "file://" + want
	wantBase := filepath.Base(want)

	rows := listAll()
	uris := loadWorkspaceURIsFromCacheFn()
	indexURIs := loadWorkspaceURIsFromIndexFn()
	if len(rows) == 0 {
		return nil, nil
	}
	count := 0
	var latest time.Time
	for _, c := range rows {
		if !rowMatchesWorkspace(c, wantURI, want, wantBase, uris, indexURIs) {
			continue
		}
		count++
		if c.ModTime.After(latest) {
			latest = c.ModTime
		}
	}
	if count == 0 {
		return nil, nil
	}
	return &WorkspaceCandidate{Dir: want, ConvCount: count, LastUsed: latest}, nil
}
