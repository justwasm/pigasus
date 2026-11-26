package modelpicker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/internal/core/confhome"
	"github.com/justwasm/pigasus/internal/core/settings"
	"github.com/justwasm/pigasus/internal/features/models/catalog"
	"github.com/justwasm/pigasus/internal/features/models/configtest"
)

func TestOrchestratorPanel_DefaultsToHighWithoutMutatingSettings(t *testing.T) {
	configtest.Isolate(t)
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	t.Setenv("AGY_ADC_AUTH", "true")

	settingsPath := filepath.Join(confhome.Antigravity(), "settings.json")
	initial := "{\n  \"model\": \"gemini-pro-orchestrator\",\n  \"orchestrator\": {\n    \"planner\": \"gemini-4-neon\",\n    \"executor\": \"gemini-4-hydrogen\"\n  }\n}\n"
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsPath, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}

	p := New(Seams{
		CurrentModelLabel: func() string { return "Orchestrator" },
	})
	rows := p.Rows()
	orchIdx := -1
	for i, r := range rows {
		if r.HasOrchestrator() {
			orchIdx = i
			break
		}
	}
	if orchIdx < 0 {
		t.Fatal("expected Orchestrator row under AGY_ADC_AUTH=true")
	}
	p.SetCursor(orchIdx)

	row := p.Rows()[orchIdx]
	if got := row.Orchestrator.Planner.CurrentEffort(); got != "high" {
		t.Fatalf("Planner default effort = %q, want high", got)
	}
	if got := row.Orchestrator.Executor.CurrentEffort(); got != "high" {
		t.Fatalf("Executor default effort = %q, want high", got)
	}

	rendered := strings.Join(p.Lines(30), "\n")
	if !strings.Contains(rendered, "Planner") || !strings.Contains(rendered, "Executor") || !strings.Contains(rendered, "Effort") {
		t.Fatalf("expected Planner, Executor, and Effort controls in rendered panel:\n%s", rendered)
	}

	after, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != initial {
		t.Fatalf("opening /model mutated settings.json:\ngot:\n%s\nwant:\n%s", string(after), initial)
	}
}

func TestOrchestratorPanel_TabAndArrowsStepAndPersist(t *testing.T) {
	configtest.Isolate(t)
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	t.Setenv("AGY_ADC_AUTH", "true")

	p := New(Seams{
		CurrentModelLabel: func() string { return "Orchestrator" },
	})
	rows := p.Rows()
	orchIdx := -1
	for i, r := range rows {
		if r.HasOrchestrator() {
			orchIdx = i
			break
		}
	}
	if orchIdx < 0 {
		t.Fatal("expected Orchestrator row")
	}
	p.SetCursor(orchIdx)

	// Tab to Planner Effort, then Right to wrap from high -> low (Carbon has low/medium/high).
	p.HandleKey(tea.Key{Code: tea.KeyTab})
	p.HandleKey(tea.Key{Code: tea.KeyRight})

	saved := settings.Orchestrator()
	if saved.Planner != "gemini-4-carbon-low" {
		t.Fatalf("saved Planner = %q, want gemini-4-carbon-low", saved.Planner)
	}
	if saved.Executor != "gemini-4-hydrogen-high" {
		t.Fatalf("saved Executor = %q, want gemini-4-hydrogen-high", saved.Executor)
	}
}

func TestOrchestratorRoleUI_StepFamilyFallbackWhenHighUnavailable(t *testing.T) {
	st := NewOrchestratorRowState(catalog.OrchestratorConfig{
		Planner: catalog.OrchestratorRoleState{
			Families: []catalog.OrchestratorFamily{
				{BaseID: "fam-a", Label: "Fam A", Efforts: []string{"low", "medium", "high"}},
				{BaseID: "fam-b", Label: "Fam B", Efforts: []string{"low", "medium"}},
			},
			BaseID: "fam-a",
			Effort: "high",
		},
		Executor: catalog.OrchestratorRoleState{
			Families: []catalog.OrchestratorFamily{
				{BaseID: "exec-a", Label: "Exec A", Efforts: []string{"xhigh", "max"}},
			},
			BaseID: "exec-a",
			Effort: "max",
		},
	})
	st.StepFocused(+1)
	if got := st.Planner.CurrentSlug(); got != "fam-b-medium" {
		t.Fatalf("Planner after stepping to family without high = %q, want fam-b-medium", got)
	}
	if got := st.Executor.CurrentSlug(); got != "exec-a-max" {
		t.Fatalf("Executor without high = %q, want exec-a-max", got)
	}
}
