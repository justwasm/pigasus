package providerpanel

import (
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/models"
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
