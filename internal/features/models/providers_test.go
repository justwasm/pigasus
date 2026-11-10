// Tests for the provider-centric configuration.
//
// The configuration has one shape: the provider section, where a provider
// holds the endpoint, the credential and the protocol and the models it
// serves are listed under it.
package models

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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
			name: "api_url identical to base_url still appends protocol path",
			p:    ProviderConfig{Type: "anthropic", BaseURL: "https://api.minimaxi.com/anthropic", APIURL: "https://api.minimaxi.com/anthropic"},
			want: "https://api.minimaxi.com/anthropic/v1/messages",
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

func TestProviderConfig_MultipleAccounts(t *testing.T) {
	// Top-level APIKey fallback when no accounts
	p := ProviderConfig{APIKey: "default-key"}
	if got := p.EffectiveAPIKey(); got != "default-key" {
		t.Errorf("EffectiveAPIKey() = %q, want default-key", got)
	}

	// ActiveAccount selected
	p.Accounts = []ProviderAccount{
		{ID: "work", APIKey: "sk-work", Active: false},
		{ID: "personal", APIKey: "sk-personal", Active: true},
	}
	p.ActiveAccount = "work"
	if got := p.EffectiveAPIKey(); got != "sk-work" {
		t.Errorf("EffectiveAPIKey() with ActiveAccount=work = %q, want sk-work", got)
	}

	// When ActiveAccount is empty, active flag is used
	p.ActiveAccount = ""
	if got := p.EffectiveAPIKey(); got != "sk-personal" {
		t.Errorf("EffectiveAPIKey() with Active flag = %q, want sk-personal", got)
	}
}

func TestProviderConfig_EffectiveModels_FallbackToPreset(t *testing.T) {
	p := ProviderConfig{
		ID: "hyper",
	}
	models := p.EffectiveModels()
	if len(models) == 0 {
		t.Fatal("expected hyper to fall back to preset models, got 0")
	}
	if len(models) != 23 {
		t.Fatalf("expected 23 hyper models, got %d", len(models))
	}

	// Flatten() also uses EffectiveModels()
	flat := p.Flatten()
	if len(flat) != 23 {
		t.Fatalf("Flatten() returned %d models, want 23", len(flat))
	}

	// When explicit models are configured, they take precedence
	p.Models = []ProviderModel{
		{ID: "custom-prism", Name: "Custom Prism"},
	}
	custom := p.EffectiveModels()
	if len(custom) != 1 || custom[0].ID != "custom-prism" {
		t.Fatalf("expected explicit models to take precedence, got %+v", custom)
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
	t.Setenv("OPENAGY_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	providers := filepath.Join(dir, "providers.json")
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

func TestProviderModel_UnmarshalStringAndObject(t *testing.T) {
	// 1. Unmarshal from plain string array
	raw := `["MiniMax-M3.1-Flash", "qwen3.8-max"]`
	var models []ProviderModel
	if err := json.Unmarshal([]byte(raw), &models); err != nil {
		t.Fatalf("Unmarshal string list: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("len = %d, want 2", len(models))
	}
	if models[0].EffectiveID() != "MiniMax-M3.1-Flash" || models[0].EffectiveWire() != "MiniMax-M3.1-Flash" {
		t.Errorf("model 0 = %+v", models[0])
	}

	// 2. Unmarshal from object with only "model"
	rawObj := `[{"model": "MiniMax-M3.1-Flash", "context_window": 1000000}]`
	var modelsObj []ProviderModel
	if err := json.Unmarshal([]byte(rawObj), &modelsObj); err != nil {
		t.Fatalf("Unmarshal object list: %v", err)
	}
	if modelsObj[0].EffectiveID() != "MiniMax-M3.1-Flash" {
		t.Errorf("EffectiveID() = %q, want MiniMax-M3.1-Flash", modelsObj[0].EffectiveID())
	}
	if modelsObj[0].ContextWindow != 1000000 {
		t.Errorf("ContextWindow = %d, want 1000000", modelsObj[0].ContextWindow)
	}

	// 3. Unmarshal from object with only "id"
	rawIDOnly := `[{"id": "MiniMax-M3.1-Flash"}]`
	var modelsIDOnly []ProviderModel
	if err := json.Unmarshal([]byte(rawIDOnly), &modelsIDOnly); err != nil {
		t.Fatalf("Unmarshal id only: %v", err)
	}
	if modelsIDOnly[0].EffectiveWire() != "MiniMax-M3.1-Flash" {
		t.Errorf("EffectiveWire() = %q, want MiniMax-M3.1-Flash", modelsIDOnly[0].EffectiveWire())
	}
}

func TestProviderModel_MarshalJSON_CleanFormat(t *testing.T) {
	// When ID == Name == Model, MarshalJSON omits redundant "name" and "model"
	m := ProviderModel{
		ID:               "MiniMax-M3.1-Flash",
		Name:             "MiniMax-M3.1-Flash",
		Model:            "MiniMax-M3.1-Flash",
		ContextWindow:    1000000,
		DefaultMaxTokens: 512000,
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	str := string(data)
	if strings.Contains(str, `"model":`) {
		t.Errorf("expected redundant 'model' to be omitted in %s", str)
	}
	if strings.Contains(str, `"name":`) {
		t.Errorf("expected redundant 'name' to be omitted in %s", str)
	}
	if !strings.Contains(str, `"id":"MiniMax-M3.1-Flash"`) {
		t.Errorf("expected 'id' to be present in %s", str)
	}

	// When Model != ID, Model is preserved
	mCustomWire := ProviderModel{
		ID:    "fast",
		Model: "gpt-4o-mini",
	}
	dataCustom, err := json.Marshal(mCustomWire)
	if err != nil {
		t.Fatalf("Marshal custom: %v", err)
	}
	if !strings.Contains(string(dataCustom), `"model":"gpt-4o-mini"`) {
		t.Errorf("expected 'model' to be preserved when differing: %s", string(dataCustom))
	}
}

func TestProviderConfig_EffectiveHeaders(t *testing.T) {
	raw := `{
		"providers": {
			"p1": {
				"headers": {"User-Agent": "agent-one", "X-Custom": "val1"},
				"header_overrides": {"X-Custom": "val2", "X-Override": "yes"},
				"extra_headers": {"Extra": "present"}
			}
		}
	}`
	parsed, err := ParseProviders([]byte(raw))
	if err != nil {
		t.Fatalf("ParseProviders: %v", err)
	}
	p := parsed["p1"]
	headers := p.EffectiveHeaders()
	if headers["User-Agent"] != "agent-one" {
		t.Errorf("User-Agent = %q, want agent-one", headers["User-Agent"])
	}
	if headers["X-Custom"] != "val2" {
		t.Errorf("X-Custom = %q, want val2", headers["X-Custom"])
	}
	if headers["X-Override"] != "yes" {
		t.Errorf("X-Override = %q, want yes", headers["X-Override"])
	}
	if headers["Extra"] != "present" {
		t.Errorf("Extra = %q, want present", headers["Extra"])
	}
	flattened := p.Flatten()
	if len(flattened) > 0 && flattened[0].ExtraHeaders["User-Agent"] != "agent-one" {
		t.Errorf("flattened ExtraHeaders[User-Agent] = %q, want agent-one", flattened[0].ExtraHeaders["User-Agent"])
	}
}
