package agyproxy

import (
	"os"
	"path/filepath"
	"testing"
)

// TestConfiguredModelsReadsTheRegistry locks the contract docs/guide/proxy.md
// states: `pigasus agy` offers the operator's registry models to the vendor
// binary. Commit fe77d3c6 removed the injection for a while; a launcher that
// quietly passes nil strips every custom model from the vendor's picker.
func TestConfiguredModelsReadsTheRegistry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "providers.json")
	raw := `{
	  "providers": {
	    "agw": {
	      "id": "agw",
	      "type": "openai",
	      "api_url": "https://example.test/v1/chat/completions",
	      "models": [{"id": "deepseek-v4", "model": "DeepSeek-V4-Flash"}]
	    },
	    "minimax-china": {
	      "id": "minimax-china",
	      "type": "anthropic",
	      "base_url": "https://example.test/anthropic",
	      "models": [{"id": "MiniMax-M3", "model": "MiniMax-M3"}]
	    }
	  }
	}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("write providers.json: %v", err)
	}
	t.Setenv("OPENAGY_PROVIDERS_CONFIG", path)

	got := configuredModels()
	if len(got) != 2 {
		t.Fatalf("configuredModels() returned %d models, want 2: %+v", len(got), got)
	}
	names := map[string]bool{}
	for _, m := range got {
		names[m.Name] = true
	}
	for _, want := range []string{"deepseek-v4", "MiniMax-M3"} {
		if !names[want] {
			t.Errorf("configuredModels() missing %q: %+v", want, names)
		}
	}
}

// A missing configuration is not an error: the launcher must still start and
// hand the vendor binary to Google untouched.
func TestConfiguredModelsMissingFileIsEmpty(t *testing.T) {
	t.Setenv("OPENAGY_PROVIDERS_CONFIG", filepath.Join(t.TempDir(), "absent.json"))
	if got := configuredModels(); len(got) != 0 {
		t.Fatalf("configuredModels() = %+v, want none", got)
	}
}
