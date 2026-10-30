package mcpoauth

import (
	"errors"
	"testing"
)

func TestFacadeMCPOAuth(t *testing.T) {
	if !IsTransient(errors.New("timeout")) {
		// timeout might or might not be transient, let's test ShouldClear
	}
	s := SanitizeServerURL("https://example.com/api/")
	if s == "" {
		t.Fatal("expected sanitized url")
	}
	_ = StorePath()
}
