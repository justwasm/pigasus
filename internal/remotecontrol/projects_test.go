// Tests for the rc-projects cycle. Covers:
//
//   - ProjectStore.MemProjectStore (List / Create /
//     Update / Get / duplicate-name / not-found) at
//     the in-process level (no Unix socket, no ws
//     adapter — just ProjectStore method calls);
//
//   - the JSON-RPC envelope shape the dispatcher emits
//     for each of the three methods
//     (ListRemoteAgentProjects /
//     CreateRemoteAgentProject /
//     UpdateRemoteAgentProject), including the
//     sentinel-error mapping (ErrProjectExists →
//     -32009, ErrProjectNotFound → -32004,
//     ErrPathNotAllowed → -32007);
//
//   - the param-validation rules (empty name → -32602,
//     negative limit → -32602, bad effort → -32602,
//     etc.);
//
//   - the FSProjectStore happy path against a real
//     t.TempDir() so the on-disk format is exercised.
//
// Wire-level round-trips (ws adapter + JSON marshalling)
// live in remotetransport/tcp_test.go,
// mirroring the split the rc-file-ops cycle established.
package remotecontrol

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

// newProjectsDispatcher returns a Dispatcher with the
// supplied ProjectStore pre-installed. The helper is
// intentionally small (no FS provider, no list backend)
// because the three project methods reach into the
// ProjectStore seam directly.
func newProjectsDispatcher(t *testing.T, ps ProjectStore) *Dispatcher {
	t.Helper()
	sess := NewSessionState("1.4.0")
	d, err := NewDispatcher(Config{
		Session:  sess,
		Projects: ps,
	})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	return d
}

// dispatchProjects sends a List/Create/Update request
// through the dispatcher and returns the (result,
// error) pair. Mirrors dispatchFile from files_test.go
// — same envelope, same shape, just calling the
// per-cycle entry point.
func dispatchProjects(t *testing.T, d *Dispatcher, method string, params any) (map[string]any, map[string]any) {
	t.Helper()
	var raw []byte
	if params != nil {
		var err error
		raw, err = jsonMarshal(params)
		if err != nil {
			t.Fatalf("marshal params: %v", err)
		}
	}
	req := jsonRPCFor(1, method, raw)
	resp := d.Dispatch(context.Background(), req)
	if errObj, ok := resp["error"].(map[string]any); ok {
		return nil, errObj
	}
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("%s: no result, got %#v", method, resp)
	}
	return res, nil
}

// ---- MemProjectStore in-process tests ----------------------------

// TestProjectStore_MemList_Empty pins the empty-store
// path: a fresh MemProjectStore returns no projects.
func TestProjectStore_MemList_Empty(t *testing.T) {
	ps := NewMemProjectStore(nil)
	rows, err := ps.List(context.Background(), "", 100)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("len(rows) = %d, want 0", len(rows))
	}
}

// TestProjectStore_MemCreateAndList pins the
// happy-path CRUD: three creates, list returns three
// rows in insertion order (oldest first).
func TestProjectStore_MemCreateAndList(t *testing.T) {
	// Monotonic clock — one nanosecond per call —
	// so CreatedAt strictly increases and the
	// insertion-order test is deterministic.
	var tick int64
	ps := NewMemProjectStore(func() time.Time {
		tick++
		return time.Unix(1700000000, tick).UTC()
	})
	for _, name := range []string{"alpha", "beta", "gamma"} {
		row, err := ps.Create(context.Background(), NewProject{
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

// TestProjectStore_MemUpdate covers the Update path:
// rename returns the new name; UpdatedAt > CreatedAt
// when the clock advances between Create and Update.
// We use a small time.Sleep to bump UpdatedAt past
// CreatedAt since the MemProjectStore's clock is in
// seconds (Unix time) and two same-second calls
// would compare equal.
func TestProjectStore_MemUpdate(t *testing.T) {
	// A monotonic clock that ticks once per call.
	var tick int64
	ps := NewMemProjectStore(func() time.Time {
		tick++
		return time.Unix(1700000000+tick, 0).UTC()
	})
	row, err := ps.Create(context.Background(), NewProject{
		Name:      "original",
		Workspace: "/ws/original",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	updated, err := ps.Update(context.Background(), row.ID, ProjectPatch{
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

// TestProjectStore_MemUpdate_PatchOnly covers the
// "leave unchanged" semantic of an Update: a patch
// that supplies only default_model leaves the name
// untouched.
func TestProjectStore_MemUpdate_PatchOnly(t *testing.T) {
	ps := NewMemProjectStore(nil)
	row, err := ps.Create(context.Background(), NewProject{
		Name:          "stable",
		Workspace:     "/ws/stable",
		DefaultModel:  "before",
		DefaultEffort: "low",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	updated, err := ps.Update(context.Background(), row.ID, ProjectPatch{
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

// TestProjectStore_MemCreate_Duplicate pins the
// duplicate-name → ErrProjectExists path.
func TestProjectStore_MemCreate_Duplicate(t *testing.T) {
	ps := NewMemProjectStore(nil)
	if _, err := ps.Create(context.Background(), NewProject{
		Name:      "twins",
		Workspace: "/ws/first",
	}); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	_, err := ps.Create(context.Background(), NewProject{
		Name:      "twins",
		Workspace: "/ws/second",
	})
	if !errors.Is(err, ErrProjectExists) {
		t.Fatalf("second Create err = %v, want ErrProjectExists", err)
	}
}

// TestProjectStore_MemUpdate_NotFound pins the
// unknown-id path.
func TestProjectStore_MemUpdate_NotFound(t *testing.T) {
	ps := NewMemProjectStore(nil)
	_, err := ps.Update(context.Background(), "nonexistent-id", ProjectPatch{
		Name: "anything",
	})
	if !errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("Update err = %v, want ErrProjectNotFound", err)
	}
}

// TestProjectStore_MemGet_NotFound pins the
// Get-unknown-id path.
func TestProjectStore_MemGet_NotFound(t *testing.T) {
	ps := NewMemProjectStore(nil)
	_, err := ps.Get(context.Background(), "missing-id")
	if !errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("Get err = %v, want ErrProjectNotFound", err)
	}
}

// TestProjectStore_MemCreate_EmptyName pins the
// store-level "name is required" guard. The dispatcher
// also enforces this at the JSON-RPC layer; the
// store-level check is the second line of defence.
func TestProjectStore_MemCreate_EmptyName(t *testing.T) {
	ps := NewMemProjectStore(nil)
	_, err := ps.Create(context.Background(), NewProject{
		Name:      "   ",
		Workspace: "/ws/somewhere",
	})
	if err == nil || !strings.Contains(err.Error(), "name is required") {
		t.Fatalf("Create err = %v, want name-required error", err)
	}
}

// ---- FSProjectStore on-disk tests ---------------------------------

// TestFSProjectStore_Roundtrip exercises the
// production FSProjectStore against t.TempDir():
// create three rows, list them back, update one,
// read the on-disk JSON to verify the layout, and
// get-by-id.
func TestFSProjectStore_Roundtrip(t *testing.T) {
	dir := t.TempDir()
	store := NewFSProjectStore(dir, nil, nil, nil)
	ctx := context.Background()

	for _, name := range []string{"alpha", "beta", "gamma"} {
		if _, err := store.Create(ctx, NewProject{
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
	updated, err := store.Update(ctx, rows[1].ID, ProjectPatch{Name: "beta-renamed"})
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

// TestFSProjectStore_Duplicate pins the on-disk
// duplicate-name path (the FS sweepName walk).
func TestFSProjectStore_Duplicate(t *testing.T) {
	dir := t.TempDir()
	store := NewFSProjectStore(dir, nil, nil, nil)
	ctx := context.Background()
	if _, err := store.Create(ctx, NewProject{Name: "dup", Workspace: "/ws/1"}); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	_, err := store.Create(ctx, NewProject{Name: "dup", Workspace: "/ws/2"})
	if !errors.Is(err, ErrProjectExists) {
		t.Fatalf("second Create err = %v, want ErrProjectExists", err)
	}
}

// ---- dispatcher-level envelope tests ------------------------------

// TestListRemoteAgentProjects_Ok exercises the
// happy path: a fresh MemProjectStore, create three
// rows, list returns three rows in the result
// envelope.
func TestListRemoteAgentProjects_Ok(t *testing.T) {
	ps := NewMemProjectStore(nil)
	ps.Create(context.Background(), NewProject{Name: "p1", Workspace: "/ws/p1"})
	ps.Create(context.Background(), NewProject{Name: "p2", Workspace: "/ws/p2"})
	ps.Create(context.Background(), NewProject{Name: "p3", Workspace: "/ws/p3"})

	d := newProjectsDispatcher(t, ps)
	res, errObj := dispatchProjects(t, d, "ListRemoteAgentProjects", map[string]any{})
	if errObj != nil {
		t.Fatalf("ListRemoteAgentProjects returned error: %#v", errObj)
	}
	rows, ok := res["projects"].([]map[string]any)
	if !ok {
		t.Fatalf("projects = %T, want []map[string]any; res = %#v", res["projects"], res)
	}
	if len(rows) != 3 {
		t.Errorf("len(projects) = %d, want 3", len(rows))
	}
	if total, _ := res["total"].(int); total != 3 {
		t.Errorf("total = %d, want 3", total)
	}
	for _, r := range rows {
		if r["id"] == "" {
			t.Errorf("row missing id: %#v", r)
		}
		if r["name"] == "" {
			t.Errorf("row missing name: %#v", r)
		}
		if r["workspace"] == "" {
			t.Errorf("row missing workspace: %#v", r)
		}
		if _, ok := r["created_at"].(string); !ok {
			t.Errorf("row missing created_at: %#v", r)
		}
	}
}

// TestListRemoteAgentProjects_NoStore pins the
// nil-store path: dispatcher returns -32603 when no
// ProjectStore is configured.
func TestListRemoteAgentProjects_NoStore(t *testing.T) {
	d := newProjectsDispatcher(t, nil)
	_, errObj := dispatchProjects(t, d, "ListRemoteAgentProjects", map[string]any{})
	if errObj == nil {
		t.Fatalf("expected error envelope, got result")
	}
	if code := codeAsInt(t, errObj["code"]); code != -32603 {
		t.Errorf("code = %d, want -32603", code)
	}
}

// TestListRemoteAgentProjects_BadLimit pins the
// negative-limit → -32602 path.
func TestListRemoteAgentProjects_BadLimit(t *testing.T) {
	ps := NewMemProjectStore(nil)
	d := newProjectsDispatcher(t, ps)
	_, errObj := dispatchProjects(t, d, "ListRemoteAgentProjects", map[string]any{
		"limit": -1,
	})
	if errObj == nil {
		t.Fatalf("expected error envelope, got result")
	}
	if code := codeAsInt(t, errObj["code"]); code != -32602 {
		t.Errorf("code = %d, want -32602", code)
	}
}

// TestListRemoteAgentProjects_LimitTooLarge pins
// the upper-bound (1000) → -32602 path.
func TestListRemoteAgentProjects_LimitTooLarge(t *testing.T) {
	ps := NewMemProjectStore(nil)
	d := newProjectsDispatcher(t, ps)
	_, errObj := dispatchProjects(t, d, "ListRemoteAgentProjects", map[string]any{
		"limit": 5000,
	})
	if errObj == nil {
		t.Fatalf("expected error envelope, got result")
	}
	if code := codeAsInt(t, errObj["code"]); code != -32602 {
		t.Errorf("code = %d, want -32602", code)
	}
}

// TestListRemoteAgentProjects_Prefix pins the
// workspace-prefix filter: three rows, list with
// prefix "/ws/secret" returns only the one matching.
func TestListRemoteAgentProjects_Prefix(t *testing.T) {
	ps := NewMemProjectStore(nil)
	ps.Create(context.Background(), NewProject{Name: "p1", Workspace: "/ws/public"})
	ps.Create(context.Background(), NewProject{Name: "p2", Workspace: "/ws/secret/inner"})
	ps.Create(context.Background(), NewProject{Name: "p3", Workspace: "/other/p3"})

	d := newProjectsDispatcher(t, ps)
	res, errObj := dispatchProjects(t, d, "ListRemoteAgentProjects", map[string]any{
		"prefix": "/ws/secret",
	})
	if errObj != nil {
		t.Fatalf("err = %#v", errObj)
	}
	rows := res["projects"].([]map[string]any)
	if len(rows) != 1 {
		t.Fatalf("len(projects) = %d, want 1", len(rows))
	}
	if rows[0]["name"] != "p2" {
		t.Errorf("rows[0].name = %v, want p2", rows[0]["name"])
	}
}

// TestCreateRemoteAgentProject_Ok pins the
// happy-path create: result carries the new project's
// full metadata, and the in-memory store now has one
// row.
func TestCreateRemoteAgentProject_Ok(t *testing.T) {
	ps := NewMemProjectStore(nil)
	d := newProjectsDispatcher(t, ps)

	res, errObj := dispatchProjects(t, d, "CreateRemoteAgentProject", map[string]any{
		"name":           "fresh",
		"workspace":      "/ws/fresh",
		"default_model":  "alpha-model",
		"default_effort": "high",
	})
	if errObj != nil {
		t.Fatalf("err = %#v", errObj)
	}
	if res["id"] == "" {
		t.Errorf("id missing: %#v", res)
	}
	if res["name"] != "fresh" {
		t.Errorf("name = %v, want fresh", res["name"])
	}
	if res["workspace"] != "/ws/fresh" {
		t.Errorf("workspace = %v, want /ws/fresh", res["workspace"])
	}
	if res["default_model"] != "alpha-model" {
		t.Errorf("default_model = %v, want alpha-model", res["default_model"])
	}
	if res["default_effort"] != "high" {
		t.Errorf("default_effort = %v, want high", res["default_effort"])
	}
	if _, ok := res["created_at"].(string); !ok {
		t.Errorf("created_at missing or wrong type: %#v", res)
	}

	rows, _ := ps.List(context.Background(), "", 100)
	if len(rows) != 1 {
		t.Errorf("List len = %d, want 1", len(rows))
	}
}

// TestCreateRemoteAgentProject_Duplicate pins the
// duplicate-name → -32009 path.
func TestCreateRemoteAgentProject_Duplicate(t *testing.T) {
	ps := NewMemProjectStore(nil)
	d := newProjectsDispatcher(t, ps)
	if _, errObj := dispatchProjects(t, d, "CreateRemoteAgentProject", map[string]any{
		"name":      "collision",
		"workspace": "/ws/first",
	}); errObj != nil {
		t.Fatalf("first create err = %#v", errObj)
	}
	_, errObj := dispatchProjects(t, d, "CreateRemoteAgentProject", map[string]any{
		"name":      "collision",
		"workspace": "/ws/second",
	})
	if errObj == nil {
		t.Fatalf("expected error envelope, got result")
	}
	if code := codeAsInt(t, errObj["code"]); code != -32009 {
		t.Errorf("code = %d, want -32009", code)
	}
}

// TestCreateRemoteAgentProject_BadParams pins the
// empty-name → -32602 path.
func TestCreateRemoteAgentProject_BadParams_EmptyName(t *testing.T) {
	ps := NewMemProjectStore(nil)
	d := newProjectsDispatcher(t, ps)
	_, errObj := dispatchProjects(t, d, "CreateRemoteAgentProject", map[string]any{
		"name":      "",
		"workspace": "/ws/somewhere",
	})
	if errObj == nil {
		t.Fatalf("expected error envelope, got result")
	}
	if code := codeAsInt(t, errObj["code"]); code != -32602 {
		t.Errorf("code = %d, want -32602", code)
	}
}

// TestCreateRemoteAgentProject_BadParams_EmptyWorkspace pins the
// empty-workspace → -32602 path.
func TestCreateRemoteAgentProject_BadParams_EmptyWorkspace(t *testing.T) {
	ps := NewMemProjectStore(nil)
	d := newProjectsDispatcher(t, ps)
	_, errObj := dispatchProjects(t, d, "CreateRemoteAgentProject", map[string]any{
		"name":      "valid",
		"workspace": "  ",
	})
	if errObj == nil {
		t.Fatalf("expected error envelope, got result")
	}
	if code := codeAsInt(t, errObj["code"]); code != -32602 {
		t.Errorf("code = %d, want -32602", code)
	}
}

// TestCreateRemoteAgentProject_BadParams_BadEffort pins the
// invalid-effort → -32602 path.
func TestCreateRemoteAgentProject_BadParams_BadEffort(t *testing.T) {
	ps := NewMemProjectStore(nil)
	d := newProjectsDispatcher(t, ps)
	_, errObj := dispatchProjects(t, d, "CreateRemoteAgentProject", map[string]any{
		"name":           "valid",
		"workspace":      "/ws/valid",
		"default_effort": "ultra",
	})
	if errObj == nil {
		t.Fatalf("expected error envelope, got result")
	}
	if code := codeAsInt(t, errObj["code"]); code != -32602 {
		t.Errorf("code = %d, want -32602", code)
	}
}

// TestCreateRemoteAgentProject_PathNotAllowed pins
// the FSProvider rejection path: an FSProjectStore
// with a *WorkdirScope rejects workspaces outside the
// scope with -32007 (matches the rc-file-ops
// ErrPathNotAllowed envelope).
func TestCreateRemoteAgentProject_PathNotAllowed(t *testing.T) {
	dir := t.TempDir()
	scope := &WorkdirScope{Workspace: dir}
	store := NewFSProjectStore(t.TempDir(), scope, nil, nil)
	d := newProjectsDispatcher(t, store)
	_, errObj := dispatchProjects(t, d, "CreateRemoteAgentProject", map[string]any{
		"name":      "outside",
		"workspace": "/etc/passwd",
	})
	if errObj == nil {
		t.Fatalf("expected error envelope, got result")
	}
	if code := codeAsInt(t, errObj["code"]); code != -32007 {
		t.Errorf("code = %d, want -32007", code)
	}
}

// TestUpdateRemoteAgentProject_Ok pins the
// happy-path update: create → update → list shows
// the new field.
func TestUpdateRemoteAgentProject_Ok(t *testing.T) {
	ps := NewMemProjectStore(func() time.Time {
		// Tick so UpdatedAt > CreatedAt.
		return time.Now().UTC().Add(time.Millisecond)
	})
	d := newProjectsDispatcher(t, ps)
	res, errObj := dispatchProjects(t, d, "CreateRemoteAgentProject", map[string]any{
		"name":      "before",
		"workspace": "/ws/before",
	})
	if errObj != nil {
		t.Fatalf("create err = %#v", errObj)
	}
	id := res["id"].(string)

	updated, errObj := dispatchProjects(t, d, "UpdateRemoteAgentProject", map[string]any{
		"id":   id,
		"name": "after",
	})
	if errObj != nil {
		t.Fatalf("update err = %#v", errObj)
	}
	if updated["name"] != "after" {
		t.Errorf("updated.name = %v, want after", updated["name"])
	}

	listRes, errObj := dispatchProjects(t, d, "ListRemoteAgentProjects", map[string]any{})
	if errObj != nil {
		t.Fatalf("list err = %#v", errObj)
	}
	rows := listRes["projects"].([]map[string]any)
	if len(rows) != 1 {
		t.Fatalf("len(projects) = %d, want 1", len(rows))
	}
	if rows[0]["name"] != "after" {
		t.Errorf("rows[0].name = %v, want after", rows[0]["name"])
	}
}

// TestUpdateRemoteAgentProject_NotFound pins the
// unknown-id → -32004 path.
func TestUpdateRemoteAgentProject_NotFound(t *testing.T) {
	ps := NewMemProjectStore(nil)
	d := newProjectsDispatcher(t, ps)
	_, errObj := dispatchProjects(t, d, "UpdateRemoteAgentProject", map[string]any{
		"id":   "ghost-id",
		"name": "anything",
	})
	if errObj == nil {
		t.Fatalf("expected error envelope, got result")
	}
	if code := codeAsInt(t, errObj["code"]); code != -32004 {
		t.Errorf("code = %d, want -32004", code)
	}
}

// TestUpdateRemoteAgentProject_BadParams pins the
// missing-id → -32602 path.
func TestUpdateRemoteAgentProject_BadParams(t *testing.T) {
	ps := NewMemProjectStore(nil)
	d := newProjectsDispatcher(t, ps)
	_, errObj := dispatchProjects(t, d, "UpdateRemoteAgentProject", map[string]any{
		"name": "no-id-supplied",
	})
	if errObj == nil {
		t.Fatalf("expected error envelope, got result")
	}
	if code := codeAsInt(t, errObj["code"]); code != -32602 {
		t.Errorf("code = %d, want -32602", code)
	}
}

// TestUpdateRemoteAgentProject_PatchOnly pins the
// "leave name alone" semantic: a patch with only
// default_model leaves the name unchanged in the
// result envelope.
func TestUpdateRemoteAgentProject_PatchOnly(t *testing.T) {
	ps := NewMemProjectStore(nil)
	d := newProjectsDispatcher(t, ps)
	res, errObj := dispatchProjects(t, d, "CreateRemoteAgentProject", map[string]any{
		"name":      "stable",
		"workspace": "/ws/stable",
	})
	if errObj != nil {
		t.Fatalf("create err = %#v", errObj)
	}
	id := res["id"].(string)

	updated, errObj := dispatchProjects(t, d, "UpdateRemoteAgentProject", map[string]any{
		"id":            id,
		"default_model": "next-model",
	})
	if errObj != nil {
		t.Fatalf("update err = %#v", errObj)
	}
	if updated["name"] != "stable" {
		t.Errorf("updated.name = %v, want stable (unchanged)", updated["name"])
	}
	if updated["default_model"] != "next-model" {
		t.Errorf("updated.default_model = %v, want next-model", updated["default_model"])
	}
}

// TestUpdateRemoteAgentProject_RenameCollision pins
// the duplicate-name-on-rename → -32009 path.
func TestUpdateRemoteAgentProject_RenameCollision(t *testing.T) {
	ps := NewMemProjectStore(nil)
	d := newProjectsDispatcher(t, ps)
	// Two existing rows.
	resA, _ := dispatchProjects(t, d, "CreateRemoteAgentProject", map[string]any{
		"name":      "alpha",
		"workspace": "/ws/a",
	})
	resB, _ := dispatchProjects(t, d, "CreateRemoteAgentProject", map[string]any{
		"name":      "beta",
		"workspace": "/ws/b",
	})
	idA := resA["id"].(string)
	_ = resB["id"]

	// Rename A to "beta" → collision.
	_, errObj := dispatchProjects(t, d, "UpdateRemoteAgentProject", map[string]any{
		"id":   idA,
		"name": "beta",
	})
	if errObj == nil {
		t.Fatalf("expected error envelope, got result")
	}
	if code := codeAsInt(t, errObj["code"]); code != -32009 {
		t.Errorf("code = %d, want -32009", code)
	}
}

// readJSONFile is a tiny helper for the on-disk JSON
// layout test.
func readJSONFile(t *testing.T, path string) ([]byte, error) {
	t.Helper()
	return os.ReadFile(path)
}

// jsonMarshal is a thin wrapper around encoding/json
// so the test file doesn't need to import the stdlib
// package directly — keeps the import block small.
func jsonMarshal(v any) ([]byte, error) {
	return json.Marshal(v)
}
