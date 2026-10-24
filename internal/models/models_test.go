// Package models — models_test.go
//
// Tests for the in-memory registry, configuration persistence, and
// agent context installation.
package models

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---- Registry tests ----------------------------------------------------

func TestRegistryAddRemoveGet(t *testing.T) {
	r := &Registry{providers: map[string]ProviderConfig{}}
	if err := r.SetProvider("gateway", ProviderConfig{
		BaseURL: "https://gateway.example.com/v1",
		APIKey:  "-",
	}); err != nil {
		t.Fatalf("SetProvider: %v", err)
	}
	if err := r.AddProviderModel("gateway", ProviderModel{ID: "gateway", Name: "MiniMax M3", Model: "MiniMax-M3"}); err != nil {
		t.Fatalf("AddProviderModel: %v", err)
	}
	if r.Len() != 1 {
		t.Fatalf("Len after Add = %d, want 1", r.Len())
	}
	m, ok := r.GetByName("gateway")
	if !ok {
		t.Fatal("GetByName(gateway) returned not-found")
	}
	if m.ExternalModelName != "MiniMax-M3" {
		t.Errorf("ExternalModelName = %q, want MiniMax-M3", m.ExternalModelName)
	}
	if m.ProviderID != "gateway" {
		t.Errorf("ProviderID = %q, want gateway", m.ProviderID)
	}
	if !r.RemoveProviderModel("gateway", "gateway") {
		t.Error("RemoveProviderModel returned false; want true")
	}
	if r.Len() != 0 {
		t.Errorf("Len after Remove = %d, want 0", r.Len())
	}
	if _, ok := r.GetByName("gateway"); ok {
		t.Error("GetByName(gateway) after Remove returned found")
	}
}

func TestRegistryValidate(t *testing.T) {
	cases := []struct {
		name    string
		m       Model
		wantErr bool
	}{
		{"ok", Model{Name: "x", Provider: "openai", APIURL: "https://x", ExternalModelName: "y"}, false},
		{"no name", Model{Provider: "openai", APIURL: "https://x", ExternalModelName: "y"}, true},
		{"no provider", Model{Name: "x", APIURL: "https://x", ExternalModelName: "y"}, true},
		{"no url", Model{Name: "x", Provider: "openai", ExternalModelName: "y"}, true},
		{"no model", Model{Name: "x", Provider: "openai", APIURL: "https://x"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.m.Validate()
			if (err != nil) != tc.wantErr {
				t.Errorf("Validate() err = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}

func TestRegistrySaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "providers.json")
	r := &Registry{providers: map[string]ProviderConfig{}}
	if err := r.SetProvider("gateway", ProviderConfig{
		Type: "openai", BaseURL: "https://gateway.example.com/v1", APIKey: "-",
		TimeoutSeconds: 30,
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.AddProviderModel("gateway", ProviderModel{ID: "gateway", Name: "MiniMax M3", Model: "MiniMax-M3", ContextWindow: 128000}); err != nil {
		t.Fatal(err)
	}
	if err := r.SetProvider("openai", ProviderConfig{
		Type: "openai", BaseURL: "https://api.openai.com/v1", APIKey: "sk-test",
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.AddProviderModel("openai", ProviderModel{ID: "openai", Model: "gpt-4o-mini"}); err != nil {
		t.Fatal(err)
	}
	if err := SaveProviders(path, r.Providers()); err != nil {
		t.Fatalf("SaveProviders: %v", err)
	}
	r2, err := LoadRegistry(path)
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	if r2.Len() != 2 {
		t.Fatalf("loaded Len = %d, want 2", r2.Len())
	}
	if m, ok := r2.GetByName("gateway"); !ok || m.ContextWindow != 128000 || m.TimeoutSeconds != 30 {
		t.Errorf("round-trip lost fields: %+v", m)
	}
}

func TestRegistryLoadMissingFile(t *testing.T) {
	dir := t.TempDir()
	r, err := LoadRegistry(filepath.Join(dir, "nope.json"))
	if err != nil {
		t.Fatalf("LoadRegistry on missing file: %v", err)
	}
	if r.Len() != 0 {
		t.Errorf("missing file should yield empty registry, got %d", r.Len())
	}
	if r.Path() == "" {
		t.Error("Path() should be set even on missing-file load")
	}
}

func TestProviderConfigWireTypeIsCaseInsensitive(t *testing.T) {
	p := ProviderConfig{Type: "OpenAI", BaseURL: "https://host/v1"}
	if got := p.WireType(); got != ProviderTypeOpenAI {
		t.Errorf("WireType() = %q, want %q", got, ProviderTypeOpenAI)
	}
	if got := p.Endpoint(); got != "https://host/v1/chat/completions" {
		t.Errorf("Endpoint() = %q", got)
	}
}

func TestProvidersPath(t *testing.T) {
	t.Setenv("OPENAGY_PROVIDERS_CONFIG", "")
	t.Setenv("HOME", "/tmp/home")
	t.Setenv("XDG_CONFIG_HOME", "")
	got := ProvidersPath()
	want := "/tmp/home/.config/openagy/providers.json"
	if got != want {
		t.Errorf("ProvidersPath() = %q, want %q", got, want)
	}
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg")
	got = ProvidersPath()
	want = "/tmp/xdg/openagy/providers.json"
	if got != want {
		t.Errorf("ProvidersPath() with XDG = %q, want %q", got, want)
	}
	t.Setenv("OPENAGY_PROVIDERS_CONFIG", "/tmp/explicit.json")
	got = ProvidersPath()
	want = "/tmp/explicit.json"
	if got != want {
		t.Errorf("ProvidersPath() with the env override = %q, want %q", got, want)
	}
}

// ---- Agent Context tests ------------------------------------------------

func TestAgentContextInstall(t *testing.T) {
	dir := t.TempDir()
	paths, err := InstallContext(dir, ModeLite)
	if err != nil {
		t.Fatalf("InstallContext lite: %v", err)
	}
	if len(paths) != 2 {
		t.Fatalf("InstallContext returned %d paths, want 2", len(paths))
	}
	for _, p := range paths {
		body, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		if len(body) < 1000 {
			t.Errorf("%s only %d bytes, expected >= 1000", p, len(body))
		}
		if !strings.Contains(string(body), "openagy") {
			t.Errorf("%s missing 'openagy' string", p)
		}
	}
	if _, err := InstallContext(dir, ModeStrip); err != nil {
		t.Fatalf("InstallContext strip: %v", err)
	}
	stripBody, _ := os.ReadFile(filepath.Join(dir, "agent-context.md"))
	if !strings.Contains(string(stripBody), "Recovery timeline") {
		t.Error("strip mode should include 'Recovery timeline' section")
	}
	paths, err = InstallContext(dir, ModePassthrough)
	if err != nil {
		t.Fatalf("InstallContext passthrough: %v", err)
	}
	if paths != nil {
		t.Errorf("passthrough should return nil paths, got %v", paths)
	}
}

func TestAgentContextSetGetMode(t *testing.T) {
	defer SetMode(string(ModeLite))
	if GetMode() != ModeLite {
		t.Fatalf("default GetMode() = %q, want %q", GetMode(), ModeLite)
	}
	if err := SetMode("strip"); err != nil {
		t.Fatal(err)
	}
	if GetMode() != ModeStrip {
		t.Errorf("after SetMode('strip') GetMode() = %q, want strip", GetMode())
	}
	if err := SetMode("BOGUS"); err == nil {
		t.Error("SetMode('BOGUS') should error")
	}
}
