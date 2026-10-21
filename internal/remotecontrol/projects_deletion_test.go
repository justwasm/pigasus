// Tests for the rc-conversation-deletions cycle's
// *MemProjectStore* Delete path. The dispatcher-level
// envelope tests for DeleteRemoteAgentProject live in
// conversation_test.go (which already covers both the
// in-process dispatcher entry point and the underlying
// MemProjectStore seam); this file is dedicated to
// the MemProjectStore seam itself so the brief's
// `TestProjectStore_MemDelete_*` matrix lives in one
// place.
//
// The Cycle rc-projects test file already covers
// List/Create/Update/Get on the same seam; this
// file only adds the Delete path so the
// MemProjectStore ↔ Dispatcher envelope tests
// don't have to grow beyond what
// rc-conversation-deletions ships.
package remotecontrol

import (
	"context"
	"errors"
	"testing"
)

// TestProjectStore_MemDelete_Ok pins the happy
// path: store has a row, Delete returns nil,
// Get then returns ErrProjectNotFound.
func TestProjectStore_MemDelete_Ok(t *testing.T) {
	ps := NewMemProjectStore(nil)
	row, err := ps.Create(context.Background(), NewProject{
		Name:      "alpha",
		Workspace: "/ws/alpha",
	})
	if err != nil {
		t.Fatalf("seed Create: %v", err)
	}
	if err := ps.Delete(context.Background(), row.ID, true); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := ps.Get(context.Background(), row.ID); !errors.Is(err, ErrProjectNotFound) {
		t.Errorf("Get after delete: err = %v, want ErrProjectNotFound", err)
	}
}

// TestProjectStore_MemDelete_Bound pins the
// "failIfBound=true, count>0" path →
// ErrProjectBound.
func TestProjectStore_MemDelete_Bound(t *testing.T) {
	ps := NewMemProjectStore(nil)
	row, err := ps.Create(context.Background(), NewProject{
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
	if err := ps.Delete(context.Background(), row.ID, true); !errors.Is(err, ErrProjectBound) {
		t.Errorf("Delete failIfBound=true with 2 conversations: err = %v, want ErrProjectBound", err)
	}
	// Project row must still be present.
	if _, err := ps.Get(context.Background(), row.ID); err != nil {
		t.Errorf("Get after failed delete: err = %v, want nil", err)
	}
}

// TestProjectStore_MemDelete_NotFound pins the
// unknown-id path → ErrProjectNotFound.
func TestProjectStore_MemDelete_NotFound(t *testing.T) {
	ps := NewMemProjectStore(nil)
	if err := ps.Delete(context.Background(), "ghost-id", true); !errors.Is(err, ErrProjectNotFound) {
		t.Errorf("Delete unknown id: err = %v, want ErrProjectNotFound", err)
	}
}

// TestProjectStore_MemDelete_BoundZero covers
// the "failIfBound=true, count=0" path → nil.
// This guards against a regression where
// BoundChecker returning 0 still surfaces
// ErrProjectBound.
func TestProjectStore_MemDelete_BoundZero(t *testing.T) {
	ps := NewMemProjectStore(nil)
	row, err := ps.Create(context.Background(), NewProject{
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

// TestProjectStore_MemDelete_BoundFalse covers
// the "failIfBound=false" override that lets the
// caller delete even when conversations still
// reference the project.
func TestProjectStore_MemDelete_BoundFalse(t *testing.T) {
	ps := NewMemProjectStore(nil)
	row, err := ps.Create(context.Background(), NewProject{
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
	if _, err := ps.Get(context.Background(), row.ID); !errors.Is(err, ErrProjectNotFound) {
		t.Errorf("Get after force-delete: err = %v, want ErrProjectNotFound", err)
	}
}

// TestProjectStore_MemDelete_NilChecker covers
// the "no BoundChecker installed" path: the
// store treats a nil BoundChecker as "always 0
// bound" so the failIfBound path never refuses.
func TestProjectStore_MemDelete_NilChecker(t *testing.T) {
	ps := NewMemProjectStore(nil)
	row, err := ps.Create(context.Background(), NewProject{
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

// TestProjectStore_MemDelete_EmptyID exercises
// the empty-id → ErrProjectNotFound path.
func TestProjectStore_MemDelete_EmptyID(t *testing.T) {
	ps := NewMemProjectStore(nil)
	if err := ps.Delete(context.Background(), "", true); !errors.Is(err, ErrProjectNotFound) {
		t.Errorf("Delete empty id: err = %v, want ErrProjectNotFound", err)
	}
}

// TestProjectStore_FSDelete_Ok exercises the
// FSProjectStore.Delete happy path against a
// t.TempDir(). Mirrors TestFSProjectStore_Roundtrip's
// pattern from the rc-projects cycle.
func TestProjectStore_FSDelete_Ok(t *testing.T) {
	dir := t.TempDir()
	store := NewFSProjectStore(dir, nil, nil, nil)
	row, err := store.Create(context.Background(), NewProject{
		Name:      "zeta",
		Workspace: "/ws/zeta",
	})
	if err != nil {
		t.Fatalf("seed Create: %v", err)
	}
	if err := store.Delete(context.Background(), row.ID, true); err != nil {
		t.Fatalf("FS Delete: %v", err)
	}
	if _, err := store.Get(context.Background(), row.ID); !errors.Is(err, ErrProjectNotFound) {
		t.Errorf("FS Get after delete: err = %v, want ErrProjectNotFound", err)
	}
}

// TestProjectStore_FSDelete_Bound exercises the
// FSProjectStore.Delete failIfBound=true path
// when the BoundChecker reports >0.
func TestProjectStore_FSDelete_Bound(t *testing.T) {
	dir := t.TempDir()
	checker := func(workspace string) (int, error) {
		if workspace == "/ws/eta" {
			return 1, nil
		}
		return 0, nil
	}
	store := NewFSProjectStore(dir, nil, checker, nil)
	row, err := store.Create(context.Background(), NewProject{
		Name:      "eta",
		Workspace: "/ws/eta",
	})
	if err != nil {
		t.Fatalf("seed Create: %v", err)
	}
	if err := store.Delete(context.Background(), row.ID, true); !errors.Is(err, ErrProjectBound) {
		t.Errorf("FS Delete failIfBound=true: err = %v, want ErrProjectBound", err)
	}
}
