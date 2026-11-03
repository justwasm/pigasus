// Package resources — store_test.go: the resources domain's
// own tests.
//
// The store is the domain's fact: the fixture set, the
// provider / resource filters, the bind semantics (replace
// style, unknown-resource-is-a-no-op, the project-not-found
// sentinel), the on-disk layout and the closed kind set.
//
// The dispatcher's round-trip against the store lives in
// internal/remotecontrol/resources_test.go, because the
// dispatcher is the host's.
package resources

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/remotecontrol/projects"
)

// TestIsValidKind asserts the trim helper
// used by the per-method param-validation sites.
func TestIsValidKind(t *testing.T) {
	// IsValidKind is the validation helper
	// the dispatcher uses — pin its behaviour so a
	// future cycle that adds a new kind only has to
	// extend AllKinds and the closed-set
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
			if got := IsValidKind(tc.in); got != tc.want {
				t.Errorf("IsValidKind(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// TestAllKinds pins the closed-set order so
// a future cycle that extends the set updates
// AllKinds + the doc comment + the IsValidKind
// helper in lockstep.
func TestAllKinds(t *testing.T) {
	want := []string{"vcs", "observability", "pm", "docs", "ci"}
	if len(AllKinds) != len(want) {
		t.Fatalf("AllKinds len = %d, want %d", len(AllKinds), len(want))
	}
	for i, k := range want {
		if AllKinds[i] != k {
			t.Errorf("AllKinds[%d] = %q, want %q", i, AllKinds[i], k)
		}
	}
}

// TestNotifChangedMethod pins the JSON-RPC
// notification method name the dispatcher uses for
// the "resources/changed" server-push frame.
func TestNotifChangedMethod(t *testing.T) {
	if NotifChangedMethod != "resources/changed" {
		t.Errorf("NotifChangedMethod = %q, want resources/changed", NotifChangedMethod)
	}
}

// TestStateVersion pins the on-disk format
// version the current store writes. v1 is the brief-
// pinned value; a future cycle that introduces a
// breaking change (renamed keys, removed fields)
// bumps this constant AND updates the load() branch
// that rejects version > StateVersion.
func TestStateVersion(t *testing.T) {
	if StateVersion != 1 {
		t.Errorf("StateVersion = %d, want 1", StateVersion)
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

// TestStore_MemProviders_Default pins the
// fixture-seeded default: a fresh MemStore
// returns 4 providers (the brief's fixture set).
func TestStore_MemProviders_Default(t *testing.T) {
	rs := NewMemStore(nil, nil)
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

// TestStore_MemResources_Default pins the
// fixture-seeded resource count: a fresh
// MemStore returns 12 resources.
func TestStore_MemResources_Default(t *testing.T) {
	rs := NewMemStore(nil, nil)
	resources := rs.Resources(context.Background(), "", "")
	if len(resources) != 12 {
		t.Fatalf("Resources len = %d, want 12", len(resources))
	}
}

// TestStore_MemResources_FilterProvider pins
// the provider_id filter: filtering by a single
// provider returns only its resources.
func TestStore_MemResources_FilterProvider(t *testing.T) {
	rs := NewMemStore(nil, nil)
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

// TestStore_MemResources_FilterKind pins the
// kind filter: filtering by "vcs" returns only the
// github resources.
func TestStore_MemResources_FilterKind(t *testing.T) {
	rs := NewMemStore(nil, nil)
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

// TestStore_MemResources_CombinedFilter
// pins the provider_id + kind combined filter.
func TestStore_MemResources_CombinedFilter(t *testing.T) {
	rs := NewMemStore(nil, nil)
	resources := rs.Resources(context.Background(), "github", "vcs")
	if len(resources) != 3 {
		t.Errorf("github+vcs resources = %d, want 3", len(resources))
	}
}

// TestStore_MemBind_UnknownProject pins the
// sentinel path: binding to an unknown project_id
// returns ErrProjectNotFound (the
// dispatcher's -32004 envelope).
func TestStore_MemBind_UnknownProject(t *testing.T) {
	ps := projects.NewMemStore(nil)
	rs := NewMemStore(nil, ps)
	_, _, err := rs.Bind(context.Background(), "no_such_project", []string{"github:openagy/openagy"})
	if !errors.Is(err, ErrProjectNotFound) {
		t.Errorf("Bind err = %v, want ErrProjectNotFound", err)
	}
}

// TestStore_MemBind_HappyPath pins the
// happy path: bind to a real project_id, the
// project's Resources field gets the supplied
// slice (replace-style), bound=1, unbound=0.
func TestStore_MemBind_HappyPath(t *testing.T) {
	ps := projects.NewMemStore(nil)
	row, err := ps.Create(context.Background(), projects.Create{Name: "p1", Workspace: "/ws/p1"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	rs := NewMemStore(nil, ps)
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

// TestStore_MemBind_UnknownResource pins the
// "unknown resource is no-op" rule: an unknown
// resource_id is counted as `unbound`, not as an
// error.
func TestStore_MemBind_UnknownResource(t *testing.T) {
	ps := projects.NewMemStore(nil)
	row, err := ps.Create(context.Background(), projects.Create{Name: "p1", Workspace: "/ws/p1"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	rs := NewMemStore(nil, ps)
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

// TestStore_MemBind_MixedKnownUnknown pins
// the partial-bind path: a mix of known + unknown
// ids returns bound=known-count, unbound=unknown-
// count, no error.
func TestStore_MemBind_MixedKnownUnknown(t *testing.T) {
	ps := projects.NewMemStore(nil)
	row, err := ps.Create(context.Background(), projects.Create{Name: "p1", Workspace: "/ws/p1"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	rs := NewMemStore(nil, ps)
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

// TestStore_MemBind_NoProjects pins the
// "no Projects store installed" path: a
// MemStore with nil projects returns
// ErrProjectNotFound unconditionally.
func TestStore_MemBind_NoProjects(t *testing.T) {
	rs := NewMemStore(nil, nil)
	_, _, err := rs.Bind(context.Background(), "any_id", []string{"github:openagy/openagy"})
	if !errors.Is(err, ErrProjectNotFound) {
		t.Errorf("Bind err = %v, want ErrProjectNotFound", err)
	}
}

// TestStore_MemBind_ReplaceStyle pins the
// brief's "replace-style" semantic: a second Bind
// with a different slice overwrites the first
// (not appended).
func TestStore_MemBind_ReplaceStyle(t *testing.T) {
	ps := projects.NewMemStore(nil)
	row, err := ps.Create(context.Background(), projects.Create{Name: "p1", Workspace: "/ws/p1"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	rs := NewMemStore(nil, ps)
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

// TestStore_MemBind_EmptySlice pins the
// "bind to empty set" semantic: an explicit empty
// slice is a valid unbind operation.
func TestStore_MemBind_EmptySlice(t *testing.T) {
	ps := projects.NewMemStore(nil)
	row, err := ps.Create(context.Background(), projects.Create{Name: "p1", Workspace: "/ws/p1"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	rs := NewMemStore(nil, ps)
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

// TestStore_MemStoreWith_Custom pins the
// custom-snapshot constructor: a fresh
// MemStore seeded with a single provider
// + zero resources returns that provider and an
// empty resource slice.
func TestStore_MemStoreWith_Custom(t *testing.T) {
	ps := projects.NewMemStore(nil)
	customProviders := []Provider{
		{ProviderID: "custom", Name: "Custom", Kind: KindCI, Configured: true, Scopes: []string{"ci"}, Version: "1.0.0"},
	}
	customResources := []Resource{}
	rs := NewMemStoreWith(customProviders, customResources, nil, ps)
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

// TestChangedToWire pins the wire envelope
// the dispatcher emits for both the response and
// the broadcast notification: a polling caller and a
// notification subscriber see byte-for-byte identical
// shapes.
func TestChangedToWire(t *testing.T) {
	updatedAt := time.Date(2026, 10, 12, 14, 30, 0, 0, time.UTC)
	wire := changedToWire("proj_abc", 3, 1, updatedAt)
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

// TestMapError pins the sentinel-to-JSON-RPC
// mapping.
func TestMapError(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		wantCode int
		wantMsg  string
	}{
		{"ErrProjectNotFound", ErrProjectNotFound, -32004, "Project not found"},
		{"ErrProviderUnknown", ErrProviderUnknown, -32602, "Invalid params"},
		{"I/O error", errors.New("disk on fire"), -32603, "Internal error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := MapError(tc.err, "test")
			if got == nil {
				t.Fatalf("MapError returned nil")
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
	if got := MapError(nil, "test"); got != nil {
		t.Errorf("MapError(nil) = %v, want nil", got)
	}
}

// TestFSStore_FreshHost pins the fresh-host
// fixture path: a brand-new FSStore (no
// on-disk file) returns the fixture-seeded state
// (4 providers + 12 resources, all configured=false).
func TestFSStore_FreshHost(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "resources.json")
	rs := NewFSStore(path, nil, nil, nil)
	providers := rs.Providers(context.Background())
	if len(providers) != 4 {
		t.Errorf("fresh Providers len = %d, want 4", len(providers))
	}
	resources := rs.Resources(context.Background(), "", "")
	if len(resources) != 12 {
		t.Errorf("fresh Resources len = %d, want 12", len(resources))
	}
}

// TestFSStore_Roundtrip pins the on-disk
// persistence: a Bind writes the bound resource
// list to the project's JSON via projects.Store.Update
// (the resource-id list lives on the project
// record, not on the resources.json registry —
// the registry is a list of *what providers + resources
// exist*, and the project is the bind target).
func TestFSStore_Roundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "resources.json")
	// Stand up a projects.Store so Bind can write
	// the project's Resources field back.
	projectsDir := filepath.Join(dir, "projects")
	ps := projects.NewFSStore(projectsDir, nil, nil, nil)
	row, err := ps.Create(context.Background(), projects.Create{Name: "p1", Workspace: "/ws/p1"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	rs := NewFSStore(path, ps, nil, nil)
	bound, unbound, err := rs.Bind(context.Background(), row.ID, []string{"github:openagy/openagy"})
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if bound != 1 || unbound != 0 {
		t.Errorf("Bind: bound=%d unbound=%d, want 1/0", bound, unbound)
	}

	// The project's Resources field persists to
	// disk via projects.Store.Update — verify by
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

	// Reload via a fresh FSStore and
	// verify the fixture providers + resources
	// are intact (the bind doesn't add or
	// remove rows from the registry).
	rs2 := NewFSStore(path, ps, nil, nil)
	if len(rs2.Providers(context.Background())) != 4 {
		t.Errorf("reloaded Providers len = %d, want 4", len(rs2.Providers(context.Background())))
	}
	if len(rs2.Resources(context.Background(), "", "")) != 12 {
		t.Errorf("reloaded Resources len = %d, want 12", len(rs2.Resources(context.Background(), "", "")))
	}
}

// TestFSStore_ConfiguredFromSecretDir pins
// the `$ANTIGRAVITY_HOME/secrets/<provider_id>`
// discovery path: when a non-empty file exists for
// a provider, the snapshot's `configured` flag is
// true for that provider.
func TestFSStore_ConfiguredFromSecretDir(t *testing.T) {
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
	rs := NewFSStore(path, nil, check, nil)
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
