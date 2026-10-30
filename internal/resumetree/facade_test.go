package resumetree_test

import (
	"testing"

	"github.com/justwasm/pigasus/internal/resumetree"
)

func TestResumeTreeFacade(t *testing.T) {
	items := []resumetree.Item{
		{ID: "c1", Title: "first"},
	}
	groups := resumetree.Build(items, "")
	if len(groups) == 0 {
		t.Fatal("expected at least one group")
	}
}
