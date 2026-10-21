// Package remotecontrol — permissions_store.go: the
// JSON-file-backed PermissionStore implementation, plus the
// small in-memory MemPermissionStore tests use.
//
// Why two implementations in one file:
//
//   - FSPermissionStore is the production seam: one
//     permissions.json under
//     $ANTIGRAVITY_HOME/permissions.json. The atomic-rename
//     write matches convstore / antigravity-token / project
//     helpers' style.
//
//   - MemPermissionStore is the test seam: a state value
//     under a mutex. Tests inject this through
//     Config.Permissions so they don't need a tempdir + real
//     filesystem.
//
// Both share the same single-state storage shape; the wire
// envelope the dispatcher emits is identical regardless of
// which backend stored the state.
//
// File layout (cycle rc-filesize-internal-remotecontrol):
//
//   - permissions_store.go         — package doc +
//     MemPermissionStore (struct /
//     NewMemPermissionStore / Get /
//     SetPreset / SetReview /
//     AddGrants / RemoveGrants) +
//     clonePermissionState
//   - permissions_store_fs.go      — FSPermissionStore
//     (struct / constructor /
//     load / atomicWrite /
//     ensureParentDir /
//     defaultState / Get /
//     SetPreset / SetReview /
//     AddGrants / RemoveGrants)
//   - permissions_store_helpers.go — shared helpers
//     (validateAndAppendGrants /
//     filterGrantsByKeys /
//     sortGrantsStable)
//
// Stdlib-only. No third-party deps.
package remotecontrol

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// MemPermissionStore is the in-memory PermissionStore
// fixture tests inject. It is a thin state-value +
// mutex; the methods implement the PermissionStore
// interface exactly the way FSPermissionStore does,
// with the differences that:
//
//   - There is no filesystem involvement: Get on a
//     fresh MemPermissionStore returns the default
//     state (preset=default, review=always, grants=[])
//     unless the test pre-populated via AddGrants.
//
//   - It does not touch the clock — UpdatedAt and
//     AddedAt are stamped with the supplied now
//     function so tests can drive monotonic time.
//
// The fields are unexported; tests construct it via
// the zero value or via NewMemPermissionStore and call
// the methods directly.
type MemPermissionStore struct {
	mu    sync.RWMutex
	state PermissionState

	now func() time.Time
}

// NewMemPermissionStore returns a fresh, default-state
// MemPermissionStore. now is used to stamp UpdatedAt /
// AddedAt; nil → time.Now (UTC).
func NewMemPermissionStore(now func() time.Time) *MemPermissionStore {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &MemPermissionStore{
		state: PermissionState{
			Version:   PermissionStateVersion,
			Preset:    DefaultPermissionPreset,
			Review:    DefaultArtifactReviewMode,
			Grants:    []PermissionGrant{},
			UpdatedAt: now(),
		},
		now: now,
	}
}

// Get returns a defensive copy of the in-memory state.
func (m *MemPermissionStore) Get(ctx context.Context) (PermissionState, error) {
	_ = ctx
	m.mu.RLock()
	defer m.mu.RUnlock()
	return clonePermissionState(m.state), nil
}

// SetPreset sets the preset, bumps UpdatedAt, and
// returns the post-mutation state. Mirrors
// (*FSPermissionStore).SetPreset semantics.
func (m *MemPermissionStore) SetPreset(ctx context.Context, preset PermissionPreset) (PermissionState, error) {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state.Preset = preset
	m.state.UpdatedAt = m.now()
	return clonePermissionState(m.state), nil
}

// SetReview sets the artifact-review mode, bumps
// UpdatedAt, and returns the post-mutation state.
// Mirrors (*FSPermissionStore).SetReview semantics.
func (m *MemPermissionStore) SetReview(ctx context.Context, mode ArtifactReviewMode) (PermissionState, error) {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state.Review = mode
	m.state.UpdatedAt = m.now()
	return clonePermissionState(m.state), nil
}

// AddGrants appends grants; duplicates return
// ErrPermissionGrantExists. Mirrors
// (*FSPermissionStore).AddGrants semantics.
func (m *MemPermissionStore) AddGrants(ctx context.Context, grants []PermissionGrant) (PermissionState, error) {
	_ = ctx
	if len(grants) == 0 {
		m.mu.RLock()
		defer m.mu.RUnlock()
		return clonePermissionState(m.state), nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := validateAndAppendGrants(&m.state.Grants, grants, m.now(), nil); err != nil {
		return PermissionState{}, err
	}
	m.state.UpdatedAt = m.now()
	return clonePermissionState(m.state), nil
}

// RemoveGrants drops the grants whose key matches the
// supplied keys (parsed as "<tool_name>:<pattern>").
// Mirrors (*FSPermissionStore).RemoveGrants semantics.
func (m *MemPermissionStore) RemoveGrants(ctx context.Context, keys []string) (PermissionState, error) {
	_ = ctx
	if len(keys) == 0 {
		m.mu.RLock()
		defer m.mu.RUnlock()
		return clonePermissionState(m.state), nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	parsed, err := parseRemoveKeys(keys)
	if err != nil {
		return PermissionState{}, err
	}
	filterGrantsByKeys(&m.state.Grants, parsed)
	sortGrantsStable(m.state.Grants)
	m.state.UpdatedAt = m.now()
	return clonePermissionState(m.state), nil
}

// clonePermissionState returns a deep-copy of state so
// callers can't observe in-place mutations (and tests
// that compare pre/post snapshots don't share the same
// Grants slice).
func clonePermissionState(state PermissionState) PermissionState {
	out := state
	if state.Grants != nil {
		out.Grants = append([]PermissionGrant(nil), state.Grants...)
	}
	return out
}

// grantAppendIndex is the per-key dedup index the
// shared validateAndAppendGrants helper threads
// through. Pulled out so the helper signature stays
// tight and future callers can swap in a more
// efficient index without rewriting the call sites.
type grantAppendIndex struct {
	m map[string]struct{}
}

// newGrantAppendIndex returns a fresh index sized to
// hold `n` entries without rehashing.
func newGrantAppendIndex(n int) *grantAppendIndex {
	return &grantAppendIndex{m: make(map[string]struct{}, n)}
}

// seenOrMark returns (true, _) if key is already in
// the index; otherwise adds it and returns (false, _).
// The caller treats true as the duplicate-stop trigger and
// surfaces ErrPermissionGrantExists.
func (idx *grantAppendIndex) seenOrMark(key string) bool {
	if _, ok := idx.m[key]; ok {
		return true
	}
	idx.m[key] = struct{}{}
	return false
}

// parseRemoveKeys walks the supplied "<tool_name>:<pattern>"
// keys and returns the normalised form. Malformed keys
// surface as ErrPermissionGrantMalformed. Centralised
// so both stores agree on the malformed-envelope shape.
func parseRemoveKeys(keys []string) ([]string, error) {
	parsed := make([]string, 0, len(keys))
	for _, k := range keys {
		tool, pattern, ok := ParseToolPattern(k)
		if !ok {
			return nil, ErrPermissionGrantMalformed
		}
		parsed = append(parsed, tool+":"+pattern)
	}
	return parsed, nil
}

// sortGrantsStable sorts grants by AddedAt ascending
// with the GrantKey as a tiebreaker. Mirrors the
// projects store's "creation order" precedent so
// dashboards see a stable enumeration.
func sortGrantsStable(grants []PermissionGrant) {
	sort.SliceStable(grants, func(i, j int) bool {
		if grants[i].AddedAt.Equal(grants[j].AddedAt) {
			return grants[i].GrantKey() < grants[j].GrantKey()
		}
		return grants[i].AddedAt.Before(grants[j].AddedAt)
	})
}

// validateAndAppendGrants walks `grants`, normalises
// each entry, and appends it to *dest. Returns
// ErrPermissionGrantExists on the first duplicate
// (without mutating dest), or a typed error for an
// empty tool_name / pattern pair. `now` stamps
// AddedAt when the caller left it zero; `idx`
// short-circuits the duplicate check.
//
// Pulled out of the two stores' AddGrants so the
// dedup + validation rule is shared verbatim.
func validateAndAppendGrants(dest *[]PermissionGrant, grants []PermissionGrant, now time.Time, idx *grantAppendIndex) error {
	if idx == nil {
		idx = newGrantAppendIndex(len(*dest))
		for _, g := range *dest {
			idx.seenOrMark(g.GrantKey())
		}
	}
	for _, g := range grants {
		tool := normaliseToolName(g.ToolName)
		pattern := normalisePattern(g.Pattern)
		if tool == "" || pattern == "" {
			return fmt.Errorf("rc-permissions: grant requires non-empty tool_name and pattern")
		}
		key := tool + ":" + pattern
		if idx.seenOrMark(key) {
			return ErrPermissionGrantExists
		}
		added := g.AddedAt
		if added.IsZero() {
			added = now
		}
		*dest = append(*dest, PermissionGrant{
			ToolName: tool,
			Pattern:  pattern,
			Reason:   strings.TrimSpace(g.Reason),
			AddedAt:  added,
		})
	}
	return nil
}

// filterGrantsByKeys walks the supplied grants and
// re-slices *dest in place so the kept entries
// (those whose GrantKey is NOT in keys) are
// contiguous. The trailing capacity is preserved
// for the caller's defensive-copy step that follows.
//
// Pulled out of the two stores' RemoveGrants so the
// filter rule is shared verbatim.
func filterGrantsByKeys(dest *[]PermissionGrant, keys []string) {
	remove := make(map[string]struct{}, len(keys))
	for _, p := range keys {
		remove[p] = struct{}{}
	}
	kept := (*dest)[:0]
	for _, g := range *dest {
		if _, drop := remove[g.GrantKey()]; drop {
			continue
		}
		kept = append(kept, g)
	}
	// Re-slice so the JSON encoder omits the trailing
	// dropped entries (kept still aliases the
	// underlying array). The caller (RemoveGrants)
	// does the defensive copy that lands the surface
	// for dashboards.
	*dest = append([]PermissionGrant(nil), kept...)
}
