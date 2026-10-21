// Package remotecontrol —
// conversation_config_store_fs.go: the
// FSConversationConfigStore implementation. The
// production seam that writes
// $ANTIGRAVITY_HOME/conversation_config.json on every
// mutation.
//
// Why a sibling file (not the parent
// conversation_config_store.go): the FS path pulls in
// encoding/json / os / path/filepath / fmt — the
// Mem variant below doesn't need any of that — and
// each implementation is naturally ~200 lines. Split
// to keep both files comfortably under the 500-line
// ceiling that cycle
// rc-filesize-internal-remotecontrol enforces.
//
// Atomicity: every write goes through atomicWrite
// (tempfile + rename + best-effort dir fsync). Read
// paths (Snapshot / Get) take the read lock so a
// write in flight doesn't hand back a half-mutated
// state.
//
// Stdlib-only. No third-party deps.
package remotecontrol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// NewFSConversationConfigStore returns a fresh
// FSConversationConfigStore rooted at path. The
// store's lazy-on-first-mutation pattern means path
// may not exist on return; the directory is created
// on the first write.
//
// When now is nil, the store uses time.Now (UTC).
// The returned store has no schema check beyond
// what load() enforces (version > supported ⇒
// error).
func NewFSConversationConfigStore(path string, now func() time.Time) *FSConversationConfigStore {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &FSConversationConfigStore{Path: path, now: now}
}

// ensureParentDir creates the parent directory of
// FSConversationConfigStore.Path if it doesn't
// exist. Single-shot (sync.Once) so concurrent
// first-use calls don't race on the MkdirAll.
func (s *FSConversationConfigStore) ensureParentDir() error {
	var err error
	s.ensureDirOnce.Do(func() {
		dir := filepath.Dir(s.Path)
		if dir == "" || dir == "." {
			return
		}
		err = os.MkdirAll(dir, 0o700)
	})
	return err
}

// load reads the on-disk state. Returns:
//
//   - (zero, false, nil) — no file exists (a
//     fresh host). The caller hands back the
//     empty state;
//   - (state, true, nil) — the file existed and
//     parsed cleanly;
//   - (state, true, err) — the file existed but
//     is malformed; the dispatcher maps this to
//     -32603 because the dashboard should not
//     see "no conversation configs" when the JSON
//     is corrupt (the operator needs to know).
func (s *FSConversationConfigStore) load() (ConversationConfigState, bool, error) {
	data, err := os.ReadFile(s.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ConversationConfigState{}, false, nil
		}
		return ConversationConfigState{}, false, err
	}
	var rec onDiskConversationConfigState
	if err := json.Unmarshal(data, &rec); err != nil {
		// Malformed file. Bubble the parse error
		// up so the dispatcher surfaces it; the
		// operator sees "conversation_config.json
		// is corrupt" in the wire envelope
		// instead of a silent fallback to empty
		// state.
		return ConversationConfigState{}, true, fmt.Errorf("rc-conversation-config: parse %s: %w", s.Path, err)
	}
	// Coerce a missing version to v1 (the
	// on-disk layout before the version stamp
	// existed). A reader that finds a version >
	// ConversationConfigStateVersion refuses to
	// touch the file (returns a "newer binary
	// required" envelope).
	if rec.Version == 0 {
		rec.Version = ConversationConfigStateVersion
	}
	if rec.Version > ConversationConfigStateVersion {
		return ConversationConfigState{}, true, fmt.Errorf("rc-conversation-config: %s version=%d is newer than supported %d", s.Path, rec.Version, ConversationConfigStateVersion)
	}
	if rec.Configs == nil {
		// Normalise nil → empty map so the wire
		// emits "{}" rather than "null".
		rec.Configs = map[string]ConversationConfig{}
	}
	return ConversationConfigState{
		Version:   rec.Version,
		Configs:   rec.Configs,
		UpdatedAt: rec.UpdatedAt,
	}, true, nil
}

// atomicWrite writes the post-mutation state to
// disk. Temp file + fsync-via-Rename gives
// crash-consistent semantics; the directory is
// fsynced too (matches antigravity-token's style).
func (s *FSConversationConfigStore) atomicWrite(state ConversationConfigState) error {
	if err := s.ensureParentDir(); err != nil {
		return err
	}
	rec := onDiskConversationConfigState{
		Version:   ConversationConfigStateVersion,
		Configs:   state.Configs,
		UpdatedAt: state.UpdatedAt,
	}
	if rec.Configs == nil {
		rec.Configs = map[string]ConversationConfig{}
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.Path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.Path); err != nil {
		return err
	}
	// Best-effort dir fsync: ensures the rename
	// is durable across a power loss.
	// Best-effort because a platform-level fsync on
	// the directory may not be available — we
	// tolerate the failure silently (matching
	// antigravity-token's style).
	if d := filepath.Dir(s.Path); d != "" && d != "." {
		if f, ferr := os.Open(d); ferr == nil {
			_ = f.Sync()
			_ = f.Close()
		}
	}
	return nil
}

// emptyState returns the fresh-host state: an
// empty configs map, version=1, and the wall-clock
// UpdatedAt. The store hands this back when
// Snapshot finds no file.
func (s *FSConversationConfigStore) emptyState() ConversationConfigState {
	return ConversationConfigState{
		Version:   ConversationConfigStateVersion,
		Configs:   map[string]ConversationConfig{},
		UpdatedAt: s.now(),
	}
}

// Snapshot returns the current state. No file →
// the empty state (version=1, configs={}). A
// malformed file bubbles the parse error up.
func (s *FSConversationConfigStore) Snapshot(ctx context.Context) (ConversationConfigState, error) {
	_ = ctx
	s.mu.RLock()
	defer s.mu.RUnlock()
	state, exists, err := s.load()
	if err != nil {
		return ConversationConfigState{}, err
	}
	if !exists {
		state = s.emptyState()
	}
	return cloneConversationConfigState(state), nil
}

// Upsert writes the supplied config for convID
// and returns whether the row was newly created
// (true) or replaced (false). The store stamps
// UpdatedAt server-side so a dashboard that posts
// a pre-populated value gets the value overwritten
// with the server's clock (matches the
// rc-fastpush-toggle / rc-resources precedent —
// the brief pins server-side stamping).
//
// The read-modify-write runs under s.mu so
// concurrent Upsert calls serialise. A Upsert's
// read snap and write commit happen under the same
// lock; Snapshot / Get read paths take the read
// lock so they don't race a writer's in-progress
// mutation.
//
// The map lookup uses normaliseConversationID so
// a whitespace-prefixed id matches the stored
// row. A blank id (after trim) returns
// ErrConversationConfigPersistenceFailed — the
// dispatcher's primary validation site is the
// "empty conversation_id" branch in
// handleUpsertRemoteAgentConversationConfig
// (-32602 "Invalid params"), but the store's
// defence-in-depth guard catches a non-dispatcher
// caller.
func (s *FSConversationConfigStore) Upsert(ctx context.Context, convID string, c ConversationConfig) (bool, error) {
	_ = ctx
	convID = normaliseConversationID(convID)
	if convID == "" {
		return false, ErrConversationConfigPersistenceFailed
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state, exists, err := s.load()
	if err != nil {
		return false, err
	}
	if !exists {
		state = s.emptyState()
	}
	if state.Configs == nil {
		state.Configs = map[string]ConversationConfig{}
	}
	_, created := state.Configs[convID]
	now := s.now()
	c.UpdatedAt = now
	state.Configs[convID] = c
	state.UpdatedAt = now
	if err := s.atomicWrite(state); err != nil {
		return false, err
	}
	return !created, nil
}

// Get returns the config for convID. Unknown
// conversation_id → (zero, false). A
// conversation that has no stored row returns the
// zero-valued ConversationConfig with no error —
// callers that need a strict "has it been
// touched?" answer can compare UpdatedAt.IsZero().
func (s *FSConversationConfigStore) Get(ctx context.Context, convID string) (ConversationConfig, bool) {
	_ = ctx
	convID = normaliseConversationID(convID)
	if convID == "" {
		return ConversationConfig{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	state, exists, err := s.load()
	if err != nil {
		return ConversationConfig{}, false
	}
	if !exists {
		return ConversationConfig{}, false
	}
	c, ok := state.Configs[convID]
	if !ok {
		return ConversationConfig{}, false
	}
	return cloneConversationConfig(c), true
}
