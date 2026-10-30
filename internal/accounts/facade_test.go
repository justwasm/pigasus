package accounts_test

import (
	"testing"

	"github.com/justwasm/pigasus/internal/accounts"
)

func TestAccountsFacade(t *testing.T) {
	if accounts.Version != 1 {
		t.Fatalf("unexpected Version: %d", accounts.Version)
	}
	p := accounts.Path()
	if p == "" {
		t.Fatal("expected non-empty path")
	}
}
