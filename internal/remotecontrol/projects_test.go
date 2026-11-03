// Package remotecontrol — projects_test.go: the dispatcher's
// round-trip against the projects domain.
//
// The domain's own tests live in internal/remotecontrol/projects;
// this file exercises the wire the dispatcher produces.
//
// Wire-level round-trips (ws adapter + JSON marshalling)
// live in remotetransport/tcp_test.go,
// mirroring the split the rc-file-ops cycle established.
package remotecontrol

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/remotecontrol/files"
	"github.com/justwasm/pigasus/internal/remotecontrol/projects"
)

// newProjectsDispatcher returns a Dispatcher with the
// supplied projects.Store pre-installed. The helper is
// intentionally small (no FS provider, no list backend)
// because the three project methods reach into the
// projects.Store seam directly.
func newProjectsDispatcher(t *testing.T, ps projects.Store) *Dispatcher {
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

// TestListRemoteAgentProjects_Ok exercises the
// happy path: a fresh projects.MemStore, create three
// rows, list returns three rows in the result
// envelope.
func TestListRemoteAgentProjects_Ok(t *testing.T) {
	ps := projects.NewMemStore(nil)
	ps.Create(context.Background(), projects.Create{Name: "p1", Workspace: "/ws/p1"})
	ps.Create(context.Background(), projects.Create{Name: "p2", Workspace: "/ws/p2"})
	ps.Create(context.Background(), projects.Create{Name: "p3", Workspace: "/ws/p3"})

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
// projects.Store is configured.
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
	ps := projects.NewMemStore(nil)
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
	ps := projects.NewMemStore(nil)
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
	ps := projects.NewMemStore(nil)
	ps.Create(context.Background(), projects.Create{Name: "p1", Workspace: "/ws/public"})
	ps.Create(context.Background(), projects.Create{Name: "p2", Workspace: "/ws/secret/inner"})
	ps.Create(context.Background(), projects.Create{Name: "p3", Workspace: "/other/p3"})

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
	ps := projects.NewMemStore(nil)
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
	ps := projects.NewMemStore(nil)
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
	ps := projects.NewMemStore(nil)
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
	ps := projects.NewMemStore(nil)
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
	ps := projects.NewMemStore(nil)
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
// the FSProvider rejection path: an projects.FSStore
// with a *files.WorkdirScope rejects workspaces outside the
// scope with -32007 (matches the rc-file-ops
// ErrPathNotAllowed envelope).
func TestCreateRemoteAgentProject_PathNotAllowed(t *testing.T) {
	dir := t.TempDir()
	scope := &files.WorkdirScope{Workspace: dir}
	store := projects.NewFSStore(t.TempDir(), scope, nil, nil)
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
	ps := projects.NewMemStore(func() time.Time {
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
	ps := projects.NewMemStore(nil)
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
	ps := projects.NewMemStore(nil)
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
	ps := projects.NewMemStore(nil)
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
	ps := projects.NewMemStore(nil)
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

// jsonMarshal is a thin wrapper around encoding/json
// so the test file doesn't need to import the stdlib
// package directly — keeps the import block small.
func jsonMarshal(v any) ([]byte, error) {
	return json.Marshal(v)
}
