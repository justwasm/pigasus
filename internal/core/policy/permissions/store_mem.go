package permissions

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// MemStore is the in-memory Store fixture tests use.
type MemStore struct {
	mu    sync.RWMutex
	state State

	now func() time.Time
}

// NewMemStore returns a fresh, default-state MemStore.
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

// SetPreset sets the preset and bumps UpdatedAt.
func (m *MemStore) SetPreset(ctx context.Context, preset Preset) (State, error) {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state.Preset = preset
	m.state.UpdatedAt = m.now()
	return cloneState(m.state), nil
}

// SetReview sets the artifact-review mode and bumps UpdatedAt.
func (m *MemStore) SetReview(ctx context.Context, mode ReviewMode) (State, error) {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state.Review = mode
	m.state.UpdatedAt = m.now()
	return cloneState(m.state), nil
}

// AddGrants appends grants to the in-memory state.
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

// RemoveGrants drops the grants whose key matches the supplied keys.
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

func cloneState(state State) State {
	out := state
	if state.Grants != nil {
		out.Grants = append([]Grant(nil), state.Grants...)
	}
	return out
}

type grantAppendIndex struct {
	m map[string]struct{}
}

func newGrantAppendIndex(n int) *grantAppendIndex {
	return &grantAppendIndex{m: make(map[string]struct{}, n)}
}

func (idx *grantAppendIndex) seenOrMark(key string) bool {
	if _, ok := idx.m[key]; ok {
		return true
	}
	idx.m[key] = struct{}{}
	return false
}

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

func sortGrantsStable(grants []Grant) {
	sort.SliceStable(grants, func(i, j int) bool {
		if grants[i].AddedAt.Equal(grants[j].AddedAt) {
			return grants[i].GrantKey() < grants[j].GrantKey()
		}
		return grants[i].AddedAt.Before(grants[j].AddedAt)
	})
}

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
			if action == ActionUnsandboxed {
				return ErrGrantUnsandboxed
			}
			if !ValidGrantAction(action) {
				return ErrUnknownGrantAction(action, GrantKeyFor(action, tool, pattern))
			}
			if pattern == "" {
				return fmt.Errorf("rc-permissions: grant requires non-empty action and pattern")
			}
		} else {
			if tool == "" || pattern == "" {
				return fmt.Errorf("rc-permissions: grant requires non-empty tool_name and pattern")
			}
		}
		key := GrantKeyFor(action, tool, pattern)
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
	*dest = append([]Grant(nil), kept...)
}
