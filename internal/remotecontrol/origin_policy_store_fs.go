// Package remotecontrol — origin_policy_store_fs.go: the
// *FSOriginPolicyStore's ensureParentDir / load /
// atomicWrite / emptyState / Snapshot / SetPatterns /
// Subscribe / fanOut methods. Pure file split out of
// origin_policy_store.go; origin_policy_store.go carries
// the type definitions + cross-store helpers, and
// origin_policy_store_mem.go carries the *MemOriginPolicyStore
// Snapshot / SetPatterns / Subscribe / fanOut methods.
package remotecontrol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/justwasm/pigasus/internal/originpolicy"
)

// ensureParentDir creates the parent
// directory of FSOriginPolicyStore.Path if
// it doesn't exist. Single-shot
// (sync.Once) so concurrent first-use calls
// don't race on the MkdirAll. 0o700 perms
// match every other
// $ANTIGRAVITY_HOME/secrets-style file in
// the cycle-9 FSProjectStore / cycle-19
// FSPermissionStore / etc. cascade.
func (s *FSOriginPolicyStore) ensureParentDir() error {
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
//   - (zero, false, nil) — no file exists
//     (a fresh host). The caller hands back
//     the empty state;
//   - (state, true, nil) — the file existed
//     and parsed cleanly;
//   - (state, true, err) — the file existed
//     but is malformed; the dispatcher maps
//     this to -32603 because the dashboard
//     should not see "no origin policy"
//     when the JSON is corrupt (the
//     operator needs to know).
func (s *FSOriginPolicyStore) load() (OriginPolicyState, bool, error) {
	data, err := os.ReadFile(s.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return OriginPolicyState{}, false, nil
		}
		return OriginPolicyState{}, false, err
	}
	var rec onDiskOriginPolicyState
	if err := json.Unmarshal(data, &rec); err != nil {
		// Malformed file. Bubble the
		// parse error up so the
		// dispatcher surfaces it; the
		// operator sees
		// "origin_policy.json is corrupt"
		// in the wire envelope instead of
		// a silent fallback to empty
		// state.
		return OriginPolicyState{}, true, fmt.Errorf("rc-origin-policy: parse %s: %w", s.Path, err)
	}
	if rec.Version == 0 {
		rec.Version = OriginPolicyStateVersion
	}
	if rec.Version > OriginPolicyStateVersion {
		return OriginPolicyState{}, true, fmt.Errorf("rc-origin-policy: %s version=%d is newer than supported %d", s.Path, rec.Version, OriginPolicyStateVersion)
	}
	if rec.Patterns == nil {
		rec.Patterns = []string{}
	}
	return OriginPolicyState{
		Version:   rec.Version,
		Patterns:  rec.Patterns,
		UpdatedAt: rec.UpdatedAt,
	}, true, nil
}

// atomicWrite writes the post-mutation state
// to disk. Temp file + fsync-via-Rename
// gives crash-consistent semantics.
func (s *FSOriginPolicyStore) atomicWrite(state OriginPolicyState) error {
	if err := s.ensureParentDir(); err != nil {
		return err
	}
	rec := onDiskOriginPolicyState{
		Version:   OriginPolicyStateVersion,
		Patterns:  state.Patterns,
		UpdatedAt: state.UpdatedAt,
	}
	if rec.Patterns == nil {
		rec.Patterns = []string{}
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
	if d := filepath.Dir(s.Path); d != "" && d != "." {
		if f, ferr := os.Open(d); ferr == nil {
			_ = f.Sync()
			_ = f.Close()
		}
	}
	return nil
}

// emptyState returns the fresh-host state:
// an empty patterns slice, version=1, and
// the wall-clock UpdatedAt. The store hands
// this back when Snapshot finds no file.
func (s *FSOriginPolicyStore) emptyState() OriginPolicyState {
	return OriginPolicyState{
		Version:   OriginPolicyStateVersion,
		Patterns:  []string{},
		UpdatedAt: s.now(),
	}
}

// Snapshot returns the current state. No
// file → the empty state (version=1,
// patterns=[]). A malformed file bubbles
// the parse error up.
func (s *FSOriginPolicyStore) Snapshot(ctx context.Context) (OriginPolicyState, error) {
	_ = ctx
	s.mu.RLock()
	defer s.mu.RUnlock()
	state, exists, err := s.load()
	if err != nil {
		return OriginPolicyState{}, err
	}
	if !exists {
		state = s.emptyState()
	}
	sort.Strings(state.Patterns)
	return cloneOriginPolicyState(state), nil
}

// SetPatterns validates each pattern, rejects
// malformed ones, persists the accepted ones
// (sorted alphabetically), bumps UpdatedAt,
// and returns the post-mutation state +
// rejected-list verbatim.
//
// Validation runs before the lock so a
// caller-supplied list with 0 good + N bad
// returns rejected=N without touching the
// on-disk file.
func (s *FSOriginPolicyStore) SetPatterns(ctx context.Context, patterns []string) (OriginPolicyState, []string, bool, error) {
	_ = ctx
	accepted, rejected := filterAndSortPatterns(patterns)

	// Reject-all early-bail: when the caller
	// submitted at least one entry but every
	// entry failed validation, treat the
	// submission as a malformed dashboard
	// request and DO NOT touch the on-disk
	// state.
	if len(accepted) == 0 && len(rejected) > 0 {
		s.mu.RLock()
		current, exists, lerr := s.load()
		s.mu.RUnlock()
		if lerr != nil {
			return OriginPolicyState{}, rejected, false, lerr
		}
		if !exists {
			current = s.emptyState()
		}
		sort.Strings(current.Patterns)
		return cloneOriginPolicyState(current), rejected, false, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	state := OriginPolicyState{
		Version:   OriginPolicyStateVersion,
		Patterns:  accepted,
		UpdatedAt: now,
	}
	if err := s.atomicWrite(state); err != nil {
		return OriginPolicyState{}, rejected, true, err
	}
	out := cloneOriginPolicyState(state)
	s.fanOut(out)
	return out, rejected, true, nil
}

// filterAndSortPatterns validates each pattern,
// populates the accepted/rejected lists, and
// sorts accepted ascending. Split out of
// SetPatterns so the per-call paths stay
// under the 50-LOC ceiling.
func filterAndSortPatterns(patterns []string) (accepted, rejected []string) {
	accepted = make([]string, 0, len(patterns))
	rejected = make([]string, 0)
	for _, p := range patterns {
		trimmed := strings.TrimSpace(p)
		if err := originpolicy.ValidateOriginPattern(trimmed); err != nil {
			rejected = append(rejected, p)
			continue
		}
		accepted = append(accepted, trimmed)
	}
	sort.Strings(accepted)
	return accepted, rejected
}

// Subscribe registers a buffered chan that
// receives the post-mutation OriginPolicy
// state on every successful SetPatterns.
// Returns the chan and an unsubscribe
// function.
//
// Cap of 16 per the brief. A slow
// subscriber loses frames on a non-blocking
// send.
func (s *FSOriginPolicyStore) Subscribe() (chan originpolicy.OriginPolicy, func()) {
	s.subMu.Lock()
	s.nextSubID++
	ch := make(chan originpolicy.OriginPolicy, 16)
	s.subscribers = append(s.subscribers, ch)
	s.subMu.Unlock()

	var once sync.Once
	unsub := func() {
		once.Do(func() {
			s.removeSubscriber(ch)
		})
	}
	return ch, unsub
}

// removeSubscriber removes ch from the
// subscribers slice and closes it. Split out
// of Subscribe so the unsub closure body
// stays compact.
func (s *FSOriginPolicyStore) removeSubscriber(ch chan originpolicy.OriginPolicy) {
	s.subMu.Lock()
	defer s.subMu.Unlock()
	for i, c := range s.subscribers {
		if c == ch {
			s.subscribers = append(s.subscribers[:i], s.subscribers[i+1:]...)
			break
		}
	}
	close(ch)
}

// fanOut broadcasts the supplied policy to
// every registered subscriber. Slow
// subscribers lose frames on a non-blocking
// send.
func (s *FSOriginPolicyStore) fanOut(state OriginPolicyState) {
	s.subMu.Lock()
	subs := make([]chan originpolicy.OriginPolicy, len(s.subscribers))
	copy(subs, s.subscribers)
	s.subMu.Unlock()
	for _, ch := range subs {
		policy := originpolicy.OriginPolicy{
			Patterns:  append([]string(nil), state.Patterns...),
			UpdatedAt: state.UpdatedAt,
		}
		select {
		case ch <- policy:
		default:
			// Drop on backpressure.
		}
	}
}
