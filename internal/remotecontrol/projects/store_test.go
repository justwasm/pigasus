// Package projects — store_test.go: the project store's own
// tests.
//
// The store is the domain's fact: the empty snapshot, the
// duplicate-name rule, the list / create / update / get /
// delete behaviour, the on-disk round trip and the
// fail-if-bound guard.
//
// The dispatcher's round-trip against the store lives in
// internal/remotecontrol/projects_test.go, because the
// dispatcher is the host's.
package projects

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestStore_MemList_Empty pins the empty-store
// path: a fresh MemStore returns no projects.
func TestStore_MemList_Empty(t *testing.T) {
	ps := NewMemStore(nil)
	rows, err := ps.List(context.Background(), "", 100)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("len(rows) = %d, want 0", len(rows))
	}
}

// TestStore_MemCreateAndList pins the
// happy-path CRUD: three creates, list returns three
// rows in insertion order (oldest first).
func TestStore_MemCreateAndList(t *testing.T) {
	// Monotonic clock — one nanosecond per call —
	// so CreatedAt strictly increases and the
	// insertion-order test is deterministic.
	var tick int64
	ps := NewMemStore(func() time.Time {
		tick++
		return time.Unix(1700000000, tick).UTC()
	})
	for _, name := range []string{"alpha", "beta", "gamma"} {
		row, err := ps.Create(context.Background(), Create{
			Name:      name,
			Workspace: "/ws/" + name,
		})
		if err != nil {
			t.Fatalf("Create %s: %v", name, err)
		}
		if row.ID == "" {
			t.Errorf("Create %s: empty ID", name)
		}
	}
	rows, err := ps.List(context.Background(), "", 100)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("len(rows) = %d, want 3", len(rows))
	}
	want := []string{"alpha", "beta", "gamma"}
	for i, r := range rows {
		if r.Name != want[i] {
			t.Errorf("rows[%d].Name = %q, want %q", i, r.Name, want[i])
		}
	}
}

// TestStore_MemUpdate covers the Update path:
// rename returns the new name; UpdatedAt > CreatedAt
// when the clock advances between Create and Update.
// We use a small time.Sleep to bump UpdatedAt past
// CreatedAt since the MemStore's clock is in
// seconds (Unix time) and two same-second calls
// would compare equal.
func TestStore_MemUpdate(t *testing.T) {
	// A monotonic clock that ticks once per call.
	var tick int64
	ps := NewMemStore(func() time.Time {
		tick++
		return time.Unix(1700000000+tick, 0).UTC()
	})
	row, err := ps.Create(context.Background(), Create{
		Name:      "original",
		Workspace: "/ws/original",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	updated, err := ps.Update(context.Background(), row.ID, Patch{
		Name: "renamed",
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Name != "renamed" {
		t.Errorf("updated.Name = %q, want renamed", updated.Name)
	}
	if !updated.UpdatedAt.After(updated.CreatedAt) {
		t.Errorf("UpdatedAt (%v) not after CreatedAt (%v)",
			updated.UpdatedAt, updated.CreatedAt)
	}
}

// TestStore_MemUpdate_PatchOnly covers the
// "leave unchanged" semantic of an Update: a patch
// that supplies only default_model leaves the name
// untouched.
func TestStore_MemUpdate_PatchOnly(t *testing.T) {
	ps := NewMemStore(nil)
	row, err := ps.Create(context.Background(), Create{
		Name:          "stable",
		Workspace:     "/ws/stable",
		DefaultModel:  "before",
		DefaultEffort: "low",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	updated, err := ps.Update(context.Background(), row.ID, Patch{
		DefaultModel: "after",
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Name != "stable" {
		t.Errorf("Name = %q, want stable (unchanged)", updated.Name)
	}
	if updated.DefaultModel != "after" {
		t.Errorf("DefaultModel = %q, want after", updated.DefaultModel)
	}
	if updated.DefaultEffort != "low" {
		t.Errorf("DefaultEffort = %q, want low (unchanged)", updated.DefaultEffort)
	}
}

// TestStore_MemCreate_Duplicate pins the
// duplicate-name → ErrExists path.
func TestStore_MemCreate_Duplicate(t *testing.T) {
	ps := NewMemStore(nil)
	if _, err := ps.Create(context.Background(), Create{
		Name:      "twins",
		Workspace: "/ws/first",
	}); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	_, err := ps.Create(context.Background(), Create{
		Name:      "twins",
		Workspace: "/ws/second",
	})
	if !errors.Is(err, ErrExists) {
		t.Fatalf("second Create err = %v, want ErrExists", err)
	}
}

// TestStore_MemUpdate_NotFound pins the
// unknown-id path.
func TestStore_MemUpdate_NotFound(t *testing.T) {
	ps := NewMemStore(nil)
	_, err := ps.Update(context.Background(), "nonexistent-id", Patch{
		Name: "anything",
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Update err = %v, want ErrNotFound", err)
	}
}

// TestStore_MemGet_NotFound pins the
// Get-unknown-id path.
func TestStore_MemGet_NotFound(t *testing.T) {
	ps := NewMemStore(nil)
	_, err := ps.Get(context.Background(), "missing-id")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get err = %v, want ErrNotFound", err)
	}
}

// TestStore_MemCreate_EmptyName pins the
// store-level "name is required" guard. The dispatcher
// also enforces this at the JSON-RPC layer; the
// store-level check is the second line of defence.
func TestStore_MemCreate_EmptyName(t *testing.T) {
	ps := NewMemStore(nil)
	_, err := ps.Create(context.Background(), Create{
		Name:      "   ",
		Workspace: "/ws/somewhere",
	})
	if err == nil || !strings.Contains(err.Error(), "name is required") {
		t.Fatalf("Create err = %v, want name-required error", err)
	}
}

// TestFSStore_Roundtrip exercises the
// production FSStore against t.TempDir():
// create three rows, list them back, update one,
// read the on-disk JSON to verify the layout, and
// get-by-id.
func TestFSStore_Roundtrip(t *testing.T) {
	dir := t.TempDir()
	store := NewFSStore(dir, nil, nil, nil)
	ctx := context.Background()

	for _, name := range []string{"alpha", "beta", "gamma"} {
		if _, err := store.Create(ctx, Create{
			Name:      name,
			Workspace: "/ws/" + name,
		}); err != nil {
			t.Fatalf("Create %s: %v", name, err)
		}
	}

	rows, err := store.List(ctx, "", 100)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("List len = %d, want 3", len(rows))
	}

	// Verify the on-disk JSON shape (per
	// projects_store.go's onDiskProject struct).
	for _, r := range rows {
		path := filepath.Join(dir, r.ID+".json")
		data, err := readJSONFile(t, path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if !strings.Contains(string(data), `"id":`) {
			t.Errorf("on-disk JSON missing id key:\n%s", data)
		}
		if !strings.Contains(string(data), `"workspace":`) {
			t.Errorf("on-disk JSON missing workspace key:\n%s", data)
		}
		if !strings.Contains(string(data), `"created_at":`) {
			t.Errorf("on-disk JSON missing created_at key:\n%s", data)
		}
	}

	// Update one. Get returns the new state.
	updated, err := store.Update(ctx, rows[1].ID, Patch{Name: "beta-renamed"})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Name != "beta-renamed" {
		t.Errorf("Updated name = %q, want beta-renamed", updated.Name)
	}
	got, err := store.Get(ctx, rows[1].ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "beta-renamed" {
		t.Errorf("Get name = %q, want beta-renamed", got.Name)
	}
}

// TestFSStore_Duplicate pins the on-disk
// duplicate-name path (the FS sweepName walk).
func TestFSStore_Duplicate(t *testing.T) {
	dir := t.TempDir()
	store := NewFSStore(dir, nil, nil, nil)
	ctx := context.Background()
	if _, err := store.Create(ctx, Create{Name: "dup", Workspace: "/ws/1"}); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	_, err := store.Create(ctx, Create{Name: "dup", Workspace: "/ws/2"})
	if !errors.Is(err, ErrExists) {
		t.Fatalf("second Create err = %v, want ErrExists", err)
	}
}

// TestStore_MemDelete_Ok pins the happy
// path: store has a row, Delete returns nil,
// Get then returns ErrNotFound.
func TestStore_MemDelete_Ok(t *testing.T) {
	ps := NewMemStore(nil)
	row, err := ps.Create(context.Background(), Create{
		Name:      "alpha",
		Workspace: "/ws/alpha",
	})
	if err != nil {
		t.Fatalf("seed Create: %v", err)
	}
	if err := ps.Delete(context.Background(), row.ID, true); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := ps.Get(context.Background(), row.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after delete: err = %v, want ErrNotFound", err)
	}
}

// TestStore_MemDelete_Bound pins the
// "failIfBound=true, count>0" path →
// ErrBound.
func TestStore_MemDelete_Bound(t *testing.T) {
	ps := NewMemStore(nil)
	row, err := ps.Create(context.Background(), Create{
		Name:      "beta",
		Workspace: "/ws/beta",
	})
	if err != nil {
		t.Fatalf("seed Create: %v", err)
	}
	ps.BoundChecker = func(workspace string) (int, error) {
		if workspace == "/ws/beta" {
			return 2, nil
		}
		return 0, nil
	}
	if err := ps.Delete(context.Background(), row.ID, true); !errors.Is(err, ErrBound) {
		t.Errorf("Delete failIfBound=true with 2 conversations: err = %v, want ErrBound", err)
	}
	// Project row must still be present.
	if _, err := ps.Get(context.Background(), row.ID); err != nil {
		t.Errorf("Get after failed delete: err = %v, want nil", err)
	}
}

// TestStore_MemDelete_NotFound pins the
// unknown-id path → ErrNotFound.
func TestStore_MemDelete_NotFound(t *testing.T) {
	ps := NewMemStore(nil)
	if err := ps.Delete(context.Background(), "ghost-id", true); !errors.Is(err, ErrNotFound) {
		t.Errorf("Delete unknown id: err = %v, want ErrNotFound", err)
	}
}

// TestStore_MemDelete_BoundZero covers
// the "failIfBound=true, count=0" path → nil.
// This guards against a regression where
// BoundChecker returning 0 still surfaces
// ErrBound.
func TestStore_MemDelete_BoundZero(t *testing.T) {
	ps := NewMemStore(nil)
	row, err := ps.Create(context.Background(), Create{
		Name:      "gamma",
		Workspace: "/ws/gamma",
	})
	if err != nil {
		t.Fatalf("seed Create: %v", err)
	}
	ps.BoundChecker = func(workspace string) (int, error) {
		return 0, nil
	}
	if err := ps.Delete(context.Background(), row.ID, true); err != nil {
		t.Errorf("Delete failIfBound=true with 0 conversations: err = %v, want nil", err)
	}
}

// TestStore_MemDelete_BoundFalse covers
// the "failIfBound=false" override that lets the
// caller delete even when conversations still
// reference the project.
func TestStore_MemDelete_BoundFalse(t *testing.T) {
	ps := NewMemStore(nil)
	row, err := ps.Create(context.Background(), Create{
		Name:      "delta",
		Workspace: "/ws/delta",
	})
	if err != nil {
		t.Fatalf("seed Create: %v", err)
	}
	called := false
	ps.BoundChecker = func(workspace string) (int, error) {
		called = true
		return 5, nil
	}
	if err := ps.Delete(context.Background(), row.ID, false); err != nil {
		t.Errorf("Delete failIfBound=false: err = %v, want nil", err)
	}
	if called {
		t.Errorf("BoundChecker called when failIfBound=false")
	}
	if _, err := ps.Get(context.Background(), row.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after force-delete: err = %v, want ErrNotFound", err)
	}
}

// TestStore_MemDelete_NilChecker covers
// the "no BoundChecker installed" path: the
// store treats a nil BoundChecker as "always 0
// bound" so the failIfBound path never refuses.
func TestStore_MemDelete_NilChecker(t *testing.T) {
	ps := NewMemStore(nil)
	row, err := ps.Create(context.Background(), Create{
		Name:      "epsilon",
		Workspace: "/ws/epsilon",
	})
	if err != nil {
		t.Fatalf("seed Create: %v", err)
	}
	// ps.BoundChecker == nil by default.
	if err := ps.Delete(context.Background(), row.ID, true); err != nil {
		t.Errorf("Delete failIfBound=true, nil checker: err = %v, want nil", err)
	}
}

// TestStore_MemDelete_EmptyID exercises
// the empty-id → ErrNotFound path.
func TestStore_MemDelete_EmptyID(t *testing.T) {
	ps := NewMemStore(nil)
	if err := ps.Delete(context.Background(), "", true); !errors.Is(err, ErrNotFound) {
		t.Errorf("Delete empty id: err = %v, want ErrNotFound", err)
	}
}

// TestFSStore_Delete_Ok exercises the
// FSStore.Delete happy path against a
// t.TempDir(). Mirrors TestFSStore_Roundtrip's
// pattern from the rc-projects cycle.
func TestFSStore_Delete_Ok(t *testing.T) {
	dir := t.TempDir()
	store := NewFSStore(dir, nil, nil, nil)
	row, err := store.Create(context.Background(), Create{
		Name:      "zeta",
		Workspace: "/ws/zeta",
	})
	if err != nil {
		t.Fatalf("seed Create: %v", err)
	}
	if err := store.Delete(context.Background(), row.ID, true); err != nil {
		t.Fatalf("FS Delete: %v", err)
	}
	if _, err := store.Get(context.Background(), row.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("FS Get after delete: err = %v, want ErrNotFound", err)
	}
}

// TestFSStore_Delete_Bound exercises the
// FSStore.Delete failIfBound=true path
// when the BoundChecker reports >0.
func TestFSStore_Delete_Bound(t *testing.T) {
	dir := t.TempDir()
	checker := func(workspace string) (int, error) {
		if workspace == "/ws/eta" {
			return 1, nil
		}
		return 0, nil
	}
	store := NewFSStore(dir, nil, checker, nil)
	row, err := store.Create(context.Background(), Create{
		Name:      "eta",
		Workspace: "/ws/eta",
	})
	if err != nil {
		t.Fatalf("seed Create: %v", err)
	}
	if err := store.Delete(context.Background(), row.ID, true); !errors.Is(err, ErrBound) {
		t.Errorf("FS Delete failIfBound=true: err = %v, want ErrBound", err)
	}
}

// readJSONFile is a tiny helper for the on-disk JSON
// layout test.
func readJSONFile(t *testing.T, path string) ([]byte, error) {
	t.Helper()
	return os.ReadFile(path)
}
