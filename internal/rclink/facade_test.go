package rclink_test

import (
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/rclink"
)

func TestRclinkFacade(t *testing.T) {
	dl := rclink.DeepLink("inst-1", "")
	if !strings.Contains(dl, "/r/inst-1") {
		t.Fatalf("unexpected DeepLink: %s", dl)
	}
}
