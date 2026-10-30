package sandbox_test

import (
	"testing"

	"github.com/justwasm/pigasus/internal/sandbox"
)

func TestSandboxFacade(t *testing.T) {
	sb := sandbox.Resolve(false, "/tmp", "/tmp/home", "conv-1")
	if sb.Enabled {
		t.Fatal("expected sandbox to be disabled")
	}
}
