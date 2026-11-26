package catalog

import (
	"strings"

	"github.com/justwasm/pigasus/internal/core/settings"
	"github.com/justwasm/pigasus/internal/features/models"
)

// OrchestratorFamily describes one selectable model family for an Orchestrator
// role (Planner or Executor) and the reasoning effort levels it offers.
type OrchestratorFamily struct {
	BaseID     string
	Label      string
	ShortLabel string
	Efforts    []string
}

// OrchestratorRoleState holds the resolved state for one Orchestrator role
// (Planner or Executor).
type OrchestratorRoleState struct {
	Families []OrchestratorFamily
	Current  string
	BaseID   string
	Effort   string
	Custom   bool
}

// OrchestratorConfig holds the resolved Planner and Executor configuration.
type OrchestratorConfig struct {
	Planner  OrchestratorRoleState
	Executor OrchestratorRoleState
	Custom   bool
}

// DefaultOrchestratorEffort returns "high" when offered by efforts, or falls
// back to the highest offered effort level according to models.EffortLevels.
func DefaultOrchestratorEffort(efforts []string) string {
	if len(efforts) == 0 {
		return ""
	}
	offered := make(map[string]string, len(efforts))
	for _, e := range efforts {
		norm := strings.ToLower(strings.TrimSpace(e))
		if norm != "" {
			offered[norm] = norm
		}
	}
	if _, ok := offered["high"]; ok {
		return "high"
	}
	for i := len(models.EffortLevels) - 1; i >= 0; i-- {
		lvl := models.EffortLevels[i]
		if _, ok := offered[lvl]; ok {
			return lvl
		}
	}
	return strings.ToLower(strings.TrimSpace(efforts[len(efforts)-1]))
}

// IsOrchestratorModel reports whether model identifies an Orchestrator /
// Coordinator model (by catalogue ID or display label).
func IsOrchestratorModel(model string) bool {
	norm := strings.ToLower(strings.TrimSpace(model))
	switch norm {
	case "gemini-pro-coordinator",
		"gemini-pro-orchestrator",
		"gemini-pro-orchestrator-argon",
		"orchestrator",
		"orchestrator (argon)":
		return true
	}
	return strings.HasPrefix(norm, "gemini-pro-orchestrator-")
}

// OrchestratorPlannerFamilies returns the eligible Planner model families.
func OrchestratorPlannerFamilies() []OrchestratorFamily {
	saved := settings.Orchestrator()
	families := []OrchestratorFamily{
		{
			BaseID:     "gemini-4-carbon",
			Label:      "Gemini 4 Carbon",
			ShortLabel: "Carbon",
			Efforts:    []string{"low", "medium", "high"},
		},
		{
			BaseID:     "gemini-4-argon",
			Label:      "Gemini 4 Argon",
			ShortLabel: "Argon",
			Efforts:    []string{"low", "medium", "high"},
		},
		{
			BaseID:     "gemini-4-neon",
			Label:      "Gemini 4 Neon",
			ShortLabel: "Neon",
			Efforts:    []string{"low", "medium", "high", "xhigh"},
		},
	}
	if Claude55Available() || strings.HasPrefix(strings.ToLower(saved.Planner), "claude-opus-5-5") {
		families = append(families, OrchestratorFamily{
			BaseID:     "claude-opus-5-5",
			Label:      "Claude Opus 5.5",
			ShortLabel: "Opus 5.5",
			Efforts:    []string{"low", "medium", "high", "max"},
		})
	}
	return families
}

// OrchestratorExecutorFamilies returns the eligible Executor model families.
func OrchestratorExecutorFamilies() []OrchestratorFamily {
	saved := settings.Orchestrator()
	families := []OrchestratorFamily{
		{
			BaseID:     "gemini-4-hydrogen",
			Label:      "Gemini 4 Hydrogen",
			ShortLabel: "Hydrogen",
			Efforts:    []string{"low", "medium", "high", "xhigh"},
		},
	}
	if Claude55Available() || strings.HasPrefix(strings.ToLower(saved.Executor), "claude-sonnet-5-5") {
		families = append(families, OrchestratorFamily{
			BaseID:     "claude-sonnet-5-5",
			Label:      "Claude Sonnet 5.5",
			ShortLabel: "Sonnet 5.5",
			Efforts:    []string{"low", "medium", "high", "max"},
		})
	}
	return families
}

// ResolveOrchestratorRole resolves the active family and effort for one role
// given its eligible families and the saved settings value, without mutating
// settings.json.
func ResolveOrchestratorRole(families []OrchestratorFamily, saved string) OrchestratorRoleState {
	if len(families) == 0 {
		return OrchestratorRoleState{}
	}
	saved = strings.TrimSpace(saved)
	chosen := families[0]
	effort := ""
	custom := false

	if saved != "" {
		if fam, eff, ok := matchOrchestratorFamily(families, saved); ok {
			chosen = fam
			custom = true
			if eff != "" && containsEffort(fam.Efforts, eff) {
				effort = eff
			}
		}
	}
	if effort == "" {
		effort = DefaultOrchestratorEffort(chosen.Efforts)
	}
	current := chosen.BaseID
	if effort != "" {
		current = chosen.BaseID + "-" + effort
	}
	return OrchestratorRoleState{
		Families: families,
		Current:  current,
		BaseID:   chosen.BaseID,
		Effort:   effort,
		Custom:   custom,
	}
}

// ResolveOrchestratorConfig resolves the current Orchestrator Planner and
// Executor configuration from settings.json without mutating saved settings.
func ResolveOrchestratorConfig() OrchestratorConfig {
	saved := settings.Orchestrator()
	planner := ResolveOrchestratorRole(OrchestratorPlannerFamilies(), saved.Planner)
	executor := ResolveOrchestratorRole(OrchestratorExecutorFamilies(), saved.Executor)
	return OrchestratorConfig{
		Planner:  planner,
		Executor: executor,
		Custom:   planner.Custom || executor.Custom,
	}
}

// ResolvePlanExecute resolves the active Orchestrator Planner model, Planner
// effort, and Executor model for activeModel. Returns ok=false when activeModel
// is not an Orchestrator model.
func ResolvePlanExecute(activeModel string) (plannerModel, plannerEffort, executionModel string, ok bool) {
	if !IsOrchestratorModel(activeModel) {
		return "", "", "", false
	}
	saved := settings.Orchestrator()
	cfg := ResolveOrchestratorConfig()

	norm := strings.ToLower(strings.TrimSpace(activeModel))
	if (norm == "gemini-pro-orchestrator-argon" || norm == "orchestrator (argon)") && saved.Planner == "" {
		argon := ResolveOrchestratorRole(cfg.Planner.Families, "gemini-4-argon")
		cfg.Planner = argon
	}

	if norm == "gemini-pro-coordinator" && !cfg.Custom {
		return cfg.Planner.Current, cfg.Planner.Effort, "", true
	}
	return cfg.Planner.Current, cfg.Planner.Effort, cfg.Executor.Current, true
}

func matchOrchestratorFamily(families []OrchestratorFamily, saved string) (OrchestratorFamily, string, bool) {
	norm := strings.ToLower(strings.TrimSpace(saved))
	for _, fam := range families {
		base := strings.ToLower(fam.BaseID)
		if norm == base {
			return fam, "", true
		}
		if strings.HasPrefix(norm, base+"-") {
			eff := strings.TrimPrefix(norm, base+"-")
			return fam, eff, true
		}
	}
	return OrchestratorFamily{}, "", false
}

func containsEffort(efforts []string, target string) bool {
	target = strings.ToLower(strings.TrimSpace(target))
	for _, e := range efforts {
		if strings.ToLower(strings.TrimSpace(e)) == target {
			return true
		}
	}
	return false
}
