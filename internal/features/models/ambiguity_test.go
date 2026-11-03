package models

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseModelRef(t *testing.T) {
	known := map[string]bool{
		"deepseek":   true,
		"openrouter": true,
	}

	// 1. Bare name
	prov, model := ParseModelRef("deepseek-chat", known)
	if prov != "" || model != "deepseek-chat" {
		t.Errorf("bare name got prov=%q, model=%q", prov, model)
	}

	// 2. Qualified name
	prov, model = ParseModelRef("deepseek/deepseek-chat", known)
	if prov != "deepseek" || model != "deepseek-chat" {
		t.Errorf("qualified name got prov=%q, model=%q", prov, model)
	}

	// 3. Model ID containing slashes under known provider
	prov, model = ParseModelRef("openrouter/meta-llama/llama-3.3-70b-instruct", known)
	if prov != "openrouter" || model != "meta-llama/llama-3.3-70b-instruct" {
		t.Errorf("multi-slash model got prov=%q, model=%q", prov, model)
	}

	// 4. Unknown provider prefix treated as bare model
	prov, model = ParseModelRef("foo/bar", known)
	if prov != "" || model != "foo/bar" {
		t.Errorf("unknown provider got prov=%q, model=%q", prov, model)
	}

	// 5. Without known map (split at first slash)
	prov, model = ParseModelRef("custom/my-model", nil)
	if prov != "custom" || model != "my-model" {
		t.Errorf("nil map split got prov=%q, model=%q", prov, model)
	}
}

func TestModelQualifiedName(t *testing.T) {
	m1 := &Model{Name: "chat", ProviderID: "deepseek"}
	if got := m1.QualifiedName(); got != "deepseek/chat" {
		t.Errorf("QualifiedName() = %q, want deepseek/chat", got)
	}

	m2 := &Model{Name: "chat"}
	if got := m2.QualifiedName(); got != "chat" {
		t.Errorf("QualifiedName() = %q, want chat", got)
	}
}

func TestRegistry_DisambiguatesQualifiedModels(t *testing.T) {
	// Setup two providers that both offer a model named "llama-3"
	reg := &Registry{
		providers: map[string]ProviderConfig{
			"groq": {
				ID:   "groq",
				Type: "openai",
				Models: []ProviderModel{
					{ID: "llama-3", Name: "Groq Llama 3"},
				},
			},
			"together": {
				ID:   "together",
				Type: "openai",
				Models: []ProviderModel{
					{ID: "llama-3", Name: "Together Llama 3"},
				},
			},
		},
	}

	// 1. Qualified lookup groq/llama-3
	mGroq, ok := reg.GetByName("groq/llama-3")
	if !ok || mGroq == nil {
		t.Fatal("GetByName(groq/llama-3) not found")
	}
	if mGroq.ProviderID != "groq" || mGroq.DisplayName != "Groq Llama 3" {
		t.Errorf("expected groq model, got %+v", mGroq)
	}

	// 2. Qualified lookup together/llama-3
	mTogether, ok := reg.GetByName("together/llama-3")
	if !ok || mTogether == nil {
		t.Fatal("GetByName(together/llama-3) not found")
	}
	if mTogether.ProviderID != "together" || mTogether.DisplayName != "Together Llama 3" {
		t.Errorf("expected together model, got %+v", mTogether)
	}

	// 3. ResolveModel on ambiguous bare name returns AmbiguousModelError
	resolved, err := reg.ResolveModel("llama-3")
	if resolved != nil || err == nil {
		t.Fatalf("expected AmbiguousModelError for bare llama-3, got model=%v, err=%v", resolved, err)
	}
	var ambErr *AmbiguousModelError
	if !errors.As(err, &ambErr) {
		t.Fatalf("expected *AmbiguousModelError, got %T: %v", err, err)
	}
	if ambErr.ModelID != "llama-3" || len(ambErr.Providers) != 2 {
		t.Errorf("ambiguous error = %+v", ambErr)
	}
	if !strings.Contains(ambErr.Error(), "found in multiple providers: groq, together") {
		t.Errorf("ambiguous error text = %q", ambErr.Error())
	}

	// 4. ResolveModel on qualified name succeeds
	resolvedGroq, err := reg.ResolveModel("groq/llama-3")
	if err != nil || resolvedGroq == nil {
		t.Fatalf("ResolveModel(groq/llama-3) failed: %v", err)
	}
	if resolvedGroq.ProviderID != "groq" {
		t.Errorf("resolved provider = %q, want groq", resolvedGroq.ProviderID)
	}
}

func TestEditor_DeleteModelFromProvider(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "providers.json")
	ed := NewEditor(path)

	// Add provider p1 and model m1
	if err := ed.PutProvider("p1", ProviderConfig{
		ID:      "p1",
		BaseURL: "https://p1.example/v1",
	}); err != nil {
		t.Fatalf("put p1: %v", err)
	}
	if err := ed.PutModel("p1", ProviderModel{ID: "m1", Name: "Model M1 in P1"}); err != nil {
		t.Fatalf("put m1 in p1: %v", err)
	}

	// Add provider p2 with same model ID m1
	if err := ed.PutProvider("p2", ProviderConfig{
		ID:      "p2",
		BaseURL: "https://p2.example/v1",
	}); err != nil {
		t.Fatalf("put p2: %v", err)
	}
	if err := ed.PutModel("p2", ProviderModel{ID: "m1", Name: "Model M1 in P2"}); err != nil {
		t.Fatalf("put m1 in p2: %v", err)
	}

	// Delete m1 specifically from p2
	removed, err := ed.DeleteModelFromProvider("p2", "m1")
	if err != nil {
		t.Fatalf("DeleteModelFromProvider: %v", err)
	}
	if !removed {
		t.Fatal("expected removed=true")
	}

	// Verify p1 still has m1!
	providers, err := ed.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(providers["p1"].Models) != 1 || providers["p1"].Models[0].ID != "m1" {
		t.Errorf("p1 models unexpectedly modified: %+v", providers["p1"].Models)
	}
	if len(providers["p2"].Models) != 0 {
		t.Errorf("p2 models still contains m1: %+v", providers["p2"].Models)
	}

	// Delete qualified string "p1/m1" via DeleteModel
	pid, ok, err := ed.DeleteModel("p1/m1")
	if err != nil || !ok || pid != "p1" {
		t.Fatalf("DeleteModel(p1/m1) = (%q, %v, %v), want (p1, true, nil)", pid, ok, err)
	}
	providers, _ = ed.List()
	if len(providers["p1"].Models) != 0 {
		t.Errorf("p1 models should now be empty: %+v", providers["p1"].Models)
	}
}
