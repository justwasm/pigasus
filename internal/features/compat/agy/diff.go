// Package agy provides compatibility behaviors, quirks, and upstream drift analysis
// for Antigravity (agy 1.5.14 -> 1.5.15+).
package agy

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Severity represents the urgency of an upstream drift alert.
type Severity string

const (
	SeverityClean    Severity = "CLEAN"
	SeverityInfo     Severity = "INFO"
	SeverityWarning  Severity = "WARNING"
	SeverityCritical Severity = "CRITICAL"
)

// ActionableAlert describes a specific drift item and recommended next step.
type ActionableAlert struct {
	Category       string   `json:"category"` // "tool", "slash_command", "flag", "symbol"
	Item           string   `json:"item"`
	Severity       Severity `json:"severity"`
	Message        string   `json:"message"`
	Recommendation string   `json:"recommendation"`
}

// DriftReport encapsulates the diff between a baseline (e.g. 1.5.14) and a canary (1.5.15+).
type DriftReport struct {
	BaselineVersion string            `json:"baseline_version"`
	CanaryVersion   string            `json:"canary_version"`
	OverallSeverity Severity          `json:"overall_severity"`
	ToolsDiff       ItemDiff          `json:"tools_diff"`
	CommandsDiff    ItemDiff          `json:"commands_diff"`
	FlagsDiff       ItemDiff          `json:"flags_diff"`
	SymbolsDiff     ItemDiff          `json:"symbols_diff"`
	Alerts          []ActionableAlert `json:"alerts"`
}

// ItemDiff describes items added, removed, or kept between versions.
type ItemDiff struct {
	BaselineCount int      `json:"baseline_count"`
	CanaryCount   int      `json:"canary_count"`
	Added         []string `json:"added,omitempty"`
	Removed       []string `json:"removed,omitempty"`
}

// DiffSets compares two string slices and returns sorted Added and Removed slices.
func DiffSets(baseline, canary []string) ItemDiff {
	baseMap := make(map[string]bool, len(baseline))
	for _, s := range baseline {
		baseMap[strings.TrimSpace(s)] = true
	}

	canaryMap := make(map[string]bool, len(canary))
	var added []string
	for _, s := range canary {
		sClean := strings.TrimSpace(s)
		if sClean == "" {
			continue
		}
		canaryMap[sClean] = true
		if !baseMap[sClean] {
			added = append(added, sClean)
		}
	}

	var removed []string
	for _, s := range baseline {
		sClean := strings.TrimSpace(s)
		if sClean != "" && !canaryMap[sClean] {
			removed = append(removed, sClean)
		}
	}

	sort.Strings(added)
	sort.Strings(removed)

	return ItemDiff{
		BaselineCount: len(baseMap),
		CanaryCount:   len(canaryMap),
		Added:         added,
		Removed:       removed,
	}
}

// AnalyzeDrift performs automated parity fuzzing & drift detection between baseline and canary releases.
func AnalyzeDrift(
	baselineVer, canaryVer string,
	baseTools, canaryTools []string,
	baseCmds, canaryCmds []string,
	baseFlags, canaryFlags []string,
	baseSymbols, canarySymbols []string,
) DriftReport {
	toolsDiff := DiffSets(baseTools, canaryTools)
	commandsDiff := DiffSets(baseCmds, canaryCmds)
	flagsDiff := DiffSets(baseFlags, canaryFlags)
	symbolsDiff := DiffSets(baseSymbols, canarySymbols)

	var alerts []ActionableAlert
	overall := SeverityClean

	// Analyze tool drift
	for _, t := range toolsDiff.Added {
		overall = maxSeverity(overall, SeverityCritical)
		alerts = append(alerts, ActionableAlert{
			Category:       "tool",
			Item:           t,
			Severity:       SeverityCritical,
			Message:        fmt.Sprintf("Upstream introduced new tool declaration: %q", t),
			Recommendation: fmt.Sprintf("Add implementation in internal/app/tools/ and register schema in internal/app/tools/surface/"),
		})
	}
	for _, t := range toolsDiff.Removed {
		overall = maxSeverity(overall, SeverityWarning)
		alerts = append(alerts, ActionableAlert{
			Category:       "tool",
			Item:           t,
			Severity:       SeverityWarning,
			Message:        fmt.Sprintf("Upstream deprecated or removed tool declaration: %q", t),
			Recommendation: fmt.Sprintf("Verify whether %q is gated by feature flag or replaced", t),
		})
	}

	// Analyze slash command drift
	for _, c := range commandsDiff.Added {
		overall = maxSeverity(overall, SeverityWarning)
		alerts = append(alerts, ActionableAlert{
			Category:       "slash_command",
			Item:           c,
			Severity:       SeverityWarning,
			Message:        fmt.Sprintf("Upstream introduced new slash command: %q", c),
			Recommendation: fmt.Sprintf("Register command and tab completion in internal/ui/slashcommands/commands.go"),
		})
	}
	for _, c := range commandsDiff.Removed {
		overall = maxSeverity(overall, SeverityInfo)
		alerts = append(alerts, ActionableAlert{
			Category:       "slash_command",
			Item:           c,
			Severity:       SeverityInfo,
			Message:        fmt.Sprintf("Upstream removed slash command: %q", c),
			Recommendation: fmt.Sprintf("Review command retirement notes in upstream changelog"),
		})
	}

	// Analyze CLI flags drift
	for _, f := range flagsDiff.Added {
		overall = maxSeverity(overall, SeverityInfo)
		alerts = append(alerts, ActionableAlert{
			Category:       "flag",
			Item:           f,
			Severity:       SeverityInfo,
			Message:        fmt.Sprintf("Upstream introduced new CLI flag: %q", f),
			Recommendation: fmt.Sprintf("Register flag in internal/core/cliflags/ and cmd/pigasus"),
		})
	}

	// Analyze symbols drift (select relevant symbols e.g. tools_pb, remote_agent_proxy)
	for _, s := range symbolsDiff.Added {
		if strings.Contains(s, "tools_pb") || strings.Contains(s, "remote_agent_proxy") || strings.Contains(s, "subagent") {
			overall = maxSeverity(overall, SeverityWarning)
			alerts = append(alerts, ActionableAlert{
				Category:       "symbol",
				Item:           s,
				Severity:       SeverityWarning,
				Message:        fmt.Sprintf("New architectural symbol discovered: %s", s),
				Recommendation: "Inspect pclntab and protoc descriptor changes in upstream release",
			})
		}
	}

	return DriftReport{
		BaselineVersion: baselineVer,
		CanaryVersion:   canaryVer,
		OverallSeverity: overall,
		ToolsDiff:       toolsDiff,
		CommandsDiff:    commandsDiff,
		FlagsDiff:       flagsDiff,
		SymbolsDiff:     symbolsDiff,
		Alerts:          alerts,
	}
}

// Markdown formats the DriftReport as a readable markdown document.
func (r DriftReport) Markdown() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# Upstream Canary Drift Report (%s -> %s)\n\n", r.BaselineVersion, r.CanaryVersion)
	fmt.Fprintf(&sb, "**Overall Status**: `%s`\n\n", r.OverallSeverity)

	fmt.Fprintf(&sb, "## Summary\n")
	fmt.Fprintf(&sb, "- Tools: %d baseline, %d canary (+%d, -%d)\n",
		r.ToolsDiff.BaselineCount, r.ToolsDiff.CanaryCount, len(r.ToolsDiff.Added), len(r.ToolsDiff.Removed))
	fmt.Fprintf(&sb, "- Slash Commands: %d baseline, %d canary (+%d, -%d)\n",
		r.CommandsDiff.BaselineCount, r.CommandsDiff.CanaryCount, len(r.CommandsDiff.Added), len(r.CommandsDiff.Removed))
	fmt.Fprintf(&sb, "- CLI Flags: %d baseline, %d canary (+%d, -%d)\n",
		r.FlagsDiff.BaselineCount, r.FlagsDiff.CanaryCount, len(r.FlagsDiff.Added), len(r.FlagsDiff.Removed))
	fmt.Fprintf(&sb, "- Pclntab Symbols: %d baseline, %d canary (+%d, -%d)\n\n",
		r.SymbolsDiff.BaselineCount, r.SymbolsDiff.CanaryCount, len(r.SymbolsDiff.Added), len(r.SymbolsDiff.Removed))

	if len(r.Alerts) > 0 {
		fmt.Fprintf(&sb, "## Actionable Alerts\n\n")
		for _, a := range r.Alerts {
			fmt.Fprintf(&sb, "### [%s] %s\n", a.Severity, a.Item)
			fmt.Fprintf(&sb, "- **Category**: %s\n", a.Category)
			fmt.Fprintf(&sb, "- **Message**: %s\n", a.Message)
			fmt.Fprintf(&sb, "- **Action**: %s\n\n", a.Recommendation)
		}
	} else {
		fmt.Fprintf(&sb, "## Parity Status\nAll tracked surfaces are fully aligned. Zero unaligned drift detected.\n")
	}

	return sb.String()
}

// JSON formats the DriftReport as formatted JSON bytes.
func (r DriftReport) JSON() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

func maxSeverity(a, b Severity) Severity {
	ranks := map[Severity]int{
		SeverityClean:    0,
		SeverityInfo:     1,
		SeverityWarning:  2,
		SeverityCritical: 3,
	}
	if ranks[b] > ranks[a] {
		return b
	}
	return a
}
