package implicit_test

import (
	"testing"

	"github.com/justwasm/pigasus/internal/implicit"
)

func TestImplicitFacade(t *testing.T) {
	if implicit.Key != "safeCodeiumworldKeYsecretBalloon" {
		t.Fatalf("unexpected Key: %s", implicit.Key)
	}
	uri := implicit.WorkspaceURI("/tmp/workspace")
	if uri != "file:///tmp/workspace" {
		t.Fatalf("unexpected WorkspaceURI: %s", uri)
	}
}
