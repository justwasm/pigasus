package settings

import (
	"encoding/json"
	"strings"
)

// OrchestratorSettings mirrors the "orchestrator" object in settings.json.
type OrchestratorSettings struct {
	Planner  string `json:"planner,omitempty"`
	Executor string `json:"executor,omitempty"`
}

// Orchestrator reads the saved "orchestrator" configuration from settings.json
// without mutating the file on disk.
func Orchestrator() OrchestratorSettings {
	data, err := readSettingsBytes()
	if err != nil || len(data) == 0 {
		return OrchestratorSettings{}
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(data, &doc); err != nil {
		return OrchestratorSettings{}
	}
	raw, ok := doc["orchestrator"]
	if !ok || len(raw) == 0 || string(raw) == "null" {
		return OrchestratorSettings{}
	}
	var cfg OrchestratorSettings
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return OrchestratorSettings{}
	}
	cfg.Planner = strings.TrimSpace(cfg.Planner)
	cfg.Executor = strings.TrimSpace(cfg.Executor)
	return cfg
}

// SetOrchestrator persists the "orchestrator" configuration to settings.json,
// deleting the key when both Planner and Executor are empty.
func SetOrchestrator(cfg OrchestratorSettings) error {
	cfg.Planner = strings.TrimSpace(cfg.Planner)
	cfg.Executor = strings.TrimSpace(cfg.Executor)
	if cfg.Planner == "" && cfg.Executor == "" {
		return DeleteField("orchestrator")
	}
	return SetField("orchestrator", cfg)
}
