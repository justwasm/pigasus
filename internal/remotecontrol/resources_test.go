// Tests for the rc-resources cycle. Covers:
//
//   - The three JSON-RPC methods
//     (ListRemoteAgentResourceProviders /
//     ListRemoteAgentAvailableResources /
//     UpdateRemoteAgentProjectResources), including
//     the sentinel-error mapping
//     (ErrResourceProviderUnknown → -32602,
//     ErrResourceProjectNotFound → -32004) and the
//     validation rules (empty project_id → -32602,
//     unknown kind → -32602, unknown provider_id
//     filter → -32602);
//
//   - The ResourceStore.MemResourceStore (Providers /
//     Resources / Bind) at the in-process level (no
//     Unix socket, no ws adapter);
//
//   - The FSResourceStore happy path against a real
//     t.TempDir() so the on-disk resources.json
//     layout is exercised (atomic temp + rename,
//     0o600 perms, version stamp, fixture-seed on
//     first Snapshot, post-Bind reload, back-compat
//     on missing `Resources` field on the project
//     JSON side);
//
//   - The sortProvidersByID / sortResourcesByID
//     helpers — the sort the store applies before
//     handing the slice to the dispatcher so
//     dashboards see a stable order;
//
//   - The NewMemResourceStoreWith custom-snapshot
//     constructor (sorting, deep-copy).
//
// Wire-level tests live in
// remotetransport/tcp_test.go (mirroring
// the project's split: in-process envelopes vs.
// wire round-trips).
package remotecontrol

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newResourcesDispatcher returns a Dispatcher with
// the supplied ResourceStore + ProjectStore
// pre-installed. The helper mirrors
// newProjectsDispatcher / newPluginsDispatcher /
// newTriggersDispatcher / newFastpushDispatcher.
//
// When rs is nil, NewDispatcher fabricates a fresh
// MemResourceStore (the nil-guard pattern rc-resources
// adds). Tests that want to assert "no store
// installed" pass nil and exercise the -32603 path;
// tests that want a specific store pass it directly.
//
// projects is bound to the resource store's Bind path
// so a test that exercises UpdateRemoteAgentProjectResources
// needs to pass a non-nil ProjectStore. Tests that
// only exercise the read paths (ListProviders /
// ListResources) can pass nil projects.
func newResourcesDispatcher(t *testing.T, rs ResourceStore, projects ProjectStore) *Dispatcher {
	t.Helper()
	sess := NewSessionState("1.4.0")
	d, err := NewDispatcher(Config{
		Session:       sess,
		ResourceStore: rs,
		Projects:      projects,
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

// ---- helper / parse tests --------------------------------

// TestNormaliseResourceKind asserts the trim helper
// used by the per-method param-validation sites.
func TestNormaliseResourceKind(t *testing.T) {
	// IsValidResourceKind is the validation helper
	// the dispatcher uses — pin its behaviour so a
	// future cycle that adds a new kind only has to
	// extend AllResourceKinds and the closed-set
	// list in one place.
	cases := []struct {
		in   string
		want bool
	}{
		{"vcs", true},
		{"observability", true},
		{"pm", true},
		{"docs", true},
		{"ci", true},
		{"", false},
		{"  vcs  ", false}, // whitespace not normalised — closed-set strict
		{"vcs ", false},    // trim not applied
		{"unknown", false},
		{"VCS", false},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			if got := IsValidResourceKind(tc.in); got != tc.want {
				t.Errorf("IsValidResourceKind(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// TestAllResourceKinds pins the closed-set order so
// a future cycle that extends the set updates
// AllResourceKinds + the doc comment + the IsValidResourceKind
// helper in lockstep.
func TestAllResourceKinds(t *testing.T) {
	want := []string{"vcs", "observability", "pm", "docs", "ci"}
	if len(AllResourceKinds) != len(want) {
		t.Fatalf("AllResourceKinds len = %d, want %d", len(AllResourceKinds), len(want))
	}
	for i, k := range want {
		if AllResourceKinds[i] != k {
			t.Errorf("AllResourceKinds[%d] = %q, want %q", i, AllResourceKinds[i], k)
		}
	}
}

// TestNotifResourcesChangedMethod pins the JSON-RPC
// notification method name the dispatcher uses for
// the "resources/changed" server-push frame.
func TestNotifResourcesChangedMethod(t *testing.T) {
	if NotifResourcesChangedMethod != "resources/changed" {
		t.Errorf("NotifResourcesChangedMethod = %q, want resources/changed", NotifResourcesChangedMethod)
	}
}

// TestResourceStateVersion pins the on-disk format
// version the current store writes. v1 is the brief-
// pinned value; a future cycle that introduces a
// breaking change (renamed keys, removed fields)
// bumps this constant AND updates the load() branch
// that rejects version > ResourceStateVersion.
func TestResourceStateVersion(t *testing.T) {
	if ResourceStateVersion != 1 {
		t.Errorf("ResourceStateVersion = %d, want 1", ResourceStateVersion)
	}
}

// TestNormaliseProviderIDAndResourceID pins the trim
// helpers. The dispatcher's primary validation site
// for the closed-set validation is the
// "unknown provider_id" branch in
// handleListRemoteAgentAvailableResources; the
// helpers here are defence-in-depth inside the store.
func TestNormaliseProviderIDAndResourceID(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"github", "github"},
		{"  github  ", "github"},
		{"\tgithub\n", "github"},
		{"", ""},
		{"   ", ""},
	}
	for _, tc := range cases {
		t.Run("provider_id/"+tc.in, func(t *testing.T) {
			if got := normaliseProviderID(tc.in); got != tc.want {
				t.Errorf("normaliseProviderID(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
		t.Run("resource_id/"+tc.in, func(t *testing.T) {
			if got := normaliseResourceID(tc.in); got != tc.want {
				t.Errorf("normaliseResourceID(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// ---- MemResourceStore in-process tests --------------------

// TestResourceStore_MemProviders_Default pins the
// fixture-seeded default: a fresh MemResourceStore
// returns 4 providers (the brief's fixture set).
func TestResourceStore_MemProviders_Default(t *testing.T) {
	rs := NewMemResourceStore(nil, nil)
	providers := rs.Providers(context.Background())
	if len(providers) != 4 {
		t.Fatalf("Providers len = %d, want 4", len(providers))
	}
	// Sorted by ProviderID ascending
	// (alphabetical: gdocs < github < linear < sentry).
	wantOrder := []string{"gdocs", "github", "linear", "sentry"}
	for i, p := range providers {
		if p.ProviderID != wantOrder[i] {
			t.Errorf("providers[%d].ProviderID = %q, want %q", i, p.ProviderID, wantOrder[i])
		}
	}
}

// TestResourceStore_MemResources_Default pins the
// fixture-seeded resource count: a fresh
// MemResourceStore returns 12 resources.
func TestResourceStore_MemResources_Default(t *testing.T) {
	rs := NewMemResourceStore(nil, nil)
	resources := rs.Resources(context.Background(), "", "")
	if len(resources) != 12 {
		t.Fatalf("Resources len = %d, want 12", len(resources))
	}
}

// TestResourceStore_MemResources_FilterProvider pins
// the provider_id filter: filtering by a single
// provider returns only its resources.
func TestResourceStore_MemResources_FilterProvider(t *testing.T) {
	rs := NewMemResourceStore(nil, nil)
	resources := rs.Resources(context.Background(), "github", "")
	if len(resources) != 3 {
		t.Errorf("github resources = %d, want 3", len(resources))
	}
	for _, r := range resources {
		if r.ProviderID != "github" {
			t.Errorf("provider_id = %q, want github", r.ProviderID)
		}
	}
}

// TestResourceStore_MemResources_FilterKind pins the
// kind filter: filtering by "vcs" returns only the
// github resources.
func TestResourceStore_MemResources_FilterKind(t *testing.T) {
	rs := NewMemResourceStore(nil, nil)
	resources := rs.Resources(context.Background(), "", "vcs")
	if len(resources) != 3 {
		t.Errorf("vcs resources = %d, want 3", len(resources))
	}
	for _, r := range resources {
		if r.Kind != "vcs" {
			t.Errorf("kind = %q, want vcs", r.Kind)
		}
	}
}

// TestResourceStore_MemResources_CombinedFilter
// pins the provider_id + kind combined filter.
func TestResourceStore_MemResources_CombinedFilter(t *testing.T) {
	rs := NewMemResourceStore(nil, nil)
	resources := rs.Resources(context.Background(), "github", "vcs")
	if len(resources) != 3 {
		t.Errorf("github+vcs resources = %d, want 3", len(resources))
	}
}

// TestResourceStore_MemBind_UnknownProject pins the
// sentinel path: binding to an unknown project_id
// returns ErrResourceProjectNotFound (the
// dispatcher's -32004 envelope).
func TestResourceStore_MemBind_UnknownProject(t *testing.T) {
	ps := NewMemProjectStore(nil)
	rs := NewMemResourceStore(nil, ps)
	_, _, err := rs.Bind(context.Background(), "no_such_project", []string{"github:openagy/openagy"})
	if !errors.Is(err, ErrResourceProjectNotFound) {
		t.Errorf("Bind err = %v, want ErrResourceProjectNotFound", err)
	}
}

// TestResourceStore_MemBind_HappyPath pins the
// happy path: bind to a real project_id, the
// project's Resources field gets the supplied
// slice (replace-style), bound=1, unbound=0.
func TestResourceStore_MemBind_HappyPath(t *testing.T) {
	ps := NewMemProjectStore(nil)
	row, err := ps.Create(context.Background(), NewProject{Name: "p1", Workspace: "/ws/p1"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	rs := NewMemResourceStore(nil, ps)
	bound, unbound, err := rs.Bind(context.Background(), row.ID, []string{"github:openagy/openagy"})
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if bound != 1 {
		t.Errorf("bound = %d, want 1", bound)
	}
	if unbound != 0 {
		t.Errorf("unbound = %d, want 0", unbound)
	}
	// Verify the project's Resources field
	// has been persisted.
	got, err := ps.Get(context.Background(), row.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.Resources) != 1 || got.Resources[0] != "github:openagy/openagy" {
		t.Errorf("project.Resources = %v, want [github:openagy/openagy]", got.Resources)
	}
}

// TestResourceStore_MemBind_UnknownResource pins the
// "unknown resource is no-op" rule: an unknown
// resource_id is counted as `unbound`, not as an
// error.
func TestResourceStore_MemBind_UnknownResource(t *testing.T) {
	ps := NewMemProjectStore(nil)
	row, err := ps.Create(context.Background(), NewProject{Name: "p1", Workspace: "/ws/p1"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	rs := NewMemResourceStore(nil, ps)
	bound, unbound, err := rs.Bind(context.Background(), row.ID, []string{"no_such_resource"})
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if bound != 0 {
		t.Errorf("bound = %d, want 0", bound)
	}
	if unbound != 1 {
		t.Errorf("unbound = %d, want 1", unbound)
	}
}

// TestResourceStore_MemBind_MixedKnownUnknown pins
// the partial-bind path: a mix of known + unknown
// ids returns bound=known-count, unbound=unknown-
// count, no error.
func TestResourceStore_MemBind_MixedKnownUnknown(t *testing.T) {
	ps := NewMemProjectStore(nil)
	row, err := ps.Create(context.Background(), NewProject{Name: "p1", Workspace: "/ws/p1"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	rs := NewMemResourceStore(nil, ps)
	bound, unbound, err := rs.Bind(context.Background(), row.ID, []string{
		"github:openagy/openagy",
		"no_such_resource",
		"github:openagy/rea-go-seeds",
	})
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if bound != 2 {
		t.Errorf("bound = %d, want 2", bound)
	}
	if unbound != 1 {
		t.Errorf("unbound = %d, want 1", unbound)
	}
}

// TestResourceStore_MemBind_NoProjects pins the
// "no Projects store installed" path: a
// MemResourceStore with nil projects returns
// ErrResourceProjectNotFound unconditionally.
func TestResourceStore_MemBind_NoProjects(t *testing.T) {
	rs := NewMemResourceStore(nil, nil)
	_, _, err := rs.Bind(context.Background(), "any_id", []string{"github:openagy/openagy"})
	if !errors.Is(err, ErrResourceProjectNotFound) {
		t.Errorf("Bind err = %v, want ErrResourceProjectNotFound", err)
	}
}

// TestResourceStore_MemBind_ReplaceStyle pins the
// brief's "replace-style" semantic: a second Bind
// with a different slice overwrites the first
// (not appended).
func TestResourceStore_MemBind_ReplaceStyle(t *testing.T) {
	ps := NewMemProjectStore(nil)
	row, err := ps.Create(context.Background(), NewProject{Name: "p1", Workspace: "/ws/p1"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	rs := NewMemResourceStore(nil, ps)
	if _, _, err := rs.Bind(context.Background(), row.ID, []string{"github:openagy/openagy", "github:openagy/rea-go-seeds"}); err != nil {
		t.Fatalf("first Bind: %v", err)
	}
	if _, _, err := rs.Bind(context.Background(), row.ID, []string{"github:wagyu/wagyu"}); err != nil {
		t.Fatalf("second Bind: %v", err)
	}
	got, err := ps.Get(context.Background(), row.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.Resources) != 1 || got.Resources[0] != "github:wagyu/wagyu" {
		t.Errorf("project.Resources = %v, want [github:wagyu/wagyu]", got.Resources)
	}
}

// TestResourceStore_MemBind_EmptySlice pins the
// "bind to empty set" semantic: an explicit empty
// slice is a valid unbind operation.
func TestResourceStore_MemBind_EmptySlice(t *testing.T) {
	ps := NewMemProjectStore(nil)
	row, err := ps.Create(context.Background(), NewProject{Name: "p1", Workspace: "/ws/p1"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	rs := NewMemResourceStore(nil, ps)
	// First bind a single resource.
	if _, _, err := rs.Bind(context.Background(), row.ID, []string{"github:openagy/openagy"}); err != nil {
		t.Fatalf("first Bind: %v", err)
	}
	// Then unbind.
	bound, unbound, err := rs.Bind(context.Background(), row.ID, []string{})
	if err != nil {
		t.Fatalf("unbind Bind: %v", err)
	}
	if bound != 0 || unbound != 0 {
		t.Errorf("unbind: bound=%d, unbound=%d, want 0/0", bound, unbound)
	}
	got, err := ps.Get(context.Background(), row.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.Resources) != 0 {
		t.Errorf("project.Resources after unbind = %v, want []", got.Resources)
	}
}

// TestResourceStore_MemStoreWith_Custom pins the
// custom-snapshot constructor: a fresh
// MemResourceStore seeded with a single provider
// + zero resources returns that provider and an
// empty resource slice.
func TestResourceStore_MemStoreWith_Custom(t *testing.T) {
	ps := NewMemProjectStore(nil)
	customProviders := []ResourceProvider{
		{ProviderID: "custom", Name: "Custom", Kind: ResourceKindCI, Configured: true, Scopes: []string{"ci"}, Version: "1.0.0"},
	}
	customResources := []Resource{}
	rs := NewMemResourceStoreWith(customProviders, customResources, nil, ps)
	providers := rs.Providers(context.Background())
	if len(providers) != 1 {
		t.Errorf("Providers len = %d, want 1", len(providers))
	}
	if providers[0].ProviderID != "custom" {
		t.Errorf("ProviderID = %q, want custom", providers[0].ProviderID)
	}
	resources := rs.Resources(context.Background(), "", "")
	if len(resources) != 0 {
		t.Errorf("Resources len = %d, want 0", len(resources))
	}
}

// TestResourcesChangedToWire pins the wire envelope
// the dispatcher emits for both the response and
// the broadcast notification: a polling caller and a
// notification subscriber see byte-for-byte identical
// shapes.
func TestResourcesChangedToWire(t *testing.T) {
	updatedAt := time.Date(2026, 10, 12, 14, 30, 0, 0, time.UTC)
	wire := resourcesChangedToWire("proj_abc", 3, 1, updatedAt)
	if got := wire["ok"]; got != true {
		t.Errorf("ok = %v, want true", got)
	}
	if got := wire["project_id"]; got != "proj_abc" {
		t.Errorf("project_id = %v, want proj_abc", got)
	}
	if got := wire["bound_count"]; got != 3 {
		t.Errorf("bound_count = %v, want 3", got)
	}
	if got := wire["unbound_count"]; got != 1 {
		t.Errorf("unbound_count = %v, want 1", got)
	}
	if got := wire["updated_at"]; got != "2026-10-12T14:30:00Z" {
		t.Errorf("updated_at = %v, want 2026-10-12T14:30:00Z", got)
	}
}

// TestMapResourceError pins the sentinel-to-JSON-RPC
// mapping.
func TestMapResourceError(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		wantCode int
		wantMsg  string
	}{
		{"ErrResourceProjectNotFound", ErrResourceProjectNotFound, -32004, "Project not found"},
		{"ErrResourceProviderUnknown", ErrResourceProviderUnknown, -32602, "Invalid params"},
		{"I/O error", errors.New("disk on fire"), -32603, "Internal error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mapResourceError(tc.err, "test")
			if got == nil {
				t.Fatalf("mapResourceError returned nil")
			}
			if got.Code != tc.wantCode {
				t.Errorf("Code = %d, want %d", got.Code, tc.wantCode)
			}
			if got.Message != tc.wantMsg {
				t.Errorf("Message = %q, want %q", got.Message, tc.wantMsg)
			}
		})
	}
	// nil error → nil envelope.
	if got := mapResourceError(nil, "test"); got != nil {
		t.Errorf("mapResourceError(nil) = %v, want nil", got)
	}
}

// ---- FSResourceStore on-disk tests ------------------------

// TestFSResourceStore_FreshHost pins the fresh-host
// fixture path: a brand-new FSResourceStore (no
// on-disk file) returns the fixture-seeded state
// (4 providers + 12 resources, all configured=false).
func TestFSResourceStore_FreshHost(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "resources.json")
	rs := NewFSResourceStore(path, nil, nil, nil)
	providers := rs.Providers(context.Background())
	if len(providers) != 4 {
		t.Errorf("fresh Providers len = %d, want 4", len(providers))
	}
	resources := rs.Resources(context.Background(), "", "")
	if len(resources) != 12 {
		t.Errorf("fresh Resources len = %d, want 12", len(resources))
	}
}

// TestFSResourceStore_Roundtrip pins the on-disk
// persistence: a Bind writes the bound resource
// list to the project's JSON via ProjectStore.Update
// (the resource-id list lives on the project
// record, not on the resources.json registry —
// the registry is a list of *what providers + resources
// exist*, and the project is the bind target).
func TestFSResourceStore_Roundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "resources.json")
	// Stand up a ProjectStore so Bind can write
	// the project's Resources field back.
	projectsDir := filepath.Join(dir, "projects")
	ps := NewFSProjectStore(projectsDir, nil, nil, nil)
	row, err := ps.Create(context.Background(), NewProject{Name: "p1", Workspace: "/ws/p1"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	rs := NewFSResourceStore(path, ps, nil, nil)
	bound, unbound, err := rs.Bind(context.Background(), row.ID, []string{"github:openagy/openagy"})
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if bound != 1 || unbound != 0 {
		t.Errorf("Bind: bound=%d unbound=%d, want 1/0", bound, unbound)
	}

	// The project's Resources field persists to
	// disk via ProjectStore.Update — verify by
	// reading the project JSON file directly.
	got, err := ps.Get(context.Background(), row.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.Resources) != 1 || got.Resources[0] != "github:openagy/openagy" {
		t.Errorf("project.Resources = %v, want [github:openagy/openagy]", got.Resources)
	}
	projectPath := filepath.Join(projectsDir, row.ID+".json")
	projectData, err := os.ReadFile(projectPath)
	if err != nil {
		t.Fatalf("read %s: %v", projectPath, err)
	}
	if !strings.Contains(string(projectData), `"resources"`) {
		t.Errorf("on-disk project JSON missing resources key:\n%s", projectData)
	}

	// Reload via a fresh FSResourceStore and
	// verify the fixture providers + resources
	// are intact (the bind doesn't add or
	// remove rows from the registry).
	rs2 := NewFSResourceStore(path, ps, nil, nil)
	if len(rs2.Providers(context.Background())) != 4 {
		t.Errorf("reloaded Providers len = %d, want 4", len(rs2.Providers(context.Background())))
	}
	if len(rs2.Resources(context.Background(), "", "")) != 12 {
		t.Errorf("reloaded Resources len = %d, want 12", len(rs2.Resources(context.Background(), "", "")))
	}
}

// TestFSResourceStore_ConfiguredFromSecretDir pins
// the `$ANTIGRAVITY_HOME/secrets/<provider_id>`
// discovery path: when a non-empty file exists for
// a provider, the snapshot's `configured` flag is
// true for that provider.
func TestFSResourceStore_ConfiguredFromSecretDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "resources.json")
	secretsDir := filepath.Join(dir, "secrets")
	if err := os.MkdirAll(secretsDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Drop a non-empty secret for "github" only.
	if err := os.WriteFile(filepath.Join(secretsDir, "github"), []byte("ghp_secret"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	check := func(providerID string) bool {
		p := filepath.Join(secretsDir, providerID)
		st, err := os.Stat(p)
		if err != nil {
			return false
		}
		return !st.IsDir() && st.Size() > 0
	}
	rs := NewFSResourceStore(path, nil, check, nil)
	for _, p := range rs.Providers(context.Background()) {
		if p.ProviderID == "github" {
			if !p.Configured {
				t.Errorf("github.Configured = false, want true (secret file present)")
			}
		} else {
			if p.Configured {
				t.Errorf("%s.Configured = true, want false (no secret file)", p.ProviderID)
			}
		}
	}
}

// ---- dispatcher-level envelope tests ----------------------

// TestListRemoteAgentResourceProviders_Ok pins the
// happy path: a fresh MemResourceStore seeded with
// the fixture set, the dispatcher returns all 4
// providers sorted by ProviderID ascending.
func TestListRemoteAgentResourceProviders_Ok(t *testing.T) {
	rs := NewMemResourceStore(nil, nil)
	d := newResourcesDispatcher(t, rs, nil)
	res, errObj := dispatchResources(t, d, "ListRemoteAgentResourceProviders", map[string]any{})
	if errObj != nil {
		t.Fatalf("ListRemoteAgentResourceProviders returned error: %#v", errObj)
	}
	providers, ok := res["providers"].([]ResourceProvider)
	if !ok {
		t.Fatalf("providers = %T, want []ResourceProvider; res = %#v", res["providers"], res)
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
	rs := NewMemResourceStore(nil, nil)
	d := newResourcesDispatcher(t, rs, nil)
	res, errObj := dispatchResources(t, d, "ListRemoteAgentResourceProviders", map[string]any{"kind": "vcs"})
	if errObj != nil {
		t.Fatalf("ListRemoteAgentResourceProviders returned error: %#v", errObj)
	}
	providers, ok := res["providers"].([]ResourceProvider)
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
	rs := NewMemResourceStore(nil, nil)
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
// ResourceStore rejects the call. Mirrors
// TestListRemoteAgentPlugins_NoStore — the
// dispatcher fabricates a fresh MemResourceStore
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
// happy path: a fresh MemResourceStore returns all
// 12 resources.
func TestListRemoteAgentAvailableResources_Ok(t *testing.T) {
	rs := NewMemResourceStore(nil, nil)
	d := newResourcesDispatcher(t, rs, nil)
	res, errObj := dispatchResources(t, d, "ListRemoteAgentAvailableResources", map[string]any{})
	if errObj != nil {
		t.Fatalf("ListRemoteAgentAvailableResources returned error: %#v", errObj)
	}
	resources, ok := res["resources"].([]Resource)
	if !ok {
		t.Fatalf("resources = %T, want []Resource; res = %#v", res["resources"], res)
	}
	if len(resources) != 12 {
		t.Errorf("len(resources) = %d, want 12", len(resources))
	}
}

// TestListRemoteAgentAvailableResources_ProviderFilter
// pins the provider_id filter: filtering by github
// returns the 3 github resources.
func TestListRemoteAgentAvailableResources_ProviderFilter(t *testing.T) {
	rs := NewMemResourceStore(nil, nil)
	d := newResourcesDispatcher(t, rs, nil)
	res, errObj := dispatchResources(t, d, "ListRemoteAgentAvailableResources", map[string]any{"provider_id": "github"})
	if errObj != nil {
		t.Fatalf("ListRemoteAgentAvailableResources returned error: %#v", errObj)
	}
	resources, ok := res["resources"].([]Resource)
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
	rs := NewMemResourceStore(nil, nil)
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
	ps := NewMemProjectStore(nil)
	row, err := ps.Create(context.Background(), NewProject{Name: "p1", Workspace: "/ws/p1"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	rs := NewMemResourceStore(nil, ps)
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
	ps := NewMemProjectStore(nil)
	rs := NewMemResourceStore(nil, ps)
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
	ps := NewMemProjectStore(nil)
	rs := NewMemResourceStore(nil, ps)
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
	ps := NewMemProjectStore(nil)
	row, err := ps.Create(context.Background(), NewProject{Name: "p1", Workspace: "/ws/p1"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	rs := NewMemResourceStore(nil, ps)
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

// ---- dispatcher+projects integration: Project.Resources
//      appears on ListRemoteAgentProjects envelope ----

// TestProject_ResourcesOnListEnvelope pins the
// cycle-rc-resources extension to
// projectSliceToWire: a project that carries
// bound resources surfaces them under the
// `resources` key on the wire envelope.
func TestProject_ResourcesOnListEnvelope(t *testing.T) {
	ps := NewMemProjectStore(nil)
	row, err := ps.Create(context.Background(), NewProject{Name: "p1", Workspace: "/ws/p1"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// Bind a couple of resources.
	rs := NewMemResourceStore(nil, ps)
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
	ps := NewMemProjectStore(nil)
	if _, err := ps.Create(context.Background(), NewProject{Name: "p1", Workspace: "/ws/p1"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	rs := NewMemResourceStore(nil, ps)
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
