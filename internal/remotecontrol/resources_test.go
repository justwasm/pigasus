// Package remotecontrol — resources_test.go: the dispatcher's
// round-trip against the resources domain.
//
// The domain's own tests live in internal/remotecontrol/resources;
// this file exercises the wire the dispatcher produces.
//
// Wire-level round-trips (ws adapter + JSON marshalling)
// live in remotetransport/tcp_test.go, mirroring the split
// the rc-file-ops cycle established.
package remotecontrol

import (
	"context"
	"testing"

	"github.com/justwasm/pigasus/internal/remotecontrol/projects"
	"github.com/justwasm/pigasus/internal/remotecontrol/resources"
)

// newResourcesDispatcher returns a Dispatcher with
// the supplied resources.Store + projects.Store
// pre-installed. The helper mirrors
// newProjectsDispatcher / newPluginsDispatcher /
// newTriggersDispatcher / newFastpushDispatcher.
//
// When rs is nil, NewDispatcher fabricates a fresh
// resources.MemStore (the nil-guard pattern rc-resources
// adds). Tests that want to assert "no store
// installed" pass nil and exercise the -32603 path;
// tests that want a specific store pass it directly.
//
// projects is bound to the resource store's Bind path
// so a test that exercises UpdateRemoteAgentProjectResources
// needs to pass a non-nil projects.Store. Tests that
// only exercise the read paths (ListProviders /
// ListResources) can pass nil projects.
func newResourcesDispatcher(t *testing.T, rs resources.Store, projectStore projects.Store) *Dispatcher {
	t.Helper()
	sess := NewSessionState("1.4.0")
	d, err := NewDispatcher(Config{
		Session:       sess,
		ResourceStore: rs,
		Projects:      projectStore,
	})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	return d
}

// dispatchResources sends a Resources request
// through the dispatcher and returns the
// (result, error) pair. Mirrors dispatchPlugins /
// dispatchTriggers / dispatchFastpush / dispatchProjects.
//
// nil params marshals to JSON `null` so the
// dispatcher's "len(params) > 0" guards see a
// present (but null) params block; jsonRPCFor
// takes a byte slice and appends verbatim, so we
// mirror the convention `[]byte("null")` rather
// than a missing-key shape — the brief pins
// `{"method": "ListRemoteAgentResourceProviders"}`
// as a params-less call that's accepted by the
// dispatcher (List has no required inputs).
func dispatchResources(t *testing.T, d *Dispatcher, method string, params any) (map[string]any, map[string]any) {
	t.Helper()
	var raw []byte
	switch v := params.(type) {
	case nil:
		raw = []byte("null")
	case []byte:
		raw = v
	default:
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

// TestListRemoteAgentResourceProviders_Ok pins the
// happy path: a fresh resources.MemStore seeded with
// the fixture set, the dispatcher returns all 4
// providers sorted by ProviderID ascending.
func TestListRemoteAgentResourceProviders_Ok(t *testing.T) {
	rs := resources.NewMemStore(nil, nil)
	d := newResourcesDispatcher(t, rs, nil)
	res, errObj := dispatchResources(t, d, "ListRemoteAgentResourceProviders", map[string]any{})
	if errObj != nil {
		t.Fatalf("ListRemoteAgentResourceProviders returned error: %#v", errObj)
	}
	providers, ok := res["providers"].([]resources.Provider)
	if !ok {
		t.Fatalf("providers = %T, want []resources.Provider; res = %#v", res["providers"], res)
	}
	if len(providers) != 4 {
		t.Errorf("len(providers) = %d, want 4", len(providers))
	}
	if total, _ := res["total"].(int); total != 4 {
		t.Errorf("total = %d, want 4", total)
	}
}

// TestListRemoteAgentResourceProviders_KindFilter
// pins the kind-filter path: filtering by "vcs"
// returns only github providers (the closed-set
// maps vcs → github in the fixture).
func TestListRemoteAgentResourceProviders_KindFilter(t *testing.T) {
	rs := resources.NewMemStore(nil, nil)
	d := newResourcesDispatcher(t, rs, nil)
	res, errObj := dispatchResources(t, d, "ListRemoteAgentResourceProviders", map[string]any{"kind": "vcs"})
	if errObj != nil {
		t.Fatalf("ListRemoteAgentResourceProviders returned error: %#v", errObj)
	}
	providers, ok := res["providers"].([]resources.Provider)
	if !ok {
		t.Fatalf("providers = %T", res["providers"])
	}
	if len(providers) != 1 {
		t.Errorf("vcs providers = %d, want 1", len(providers))
	}
	if providers[0].ProviderID != "github" {
		t.Errorf("vcs ProviderID = %q, want github", providers[0].ProviderID)
	}
}

// TestListRemoteAgentResourceProviders_BadKind pins
// the -32602 envelope: an unknown kind is rejected
// before the store call.
func TestListRemoteAgentResourceProviders_BadKind(t *testing.T) {
	rs := resources.NewMemStore(nil, nil)
	d := newResourcesDispatcher(t, rs, nil)
	_, errObj := dispatchResources(t, d, "ListRemoteAgentResourceProviders", map[string]any{"kind": "unknown"})
	if errObj == nil {
		t.Fatalf("ListRemoteAgentResourceProviders should have returned an error")
	}
	if code, _ := errObj["code"].(int); code != -32602 {
		t.Errorf("code = %d, want -32602", code)
	}
}

// TestListRemoteAgentResourceProviders_NoStore pins
// the -32603 envelope: a dispatcher built without a
// resources.Store rejects the call. Mirrors
// TestListRemoteAgentPlugins_NoStore — the
// dispatcher fabricates a fresh resources.MemStore
// by default, so the test clears the field after
// construction.
func TestListRemoteAgentResourceProviders_NoStore(t *testing.T) {
	sess := NewSessionState("1.4.0")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	d.cfg.ResourceStore = nil
	_, errObj := dispatchResources(t, d, "ListRemoteAgentResourceProviders", nil)
	if errObj == nil {
		t.Fatalf("ListRemoteAgentResourceProviders should have returned an error")
	}
	if code, _ := errObj["code"].(int); code != -32603 {
		t.Errorf("code = %d, want -32603", code)
	}
}

// TestListRemoteAgentAvailableResources_Ok pins the
// happy path: a fresh resources.MemStore returns all
// 12 resources.
func TestListRemoteAgentAvailableResources_Ok(t *testing.T) {
	rs := resources.NewMemStore(nil, nil)
	d := newResourcesDispatcher(t, rs, nil)
	res, errObj := dispatchResources(t, d, "ListRemoteAgentAvailableResources", map[string]any{})
	if errObj != nil {
		t.Fatalf("ListRemoteAgentAvailableResources returned error: %#v", errObj)
	}
	resources, ok := res["resources"].([]resources.Resource)
	if !ok {
		t.Fatalf("resources = %T, want []resources.Resource; res = %#v", res["resources"], res)
	}
	if len(resources) != 12 {
		t.Errorf("len(resources) = %d, want 12", len(resources))
	}
}

// TestListRemoteAgentAvailableResources_ProviderFilter
// pins the provider_id filter: filtering by github
// returns the 3 github resources.
func TestListRemoteAgentAvailableResources_ProviderFilter(t *testing.T) {
	rs := resources.NewMemStore(nil, nil)
	d := newResourcesDispatcher(t, rs, nil)
	res, errObj := dispatchResources(t, d, "ListRemoteAgentAvailableResources", map[string]any{"provider_id": "github"})
	if errObj != nil {
		t.Fatalf("ListRemoteAgentAvailableResources returned error: %#v", errObj)
	}
	resources, ok := res["resources"].([]resources.Resource)
	if !ok {
		t.Fatalf("resources = %T", res["resources"])
	}
	if len(resources) != 3 {
		t.Errorf("github resources = %d, want 3", len(resources))
	}
}

// TestListRemoteAgentAvailableResources_UnknownProvider
// pins the -32602 envelope: an unknown provider_id
// filter is rejected before the store call.
func TestListRemoteAgentAvailableResources_UnknownProvider(t *testing.T) {
	rs := resources.NewMemStore(nil, nil)
	d := newResourcesDispatcher(t, rs, nil)
	_, errObj := dispatchResources(t, d, "ListRemoteAgentAvailableResources", map[string]any{"provider_id": "no_such"})
	if errObj == nil {
		t.Fatalf("ListRemoteAgentAvailableResources should have returned an error")
	}
	if code, _ := errObj["code"].(int); code != -32602 {
		t.Errorf("code = %d, want -32602", code)
	}
}

// TestUpdateRemoteAgentProjectResources_Ok pins the
// happy path: bind one known resource to a fresh
// project, the response envelope carries
// ok=true, bound=1, unbound=0.
func TestUpdateRemoteAgentProjectResources_Ok(t *testing.T) {
	ps := projects.NewMemStore(nil)
	row, err := ps.Create(context.Background(), projects.Create{Name: "p1", Workspace: "/ws/p1"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	rs := resources.NewMemStore(nil, ps)
	d := newResourcesDispatcher(t, rs, ps)

	res, errObj := dispatchResources(t, d, "UpdateRemoteAgentProjectResources", map[string]any{
		"project_id":   row.ID,
		"resource_ids": []string{"github:openagy/openagy"},
	})
	if errObj != nil {
		t.Fatalf("UpdateRemoteAgentProjectResources returned error: %#v", errObj)
	}
	if got := res["ok"]; got != true {
		t.Errorf("ok = %v, want true", got)
	}
	if got := res["project_id"]; got != row.ID {
		t.Errorf("project_id = %v, want %q", got, row.ID)
	}
	if got := res["bound_count"]; got != 1 {
		t.Errorf("bound_count = %v, want 1", got)
	}
	if got := res["unbound_count"]; got != 0 {
		t.Errorf("unbound_count = %v, want 0", got)
	}
	if _, ok := res["updated_at"].(string); !ok {
		t.Errorf("updated_at = %T, want string", res["updated_at"])
	}
}

// TestUpdateRemoteAgentProjectResources_UnknownProject
// pins the -32004 envelope: an unknown project_id
// returns the project's not-found envelope.
func TestUpdateRemoteAgentProjectResources_UnknownProject(t *testing.T) {
	ps := projects.NewMemStore(nil)
	rs := resources.NewMemStore(nil, ps)
	d := newResourcesDispatcher(t, rs, ps)
	_, errObj := dispatchResources(t, d, "UpdateRemoteAgentProjectResources", map[string]any{
		"project_id":   "no_such",
		"resource_ids": []string{"github:openagy/openagy"},
	})
	if errObj == nil {
		t.Fatalf("UpdateRemoteAgentProjectResources should have returned an error")
	}
	if code, _ := errObj["code"].(int); code != -32004 {
		t.Errorf("code = %d, want -32004", code)
	}
}

// TestUpdateRemoteAgentProjectResources_BadParams pins
// the -32602 envelope: an empty project_id is rejected.
func TestUpdateRemoteAgentProjectResources_BadParams(t *testing.T) {
	ps := projects.NewMemStore(nil)
	rs := resources.NewMemStore(nil, ps)
	d := newResourcesDispatcher(t, rs, ps)
	_, errObj := dispatchResources(t, d, "UpdateRemoteAgentProjectResources", map[string]any{
		"project_id":   "",
		"resource_ids": []string{"github:openagy/openagy"},
	})
	if errObj == nil {
		t.Fatalf("UpdateRemoteAgentProjectResources should have returned an error")
	}
	if code, _ := errObj["code"].(int); code != -32602 {
		t.Errorf("code = %d, want -32602", code)
	}
}

// TestUpdateRemoteAgentProjectResources_PersistsToProject
// pins the on-disk persistence: a successful Bind
// is reflected on the next Get.
func TestUpdateRemoteAgentProjectResources_PersistsToProject(t *testing.T) {
	ps := projects.NewMemStore(nil)
	row, err := ps.Create(context.Background(), projects.Create{Name: "p1", Workspace: "/ws/p1"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	rs := resources.NewMemStore(nil, ps)
	d := newResourcesDispatcher(t, rs, ps)

	_, errObj := dispatchResources(t, d, "UpdateRemoteAgentProjectResources", map[string]any{
		"project_id":   row.ID,
		"resource_ids": []string{"github:openagy/openagy", "sentry:production"},
	})
	if errObj != nil {
		t.Fatalf("UpdateRemoteAgentProjectResources: %#v", errObj)
	}

	got, err := ps.Get(context.Background(), row.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.Resources) != 2 {
		t.Errorf("Resources len = %d, want 2", len(got.Resources))
	}
	// Bound ids are sorted ascending by the store.
	want := []string{"github:openagy/openagy", "sentry:production"}
	for i, r := range want {
		if got.Resources[i] != r {
			t.Errorf("Resources[%d] = %q, want %q", i, got.Resources[i], r)
		}
	}
}

// TestProject_ResourcesOnListEnvelope pins the
// cycle-rc-resources extension to
// projectSliceToWire: a project that carries
// bound resources surfaces them under the
// `resources` key on the wire envelope.
func TestProject_ResourcesOnListEnvelope(t *testing.T) {
	ps := projects.NewMemStore(nil)
	row, err := ps.Create(context.Background(), projects.Create{Name: "p1", Workspace: "/ws/p1"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// Bind a couple of resources.
	rs := resources.NewMemStore(nil, ps)
	if _, _, err := rs.Bind(context.Background(), row.ID, []string{"github:openagy/openagy"}); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	// List the projects via the dispatcher.
	d := newResourcesDispatcher(t, rs, ps)
	res, errObj := dispatchProjects(t, d, "ListRemoteAgentProjects", map[string]any{})
	if errObj != nil {
		t.Fatalf("ListRemoteAgentProjects: %#v", errObj)
	}
	rows, ok := res["projects"].([]map[string]any)
	if !ok {
		t.Fatalf("projects = %T, want []map[string]any", res["projects"])
	}
	if len(rows) != 1 {
		t.Fatalf("len(projects) = %d, want 1", len(rows))
	}
	resources, ok := rows[0]["resources"].([]string)
	if !ok {
		t.Fatalf("rows[0].resources = %T, want []string", rows[0]["resources"])
	}
	if len(resources) != 1 || resources[0] != "github:openagy/openagy" {
		t.Errorf("resources = %v, want [github:openagy/openagy]", resources)
	}
}

// TestProject_NoResourcesKeyWhenNil pins the
// back-compat path: a project whose Resources
// field is nil (the default state after Create)
// surfaces NO `resources` key on the wire
// envelope (so a pre-rc-resources dashboard parses
// the envelope unchanged).
func TestProject_NoResourcesKeyWhenNil(t *testing.T) {
	ps := projects.NewMemStore(nil)
	if _, err := ps.Create(context.Background(), projects.Create{Name: "p1", Workspace: "/ws/p1"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	rs := resources.NewMemStore(nil, ps)
	d := newResourcesDispatcher(t, rs, ps)
	res, errObj := dispatchProjects(t, d, "ListRemoteAgentProjects", map[string]any{})
	if errObj != nil {
		t.Fatalf("ListRemoteAgentProjects: %#v", errObj)
	}
	rows, ok := res["projects"].([]map[string]any)
	if !ok {
		t.Fatalf("projects = %T", res["projects"])
	}
	if _, present := rows[0]["resources"]; present {
		t.Errorf("rows[0] should not carry a `resources` key when project.Resources is nil")
	}
}
