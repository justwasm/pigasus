package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/infra/referencedrift"
)

func TestCLI_Version(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--version"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected code 0, got %d", code)
	}
	if !strings.Contains(stdout.String(), "agy-canary") {
		t.Errorf("stdout missing version info: %s", stdout.String())
	}
}

func TestCLI_Help(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"-h"}, &stdout, &stderr)
	if code != 0 && code != 2 {
		t.Fatalf("unexpected exit code %d", code)
	}
}

func TestCLI_RunCanary_JSON(t *testing.T) {
	root, err := referencedrift.RepoRoot(".")
	if err != nil {
		t.Fatalf("RepoRoot: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"-repo", root, "-json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected code 0 in non-check mode, got %d, stderr: %s", code, stderr.String())
	}

	var rep referencedrift.CanaryReport
	if err := json.Unmarshal(stdout.Bytes(), &rep); err != nil {
		t.Fatalf("failed to parse JSON output: %v\noutput: %s", err, stdout.String())
	}
	if rep.PinnedVersion == "" {
		t.Errorf("expected PinnedVersion in JSON")
	}
}

func TestCLI_RunCanary_CheckMode(t *testing.T) {
	root, err := referencedrift.RepoRoot(".")
	if err != nil {
		t.Fatalf("RepoRoot: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"-repo", root, "-check"}, &stdout, &stderr)
	// In 1.5.14 live binary, methods drifted 79 -> 141 and embed 279 -> 513, so drift is detected and code is 1!
	t.Logf("check mode returned exit code %d (expected 1 when drift is present)", code)
	if !strings.Contains(stdout.String(), "AGY Canary & Reference Drift Report") {
		t.Errorf("expected report output in stdout, got:\n%s", stdout.String())
	}
}

func TestCLI_UpdateMode_InTempDir(t *testing.T) {
	tmp := t.TempDir()

	lockDir := filepath.Join(tmp, filepath.Dir(referencedrift.LockPath))
	if err := os.MkdirAll(lockDir, 0o755); err != nil {
		t.Fatal(err)
	}

	initialLock := &referencedrift.Lock{
		Version:   "1.0.0",
		Artifacts: []referencedrift.Artifact{},
	}
	if err := referencedrift.WriteLock(tmp, initialLock); err != nil {
		t.Fatalf("WriteLock: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"-repo", tmp, "-update", "-json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("update failed with code %d: %s", code, stderr.String())
	}

	var res referencedrift.AutoUpdateResult
	if err := json.Unmarshal(stdout.Bytes(), &res); err != nil {
		t.Fatalf("parse update result JSON: %v", err)
	}
	if res.NewVersion != "1.0.0" {
		t.Errorf("got version %s, want 1.0.0", res.NewVersion)
	}
}
