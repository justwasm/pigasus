package providerpanel

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/models"
)

func TestValidateAndSaveProvider_NilOrEmptyForm(t *testing.T) {
	_, _, _, err := ValidateAndSaveProvider(nil, nil, nil)
	if err == nil || err.Error() != "the provider needs an id" {
		t.Fatalf("expected 'the provider needs an id', got %v", err)
	}

	form := &Form{}
	_, _, _, err = ValidateAndSaveProvider(nil, nil, form)
	if err == nil || err.Error() != "the provider needs an id" {
		t.Fatalf("expected 'the provider needs an id', got %v", err)
	}

	form = &Form{
		Fields: []FormField{{Label: FieldID, Value: "   "}},
	}
	_, _, _, err = ValidateAndSaveProvider(nil, nil, form)
	if err == nil || err.Error() != "the provider needs an id" {
		t.Fatalf("expected 'the provider needs an id', got %v", err)
	}
}

func TestValidateAndSaveProvider_MissingEndpoint(t *testing.T) {
	form := &Form{
		Provider: "testprov",
	}
	_, _, _, err := ValidateAndSaveProvider(nil, nil, form)
	if err == nil || err.Error() != "the provider needs a base URL or an API URL" {
		t.Fatalf("expected 'the provider needs a base URL or an API URL', got %v", err)
	}
}

func TestValidateAndSaveProvider_NewProviderSuccess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "providers.json")
	ed := models.NewEditor(path)
	form := &Form{
		Fields: []FormField{
			{Label: FieldID, Value: "newprov"},
			{Label: FieldName, Value: "New Provider"},
			{Label: FieldType, Value: models.ProviderTypeOpenAI},
			{Label: FieldBaseURL, Value: "https://example.com/v1"},
			{Label: FieldAPIKey, Value: "secret-key"},
		},
	}
	id, p, probe, err := ValidateAndSaveProvider(ed, nil, form)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "newprov" {
		t.Errorf("got id %q, want 'newprov'", id)
	}
	if p.BaseURL != "https://example.com/v1" {
		t.Errorf("got base URL %q, want 'https://example.com/v1'", p.BaseURL)
	}
	if p.APIKey != "secret-key" {
		t.Errorf("got api key %q, want 'secret-key'", p.APIKey)
	}
	if probe != "" {
		t.Errorf("got probe %q, want empty", probe)
	}

	list, err := ed.List()
	if err != nil || list["newprov"].ID != "newprov" {
		t.Fatalf("provider not found in editor: %v, list: %+v", err, list)
	}
}

func TestValidateAndSaveProvider_ExistingProviderWithModels(t *testing.T) {
	path := filepath.Join(t.TempDir(), "providers.json")
	ed := models.NewEditor(path)
	existing := models.ProviderConfig{
		ID:      "exist",
		BaseURL: "https://old.com/v1",
		Models: []models.ProviderModel{
			{ID: "m1", Model: "gpt-4-wire"},
		},
	}
	if err := ed.PutProvider("exist", existing); err != nil {
		t.Fatalf("PutProvider: %v", err)
	}

	providers := map[string]models.ProviderConfig{"exist": existing}
	form := &Form{
		Provider: "exist",
		Fields: []FormField{
			{Label: FieldBaseURL, Value: "https://new.com/v1"},
		},
	}
	id, p, probe, err := ValidateAndSaveProvider(ed, providers, form)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "exist" || p.BaseURL != "https://new.com/v1" {
		t.Errorf("unexpected id or url: id=%s, url=%s", id, p.BaseURL)
	}
	if probe != "gpt-4-wire" {
		t.Errorf("got probe %q, want 'gpt-4-wire'", probe)
	}
}

func TestValidateAndSaveProvider_PutError(t *testing.T) {
	// Using a directory as file path causes edit to fail when reading/writing.
	ed := models.NewEditor(t.TempDir())
	form := &Form{
		Provider: "prov",
		Fields: []FormField{
			{Label: FieldBaseURL, Value: "https://example.com/v1"},
		},
	}
	_, _, _, err := ValidateAndSaveProvider(ed, nil, form)
	if err == nil {
		t.Fatal("expected error from ed.PutProvider, got nil")
	}
}

func TestValidateAndSaveModel_NilOrEmptyProvider(t *testing.T) {
	_, _, err := ValidateAndSaveModel(nil, nil, nil)
	if err == nil || err.Error() != "the model needs a provider" {
		t.Fatalf("expected 'the model needs a provider', got %v", err)
	}

	form := &Form{}
	_, _, err = ValidateAndSaveModel(nil, nil, form)
	if err == nil || err.Error() != "the model needs a provider" {
		t.Fatalf("expected 'the model needs a provider', got %v", err)
	}

	form = &Form{Provider: "unknown"}
	_, _, err = ValidateAndSaveModel(nil, nil, form)
	if err == nil || !strings.Contains(err.Error(), `no provider named "unknown"`) {
		t.Fatalf("expected 'no provider named \"unknown\"', got %v", err)
	}
}

func TestValidateAndSaveModel_EmptyModelID(t *testing.T) {
	providers := map[string]models.ProviderConfig{
		"myprov": {ID: "myprov"},
	}
	form := &Form{Provider: "myprov"}
	_, _, err := ValidateAndSaveModel(nil, providers, form)
	if err == nil || err.Error() != "the model needs an id" {
		t.Fatalf("expected 'the model needs an id', got %v", err)
	}
}

func TestValidateAndSaveModel_SuccessWithUpstream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "providers.json")
	ed := models.NewEditor(path)
	if err := ed.PutProvider("myprov", models.ProviderConfig{ID: "myprov", BaseURL: "https://x/v1"}); err != nil {
		t.Fatalf("PutProvider: %v", err)
	}
	providers := map[string]models.ProviderConfig{
		"myprov": {ID: "myprov"},
	}
	form := &Form{
		Provider: "myprov",
		Fields: []FormField{
			{Label: FieldID, Value: "custom-model"},
			{Label: FieldUpstream, Value: "upstream-v1"},
		},
	}
	prov, wire, err := ValidateAndSaveModel(ed, providers, form)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prov != "myprov" {
		t.Errorf("got provider %q, want 'myprov'", prov)
	}
	if wire != "upstream-v1" {
		t.Errorf("got wire %q, want 'upstream-v1'", wire)
	}
}

func TestValidateAndSaveModel_SuccessDefaultWire(t *testing.T) {
	path := filepath.Join(t.TempDir(), "providers.json")
	ed := models.NewEditor(path)
	if err := ed.PutProvider("myprov", models.ProviderConfig{ID: "myprov", BaseURL: "https://x/v1"}); err != nil {
		t.Fatalf("PutProvider: %v", err)
	}
	providers := map[string]models.ProviderConfig{
		"myprov": {ID: "myprov"},
	}
	form := &Form{
		Provider: "myprov",
		Model:    "direct-model",
	}
	prov, wire, err := ValidateAndSaveModel(ed, providers, form)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prov != "myprov" {
		t.Errorf("got provider %q, want 'myprov'", prov)
	}
	if wire != "direct-model" {
		t.Errorf("got wire %q, want 'direct-model'", wire)
	}
}

func TestValidateAndSaveModel_PutError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "providers.json")
	ed := models.NewEditor(path)
	// provider exists in map, but not in editor file -> PutModel will fail.
	providers := map[string]models.ProviderConfig{
		"not_in_file": {ID: "not_in_file"},
	}
	form := &Form{
		Provider: "not_in_file",
		Model:    "m1",
	}
	_, _, err := ValidateAndSaveModel(ed, providers, form)
	if err == nil {
		t.Fatal("expected error from ed.PutModel, got nil")
	}
}
