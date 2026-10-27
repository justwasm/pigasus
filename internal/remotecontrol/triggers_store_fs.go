// Package remotecontrol — triggers_store_fs.go: the
// *FSTriggerStore's ensureParentDir / load / atomicWrite /
// emptyState / newTriggerID / mutate / Snapshot / List /
// Create / Update / Delete methods. Pure file split out
// of triggers_store.go; triggers_store.go carries the
// type definitions + cross-store helpers, and
// triggers_store_mem.go carries the *MemTriggerStore
// constructors + Snapshot / List / Create / Update /
// Delete implementations.
//
// Stdlib-only. No third-party deps.
package remotecontrol

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/justwasm/pigasus/atomicfile"
)

// ensureParentDir creates the parent directory of
// FSTriggerStore.Path if it doesn't exist. Single-shot
// (sync.Once) so concurrent first-use calls don't race
// on the MkdirAll.
func (s *FSTriggerStore) ensureParentDir() error {
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
//   - (zero, false, nil) — no file exists (a fresh host).
//     The caller hands back the empty state;
//   - (state, true, nil) — the file existed and parsed
//     cleanly;
//   - (state, true, err) — the file existed but is
//     malformed; the dispatcher maps this to -32603
//     because the dashboard should not see "no triggers"
//     when the JSON is corrupt (the operator needs to
//     know).
func (s *FSTriggerStore) load() (TriggerState, bool, error) {
	data, err := os.ReadFile(s.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return TriggerState{}, false, nil
		}
		return TriggerState{}, false, err
	}
	var rec onDiskTriggerState
	if err := json.Unmarshal(data, &rec); err != nil {
		// Malformed file. Bubble the parse error up
		// so the dispatcher surfaces it; the operator
		// sees "triggers.json is corrupt" in the wire
		// envelope instead of a silent fallback to
		// empty state.
		return TriggerState{}, true, fmt.Errorf("rc-triggers: parse %s: %w", s.Path, err)
	}
	// Coerce a missing version to v1 (the on-disk
	// layout before the version stamp existed). A
	// reader that finds a version > TriggerStateVersion
	// refuses to touch the file (returns a "newer
	// binary required" envelope).
	if rec.Version == 0 {
		rec.Version = TriggerStateVersion
	}
	if rec.Version > TriggerStateVersion {
		return TriggerState{}, true, fmt.Errorf("rc-triggers: %s version=%d is newer than supported %d", s.Path, rec.Version, TriggerStateVersion)
	}
	if rec.Triggers == nil {
		// Normalise nil → empty slice so the wire
		// emits `[]` rather than `null`.
		rec.Triggers = []Trigger{}
	}
	return TriggerState{
		Version:   rec.Version,
		Triggers:  rec.Triggers,
		UpdatedAt: rec.UpdatedAt,
	}, true, nil
}

// atomicWrite writes the post-mutation state to disk.
// Temp file + fsync-via-Rename gives crash-consistent
// semantics; the directory is fsynced too (matches
// antigravity-token's style).
func (s *FSTriggerStore) atomicWrite(state TriggerState) error {
	if err := s.ensureParentDir(); err != nil {
		return err
	}
	rec := onDiskTriggerState{
		Version:   TriggerStateVersion,
		Triggers:  state.Triggers,
		UpdatedAt: state.UpdatedAt,
	}
	if rec.Triggers == nil {
		rec.Triggers = []Trigger{}
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicfile.WriteFile(s.Path, data, 0o600); err != nil {
		return err
	}
	// Best-effort dir fsync: ensures the rename is
	// durable across a power loss. Best-effort
	// because a platform-level fsync on the directory
	// may not be available — we tolerate the failure
	// silently (matching antigravity-token's style).
	if d := filepath.Dir(s.Path); d != "" && d != "." {
		if f, ferr := os.Open(d); ferr == nil {
			_ = f.Sync()
			_ = f.Close()
		}
	}
	return nil
}

// emptyState returns the fresh-host state: an empty
// triggers slice, version=1, and the wall-clock
// UpdatedAt. The store hands this back when Snapshot
// finds no file or when the in-memory mutation lands on
// an empty registry.
func (s *FSTriggerStore) emptyState() TriggerState {
	return TriggerState{
		Version:   TriggerStateVersion,
		Triggers:  []Trigger{},
		UpdatedAt: s.now(),
	}
}

// newTriggerID generates a fresh trigger id of the form
// "tri_<32hex>" (16 bytes of crypto/rand entropy, hex-
// encoded). The store mints the id at Create time; the
// dashboard never posts an id.
//
// randRead is nil → crypto/rand.Read.
func (s *FSTriggerStore) newTriggerID() (string, error) {
	r := s.randRead
	if r == nil {
		r = rand.Read
	}
	var buf [16]byte
	if _, err := io.ReadFull(readerFunc(r), buf[:]); err != nil {
		return "", fmt.Errorf("rc-triggers: random: %w", err)
	}
	return TriggerIDPrefix + hex.EncodeToString(buf[:]), nil
}

// mutate runs the read-modify-write cycle for Create,
// Update, and Delete. load() runs under s.mu.Lock; the
// caller's mutation runs under the same lock; the
// atomicWrite runs under it too. Snapshot / List
// read paths use s.mu.RLock so they don't race a writer.
//
// The caller is responsible for invoking fn with the
// loaded (or empty) state and returning either (a) the
// post-mutation state plus a result, or (b) an error —
// in which case mutate aborts the write.
func (s *FSTriggerStore) mutate(fn func(state TriggerState) (TriggerState, any, error)) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, exists, err := s.load()
	if err != nil {
		return nil, err
	}
	if !exists {
		state = s.emptyState()
	}
	state, out, err := fn(state)
	if err != nil {
		return nil, err
	}
	state.UpdatedAt = s.now()
	if err := s.atomicWrite(state); err != nil {
		return nil, err
	}
	return out, nil
}

// Snapshot returns the current state. No file → the
// empty state (version=1, triggers=[]). A malformed
// file bubbles the parse error up.
func (s *FSTriggerStore) Snapshot(ctx context.Context) (TriggerState, error) {
	_ = ctx
	s.mu.RLock()
	defer s.mu.RUnlock()
	state, exists, err := s.load()
	if err != nil {
		return TriggerState{}, err
	}
	if !exists {
		state = s.emptyState()
	}
	return cloneTriggerState(state), nil
}

// List returns the triggers filtered by projectID. Empty
// projectID returns all triggers. The returned slice is
// sorted by CreatedAt ascending so dashboards see a
// stable sequence; the test for the brief's "List sorted
// by created_at" pins this contract.
//
// A defensive copy is returned so a test that mutates
// the slice can't race the store's internal state.
func (s *FSTriggerStore) List(ctx context.Context, projectID string) ([]Trigger, error) {
	_ = ctx
	s.mu.RLock()
	defer s.mu.RUnlock()
	state, exists, err := s.load()
	if err != nil {
		return nil, err
	}
	if !exists {
		return []Trigger{}, nil
	}
	out := make([]Trigger, 0, len(state.Triggers))
	for _, t := range state.Triggers {
		if projectID == "" || t.ProjectID == projectID {
			out = append(out, t)
		}
	}
	sortTriggersByCreatedAt(out)
	return out, nil
}

// Create adds a new trigger to the store. The store
// stamps TriggerID / CreatedAt / UpdatedAt before
// appending; the caller's Trigger fields (those three
// otherwise) are ignored (overwritten with the
// store-stamped values). The (project_id, name) pair
// must be unique within the registry — a duplicate
// returns ErrTriggerExists.
//
// On success returns the post-mutation trigger (with
// store-stamped values) for the dispatcher's response
// envelope.
func (s *FSTriggerStore) Create(ctx context.Context, t Trigger) (Trigger, error) {
	_ = ctx
	id, err := s.newTriggerID()
	if err != nil {
		return Trigger{}, err
	}
	var out Trigger
	_, err = s.mutate(func(state TriggerState) (TriggerState, any, error) {
		if err := validateCreateInput(t); err != nil {
			return state, nil, err
		}
		projectID := stringsTrimSpace(t.ProjectID)
		name := normaliseName(t.Name)
		// Dedup on (project_id, name).
		for _, existing := range state.Triggers {
			if existing.ProjectID == projectID && existing.Name == name {
				return state, nil, ErrTriggerExists
			}
		}
		now := s.now()
		out = Trigger{
			TriggerID: id,
			ProjectID: projectID,
			Name:      name,
			Kind:      t.Kind,
			Spec:      cloneTriggerSpec(t.Spec),
			Enabled:   t.Enabled,
			CreatedAt: now,
			UpdatedAt: now,
		}
		state.Triggers = append(state.Triggers, out)
		return state, nil, nil
	})
	if err != nil {
		return Trigger{}, err
	}
	return out, nil
}

// validateCreateInput runs the same input checks the
// (*FSTriggerStore).Create and (*MemTriggerStore).Create
// closures used to inline. Split out so both Create
// bodies stay under the 50-LOC ceiling.
func validateCreateInput(t Trigger) error {
	name := normaliseName(t.Name)
	projectID := stringsTrimSpace(t.ProjectID)
	if name == "" {
		return fmt.Errorf("%w: name is required", ErrInvalidSpec)
	}
	if projectID == "" {
		return fmt.Errorf("%w: project_id is required", ErrInvalidSpec)
	}
	if !IsValidTriggerKind(t.Kind) {
		return fmt.Errorf("%w: kind must be one of file-changed|schedule|webhook|manual (got %q)", ErrInvalidSpec, string(t.Kind))
	}
	if err := ValidateTriggerSpec(t.Kind, t.Spec); err != nil {
		return err
	}
	return nil
}

// Update applies the non-nil fields of patch to the
// trigger identified by id. An unknown id returns
// ErrTriggerNotFound. A Name change that collides with
// another trigger in the same project returns
// ErrTriggerExists.
//
// The (kind, spec) pair can only be mutated together — a
// patch that changes the spec without changing the kind
// is a no-op for the kind (the spec re-validates under
// the existing kind).
//
// On success returns the post-mutation trigger for the
// dispatcher's response envelope.
func (s *FSTriggerStore) Update(ctx context.Context, id string, patch TriggerPatch) (Trigger, error) {
	_ = ctx
	id = normaliseName(id)
	if id == "" {
		return Trigger{}, ErrTriggerNotFound
	}
	var out Trigger
	_, err := s.mutate(func(state TriggerState) (TriggerState, any, error) {
		t, perr := s.applyTriggerPatch(&state, id, patch)
		if perr != nil {
			return state, nil, perr
		}
		out = t
		return state, nil, nil
	})
	if err != nil {
		return Trigger{}, err
	}
	return out, nil
}

// applyTriggerPatch mutates state in place: locates
// the row by id, applies the patch fields (with
// per-field validation / duplicate-name sweep /
// spec validation), stamps UpdatedAt, and returns
// the post-mutation Trigger row. Caller owns the
// lock (this helper runs under s.mutate so the FS
// store's single mutex is held). Extracted from
// Update to keep that handler body under the 50-fn
// ceiling that cycle
// rc-filesize-internal-remotecontrol enforces.
func (s *FSTriggerStore) applyTriggerPatch(state *TriggerState, id string, patch TriggerPatch) (Trigger, error) {
	idx := -1
	for i, t := range state.Triggers {
		if t.TriggerID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return Trigger{}, ErrTriggerNotFound
	}
	t := state.Triggers[idx]
	if patch.Name != nil {
		newName := normaliseName(*patch.Name)
		if newName == "" {
			return Trigger{}, fmt.Errorf("%w: name is required", ErrInvalidSpec)
		}
		if newName != t.Name {
			// Dedup on the new (project_id,
			// name) pair.
			for j, existing := range state.Triggers {
				if j == idx {
					continue
				}
				if existing.ProjectID == t.ProjectID && existing.Name == newName {
					return Trigger{}, ErrTriggerExists
				}
			}
			t.Name = newName
		}
	}
	if patch.Enabled != nil {
		t.Enabled = *patch.Enabled
	}
	if patch.Spec != nil {
		if err := ValidateTriggerSpec(t.Kind, *patch.Spec); err != nil {
			return Trigger{}, err
		}
		t.Spec = cloneTriggerSpec(*patch.Spec)
	}
	t.UpdatedAt = s.now()
	state.Triggers[idx] = t
	return t, nil
}

// Delete removes the trigger identified by id. An
// unknown id returns ErrTriggerNotFound. Returns the
// post-mutation state.
func (s *FSTriggerStore) Delete(ctx context.Context, id string) (TriggerState, error) {
	_ = ctx
	id = normaliseName(id)
	if id == "" {
		return TriggerState{}, ErrTriggerNotFound
	}
	stateAny, err := s.mutate(func(state TriggerState) (TriggerState, any, error) {
		idx := -1
		for i, t := range state.Triggers {
			if t.TriggerID == id {
				idx = i
				break
			}
		}
		if idx < 0 {
			return state, nil, ErrTriggerNotFound
		}
		kept := make([]Trigger, 0, len(state.Triggers)-1)
		kept = append(kept, state.Triggers[:idx]...)
		kept = append(kept, state.Triggers[idx+1:]...)
		state.Triggers = kept
		return state, nil, nil
	})
	if err != nil {
		return TriggerState{}, err
	}
	// Drop the post-mutation state back into the
	// snapshot the dispatcher needs in the broadcast /
	// response envelope.
	if st, ok := stateAny.(*TriggerState); ok && st != nil {
		return cloneTriggerState(*st), nil
	}
	// If mutate returned nil (it shouldn't here; the
	// closure never does), fall back to a fresh
	// snapshot. Defensive fallback; never trigger in
	// practice.
	fresh, err := s.Snapshot(ctx)
	if err != nil {
		return TriggerState{}, err
	}
	return fresh, nil
}
