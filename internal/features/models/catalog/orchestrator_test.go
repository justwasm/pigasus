package catalog

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/justwasm/pigasus/internal/core/confhome"
	"github.com/justwasm/pigasus/internal/core/settings"
	"github.com/justwasm/pigasus/internal/features/models/configtest"
)

func TestDefaultOrchestratorEffort(t *testing.T) {
	cases := []struct {
		name    string
		efforts []string
		want    string
	}{
		{
			name:    "prefers high when xhigh is also offered",
			efforts: []string{"low", "medium", "high", "xhigh"},
			want:    "high",
		},
		{
			name:    "prefers high when max is also offered",
			efforts: []string{"low", "medium", "high", "max"},
			want:    "high",
		},
		{
			name:    "falls back to highest offered when high unavailable (low/medium)",
			efforts: []string{"low", "medium"},
			want:    "medium",
		},
		{
			name:    "falls back to highest offered when high unavailable (xhigh/max)",
			efforts: []string{"xhigh", "max"},
			want:    "max",
		},
		{
			name:    "empty efforts",
			efforts: nil,
			want:    "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DefaultOrchestratorEffort(tc.efforts); got != tc.want {
				t.Fatalf("DefaultOrchestratorEffort(%v) = %q, want %q", tc.efforts, got, tc.want)
			}
		})
	}
}

func TestResolveOrchestratorConfig_DefaultsToHighWithoutMutatingSettings(t *testing.T) {
	configtest.Isolate(t)
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	t.Setenv("AGY_CLAUDE_55", "1")

	settingsPath := filepath.Join(confhome.Antigravity(), "settings.json")
	initial := "{\n  \"model\": \"gemini-pro-orchestrator\",\n  \"orchestrator\": {\n    \"planner\": \"gemini-4-neon\",\n    \"executor\": \"gemini-4-hydrogen\"\n  }\n}\n"
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsPath, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := ResolveOrchestratorConfig()
	if cfg.Planner.Effort != "high" || cfg.Planner.Current != "gemini-4-neon-high" {
		t.Fatalf("Planner = %+v, want gemini-4-neon-high / high", cfg.Planner)
	}
	if cfg.Executor.Effort != "high" || cfg.Executor.Current != "gemini-4-hydrogen-high" {
		t.Fatalf("Executor = %+v, want gemini-4-hydrogen-high / high", cfg.Executor)
	}

	after, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != initial {
		t.Fatalf("settings.json was mutated on read:\ngot:\n%s\nwant:\n%s", string(after), initial)
	}
}

func TestResolveOrchestratorRole_FallbackWhenHighUnavailable(t *testing.T) {
	families := []OrchestratorFamily{
		{
			BaseID:     "custom-no-high",
			Label:      "Custom No High",
			ShortLabel: "Custom",
			Efforts:    []string{"low", "medium"},
		},
	}
	role := ResolveOrchestratorRole(families, "")
	if role.Effort != "medium" || role.Current != "custom-no-high-medium" {
		t.Fatalf("ResolveOrchestratorRole = %+v, want effort=medium current=custom-no-high-medium", role)
	}
}

func TestResolvePlanExecute(t *testing.T) {
	configtest.Isolate(t)
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	if err := settings.SetOrchestrator(settings.OrchestratorSettings{
		Planner:  "gemini-4-neon-medium",
		Executor: "gemini-4-hydrogen-high",
	}); err != nil {
		t.Fatal(err)
	}

	planner, effort, exec, ok := ResolvePlanExecute("gemini-pro-orchestrator")
	if !ok {
		t.Fatal("expected ResolvePlanExecute ok=true for gemini-pro-orchestrator")
	}
	if planner != "gemini-4-neon-medium" || effort != "medium" || exec != "gemini-4-hydrogen-high" {
		t.Fatalf("ResolvePlanExecute = (%q, %q, %q), want (gemini-4-neon-medium, medium, gemini-4-hydrogen-high)", planner, effort, exec)
	}

	if _, _, _, ok := ResolvePlanExecute("gemini-3.8-flash-high"); ok {
		t.Fatal("expected ResolvePlanExecute ok=false for non-orchestrator model")
	}
}
