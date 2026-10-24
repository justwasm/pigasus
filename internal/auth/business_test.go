package auth_test

import (
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/auth"
)

func TestIsBusinessAccount(t *testing.T) {
	t.Setenv("AGY_ADC_AUTH", "")
	if auth.IsBusinessAccount() {
		t.Fatal("expected false when AGY_ADC_AUTH unset")
	}

	t.Setenv("AGY_ADC_AUTH", "1")
	if !auth.IsBusinessAccount() {
		t.Fatal("expected true when AGY_ADC_AUTH=1")
	}

	t.Setenv("AGY_ADC_AUTH", "   ")
	if auth.IsBusinessAccount() {
		t.Fatal("expected false when AGY_ADC_AUTH whitespace")
	}
}

func TestTelemetryBusinessDescription(t *testing.T) {
	desc := auth.TelemetryBusinessDescription()
	if !strings.Contains(desc, "Disabled for Business accounts") {
		t.Errorf("description missing expected prefix: %q", desc)
	}
	if !strings.Contains(desc, auth.TelemetryProductName) {
		t.Errorf("description missing product name: %q", desc)
	}
}
