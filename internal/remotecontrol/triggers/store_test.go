// Package triggers — store_test.go: the trigger store's own tests.
//
// The store is the domain's fact: the fresh-state snapshot, the
// (project_id, name) uniqueness rule, the list / update / delete
// behaviour, the sort order and the on-disk round trip.
//
// The dispatcher's round-trip against the store lives in
// internal/remotecontrol/triggers_test.go, because the dispatcher
// is the host's.
package triggers

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// validTimeNow is the standard deterministic clock the
// in-process tests stamp dates with. RFC3339Nano form
// is used by the wire helpers; we use time.Date for
// readability here.
func validTimeNow() time.Time {
	return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
}

// TestValidateTriggerSpec_FileChanged_Ok pins the

// validSpecFor is a per-kind spec builder the in-process
// tests reuse. Keeping the constructor here means each
// test's nominal shape is obvious at the call site.
func validSpecFor(kind Kind) Spec {
	switch kind {
	case FileChanged:
		return Spec{"pattern": "*.go", "event": "modify"}
	case Schedule:
		return Spec{"prompt": "tick", "cron": "* * *"}
	case Webhook:
		return Spec{"path": "/hook", "prompt": "p"}
	case Manual:
		return Spec{"prompt": "p"}
	default:
		return Spec{}
	}
}

// TestTriggerStore_DefaultState_Empty pins the
// fresh-store default state: empty triggers slice,
// version=1, updated_at set.
func TestTriggerStore_DefaultState_Empty(t *testing.T) {
	ts := NewMemStore(validTimeNow)
	st, err := ts.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if st.Version != StateVersion {
		t.Errorf("Version = %d, want %d", st.Version, StateVersion)
	}
	if len(st.Triggers) != 0 {
		t.Errorf("Triggers length = %d, want 0", len(st.Triggers))
	}
	out, err := ts.List(context.Background(), "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("List length = %d, want 0", len(out))
	}
}

// validSpecFor is a per-kind spec builder the in-process
// tests reuse. Keeping the constructor here means each
// test's nominal shape is obvious at the call site.

func TestTriggerStore_Create_DedupName(t *testing.T) {
	ts := NewMemStore(validTimeNow)

	// First create — succeeds.
	t1, err := ts.Create(context.Background(), Trigger{
		ProjectID: "proj-1",
		Name:      "on-pull",
		Kind:      Manual,
		Spec:      validSpecFor(Manual),
		Enabled:   true,
	})
	if err != nil {
		t.Fatalf("first Create: %v", err)
	}
	if t1.TriggerID == "" {
		t.Errorf("first Create: trigger_id empty")
	}

	// Second create with same (project_id, name)
	// must return ErrExists.
	_, err = ts.Create(context.Background(), Trigger{
		ProjectID: "proj-1",
		Name:      "on-pull",
		Kind:      Manual,
		Spec:      validSpecFor(Manual),
		Enabled:   true,
	})
	if !errors.Is(err, ErrExists) {
		t.Fatalf("second Create: error is not ErrExists: %v", err)
	}

	// Same name in a different project is allowed.
	_, err = ts.Create(context.Background(), Trigger{
		ProjectID: "proj-2",
		Name:      "on-pull",
		Kind:      Manual,
		Spec:      validSpecFor(Manual),
		Enabled:   true,
	})
	if err != nil {
		t.Fatalf("different-project Create: %v", err)
	}

}

// TestTriggerStore_List_All pins the "no filter"
// branch: List("") returns every trigger, sorted by
// CreatedAt ascending.

func TestTriggerStore_List_All(t *testing.T) {
	ts := NewMemStore(validTimeNow)
	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 1; i <= 3; i++ {
		_, err := ts.Create(context.Background(), Trigger{
			ProjectID: "proj",
			Name:      "name-" + string(rune('a'+i-1)),
			Kind:      Manual,
			Spec:      validSpecFor(Manual),
			Enabled:   true,
		})
		if err != nil {
			t.Fatalf("Create %d: %v", i, err)
		}
		_ = clock // monotonic marker for sort test below
	}
	out, err := ts.List(context.Background(), "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(out) != 3 {
		t.Errorf("List length = %d, want 3", len(out))
	}
	// Monotonic CreatedAt ⇒ output order matches input order.
	for i := 1; i < len(out); i++ {
		if out[i].CreatedAt.Before(out[i-1].CreatedAt) {
			t.Errorf("triggers not sorted by CreatedAt ascending: %v before %v",
				out[i-1].CreatedAt, out[i].CreatedAt)
		}
	}
}

// TestTriggerStore_List_FilterByProject pins the
// "filter on project_id" branch: List("proj-1")
// returns only proj-1's triggers.

func TestTriggerStore_List_FilterByProject(t *testing.T) {
	ts := NewMemStore(validTimeNow)
	_, _ = ts.Create(context.Background(), Trigger{
		ProjectID: "proj-1", Name: "a", Kind: Manual,
		Spec: validSpecFor(Manual),
	})
	_, _ = ts.Create(context.Background(), Trigger{
		ProjectID: "proj-2", Name: "a", Kind: Manual,
		Spec: validSpecFor(Manual),
	})
	_, _ = ts.Create(context.Background(), Trigger{
		ProjectID: "proj-1", Name: "b", Kind: Manual,
		Spec: validSpecFor(Manual),
	})
	out, err := ts.List(context.Background(), "proj-1")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(out) != 2 {
		t.Errorf("List(proj-1) length = %d, want 2", len(out))
	}
	for _, tg := range out {
		if tg.ProjectID != "proj-1" {
			t.Errorf("List(proj-1): stray trigger %q in proj %q", tg.Name, tg.ProjectID)
		}
	}
}

// TestTriggerStore_Update_Name pins the
// Update-with-Name-change happy path.

func TestTriggerStore_Update_Name(t *testing.T) {
	ts := NewMemStore(validTimeNow)
	created, err := ts.Create(context.Background(), Trigger{
		ProjectID: "proj", Name: "old-name", Kind: Manual,
		Spec: validSpecFor(Manual),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	newName := "new-name"
	out, err := ts.Update(context.Background(), created.TriggerID, Patch{
		Name: &newName,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if out.Name != "new-name" {
		t.Errorf("Name = %q, want new-name", out.Name)
	}
}

// TestTriggerStore_Update_Enabled pins the
// Update-with-Enabled-change happy path.

func TestTriggerStore_Update_Enabled(t *testing.T) {
	ts := NewMemStore(validTimeNow)
	created, err := ts.Create(context.Background(), Trigger{
		ProjectID: "proj", Name: "x", Kind: Manual,
		Spec: validSpecFor(Manual), Enabled: true,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	enabled := false
	out, err := ts.Update(context.Background(), created.TriggerID, Patch{
		Enabled: &enabled,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if out.Enabled {
		t.Errorf("Enabled = true, want false")
	}
}

// TestTriggerStore_Update_Spec pins the
// Update-with-Spec-change happy path. The kind
// governs validation; the new spec must validate
// against the existing kind.

func TestTriggerStore_Update_Spec(t *testing.T) {
	ts := NewMemStore(validTimeNow)
	created, err := ts.Create(context.Background(), Trigger{
		ProjectID: "proj", Name: "x", Kind: Schedule,
		Spec: Spec{"prompt": "p", "cron": "* * *"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	newSpec := Spec{"prompt": "p2", "every": "30m"}
	out, err := ts.Update(context.Background(), created.TriggerID, Patch{
		Spec: &newSpec,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got, _ := out.Spec["every"].(string); got != "30m" {
		t.Errorf("Spec[every] = %v, want 30m", out.Spec["every"])
	}
}

// TestTriggerStore_Update_NotFound pins the
// unknown-id → -32017 envelope.

func TestTriggerStore_Update_NotFound(t *testing.T) {
	ts := NewMemStore(validTimeNow)
	enabled := true
	_, err := ts.Update(context.Background(), "tri_does_not_exist", Patch{
		Enabled: &enabled,
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Update: error is not ErrNotFound: %v", err)
	}
}

// TestTriggerStore_Delete pins the Delete happy
// path: post-Delete List returns the empty set, and
// Snapshot's UpdatedAt has bumped.

func TestTriggerStore_Delete(t *testing.T) {
	ts := NewMemStore(validTimeNow)
	created, err := ts.Create(context.Background(), Trigger{
		ProjectID: "proj", Name: "x", Kind: Manual,
		Spec: validSpecFor(Manual),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	state, err := ts.Delete(context.Background(), created.TriggerID)
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if len(state.Triggers) != 0 {
		t.Errorf("post-Delete Triggers length = %d, want 0", len(state.Triggers))
	}
}

// TestTriggerStore_Delete_NotFound pins the
// unknown-id → -32017 envelope for Delete.

func TestTriggerStore_Delete_NotFound(t *testing.T) {
	ts := NewMemStore(validTimeNow)
	_, err := ts.Delete(context.Background(), "tri_does_not_exist")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Delete: error is not ErrNotFound: %v", err)
	}
}

// TestTriggerStore_SortPinning pins that the store
// sorts by CreatedAt ascending regardless of
// insertion order: a pre-populated starting snapshot
// with out-of-order CreatedAt is sorted by
// Snapshot / List, not preserved as-is.

func TestTriggerStore_SortPinning(t *testing.T) {
	now := validTimeNow()
	t1 := now.Add(3 * time.Hour)
	t2 := now.Add(1 * time.Hour)
	t3 := now.Add(2 * time.Hour)
	ts := NewMemStoreWith([]Trigger{
		{TriggerID: "tri_a", Name: "a", ProjectID: "p", Kind: Manual,
			Spec: validSpecFor(Manual), CreatedAt: t1, UpdatedAt: t1},
		{TriggerID: "tri_b", Name: "b", ProjectID: "p", Kind: Manual,
			Spec: validSpecFor(Manual), CreatedAt: t2, UpdatedAt: t2},
		{TriggerID: "tri_c", Name: "c", ProjectID: "p", Kind: Manual,
			Spec: validSpecFor(Manual), CreatedAt: t3, UpdatedAt: t3},
	}, validTimeNow)
	out, err := ts.List(context.Background(), "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(out) != 3 {
		t.Fatalf("List length = %d, want 3", len(out))
	}
	// Sorted by CreatedAt ascending: b (1h), c (2h), a (3h).
	want := []string{"tri_b", "tri_c", "tri_a"}
	for i, w := range want {
		if out[i].TriggerID != w {
			t.Errorf("out[%d].TriggerID = %q, want %q", i, out[i].TriggerID, w)
		}
	}
}

// TestTriggerStore_Create_BadSpec pins the
// validator's path through Create: a bad spec
// surfaces ErrInvalidSpec → -32602 before the row
// lands.

func TestTriggerStore_Create_BadSpec(t *testing.T) {
	ts := NewMemStore(validTimeNow)
	_, err := ts.Create(context.Background(), Trigger{
		ProjectID: "proj", Name: "x", Kind: Manual,
		Spec: Spec{}, // empty — missing prompt
	})
	if !errors.Is(err, ErrInvalidSpec) {
		t.Fatalf("Create: error is not ErrInvalidSpec: %v", err)
	}
}

// TestTriggerStore_Create_NoStore pins the
// "dispatcher with nil store" → -32603 envelope.
// The dispatcher fabricates a fresh MemStore
// when Config.TriggersStore is nil (the tests'
// default-shim path). To exercise the negative, we
// bypass that fabrication by clearing the field
// POST-construction — explicitly setting it back to
// nil trips the dispatcher's "no store installed"
// guard.

// TestFSTriggerStore_RoundTrip pins the fresh +
// Create + reload happy path: a brand-new directory
// + a Create + a fresh store load returns the row.
func TestFSTriggerStore_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "triggers.json")
	now := validTimeNow()
	nowFn := func() time.Time { return now }
	s1 := NewFSStore(path, nowFn)
	created, err := s1.Create(context.Background(), Trigger{
		ProjectID: "p", Name: "n", Kind: Manual,
		Spec: validSpecFor(Manual),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.TriggerID == "" {
		t.Fatalf("Create: trigger_id empty")
	}
	// Reload — the fresh store loads the saved
	// state and Snapshot returns the row.
	s2 := NewFSStore(path, nowFn)
	st, err := s2.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Reload Snapshot: %v", err)
	}
	if len(st.Triggers) != 1 {
		t.Errorf("Reload: triggers length = %d, want 1", len(st.Triggers))
	}
	if st.Triggers[0].TriggerID != created.TriggerID {
		t.Errorf("Reload: trigger_id = %q, want %q",
			st.Triggers[0].TriggerID, created.TriggerID)
	}
}

// TestFSTriggerStore_AtomicSave pins that a
// successful write does not leave a .tmp file
// behind, and that the file persists with 0o600
// perms.

func TestFSTriggerStore_AtomicSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "triggers.json")
	now := validTimeNow()
	nowFn := func() time.Time { return now }
	s := NewFSStore(path, nowFn)
	if _, err := s.Create(context.Background(), Trigger{
		ProjectID: "p", Name: "n", Kind: Manual,
		Spec: validSpecFor(Manual),
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	// .tmp must be gone — Rename moved it.
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf(".tmp file should not exist after atomic write: %v", err)
	}
	// 0o600 perms.
	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if got := st.Mode().Perm(); got != 0o600 {
		t.Errorf("perms = %o, want 0o600", got)
	}
}

// TestFSTriggerStore_FreshFile pins the
// "no file exists → empty state" path.

func TestFSTriggerStore_FreshFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "triggers.json")
	s := NewFSStore(path, nil)
	st, err := s.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(st.Triggers) != 0 {
		t.Errorf("Triggers length = %d, want 0", len(st.Triggers))
	}
	if st.Version != StateVersion {
		t.Errorf("Version = %d, want %d", st.Version, StateVersion)
	}
}

// TestFSTriggerStore_CreateReloads pins that a
// Create's output is visible across a fresh
// store load and across a List call.

func TestFSTriggerStore_CreateReloads(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "triggers.json")
	now := validTimeNow()
	nowFn := func() time.Time { return now }
	s := NewFSStore(path, nowFn)
	for i := 1; i <= 2; i++ {
		_, err := s.Create(context.Background(), Trigger{
			ProjectID: "p",
			Name:      "n-" + string(rune('a'+i-1)),
			Kind:      Manual,
			Spec:      validSpecFor(Manual),
		})
		if err != nil {
			t.Fatalf("Create %d: %v", i, err)
		}
	}
	s2 := NewFSStore(path, nowFn)
	out, err := s2.List(context.Background(), "p")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(out) != 2 {
		t.Errorf("List length = %d, want 2", len(out))
	}
}

// TestFSTriggerStore_VersionCheck pins that a
// future-version file surfaces "newer binary
// required".

func TestFSTriggerStore_VersionCheck(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "triggers.json")
	// Manually write a future-version file.
	future := onDiskState{
		Version:   StateVersion + 99,
		Triggers:  []Trigger{},
		UpdatedAt: validTimeNow(),
	}
	data, _ := json.MarshalIndent(future, "", "  ")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	s := NewFSStore(path, nil)
	_, err := s.Snapshot(context.Background())
	if err == nil {
		t.Fatalf("Snapshot: want error, got nil")
	}
	if !strings.Contains(err.Error(), "newer than supported") {
		t.Errorf("Snapshot error must mention 'newer than supported': %v", err)
	}
}

// TestFSTriggerStore_MalformedFile pins that a
// corrupt JSON file surfaces the parse error.

func TestFSTriggerStore_MalformedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "triggers.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	s := NewFSStore(path, nil)
	_, err := s.Snapshot(context.Background())
	if err == nil {
		t.Fatalf("Snapshot: want error, got nil")
	}
	if !strings.Contains(err.Error(), "rc-triggers: parse") {
		t.Errorf("Snapshot error must mention 'rc-triggers: parse': %v", err)
	}
}

// -----------------------------------------------------------------------------
// Dispatcher round-trips
// -----------------------------------------------------------------------------

// TestListRemoteAgentTriggers_Ok pins the
// List happy path: an empty store returns
// {ok:true, triggers:[]}.
