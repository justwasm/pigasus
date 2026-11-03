package providerpanel

import (
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/features/models"
)

func TestBuildRows(t *testing.T) {
	providers := map[string]models.ProviderConfig{
		"custom": {
			ID: "custom",
			Models: []models.ProviderModel{
				{ID: "m1", Name: "Model 1"},
			},
		},
	}
	rows := BuildRows(providers)
	if len(rows) == 0 {
		t.Fatal("BuildRows returned empty slice")
	}
	foundCustom := false
	foundM1 := false
	for _, r := range rows {
		if r.Kind == RowProvider && r.Provider == "custom" {
			foundCustom = true
		}
		if r.Kind == RowModel && r.Model == "m1" {
			foundM1 = true
		}
	}
	if !foundCustom || !foundM1 {
		t.Errorf("BuildRows missing expected provider/model rows: %v", rows)
	}

	foundPresetHeader := false
	foundDeepseekPreset := false
	for _, r := range rows {
		if r.Kind == RowPreset && r.Provider == PresetsProviderLabel {
			foundPresetHeader = true
		}
		if r.Kind == RowPreset && r.Provider == "deepseek" {
			foundDeepseekPreset = true
			if !strings.Contains(r.Label, "DeepSeek") {
				t.Errorf("expected DeepSeek in label, got %q", r.Label)
			}
		}
	}
	if !foundPresetHeader || !foundDeepseekPreset {
		t.Errorf("BuildRows missing preset rows: %v", rows)
	}
}

func TestRowText(t *testing.T) {
	txt := RowText("test", "val", false, false, "<blue>", "</blue>")
	if !strings.HasPrefix(txt, "  test") {
		t.Errorf("RowText prefix want '  test', got %q", txt)
	}
	cursorTxt := RowText("test", "val", true, true, "<blue>", "</blue>")
	if !strings.Contains(cursorTxt, "<blue>> test</blue>") {
		t.Errorf("RowText with cursor want highlighted, got %q", cursorTxt)
	}

	// Top-level row: name starts at column 2 both with and without cursor.
	topPlain := RowText("provider1", "val", false, false, "", "")
	topCursor := RowText("provider1", "val", true, false, "", "")
	if !strings.HasPrefix(topPlain, "  provider1") {
		t.Errorf("topPlain prefix want '  provider1', got %q", topPlain)
	}
	if !strings.HasPrefix(topCursor, "> provider1") {
		t.Errorf("topCursor prefix want '> provider1', got %q", topCursor)
	}

	// Indented child row: name starts at column 4 both with and without cursor.
	childPlain := RowText("model1", "val", false, true, "", "")
	childCursor := RowText("model1", "val", true, true, "", "")
	if !strings.HasPrefix(childPlain, "    model1") {
		t.Errorf("childPlain prefix want '    model1', got %q", childPlain)
	}
	if !strings.HasPrefix(childCursor, "  > model1") {
		t.Errorf("childCursor prefix want '  > model1', got %q", childCursor)
	}

	// Value column alignment should match exactly between cursor and non-cursor.
	if strings.Index(topPlain, "val") != strings.Index(topCursor, "val") {
		t.Errorf("top-level val alignment mismatch: plain=%d, cursor=%d",
			strings.Index(topPlain, "val"), strings.Index(topCursor, "val"))
	}
	if strings.Index(childPlain, "val") != strings.Index(childCursor, "val") {
		t.Errorf("child val alignment mismatch: plain=%d, cursor=%d",
			strings.Index(childPlain, "val"), strings.Index(childCursor, "val"))
	}
}

func TestRowValues(t *testing.T) {
	p := models.ProviderConfig{
		ID:      "test",
		Type:    "openai",
		BaseURL: "http://localhost:8080",
		Models: []models.ProviderModel{
			{ID: "m1", Name: "Model One", Model: "m1-wire", ContextWindow: 4096},
		},
	}
	pv := ProviderRowValue(p)
	if !strings.Contains(pv, "openai") || !strings.Contains(pv, "http://localhost:8080") {
		t.Errorf("ProviderRowValue = %q", pv)
	}
	mv := ModelRowValue(p, "m1")
	if !strings.Contains(mv, "Model One") || !strings.Contains(mv, "m1-wire") {
		t.Errorf("ModelRowValue = %q", mv)
	}
}
