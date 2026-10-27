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
	"reflect"
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

// TestEditor_PutModelCarriesTheSupportsImagesMarker pins the
// per-model marker that says a model takes pictures.
//
// It is a pointer because "not stated" is an answer of its own: a model
// that says nothing carries no key at all, and the catalogue's family
// table decides for it. crush's own model list carries the same marker
// per model (catwalk.Model.SupportsImages, json "supports_attachments").
func TestEditor_PutModelCarriesTheSupportsImagesMarker(t *testing.T) {
	path := filepath.Join(t.TempDir(), "providers.json")
	ed := NewEditor(path)
	if err := ed.PutProvider("gateway", ProviderConfig{ID: "gateway", BaseURL: "https://x/v1"}); err != nil {
		t.Fatalf("PutProvider: %v", err)
	}
	yes, no := true, false
	if err := ed.PutModel("gateway", ProviderModel{ID: "yes", Model: "Yes", SupportsImages: &yes}); err != nil {
		t.Fatalf("PutModel: %v", err)
	}
	if err := ed.PutModel("gateway", ProviderModel{ID: "no", Model: "No", SupportsImages: &no}); err != nil {
		t.Fatalf("PutModel: %v", err)
	}
	if err := ed.PutModel("gateway", ProviderModel{ID: "silent", Model: "Silent"}); err != nil {
		t.Fatalf("PutModel: %v", err)
	}
	body := readFile(t, path)
	var doc struct {
		Providers map[string]struct {
			Models []map[string]any `json:"models"`
		} `json:"providers"`
	}
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatalf("unmarshal the file: %v\n%s", err, body)
	}
	for _, m := range doc.Providers["gateway"].Models {
		id, _ := m["id"].(string)
		want, stated := map[string]any{"yes": true, "no": false}[id]
		got, ok := m["supports_images"]
		if !stated {
			if ok {
				t.Errorf("the model %q carries a marker it never stated: %v", id, got)
			}
			continue
		}
		if !ok || got != want {
			t.Errorf("the model %q carries supports_images = %v, want %v", id, got, want)
		}
	}
	// A model that says nothing carries no key: the family table
	// decides for it, and a key with a default would be a statement.
	providers, err := ed.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, m := range providers["gateway"].Models {
		switch m.ID {
		case "yes":
			if m.SupportsImages == nil || !*m.SupportsImages {
				t.Errorf("the marker did not survive the write: %+v", m)
			}
		case "no":
			if m.SupportsImages == nil || *m.SupportsImages {
				t.Errorf("the marker's no did not survive the write: %+v", m)
			}
		case "silent":
			if m.SupportsImages != nil {
				t.Errorf("a model that says nothing must carry no marker: %+v", m)
			}
		}
	}
}

// TestEditor_PutModelCarriesTheModelAttributes pins the per-model
// attributes the Web UI's model picker reads.
//
// The names are the reference's own (the `ClientModelConfig` proto):
// `disabled`, `supported_mime_types`, `quota_info`,
// `supports_thought_circulation`, `model_url`, `tag_title`,
// `tag_description` and `effort_supported`. The file spells them in
// snake_case, the wire in camelCase — the conversion is the picker
// builder's job (remotecontrol/model_config_fields.go).
//
// A model that says nothing carries none of the keys: the file is the
// operator's own words, and a default in it would be a statement the
// operator never made.
func TestEditor_PutModelCarriesTheModelAttributes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "providers.json")
	ed := NewEditor(path)
	if err := ed.PutProvider("gateway", ProviderConfig{ID: "gateway", BaseURL: "https://x/v1"}); err != nil {
		t.Fatalf("PutProvider: %v", err)
	}
	yes, no := true, false
	fraction := 0.25
	full := ProviderModel{
		ID:                         "full",
		Model:                      "Full",
		SupportedMimeTypes:         []string{"image/png", "application/pdf"},
		Disabled:                   true,
		QuotaInfo:                  &QuotaInfo{RemainingFraction: &fraction, ResetTime: "2026-10-27T00:00:00Z"},
		SupportsThoughtCirculation: &yes,
		ModelURL:                   "https://example.com/models/full",
		TagTitle:                   "Fast",
		TagDescription:             "A quick model",
		EffortSupported:            &no,
	}
	if err := ed.PutModel("gateway", full); err != nil {
		t.Fatalf("PutModel: %v", err)
	}
	if err := ed.PutModel("gateway", ProviderModel{ID: "silent", Model: "Silent"}); err != nil {
		t.Fatalf("PutModel: %v", err)
	}

	body := readFile(t, path)
	var doc struct {
		Providers map[string]struct {
			Models []map[string]any `json:"models"`
		} `json:"providers"`
	}
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatalf("unmarshal the file: %v\n%s", err, body)
	}
	models := doc.Providers["gateway"].Models
	if len(models) != 2 {
		t.Fatalf("models = %d, want 2: %+v", len(models), models)
	}
	fullRow := models[0]
	if got, want := fullRow["supported_mime_types"], []any{"image/png", "application/pdf"}; !reflect.DeepEqual(got, want) {
		t.Errorf("supported_mime_types = %v, want %v", got, want)
	}
	if got := fullRow["disabled"]; got != true {
		t.Errorf("disabled = %v, want true", got)
	}
	quota, ok := fullRow["quota_info"].(map[string]any)
	if !ok {
		t.Fatalf("quota_info = %v, want an object", fullRow["quota_info"])
	}
	if got := quota["remaining_fraction"]; got != 0.25 {
		t.Errorf("quota_info.remaining_fraction = %v, want 0.25", got)
	}
	if got := quota["reset_time"]; got != "2026-10-27T00:00:00Z" {
		t.Errorf("quota_info.reset_time = %v, want the timestamp", got)
	}
	if got := fullRow["supports_thought_circulation"]; got != true {
		t.Errorf("supports_thought_circulation = %v, want true", got)
	}
	if got := fullRow["model_url"]; got != "https://example.com/models/full" {
		t.Errorf("model_url = %v", got)
	}
	if got := fullRow["tag_title"]; got != "Fast" {
		t.Errorf("tag_title = %v", got)
	}
	if got := fullRow["tag_description"]; got != "A quick model" {
		t.Errorf("tag_description = %v", got)
	}
	if got := fullRow["effort_supported"]; got != false {
		t.Errorf("effort_supported = %v, want false", got)
	}
	// A model that says nothing carries none of the keys.
	for _, key := range []string{
		"supported_mime_types", "disabled", "quota_info",
		"supports_thought_circulation", "model_url", "tag_title",
		"tag_description", "effort_supported",
	} {
		if got, ok := models[1][key]; ok {
			t.Errorf("the model that said nothing carries %q = %v", key, got)
		}
	}

	// The attributes survive the round trip through the file.
	providers, err := ed.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	rows := providers["gateway"].Models
	if len(rows) != 2 {
		t.Fatalf("models = %d, want 2", len(rows))
	}
	got := rows[0]
	if !reflect.DeepEqual(got.SupportedMimeTypes, []string{"image/png", "application/pdf"}) {
		t.Errorf("supported_mime_types = %v", got.SupportedMimeTypes)
	}
	if !got.Disabled {
		t.Errorf("disabled did not survive the write")
	}
	if got.QuotaInfo == nil || got.QuotaInfo.RemainingFraction == nil || *got.QuotaInfo.RemainingFraction != 0.25 {
		t.Errorf("quota_info did not survive the write: %+v", got.QuotaInfo)
	}
	if got.QuotaInfo != nil && got.QuotaInfo.ResetTime != "2026-10-27T00:00:00Z" {
		t.Errorf("quota_info.reset_time = %q", got.QuotaInfo.ResetTime)
	}
	if got.SupportsThoughtCirculation == nil || !*got.SupportsThoughtCirculation {
		t.Errorf("supports_thought_circulation did not survive the write: %+v", got.SupportsThoughtCirculation)
	}
	if got.ModelURL != "https://example.com/models/full" {
		t.Errorf("model_url = %q", got.ModelURL)
	}
	if got.TagTitle != "Fast" || got.TagDescription != "A quick model" {
		t.Errorf("the tag line did not survive the write: %+v", got)
	}
	if got.EffortSupported == nil || *got.EffortSupported {
		t.Errorf("effort_supported did not survive the write: %+v", got.EffortSupported)
	}

	// Clearing the quota removes the object, not just its fields.
	full.QuotaInfo = nil
	if err := ed.PutModel("gateway", full); err != nil {
		t.Fatalf("PutModel: %v", err)
	}
	body = readFile(t, path)
	if strings.Contains(body, "quota_info") {
		t.Errorf("the cleared quota is still in the file:\n%s", body)
	}
}

// TestEditor_PutModelReplacesTheModelAttributes pins that a second
// write replaces the attributes rather than merging them: a model that
// used to take PDF and no longer does must say so in the file.
func TestEditor_PutModelReplacesTheModelAttributes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "providers.json")
	ed := NewEditor(path)
	if err := ed.PutProvider("gateway", ProviderConfig{ID: "gateway", BaseURL: "https://x/v1"}); err != nil {
		t.Fatalf("PutProvider: %v", err)
	}
	entry := ProviderModel{ID: "m", Model: "M", SupportedMimeTypes: []string{"image/png"}}
	if err := ed.PutModel("gateway", entry); err != nil {
		t.Fatalf("PutModel: %v", err)
	}
	entry.SupportedMimeTypes = nil
	if err := ed.PutModel("gateway", entry); err != nil {
		t.Fatalf("PutModel: %v", err)
	}
	body := readFile(t, path)
	if strings.Contains(body, "supported_mime_types") {
		t.Errorf("the old list is still in the file:\n%s", body)
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
