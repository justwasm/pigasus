package referencedrift_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/infra/referencedrift"
)

func TestCanary_InspectBinary_TempFile(t *testing.T) {
	tmp := t.TempDir()
	binPath := filepath.Join(tmp, "fake-agy")
	content := []byte("#!/bin/sh\necho 1.5.99\n")
	if err := os.WriteFile(binPath, content, 0o755); err != nil {
		t.Fatal(err)
	}

	info, err := referencedrift.InspectBinary(binPath)
	if err != nil {
		t.Fatalf("InspectBinary error: %v", err)
	}
	if !info.Available {
		t.Errorf("expected Available=true")
	}
	if info.SizeBytes != int64(len(content)) {
		t.Errorf("got size %d, want %d", info.SizeBytes, len(content))
	}
	if info.SHA256 != referencedrift.Digest(content) {
		t.Errorf("got sha256 %s, want %s", info.SHA256, referencedrift.Digest(content))
	}
}

func TestCanary_InspectBinary_LiveIfAvailable(t *testing.T) {
	liveBin := referencedrift.FindBinary()
	if liveBin == "" {
		t.Skip("no live agy binary installed")
	}

	info, err := referencedrift.InspectBinary(liveBin)
	if err != nil {
		t.Fatalf("InspectBinary on live %s: %v", liveBin, err)
	}
	if !info.Available || info.SHA256 == "" || info.Version == "" {
		t.Errorf("incomplete binary info: %+v", info)
	}
	t.Logf("live binary inspected: %s version=%s sha256=%s size=%d",
		info.Path, info.Version, info.SHA256, info.SizeBytes)
}

func TestCanary_InspectSubsystems(t *testing.T) {
	root, err := referencedrift.RepoRoot(".")
	if err != nil {
		t.Fatalf("RepoRoot: %v", err)
	}
	liveBin := referencedrift.FindBinary()

	// 1. Inspect Methods
	mCheck, err := referencedrift.InspectMethods(liveBin, root)
	if err != nil {
		t.Fatalf("InspectMethods: %v", err)
	}
	if mCheck.ExpectedCount != 79 {
		t.Errorf("expected count want 79, got %d", mCheck.ExpectedCount)
	}
	if liveBin != "" {
		if mCheck.LiveCount <= 0 {
			t.Errorf("expected live methods count > 0, got %d", mCheck.LiveCount)
		}
		t.Logf("live methods check: service=%s count=%d expected=%d drifted=%v",
			mCheck.ServiceName, mCheck.LiveCount, mCheck.ExpectedCount, mCheck.Drifted)
	}

	// 2. Inspect Embed Assets
	eCheck, err := referencedrift.InspectEmbedAssets(liveBin, root)
	if err != nil {
		t.Fatalf("InspectEmbedAssets: %v", err)
	}
	if eCheck.ExpectedRecords <= 0 {
		t.Errorf("expected records want > 0, got %d", eCheck.ExpectedRecords)
	}
	if liveBin != "" {
		if eCheck.LiveRecords <= 0 {
			t.Errorf("expected live records > 0, got %d", eCheck.LiveRecords)
		}
		t.Logf("live embed assets: %d records, %d bytes, %d zips, drifted=%v",
			eCheck.LiveRecords, eCheck.TotalBytes, eCheck.ZipArchives, eCheck.Drifted)
	}

	// 3. Inspect Tools
	tCheck, err := referencedrift.InspectTools(liveBin, root)
	if err != nil {
		t.Fatalf("InspectTools: %v", err)
	}
	if tCheck.ExpectedCount != 63 {
		t.Errorf("expected tools count want 63, got %d", tCheck.ExpectedCount)
	}
	if liveBin != "" {
		if tCheck.LiveCount != 63 {
			t.Errorf("expected 63 known tools matched in live binary, got %d", tCheck.LiveCount)
		}
		t.Logf("live tools: %d matched, missing=%v, drifted=%v",
			tCheck.LiveCount, tCheck.Missing, tCheck.Drifted)
	}
}

func TestCanary_RunCanary(t *testing.T) {
	root, err := referencedrift.RepoRoot(".")
	if err != nil {
		t.Fatalf("RepoRoot: %v", err)
	}
	liveBin := referencedrift.FindBinary()

	rep, err := referencedrift.RunCanary(root, liveBin)
	if err != nil {
		t.Fatalf("RunCanary failed: %v", err)
	}

	if rep.PinnedVersion == "" {
		t.Errorf("PinnedVersion is empty")
	}
	if rep.Summary == "" {
		t.Errorf("Summary is empty")
	}

	// Test JSON marshaling
	jsonBytes, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	if !json.Valid(jsonBytes) {
		t.Fatalf("invalid json generated")
	}

	// Test report formatting
	formatted := referencedrift.FormatCanaryReport(rep)
	if !strings.Contains(formatted, "AGY Canary & Reference Drift Report") {
		t.Errorf("formatted report missing header: %s", formatted)
	}
	t.Logf("Canary summary:\n%s", formatted)
}

func TestCanary_AutoUpdateLock_InTempDir(t *testing.T) {
	tmp := t.TempDir()

	// 1. Setup mock repo layout
	lockDir := filepath.Join(tmp, filepath.Dir(referencedrift.LockPath))
	if err := os.MkdirAll(lockDir, 0o755); err != nil {
		t.Fatal(err)
	}

	artRelPath := "fixtures/sample.txt"
	artFullPath := filepath.Join(tmp, artRelPath)
	if err := os.MkdirAll(filepath.Dir(artFullPath), 0o755); err != nil {
		t.Fatal(err)
	}
	initContent := []byte("initial version 1")
	if err := os.WriteFile(artFullPath, initContent, 0o644); err != nil {
		t.Fatal(err)
	}

	initialLock := &referencedrift.Lock{
		Version: "1.0.0",
		Artifacts: []referencedrift.Artifact{
			{
				Path:       artRelPath,
				CapturedAt: "1.0.0",
				Digest:     referencedrift.Digest(initContent),
			},
		},
	}
	if err := referencedrift.WriteLock(tmp, initialLock); err != nil {
		t.Fatalf("WriteLock: %v", err)
	}

	// 2. Modify artifact to simulate drift
	updatedContent := []byte("updated version 2 with changes")
	if err := os.WriteFile(artFullPath, updatedContent, 0o644); err != nil {
		t.Fatal(err)
	}

	// 3. Create a fake agy binary
	fakeBin := filepath.Join(tmp, "fake-agy")
	binContent := []byte("#!/bin/sh\necho 1.1.0\n")
	if err := os.WriteFile(fakeBin, binContent, 0o755); err != nil {
		t.Fatal(err)
	}

	// 4. Run AutoUpdateLock
	res, err := referencedrift.AutoUpdateLock(tmp, referencedrift.AutoUpdateOptions{
		BinaryPath:         fakeBin,
		UpdateVersion:      true,
		UpdateBinarySHA256: true,
		AcknowledgeLag:     true,
		LagNote:            "auto-updated in test",
	})
	if err != nil {
		t.Fatalf("AutoUpdateLock failed: %v", err)
	}

	if !res.Updated {
		t.Errorf("expected Updated=true")
	}
	if res.NewVersion != "1.1.0" {
		t.Errorf("expected NewVersion=1.1.0, got %s", res.NewVersion)
	}
	if len(res.ChangedArtifacts) != 1 || res.ChangedArtifacts[0] != artRelPath {
		t.Errorf("expected ChangedArtifacts=[%s], got %v", artRelPath, res.ChangedArtifacts)
	}

	// 5. Reload lock and verify state
	loaded, err := referencedrift.LoadFrom(filepath.Join(tmp, referencedrift.LockPath))
	if err != nil {
		t.Fatalf("reload lock: %v", err)
	}
	if loaded.Version != "1.1.0" {
		t.Errorf("loaded version got %s, want 1.1.0", loaded.Version)
	}
	if loaded.Artifacts[0].Digest != referencedrift.Digest(updatedContent) {
		t.Errorf("loaded digest got %s, want %s", loaded.Artifacts[0].Digest, referencedrift.Digest(updatedContent))
	}
	if loaded.BinarySHA256 != referencedrift.Digest(binContent) {
		t.Errorf("loaded binary sha256 got %s, want %s", loaded.BinarySHA256, referencedrift.Digest(binContent))
	}
}
