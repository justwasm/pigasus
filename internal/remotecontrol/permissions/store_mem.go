// Package permissions — store_mem.go: the
// JSON-file-backed Store implementation, plus the
// small in-memory MemStore tests use.
//
// Why two implementations in one file:
//
//   - FSStore is the production seam: one
//     permissions.json under
//     $ANTIGRAVITY_HOME/permissions.json. The atomic-rename
//     write matches convstore / antigravity-token / project
//     helpers' style.
//
//   - MemStore is the test seam: a state value
//     under a mutex. Tests inject this through
//     Config.PermissionsStore so they don't need a tempdir + real
//     filesystem.
//
// Both share the same single-state storage shape; the wire
// envelope the dispatcher emits is identical regardless of
// which backend stored the state.
//
// File layout (cycle rc-filesize-internal-remotecontrol):
//
//   - store_mem.go                — package doc +
//     MemStore (struct /
//     NewMemStore / Get /
//     SetPreset / SetReview /
//     AddGrants / RemoveGrants) +
//     cloneState
//   - store_fs.go                 — FSStore
//     (struct / constructor /
//     load / atomicWrite /
//     ensureParentDir /
//     defaultState / Get /
//     SetPreset / SetReview /
//     AddGrants / RemoveGrants)
//   - store_mem.go (this file) also carries the shared
//     helpers (validateAndAppendGrants /
//     filterGrantsByKeys /
//     sortGrantsStable)
//
// Stdlib-only. No third-party deps.
package permissions

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// MemStore is the in-memory Store
// fixture tests inject. It is a thin state-value +
// mutex; the methods implement the Store
// interface exactly the way FSStore does,
// with the differences that:
//
//   - There is no filesystem involvement: Get on a
//     fresh MemStore returns the default
//     state (preset=default, review=always, grants=[])
//     unless the test pre-populated via AddGrants.
//
//   - It does not touch the clock — UpdatedAt and
//     AddedAt are stamped with the supplied now
//     function so tests can drive monotonic time.
//
// The fields are unexported; tests construct it via
// the zero value or via NewMemStore and call
// the methods directly.
type MemStore struct {
	mu    sync.RWMutex
	state State

	now func() time.Time
}

// NewMemStore returns a fresh, default-state
// MemStore. now is used to stamp UpdatedAt /
// AddedAt; nil → time.Now (UTC).
func NewMemStore(now func() time.Time) *MemStore {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &MemStore{
		state: State{
			Version:   StateVersion,
			Preset:    DefaultPreset,
			Review:    DefaultReviewMode,
			Grants:    []Grant{},
			UpdatedAt: now(),
		},
		now: now,
	}
}

// Get returns a defensive copy of the in-memory state.
func (m *MemStore) Get(ctx context.Context) (State, error) {
	_ = ctx
	m.mu.RLock()
	defer m.mu.RUnlock()
	return cloneState(m.state), nil
}

// SetPreset sets the preset, bumps UpdatedAt, and
// returns the post-mutation state. Mirrors
// (*FSStore).SetPreset semantics.
func (m *MemStore) SetPreset(ctx context.Context, preset Preset) (State, error) {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state.Preset = preset
	m.state.UpdatedAt = m.now()
	return cloneState(m.state), nil
}

// SetReview sets the artifact-review mode, bumps
// UpdatedAt, and returns the post-mutation state.
// Mirrors (*FSStore).SetReview semantics.
func (m *MemStore) SetReview(ctx context.Context, mode ReviewMode) (State, error) {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state.Review = mode
	m.state.UpdatedAt = m.now()
	return cloneState(m.state), nil
}

// AddGrants appends grants; duplicates return
// ErrGrantExists. Mirrors
// (*FSStore).AddGrants semantics.
func (m *MemStore) AddGrants(ctx context.Context, grants []Grant) (State, error) {
	_ = ctx
	if len(grants) == 0 {
		m.mu.RLock()
		defer m.mu.RUnlock()
		return cloneState(m.state), nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := validateAndAppendGrants(&m.state.Grants, grants, m.now(), nil); err != nil {
		return State{}, err
	}
	m.state.UpdatedAt = m.now()
	return cloneState(m.state), nil
}

// RemoveGrants drops the grants whose key matches the
// supplied keys — the reference's "action(target)" grant or
// the legacy "<tool_name>:<pattern>" one.
// Mirrors (*FSStore).RemoveGrants semantics.
func (m *MemStore) RemoveGrants(ctx context.Context, keys []string) (State, error) {
	_ = ctx
	if len(keys) == 0 {
		m.mu.RLock()
		defer m.mu.RUnlock()
		return cloneState(m.state), nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	parsed, err := parseRemoveKeys(keys)
	if err != nil {
		return State{}, err
	}
	filterGrantsByKeys(&m.state.Grants, parsed)
	sortGrantsStable(m.state.Grants)
	m.state.UpdatedAt = m.now()
	return cloneState(m.state), nil
}

// cloneState returns a deep-copy of state so
// callers can't observe in-place mutations (and tests
// that compare pre/post snapshots don't share the same
// Grants slice).
func cloneState(state State) State {
	out := state
	if state.Grants != nil {
		out.Grants = append([]Grant(nil), state.Grants...)
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
// surfaces ErrGrantExists.
func (idx *grantAppendIndex) seenOrMark(key string) bool {
	if _, ok := idx.m[key]; ok {
		return true
	}
	idx.m[key] = struct{}{}
	return false
}

// parseRemoveKeys walks the supplied grant keys and returns
// the normalised dedup keys. Two forms are accepted:
//
//   - the reference's "action(target)" — the grant form;
//   - the clone's legacy "<tool_name>:<pattern>".
//
// Both normalise to the same key the store files a grant
// under (Grant.GrantKey), so a grant added as
// "command(git status)" is removed by the same spelling.
//
// Malformed keys surface as ErrGrantMalformed. Centralised
// so both stores agree on the malformed-envelope shape.
func parseRemoveKeys(keys []string) ([]string, error) {
	parsed := make([]string, 0, len(keys))
	for _, k := range keys {
		g, err := ParseGrantKey(k)
		if err != nil {
			return nil, ErrGrantMalformed
		}
		parsed = append(parsed, g.GrantKey())
	}
	return parsed, nil
}

// sortGrantsStable sorts grants by AddedAt ascending
// with the GrantKey as a tiebreaker. Mirrors the
// projects store's "creation order" precedent so
// dashboards see a stable enumeration.
func sortGrantsStable(grants []Grant) {
	sort.SliceStable(grants, func(i, j int) bool {
		if grants[i].AddedAt.Equal(grants[j].AddedAt) {
			return grants[i].GrantKey() < grants[j].GrantKey()
		}
		return grants[i].AddedAt.Before(grants[j].AddedAt)
	})
}

// validateAndAppendGrants walks `grants`, normalises
// each entry, and appends it to *dest. Returns
// ErrGrantExists on the first duplicate
// (without mutating dest), or a typed error for an
// empty action / tool_name / pattern pair. `now` stamps
// AddedAt when the caller left it zero; `idx`
// short-circuits the duplicate check.
//
// Two grant forms are accepted:
//
//   - the reference's "action(target)" — the grant form;
//   - the clone's legacy "tool_name:pattern".
//
// The dedup key is the grant's own (Grant.GrantKey), so
// the same rule is never stored twice under two spellings.
//
// Pulled out of the two stores' AddGrants so the
// dedup + validation rule is shared verbatim.
func validateAndAppendGrants(dest *[]Grant, grants []Grant, now time.Time, idx *grantAppendIndex) error {
	if idx == nil {
		idx = newGrantAppendIndex(len(*dest))
		for _, g := range *dest {
			idx.seenOrMark(g.GrantKey())
		}
	}
	for _, g := range grants {
		action := normaliseAction(g.Action)
		tool := normaliseToolName(g.ToolName)
		pattern := normalisePattern(g.Pattern)
		if action != "" {
			// The reference's own form. The action is validated
			// against the closed set, and the refusal is the
			// reference's own sentence — the retired action has its
			// own sentence, not the unknown-action one.
			if action == ActionUnsandboxed {
				return ErrGrantUnsandboxed
			}
			if !ValidGrantAction(action) {
				return ErrUnknownGrantAction(action, grantKeyFor(action, tool, pattern))
			}
			if pattern == "" {
				return fmt.Errorf("rc-permissions: grant requires non-empty action and pattern")
			}
		} else {
			if tool == "" || pattern == "" {
				return fmt.Errorf("rc-permissions: grant requires non-empty tool_name and pattern")
			}
		}
		key := grantKeyFor(action, tool, pattern)
		if idx.seenOrMark(key) {
			return ErrGrantExists
		}
		added := g.AddedAt
		if added.IsZero() {
			added = now
		}
		*dest = append(*dest, Grant{
			Action:   action,
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
func filterGrantsByKeys(dest *[]Grant, keys []string) {
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
	*dest = append([]Grant(nil), kept...)
}
