package statusline_test

import (
	"testing"

	"github.com/justwasm/pigasus/internal/statusline"
)

func TestStatuslineFacade(t *testing.T) {
	if statusline.MaxConsecutiveFailures != 30 {
		t.Fatalf("unexpected MaxConsecutiveFailures: %d", statusline.MaxConsecutiveFailures)
	}
	s := &statusline.State{}
	if s.Failures() != 0 {
		t.Fatalf("expected 0 failures, got %d", s.Failures())
	}
}
