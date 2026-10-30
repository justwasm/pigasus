package cumulativeprompt_test

import (
	"testing"

	"github.com/justwasm/pigasus/internal/cumulativeprompt"
)

func TestCumulativePromptFacade(t *testing.T) {
	if cumulativeprompt.RoleUser != "user" {
		t.Fatalf("unexpected RoleUser: %s", cumulativeprompt.RoleUser)
	}
	msgs := cumulativeprompt.Convert(nil, cumulativeprompt.Options{})
	if len(msgs) != 0 {
		t.Fatalf("expected 0 msgs, got %d", len(msgs))
	}
}
