package agy

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get wd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not locate repo root containing go.mod from %s", dir)
		}
		dir = parent
	}
}

func TestDriftCheckScript_CleanRun(t *testing.T) {
	root := findRepoRoot(t)
	scriptPath := filepath.Join(root, "scripts", "re", "drift_check.sh")

	cmd := exec.Command("bash", scriptPath, "--no-update-docs")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("drift_check.sh execution failed: %v\nOutput:\n%s", err, string(out))
	}

	outStr := string(out)
	if !strings.Contains(outStr, "[CLEAN]") {
		t.Errorf("expected [CLEAN] in baseline run, got output:\n%s", outStr)
	}
	if !strings.Contains(outStr, "All upstream reference contracts are fully aligned") {
		t.Errorf("expected clean alignment confirmation, got:\n%s", outStr)
	}
}

func TestDriftCheckScript_SimulateCanaryAlerts(t *testing.T) {
	root := findRepoRoot(t)
	scriptPath := filepath.Join(root, "scripts", "re", "drift_check.sh")

	cmd := exec.Command("bash", scriptPath, "--simulate-canary", "--fail-on-drift", "--no-update-docs")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	// Must fail with non-zero exit code due to --fail-on-drift
	if err == nil {
		t.Fatalf("expected non-zero exit code with --fail-on-drift on simulated canary, got success.\nOutput:\n%s", string(out))
	}

	outStr := string(out)
	if !strings.Contains(outStr, "[CRITICAL]") {
		t.Errorf("expected [CRITICAL] badge in output, got:\n%s", outStr)
	}

	// Verify specific alerts were surfaced
	expectedItems := []string{
		"batch_execute",
		"edit_file.Checksum",
		"run_command.TimeoutSeconds",
		"/canary-tune",
		"/parallel-run",
		"--canary-feature-gate",
		"CanaryAgentService",
	}

	for _, item := range expectedItems {
		if !strings.Contains(outStr, item) {
			t.Errorf("expected alert for item %q in output, but was missing:\n%s", item, outStr)
		}
	}
}

func TestDriftCheckScript_JSONReport(t *testing.T) {
	root := findRepoRoot(t)
	scriptPath := filepath.Join(root, "scripts", "re", "drift_check.sh")

	tmpDir := t.TempDir()
	jsonReportPath := filepath.Join(tmpDir, "report.json")
	mdReportPath := filepath.Join(tmpDir, "report.md")

	cmd := exec.Command(
		"bash", scriptPath,
		"--report-json", jsonReportPath,
		"--report-md", mdReportPath,
		"--no-update-docs",
	)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("drift_check.sh JSON run failed: %v\nOutput:\n%s", err, string(out))
	}

	// Verify JSON report
	rawJSON, err := os.ReadFile(jsonReportPath)
	if err != nil {
		t.Fatalf("failed to read generated JSON report: %v", err)
	}

	var report map[string]interface{}
	if err := json.Unmarshal(rawJSON, &report); err != nil {
		t.Fatalf("failed to parse JSON report: %v\nRaw: %s", err, string(rawJSON))
	}

	if report["baseline_version"] != "1.5.14" {
		t.Errorf("expected baseline_version 1.5.14, got %v", report["baseline_version"])
	}
	if report["overall_status"] != "CLEAN" {
		t.Errorf("expected overall_status CLEAN, got %v", report["overall_status"])
	}

	// Verify Markdown report
	rawMD, err := os.ReadFile(mdReportPath)
	if err != nil {
		t.Fatalf("failed to read generated Markdown report: %v", err)
	}
	if !strings.Contains(string(rawMD), "Automated Reference Drift Inspection & Parity Audit") {
		t.Errorf("expected Markdown report header missing: %s", string(rawMD))
	}
}
