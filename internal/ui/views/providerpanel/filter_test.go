// filter_test.go — tests for provider panel tab and search filtering.
package providerpanel

import (
	"testing"

	"github.com/justwasm/pigasus/internal/features/models"
)

func TestFilterRows_TabFiltering(t *testing.T) {
	providers := map[string]models.ProviderConfig{
		"gateway": {
			ID:      "gateway",
			Type:    "openai",
			BaseURL: "https://gw.example/v1",
			Models: []models.ProviderModel{
				{ID: "m1", Name: "Model One"},
			},
		},
	}
	allRows := BuildRows(providers)

	// TabConfigured: must contain gateway and builtin, but NO RowPreset
	cfgRows := FilterRows(allRows, providers, TabConfigured, "")
	for _, r := range cfgRows {
		if r.Kind == RowPreset {
			t.Fatalf("TabConfigured contains RowPreset: %+v", r)
		}
	}
	hasGateway := false
	hasBuiltin := false
	for _, r := range cfgRows {
		if r.Kind == RowProvider && r.Provider == "gateway" {
			hasGateway = true
		}
		if r.Kind == RowBuiltin {
			hasBuiltin = true
		}
	}
	if !hasGateway || !hasBuiltin {
		t.Fatalf("TabConfigured missing gateway (%v) or builtin (%v)", hasGateway, hasBuiltin)
	}

	// TabPresets: must contain ONLY RowPreset
	presetRows := FilterRows(allRows, providers, TabPresets, "")
	if len(presetRows) == 0 {
		t.Fatal("TabPresets returned 0 rows")
	}
	for _, r := range presetRows {
		if r.Kind != RowPreset {
			t.Fatalf("TabPresets contains non-preset row: %+v", r)
		}
	}

	// TabAll: must contain gateway, builtin, and preset
	allFiltered := FilterRows(allRows, providers, TabAll, "")
	if len(allFiltered) != len(allRows) {
		t.Fatalf("TabAll len = %d, want %d", len(allFiltered), len(allRows))
	}
}

func TestFilterRows_QueryFiltering(t *testing.T) {
	providers := map[string]models.ProviderConfig{
		"gateway": {
			ID:      "gateway",
			Type:    "openai",
			BaseURL: "https://gw.example/v1",
			Models: []models.ProviderModel{
				{ID: "minimax-m3", Name: "MiniMax M3", Model: "MiniMax-M3"},
				{ID: "other-model", Name: "Other"},
			},
		},
	}
	allRows := BuildRows(providers)

	// 1. Search matching a model: should include parent provider and that model, not the other model
	mRows := FilterRows(allRows, providers, TabAll, "minimax")
	if len(mRows) < 2 {
		t.Fatalf("want at least provider + minimax model, got: %+v", mRows)
	}
	if mRows[0].Kind != RowProvider || mRows[0].Provider != "gateway" {
		t.Errorf("row 0 want gateway provider header, got: %+v", mRows[0])
	}
	if mRows[1].Kind != RowModel || mRows[1].Model != "minimax-m3" {
		t.Errorf("row 1 want minimax-m3 model, got: %+v", mRows[1])
	}
	for _, r := range mRows {
		if r.Model == "other-model" {
			t.Errorf("other-model should have been filtered out: %+v", r)
		}
	}

	// 2. Search matching a builtin model
	bRows := FilterRows(allRows, providers, TabAll, "claude-sonnet")
	if len(bRows) < 2 {
		t.Fatalf("want builtin header + claude model, got: %+v", bRows)
	}
	if bRows[0].Kind != RowBuiltin || bRows[0].Model != "" {
		t.Errorf("row 0 want builtin header, got: %+v", bRows[0])
	}
	if bRows[1].Kind != RowBuiltin || bRows[1].Model != "claude-sonnet-4-6" {
		t.Errorf("row 1 want claude-sonnet-4-6, got: %+v", bRows[1])
	}

	// 3. Search matching Catwalk preset by provider ID
	deepseekRows := FilterRows(allRows, providers, TabPresets, "deepseek")
	if len(deepseekRows) == 0 {
		t.Fatal("expected deepseek preset to match 'deepseek'")
	}
	foundDeepseek := false
	for _, r := range deepseekRows {
		if r.Kind == RowPreset && r.Provider == "deepseek" {
			foundDeepseek = true
		}
	}
	if !foundDeepseek {
		t.Errorf("deepseek not found in results: %+v", deepseekRows)
	}

	// 4. Search matching Catwalk preset by Catwalk model ID (e.g. claude-sonnet matching anthropic)
	anthropicRows := FilterRows(allRows, providers, TabPresets, "claude-sonnet")
	foundAnthropic := false
	for _, r := range anthropicRows {
		if r.Kind == RowPreset && r.Provider == "anthropic" {
			foundAnthropic = true
		}
	}
	if !foundAnthropic {
		t.Errorf("expected anthropic preset to match 'claude-sonnet', got: %+v", anthropicRows)
	}

	// 5. Non-matching query returns empty
	empty := FilterRows(allRows, providers, TabAll, "nonexistent-query-xyz-123")
	if len(empty) != 0 {
		t.Fatalf("expected empty for non-matching query, got: %+v", empty)
	}
}

func TestFilterRows_Collapse(t *testing.T) {
	providers := map[string]models.ProviderConfig{
		"gateway": {
			ID:      "gateway",
			Type:    "openai",
			BaseURL: "https://gw.example/v1",
			Models: []models.ProviderModel{
				{ID: "m1", Name: "Model 1"},
				{ID: "m2", Name: "Model 2"},
			},
		},
	}
	allRows := BuildRows(providers)

	// 1. By default with nil or empty collapse map, providers are expanded
	uncollapsed := FilterRowsWithCollapse(allRows, providers, TabConfigured, "", nil)
	foundM1 := false
	for _, r := range uncollapsed {
		if r.Model == "m1" {
			foundM1 = true
		}
	}
	if !foundM1 {
		t.Fatal("expected m1 to be present when uncollapsed")
	}

	// 2. When gateway is collapsed, child models are omitted and marker is not expanded
	collapsedMap := map[string]bool{"gateway": true}
	collapsed := FilterRowsWithCollapse(allRows, providers, TabConfigured, "", collapsedMap)
	for _, r := range collapsed {
		if r.Kind == RowModel && r.Provider == "gateway" {
			t.Fatalf("expected child model to be hidden when gateway collapsed, got: %+v", r)
		}
		if r.Kind == RowProvider && r.Provider == "gateway" && r.Expanded {
			t.Fatal("gateway row should have Expanded = false")
		}
	}

	// 3. When a search query matches a model under a collapsed provider, it auto-expands
	searched := FilterRowsWithCollapse(allRows, providers, TabConfigured, "m1", collapsedMap)
	foundModel := false
	for _, r := range searched {
		if r.Kind == RowProvider && r.Provider == "gateway" {
			if !r.Expanded {
				t.Fatal("expected gateway row to be auto-expanded in search results")
			}
		}
		if r.Kind == RowModel && r.Model == "m1" {
			foundModel = true
		}
	}
	if !foundModel {
		t.Fatal("expected m1 to be shown in search results despite collapse map")
	}
}
