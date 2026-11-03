// Package remotecontrol — origin_policy_store_mem.go: the
// *MemStore's Snapshot / SetPatterns /
// Subscribe / fanOut methods. Pure file split out of
// origin_policy_store.go; origin_policy_store.go carries
// the type definitions + cross-store helpers, and
// origin_policy_store_fs.go carries the
// *FSStore load / write / Snapshot /
// SetPatterns / Subscribe / fanOut methods.
package origins

import (
	"context"
	"sort"
	"sync"

	"github.com/justwasm/pigasus/originpolicy"
)

// Snapshot returns a defensive copy of the
// in-memory state. Patterns is sorted
// ascending — matches the
// FSStore semantics so the wire
// envelope is identical regardless of
// which backend stored the state.
func (m *MemStore) Snapshot(ctx context.Context) (State, error) {
	_ = ctx
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := cloneState(m.state)
	sort.Strings(out.Patterns)
	return out, nil
}

// SetPatterns validates each pattern,
// persists the accepted ones (sorted
// alphabetically) in memory, bumps
// UpdatedAt, and returns the post-mutation
// state + the rejected-list verbatim.
//
// Mirrors (*FSStore).SetPatterns.
func (m *MemStore) SetPatterns(ctx context.Context, patterns []string) (State, []string, bool, error) {
	_ = ctx
	accepted, rejected := filterAndSortPatterns(patterns)

	// Reject-all early-bail: when the caller
	// submitted at least one entry but every
	// entry failed validation, treat the
	// submission as a malformed dashboard
	// request and DO NOT touch the in-memory
	// state.
	if len(accepted) == 0 && len(rejected) > 0 {
		m.mu.RLock()
		snapshot := cloneState(m.state)
		m.mu.RUnlock()
		return snapshot, rejected, false, nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	m.state = State{
		Version:   StateVersion,
		Patterns:  accepted,
		UpdatedAt: now,
	}
	out := cloneState(m.state)
	m.fanOut(out)
	return out, rejected, true, nil
}

// Subscribe registers a buffered chan that
// receives the post-mutation OriginPolicy
// state on every successful SetPatterns.
// Returns the chan and an unsubscribe
// function. Cap=16, matching
// FSStore.
func (m *MemStore) Subscribe() (chan originpolicy.OriginPolicy, func()) {
	m.subMu.Lock()
	m.nextSubID++
	ch := make(chan originpolicy.OriginPolicy, 16)
	m.subscribers = append(m.subscribers, ch)
	m.subMu.Unlock()

	var once sync.Once
	unsub := func() {
		once.Do(func() {
			m.removeSubscriber(ch)
		})
	}
	return ch, unsub
}

// removeSubscriber removes ch from the
// subscribers slice and closes it. Mirrors
// (*FSStore).removeSubscriber so
// the two Subscribe paths stay
// behaviourally identical.
func (m *MemStore) removeSubscriber(ch chan originpolicy.OriginPolicy) {
	m.subMu.Lock()
	defer m.subMu.Unlock()
	for i, c := range m.subscribers {
		if c == ch {
			m.subscribers = append(m.subscribers[:i], m.subscribers[i+1:]...)
			break
		}
	}
	close(ch)
}

// fanOut broadcasts the supplied state to
// every registered subscriber. Slow
// subscribers lose frames on a non-blocking
// send. Mirrors
// (*FSStore).fanOut.
func (m *MemStore) fanOut(state State) {
	m.subMu.Lock()
	subs := make([]chan originpolicy.OriginPolicy, len(m.subscribers))
	copy(subs, m.subscribers)
	m.subMu.Unlock()
	for _, ch := range subs {
		policy := originpolicy.OriginPolicy{
			Patterns:  append([]string(nil), state.Patterns...),
			UpdatedAt: state.UpdatedAt,
		}
		select {
		case ch <- policy:
		default:
		}
	}
}
