package crush

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestCrushRemote_EndToEndWithBinary(t *testing.T) {
	crushBin := "/tmp/crush-bin"
	if _, err := os.Stat(crushBin); err != nil {
		t.Skip("skipping integration test: /tmp/crush-bin not found")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tmpDir := t.TempDir()
	remote, err := SpawnCrushRemote(ctx, crushBin, tmpDir, "default")
	if err != nil {
		t.Fatalf("SpawnCrushRemote failed: %v", err)
	}
	defer remote.Close()

	if remote.ThreadID() == "" {
		t.Errorf("expected non-empty thread ID, got empty")
	}

	models, err := remote.ListModels(ctx)
	if err != nil {
		t.Fatalf("ListModels failed: %v", err)
	}
	if len(models) == 0 {
		t.Errorf("expected at least one model, got 0")
	}

	t.Logf("Initialized CrushRemote successfully: thread=%s, defaultModel=%s, modelCount=%d",
		remote.ThreadID(), remote.DefaultModel(), len(models))
}
