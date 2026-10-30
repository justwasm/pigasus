package agentsdir_test

import (
	"testing"

	"github.com/justwasm/pigasus/internal/agentsdir"
)

func TestAgentsDirFacade(t *testing.T) {
	entries := agentsdir.Discover(t.TempDir(), t.TempDir())
	if len(entries) != 0 {
		t.Fatalf("expected empty entries, got %v", entries)
	}
}
