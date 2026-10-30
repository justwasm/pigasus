package persistentrc_test

import (
	"testing"

	"github.com/justwasm/pigasus/internal/persistentrc"
)

func TestPersistentrcFacade(t *testing.T) {
	if persistentrc.DefaultAddr != "0.0.0.0:8787" {
		t.Fatalf("unexpected DefaultAddr: %s", persistentrc.DefaultAddr)
	}
	lbl := persistentrc.Label("foo")
	if lbl == "" {
		t.Fatal("expected non-empty label")
	}
}
