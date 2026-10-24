// Tests for the file-scoped editor.
//
// The editor's contract is narrow and load-bearing:
//
//   - a write changes the field it was asked to change and nothing else;
//   - a field the editor does not model survives the write;
//   - the key order survives the write;
//   - two writers do not clobber each other.
package models

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// writeFile writes a configuration file and returns its path.
func writeFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "providers.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

// readFile reads a configuration file.
func readFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	return string(raw)
}

func TestEditor_PutProviderCreatesTheSection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "providers.json")
	ed := NewEditor(path)
	if err := ed.PutProvider("gateway", ProviderConfig{
		ID:      "gateway",
		Name:    "Gateway",
		Type:    ProviderTypeOpenAI,
		BaseURL: "https://gw.example/v1",
		APIKey:  "$OPENAGY_GATEWAY_KEY",
	}); err != nil {
		t.Fatalf("PutProvider: %v", err)
	}
	body := readFile(t, path)
	if !strings.Contains(body, `"gateway"`) {
		t.Fatalf("provider missing from the file:\n%s", body)
	}
	if !strings.Contains(body, `"base_url": "https://gw.example/v1"`) {
		t.Fatalf("base_url missing from the file:\n%s", body)
	}
	providers, err := ed.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	p, ok := providers["gateway"]
	if !ok {
		t.Fatalf("provider not listed: %+v", providers)
	}
	if p.Endpoint() != "https://gw.example/v1/chat/completions" {
		t.Fatalf("endpoint = %q", p.Endpoint())
	}
}

func TestEditor_PutProviderKeepsUnknownFields(t *testing.T) {
	path := writeFile(t, `{
  "oauth": {"refresh_token": "keep-me"},
  "providers": {
    "gateway": {
      "id": "gateway",
      "base_url": "https://old.example/v1",
      "future_field": "keep-me-too"
    }
  }
}`)
	ed := NewEditor(path)
	if err := ed.PutProvider("gateway", ProviderConfig{
		ID:      "gateway",
		BaseURL: "https://new.example/v1",
	}); err != nil {
		t.Fatalf("PutProvider: %v", err)
	}
	body := readFile(t, path)
	if !strings.Contains(body, `"refresh_token": "keep-me"`) {
		t.Fatalf("the unknown top-level section was dropped:\n%s", body)
	}
	if !strings.Contains(body, `"future_field": "keep-me-too"`) {
		t.Fatalf("the unknown provider field was dropped:\n%s", body)
	}
	if !strings.Contains(body, `"https://new.example/v1"`) {
		t.Fatalf("the new base_url was not written:\n%s", body)
	}
	if strings.Contains(body, "old.example") {
		t.Fatalf("the old base_url survived the write:\n%s", body)
	}
}

func TestEditor_PutProviderKeepsTheKeyOrder(t *testing.T) {
	path := writeFile(t, `{
  "providers": {
    "gateway": {
      "id": "gateway",
      "name": "Gateway",
      "base_url": "https://gw.example/v1",
      "api_key": "$KEY"
    }
  }
}`)
	ed := NewEditor(path)
	if err := ed.PutProvider("gateway", ProviderConfig{
		ID:      "gateway",
		Name:    "Gateway",
		BaseURL: "https://gw.example/v2",
		APIKey:  "$KEY",
	}); err != nil {
		t.Fatalf("PutProvider: %v", err)
	}
	body := readFile(t, path)
	idx := func(s string) int { return strings.Index(body, s) }
	if idx("\"id\"") > idx("\"name\"") || idx("\"name\"") > idx("\"base_url\"") {
		t.Fatalf("the key order changed:\n%s", body)
	}
}

func TestEditor_PutModelAddsReplacesAndKeepsTheOthers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "providers.json")
	ed := NewEditor(path)
	if err := ed.PutProvider("gateway", ProviderConfig{
		ID:      "gateway",
		BaseURL: "https://gw.example/v1",
	}); err != nil {
		t.Fatalf("PutProvider: %v", err)
	}
	if err := ed.PutModel("gateway", ProviderModel{ID: "one", Model: "One"}); err != nil {
		t.Fatalf("PutModel: %v", err)
	}
	if err := ed.PutModel("gateway", ProviderModel{ID: "two", Model: "Two"}); err != nil {
		t.Fatalf("PutModel: %v", err)
	}
	if err := ed.PutModel("gateway", ProviderModel{ID: "one", Model: "One-v2"}); err != nil {
		t.Fatalf("PutModel replace: %v", err)
	}
	providers, err := ed.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	models := providers["gateway"].Models
	if len(models) != 2 {
		t.Fatalf("models = %d, want 2: %+v", len(models), models)
	}
	if models[0].ID != "one" || models[0].Model != "One-v2" {
		t.Fatalf("the model was not replaced in place: %+v", models[0])
	}
	if models[1].ID != "two" {
		t.Fatalf("the other model was lost: %+v", models[1])
	}
}

func TestEditor_PutModelRejectsAnUnknownProvider(t *testing.T) {
	path := filepath.Join(t.TempDir(), "providers.json")
	ed := NewEditor(path)
	err := ed.PutModel("nope", ProviderModel{ID: "one"})
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("err = %v, want an error naming the provider", err)
	}
}

func TestEditor_DeleteModelAndProvider(t *testing.T) {
	path := filepath.Join(t.TempDir(), "providers.json")
	ed := NewEditor(path)
	if err := ed.PutProvider("gateway", ProviderConfig{ID: "gateway", BaseURL: "https://x/v1"}); err != nil {
		t.Fatalf("PutProvider: %v", err)
	}
	if err := ed.PutModel("gateway", ProviderModel{ID: "one"}); err != nil {
		t.Fatalf("PutModel: %v", err)
	}
	provider, found, err := ed.DeleteModel("one")
	if err != nil || !found || provider != "gateway" {
		t.Fatalf("DeleteModel = %q, %v, %v", provider, found, err)
	}
	if _, found, _ := ed.DeleteModel("one"); found {
		t.Fatal("the second delete found something")
	}
	if err := ed.DeleteProvider("gateway"); err != nil {
		t.Fatalf("DeleteProvider: %v", err)
	}
	if err := ed.DeleteProvider("gateway"); err == nil {
		t.Fatal("deleting a missing provider succeeded")
	}
}

func TestEditor_WriteLeavesNoLockBehind(t *testing.T) {
	path := filepath.Join(t.TempDir(), "providers.json")
	ed := NewEditor(path)
	if err := ed.PutProvider("gateway", ProviderConfig{ID: "gateway", BaseURL: "https://x/v1"}); err != nil {
		t.Fatalf("PutProvider: %v", err)
	}
	if _, err := os.Stat(path + ".lock"); !os.IsNotExist(err) {
		t.Fatalf("the lock file survived the write: %v", err)
	}
}

func TestEditor_ConcurrentWritesKeepTheFileValid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "providers.json")
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ed := NewEditor(path)
			if err := ed.PutProvider("gateway", ProviderConfig{
				ID:      "gateway",
				BaseURL: "https://gw.example/v1",
			}); err != nil {
				t.Errorf("PutProvider: %v", err)
				return
			}
			if err := ed.PutModel("gateway", ProviderModel{ID: "m"}); err != nil {
				t.Errorf("PutModel: %v", err)
			}
		}(i)
	}
	wg.Wait()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("the file is not valid JSON after concurrent writes: %v\n%s", err, raw)
	}
}

func TestEditor_PutProviderRejectsAnEmptyID(t *testing.T) {
	ed := NewEditor(filepath.Join(t.TempDir(), "providers.json"))
	if err := ed.PutProvider("  ", ProviderConfig{}); err == nil {
		t.Fatal("an empty provider id was accepted")
	}
}
