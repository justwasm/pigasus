package store_test

import (
	"testing"

	"github.com/justwasm/pigasus/internal/store"
)

func TestStoreFacade(t *testing.T) {
	mgr := store.NewManager()
	if mgr == nil {
		t.Fatal("expected non-nil manager")
	}
	if store.AuthStateInit.String() != "init" {
		t.Fatalf("unexpected AuthStateInit: %s", store.AuthStateInit)
	}
}
