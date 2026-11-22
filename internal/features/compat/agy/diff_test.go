package agy

import (
	"strings"
	"testing"
)

func TestDiffSets(t *testing.T) {
	base := []string{"read_file", "write_file", "run_command"}
	canary := []string{"read_file", "write_file", "run_command", "smart_patch"}

	diff := DiffSets(base, canary)
	if len(diff.Added) != 1 || diff.Added[0] != "smart_patch" {
		t.Fatalf("expected Added ['smart_patch'], got %v", diff.Added)
	}
	if len(diff.Removed) != 0 {
		t.Fatalf("expected 0 removed, got %v", diff.Removed)
	}
	if diff.BaselineCount != 3 || diff.CanaryCount != 4 {
		t.Fatalf("counts mismatch: base=%d canary=%d", diff.BaselineCount, diff.CanaryCount)
	}
}

func TestAnalyzeDrift_AlertsAndSeverities(t *testing.T) {
	baseTools := []string{"read_file", "write_file", "run_command"}
	canaryTools := []string{"read_file", "write_file", "run_command", "fast_search"}

	baseCmds := []string{"/help", "/exit", "/model"}
	canaryCmds := []string{"/help", "/exit", "/model", "/boost"}

	baseFlags := []string{"--model", "--backend"}
	canaryFlags := []string{"--model", "--backend", "--canary"}

	baseSymbols := []string{"internal/abi.TypeOf"}
	canarySymbols := []string{"internal/abi.TypeOf", "tools_pb.(*FastSearch).Reset"}

	report := AnalyzeDrift(
		"1.5.14", "1.5.15",
		baseTools, canaryTools,
		baseCmds, canaryCmds,
		baseFlags, canaryFlags,
		baseSymbols, canarySymbols,
	)

	if report.OverallSeverity != SeverityCritical {
		t.Fatalf("expected SeverityCritical due to new tool, got %s", report.OverallSeverity)
	}

	if len(report.Alerts) < 3 {
		t.Fatalf("expected at least 3 alerts, got %d", len(report.Alerts))
	}

	// Verify markdown contains actionable recommendation
	md := report.Markdown()
	if !strings.Contains(md, "fast_search") || !strings.Contains(md, "Add implementation") {
		t.Errorf("markdown does not contain actionable recommendation: %s", md)
	}

	// Verify JSON output
	jsonBytes, err := report.JSON()
	if err != nil {
		t.Fatalf("JSON marshal failed: %v", err)
	}
	if !strings.Contains(string(jsonBytes), "CRITICAL") {
		t.Errorf("JSON output does not contain severity: %s", string(jsonBytes))
	}
}

func TestAnalyzeDrift_ZeroDriftIsClean(t *testing.T) {
	tools := []string{"read_file", "write_file"}
	cmds := []string{"/help"}
	flags := []string{"--model"}
	symbols := []string{"main.main"}

	report := AnalyzeDrift("1.5.14", "1.5.14", tools, tools, cmds, cmds, flags, flags, symbols, symbols)
	if report.OverallSeverity != SeverityClean {
		t.Fatalf("expected clean, got %s", report.OverallSeverity)
	}
	if len(report.Alerts) != 0 {
		t.Fatalf("expected 0 alerts, got %d", len(report.Alerts))
	}
	if !strings.Contains(report.Markdown(), "Zero unaligned drift detected") {
		t.Errorf("expected clean message in markdown, got:\n%s", report.Markdown())
	}
}
