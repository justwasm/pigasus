package routing

import (
	"encoding/json"
	"fmt"
	"strings"
)

// RoleName represents a designated agent role in a heterogeneous multi-model swarm.
type RoleName string

const (
	// RolePlanner specializes in high-level architecture, task breakdown, and planning.
	RolePlanner RoleName = "planner"
	// RoleCoder specializes in code generation, file editing, and direct implementation.
	RoleCoder RoleName = "coder"
	// RoleReviewer specializes in code auditing, edge-case checking, and test verification.
	RoleReviewer RoleName = "reviewer"
	// RoleSummarizer specializes in fast, token-efficient context compression and summaries.
	RoleSummarizer RoleName = "summarizer"
)

// StandardRoles returns the four canonical roles in a heterogeneous swarm.
func StandardRoles() []RoleName {
	return []RoleName{RolePlanner, RoleCoder, RoleReviewer, RoleSummarizer}
}

// NormalizeRole returns canonical RoleName for a given input string (case-insensitive, trimmed).
func NormalizeRole(s string) RoleName {
	cleaned := strings.ToLower(strings.TrimSpace(s))
	switch cleaned {
	case "planner":
		return RolePlanner
	case "coder":
		return RoleCoder
	case "reviewer":
		return RoleReviewer
	case "summarizer":
		return RoleSummarizer
	default:
		return RoleName(cleaned)
	}
}

// RoleModelConfig holds the model selection and execution parameters for a role.
type RoleModelConfig struct {
	Model           string         `json:"model"`
	Provider        string         `json:"provider,omitempty"`
	ReasoningEffort string         `json:"reasoning_effort,omitempty"`
	Temperature     *float32       `json:"temperature,omitempty"`
	MaxTokens       int            `json:"max_tokens,omitempty"`
	Extra           map[string]any `json:"extra,omitempty"`
}

// Clone creates a deep copy of RoleModelConfig.
func (c RoleModelConfig) Clone() RoleModelConfig {
	out := c
	if c.Temperature != nil {
		t := *c.Temperature
		out.Temperature = &t
	}
	if c.Extra != nil {
		out.Extra = make(map[string]any, len(c.Extra))
		for k, v := range c.Extra {
			out.Extra[k] = v
		}
	}
	return out
}

// DefaultBaselineRoles returns the baseline system defaults when no configuration is specified:
// - Planner: gemini-2.5-pro (deep reasoning, planning)
// - Coder: claude-3-7-sonnet (coding and tool accuracy)
// - Reviewer: claude-3-7-sonnet (critical review and verification)
// - Summarizer: gemini-2.5-flash (fast, large context window)
func DefaultBaselineRoles() map[RoleName]RoleModelConfig {
	return map[RoleName]RoleModelConfig{
		RolePlanner: {
			Model:           "gemini-2.5-pro",
			ReasoningEffort: "high",
		},
		RoleCoder: {
			Model:           "claude-3-7-sonnet",
			ReasoningEffort: "medium",
		},
		RoleReviewer: {
			Model:           "claude-3-7-sonnet",
			ReasoningEffort: "high",
		},
		RoleSummarizer: {
			Model: "gemini-2.5-flash",
		},
	}
}

// SwarmRolesConfig represents heterogeneous multi-model role configurations.
type SwarmRolesConfig struct {
	Planner     *RoleModelConfig              `json:"planner,omitempty"`
	Coder       *RoleModelConfig              `json:"coder,omitempty"`
	Reviewer    *RoleModelConfig              `json:"reviewer,omitempty"`
	Summarizer  *RoleModelConfig              `json:"summarizer,omitempty"`
	Default     *RoleModelConfig              `json:"default,omitempty"`
	CustomRoles map[RoleName]*RoleModelConfig `json:"custom_roles,omitempty"`
}

// NewSwarmRolesConfig creates an empty SwarmRolesConfig.
func NewSwarmRolesConfig() *SwarmRolesConfig {
	return &SwarmRolesConfig{
		CustomRoles: make(map[RoleName]*RoleModelConfig),
	}
}

// ParseSwarmRoles parses JSON bytes into a SwarmRolesConfig.
func ParseSwarmRoles(data []byte) (*SwarmRolesConfig, error) {
	if len(data) == 0 {
		return NewSwarmRolesConfig(), nil
	}
	cfg := NewSwarmRolesConfig()
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse swarm roles config: %w", err)
	}
	return cfg, nil
}

// ParseSwarmRolesJSON parses a JSON string into a SwarmRolesConfig.
func ParseSwarmRolesJSON(jsonStr string) (*SwarmRolesConfig, error) {
	return ParseSwarmRoles([]byte(jsonStr))
}

// ResolveRole returns the effective RoleModelConfig for the specified role.
// Precedence:
// 1. Explicit role configuration (Planner, Coder, Reviewer, Summarizer)
// 2. Custom role from CustomRoles map
// 3. Swarm Default fallback configuration
// 4. System baseline default for the role
func (c *SwarmRolesConfig) ResolveRole(role RoleName) RoleModelConfig {
	normalized := NormalizeRole(string(role))

	// 1. Check explicit standard role
	var explicit *RoleModelConfig
	switch normalized {
	case RolePlanner:
		explicit = c.Planner
	case RoleCoder:
		explicit = c.Coder
	case RoleReviewer:
		explicit = c.Reviewer
	case RoleSummarizer:
		explicit = c.Summarizer
	}

	if explicit != nil && explicit.Model != "" {
		return explicit.Clone()
	}

	// 2. Check CustomRoles
	if c.CustomRoles != nil {
		if custom, ok := c.CustomRoles[normalized]; ok && custom != nil && custom.Model != "" {
			return custom.Clone()
		}
	}

	// 3. Check swarm Default
	if c.Default != nil && c.Default.Model != "" {
		return c.Default.Clone()
	}

	// 4. Fall back to system baseline defaults
	baselines := DefaultBaselineRoles()
	if base, ok := baselines[normalized]; ok {
		return base.Clone()
	}

	// Ultimate fallback to generic summarizer baseline model
	return RoleModelConfig{
		Model: "gemini-2.5-flash",
	}
}

// Validate checks that configured roles have valid model identifiers.
func (c *SwarmRolesConfig) Validate() error {
	checkRole := func(name string, cfg *RoleModelConfig) error {
		if cfg == nil {
			return nil
		}
		if strings.TrimSpace(cfg.Model) == "" {
			return fmt.Errorf("role %q configuration has empty model name", name)
		}
		return nil
	}

	if err := checkRole("planner", c.Planner); err != nil {
		return err
	}
	if err := checkRole("coder", c.Coder); err != nil {
		return err
	}
	if err := checkRole("reviewer", c.Reviewer); err != nil {
		return err
	}
	if err := checkRole("summarizer", c.Summarizer); err != nil {
		return err
	}
	if err := checkRole("default", c.Default); err != nil {
		return err
	}
	for k, v := range c.CustomRoles {
		if err := checkRole(string(k), v); err != nil {
			return err
		}
	}
	return nil
}

// ToMap resolves all 4 standard roles into a complete map.
func (c *SwarmRolesConfig) ToMap() map[RoleName]RoleModelConfig {
	m := make(map[RoleName]RoleModelConfig, 4)
	for _, r := range StandardRoles() {
		m[r] = c.ResolveRole(r)
	}
	return m
}
