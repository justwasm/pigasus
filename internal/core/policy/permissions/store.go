package permissions

import (
	"context"
	"errors"
	"strings"
	"time"
)

// Preset is the closed set of presets the dashboard and remote control
// can choose between via SetRemoteAgentPermissionPreset.
type Preset string

const (
	// PresetDefault is the host's policy-baseline.
	PresetDefault Preset = "default"
	// PresetStrict refuses every tool call that would normally require user confirmation.
	PresetStrict Preset = "strict"
	// PresetPermissive auto-allows every tool call without confirmation.
	PresetPermissive Preset = "permissive"
)

// IsValidPreset reports whether preset is one of the three constants above.
func IsValidPreset(p Preset) bool {
	switch p {
	case PresetDefault,
		PresetStrict,
		PresetPermissive:
		return true
	}
	return false
}

// ToolPermission maps Preset to the CLI toolPermission setting vocabulary.
func (p Preset) ToolPermission() string {
	switch p {
	case PresetStrict:
		return ToolPermissionStrict
	case PresetPermissive:
		return ToolPermissionAlwaysProceed
	case PresetDefault:
		return ToolPermissionRequestReview
	default:
		return ""
	}
}

// PresetFromToolPermission maps a CLI toolPermission string onto a Preset.
func PresetFromToolPermission(tp string) Preset {
	switch tp {
	case ToolPermissionStrict:
		return PresetStrict
	case ToolPermissionAlwaysProceed:
		return PresetPermissive
	default:
		return PresetDefault
	}
}

// ReviewMode is the closed set of modes the dashboard and remote control
// can choose between via SetRemoteAgentArtifactReviewMode.
type ReviewMode string

const (
	// ReviewModeAlways forces every artifact the host produces to wait for
	// explicit human review before delivery.
	ReviewModeAlways ReviewMode = "always"
	// ReviewModeNever skips the review step entirely.
	ReviewModeNever ReviewMode = "never"
	// ReviewModeAuto lets the host decide per artifact.
	ReviewModeAuto ReviewMode = "auto"
)

// IsValidReviewMode reports whether mode is one of the three constants above.
func IsValidReviewMode(m ReviewMode) bool {
	switch m {
	case ReviewModeAlways,
		ReviewModeNever,
		ReviewModeAuto:
		return true
	}
	return false
}

// Grant is one rule the user has auto-allowed (or revoked).
type Grant struct {
	Action   string    `json:"action,omitempty"`
	ToolName string    `json:"tool_name"`
	Pattern  string    `json:"pattern"`
	Reason   string    `json:"reason,omitempty"`
	AddedAt  time.Time `json:"added_at"`
}

// GrantKey returns the dedup key for a grant.
func (g Grant) GrantKey() string {
	return GrantKeyFor(g.Action, g.ToolName, g.Pattern)
}

// ToRule converts the grant into a Policy Rule.
func (g Grant) ToRule() Rule {
	return Rule{
		Action:  g.Action,
		Tool:    g.ToolName,
		Pattern: g.Pattern,
	}
}

// State is the on-disk and wire shape representing permissions state.
type State struct {
	Version   int        `json:"version"`
	Preset    Preset     `json:"preset"`
	Review    ReviewMode `json:"review"`
	Grants    []Grant    `json:"grants"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// ToRules converts all grants in the state to Policy Rules.
func (s State) ToRules() []Rule {
	if len(s.Grants) == 0 {
		return nil
	}
	rules := make([]Rule, len(s.Grants))
	for i, g := range s.Grants {
		rules[i] = g.ToRule()
	}
	return rules
}

// ToSettings converts state into CLI Settings.
func (s State) ToSettings() Settings {
	return Settings{
		Allow: s.ToRules(),
	}
}

// Store is the unified storage contract for permission state.
type Store interface {
	Get(ctx context.Context) (State, error)
	SetPreset(ctx context.Context, preset Preset) (State, error)
	SetReview(ctx context.Context, mode ReviewMode) (State, error)
	AddGrants(ctx context.Context, grants []Grant) (State, error)
	RemoveGrants(ctx context.Context, keys []string) (State, error)
}

// Sentinel errors.
var (
	ErrGrantExists    = errors.New("rc-permissions: grant already exists")
	ErrGrantMalformed = errors.New("rc-permissions: grant key malformed")
)

// StateVersion is the on-disk format version the current store writes.
const StateVersion = 1

// DefaultPreset and DefaultReviewMode baseline values.
const (
	DefaultPreset     Preset     = PresetDefault
	DefaultReviewMode ReviewMode = ReviewModeAlways
)

func normaliseToolName(name string) string {
	return strings.TrimSpace(name)
}

func normaliseAction(action string) string {
	return strings.TrimSpace(action)
}

func normalisePattern(pattern string) string {
	return strings.TrimSpace(pattern)
}

// ParseToolPattern parses the legacy "<tool_name>:<pattern>" encoding.
func ParseToolPattern(s string) (tool, pattern string, ok bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", "", false
	}
	idx := strings.IndexByte(s, ':')
	if idx < 0 {
		return "", "", false
	}
	tool = normaliseToolName(s[:idx])
	pattern = normalisePattern(s[idx+1:])
	if tool == "" || pattern == "" {
		return "", "", false
	}
	return tool, pattern, true
}

// GrantKeyFor renders the dedup key of a grant.
func GrantKeyFor(action, tool, pattern string) string {
	if action != "" {
		return action + "(" + pattern + ")"
	}
	return tool + ":" + pattern
}

// GrantParseError renders a refused grant string for a wire envelope.
func GrantParseError(err error) string {
	var invalid *InvalidGrantError
	if errors.As(err, &invalid) {
		return err.Error() + " (" + GrantFormatHint + ")"
	}
	return err.Error()
}

// ParseGrantKey parses one grant key into a Grant.
func ParseGrantKey(s string) (Grant, error) {
	action, target, err := ParseGrantString(s)
	if err == nil {
		return Grant{Action: action, Pattern: target}, nil
	}
	var invalid *InvalidGrantError
	if !errors.As(err, &invalid) {
		// A grant-shaped string the reference refuses.
		return Grant{}, err
	}
	tool, pattern, ok := ParseToolPattern(s)
	if !ok {
		return Grant{}, ErrInvalidGrantString(strings.TrimSpace(s))
	}
	return Grant{ToolName: tool, Pattern: pattern}, nil
}
