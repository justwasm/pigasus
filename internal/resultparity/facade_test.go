package resultparity_test

import (
	"testing"

	"github.com/justwasm/pigasus/internal/resultparity"
)

func TestResultparityFacade(t *testing.T) {
	s := resultparity.Section{
		Name: "test",
		Body: "ok",
	}
	if s.Name != "test" || s.Body != "ok" {
		t.Fatalf("unexpected section: %+v", s)
	}
}
