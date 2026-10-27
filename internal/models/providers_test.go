// Tests for the provider-centric configuration.
//
// The configuration has one shape: the provider section, where a provider
// holds the endpoint, the credential and the protocol and the models it
// serves are listed under it.
package models

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProviderConfig_Endpoint(t *testing.T) {
	cases := []struct {
		name string
		p    ProviderConfig
		want string
	}{
		{
			name: "openai appends chat/completions",
			p:    ProviderConfig{Type: "openai", BaseURL: "https://host/v1"},
			want: "https://host/v1/chat/completions",
		},
		{
			name: "openai is the default type",
			p:    ProviderConfig{BaseURL: "https://host/v1"},
			want: "https://host/v1/chat/completions",
		},
		{
			name: "anthropic appends v1/messages",
			p:    ProviderConfig{Type: "anthropic", BaseURL: "https://host/anthropic"},
			want: "https://host/anthropic/v1/messages",
		},
		{
			name: "an explicit api_url wins",
			p:    ProviderConfig{Type: "anthropic", BaseURL: "https://host/anthropic", APIURL: "https://other/messages"},
			want: "https://other/messages",
		},
		{
			name: "a trailing slash on the base is trimmed",
			p:    ProviderConfig{BaseURL: "https://host/v1/"},
			want: "https://host/v1/chat/completions",
		},
		{
			name: "no base and no url is empty",
			p:    ProviderConfig{},
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.p.Endpoint(); got != tc.want {
				t.Errorf("Endpoint() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestProviderConfig_ResolvedAPIKey(t *testing.T) {
	t.Setenv("OPENAGY_TEST_KEY", "secret")
	cases := []struct {
		in   string
		want string
	}{
		{"$OPENAGY_TEST_KEY", "secret"},
		{"${OPENAGY_TEST_KEY}", "secret"},
		{"literal", "literal"},
		{"", ""},
		{"$", "$"},
	}
	for _, tc := range cases {
		p := ProviderConfig{APIKey: tc.in}
		if got := p.ResolvedAPIKey(); got != tc.want {
			t.Errorf("ResolvedAPIKey(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestProviderConfig_Flatten(t *testing.T) {
	p := ProviderConfig{
		ID:      "gateway",
		Name:    "Gateway",
		Type:    "openai",
		BaseURL: "https://host/v1",
		APIKey:  "k",
		Models: []ProviderModel{
			{ID: "m1", Name: "Model One", Model: "Upstream-One", ContextWindow: 1048576, DefaultMaxTokens: 8192},
			{ID: "m2"},
		},
	}
	got := p.Flatten()
	if len(got) != 2 {
		t.Fatalf("Flatten() returned %d entries, want 2", len(got))
	}
	m1 := got[0]
	if m1.Name != "m1" || m1.DisplayName != "Model One" {
		t.Errorf("m1 = %+v", m1)
	}
	if m1.ExternalModelName != "Upstream-One" {
		t.Errorf("m1 wire id = %q, want %q", m1.ExternalModelName, "Upstream-One")
	}
	if m1.APIURL != "https://host/v1/chat/completions" {
		t.Errorf("m1 url = %q", m1.APIURL)
	}
	if m1.Provider != ProviderTypeOpenAI || m1.Type != ProviderTypeOpenAI {
		t.Errorf("m1 protocol = %q/%q, want openai", m1.Provider, m1.Type)
	}
	if m1.ProviderID != "gateway" {
		t.Errorf("m1 provider id = %q, want %q", m1.ProviderID, "gateway")
	}
	if m1.ContextWindow != 1048576 {
		t.Errorf("m1 context window = %d, want 1048576", m1.ContextWindow)
	}
	// A model with no upstream id falls back to the catalogue id, and its
	// label falls back to the catalogue id too.
	m2 := got[1]
	if m2.ExternalModelName != "m2" || m2.DisplayName != "m2" {
		t.Errorf("m2 = %+v", m2)
	}
}

func TestRegistry_LoadsTheProviderSection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "providers.json")
	body := `{
	  "providers": {
	    "gateway": {
	      "type": "openai",
	      "base_url": "https://host/v1",
	      "models": [{"id": "central", "name": "Central", "model": "central-upstream"}]
	    }
	  }
	}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	reg, err := LoadRegistry(path)
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	list := reg.List()
	if len(list) != 1 {
		t.Fatalf("List() = %d entries, want 1: %+v", len(list), list)
	}
	if list[0].Name != "central" {
		t.Errorf("List() = %q, want central", list[0].Name)
	}
	m, ok := reg.GetByName("central")
	if !ok {
		t.Fatal("GetByName(central) not found")
	}
	if m.ExternalModelName != "central-upstream" {
		t.Errorf("central wire id = %q", m.ExternalModelName)
	}
	if reg.Len() != 1 {
		t.Errorf("Len() = %d, want 1", reg.Len())
	}
}

func TestRegistry_IgnoresAnUnknownSection(t *testing.T) {
	// A document may carry sections the clone does not model. They are
	// ignored, not fatal, and the provider section still loads.
	dir := t.TempDir()
	path := filepath.Join(dir, "providers.json")
	body := `{
	  "something_else": {"a": 1},
	  "providers": {
	    "gateway": {"base_url": "https://host/v1", "models": [{"id": "central"}]}
	  }
	}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	reg, err := LoadRegistry(path)
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	if reg.Len() != 1 {
		t.Errorf("Len() = %d, want 1", reg.Len())
	}
}

func TestRegistry_AddAndRemoveProviderModel(t *testing.T) {
	reg := &Registry{providers: map[string]ProviderConfig{}}
	if err := reg.SetProvider("gateway", ProviderConfig{BaseURL: "https://host/v1"}); err != nil {
		t.Fatalf("SetProvider: %v", err)
	}
	if err := reg.AddProviderModel("gateway", ProviderModel{ID: "m1", Model: "Up1"}); err != nil {
		t.Fatalf("AddProviderModel: %v", err)
	}
	if err := reg.AddProviderModel("gateway", ProviderModel{ID: "m1", Model: "Up2"}); err != nil {
		t.Fatalf("AddProviderModel (replace): %v", err)
	}
	if err := reg.AddProviderModel("nope", ProviderModel{ID: "m2"}); err == nil {
		t.Error("AddProviderModel on an unknown provider should fail")
	}
	list := reg.List()
	if len(list) != 1 || list[0].ExternalModelName != "Up2" {
		t.Fatalf("List() = %+v", list)
	}
	if !reg.RemoveProviderModel("gateway", "m1") {
		t.Error("RemoveProviderModel should report the removal")
	}
	if reg.Len() != 0 {
		t.Errorf("Len() = %d, want 0", reg.Len())
	}
}

func TestRegistry_SaveAndReloadKeepsProviders(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "providers.json")
	reg := &Registry{providers: map[string]ProviderConfig{}}
	if err := reg.SetProvider("gateway", ProviderConfig{
		Type:    "anthropic",
		BaseURL: "https://host/anthropic",
		APIKey:  "$OPENAGY_TEST_KEY",
	}); err != nil {
		t.Fatal(err)
	}
	if err := reg.AddProviderModel("gateway", ProviderModel{ID: "m1", Model: "Up1", ContextWindow: 4096}); err != nil {
		t.Fatal(err)
	}
	if err := SaveProviders(path, reg.Providers()); err != nil {
		t.Fatalf("SaveProviders: %v", err)
	}
	reloaded, err := LoadRegistry(path)
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	p, ok := reloaded.Providers()["gateway"]
	if !ok {
		t.Fatal("provider lost on reload")
	}
	if p.Type != "anthropic" || p.BaseURL != "https://host/anthropic" {
		t.Errorf("provider = %+v", p)
	}
	if len(p.Models) != 1 || p.Models[0].ContextWindow != 4096 {
		t.Errorf("provider models = %+v", p.Models)
	}
}

// TestSaveProviders_WritesTheFilePrivately pins the mode of the
// provider file: it holds the API keys, so it is 0600 even when the
// file was already there with a wider mode (the atomic write replaces
// the file rather than editing it in place, so the old mode must not
// survive).
func TestSaveProviders_WritesTheFilePrivately(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "providers.json")
	if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SaveProviders(path, map[string]ProviderConfig{
		"gateway": {Type: "anthropic", BaseURL: "https://host/anthropic"},
	}); err != nil {
		t.Fatalf("SaveProviders: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("file mode = %o, want 600", perm)
	}
	// No sibling temp file is left behind.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "providers.json" {
			t.Errorf("leftover file %q", e.Name())
		}
	}
}

func TestLoadAll_ReadsTheProviderFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	providers := filepath.Join(dir, "openagy", "providers.json")
	if err := os.MkdirAll(filepath.Dir(providers), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(providers, []byte(`{"providers":{"p":{"base_url":"https://h/v1","models":[{"id":"central"}]}}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	reg, err := LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	names := map[string]bool{}
	for _, m := range reg.List() {
		names[m.Name] = true
	}
	if !names["central"] {
		t.Errorf("LoadAll() = %v, want the configured model", names)
	}
	if reg.Path() != providers {
		t.Errorf("Path() = %q, want %q", reg.Path(), providers)
	}
}
