package atfiles_test

import (
	"testing"

	"github.com/justwasm/pigasus/internal/atfiles"
)

func TestAtFilesFacade(t *testing.T) {
	m, ok := atfiles.Parse("@main.go")
	if !ok || m.Prefix != "main.go" {
		t.Fatalf("unexpected parse: ok=%v prefix=%s", ok, m.Prefix)
	}
	dd := &atfiles.Dropdown{}
	if dd.Visible() {
		t.Fatal("expected newly initialized dropdown to be hidden")
	}
}
