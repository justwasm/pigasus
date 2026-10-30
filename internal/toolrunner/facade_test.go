package toolrunner_test

import (
	"testing"

	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/toolrunner"
)

func TestToolrunnerFacade(t *testing.T) {
	call := backend.ToolCall{
		Name:      "read_file",
		Arguments: []byte(`{"path": "/tmp/test"}`),
	}
	target := toolrunner.ToolTarget(call)
	if target != "/tmp/test" {
		t.Fatalf("expected /tmp/test, got %q", target)
	}
}
