package storybook

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/palette"
)

// DefaultSnapshotDir specifies the repository location for golden visual snapshots.
const DefaultSnapshotDir = "testdata/snapshots"

// IsUpdateMode returns true if UPDATE_SNAPSHOTS=1 or UPDATE_SNAPSHOTS=true is set.
func IsUpdateMode() bool {
	val := strings.TrimSpace(os.Getenv("UPDATE_SNAPSHOTS"))
	return val == "1" || strings.ToLower(val) == "true"
}

// SnapshotFilename returns the canonical filename for a story's state snapshot.
func SnapshotFilename(storyID, stateID string) string {
	return fmt.Sprintf("%s_%s.golden", storyID, stateID)
}

// SnapshotPath returns the absolute or relative path to the snapshot file.
func SnapshotPath(storyID, stateID string, dir ...string) string {
	targetDir := DefaultSnapshotDir
	if len(dir) > 0 && dir[0] != "" {
		targetDir = dir[0]
	}
	return filepath.Join(targetDir, SnapshotFilename(storyID, stateID))
}

// AssertSnapshot asserts that the actual ScreenBuffer matches the golden snapshot in testdata/snapshots.
// When UPDATE_SNAPSHOTS=1 is set, it creates or overwrites the golden snapshot file.
// When a mismatch occurs, it fails the test and prints a detailed character-level diff report.
func AssertSnapshot(t *testing.T, storyID, stateID string, actual *ScreenBuffer, snapshotDir ...string) {
	t.Helper()

	if actual == nil {
		t.Fatalf("AssertSnapshot: actual ScreenBuffer is nil for story %s [%s]", storyID, stateID)
	}

	targetDir := DefaultSnapshotDir
	if len(snapshotDir) > 0 && snapshotDir[0] != "" {
		targetDir = snapshotDir[0]
	}

	snapPath := filepath.Join(targetDir, SnapshotFilename(storyID, stateID))

	if IsUpdateMode() {
		if err := os.MkdirAll(targetDir, 0755); err != nil {
			t.Fatalf("failed to create snapshot dir %s: %v", targetDir, err)
		}
		encoded := actual.Encode()
		if err := os.WriteFile(snapPath, []byte(encoded), 0644); err != nil {
			t.Fatalf("failed to write golden snapshot %s: %v", snapPath, err)
		}
		t.Logf("UPDATED snapshot: %s", snapPath)
		return
	}

	goldenData, err := os.ReadFile(snapPath)
	if err != nil {
		if os.IsNotExist(err) {
			t.Fatalf("Golden snapshot does not exist for %s [%s] at: %s\nRun with UPDATE_SNAPSHOTS=1 to generate.",
				storyID, stateID, snapPath)
		}
		t.Fatalf("failed to read golden snapshot %s: %v", snapPath, err)
	}

	expected, err := Decode(string(goldenData))
	if err != nil {
		t.Fatalf("failed to parse golden snapshot %s: %v", snapPath, err)
	}

	diffReport := Diff(expected, actual)
	if diffReport != "" {
		t.Errorf("Visual regression in %s [%s] (snapshot %s):\n\n%s",
			storyID, stateID, snapPath, diffReport)
	}
}

// CaptureAndAssertStory renders a story variation and immediately asserts against its golden snapshot.
func CaptureAndAssertStory(t *testing.T, inst StoryInstance, stateIndex int, storyID, stateID string, width, height int, snapshotDir ...string) {
	t.Helper()
	palette.SetScheme("solarized light")
	inst.SetState(stateIndex)
	inst.SetSize(width, height)
	actual := CaptureScreen(inst.View(), width, height)
	AssertSnapshot(t, storyID, stateID, actual, snapshotDir...)
}
