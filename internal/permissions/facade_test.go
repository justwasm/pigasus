package permissions_test

import (
	"testing"

	"github.com/justwasm/pigasus/internal/permissions"
)

func TestPermissionsFacade(t *testing.T) {
	if !permissions.ValidMode(permissions.ModeAuto) {
		t.Fatal("expected ModeAuto to be valid")
	}

	p := permissions.Policy{Mode: permissions.ModeAuto}
	req := permissions.Request{
		Tool:   "view_file",
		Target: "/tmp/foo.txt",
	}
	verdict := p.Decide(req)
	if verdict != permissions.Allow {
		t.Fatalf("expected Allow, got %v", verdict)
	}

	if permissions.Classify("write_to_file") != permissions.ClassEdit {
		t.Fatal("expected ClassEdit for write_to_file")
	}
}
